package game

import (
	"math"
	"reflect"
	"testing"
)

func TestLaneBounds(t *testing.T) {
	cfg := DefaultConfig()
	g := New(cfg, 1)
	for range cfg.Lanes + 2 {
		g.Step(Left)
	}
	if g.lane != 0 {
		t.Fatalf("lane = %d after steering left past the edge, want 0", g.lane)
	}
	for range cfg.Lanes + 2 {
		g.Step(Right)
	}
	if g.lane != cfg.Lanes-1 {
		t.Fatalf("lane = %d after steering right past the edge, want %d", g.lane, cfg.Lanes-1)
	}
}

func TestSameSeedSameRun(t *testing.T) {
	a, b := New(DefaultConfig(), 7), New(DefaultConfig(), 7)
	for range 3000 {
		act := Solve(a.Snapshot())
		a.Step(act)
		b.Step(act)
	}
	if a.GameOver() {
		t.Fatal("solver crashed")
	}
	if !reflect.DeepEqual(a.Snapshot(), b.Snapshot()) {
		t.Fatal("same seed and actions produced different states")
	}
	c := New(DefaultConfig(), 8)
	for range 3000 {
		c.Step(Solve(c.Snapshot()))
	}
	if reflect.DeepEqual(a.Snapshot().Obstacles, c.Snapshot().Obstacles) {
		t.Fatal("different seeds produced the same obstacles")
	}
}

func TestSpeedRamp(t *testing.T) {
	cfg := DefaultConfig()
	g := New(cfg, 1)
	ramp := g.rampTicks()
	if got := g.speedAt(0); got != cfg.StartSpeed {
		t.Fatalf("speed at start = %v, want %v", got, cfg.StartSpeed)
	}
	if got := g.speedAt(ramp); got != cfg.MaxSpeed {
		t.Fatalf("speed at end of ramp = %v, want %v", got, cfg.MaxSpeed)
	}
	if got := g.speedAt(ramp * 3); got != cfg.MaxSpeed {
		t.Fatalf("speed long after the ramp = %v, want %v", got, cfg.MaxSpeed)
	}
	// distAt must equal the tick-by-tick sum, through and past the ramp.
	sum := 0.0
	for tick := 1; tick <= ramp+500; tick++ {
		if g.speedAt(tick) < g.speedAt(tick-1) {
			t.Fatalf("speed dropped at tick %d", tick)
		}
		sum += g.speedAt(tick) / float64(cfg.TickRate)
		if got := g.distAt(tick); math.Abs(got-sum) > 1e-6 {
			t.Fatalf("distAt(%d) = %v, want %v", tick, got, sum)
		}
	}
}

func TestCollisionEndsRun(t *testing.T) {
	cfg := DefaultConfig()
	g := New(cfg, 3)
	target := g.obstacles[0]
	for g.lane != target.lane {
		steerInto(g, target.lane)
	}
	for range 20 * cfg.TickRate {
		g.Step(None)
	}
	if !g.GameOver() {
		t.Fatal("expected game over after driving into an obstacle")
	}
	tick := g.tick
	g.Step(Left)
	if g.tick != tick {
		t.Fatal("game kept advancing after game over")
	}
	if g.tick < target.encTick || g.tick > target.encTick+2 {
		t.Fatalf("crashed at tick %d, obstacle is met at tick %d", g.tick, target.encTick)
	}
}

func TestRowsLeaveAGapAndTimeToReachIt(t *testing.T) {
	cfg := DefaultConfig()
	for seed := range uint64(50) {
		g := New(cfg, seed)
		for range 2000 {
			g.planRow()
		}
		perRow := map[int]int{}
		clearTick := map[int]int{}
		var order []int
		for i := range g.obstacles {
			o := &g.obstacles[i]
			if perRow[o.encTick] == 0 {
				order = append(order, o.encTick)
			}
			perRow[o.encTick]++
			clearTick[o.encTick] = max(clearTick[o.encTick], g.clearTick(o))
		}
		for i, tick := range order {
			if perRow[tick] >= cfg.Lanes {
				t.Fatalf("seed %d: row at tick %d blocks all %d lanes", seed, tick, cfg.Lanes)
			}
			if i > 0 {
				if gap := tick - clearTick[order[i-1]]; gap < cfg.MinGapTicks {
					t.Fatalf("seed %d: only %d ticks between rows at ticks %d and %d", seed, gap, order[i-1], tick)
				}
			}
		}
	}
}

// The spawner's promise: a player who only sees the public observation
// can always get through, from the slow start to well past top speed.
// Along the way, no moving car may drive through a barrier.
func TestSolverSurvivesEverySeed(t *testing.T) {
	cfg := DefaultConfig()
	ticks := (int(cfg.RampSeconds) + 120) * cfg.TickRate
	for seed := range uint64(200) {
		g := New(cfg, seed)
		for range ticks {
			s := g.Snapshot()
			g.Step(Solve(s))
			if g.GameOver() {
				t.Fatalf("seed %d: solver crashed at tick %d (speed %v)", seed, g.tick, s.Speed)
			}
			for i, a := range s.Obstacles {
				for _, b := range s.Obstacles[i+1:] {
					if a.Lane == b.Lane && a.Ahead < b.Ahead+b.Length && b.Ahead < a.Ahead+a.Length {
						t.Fatalf("seed %d tick %d: obstacles overlap: %+v and %+v", seed, g.tick, a, b)
					}
				}
			}
		}
	}
}

func TestBothObstacleTypesAppear(t *testing.T) {
	g := New(DefaultConfig(), 11)
	seen := map[string]bool{}
	for range 3000 {
		s := g.Snapshot()
		for _, o := range s.Obstacles {
			seen[o.Type] = true
			if o.Type == KindCar && o.Speed <= 0 {
				t.Fatalf("car without speed: %+v", o)
			}
			if o.Type == KindObstacle && o.Speed != 0 {
				t.Fatalf("barrier with speed: %+v", o)
			}
		}
		g.Step(Solve(s))
	}
	if !seen[KindCar] || !seen[KindObstacle] {
		t.Fatalf("expected both types, saw %v", seen)
	}
}

func TestGridMatchesObstacles(t *testing.T) {
	g := New(DefaultConfig(), 5)
	want := map[string]int{KindObstacle: CellObstacle, KindCar: CellCar}
	for range 2000 {
		s := g.Snapshot()
		for _, o := range s.Obstacles {
			row := max(int(math.Floor(o.Ahead/s.Grid.RowLength)), 0)
			if row >= len(s.Grid.Rows) {
				continue
			}
			if s.Grid.Rows[row][o.Lane] == CellEmpty {
				t.Fatalf("tick %d: obstacle %+v missing from grid row %d", g.tick, o, row)
			}
			if o.Hittable(s.CarLength) && s.Grid.Rows[0][o.Lane] != want[o.Type] {
				t.Fatalf("tick %d: obstacle alongside the car not in grid row 0: %+v", g.tick, o)
			}
		}
		g.Step(Solve(s))
	}
}

// besideFirstObstacle puts the car in a free lane right next to the first
// obstacle. It reports false when both neighbouring lanes are blocked too.
func besideFirstObstacle(g *Game) (target obstacle, ok bool) {
	target = g.obstacles[0]
	blocked := map[int]bool{}
	for _, o := range g.obstacles {
		if o.encTick == target.encTick {
			blocked[o.lane] = true
		}
	}
	side := -1
	for _, l := range []int{target.lane - 1, target.lane + 1} {
		if l >= 0 && l < g.cfg.Lanes && !blocked[l] {
			side = l
		}
	}
	if side < 0 {
		return target, false
	}
	for g.lane != side {
		steerInto(g, side)
	}
	return target, true
}

func steerInto(g *Game, lane int) {
	if g.lane < lane {
		g.Step(Right)
	} else {
		g.Step(Left)
	}
}

func TestPassedObstacleCannotBeHit(t *testing.T) {
	cfg := DefaultConfig()
	kinds := map[string]bool{}
	for seed := range uint64(40) {
		g := New(cfg, seed)
		target, ok := besideFirstObstacle(g)
		if !ok {
			continue
		}
		kinds[target.kind] = true
		// Drive alongside, then steer in on the very first tick on which
		// the obstacle is behind the car's rear edge.
		for !g.GameOver() && g.tick+1 < g.clearTick(&target) {
			g.Step(None)
		}
		steerInto(g, target.lane)
		if g.GameOver() {
			t.Fatalf("seed %d: crashed into a %s already behind the car", seed, target.kind)
		}
		if g.lane != target.lane {
			t.Fatalf("seed %d: lane = %d, want %d", seed, g.lane, target.lane)
		}
	}
	if !kinds[KindCar] || !kinds[KindObstacle] {
		t.Fatalf("expected to test both types, tested %v", kinds)
	}
}

func TestObstacleAlongsideIsHit(t *testing.T) {
	cfg := DefaultConfig()
	kinds := map[string]bool{}
	for seed := range uint64(40) {
		g := New(cfg, seed)
		target, ok := besideFirstObstacle(g)
		if !ok {
			continue
		}
		kinds[target.kind] = true
		// Steer in on the last tick before the obstacle clears the car.
		for !g.GameOver() && g.tick+2 < g.clearTick(&target) {
			g.Step(None)
		}
		steerInto(g, target.lane)
		if !g.GameOver() {
			t.Fatalf("seed %d: expected a crash when steering into a %s alongside the car", seed, target.kind)
		}
	}
	if !kinds[KindCar] || !kinds[KindObstacle] {
		t.Fatalf("expected to test both types, tested %v", kinds)
	}
}

func TestBadConfigIsRejected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.TrafficSpeed = cfg.StartSpeed
	defer func() {
		if recover() == nil {
			t.Fatal("expected New to reject traffic as fast as the car")
		}
	}()
	New(cfg, 1)
}
