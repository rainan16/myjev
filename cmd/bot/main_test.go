package main

import (
	"math/rand/v2"
	"testing"

	"myjev/internal/game"
)

// drive plays one run the way the real bot does, except that each move
// lands after delay(tick) extra ticks instead of travelling over a socket.
func drive(seed uint64, ticks int, delay func(tick int) int) (survived int, crashed bool) {
	g := game.New(game.DefaultConfig(), seed)
	var p player
	due := map[int]game.Action{} // tick on which a sent move is applied
	for tick := range ticks {
		s := g.Snapshot()
		if move := p.next(s, 1); move != "" {
			a, _ := game.ParseAction(move)
			at := tick + delay(tick)
			for due[at] != game.None { // the server applies one move per tick
				at++
			}
			due[at] = a
		}
		g.Step(due[tick])
		delete(due, tick)
		if g.GameOver() {
			return tick, true
		}
	}
	return ticks, false
}

func TestBotSurvivesWithAndWithoutLag(t *testing.T) {
	cfg := game.DefaultConfig()
	ticks := (int(cfg.RampSeconds) + 60) * cfg.TickRate
	rng := rand.New(rand.NewPCG(1, 2))
	cases := map[string]func(int) int{
		"move lands on the next tick":  func(int) int { return 0 },
		"move lands one tick late":     func(int) int { return 1 },
		"move lands late now and then": func(int) int { return rng.IntN(2) },
	}
	for name, delay := range cases {
		for seed := range uint64(60) {
			if at, crashed := drive(seed, ticks, delay); crashed {
				t.Errorf("%s: seed %d crashed at tick %d", name, seed, at)
				break
			}
		}
	}
}

func TestPlayerDoesNotRepeatAMoveInFlight(t *testing.T) {
	// One barrier straight ahead in the car's lane, close enough to act on.
	s := game.State{
		Tick: 10, TickRate: 30, Lanes: 4, Lane: 1, CarLength: 4, Speed: 40,
		Obstacles: []game.ObstacleState{{ID: 1, Lane: 1, Ahead: 40, Length: 1, Type: game.KindObstacle}},
	}
	var p player
	first := p.next(s, 1)
	if first == "" {
		t.Fatal("expected a move away from the barrier")
	}
	s.Tick++
	if again := p.next(s, 1); again != "" {
		t.Fatalf("sent %q again while the first move was still on its way", again)
	}
	// If the move never shows up, the player tries again after a few ticks.
	s.Tick += patience
	if retry := p.next(s, 1); retry == "" {
		t.Fatal("expected a retry after the move was lost")
	}
	// A new run clears the wait.
	if fresh := p.next(s, 2); fresh == "" {
		t.Fatal("expected a move at the start of a new run")
	}
}
