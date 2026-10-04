package main

import (
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"myjev/internal/game"
)

// outcome is what became of one answer.
type outcome int

const (
	sent        outcome = iota // a move went to the server
	stayed                     // the answer was to stay in lane
	stale                      // too old to act on
	gated                      // confidence below -min-confidence
	laneChanged                // the car left the lane the answer was about
)

func (o outcome) String() string {
	return [...]string{"", "", "dropped: stale", "not sent: low confidence", "not sent: the car has changed lane"}[o]
}

// answer is a decision together with the state it was made on.
type answer struct {
	decision
	err   error
	run   int
	tick  int
	lane  int
	state string
}

// judge decides what to do with an answer given where the game is now.
// maxStale below zero keeps every answer however old.
func judge(a answer, nowTick, nowLane, maxStale int, minConfidence float64) outcome {
	switch {
	case maxStale >= 0 && nowTick-a.tick > maxStale:
		return stale
	case a.action == "none":
		return stayed
	case a.hasConfidence && a.confidence < minConfidence:
		return gated
	case nowLane != a.lane:
		return laneChanged
	}
	return sent
}

// stats collects what the summary of one run reports.
type stats struct {
	latencies []time.Duration
	outcomes  [laneChanged + 1]int
	unusable  int // replies that held no move

	rows    int // rows the car has come level with
	lastMet int // highest obstacle id the car has come level with
}

func (st *stats) decided(d decision, o outcome) {
	st.latencies = append(st.latencies, d.latency)
	st.outcomes[o]++
}

// observe counts rows. The obstacles of a row are met on the same tick
// and ids only grow, so a row is new when an obstacle the car is level
// with has a higher id than any seen level before.
func (st *stats) observe(s game.State) {
	met := st.lastMet
	for _, o := range s.Obstacles {
		if o.Ahead <= 0 {
			met = max(met, o.ID)
		}
	}
	if met > st.lastMet {
		st.rows++
		st.lastMet = met
	}
}

// rowsPassed leaves out the row the car crashed in.
func (st *stats) rowsPassed(crashed bool) int {
	if crashed && st.rows > 0 {
		return st.rows - 1
	}
	return st.rows
}

// percentile of the latencies, p from 0 to 100.
func (st *stats) percentile(p int) time.Duration {
	if len(st.latencies) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(st.latencies))
	return sorted[min(len(sorted)*p/100, len(sorted)-1)]
}

// summary is printed at the end of a run. rssMB below zero means the
// runner's memory could not be read.
func (st *stats) summary(s state, crashed bool, elapsed time.Duration, rssMB int) string {
	end := "stopped"
	if crashed {
		end = "crashed"
	}
	n := len(st.latencies)
	rate, staleShare := 0.0, 0.0
	if elapsed > 0 {
		rate = float64(n) / elapsed.Seconds()
	}
	if n > 0 {
		staleShare = 100 * float64(st.outcomes[stale]) / float64(n)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "run %d %s at pace %v: %d ticks, %d rows passed, score %d (speed %.0f)\n",
		s.RunID, end, s.Pace, s.Tick, st.rowsPassed(crashed), s.Score, s.Speed)
	fmt.Fprintf(&b, "  decisions  %d, %.1f per second\n", n, rate)
	fmt.Fprintf(&b, "  latency    median %d ms, p95 %d ms\n", st.percentile(50).Milliseconds(), st.percentile(95).Milliseconds())
	fmt.Fprintf(&b, "  answers    %d moves sent, %d stay, %d stale (%.0f%%), %d below confidence, %d after a lane change, %d unusable\n",
		st.outcomes[sent], st.outcomes[stayed], st.outcomes[stale], staleShare, st.outcomes[gated], st.outcomes[laneChanged], st.unusable)
	if rssMB >= 0 {
		fmt.Fprintf(&b, "  memory     %d MB in Ollama's model runner", rssMB)
	} else {
		b.WriteString("  memory     could not read Ollama's model runner")
	}
	return b.String()
}

// runnerRSS reads the memory of Ollama's model runner processes in MB,
// or -1 if there is none. /api/ps is not used: it reports far less than
// the process holds.
func runnerRSS() int {
	out, err := exec.Command("ps", "-axo", "rss=,command=").Output()
	if err != nil {
		return -1
	}
	return parseRunnerRSS(string(out))
}

// parseRunnerRSS sums the resident memory of the runner processes in the
// output of `ps -axo rss=,command=`. With models run one at a time there
// is one.
func parseRunnerRSS(ps string) int {
	kb, found := 0, false
	for line := range strings.Lines(ps) {
		rss, command, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || !(strings.Contains(command, "llama-server") || strings.Contains(command, "ollama runner")) {
			continue
		}
		if n, err := strconv.Atoi(rss); err == nil {
			kb += n
			found = true
		}
	}
	if !found {
		return -1
	}
	return kb / 1024
}
