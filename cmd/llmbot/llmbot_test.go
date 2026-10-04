package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

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
	for _, c := range cases {
		if got := digest(c.state); got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
	}
}

func TestTevAsksOneChoiceQuestionAndReadsTheAnswer(t *testing.T) {
	var got systemoneRequest
	var path string
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Write([]byte(`{"model":"tev1:0.8b","answers":{"move":{"type":"choice","choice":"right",
			"probabilities":{"left":0.1,"none":0.2,"right":0.7},"confidence":0.45}}}`))
	}))
	defer ollama.Close()

	model := &tev{url: ollama.URL, model: "tev1:0.8b", client: ollama.Client()}
	d, err := model.decide(context.Background(), "the state")
	if err != nil {
		t.Fatal(err)
	}
	if d.action != "right" || d.confidence != 0.45 || d.probability != 0.7 {
		t.Errorf("decision = %+v", d)
	}

	want := systemoneRequest{
		Model: "tev1:0.8b",
		State: "the state",
		Questions: map[string]systemoneQuestion{
			"move": {Type: "choice", Instructions: instructions, Criteria: criteria},
		},
	}
	if path != "/v1/systemone" || !reflect.DeepEqual(got, want) {
		t.Errorf("request to %s = %+v", path, got)
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
		ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}))
		model := &tev{url: ollama.URL, model: "tev1:0.8b", client: ollama.Client()}
		if d, err := model.decide(context.Background(), "the state"); err == nil {
			t.Errorf("%s: got %+v, want an error", name, d)
		}
		ollama.Close()
	}
}
