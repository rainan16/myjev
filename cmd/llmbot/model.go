package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"myjev/internal/game"
)

// decision is one answer from a model.
type decision struct {
	action string // "left", "none" or "right"
	// A decision model reports how sure it is. A chat model does not.
	hasConfidence bool
	confidence    float64
	probability   float64 // of the chosen action
	latency       time.Duration
}

// decider is a model that picks a move. The two kinds of model need
// different calls; this is what they have in common.
type decider interface {
	decide(ctx context.Context, p prompt) (decision, error)
}

// errUnusable marks a reply that arrived but holds no move. The bot
// counts it and carries on; any other error stops the run.
var errUnusable = errors.New("reply holds no move")

// ollama is the connection both kinds of model are called through.
type ollama struct {
	url    string // base URL
	model  string
	client *http.Client
}

// post sends a JSON body and reports the reply and how long it took.
func (o ollama) post(ctx context.Context, path string, body any) ([]byte, time.Duration, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.url+path, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	latency := time.Since(start)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("%s: %s: %s", o.model, resp.Status, bytes.TrimSpace(reply))
	}
	return reply, latency, nil
}

// newDecider picks the call that fits the model. api is "systemone",
// "chat" or "auto", which asks Ollama whether the model is a decision
// model.
func newDecider(ctx context.Context, o ollama, api string) (decider, error) {
	if api == "auto" {
		reply, _, err := o.post(ctx, "/api/show", map[string]string{"model": o.model})
		if err != nil {
			return nil, err
		}
		var show struct {
			Capabilities []string `json:"capabilities"`
		}
		if err := json.Unmarshal(reply, &show); err != nil {
			return nil, fmt.Errorf("%s: %w", o.model, err)
		}
		api = "chat"
		if slices.Contains(show.Capabilities, "decision") {
			api = "systemone"
		}
	}
	switch api {
	case "systemone":
		return &tev{o}, nil
	case "chat":
		return &gemma{o}, nil
	}
	return nil, fmt.Errorf("unknown api %q", api)
}

// tev asks a decision model on Ollama's /v1/systemone endpoint: a state
// and one typed choice question in, a choice with probabilities out.
type tev struct{ ollama }

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

func (t *tev) decide(ctx context.Context, p prompt) (decision, error) {
	data, latency, err := t.post(ctx, "/v1/systemone", systemoneRequest{
		Model: t.model,
		State: p.state,
		Questions: map[string]systemoneQuestion{
			"move": {Type: "choice", Instructions: p.instructions, Criteria: criteria},
		},
	})
	if err != nil {
		return decision{}, err
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
		action:        move.Choice,
		hasConfidence: true,
		confidence:    move.Confidence,
		probability:   move.Probabilities[move.Choice],
		latency:       latency,
	}, nil
}

// gemma asks a general-purpose model on /api/chat. Such a model answers
// in text, so the harness has to pin it down: thinking off, temperature
// 0, and a JSON schema that leaves room for nothing but the move.
type gemma struct{ ollama }

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Think    bool          `json:"think"`
	Format   any           `json:"format"`
	Options  chatOptions   `json:"options"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	Temperature float64 `json:"temperature"`
}

var moveSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"move": map[string]any{"type": "string", "enum": []string{"left", "none", "right"}},
	},
	"required": []string{"move"},
}

func (g *gemma) decide(ctx context.Context, p prompt) (decision, error) {
	content := fmt.Sprintf("State: %s\n\n%s\n- left: %s\n- none: %s\n- right: %s",
		p.state, p.instructions, criteria["left"], criteria["none"], criteria["right"])
	data, latency, err := g.post(ctx, "/api/chat", chatRequest{
		Model:    g.model,
		Messages: []chatMessage{{Role: "user", Content: content}},
		Format:   moveSchema,
	})
	if err != nil {
		return decision{}, err
	}
	var reply struct {
		Message chatMessage `json:"message"`
	}
	if err := json.Unmarshal(data, &reply); err != nil {
		return decision{}, fmt.Errorf("%s: %w", g.model, err)
	}
	// The text is the model's own, so it may be anything.
	var answer struct {
		Move string `json:"move"`
	}
	if err := json.Unmarshal([]byte(reply.Message.Content), &answer); err != nil {
		return decision{latency: latency}, fmt.Errorf("%w: %q", errUnusable, reply.Message.Content)
	}
	if _, ok := game.ParseAction(answer.Move); !ok {
		return decision{latency: latency}, fmt.Errorf("%w: %q", errUnusable, reply.Message.Content)
	}
	return decision{action: answer.Move, latency: latency}, nil
}
