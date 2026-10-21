package main

import (
	"fmt"
	"os/exec"
	"path"
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
	gated                      // confidence below -min-confidence
	laneChanged                // the car left the lane the answer was about, or is leaving it
	worsened                   // the lane the move leads to got worse while the model answered
)

func (o outcome) String() string {
	return [...]string{"", "", "not sent: low confidence", "not sent: the car has changed lane or is about to", "not sent: that lane got worse meanwhile"}[o]
}

// answer is a decision together with the state it was made on.
type answer struct {
	decision
	err   error
	run   int
	tick  int
	lane  int
	state string
	seen  game.State // the observation the question was about
	// decided is 1 when seen was moved ahead to when the answer arrives,
	// so the question counted its first move as decided; see digested.
	decided int
	// variants is what a fan-out asked; see choose.
	variants []variant
}

// choose settles a fan-out answer once its age is known: the answer to
// the question whose premise, the road lead ticks ahead, is nearest to
// that age. On a tie the nearer premise counts. The answer is then judged
// against the state of that premise like any other.
func (a *answer) choose(age int) {
	if len(a.variants) == 0 {
		return
	}
	best := 0
	for i, v := range a.variants {
		if abs(v.lead-age) < abs(a.variants[best].lead-age) {
			best = i
		}
	}
	v, c := a.variants[best], a.choices[best]
	a.action, a.hasConfidence, a.confidence, a.probability = c.action, true, c.confidence, c.probability
	a.seen, a.decided, a.state = v.seen, v.decided, v.sentence
}

func abs(n int) int { return max(n, -n) }

// judge decides what to do with an answer given where the game is now.
// A move is checked against the newest state before it is sent: the
// model judged an older one. How old the answer is does not decide by
// itself. A late move whose lane has not got worse still goes out.
// moving says that a move sent earlier has yet to show up: with several
// questions open, a second answer must not move the car a second lane.
func judge(a answer, now game.State, reaction int, minConfidence float64, moving bool) outcome {
	switch {
	case a.action == "none":
		return stayed
	case a.hasConfidence && a.confidence < minConfidence:
		return gated
	case now.Lane != a.lane, moving:
		return laneChanged
	case gotWorse(a.seen, now, a.action, reaction, a.decided):
		return worsened
	}
	return sent
}

// ages remembers how old the last few answers were, in ticks, when they
// arrived. It is how the bot knows how long its model takes.
type ages []int

const agesKept = 9

func (a *ages) add(age int) {
	*a = append(*a, age)
	if len(*a) > agesKept {
		*a = (*a)[1:]
	}
}

// spacing is the least number of ticks between two questions. With one
// question at a time the next goes out as soon as the answer is in. With
// several they are spread over the time one answer takes, so the answers
// come in evenly and not in a bunch.
func spacing(typical, questions int) int {
	if questions <= 1 {
		return 1
	}
	return max(1, (typical+questions-1)/questions)
}

// typical is the age to expect of the next answer: the median.
func (a ages) typical() int {
	if len(a) == 0 {
		return 0
	}
	return slices.Sorted(slices.Values(a))[len(a)/2]
}

// slowest is the oldest of the last few answers: what to allow for when
// a further move has to be decided in time.
func (a ages) slowest() int {
	if len(a) == 0 {
		return 0
	}
	return slices.Max(a)
}

// stats collects what the summary of one run reports.
type stats struct {
	latencies []time.Duration
	outcomes  [worsened + 1]int
	late      int // answers older than -max-stale
	lateSent  int // late moves that still went out
	unusable  int // replies that held no move, or that did not come in time
	retries   int // calls repeated because the service was busy
	tokens    int // input tokens, where the service counts them

	rows    int // rows the car has come level with
	lastMet int // highest obstacle id the car has come level with
}

func (st *stats) decided(d decision, o outcome, late bool) {
	st.latencies = append(st.latencies, d.latency)
	st.outcomes[o]++
	if late {
		st.late++
		if o == sent {
			st.lateSent++
		}
	}
	st.retries += d.retries
	st.tokens += d.tokens
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

// summary is printed at the end of a run.
func (st *stats) summary(s state, crashed bool, elapsed time.Duration, mem runnerMemory) string {
	end := "stopped"
	if crashed {
		end = "crashed"
	}
	n := len(st.latencies)
	rate, lateShare := 0.0, 0.0
	if elapsed > 0 {
		rate = float64(n) / elapsed.Seconds()
	}
	if n > 0 {
		lateShare = 100 * float64(st.late) / float64(n)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "run %d %s at pace %v: %d ticks, %d rows passed, score %d (speed %.0f)\n",
		s.RunID, end, s.Pace, s.Tick, st.rowsPassed(crashed), s.Score, s.Speed)
	fmt.Fprintf(&b, "  decisions  %d, %.1f per second\n", n, rate)
	fmt.Fprintf(&b, "  latency    median %d ms, p95 %d ms\n", st.percentile(50).Milliseconds(), st.percentile(95).Milliseconds())
	fmt.Fprintf(&b, "  answers    %d moves sent, %d stay, %d below confidence, %d after a lane change, %d held as the lane got worse, %d unusable\n",
		st.outcomes[sent], st.outcomes[stayed], st.outcomes[gated], st.outcomes[laneChanged], st.outcomes[worsened], st.unusable)
	fmt.Fprintf(&b, "  late       %d answers (%.0f%%), %d of them moves that were still sent\n", st.late, lateShare, st.lateSent)
	if st.retries > 0 || st.tokens > 0 {
		fmt.Fprintf(&b, "  service    %d input tokens, %d retries after a busy reply\n", st.tokens, st.retries)
	}
	switch {
	case mem.hosted:
		b.WriteString("  memory     does not apply, the model is hosted")
	case mem.processes == 0:
		b.WriteString("  memory     could not read Ollama's model runner")
	case mem.processes == 1:
		fmt.Fprintf(&b, "  memory     %d MB in Ollama's model runner", mem.mb)
	default:
		// Another model is still loaded, so the figure is not this model's.
		fmt.Fprintf(&b, "  memory     %d MB in %d model runners; unload the other models for a figure for this one", mem.mb, mem.processes)
	}
	return b.String()
}

// runnerMemory is the memory of Ollama's model runner
// processes. Each loaded model has its own runner.
type runnerMemory struct {
	mb        int
	processes int
	hosted    bool // the model runs elsewhere; there is nothing to read
}

// readRunnerMemory reads the memory of Ollama's model runners. /api/ps is
// not used: it reports far less than the process holds.
func readRunnerMemory() runnerMemory {
	out, err := exec.Command("ps", "-axo", "pid=,rss=,command=").Output()
	if err != nil {
		return runnerMemory{}
	}
	return parseRunners(string(out), physFootprint)
}

// parseRunners adds up the runner processes in the output of
// `ps -axo pid=,rss=,command=`. Each runner counts with the larger of its
// resident size and its footprint (bytes, 0 if unknown): the llama.cpp
// runner maps the weights from the file, which the resident size counts
// and the footprint does not, and the MLX runner holds them in GPU
// memory, which the footprint counts and the resident size does not.
func parseRunners(ps string, footprint func(pid int) int) runnerMemory {
	var mem runnerMemory
	kb := 0
	for line := range strings.Lines(ps) {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if !isRunner(fields[2:]) {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		rss, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		kb += max(rss, footprint(pid)/1024)
		mem.processes++
	}
	mem.mb = kb / 1024
	return mem
}

// isRunner reports whether a command line starts a model runner: the
// llama.cpp server, or `ollama runner`. Only the program and its first
// argument count, so a shell whose arguments mention a runner is not one.
func isRunner(args []string) bool {
	switch path.Base(args[0]) {
	case "llama-server":
		return true
	case "ollama":
		return len(args) > 1 && args[1] == "runner"
	}
	return false
}

// physFootprint is the footprint of a process in bytes as macOS's
// footprint tool reports it, or 0 where the tool is missing or fails.
func physFootprint(pid int) int {
	out, err := exec.Command("footprint", "-p", strconv.Itoa(pid), "--noCategories", "-f", "bytes").Output()
	if err != nil {
		return 0
	}
	return parsePhysFootprint(string(out))
}

// parsePhysFootprint reads the `phys_footprint: N B` line of footprint's
// output.
func parsePhysFootprint(out string) int {
	for line := range strings.Lines(out) {
		value, ok := strings.CutPrefix(strings.TrimSpace(line), "phys_footprint:")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(value), " B"))
		if err == nil {
			return n
		}
	}
	return 0
}
