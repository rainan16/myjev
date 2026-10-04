package main

import (
	"encoding/json"
	"fmt"
	"math"

	"myjev/internal/game"
)

// prompt is what a model is asked on one tick. The criteria, the three
// moves, are the same for every prompt.
type prompt struct {
	state        string
	instructions string
}

var criteria = map[string]string{
	"left":  "move one lane to the left",
	"none":  "stay in the current lane",
	"right": "move one lane to the right",
}

// The instructions are the same on every call; only the state changes.
// Ollama answers a prompt it has seen before much faster than a new one,
// so everything that varies is kept in the state.
const (
	digestedInstructions = "Which move keeps the car on a clear lane? Stay if the current lane is clear. " +
		"Otherwise move to a clear lane, or to a lane that is blocked later, or towards a clear lane beyond. " +
		"Never move into a wall or a lane that is blocked now."
	rawInstructions = "Which move avoids hitting an obstacle?"
)

const (
	// digestHorizon is how many ticks ahead a lane must stay free to be
	// called clear.
	digestHorizon = 60
	// laterTicks separates blocked soon from blocked later.
	laterTicks = 20
	// nowTicks is how close a blockage must be for the lane to count as
	// blocked now: moving into it is a crash.
	nowTicks = 2
	// rawObstacles caps the obstacle list of the raw state, nearest first.
	rawObstacles = 8
)

// Words used for a lane in the digested state.
const (
	wordClear  = "clear"
	wordLater  = "blocked later"
	wordSoon   = "blocked soon"
	wordNow    = "blocked now"
	wordWall   = "wall"
	wordBeyond = "blocked soon, clear lane beyond"
)

// digested turns an observation into one sentence about the lane each
// move leads to. All the arithmetic on distance and speed happens here,
// so the model is left with a judgment and no calculation.
func digested(s game.State) prompt {
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
		case free[lane] > laterTicks:
			return wordLater
		case step != 0 && leadsToClear(free, lane, step):
			return wordBeyond
		}
		return wordSoon
	}
	return prompt{sentence(word(-1), word(0), word(1)), digestedInstructions}
}

func sentence(left, current, right string) string {
	return fmt.Sprintf("Lane to the left: %s. Current lane: %s. Lane to the right: %s.", left, current, right)
}

// allSentences lists every state digested can produce. Asking each once
// before driving means no sentence is new to the model during a run.
func allSentences() []string {
	side := []string{wordWall, wordClear, wordLater, wordSoon, wordNow, wordBeyond}
	var all []string
	for _, left := range side {
		for _, current := range []string{wordClear, wordLater, wordSoon, wordNow} {
			for _, right := range side {
				if left == wordWall && right == wordWall {
					continue
				}
				all = append(all, sentence(left, current, right))
			}
		}
	}
	return all
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

type rawState struct {
	Lane      int           `json:"lane"`
	Speed     float64       `json:"speed"`
	Obstacles []rawObstacle `json:"obstacles"`
}

type rawObstacle struct {
	Lane  int     `json:"lane"`
	Ahead float64 `json:"ahead"`
	Speed float64 `json:"speed"`
}

// raw hands the model the observation much as the API gives it: the
// car's lane and speed and the nearest obstacles. Working out which lane
// is blocked when is left to the model.
func raw(s game.State) prompt {
	r := rawState{Lane: s.Lane, Speed: s.Speed, Obstacles: []rawObstacle{}}
	for _, o := range s.Obstacles {
		if len(r.Obstacles) == rawObstacles {
			break
		}
		r.Obstacles = append(r.Obstacles, rawObstacle{o.Lane, o.Ahead, o.Speed})
	}
	data, _ := json.Marshal(r) // cannot fail: plain numbers
	return prompt{string(data), rawInstructions}
}
