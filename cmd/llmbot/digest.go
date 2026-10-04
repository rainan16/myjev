package main

import (
	"fmt"
	"math"

	"myjev/internal/game"
)

// The question put to the model. It is the same on every call; only the
// state changes. Ollama answers a prompt it has seen before much faster
// than a new one, so everything that varies is kept in the state.
const instructions = "Which move keeps the car on a clear lane? Stay if the current lane is clear. " +
	"Otherwise move to a clear lane, or towards a clear lane beyond."

var criteria = map[string]string{
	"left":  "move one lane to the left",
	"none":  "stay in the current lane",
	"right": "move one lane to the right",
}

const (
	// digestHorizon is how many ticks ahead a lane must stay free to be
	// called clear.
	digestHorizon = 60
	// nowTicks is how close a blockage must be for the lane to count as
	// blocked now: moving into it is a crash.
	nowTicks = 2
)

// Words used for a lane in the digested state.
const (
	wordClear  = "clear"
	wordSoon   = "blocked soon"
	wordNow    = "blocked now"
	wordWall   = "wall"
	wordBeyond = "blocked soon, clear lane beyond"
)

// digest turns an observation into one sentence about the lane each move
// leads to. All the arithmetic on distance and speed happens here, so the
// model is left with a judgment and no calculation.
func digest(s game.State) string {
	free := clearTicks(s)
	word := func(step int) string {
		lane := s.Lane + step
		if lane < 0 || lane >= s.Lanes {
			return wordWall
		}
		switch {
		case free[lane] >= digestHorizon:
			return wordClear
		case free[lane] <= nowTicks:
			return wordNow
		case step != 0 && leadsToClear(free, lane, step):
			return wordBeyond
		}
		return wordSoon
	}
	return fmt.Sprintf("Lane to the left: %s. Current lane: %s. Lane to the right: %s.", word(-1), word(0), word(1))
}

// clearTicks reports, per lane, for how many ticks from now nothing is
// level with the car, capped at digestHorizon. The spans match game.Solve.
func clearTicks(s game.State) []int {
	free := make([]int, s.Lanes)
	for i := range free {
		free[i] = digestHorizon
	}
	for _, o := range s.Obstacles {
		if o.Lane < 0 || o.Lane >= s.Lanes {
			continue
		}
		const margin = 0.5
		closing := (s.Speed - o.Speed) / float64(s.TickRate) // per tick
		if closing <= 0 {
			if o.Hittable(s.CarLength) {
				free[o.Lane] = 0
			}
			continue
		}
		from := int(math.Floor((o.Ahead - margin) / closing))
		to := int(math.Ceil((o.Ahead + o.Length + s.CarLength + margin) / closing))
		if to < 1 || from > digestHorizon {
			continue
		}
		free[o.Lane] = min(free[o.Lane], max(from-1, 0))
	}
	return free
}

// leadsToClear reports whether a clear lane lies further in the direction
// of step, and every lane on the way stays free long enough to cross.
func leadsToClear(free []int, lane, step int) bool {
	for l, moves := lane+step, 2; l >= 0 && l < len(free); l, moves = l+step, moves+1 {
		if free[l] < digestHorizon {
			continue
		}
		passable := true
		for k := lane; k != l; k += step {
			passable = passable && free[k] > moves+1
		}
		if passable {
			return true
		}
	}
	return false
}
