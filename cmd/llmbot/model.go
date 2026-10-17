package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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
	// choices holds one answer per variant of a fan-out prompt, in the
	// order of the prompt; the bot picks one when the answer's age is known.
	choices []choice
	latency time.Duration
	retries int // times the call was repeated because the service was busy
	tokens  int // input tokens the service counted, where it reports them
}

// choice is one answer to one variant of a fan-out prompt.
type choice struct {
	action                  string
	confidence, probability float64
}

// decider is a model that picks a move. The two kinds of model need
// different calls; this is what they have in common.
type decider interface {
	decide(ctx context.Context, p prompt) (decision, error)
}

// errUnusable marks a reply that arrived but holds no move. The bot
// counts it and carries on; any other error stops the run.
var errUnusable = errors.New("reply holds no move")

// errSlow marks a call to a hosted model that got no reply in time. The
// bot counts it like an unusable reply and asks again on a newer state.
var errSlow = errors.New("no reply in time")

// errBusy marks a reply that says to come back later: 429 or 529.
var errBusy = errors.New("service is busy")

// ollama is the connection a model is called through. Hosted Jev takes
// the same call as Tev on Ollama, so it is the same connection with a
// key and with patience for a busy service. Claude is reached the same
// way, with the key in another header.
type ollama struct {
	url    string // base URL
	model  string
	client *http.Client

	key     string // sent as a bearer token when set; never logged
	version string // Anthropic API version; with it the key goes in x-api-key
	// workspace is sent to Anthropic when set. A key that is not tied to
	// a workspace is refused without it.
	workspace string
	retries   int           // how often a busy reply is tried again
	backoff   time.Duration // wait before the first retry, doubled each time
}

const (
	typesafeURL = "https://api.typesafe.ai"
	// jevModel is pinned: jev-latest can move and change the answers.
	jevModel = "jev-1.13.0"
	// hostedTimeout is how long a hosted call may take. An answer that
	// late is stale at any pace, so it is given up and not waited for.
	hostedTimeout = 2 * time.Second
)

// hosted is the connection to Jev on TypeSafe's API.
func hosted(model, key string) ollama {
	return ollama{
		url:     typesafeURL,
		model:   model,
		client:  &http.Client{Timeout: hostedTimeout},
		key:     key,
		retries: 3,
		backoff: 100 * time.Millisecond,
	}
}

const (
	anthropicURL     = "https://api.anthropic.com"
	anthropicVersion = "2023-06-01"
	// claudeModel is the general-purpose LLM that needs no local runner.
	claudeModel = "claude-haiku-4-5"
)

// hostedClaude is the connection to Claude on Anthropic's API.
func hostedClaude(model, key, workspace string) ollama {
	o := hosted(model, key)
	o.url, o.version, o.workspace = anthropicURL, anthropicVersion, workspace
	return o
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
	switch {
	case o.version != "":
		req.Header.Set("x-api-key", o.key)
		req.Header.Set("anthropic-version", o.version)
		if o.workspace != "" {
			req.Header.Set("anthropic-workspace-id", o.workspace)
		}
	case o.key != "":
		req.Header.Set("Authorization", "Bearer "+o.key)
	}

	start := time.Now()
	resp, err := o.client.Do(req)
	if err != nil {
		var timeout net.Error
		if o.key != "" && ctx.Err() == nil && errors.As(err, &timeout) && timeout.Timeout() {
			return nil, 0, fmt.Errorf("%s: %w", o.model, errSlow)
		}
		return nil, 0, err
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	latency := time.Since(start)
	if err != nil {
		return nil, 0, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return reply, latency, nil
	case http.StatusTooManyRequests, 529:
		return nil, 0, fmt.Errorf("%s: %w: %s", o.model, errBusy, resp.Status)
	}
	return nil, 0, fmt.Errorf("%s: %s: %s", o.model, resp.Status, bytes.TrimSpace(reply))
}

// ask posts like post, and asks a busy service again after a wait. The
// wait counts as latency: the answer is that much older when it arrives.
func (o ollama) ask(ctx context.Context, path string, body any) (data []byte, latency time.Duration, retries int, err error) {
	start := time.Now()
	data, latency, err = o.post(ctx, path, body)
	for ; errors.Is(err, errBusy) && retries < o.retries; retries++ {
		select {
		case <-ctx.Done():
			return nil, 0, retries, ctx.Err()
		case <-time.After(o.backoff << retries):
		}
		data, _, err = o.post(ctx, path, body)
		latency = time.Since(start)
	}
	return data, latency, retries, err
}

// newDecider picks the call that fits the model. api is "systemone",
// "chat", "messages" for Claude, or "auto", which asks Ollama whether
// the model is a decision model.
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
	case "messages":
		return &claude{o}, nil
	}
	return nil, fmt.Errorf("unknown api %q", api)
}

// tev asks a decision model on Ollama's /v1/systemone endpoint: a state
// and one typed choice question in, a choice with probabilities out.
type tev struct{ ollama }

type systemoneRequest struct {
	Model     string                       `json:"model"`
	State     any                          `json:"state"`
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
	Usage struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
}

func (t *tev) decide(ctx context.Context, p prompt) (decision, error) {
	request := systemoneRequest{
		Model: t.model,
		State: p.state,
		Questions: map[string]systemoneQuestion{
			"move": {Type: "choice", Instructions: p.instructions, Criteria: criteria},
		},
	}
	if len(p.variants) > 0 {
		// A JSON object, so that the questions can point into it.
		request.State = json.RawMessage(p.state)
		request.Questions = map[string]systemoneQuestion{}
		for _, v := range p.variants {
			request.Questions[v.id] = systemoneQuestion{Type: "choice", Instructions: variantInstructions(v), Criteria: criteria}
		}
	}
	data, latency, retries, err := t.ask(ctx, "/v1/systemone", request)
	if err != nil {
		return decision{}, err
	}
	var reply systemoneReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return decision{}, fmt.Errorf("%s: %w", t.model, err)
	}
	if len(p.variants) > 0 {
		d := decision{hasConfidence: true, latency: latency, retries: retries, tokens: reply.Usage.InputTokens}
		for _, v := range p.variants {
			a, ok := reply.Answers[v.id]
			if _, valid := game.ParseAction(a.Choice); !ok || !valid {
				return decision{}, fmt.Errorf("%s: no usable answer for %s", t.model, v.id)
			}
			d.choices = append(d.choices, choice{a.Choice, a.Confidence, a.Probabilities[a.Choice]})
		}
		return d, nil
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
		retries:       retries,
		tokens:        reply.Usage.InputTokens,
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

// chatContent is the one user message a general-purpose model gets: the
// same three parts a decision model takes as separate fields.
func chatContent(p prompt) string {
	return fmt.Sprintf("State: %s\n\n%s\n- left: %s\n- none: %s\n- right: %s",
		p.state, p.instructions, criteria["left"], criteria["none"], criteria["right"])
}

func (g *gemma) decide(ctx context.Context, p prompt) (decision, error) {
	data, latency, err := g.post(ctx, "/api/chat", chatRequest{
		Model:    g.model,
		Messages: []chatMessage{{Role: "user", Content: chatContent(p)}},
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

// claude asks a general-purpose model on Anthropic's /v1/messages. It is
// pinned down the way gemma is: temperature 0 and a JSON schema that
// leaves room for nothing but the move. Haiku does not think unless
// asked to, so there is nothing to turn off.
type claude struct{ ollama }

type messagesRequest struct {
	Model        string        `json:"model"`
	MaxTokens    int           `json:"max_tokens"`
	Temperature  float64       `json:"temperature"`
	Messages     []chatMessage `json:"messages"`
	OutputConfig outputConfig  `json:"output_config"`
}

type outputConfig struct {
	Format outputFormat `json:"format"`
}

type outputFormat struct {
	Type   string `json:"type"`
	Schema any    `json:"schema"`
}

type messagesReply struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
}

// claudeSchema is moveSchema closed to further properties, which
// Anthropic's structured output asks for.
var claudeSchema = map[string]any{
	"type":                 moveSchema["type"],
	"properties":           moveSchema["properties"],
	"required":             moveSchema["required"],
	"additionalProperties": false,
}

// claudeMaxTokens leaves room for the move and nothing else; the reply
// is about 10 tokens.
const claudeMaxTokens = 64

func (c *claude) decide(ctx context.Context, p prompt) (decision, error) {
	data, latency, retries, err := c.ask(ctx, "/v1/messages", messagesRequest{
		Model:        c.model,
		MaxTokens:    claudeMaxTokens,
		Messages:     []chatMessage{{Role: "user", Content: chatContent(p)}},
		OutputConfig: outputConfig{outputFormat{Type: "json_schema", Schema: claudeSchema}},
	})
	if err != nil {
		return decision{}, err
	}
	var reply messagesReply
	if err := json.Unmarshal(data, &reply); err != nil {
		return decision{}, fmt.Errorf("%s: %w", c.model, err)
	}
	d := decision{latency: latency, retries: retries, tokens: reply.Usage.InputTokens}
	// A refusal or a reply cut short need not match the schema.
	var text string
	for _, block := range reply.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	var answer struct {
		Move string `json:"move"`
	}
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		return d, fmt.Errorf("%w: %s: %q", errUnusable, reply.StopReason, text)
	}
	if _, ok := game.ParseAction(answer.Move); !ok {
		return d, fmt.Errorf("%w: %s: %q", errUnusable, reply.StopReason, text)
	}
	d.action = answer.Move
	return d, nil
}
