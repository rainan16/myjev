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
	RunID      int    `json:"run_id"`
	Controller string `json:"controller"`
}

type message struct {
	Type  string `json:"type"`
	Error string `json:"error"`
	State state  `json:"state"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "address of the game server")
	ollama := flag.String("ollama", "http://127.0.0.1:11434", "base URL of Ollama")
	model := flag.String("model", "tev1:0.8b", "model that drives")
	name := flag.String("name", "", "name to drive under (default: the model name)")
	runs := flag.Int("runs", 0, "stop after this many runs (0 = keep playing)")
	seed := flag.Int64("seed", -1, "seed for every run (-1 = random)")
	pause := flag.Duration("pause", 2*time.Second, "wait after a crash before restarting")
	flag.Parse()
	if *name == "" {
		*name = *model
	}
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	b := &bot{
		name:  *name,
		runs:  *runs,
		seed:  *seed,
		pause: *pause,
		model: &tev{url: *ollama, model: *model, client: &http.Client{Timeout: time.Minute}},
	}
	if err := b.play(ctx, "ws://"+*addr+"/ws"); err != nil {
		log.Fatalf("llmbot: %v", err)
	}
}

type bot struct {
	name  string
	runs  int
	seed  int64
	pause time.Duration
	model *tev

	conn    *websocket.Conn
	driving bool // the server has shown us as the controller
	played  int
}

var errTakenOver = errors.New("someone else took the car")

func (b *bot) play(ctx context.Context, url string) error {
	// Loading the model takes seconds. Do it before taking the car, so it
	// does not count as a slow decision.
	d, err := b.model.decide(ctx, "Lane to the left: clear. Current lane: clear. Lane to the right: clear.")
	if err != nil {
		return fmt.Errorf("warm-up: %w", err)
	}
	log.Printf("llmbot: %s is loaded (warm-up took %d ms)", b.model.model, d.latency.Milliseconds())

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

// answer is a decision together with the state it was made on.
type answer struct {
	decision
	err    error
	run    int
	tick   int
	lane   int
	digest string
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
		decisions int
		crashed   bool
		restart   <-chan time.Time

		asking    bool // a question is open
		askedTick = -1 // tick of the state the last question was about
		// A move is on its way and has not shown up yet. Asking again
		// before it lands would move two lanes.
		waiting            bool
		sentLane, sentTick int
	)
	answers := make(chan answer, 1)

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
			a := answer{run: s.RunID, tick: s.Tick, lane: s.Lane, digest: digest(s.State)}
			a.decision, a.err = b.model.decide(ctx, a.digest)
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
			if a.err != nil {
				return a.err
			}
			if a.run != run || latest.GameOver {
				continue // the run it was about is over
			}
			decisions++
			note := ""
			switch {
			case a.action == "none":
			case latest.Lane != a.lane:
				// An earlier move landed while the model was thinking;
				// the answer is about a lane the car has left.
				note = "  (not sent: the car has changed lane)"
			default:
				if err := b.send(ctx, map[string]any{"type": "input", "action": a.action}); err != nil {
					return err
				}
				waiting, sentLane, sentTick = true, latest.Lane, latest.Tick
			}
			log.Printf("tick %5d  lane %d  %-5s  conf %.2f  p %.2f  %4d ms  %d ticks old  | %s%s",
				a.tick, a.lane, a.action, a.confidence, a.probability, a.latency.Milliseconds(), latest.Tick-a.tick, a.digest, note)
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
				run, crashed, decisions, waiting = s.RunID, false, 0, false
				log.Printf("llmbot: run %d, seed %d", s.RunID, s.Seed)
			}
			latest = s

			if s.GameOver {
				if crashed {
					continue
				}
				crashed = true
				b.played++
				log.Printf("llmbot: run %d crashed at tick %d, score %d (speed %.0f), after %d decisions",
					s.RunID, s.Tick, s.Score, s.Speed, decisions)
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
