package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"myjev/internal/game"
)

// newTestServer starts the HTTP side only. Tests advance the game by
// calling tick themselves, so nothing depends on real time.
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	web := fstest.MapFS{"index.html": {Data: []byte("<title>outrun</title>")}}
	s := New(game.DefaultConfig(), 42, web)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return s, ts
}

type reply struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error"`
	Controller string `json:"controller"`
}

func post(t *testing.T, ts *httptest.Server, path, body string, header ...string) (int, reply) {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var r reply
	json.NewDecoder(resp.Body).Decode(&r)
	return resp.StatusCode, r
}

func getState(t *testing.T, ts *httptest.Server) State {
	t.Helper()
	resp, err := http.Get(ts.URL + "/state")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st State
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStateAndViewer(t *testing.T) {
	_, ts := newTestServer(t)
	st := getState(t, ts)
	if st.Seed != 42 || st.RunID != 1 || st.Controller != "human" || st.Lanes != 4 || st.GameOver {
		t.Fatalf("unexpected first state: %+v", st)
	}
	if len(st.Grid.Rows) != 12 || len(st.Obstacles) == 0 {
		t.Fatalf("state is missing the grid or obstacles: %+v", st)
	}
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d", resp.StatusCode)
	}
}

func TestInputIsAppliedOnePerTick(t *testing.T) {
	s, ts := newTestServer(t)
	start := getState(t, ts).Lane
	for range 2 {
		if code, r := post(t, ts, "/input", `{"action":"left"}`); code != 200 || !r.OK {
			t.Fatalf("input: %d %+v", code, r)
		}
	}
	if got := getState(t, ts).Lane; got != start {
		t.Fatalf("lane changed before a tick: %d", got)
	}
	s.tick()
	if got := getState(t, ts).Lane; got != start-1 {
		t.Fatalf("after one tick lane = %d, want %d", got, start-1)
	}
	s.tick()
	if got := getState(t, ts).Lane; got != start-2 {
		t.Fatalf("after two ticks lane = %d, want %d", got, start-2)
	}
}

func TestInputQueueIsBounded(t *testing.T) {
	s, ts := newTestServer(t)
	for range 20 {
		post(t, ts, "/input", `{"action":"right"}`)
	}
	s.mu.Lock()
	queued := len(s.queue)
	s.mu.Unlock()
	if queued != maxQueuedActions {
		t.Fatalf("queued %d actions, want %d", queued, maxQueuedActions)
	}
}

func TestBadRequests(t *testing.T) {
	_, ts := newTestServer(t)
	if code, r := post(t, ts, "/input", `{"action":"up"}`); code != 400 || r.OK {
		t.Fatalf("unknown action: %d %+v", code, r)
	}
	if code, _ := post(t, ts, "/input", `not json`); code != 400 {
		t.Fatalf("bad JSON: %d", code)
	}
	if code, _ := post(t, ts, "/input", `{"action":"left"}`, "Origin", "http://evil.example"); code != 403 {
		t.Fatalf("cross-origin input: %d, want 403", code)
	}
	if code, _ := post(t, ts, "/control/claim", `{"name":"x"}`, "Origin", "http://evil.example"); code != 403 {
		t.Fatalf("cross-origin claim: %d, want 403", code)
	}
	if code, _ := post(t, ts, "/input", `{"action":"left"}`, "Origin", ts.URL); code != 200 {
		t.Fatalf("same-origin input: %d, want 200", code)
	}
	for _, name := range []string{"", "two words", strings.Repeat("x", 33)} {
		body, _ := json.Marshal(map[string]string{"name": name})
		if code, _ := post(t, ts, "/control/claim", string(body)); code != 400 {
			t.Fatalf("claim with name %q: %d, want 400", name, code)
		}
	}
}

func TestResetStartsANewRun(t *testing.T) {
	s, ts := newTestServer(t)
	for range 30 {
		s.tick()
	}
	if code, _ := post(t, ts, "/reset", `{"seed":7}`); code != 200 {
		t.Fatalf("reset: %d", code)
	}
	st := getState(t, ts)
	if st.Seed != 7 || st.RunID != 2 || st.Tick != 0 || st.BestScore == 0 {
		t.Fatalf("after reset: seed %d run %d tick %d best %d", st.Seed, st.RunID, st.Tick, st.BestScore)
	}
	if code, _ := post(t, ts, "/reset", ``); code != 200 {
		t.Fatalf("reset with empty body: %d", code)
	}
	if st := getState(t, ts); st.RunID != 3 {
		t.Fatalf("run id = %d, want 3", st.RunID)
	}
}

func TestOnlyTheControllerSteers(t *testing.T) {
	s, ts := newTestServer(t)
	start := getState(t, ts).Lane

	if code, _ := post(t, ts, "/input", `{"action":"left","name":"bot"}`); code != 409 {
		t.Fatalf("input from a bot that has not claimed: %d, want 409", code)
	}
	if code, r := post(t, ts, "/control/claim", `{"name":"bot"}`); code != 200 || r.Controller != "bot" {
		t.Fatalf("claim: %d %+v", code, r)
	}
	if got := getState(t, ts).Controller; got != "bot" {
		t.Fatalf("controller = %q, want bot", got)
	}
	if code, _ := post(t, ts, "/input", `{"action":"left"}`); code != 409 {
		t.Fatalf("human input while the bot drives: %d, want 409", code)
	}
	if code, _ := post(t, ts, "/control/claim", `{"name":"other"}`); code != 409 {
		t.Fatalf("second bot claiming: %d, want 409", code)
	}
	if code, _ := post(t, ts, "/control/release", `{"name":"other"}`); code != 409 {
		t.Fatalf("release by a non-controller: %d, want 409", code)
	}
	if code, _ := post(t, ts, "/input", `{"action":"left","name":"bot"}`); code != 200 {
		t.Fatalf("bot input: %d", code)
	}
	s.tick()
	if got := getState(t, ts).Lane; got != start-1 {
		t.Fatalf("lane = %d, want %d", got, start-1)
	}
	if code, r := post(t, ts, "/control/release", `{"name":"bot"}`); code != 200 || r.Controller != "human" {
		t.Fatalf("release: %d %+v", code, r)
	}
	if code, _ := post(t, ts, "/input", `{"action":"left"}`); code != 200 {
		t.Fatalf("human input after release: %d", code)
	}
}

func TestHumanCanTakeOverAndPendingMovesAreDropped(t *testing.T) {
	s, ts := newTestServer(t)
	start := getState(t, ts).Lane
	post(t, ts, "/control/claim", `{"name":"bot"}`)
	post(t, ts, "/input", `{"action":"right","name":"bot"}`)
	if code, r := post(t, ts, "/control/claim", `{"name":"human"}`); code != 200 || r.Controller != "human" {
		t.Fatalf("human take-over: %d %+v", code, r)
	}
	s.tick()
	if got := getState(t, ts).Lane; got != start {
		t.Fatalf("the bot's queued move was applied after take-over: lane %d, want %d", got, start)
	}
}

// --- WebSocket ---

type wsMsg struct {
	Type  string `json:"type"`
	Error string `json:"error"`
	State State  `json:"state"`
}

func dial(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func send(t *testing.T, conn *websocket.Conn, msg string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatal(err)
	}
}

// readUntil reads messages until one satisfies want.
func readUntil(t *testing.T, conn *websocket.Conn, what string, want func(wsMsg) bool) wsMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		var m wsMsg
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if want(m) {
			return m
		}
	}
}

// waitFor polls the server until cond holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestWebSocketStreamsStateAndTakesInput(t *testing.T) {
	s, ts := newTestServer(t)
	conn := dial(t, ts)

	first := readUntil(t, conn, "first state", func(m wsMsg) bool { return m.Type == "state" })
	if first.State.Seed != 42 || first.State.Controller != "human" {
		t.Fatalf("first state: %+v", first.State)
	}
	start := first.State.Lane

	send(t, conn, `{"type":"input","action":"right"}`)
	waitFor(t, "the input to be queued", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.queue) == 1
	})
	s.tick()
	readUntil(t, conn, "state with the new lane", func(m wsMsg) bool {
		return m.Type == "state" && m.State.Lane == start+1
	})

	send(t, conn, `{"type":"input","action":"sideways"}`)
	readUntil(t, conn, "error for a bad action", func(m wsMsg) bool { return m.Type == "error" })
	send(t, conn, `{"type":"dance"}`)
	readUntil(t, conn, "error for an unknown type", func(m wsMsg) bool { return m.Type == "error" })

	send(t, conn, `{"type":"reset","seed":9}`)
	readUntil(t, conn, "state of the new run", func(m wsMsg) bool {
		return m.Type == "state" && m.State.Seed == 9 && m.State.RunID == 2
	})
}

func TestWebSocketClaimAndDisconnect(t *testing.T) {
	s, ts := newTestServer(t)
	bot := dial(t, ts)
	viewer := dial(t, ts)

	send(t, bot, `{"type":"claim","name":"bot"}`)
	readUntil(t, viewer, "viewer to see the bot driving", func(m wsMsg) bool {
		return m.Type == "state" && m.State.Controller == "bot"
	})

	// The viewer's keyboard is ignored while the bot drives.
	send(t, viewer, `{"type":"input","action":"left"}`)
	readUntil(t, viewer, "error for input from the viewer", func(m wsMsg) bool { return m.Type == "error" })

	start := s.state().Lane
	send(t, bot, `{"type":"input","action":"left"}`)
	waitFor(t, "the bot's input to be queued", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.queue) == 1
	})
	s.tick()
	if got := s.state().Lane; got != start-1 {
		t.Fatalf("lane = %d, want %d", got, start-1)
	}

	// Dropping the connection hands the car back.
	bot.CloseNow()
	readUntil(t, viewer, "control to return to the human", func(m wsMsg) bool {
		return m.Type == "state" && m.State.Controller == "human"
	})
	send(t, viewer, `{"type":"input","action":"left"}`)
	waitFor(t, "the viewer's input to be queued", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.queue) == 1
	})
}

func TestWebSocketReleaseAndHumanTakeOver(t *testing.T) {
	s, ts := newTestServer(t)
	bot := dial(t, ts)
	viewer := dial(t, ts)

	send(t, bot, `{"type":"claim","name":"bot"}`)
	waitFor(t, "bot to drive", func() bool { return s.controllerName() == "bot" })
	send(t, bot, `{"type":"release"}`)
	waitFor(t, "release", func() bool { return s.controllerName() == "human" })

	send(t, bot, `{"type":"claim","name":"bot"}`)
	waitFor(t, "bot to drive again", func() bool { return s.controllerName() == "bot" })
	send(t, viewer, `{"type":"claim","name":"human"}`)
	waitFor(t, "human take-over", func() bool { return s.controllerName() == "human" })

	// The bot is told its input no longer counts.
	send(t, bot, `{"type":"input","action":"left"}`)
	readUntil(t, bot, "error for input after take-over", func(m wsMsg) bool { return m.Type == "error" })
}

func TestWebSocketRejectsOtherOrigins(t *testing.T) {
	_, ts := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {"http://evil.example"}},
	})
	if err == nil {
		conn.CloseNow()
		t.Fatal("a connection from another origin was accepted")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", resp)
	}
}
