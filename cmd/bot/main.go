// Command bot is a sample player. It talks to the game server only
// through the public WebSocket API (see docs/API.md): it claims the car,
// steers from the state stream, and restarts after a crash.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
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
	name := flag.String("name", "gobot", "name to drive under")
	runs := flag.Int("runs", 0, "stop after this many runs (0 = keep playing)")
	seed := flag.Int64("seed", -1, "seed for every run (-1 = random)")
	pause := flag.Duration("pause", 2*time.Second, "wait after a crash before restarting")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	b := &bot{name: *name, runs: *runs, seed: *seed, pause: *pause}
	if err := b.play(ctx, "ws://"+*addr+"/ws"); err != nil {
		log.Fatalf("bot: %v", err)
	}
}

type bot struct {
	name  string
	runs  int
	seed  int64
	pause time.Duration

	conn    *websocket.Conn
	player  player
	driving bool // the server has shown us as the controller
	played  int
}

var errTakenOver = errors.New("someone else took the car")

func (b *bot) play(ctx context.Context, url string) error {
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
		log.Printf("bot: %v, stopping", err)
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

func (b *bot) loop(ctx context.Context) error {
	// The server greets every connection with the state of the run that
	// was already there. That run is not ours; wait for the one our
	// reset starts.
	inherited := -1
	lastRun, crashed, lastReport := 0, false, time.Now()
	for {
		_, data, err := b.conn.Read(ctx)
		if err != nil {
			return err
		}
		var m message
		if err := json.Unmarshal(data, &m); err != nil {
			return err
		}
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

		if s.RunID != lastRun {
			lastRun, crashed, lastReport = s.RunID, false, time.Now()
			log.Printf("bot: run %d, seed %d", s.RunID, s.Seed)
		}

		if s.GameOver {
			if crashed {
				continue
			}
			crashed = true
			b.played++
			log.Printf("bot: run %d crashed at score %d (speed %.0f)", s.RunID, s.Score, s.Speed)
			if b.runs > 0 && b.played >= b.runs {
				return b.send(ctx, map[string]any{"type": "release"})
			}
			// Leave the crash on screen for a moment, then start over.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(b.pause):
			}
			if err := b.reset(ctx); err != nil {
				return err
			}
			continue
		}

		if time.Since(lastReport) >= 30*time.Second {
			lastReport = time.Now()
			log.Printf("bot: run %d still going, score %d (speed %.0f)", s.RunID, s.Score, s.Speed)
		}

		if action := b.player.next(s.State, s.RunID); action != "" {
			if err := b.send(ctx, map[string]any{"type": "input", "action": action}); err != nil {
				return err
			}
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

// player turns states into moves. The game runs in real time, so a move
// may take effect on the next tick or, if the message is slow, the one
// after. The player allows for both.
type player struct {
	run      int
	sentTick int // tick of the state the last move was based on
	sentLane int
	waiting  bool // a move is on its way and has not shown up yet
}

// patience is how many ticks to wait for a sent move to show up before
// deciding it was lost.
const patience = 4

// next returns "left", "right" or "" for no move.
func (p *player) next(s game.State, run int) string {
	if run != p.run {
		p.run, p.waiting = run, false
	}
	if p.waiting {
		// Sending again before the first move lands would move two lanes.
		if s.Lane == p.sentLane && s.Tick < p.sentTick+patience {
			return ""
		}
		p.waiting = false
	}

	action := decide(s)
	if action == game.None {
		return ""
	}
	p.sentTick, p.sentLane, p.waiting = s.Tick, s.Lane, true
	if action == game.Left {
		return "left"
	}
	return "right"
}

// decide only moves when the move is right both if it lands on the next
// tick and if it lands one tick late.
func decide(s game.State) game.Action {
	action := game.Solve(s)
	if action != game.None && game.Solve(oneTickLater(s)) != action {
		return game.None
	}
	return action
}

// oneTickLater predicts the observation one tick ahead with no move made.
func oneTickLater(s game.State) game.State {
	later := s
	later.Obstacles = make([]game.ObstacleState, len(s.Obstacles))
	for i, o := range s.Obstacles {
		o.Ahead -= (s.Speed - o.Speed) / float64(s.TickRate)
		later.Obstacles[i] = o
	}
	return later
}
