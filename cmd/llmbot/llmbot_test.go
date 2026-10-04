package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

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
		got := digested(c.state)
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
	got := raw(observation(1, 47.6, barrier(1, 30.7), traffic(0, 88.2)))
	want := `{"lane":1,"speed":47.6,"obstacles":[{"lane":1,"ahead":30.7,"speed":0},{"lane":0,"ahead":88.2,"speed":20}]}`
	if got.state != want || got.instructions != rawInstructions {
		t.Errorf("raw = %+v", got)
	}

	var many []game.ObstacleState
	for i := range 20 {
		many = append(many, barrier(i%4, float64(10*i)))
	}
	var r rawState
	if err := json.Unmarshal([]byte(raw(observation(1, 40, many...)).state), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Obstacles) != rawObstacles || r.Obstacles[rawObstacles-1].Ahead != 70 {
		t.Errorf("obstacles = %+v", r.Obstacles)
	}
	if empty := raw(observation(1, 40)).state; !strings.Contains(empty, `"obstacles":[]`) {
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
		a := answer{decision: c.d, tick: 100, lane: 1}
		if got := judge(a, c.nowTick, c.nowLane, c.maxStale, c.minConfidence); got != c.want {
			t.Errorf("%s: outcome %d, want %d", c.name, got, c.want)
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
	s := state{RunID: 3}
	s.Tick, s.Score, s.Speed = 600, 900, 51
	got := st.summary(s, true, 10*time.Second, 2911)
	for _, want := range []string{
		"run 3 crashed: 600 ticks, 0 rows passed, score 900 (speed 51)",
		"decisions  20, 2.0 per second",
		"median 110 ms, p95 200 ms",
		"3 moves sent, 12 stay, 5 stale (25%)",
		"2 unusable",
		"2911 MB",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary lacks %q:\n%s", want, got)
		}
	}
	if got := (&stats{}).summary(state{}, false, 0, -1); !strings.Contains(got, "stopped") || !strings.Contains(got, "could not read") {
		t.Errorf("empty summary:\n%s", got)
	}
}

func TestRunnerMemoryIsReadFromTheRunnerProcess(t *testing.T) {
	ps := `  41152 ollama serve
5327392 /opt/homebrew/Cellar/ollama/0.35.1_1/libexec/lib/ollama/llama-server --model /Users/x/.ollama/models/blobs/sha256-6dd7 --port 51234
   1024 /usr/bin/some-editor llama-notes.txt
`
	if got := parseRunnerRSS(ps); got != 5327392/1024 {
		t.Errorf("rss = %d MB", got)
	}
	if got := parseRunnerRSS("  41152 ollama serve\n"); got != -1 {
		t.Errorf("no runner: rss = %d", got)
	}
}
