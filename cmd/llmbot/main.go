// Command llmbot lets a model drive the car: a local one on Ollama, or
// hosted Jev on TypeSafe's API. Like the
// sample bot it talks to the game server only through the public
// WebSocket API (see docs/API.md). The game does not wait for the model:
// one question is open at a time, always about the newest state, or
// several spread over the time one answer takes (-questions).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"myjev/internal/game"
)

// state is the part of the state object the bot uses.
type state struct {
	game.State
	RunID      int     `json:"run_id"`
	Controller string  `json:"controller"`
	Pace       float64 `json:"pace"`
}

type message struct {
	Type  string `json:"type"`
	Error string `json:"error"`
	State state  `json:"state"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "address of the game server")
	url := flag.String("ollama", "http://127.0.0.1:11434", "base URL of Ollama")
	backend := flag.String("backend", "ollama", "where the model runs: ollama, typesafe for hosted Jev (needs TYPESAFE_API_KEY; sends the state to api.typesafe.ai), or anthropic for Claude (needs ANTHROPIC_API_KEY; sends the state to api.anthropic.com)")
	model := flag.String("model", "", "model that drives (default: tev1:0.8b on ollama, "+jevModel+" on typesafe, "+claudeModel+" on anthropic)")
	api := flag.String("api", "auto", "how the model is called on ollama: systemone, chat, or auto to ask Ollama")
	form := flag.String("state", "digested", "state given to the model: digested or raw")
	maxStale := flag.Int("max-stale", 6, "count answers older than this many ticks as late (-1 = none); a late move is still sent if its lane has not got worse")
	lookAhead := flag.Int("look-ahead", -1, "ticks ahead of now the digested state describes (-1 = as many as the model's answers are old, 0 = off)")
	leadList := flag.String("leads", "", "ask about the road at several moments ahead in one call, for example 5,8,11 ticks, and use the answer nearest the real age (digested state on a decision model only; replaces -look-ahead)")
	questions := flag.Int("questions", 1, "questions open at a time; above 1 they are spread over the time one answer takes, and each is one more call to the model")
	minConfidence := flag.Float64("min-confidence", 0, "send no move the model is less sure of than this (0 = off)")
	warm := flag.Bool("warm", false, "ask every digested state once before driving; only helps a model that stays fast on any prompt it has seen")
	verbose := flag.Bool("v", false, "log every decision, not only the changes")
	name := flag.String("name", "", "name to drive under (default: the model name)")
	runs := flag.Int("runs", 0, "stop after this many runs (0 = keep playing)")
	seed := flag.Int64("seed", -1, "seed for every run (-1 = random)")
	pause := flag.Duration("pause", 2*time.Second, "wait after a crash before restarting")
	flag.Parse()
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	var conn ollama
	switch *backend {
	case "ollama":
		if *model == "" {
			*model = "tev1:0.8b"
		}
		conn = ollama{url: *url, model: *model, client: &http.Client{Timeout: time.Minute}}
	case "typesafe":
		if *model == "" {
			*model = jevModel
		}
		conn, *api = hosted(*model, apiKey(*backend, typesafeKey)), "systemone"
	case "anthropic":
		if *model == "" {
			*model = claudeModel
		}
		conn, *api = hostedClaude(*model, apiKey(*backend, anthropicKey), fromEnv(anthropicWorkspace)), "messages"
	default:
		log.Fatalf("llmbot: unknown backend %q", *backend)
	}
	if *name == "" {
		*name = *model
	}

	b := &bot{
		name:          *name,
		runs:          *runs,
		seed:          *seed,
		pause:         *pause,
		maxStale:      *maxStale,
		lookAhead:     *lookAhead,
		questions:     *questions,
		minConfidence: *minConfidence,
		warm:          *warm,
		verbose:       *verbose,
		form:          *form,
		hosted:        *backend != "ollama",
	}
	leads, err := parseLeads(*leadList)
	if err != nil {
		log.Fatalf("llmbot: %v", err)
	}
	if len(leads) > 0 && *form != "digested" {
		log.Fatalf("llmbot: -leads needs the digested state")
	}
	switch *form {
	case "digested":
		b.build, b.predicts = digested, true
		if len(leads) > 0 {
			b.leads, b.predicts = leads, false
			b.build = func(s game.State, reaction, _ int) prompt { return fanOut(s, leads, reaction) }
		}
	case "raw":
		b.build, b.warm = raw, false
	default:
		log.Fatalf("llmbot: unknown state form %q", *form)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	b.model, err = newDecider(ctx, conn, *api)
	if err != nil {
		log.Fatalf("llmbot: %v", err)
	}
	if _, ok := b.model.(*tev); len(leads) > 0 && !ok {
		log.Fatalf("llmbot: -leads needs a decision model, and %s is not one", *model)
	}
	log.Printf("llmbot: %s, %s state", *model, *form)
	if err := b.play(ctx, "ws://"+*addr+"/ws"); err != nil {
		log.Fatalf("llmbot: %v", err)
	}
}

// parseLeads reads -leads: whole ticks, each once, nearest first.
func parseLeads(list string) ([]int, error) {
	var leads []int
	for _, field := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 || slices.Contains(leads, n) {
			return nil, fmt.Errorf("-leads: %q is not a new, whole number of ticks", field)
		}
		leads = append(leads, n)
	}
	slices.Sort(leads)
	return leads, nil
}

// The variables that hold the key for hosted Jev and for Claude.
const (
	typesafeKey  = "TYPESAFE_API_KEY"
	anthropicKey = "ANTHROPIC_API_KEY"
	// anthropicWorkspace is optional: only a key that is not tied to a
	// workspace needs it.
	anthropicWorkspace = "ANTHROPIC_WORKSPACE_ID"
)

// fromEnv reads a variable from the environment or, failing that, from
// a .env file in the working directory.
func fromEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		env, _ := os.ReadFile(".env") // a missing file is the same as no value
		value = keyFromEnvFile(string(env), name)
	}
	return value
}

// apiKey reads the key a hosted backend needs and stops the bot when
// there is none.
func apiKey(backend, keyName string) string {
	key := fromEnv(keyName)
	if key == "" {
		log.Fatalf("llmbot: -backend %s needs an API key in %s, set in the environment or in a .env file here", backend, keyName)
	}
	return key
}

// keyFromEnvFile finds the key in the text of a .env file: a line
// NAME=value, with or without quotes around the value.
func keyFromEnvFile(env, keyName string) string {
	for line := range strings.Lines(env) {
		name, value, ok := strings.Cut(strings.TrimPrefix(strings.TrimSpace(line), "export "), "=")
		if ok && strings.TrimSpace(name) == keyName {
			return strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return ""
}

type bot struct {
	name          string
	runs          int
	seed          int64
	pause         time.Duration
	maxStale      int
	lookAhead     int   // ticks the state is moved ahead; below zero follows the model
	questions     int   // questions open at a time; below 1 counts as 1
	leads         []int // premises of a fan-out, ticks ahead; none = ask one question
	predicts      bool  // the state form can be moved ahead (digested only)
	minConfidence float64
	warm          bool
	verbose       bool
	form          string // "digested" or "raw", for the status text
	hosted        bool   // the model runs elsewhere, so there is no runner to measure
	build         func(s game.State, reaction, decided int) prompt
	model         decider

	conn    *websocket.Conn
	driving bool // the server has shown us as the controller
	played  int
	// ages is how old the last few answers were when they arrived.
	ages ages
}

var errTakenOver = errors.New("someone else took the car")

func (b *bot) play(ctx context.Context, url string) error {
	if err := b.warmUp(ctx); err != nil {
		return fmt.Errorf("warm-up: %w", err)
	}

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	b.conn = conn

	if err := b.send(ctx, map[string]any{"type": "claim", "name": b.name}); err != nil {
		return err
	}

	err = b.loop(ctx)
	switch {
	case errors.Is(err, errTakenOver):
		log.Printf("llmbot: %v, stopping", err)
		return nil
	case ctx.Err() != nil:
		// Interrupted: hand the car back before leaving.
		quit, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		b.send(quit, map[string]any{"type": "release"})
		conn.Close(websocket.StatusNormalClosure, "")
		return nil
	}
	return err
}

// warmUp loads the model before the car is taken, so that loading does
// not count as a slow decision. A model answers a prompt it has seen
// before much faster, so every digested state is asked once as well.
func (b *bot) warmUp(ctx context.Context) error {
	start := time.Now()
	prompts := []prompt{b.build(game.State{}, 0, 0)}
	if b.warm {
		prompts = prompts[:0]
		for _, s := range allSentences() {
			prompts = append(prompts, prompt{state: s, instructions: digestedInstructions})
		}
	}
	for _, p := range prompts {
		if _, err := b.model.decide(ctx, p); err != nil && !errors.Is(err, errUnusable) {
			return err
		}
	}
	log.Printf("llmbot: model is loaded, %d warm-up questions took %.1f s", len(prompts), time.Since(start).Seconds())
	return nil
}

// memory is what the summary reports as the model's memory.
func (b *bot) memory() runnerMemory {
	if b.hosted {
		return runnerMemory{hosted: true}
	}
	return runnerRSS()
}

// lead is how many ticks ahead of now the model is asked about.
func (b *bot) lead() int {
	switch {
	case !b.predicts:
		return 0
	case b.lookAhead >= 0:
		return b.lookAhead
	}
	return b.ages.typical()
}

// statusEvery is the least number of ticks between two status texts, so
// the viewer's HUD is kept current without flooding the socket.
const statusEvery = 15

// statusText is what the viewer shows under the driver's name.
func statusText(form string, d decision) string {
	if !d.hasConfidence {
		return fmt.Sprintf("%s  %d ms  no confidence", form, d.latency.Milliseconds())
	}
	return fmt.Sprintf("%s  %d ms  conf %.2f", form, d.latency.Milliseconds(), d.confidence)
}

// patience is how many ticks to wait for a sent move to show up before
// deciding it was lost.
const patience = 4

func (b *bot) loop(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	msgs := make(chan message)
	readErr := make(chan error, 1)
	go func() {
		for {
			_, data, err := b.conn.Read(ctx)
			if err != nil {
				readErr <- err
				return
			}
			var m message
			if err := json.Unmarshal(data, &m); err != nil {
				readErr <- err
				return
			}
			select {
			case msgs <- m:
			case <-ctx.Done():
				return
			}
		}
	}()

	var (
		// The run that is going when the car becomes ours is not ours.
		// We reset once the server shows us driving and play from the
		// run after that one. Going by the first state received instead
		// is not safe: a slow connection may never see the state the
		// server greets it with.
		inherited = -1
		latest    state
		run       int
		st        *stats
		started   time.Time
		crashed   bool
		restart   <-chan time.Time
		lastLine  string // what the last log line said, without the numbers

		lastStatus string
		statusTick int // tick at which the last status was sent

		open      int  // questions that have no answer yet
		askedTick = -1 // tick of the state the last question was about
		// A move is on its way and has not shown up yet. Asking again
		// before it lands would move two lanes, and so would a second
		// move from a question that was open when the first was sent.
		waiting            bool
		sentLane, sentTick int
	)
	questions := max(b.questions, 1)
	answers := make(chan answer, questions)
	moving := func() bool {
		return waiting && latest.Lane == sentLane && latest.Tick < sentTick+patience
	}

	// An interrupted run still gets its summary.
	defer func() {
		if st != nil && !crashed {
			log.Printf("llmbot: %s", st.summary(latest, false, time.Since(started), b.memory()))
		}
	}()

	ask := func() {
		s := latest
		if open >= questions || s.RunID != run || s.GameOver || s.Tick < askedTick+spacing(b.ages.typical(), questions) {
			return
		}
		if waiting {
			if moving() {
				return
			}
			waiting = false
		}
		open, askedTick = open+1, s.Tick
		// The model is asked about the road as it will be when its
		// answer arrives, and the answer is judged against that state.
		lead := b.lead()
		seen, decided := ahead(s.State, lead), min(lead, 1)
		p := b.build(seen, b.ages.slowest(), decided)
		go func() {
			a := answer{run: s.RunID, tick: s.Tick, lane: s.Lane, state: p.state, seen: seen, decided: decided, variants: p.variants}
			a.decision, a.err = b.model.decide(ctx, p)
			answers <- a
		}()
	}

	for {
		select {
		case err := <-readErr:
			return err

		case <-restart:
			restart = nil
			if err := b.reset(ctx); err != nil {
				return err
			}

		case a := <-answers:
			open--
			if errors.Is(a.err, errUnusable) || errors.Is(a.err, errSlow) {
				if a.run == run && !latest.GameOver {
					st.unusable++
					log.Printf("tick %5d  lane %d  %v", a.tick, a.lane, a.err)
				}
				ask()
				continue
			}
			if a.err != nil {
				return a.err
			}
			if a.run != run || latest.GameOver {
				continue // the run it was about is over
			}
			age := latest.Tick - a.tick
			a.choose(age)
			// The question allowed for the slowest recent answer. The
			// check before sending goes by the usual one: holding a move
			// for fear of one slow answer leaves the car where it is.
			o := judge(a, latest.State, b.ages.typical(), b.minConfidence, moving())
			b.ages.add(age)
			st.decided(a.decision, o, b.maxStale >= 0 && age > b.maxStale)
			if o == sent {
				if err := b.send(ctx, map[string]any{"type": "input", "action": a.action}); err != nil {
					return err
				}
				waiting, sentLane, sentTick = true, latest.Lane, latest.Tick
			}
			if line := fmt.Sprintf("lane %d  %-5s  %s  %v", a.lane, a.action, b.shown(a), o); b.verbose || line != lastLine {
				lastLine = line
				log.Print(decisionLine(a, age, b.shown(a), o))
			}
			if text := statusText(b.form, a.decision); text != lastStatus && latest.Tick >= statusTick+statusEvery {
				if err := b.send(ctx, map[string]any{"type": "status", "text": text}); err != nil {
					return err
				}
				lastStatus, statusTick = text, latest.Tick
			}
			ask()

		case m := <-msgs:
			if m.Type == "error" {
				if !b.driving {
					return fmt.Errorf("could not take the car: %s", m.Error)
				}
				return fmt.Errorf("%w (%s)", errTakenOver, m.Error)
			}
			if m.Type != "state" {
				continue
			}
			s := m.State
			if s.Controller != b.name {
				if b.driving {
					return errTakenOver
				}
				continue // our claim has not been processed yet
			}
			b.driving = true
			if inherited < 0 {
				inherited = s.RunID
				if err := b.reset(ctx); err != nil {
					return err
				}
			}
			if s.RunID <= inherited {
				continue
			}

			if s.RunID != run {
				if st != nil && !crashed {
					// Someone reset the game in the middle of our run.
					log.Printf("llmbot: %s", st.summary(latest, false, time.Since(started), b.memory()))
				}
				run, crashed, waiting, lastLine = s.RunID, false, false, ""
				askedTick = -1            // ticks start over with the run
				statusTick = -statusEvery // ticks start over with the run
				st, started = &stats{}, time.Now()
				log.Printf("llmbot: run %d, seed %d", s.RunID, s.Seed)
			}
			latest = s
			st.observe(s.State)

			if s.GameOver {
				if crashed {
					continue
				}
				crashed = true
				b.played++
				log.Printf("llmbot: %s", st.summary(s, true, time.Since(started), b.memory()))
				if b.runs > 0 && b.played >= b.runs {
					return b.send(ctx, map[string]any{"type": "release"})
				}
				// Leave the crash on screen for a moment, then start over.
				restart = time.After(b.pause)
				continue
			}
			ask()
		}
	}
}

// shown is the state as it appears in the log. The raw state changes on
// every tick and is long, so it is left out.
func (b *bot) shown(a answer) string {
	if len(a.state) > 120 {
		return "(raw state)"
	}
	return a.state
}

func decisionLine(a answer, age int, shown string, o outcome) string {
	confidence := "conf  -    p  -  "
	if a.hasConfidence {
		confidence = fmt.Sprintf("conf %.2f  p %.2f", a.confidence, a.probability)
	}
	line := fmt.Sprintf("tick %5d  lane %d  %-5s  %s  %4d ms  %2d ticks old  | %s",
		a.tick, a.lane, a.action, confidence, a.latency.Milliseconds(), age, shown)
	if note := o.String(); note != "" {
		line += "  (" + note + ")"
	}
	return line
}

func (b *bot) reset(ctx context.Context) error {
	msg := map[string]any{"type": "reset"}
	if b.seed >= 0 {
		msg["seed"] = b.seed
	}
	return b.send(ctx, msg)
}

func (b *bot) send(ctx context.Context, msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return b.conn.Write(ctx, websocket.MessageText, data)
}
