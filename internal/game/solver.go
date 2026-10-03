package game

import "math"

// solveHorizon is how many ticks ahead Solve plans.
const solveHorizon = 60

// Solve picks an action from an observation alone. It predicts where
// every obstacle will be over the next ticks, heads early for the lane
// that stays clear longest, and never takes a step that shortens how
// long it can survive. It is the reference player used to prove that the
// spawner is fair, and what the sample bot plays with.
func Solve(s State) Action {
	// For each lane, the tick ranges (counted from now) during which
	// something is level with the car.
	type span struct{ from, to int }
	busy := make([][]span, s.Lanes)
	for _, o := range s.Obstacles {
		if o.Lane < 0 || o.Lane >= s.Lanes {
			continue
		}
		const margin = 0.5
		closing := (s.Speed - o.Speed) / float64(s.TickRate) // per tick
		if closing <= 0 {
			if o.Hittable(s.CarLength) {
				busy[o.Lane] = append(busy[o.Lane], span{0, solveHorizon})
			}
			continue
		}
		from := int(math.Floor((o.Ahead - margin) / closing))
		to := int(math.Ceil((o.Ahead + o.Length + s.CarLength + margin) / closing))
		if to < 0 || from > solveHorizon {
			continue
		}
		busy[o.Lane] = append(busy[o.Lane], span{from, to})
	}
	free := func(lane, tick int) bool {
		for _, b := range busy[lane] {
			if tick >= b.from && tick <= b.to {
				return false
			}
		}
		return true
	}

	// survive reports for how many ticks some path stays clear after
	// moving into the given lane on the next tick.
	survive := func(lane int) int {
		if lane < 0 || lane >= s.Lanes || !free(lane, 1) {
			return 0
		}
		reach := make([]bool, s.Lanes)
		next := make([]bool, s.Lanes)
		reach[lane] = true
		for tick := 2; tick <= solveHorizon; tick++ {
			any := false
			for l := range next {
				from := reach[l] || l > 0 && reach[l-1] || l < s.Lanes-1 && reach[l+1]
				next[l] = from && free(l, tick)
				any = any || next[l]
			}
			if !any {
				return tick - 1
			}
			reach, next = next, reach
		}
		return solveHorizon
	}

	// straight reports for how many ticks the lane stays clear with no
	// further steering.
	straight := func(lane int) int {
		for tick := 1; tick <= solveHorizon; tick++ {
			if !free(lane, tick) {
				return tick - 1
			}
		}
		return solveHorizon
	}

	// Head for the lane that needs no steering for longest (the nearest
	// one among equals), as long as the step towards it costs nothing.
	// This moves the car over early instead of at the last possible tick.
	stay := survive(s.Lane)
	target, targetClear := s.Lane, straight(s.Lane)
	for lane := range s.Lanes {
		c := straight(lane)
		if c > targetClear || c == targetClear && abs(lane-s.Lane) < abs(target-s.Lane) {
			target, targetClear = lane, c
		}
	}
	if target < s.Lane && survive(s.Lane-1) >= max(stay, 1) {
		return Left
	}
	if target > s.Lane && survive(s.Lane+1) >= max(stay, 1) {
		return Right
	}

	// Otherwise take whatever survives longest; staying wins ties.
	action, best := None, stay
	if d := survive(s.Lane - 1); d > best {
		action, best = Left, d
	}
	if d := survive(s.Lane + 1); d > best {
		action = Right
	}
	return action
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
