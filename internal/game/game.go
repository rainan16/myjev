// Package game is the headless core. It has no networking and no clock:
// time only advances through Step, so the same seed and the same actions
// always produce the same run.
package game

import (
	"fmt"
	"math/rand/v2"
)

type Action int

const (
	None Action = iota
	Left
	Right
)

// ParseAction maps the API strings to actions.
func ParseAction(s string) (Action, bool) {
	switch s {
	case "none":
		return None, true
	case "left":
		return Left, true
	case "right":
		return Right, true
	}
	return None, false
}

const (
	KindObstacle = "obstacle"
	KindCar      = "car"
)

// Grid cell values.
const (
	CellEmpty    = 0
	CellObstacle = 1
	CellCar      = 2
)

type Config struct {
	Lanes    int
	TickRate int // ticks per second

	// The car speeds up linearly from StartSpeed to MaxSpeed over
	// RampSeconds, then holds. Speeds are in world units per second.
	StartSpeed  float64
	MaxSpeed    float64
	RampSeconds float64

	CarLength      float64
	ObstacleLength float64 // static barrier
	TrafficLength  float64 // moving car
	TrafficSpeed   float64 // must be below StartSpeed so traffic is always overtaken

	// Obstacles come in rows that the car meets at the same moment.
	// After one row has been passed, the next is at least MinGapTicks
	// and a random RowSpacingMin..RowSpacingMax of road away.
	RowSpacingMin float64
	RowSpacingMax float64
	MinGapTicks   int
	FirstRow      float64 // clear road before the first row
	CarChance     float64 // chance that an obstacle is a moving car
	TightChance   float64 // chance that a row leaves only one lane open

	ViewDistance  float64 // how far ahead obstacles are reported
	GridRows      int
	GridRowLength float64
}

func DefaultConfig() Config {
	return Config{
		Lanes:          4,
		TickRate:       30,
		StartSpeed:     40,
		MaxSpeed:       90,
		RampSeconds:    90,
		CarLength:      4,
		ObstacleLength: 1, // a thin barrier: once it is past the car's rear it can no longer be hit
		TrafficLength:  4,
		TrafficSpeed:   20,
		RowSpacingMin:  60,
		RowSpacingMax:  110,
		MinGapTicks:    12,
		FirstRow:       150,
		CarChance:      0.5,
		TightChance:    0.15,
		ViewDistance:   300,
		GridRows:       12,
		GridRowLength:  20,
	}
}

func (c Config) validate() error {
	switch {
	case c.Lanes < 2:
		return fmt.Errorf("game: need at least 2 lanes, got %d", c.Lanes)
	case c.TrafficSpeed >= c.StartSpeed:
		return fmt.Errorf("game: traffic speed %v must be below start speed %v", c.TrafficSpeed, c.StartSpeed)
	case c.MaxSpeed < c.StartSpeed:
		return fmt.Errorf("game: max speed %v is below start speed %v", c.MaxSpeed, c.StartSpeed)
	case c.MinGapTicks < c.Lanes-1:
		return fmt.Errorf("game: a gap of %d ticks is too short to cross %d lanes", c.MinGapTicks, c.Lanes)
	}
	return nil
}

// An obstacle is defined by where and when the car meets it. The car's
// speed depends only on the tick, never on steering, so that moment is
// known when the obstacle is planned.
type obstacle struct {
	id      int
	lane    int
	kind    string
	length  float64
	speed   float64
	encTick int     // tick at which its rear edge is level with the car's front
	encZ    float64 // world position of its rear edge at that tick
}

type Game struct {
	cfg       Config
	seed      uint64
	rng       *rand.Rand
	tick      int
	lane      int
	distance  float64 // world position of the car's rear edge
	gameOver  bool
	obstacles []obstacle // every planned obstacle that has not been passed yet
	nextRow   int        // encounter tick of the next row to plan
	nextID    int
	// lastBarrier[lane] is the world position of the newest barrier in
	// that lane, or a negative number if there is none.
	lastBarrier []float64
}

// New starts a run. It panics on a config that cannot produce a fair game.
func New(cfg Config, seed uint64) *Game {
	if err := cfg.validate(); err != nil {
		panic(err)
	}
	g := &Game{
		cfg:         cfg,
		seed:        seed,
		rng:         rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
		lane:        cfg.Lanes / 2,
		nextID:      1,
		lastBarrier: make([]float64, cfg.Lanes),
	}
	for i := range g.lastBarrier {
		g.lastBarrier[i] = -1
	}
	for g.distAt(g.nextRow)+cfg.CarLength < cfg.FirstRow {
		g.nextRow++
	}
	g.plan()
	return g
}

func (g *Game) GameOver() bool { return g.gameOver }

// Score is the distance survived, rounded down.
func (g *Game) Score() int { return int(g.distance) }

func (g *Game) rampTicks() int { return int(g.cfg.RampSeconds * float64(g.cfg.TickRate)) }

// speedAt is the car's speed during the given tick.
func (g *Game) speedAt(tick int) float64 {
	r := g.rampTicks()
	if tick >= r {
		return g.cfg.MaxSpeed
	}
	return g.cfg.StartSpeed + (g.cfg.MaxSpeed-g.cfg.StartSpeed)*float64(tick)/float64(r)
}

// distAt is the car's rear position after the given number of ticks: the
// sum of speedAt(1..tick) / TickRate, in closed form.
func (g *Game) distAt(tick int) float64 {
	rate := float64(g.cfg.TickRate)
	r := g.rampTicks()
	n := float64(min(tick, r))
	d := g.cfg.StartSpeed * n
	if r > 0 {
		d += (g.cfg.MaxSpeed - g.cfg.StartSpeed) / float64(r) * n * (n + 1) / 2
	}
	if tick > r {
		d += g.cfg.MaxSpeed * float64(tick-r)
	}
	return d / rate
}

// zAt is the world position of an obstacle's rear edge at the given tick.
func (g *Game) zAt(o *obstacle, tick int) float64 {
	return o.encZ - o.speed*float64(o.encTick-tick)/float64(g.cfg.TickRate)
}

// Step advances the game by one tick. After game over it does nothing.
func (g *Game) Step(a Action) {
	if g.gameOver {
		return
	}
	g.tick++

	switch a {
	case Left:
		if g.lane > 0 {
			g.lane--
		}
	case Right:
		if g.lane < g.cfg.Lanes-1 {
			g.lane++
		}
	}

	prev := g.distance
	g.distance = g.distAt(g.tick)

	// The car occupies rear..front. If one tick ever covers more than the
	// car's length, also cover the gap back to where the front was, so
	// nothing is skipped. Never reach behind the car's current rear: that
	// would hit obstacles the car has already passed.
	front := g.distance + g.cfg.CarLength
	rear := min(g.distance, prev+g.cfg.CarLength)
	for i := range g.obstacles {
		o := &g.obstacles[i]
		if o.lane != g.lane {
			continue
		}
		if z := g.zAt(o, g.tick); z < front && z+o.length > rear {
			g.gameOver = true
			return
		}
	}

	g.prune()
	g.plan()
}

// plan keeps rows planned far enough ahead that everything within view
// distance already exists. Traffic closes in slowest, so it sets the horizon.
func (g *Game) plan() {
	slowest := g.cfg.StartSpeed - g.cfg.TrafficSpeed
	horizon := int(g.cfg.ViewDistance/slowest*float64(g.cfg.TickRate)) + 2
	for g.nextRow <= g.tick+horizon {
		g.planRow()
	}
}

// planRow adds the row met at tick g.nextRow and schedules the row after
// it. A row never blocks every lane, and the next row is only met once
// this one is fully behind the car plus a gap long enough to reach any
// lane, so there is always a way through.
func (g *Game) planRow() {
	t := g.nextRow
	blocked := 1 + g.rng.IntN(min(2, g.cfg.Lanes-1))
	if g.rng.Float64() < g.cfg.TightChance {
		blocked = g.cfg.Lanes - 1
	}

	clear := t
	for _, lane := range g.rng.Perm(g.cfg.Lanes)[:blocked] {
		o := obstacle{
			id:      g.nextID,
			lane:    lane,
			kind:    KindObstacle,
			length:  g.cfg.ObstacleLength,
			encTick: t,
			encZ:    g.distAt(t) + g.cfg.CarLength,
		}
		if g.rng.Float64() < g.cfg.CarChance {
			car := o
			car.kind, car.length, car.speed = KindCar, g.cfg.TrafficLength, g.cfg.TrafficSpeed
			if !g.wouldDriveThroughBarrier(&car) {
				o = car
			}
		}
		if o.kind == KindObstacle {
			g.lastBarrier[lane] = o.encZ
		}
		g.nextID++
		g.obstacles = append(g.obstacles, o)
		clear = max(clear, g.clearTick(&o))
	}

	spacing := g.cfg.RowSpacingMin + g.rng.Float64()*(g.cfg.RowSpacingMax-g.cfg.RowSpacingMin)
	next := clear + g.cfg.MinGapTicks
	for g.distAt(next) < g.distAt(clear)+spacing {
		next++
	}
	g.nextRow = next
}

// clearTick is the first tick at which the obstacle is fully behind the car.
func (g *Game) clearTick(o *obstacle) int {
	t := o.encTick
	for g.zAt(o, t)+o.length > g.distAt(t) {
		t++
	}
	return t
}

// wouldDriveThroughBarrier reports whether a moving car, on its way to
// the point where it is met, would be seen driving through a barrier in
// its lane. That is the case when the barrier lies beyond the car at the
// moment the car comes into view.
func (g *Game) wouldDriveThroughBarrier(car *obstacle) bool {
	barrier := g.lastBarrier[car.lane]
	if barrier < 0 {
		return false
	}
	t := car.encTick
	for t > 0 && g.zAt(car, t)-g.distAt(t)-g.cfg.CarLength < g.cfg.ViewDistance {
		t--
	}
	return barrier+g.cfg.ObstacleLength > g.zAt(car, t)
}

// prune drops obstacles that are well behind the car.
func (g *Game) prune() {
	kept := g.obstacles[:0]
	for i := range g.obstacles {
		o := &g.obstacles[i]
		if g.zAt(o, g.tick)+o.length > g.distance-10 {
			kept = append(kept, *o)
		}
	}
	g.obstacles = kept
}
