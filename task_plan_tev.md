# Task Plan: Tev versus Gemma driving the OutRun game

<!--
  Working memory on disk for this task. Re-read it at the start of every session and before major decisions.
  Source of the decisions: ./intent_decisions_answers_tev.md (question-me output, 2026-10-04). It also holds
  the probe results, the working Tev call and the reasoning behind each decision.
  The game itself has its own plan in ./task_plan.md. Its rules (and AGENTS.md) still apply here.
-->

## Goal
A new bot, `cmd/llmbot`, lets the local Ollama models `tev1:0.8b`, `tev1:4b` and `gemma4:e2b` drive the OutRun game through its public API, so the user can watch and read off how a decision model and a traditional LLM differ in decision speed, RAM footprint and the harness each one needs.

This is a hands-on quick look, not a benchmark.

## Current Phase
Phase 2

## Phases

### Phase 1: Orient and finish the probe
No bot code yet. The aim is to know that the digested state makes a model drive before building around it.
- [x] Read `AGENTS.md`, `task_plan.md` and `intent_decisions_answers_tev.md`; confirm Ollama is up and the three models are listed
- [x] Capture three or four real game states from a running server (`curl -s localhost:8765/state`), including one where only one move is safe
- [x] Write the digested form by hand for each, in two variants: ticks-until-blocked per lane as numbers, and coarse words ("blocked soon", "clear")
- [x] Send raw and both digested variants to all three models; note answer, confidence and latency
- [x] Choose the digested shape and the wording of `instructions` and `criteria` (Key Questions 1 and 2)
- [x] Record what was seen under Notes, and any dead end under Errors Encountered
- **Status:** complete

### Phase 2: Tracer bullet, one model driving end to end
The thinnest slice through every layer: game API, prompt builder, Ollama call, input sent, a line in the terminal.
- [ ] `cmd/llmbot/main.go`: connect to `/ws`, claim under the model name, release on Ctrl-C, stop if someone else takes the car (same manners as `cmd/bot`)
- [ ] Digested prompt builder only, as chosen in Phase 1; pure function with a unit test
- [ ] Tev caller only: `POST /v1/systemone`, one `choice` question, plain `net/http`
- [ ] One call at a time on the newest state; send the chosen action; one warm-up call before driving
- [ ] Terminal line per decision: tick, action, confidence, latency
- [ ] Run `tev1:0.8b` on one seed at normal speed with the viewer open
- [ ] **STOP: show the run to the user and get sign-off before Phase 3.** Confirm the log format and that the digested state is the right shape
- **Status:** pending

### Phase 3: The comparison harness
Everything the bot needs so the three models can be put side by side.
- [ ] Gemma caller: `/api/chat`, `think: false`, `temperature: 0`, `format` schema with `move` as an enum; parse the JSON, define what happens on an unparseable reply
- [ ] Common shape for both callers: decide(state) gives action, confidence (absent for Gemma), latency
- [ ] Raw prompt builder and the `-state raw|digested` flag; cap the obstacle count (Key Question 4)
- [ ] Stale answers: drop when older than `-max-stale` ticks, count them
- [ ] `-min-confidence` gate, off by default
- [ ] End-of-run summary: ticks survived, rows passed, score, decisions per second, median and p95 latency, share dropped as stale, runner process RSS
- [ ] Tests without real time or a live Ollama: prompt builders, reply parsing, stale and gate logic against an `httptest` stand-in
- **Status:** pending

### Phase 4: Clock pace in the server
A small server change plus one new state field. The game core is not touched.
- [ ] `-pace` flag on `cmd/outrun`, default 1.0; reject values outside the allowed range at startup (Key Question 5)
- [ ] Ticker interval in `internal/server` becomes `1 / (tick_rate × pace)`; `internal/game` and `DefaultConfig` stay as they are
- [ ] State gains `pace`; `tick_rate` keeps its meaning (game ticks per game second)
- [ ] Viewer: show the pace on the HUD, and use the real tick length for smoothing between ticks so motion stays even at low pace
- [ ] Server test with `httptest` that the state carries the pace, and a test of the interval calculation; no sleeping
- [ ] `docs/API.md` and `README.md` updated in the same change
- [ ] `go vet ./...`, `gofmt -l .`, `go test -race ./...`
- **Status:** pending

### Phase 5: Status text in the HUD
An API change: docs and README move in the same change.
- [ ] WebSocket message `{"type":"status","text":"..."}` and an HTTP twin, accepted only from the current controller; 409 or `error` otherwise
- [ ] Length cap, printable characters only; cleared on release, disconnect and controller change
- [ ] State gains `controller_status`
- [ ] Viewer draws it under the driver name (restart the server after editing `web/`)
- [ ] Server tests with `tick()` and `httptest`, named after the behaviour; `go test -race ./...`
- [ ] `docs/API.md` and `README.md` updated
- [ ] `llmbot` publishes latency, confidence and state form as status, at a rate that does not flood the socket
- **Status:** pending

### Phase 6: Quick look and write-up
- [ ] Pick the seed and the run length (Key Question 7)
- [ ] For each model in turn, others unloaded with `ollama stop`: `raw` then `digested`, starting at pace 1.0 and stepping down until it survives; note that pace
- [ ] One pass with `-min-confidence` set, on a Tev model, to see what the gate changes
- [ ] README section: the Tev call and the Gemma call side by side, the knobs Gemma needed, and the observed numbers with their caveats (one seed, RAM figures across different quantizations)
- [ ] Move the decisions and failed approaches that matter to the game into `task_plan.md`
- [ ] Walk through the result with the user
- **Status:** pending

## Key Questions
Proposed answers in brackets. Numbers 1 and 2 block Phase 2; the rest can be settled in the phase that needs them.

1. Settled in Phase 1: yes, with the word form. On 120 probe states `tev1:4b` chose a survivable move 120 times and moved in 45 of the 46 where its lane was not clear; `tev1:0.8b` 118 and 32 of 46. Whether 0.8b moves early enough to drive is for the Phase 2 run to show
2. Settled in Phase 1: coarse words, relative to the car, as one sentence. See "Digested state" under Notes
3. Stale threshold? It is counted in ticks, so it adapts to the pace: Gemma's 230 ms is about 7 ticks at pace 1.0 and about 2 at 0.25. [Default 6 ticks, set by `-max-stale`]
4. How many obstacles in the raw state? Tev's `num_ctx` is 2050; a state with 2 obstacles used 204 input tokens. [Nearest 8]
5. Which paces are allowed? [Any value above 0 up to 1.0]
6. Settled: the state JSON reports `pace`, because the HUD and the viewer's smoothing need it
7. Which seed and how long is a run? [Seed 7, which the sample bot is known to survive; stop by hand after about 3 minutes]
8. Is runner process RSS comparable across Tev (Q8) and Gemma (Q4, with a vision projector loaded)? [Report it with that caveat]

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| "Memory" means RAM footprint of the loaded model | User's choice; measurable locally |
| Contenders: `tev1:0.8b`, `tev1:4b`, `gemma4:e2b` | Two Tev sizes separate "decision model vs LLM" from "small vs large" |
| Hosted Jev is out of scope | Waitlisted; only Tev runs locally |
| Tev is called on `POST /v1/systemone` with `state` and `questions`; `criteria` is a map of option to description | Found by probing Ollama 0.35.1; a list is rejected |
| Gemma is called on `/api/chat` with a JSON enum `format`, thinking off, temperature 0 | Gemma is rejected on the decision endpoint; this returned a clean answer in about 230 ms |
| One `choice` question per tick: `left`, `none`, `right` | The question type Tev was trained for; keeps both models answering the same thing |
| Two state forms, `raw` and `digested`, switched by a flag | Seeing the same model on both is the core lesson: the model judges, the harness calculates |
| No step mode; the game runs on its own clock and never waits for the driver | User's choice; model speed must stay visible as a difference |
| Slow-down is a `-pace` flag that stretches the wall clock; the game core and its defaults are untouched; the pace is shown on the HUD | Every model plays the identical game, so the pace a model needs is a finding about its speed alone. Replaces the speed scale chosen earlier the same day |
| Scores are comparable across paces | Distance per tick does not change with the pace |
| One call at a time on the newest state; stale answers dropped and counted | Decision rate equals model speed; the dropped share shows when a model is simply too slow |
| Confidence is logged and shown; `-min-confidence` gate is off by default | See the ungated model first. Staying in lane is not a safe fallback, so a gate can cause a crash by inaction |
| No separate determinism test | User's choice. The probe already showed identical probabilities on repeat calls |
| One seed, watched live | A quick look; latency and RAM are the dependable numbers |
| Terminal log and summary, plus a generic `status` text from the driver shown in the HUD | Keeps LLM vocabulary out of the API contract |
| `cmd/llmbot` is a new Go command using only the public API and `net/http` | AGENTS.md: no new dependency, bots never import `internal/server` |
| RAM is read from the Ollama runner process, not `/api/ps` | `/api/ps` reported 383 MB for Gemma against about 5 GB of process memory |
| One warm-up call before each run; models run one at a time | Load takes 3 to 6 s and must not count as decision latency |
| Digested state is one sentence of coarse words per move (left, current, right), not numbers and not absolute lanes | Phase 1 probe: numbers scored no better than the raw state, and absolute lane numbers make the model work out which lane a move leads to |
| `instructions` and `criteria` are the same on every call; only `state` changes | Ollama answers a prompt it has seen before in about 20 ms (0.8b) or 55 ms (4b). Putting the lane words into `criteria` scored the same but lost that |
| This plan lives in `task_plan_tev.md`; `task_plan.md` is left as the game's plan | The existing plan is the project's memory and must not be overwritten |

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
| Tev called through `/api/chat` with lettered options: 21 s of thinking, empty answer | 1 | Wrong endpoint. Use `/v1/systemone` |
| `/v1/systemone` with `options` as a list: 400 "choice criteria must map option keys to descriptions or null" | 1 | The field is `criteria` and it is a map |
| Gemma with `num_predict` 1 or 8 and "return only the letter": empty string | 1 | Use the JSON enum `format`; needs about 7 output tokens |
| Raw obstacle list as state: both Tev sizes chose a crashing move on the probe state | 1 | Expected from the docs (no arithmetic). Kept as the `raw` mode on purpose; `digested` is the remedy to test |
| `/api/ps` size used for RAM: 383 MB reported for Gemma | 1 | Read the runner process RSS instead |
| Digested state as ticks until blocked, keyed by absolute lane (`lane_0` to `lane_3`) or by move | 1 | No better than raw: 0.8b answered `none` on all seven probe states, 4b 3 to 5 of 7 safe, Gemma answered `right` every time on the absolute form. Use words |
| Word digest as bare JSON (`{"left":"blocked soon",...}`) with a one-line question | 1 | 0.8b still answered `none` nearly always, with confidence under 0.1. A sentence naming the lanes plus an instruction that states the rule fixed it |
| Latency taken from repeat calls with the same prompt (21 ms and 52 ms) | 1 | Those were cache hits. A prompt not seen before costs about 90 ms (0.8b) and 420 ms (4b) digested, 127 ms and 627 ms raw. Measure on changing states |
| Planned slow-down by scaling the car's speeds (`-speed-scale`) | 1 | Dropped before any code: it makes a different game per scale, scores stop being comparable, the stale threshold breaks and the fairness tests must be rerun. Replaced by `-pace` |

## Notes
- Update phase status as you progress: pending → in_progress → complete
- Re-read this plan before major decisions; log every error and dead end
- Do not start Phase 3 without the user's sign-off on the Phase 2 run
- Phases 4 and 5 touch the server and the API, not the game core. `AGENTS.md` applies: API docs in the same change, loopback rules untouched, nothing written to disk, commit only when asked
- Phases 4 and 5 do not depend on each other and can swap order
- First probe figures (one state, repeated, M2 Pro): `tev1:0.8b` about 21 ms, `tev1:4b` about 52 ms, Gemma with JSON enum about 230 ms, Gemma out of the box about 15 s. Phase 1 showed the Tev figures were cache hits; see below
- **Digested state (Phase 1 result).** `state` is one sentence: `Lane to the left: blocked soon. Current lane: clear. Lane to the right: wall.` Each lane gets one of `clear` (nothing level with the car for 60 ticks), `blocked soon`, `blocked now` (within 2 ticks, so moving there is a crash), `wall` (road edge), or `blocked soon, clear lane beyond` for a neighbour that is blocked but can be crossed in time to reach a clear lane further that way. The spans are computed as in `game.Solve` (margin 0.5, closing speed per tick)
- **Wording.** `instructions`: `Which move keeps the car on a clear lane? Stay if the current lane is clear. Otherwise move to a clear lane, or towards a clear lane beyond.` `criteria`: `left` = `move one lane to the left`, `none` = `stay in the current lane`, `right` = `move one lane to the right`. Gemma gets the same three parts in one user message
- **Probe scores**, 120 states (30 captured on seed 7 with the sample bot driving, car placed in each lane; 46 have the own lane not clear). Safe = a survivable move; moved = left the lane when it was not clear. `tev1:0.8b` 118 safe, 32 of 46 moved, confidence 0.55 when safe and 0.20 when not. `tev1:4b` 120 and 45, confidence 0.92. `gemma4:e2b` 119 and 36
- **Latency depends on whether the prompt was seen before** (one model loaded, median ms). Raw, every state new: 0.8b 127, 4b 627, Gemma 485. Digested, sentence new: 0.8b about 90, 4b about 420, Gemma about 360. Digested, sentence seen before: 0.8b 22, 4b 56, Gemma 242. The digested sentence has few distinct values, so most calls while driving should be hits; the raw state changes every tick and never is. So on raw, `tev1:4b` is the slowest of the three (about 19 ticks at pace 1.0)
- Runner RSS seen during the probe (`llama-server` process): 0.8b 2,911 MB, 4b 9,562 MB, Gemma 5,365 MB. Higher than the first figures for Tev; to be measured properly in Phase 3
- The probe is a single-tick check. A model that stays in a blocked lane is still "safe" until the last few ticks, so only driving (Phase 2) shows whether it moves in time
- Start the game with `go run ./cmd/outrun`, viewer at http://127.0.0.1:8765; Ollama at `localhost:11434`
