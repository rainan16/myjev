package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"myjev/internal/game"
)

func barrier(lane int, ahead float64) game.ObstacleState {
	return game.ObstacleState{Lane: lane, Ahead: ahead, Length: 1, Type: game.KindObstacle}
}

func traffic(lane int, ahead float64) game.ObstacleState {
	return game.ObstacleState{Lane: lane, Ahead: ahead, Length: 4, Speed: 20, Type: game.KindCar}
}

func observation(lane int, speed float64, obstacles ...game.ObstacleState) game.State {
	return game.State{TickRate: 30, Lanes: 4, Lane: lane, CarLength: 4, Speed: speed, Obstacles: obstacles}
}

func TestDigestDescribesTheLaneEachMoveLeadsTo(t *testing.T) {
	// Layouts taken from a run on seed 7.
	tightRow := []game.ObstacleState{traffic(3, 17.9), barrier(0, 32.5), barrier(1, 32.5), barrier(2, 130.3), barrier(0, 130.3)}
	cases := []struct {
		name  string
		state game.State
		want  string
	}{
		{
			"empty road",
			observation(1, 40),
			"Lane to the left: clear. Current lane: clear. Lane to the right: clear.",
		},
		{
			"only the own lane is open",
			observation(2, 44.1, tightRow...),
			"Lane to the left: blocked soon. Current lane: clear. Lane to the right: blocked soon.",
		},
		{
			"own lane blocked, right is open",
			observation(1, 44.1, tightRow...),
			"Lane to the left: blocked soon. Current lane: blocked soon. Lane to the right: clear.",
		},
		{
			"road edge on the left, a clear lane two to the right",
			observation(0, 48.7, barrier(2, 26), barrier(1, 26), barrier(1, 128.5), barrier(3, 219.6)),
			"Lane to the left: wall. Current lane: clear. Lane to the right: blocked soon, clear lane beyond.",
		},
		{
			"two lanes from safety",
			observation(3, 47.7, traffic(3, 13.9), barrier(2, 23.9), barrier(2, 111.2), barrier(1, 111.2)),
			"Lane to the left: blocked soon, clear lane beyond. Current lane: blocked soon. Lane to the right: wall.",
		},
		{
			"barriers alongside",
			observation(1, 45.8, barrier(2, -1.5), barrier(0, -1.5), traffic(3, 107.5)),
			"Lane to the left: blocked now. Current lane: clear. Lane to the right: blocked now.",
		},
		{
			"too late to cross a blocked lane",
			observation(3, 47.7, traffic(3, 8), barrier(2, 7.5)),
			"Lane to the left: blocked soon. Current lane: blocked soon. Lane to the right: wall.",
		},
		{
			"an obstacle beyond the horizon does not count yet",
			observation(1, 45, barrier(1, 200)),
			"Lane to the left: clear. Current lane: clear. Lane to the right: clear.",
		},
	}
	all := allSentences()
	for _, c := range cases {
		got := digested(c.state, 0)
		if got.state != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got.state, c.want)
		}
		if got.instructions != digestedInstructions {
			t.Errorf("%s: instructions = %q", c.name, got.instructions)
		}
		if !slices.Contains(all, got.state) {
			t.Errorf("%s: %q is not among the warm-up sentences", c.name, got.state)
		}
	}
}

func TestWarmUpSentencesAreAllDifferent(t *testing.T) {
	all := allSentences()
	seen := map[string]bool{}
	for _, s := range all {
		if seen[s] {
			t.Errorf("twice: %s", s)
		}
		seen[s] = true
	}
	// Five words for each side, three for the current lane, and no road
	// with a wall on both sides.
	if want := 5*3*5 - 3; len(all) != want {
		t.Errorf("%d sentences, want %d", len(all), want)
	}
}

func TestRawStateKeepsTheNearestObstaclesOnly(t *testing.T) {
	got := raw(observation(1, 47.6, barrier(1, 30.7), traffic(0, 88.2)), 0)
	want := `{"lane":1,"speed":47.6,"obstacles":[{"lane":1,"ahead":30.7,"speed":0},{"lane":0,"ahead":88.2,"speed":20}]}`
	if got.state != want || got.instructions != rawInstructions {
		t.Errorf("raw = %+v", got)
	}

	var many []game.ObstacleState
	for i := range 20 {
		many = append(many, barrier(i%4, float64(10*i)))
	}
	var r rawState
	if err := json.Unmarshal([]byte(raw(observation(1, 40, many...), 0).state), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Obstacles) != rawObstacles || r.Obstacles[rawObstacles-1].Ahead != 70 {
		t.Errorf("obstacles = %+v", r.Obstacles)
	}
	if empty := raw(observation(1, 40), 0).state; !strings.Contains(empty, `"obstacles":[]`) {
		t.Errorf("empty road = %s", empty)
	}
}

// stubOllama answers every request with the given status and body and
// records the last request.
func stubOllama(t *testing.T, status int, body string) (o ollama, path *string, request *[]byte) {
	path, request = new(string), new([]byte)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path
		*request, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return ollama{srv.URL, "some-model", srv.Client()}, path, request
}

func TestTevAsksOneChoiceQuestionAndReadsTheAnswer(t *testing.T) {
	o, path, request := stubOllama(t, http.StatusOK, `{"answers":{"move":{"type":"choice","choice":"right",
		"probabilities":{"left":0.1,"none":0.2,"right":0.7},"confidence":0.45}}}`)

	d, err := (&tev{o}).decide(context.Background(), prompt{"the state", "the question"})
	if err != nil {
		t.Fatal(err)
	}
	if d.action != "right" || !d.hasConfidence || d.confidence != 0.45 || d.probability != 0.7 {
		t.Errorf("decision = %+v", d)
	}

	var got systemoneRequest
	if err := json.Unmarshal(*request, &got); err != nil {
		t.Fatal(err)
	}
	want := systemoneRequest{
		Model: "some-model",
		State: "the state",
		Questions: map[string]systemoneQuestion{
			"move": {Type: "choice", Instructions: "the question", Criteria: criteria},
		},
	}
	if *path != "/v1/systemone" || !reflect.DeepEqual(got, want) {
		t.Errorf("request to %s = %+v", *path, got)
	}
}

func TestTevRejectsAReplyItCannotUse(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
	}{
		"error status": {http.StatusBadRequest, `{"error":"model does not support decision"}`},
		"no answer":    {http.StatusOK, `{"answers":{}}`},
		"unknown move": {http.StatusOK, `{"answers":{"move":{"choice":"up"}}}`},
		"not JSON":     {http.StatusOK, `oops`},
	}
	for name, c := range cases {
		o, _, _ := stubOllama(t, c.status, c.body)
		if d, err := (&tev{o}).decide(context.Background(), prompt{"the state", "the question"}); err == nil {
			t.Errorf("%s: got %+v, want an error", name, d)
		}
	}
}

func TestGemmaIsPinnedToAMoveByASchema(t *testing.T) {
	o, path, request := stubOllama(t, http.StatusOK, `{"message":{"role":"assistant","content":"{\"move\": \"left\"}"}}`)

	d, err := (&gemma{o}).decide(context.Background(), prompt{"the state", "the question"})
	if err != nil {
		t.Fatal(err)
	}
	if d.action != "left" || d.hasConfidence {
		t.Errorf("decision = %+v", d)
	}

	var got map[string]any
	if err := json.Unmarshal(*request, &got); err != nil {
		t.Fatal(err)
	}
	if *path != "/api/chat" || got["think"] != false || got["stream"] != false {
		t.Errorf("request to %s = %s", *path, *request)
	}
	if temperature := got["options"].(map[string]any)["temperature"]; temperature != 0.0 {
		t.Errorf("temperature = %v", temperature)
	}
	move := got["format"].(map[string]any)["properties"].(map[string]any)["move"].(map[string]any)
	if !reflect.DeepEqual(move["enum"], []any{"left", "none", "right"}) {
		t.Errorf("format = %v", got["format"])
	}
	content := got["messages"].([]any)[0].(map[string]any)["content"].(string)
	want := "State: the state\n\nthe question\n- left: move one lane to the left\n- none: stay in the current lane\n- right: move one lane to the right"
	if content != want {
		t.Errorf("content = %q", content)
	}
}

func TestGemmaReplyWithoutAMoveIsUnusableNotFatal(t *testing.T) {
	for name, content := range map[string]string{
		"prose":        `I would go left.`,
		"unknown move": `{\"move\": \"up\"}`,
		"empty":        ``,
	} {
		o, _, _ := stubOllama(t, http.StatusOK, `{"message":{"content":"`+content+`"}}`)
		if _, err := (&gemma{o}).decide(context.Background(), prompt{}); !errors.Is(err, errUnusable) {
			t.Errorf("%s: err = %v, want errUnusable", name, err)
		}
	}
	o, _, _ := stubOllama(t, http.StatusInternalServerError, `{"error":"out of memory"}`)
	if _, err := (&gemma{o}).decide(context.Background(), prompt{}); err == nil || errors.Is(err, errUnusable) {
		t.Errorf("server error: err = %v, want a fatal error", err)
	}
}

func TestDecisionModelsAreCalledOnSystemoneOthersOnChat(t *testing.T) {
	o, path, _ := stubOllama(t, http.StatusOK, `{"capabilities":["decision"]}`)
	if d, err := newDecider(context.Background(), o, "auto"); err != nil || reflect.TypeOf(d) != reflect.TypeFor[*tev]() {
		t.Errorf("decision model: %T, %v", d, err)
	}
	if *path != "/api/show" {
		t.Errorf("asked %s", *path)
	}

	o, _, _ = stubOllama(t, http.StatusOK, `{"capabilities":["completion","vision","thinking"]}`)
	if d, err := newDecider(context.Background(), o, "auto"); err != nil || reflect.TypeOf(d) != reflect.TypeFor[*gemma]() {
		t.Errorf("chat model: %T, %v", d, err)
	}

	o, _, _ = stubOllama(t, http.StatusNotFound, `{"error":"model not found"}`)
	if d, err := newDecider(context.Background(), o, "auto"); err == nil {
		t.Errorf("missing model: got %T", d)
	}
	if d, err := newDecider(context.Background(), o, "telepathy"); err == nil {
		t.Errorf("unknown api: got %T", d)
	}
}

func TestAnswerIsDroppedWhenStaleGatedOrOvertaken(t *testing.T) {
	sure := decision{action: "left", hasConfidence: true, confidence: 0.8}
	unsure := decision{action: "left", hasConfidence: true, confidence: 0.2}
	chat := decision{action: "left"}
	stay := decision{action: "none", hasConfidence: true, confidence: 0.1}
	cases := []struct {
		name          string
		d             decision
		nowTick       int
		nowLane       int
		maxStale      int
		minConfidence float64
		want          outcome
	}{
		{"fresh move", sure, 102, 1, 6, 0, sent},
		{"exactly at the limit", sure, 106, 1, 6, 0, sent},
		{"one tick past the limit", sure, 107, 1, 6, 0, stale},
		{"old, but staleness is off", sure, 400, 1, -1, 0, sent},
		{"stay", stay, 102, 1, 6, 0, stayed},
		{"stay is never gated", stay, 102, 1, 6, 0.5, stayed},
		{"an old stay counts as stale", stay, 110, 1, 6, 0, stale},
		{"below the gate", unsure, 102, 1, 6, 0.5, gated},
		{"above the gate", sure, 102, 1, 6, 0.5, sent},
		{"gate off", unsure, 102, 1, 6, 0, sent},
		{"no confidence to gate on", chat, 102, 1, 6, 0.5, sent},
		{"the car has moved on", sure, 102, 2, 6, 0, laneChanged},
	}
	for _, c := range cases {
		a := answer{decision: c.d, tick: 100, lane: 1, seen: observation(1, 50)}
		now := observation(c.nowLane, 50)
		now.Tick = c.nowTick
		if got := judge(a, now, 0, c.maxStale, c.minConfidence); got != c.want {
			t.Errorf("%s: outcome %d, want %d", c.name, got, c.want)
		}
	}
}

// configOfTheQuickLook is the game the models were watched on. The start
// speed has been raised since, which moves every row.
func configOfTheQuickLook() game.Config {
	cfg := game.DefaultConfig()
	cfg.StartSpeed, cfg.RampSpeed = 40, 90
	return cfg
}

// Seed 7, tick 824, car in lane 1: the lane to the right has a barrier
// 9 ticks away and a clear lane beyond it. Both tev1:4b and gemma4:e2b
// answered "right"; the answer came 5 to 6 ticks later, when the barrier
// was too close to cross, and the car drove into it.
func TestMoveIsHeldWhenItsLaneGotWorseWhileTheModelAnswered(t *testing.T) {
	g := game.New(configOfTheQuickLook(), 7)
	at := map[int]game.State{}
	for !g.GameOver() && g.Snapshot().Tick <= 831 {
		s := g.Snapshot()
		g.Step(game.Solve(s))
		s.Lane = 1 // where the models had the car
		at[s.Tick] = s
	}
	right := decision{action: "right", hasConfidence: true, confidence: 0.86}
	left := decision{action: "left", hasConfidence: true, confidence: 0.86}
	for _, c := range []struct {
		name       string
		d          decision
		asked, now int
		want       outcome
	}{
		{"tev1:4b, answer 6 ticks old", right, 824, 830, worsened},
		{"gemma4:e2b, answer 5 ticks old", right, 826, 831, worsened},
		{"an answer that came at once", right, 824, 825, sent},
		{"the other lane is as it was", left, 824, 830, sent},
	} {
		a := answer{decision: c.d, tick: c.asked, lane: 1, seen: at[c.asked]}
		if got := judge(a, at[c.now], 0, 6, 0); got != c.want {
			t.Errorf("%s: outcome %d, want %d", c.name, got, c.want)
		}
	}
}

// The same moment on seed 7. The barrier to the right is 9 ticks away:
// a driver that moves on every tick can cross that lane to the clear one
// beyond, a model that takes 5 ticks to answer cannot. gemma4:e2b was
// told "clear lane beyond", moved right and never made the second move.
func TestClearLaneBeyondAllowsForHowLongTheDriverTakesToAnswer(t *testing.T) {
	g := game.New(configOfTheQuickLook(), 7)
	for g.Snapshot().Tick < 824 {
		g.Step(game.Solve(g.Snapshot()))
	}
	s := g.Snapshot()
	s.Lane = 1
	for reaction, want := range map[int]string{
		0: sentence(wordClear, wordClear, wordBeyond),
		2: sentence(wordClear, wordClear, wordBeyond),
		3: sentence(wordClear, wordClear, wordSoon),
		6: sentence(wordClear, wordClear, wordSoon),
	} {
		if got := digested(s, reaction).state; got != want {
			t.Errorf("reaction %d ticks:\n got %s\nwant %s", reaction, got, want)
		}
	}
}

// Seed 7 with the car starting at 50, lane 1, speed 81. The next row is
// inside the 60-tick horizon before the last one is behind the car, so
// no lane is free for the whole horizon. For 34 ticks the lane to the
// left had 28 ticks more room than the car's own, every lane read
// "blocked soon", and tev1:4b stayed where it was until it crashed.
func TestLaneThatOutlastsTheNearestBlockageIsClear(t *testing.T) {
	cfg := game.DefaultConfig()
	cfg.StartSpeed, cfg.RampSpeed = 50, 100
	g := game.New(cfg, 7)
	for g.Snapshot().Tick < 1702 {
		s := g.Snapshot()
		g.Step(game.Solve(s))
		if s.Tick < 1674 {
			continue
		}
		s.Lane = 1 // where the model had the car
		// The lane to the right leads to the clear lane 3 for a while,
		// so only the two lanes that decide the move are checked.
		free := clearTicks(s)
		if left, current := laneWord(s, free, -1, 6, 0), laneWord(s, free, 0, 6, 0); left != wordClear || current != wordSoon {
			t.Fatalf("tick %d, free for %v ticks: left %q, current %q; want %q and %q",
				s.Tick, free, left, current, wordClear, wordSoon)
		}
	}

	for _, c := range []struct {
		name string
		free []int
		want int
	}{
		{"nothing near: the whole horizon", []int{60, 60, 60, 60}, 60},
		{"a row ahead: outlast it by the margin", []int{11, 56, 11, 11}, 23},
		{"a row being passed does not count", []int{0, 41, 41, 0}, 53},
		{"never more than the horizon", []int{59, 59, 60, 60}, 60},
	} {
		if got := clearFrom(c.free); got != c.want {
			t.Errorf("%s: clear from %d ticks, want %d", c.name, got, c.want)
		}
	}
}

// Seed 7 with the car starting at 50, lane 0. The row ahead leaves only
// lane 2 open and comes into sight 21 ticks before it is met. tev1:4b
// answered "right" 6 ticks after it was asked. The check before sending
// held the move, because it asked for room for two answers when one was
// already given, and the car stayed in lane 0 until it crashed.
func TestMoveTowardsAClearLaneBeyondIsSentWhenTheRestCanStillBeDone(t *testing.T) {
	cfg := game.DefaultConfig()
	cfg.StartSpeed, cfg.RampSpeed = 50, 100
	g := game.New(cfg, 7)
	at := map[int]game.State{}
	for g.Snapshot().Tick <= 1725 {
		s := g.Snapshot()
		g.Step(game.Solve(s))
		s.Lane = 0 // where the model had the car
		at[s.Tick] = s
	}
	const reaction = 6
	if got, want := digested(at[1713], reaction).state, sentence(wordWall, wordSoon, wordBeyond); got != want {
		t.Fatalf("asked:\n got %s\nwant %s", got, want)
	}
	right := answer{decision: decision{action: "right"}, tick: 1713, lane: 0, seen: at[1713]}
	for _, c := range []struct {
		name string
		now  int
		want outcome
	}{
		{"the answer as it came, 6 ticks old", 1719, sent},
		{"too late for the second move", 1725, worsened},
	} {
		if got := judge(right, at[c.now], reaction, -1, 0); got != c.want {
			t.Errorf("%s: outcome %d, want %d (free for %v ticks)", c.name, got, c.want, clearTicks(at[c.now]))
		}
	}
}

func TestRowsAreCountedOnceAndTheCrashRowIsNotPassed(t *testing.T) {
	at := func(id int, ahead float64) game.ObstacleState {
		return game.ObstacleState{ID: id, Lane: 0, Ahead: ahead, Length: 1}
	}
	var st stats
	for _, obstacles := range [][]game.ObstacleState{
		{at(1, 20), at(2, 20), at(3, 90)},
		{at(1, 0), at(2, 0), at(3, 70)},   // first row met
		{at(1, -2), at(2, -2), at(3, 68)}, // still the same row
		{at(3, 30)},
		{at(3, -0.5), at(4, 80)}, // second row met
	} {
		st.observe(game.State{Obstacles: obstacles})
	}
	if st.rowsPassed(false) != 2 || st.rowsPassed(true) != 1 {
		t.Errorf("rows passed: %d running, %d crashed", st.rowsPassed(false), st.rowsPassed(true))
	}
	if empty := (&stats{}); empty.rowsPassed(true) != 0 {
		t.Errorf("crash before any row: %d", empty.rowsPassed(true))
	}
}

func TestSummaryReportsRateLatencyAndStaleShare(t *testing.T) {
	var st stats
	for i := 1; i <= 20; i++ {
		o := stayed
		switch {
		case i <= 5:
			o = stale
		case i <= 8:
			o = sent
		}
		st.decided(decision{latency: time.Duration(i) * 10 * time.Millisecond}, o)
	}
	st.unusable = 2
	s := state{RunID: 3, Pace: 0.5}
	s.Tick, s.Score, s.Speed = 600, 900, 51
	got := st.summary(s, true, 10*time.Second, runnerMemory{2911, 1})
	for _, want := range []string{
		"run 3 crashed at pace 0.5: 600 ticks, 0 rows passed, score 900 (speed 51)",
		"decisions  20, 2.0 per second",
		"median 110 ms, p95 200 ms",
		"3 moves sent, 12 stay, 5 stale (25%)",
		"0 held as the lane got worse",
		"2 unusable",
		"2911 MB in Ollama's model runner",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
	if got := (&stats{}).summary(state{}, false, 0, runnerMemory{}); !strings.Contains(got, "stopped") || !strings.Contains(got, "could not read") {
		t.Errorf("empty summary:\n%s", got)
	}
	if got := st.summary(s, true, time.Second, runnerMemory{9000, 2}); !strings.Contains(got, "9000 MB in 2 model runners") {
		t.Errorf("summary with two models loaded:\n%s", got)
	}
}

func TestStatusTextNamesStateFormLatencyAndConfidence(t *testing.T) {
	d := decision{action: "left", hasConfidence: true, confidence: 0.934, latency: 21 * time.Millisecond}
	if got := statusText("digested", d); got != "digested  21 ms  conf 0.93" {
		t.Errorf("status = %q", got)
	}
	if got := statusText("raw", decision{latency: 248 * time.Millisecond}); got != "raw  248 ms  no confidence" {
		t.Errorf("status without confidence = %q", got)
	}
}

func TestRunnerMemoryIsReadFromTheRunnerProcess(t *testing.T) {
	ps := `  41152 ollama serve
5327392 /opt/homebrew/Cellar/ollama/0.35.1_1/libexec/lib/ollama/llama-server --model /Users/x/.ollama/models/blobs/sha256-6dd7 --port 51234
   1024 /usr/bin/some-editor llama-notes.txt
`
	if got := parseRunnerRSS(ps); got != (runnerMemory{5327392 / 1024, 1}) {
		t.Errorf("rss = %+v", got)
	}
	if got := parseRunnerRSS(ps + "1048576 /opt/ollama/llama-server --model other\n"); got != (runnerMemory{5327392/1024 + 1024, 2}) {
		t.Errorf("two runners: rss = %+v", got)
	}
	if got := parseRunnerRSS("  41152 ollama serve\n"); got.processes != 0 {
		t.Errorf("no runner: rss = %+v", got)
	}
}

// asked is a model that reports each question it gets and always stays.
type asked chan prompt

func (a asked) decide(ctx context.Context, p prompt) (decision, error) {
	select {
	case a <- p:
	case <-ctx.Done():
	}
	return decision{action: "none"}, nil
}

func TestBotResetsOnceItIsShownDrivingAndPlaysTheNextRun(t *testing.T) {
	// The first state this server sends already has the bot driving, as
	// happens when a slow connection misses the state it is greeted
	// with. The bot must take that run as not its own, reset, and play
	// the run after it.
	problem := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		expect := func(want string) bool {
			var m struct {
				Type string `json:"type"`
			}
			_, data, err := conn.Read(ctx)
			if err != nil {
				return false
			}
			if json.Unmarshal(data, &m); m.Type != want {
				problem <- "got " + string(data) + ", want a " + want
				return false
			}
			return true
		}
		show := func(run, tick int) {
			conn.Write(ctx, websocket.MessageText, fmt.Appendf(nil,
				`{"type":"state","state":{"run_id":%d,"tick":%d,"controller":"driver","tick_rate":30,"lanes":4,"lane":2,"car_length":4,"speed":40,"pace":1}}`, run, tick))
		}
		if !expect("claim") {
			return
		}
		show(5, 80)
		if !expect("reset") {
			return
		}
		show(6, 0)
		conn.Read(ctx) // hold the connection until the bot leaves
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := make(asked)
	b := &bot{name: "driver", seed: -1, maxStale: 6, build: digested, form: "digested", model: model}
	done := make(chan error, 1)
	go func() { done <- b.play(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")) }()

	<-model // warm-up
	select {
	case <-model: // a question about the run the reset started
	case msg := <-problem:
		t.Fatal(msg)
	case err := <-done:
		t.Fatalf("the bot stopped before asking anything: %v", err)
	}
	cancel()
	<-done
}
