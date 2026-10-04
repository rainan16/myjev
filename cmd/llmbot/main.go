// Command llmbot lets a local model on Ollama drive the car. Like the
// sample bot it talks to the game server only through the public
// WebSocket API (see docs/API.md). The game does not wait for the model:
// one question is open at a time, always about the newest state.
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
	model := flag.String("model", "tev1:0.8b", "model that drives")
	api := flag.String("api", "auto", "how the model is called: systemone, chat, or auto to ask Ollama")
	form := flag.String("state", "digested", "state given to the model: digested or raw")
	maxStale := flag.Int("max-stale", 6, "drop answers older than this many ticks (-1 = keep all)")
	minConfidence := flag.Float64("min-confidence", 0, "send no move the model is less sure of than this (0 = off)")
	warm := flag.Bool("warm", false, "ask every digested state once before driving; only helps a model that stays fast on any prompt it has seen")
	verbose := flag.Bool("v", false, "log every decision, not only the changes")
	name := flag.String("name", "", "name to drive under (default: the model name)")
	runs := flag.Int("runs", 0, "stop after this many runs (0 = keep playing)")
	seed := flag.Int64("seed", -1, "seed for every run (-1 = random)")
	pause := flag.Duration("pause", 2*time.Second, "wait after a crash before restarting")
	flag.Parse()
	if *name == "" {
		*name = *model
	}
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	b := &bot{
		name:          *name,
		runs:          *runs,
		seed:          *seed,
		pause:         *pause,
		maxStale:      *maxStale,
		minConfidence: *minConfidence,
		warm:          *warm,
		verbose:       *verbose,
	}
	switch *form {
	case "digested":
		b.build = digested
	case "raw":
		b.build, b.warm = raw, false
	default:
		log.Fatalf("llmbot: unknown state form %q", *form)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	b.model, err = newDecider(ctx, ollama{*url, *model, &http.Client{Timeout: time.Minute}}, *api)
	if err != nil {
		log.Fatalf("llmbot: %v", err)
	}
	log.Printf("llmbot: %s, %s state", *model, *form)
	if err := b.play(ctx, "ws://"+*addr+"/ws"); err != nil {
		log.Fatalf("llmbot: %v", err)
	}
}

type bot struct {
	name          string
	runs          int
	seed          int64
	pause         time.Duration
	maxStale      int
	minConfidence float64
	warm          bool
	verbose       bool
	build         func(game.State) prompt
	model         decider

	conn    *websocket.Conn
	driving bool // the server has shown us as the controller
	played  int
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
	if err := b.reset(ctx); err != nil {
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
	prompts := []prompt{b.build(game.State{})}
	if b.warm {
		prompts = prompts[:0]
		for _, s := range allSentences() {
			prompts = append(prompts, prompt{s, digestedInstructions})
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
		// The server greets every connection with the state of the run
		// that was already there. That run is not ours; wait for the one
		// our reset starts.
		inherited = -1
		latest    state
		run       int
		st        *stats
		started   time.Time
		crashed   bool
		restart   <-chan time.Time
		lastLine  string // what the last log line said, without the numbers

		asking    bool // a question is open
		askedTick = -1 // tick of the state the last question was about
		// A move is on its way and has not shown up yet. Asking again
		// before it lands would move two lanes.
		waiting            bool
		sentLane, sentTick int
	)
	answers := make(chan answer, 1)

	// An interrupted run still gets its summary.
	defer func() {
		if st != nil && !crashed {
			log.Printf("llmbot: %s", st.summary(latest, false, time.Since(started), runnerRSS()))
		}
	}()

	ask := func() {
		s := latest
		if asking || s.RunID != run || s.GameOver || s.Tick == askedTick {
			return
		}
		if waiting {
			if s.Lane == sentLane && s.Tick < sentTick+patience {
				return
			}
			waiting = false
		}
		asking, askedTick = true, s.Tick
		go func() {
			p := b.build(s.State)
			a := answer{run: s.RunID, tick: s.Tick, lane: s.Lane, state: p.state}
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
			asking = false
			if errors.Is(a.err, errUnusable) {
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
			o := judge(a, latest.Tick, latest.Lane, b.maxStale, b.minConfidence)
			st.decided(a.decision, o)
			if o == sent {
				if err := b.send(ctx, map[string]any{"type": "input", "action": a.action}); err != nil {
					return err
				}
				waiting, sentLane, sentTick = true, latest.Lane, latest.Tick
			}
			if line := fmt.Sprintf("lane %d  %-5s  %s  %v", a.lane, a.action, b.shown(a), o); b.verbose || line != lastLine {
				lastLine = line
				log.Print(decisionLine(a, latest.Tick-a.tick, b.shown(a), o))
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
			if inherited < 0 {
				inherited = s.RunID
			}
			if s.Controller != b.name {
				if b.driving {
					return errTakenOver
				}
				continue // our claim has not been processed yet
			}
			b.driving = true
			if s.RunID <= inherited {
				continue
			}

			if s.RunID != run {
				if st != nil && !crashed {
					// Someone reset the game in the middle of our run.
					log.Printf("llmbot: %s", st.summary(latest, false, time.Since(started), runnerRSS()))
				}
				run, crashed, waiting, lastLine = s.RunID, false, false, ""
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
				log.Printf("llmbot: %s", st.summary(s, true, time.Since(started), runnerRSS()))
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
