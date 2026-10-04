package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"myjev/internal/game"
)

// decision is one answer from a model.
type decision struct {
	action      string // "left", "none" or "right"
	confidence  float64
	probability float64 // of the chosen action
	latency     time.Duration
}

// tev asks a decision model on Ollama's /v1/systemone endpoint: a state
// and one typed choice question in, a choice with probabilities out.
type tev struct {
	url    string // base URL of Ollama
	model  string
	client *http.Client
}

type systemoneRequest struct {
	Model     string                       `json:"model"`
	State     string                       `json:"state"`
	Questions map[string]systemoneQuestion `json:"questions"`
}

type systemoneQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type systemoneReply struct {
	Answers map[string]struct {
		Choice        string             `json:"choice"`
		Probabilities map[string]float64 `json:"probabilities"`
		Confidence    float64            `json:"confidence"`
	} `json:"answers"`
}

func (t *tev) decide(ctx context.Context, state string) (decision, error) {
	body, err := json.Marshal(systemoneRequest{
		Model: t.model,
		State: state,
		Questions: map[string]systemoneQuestion{
			"move": {Type: "choice", Instructions: instructions, Criteria: criteria},
		},
	})
	if err != nil {
		return decision{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := t.client.Do(req)
	if err != nil {
		return decision{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	latency := time.Since(start)
	if err != nil {
		return decision{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return decision{}, fmt.Errorf("%s: %s: %s", t.model, resp.Status, bytes.TrimSpace(data))
	}

	var reply systemoneReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return decision{}, fmt.Errorf("%s: %w", t.model, err)
	}
	move, ok := reply.Answers["move"]
	if !ok {
		return decision{}, fmt.Errorf("%s: no answer in reply", t.model)
	}
	if _, ok := game.ParseAction(move.Choice); !ok {
		return decision{}, fmt.Errorf("%s: unknown move %q", t.model, move.Choice)
	}
	return decision{
		action:      move.Choice,
		confidence:  move.Confidence,
		probability: move.Probabilities[move.Choice],
		latency:     latency,
	}, nil
}
