package game

import (
	"math"
	"sort"
)

// State is the observation handed to viewers and bots. See docs/API.md.
type State struct {
	Tick      int             `json:"tick"`
	Seed      uint64          `json:"seed"`
	TickRate  int             `json:"tick_rate"`
	GameOver  bool            `json:"game_over"`
	Lanes     int             `json:"lanes"`
	Lane      int             `json:"lane"`
	CarLength float64         `json:"car_length"`
	Speed     float64         `json:"speed"`
	Distance  float64         `json:"distance"`
	Score     int             `json:"score"`
	Obstacles []ObstacleState `json:"obstacles"`
	Grid      Grid            `json:"grid"`
}

type ObstacleState struct {
	ID     int     `json:"id"`
	Lane   int     `json:"lane"`
	Ahead  float64 `json:"ahead"` // from the car's front edge to the obstacle's rear edge
	Length float64 `json:"length"`
	Speed  float64 `json:"speed"`
	Type   string  `json:"type"`
}

// Hittable reports whether the obstacle currently overlaps the car's
// position along the road, so steering into its lane would be a crash.
func (o ObstacleState) Hittable(carLength float64) bool {
	return o.Ahead < 0 && o.Ahead+o.Length > -carLength
}

type Grid struct {
	RowLength float64 `json:"row_length"`
	Rows      [][]int `json:"rows"` // Rows[0] is directly ahead; one cell per lane
}

func (g *Game) Snapshot() State {
	front := g.distance + g.cfg.CarLength

	s := State{
		Tick:      g.tick,
		Seed:      g.seed,
		TickRate:  g.cfg.TickRate,
		GameOver:  g.gameOver,
		Lanes:     g.cfg.Lanes,
		Lane:      g.lane,
		CarLength: g.cfg.CarLength,
		Speed:     round1(g.speedAt(g.tick)),
		Distance:  round1(g.distance),
		Score:     g.Score(),
		Obstacles: make([]ObstacleState, 0, len(g.obstacles)),
		Grid: Grid{
			RowLength: g.cfg.GridRowLength,
			Rows:      make([][]int, g.cfg.GridRows),
		},
	}
	for i := range s.Grid.Rows {
		s.Grid.Rows[i] = make([]int, g.cfg.Lanes)
	}

	for i := range g.obstacles {
		o := &g.obstacles[i]
		ahead := g.zAt(o, g.tick) - front
		if ahead > g.cfg.ViewDistance || ahead+o.length <= -g.cfg.CarLength {
			continue // not in view yet, or already behind the car
		}
		s.Obstacles = append(s.Obstacles, ObstacleState{
			ID:     o.id,
			Lane:   o.lane,
			Ahead:  round1(ahead),
			Length: o.length,
			Speed:  o.speed,
			Type:   o.kind,
		})

		cell := CellObstacle
		if o.kind == KindCar {
			cell = CellCar
		}
		// Mark every grid row the obstacle overlaps. Anything alongside
		// the car counts as row 0.
		first := max(int(math.Floor(ahead/g.cfg.GridRowLength)), 0)
		last := max(int(math.Floor((ahead+o.length)/g.cfg.GridRowLength)), 0)
		for r := first; r <= last && r < g.cfg.GridRows; r++ {
			s.Grid.Rows[r][o.lane] = cell
		}
	}
	sort.SliceStable(s.Obstacles, func(i, j int) bool { return s.Obstacles[i].Ahead < s.Obstacles[j].Ahead })
	return s
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
