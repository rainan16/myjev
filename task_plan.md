# Task Plan: Raw OutRun-style lane dodger in Go with a bot API

<!--
  This file is the working memory on disk for the whole task.
  Re-read it at the start of every session and before major decisions.
  Source of the decisions: ./intent_decisions_answers.md (question-me output, 2026-10-03).
-->

## Goal
A single local Go binary runs a raw, wireframe OutRun-style game (4 lanes, K = left, L = right, dodge cars and obstacles) whose headless core exposes a WebSocket + HTTP API on 127.0.0.1, so a human in the embedded web viewer or an external program can play it and watch it in real time, with a sample Go bot included.

## Current Phase
Phase 7 (Phases 1 to 6 complete; Phase 5 was finished early when the user asked for better graphics)

## Target layout

```
myjev/
  go.mod
  cmd/outrun/main.go        # game server binary (core + API + embedded viewer)
  cmd/bot/main.go           # sample bot, separate binary, uses only the public API
  internal/game/            # headless core: state, tick, spawner, collision (no net, no time.Now)
  internal/server/          # HTTP + WebSocket, controller arbitration, real-time loop
  web/                      # index.html, viewer.js (embedded with go:embed)
  docs/API.md               # the API contract
```

`internal/game` must stay pure: a `Step(action)` function on a state seeded by an RNG. The real-time loop lives in `internal/server`. This keeps the core unit-testable even though v1 ships real-time only.

## Phases

### Phase 1: Setup and API contract
Just enough to orient and to pin down the interface everything else depends on.
- [x] Ask the user whether to `git init` the directory (it is not a repo yet)
- [x] `go mod init`, create the directory layout above, confirm `go build ./...` works (Go 1.26.4 is installed)
- [x] Confirm the proposed defaults in "Key Questions" with the user, then move them to "Decisions Made"
- [x] Write `docs/API.md`: endpoints, WebSocket message types, the state JSON schema, the controller claim/release flow
- [x] Pick the WebSocket library (see Key Question 8)
- **Status:** complete

### Phase 2: Tracer bullet (thin slice through every layer)
The smallest thing that proves the architecture: core, API, viewer and an external client all talking.
- [x] Core: 4 lanes, car in one lane, constant speed, one static obstacle type spawned from a seed, collision sets game over
- [x] Server: fixed-rate tick loop, WebSocket pushes state each tick, WebSocket accepts `left` / `right`, `GET /state`, `POST /input`, `POST /reset`
- [x] Viewer: embedded page on localhost, straight road drawn as plain perspective lines, car and obstacles as outlines, K / L send input, a key restarts
- [x] External check: drive the car with `curl -X POST /input` while watching the viewer
- [x] **STOP: demo to the user and get sign-off before Phase 3.** Confirm the feel of lane hopping and the shape of the state JSON
- **Status:** complete

### Phase 3: Complete game rules in the core
- [x] Speed ramps from start speed to max speed over time; score = distance survived
- [x] Two obstacle types: static obstacles and slower moving cars
- [x] Seeded spawner that always leaves a passable path, with row spacing derived from max speed and tick rate
- [x] Observation: car lane, speed, distance, score, game-over flag, obstacle list (lane, distance ahead, type), lookahead lane grid
- [x] Reset with optional seed; best score kept in memory only
- [x] Unit tests: collision, lane bounds, same seed gives the same obstacle layout
- [x] Fairness test: a perfect-lookahead solver survives a long run across many seeds (see Risks)
- **Status:** complete

### Phase 4: Complete API and controller arbitration
- [x] One active controller at a time: human by default, a client claims and releases control; inputs from a non-controller are rejected with a clear error
- [x] State includes who is driving; releasing or disconnecting hands control back to the human
- [x] Input handling: at most one lane change applied per tick (see Key Question 7)
- [x] Bind to 127.0.0.1 only, port set by flag, WebSocket origin check
- [x] Bring `docs/API.md` in line with what was built; server tests with `httptest`
- **Status:** complete

### Phase 5: Viewer with the OutRun look
- [x] Segment-based pseudo-3D road in outline, 4 lanes, lane markers scrolling with speed
- [x] Curves as a purely visual effect (no physics, no hills)
- [x] Cars and obstacles as scaled wireframe shapes by distance
- [x] HUD: score, best score, speed, who is driving, game-over screen with restart hint
- [x] One lane per key press, key auto-repeat ignored
- **Status:** complete

### Phase 6: Sample bot
- [x] `cmd/bot`: connect over WebSocket, claim control, read the lane grid, choose the safest reachable lane, send left / right / none
- [x] Bot resets after game over and logs score per run
- [x] End-to-end check: start the game, open the viewer, start the bot, watch it play; then release control and play by hand
- **Status:** complete

### Phase 7: Tuning and delivery
- [ ] Tune tick rate, speed ramp and spawn spacing by playing as a human (waiting for the user's feedback after playing)
- [x] `go vet`, `go test ./...`, check the binary runs with no files beside it
- [x] README: how to run, keys, API quick start with curl and a WebSocket example, how to run the bot
- [ ] Final walkthrough with the user
- [x] Initial commit (`c7a2b23`, made by the user)
- **Status:** in_progress (README and checks done; tuning and walkthrough need the user)

## Key Questions
Proposed defaults are in brackets. Confirm or change them in Phase 1.

1. Tick rate and broadcast rate? [30 Hz tick, state pushed every tick]
2. Speed ramp? [start slow enough for about 1.5 s of reaction time per row, linear ramp to max over about 90 s, then flat]
3. Minimum spacing between obstacle rows? [derived, not hand-set: enough ticks at max speed to cross 3 lanes at one lane per tick, plus a margin]
4. Endpoint names and JSON schema? [`GET /state`, `POST /input`, `POST /reset`, `POST /control/claim`, `POST /control/release`, `GET /ws`; written down in `docs/API.md`]
5. Lane grid depth? [4 lanes x 12 rows ahead, each cell empty / car / obstacle]
6. Default port and flag? [`-addr 127.0.0.1:8765`]
7. Does a bot claiming control mid-run reset the run? [No. The run continues, state carries a run id, and the bot calls reset itself when it wants a clean run.] Related: if two inputs arrive within one tick, are they queued or does the last one win? [short queue, one applied per tick, so a fast human double-tap crosses two lanes]
8. WebSocket library? Go's standard library has none. [`github.com/coder/websocket`: small, maintained, the only external dependency]
9. Step mode later? [out of v1; the pure `Step` core keeps the door open]

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Headless core, viewer is just an API client | Human and bot use the same interface |
| Go, single binary | Small footprint, runs and tests locally |
| Embedded web viewer on canvas, wireframe pseudo-3D | No extra install; graphics do not matter much |
| Speed is automatic and ramps up | Keeps input at exactly two keys, gives a difficulty curve |
| Hit = game over, score = distance | Clear end signal for bots, easy to reset |
| Curves yes, hills no, curves visual only | OutRun look without conflicting with discrete lanes |
| 4 discrete lanes, key press = one lane change | Simple and easy for bots; user's explicit choice over continuous steering |
| WebSocket stream + HTTP endpoints | Real-time push for bots and viewer, curl-friendly for poking |
| Real-time tick loop only, no step mode | User's choice; accepted that bot latency affects outcomes |
| The game's clock can be slowed with `-pace` (above 0 up to 1); the core, the speeds and the spacing are untouched, and the state reports `pace` | 2026-10-04, from the Tev and Gemma task. Every driver plays the same game tick for tick, so scores compare across paces. Scaling the car's speeds instead was rejected: it makes a different game per scale and the fairness tests would have to be rerun |
| The pace is fixed for the life of the server | A run's numbers belong to one pace |
| The controller can publish one line of free text (`status` message, `POST /control/status`, `controller_status` in the state); the server does not interpret it; cleared when the controller changes, kept across a reset | 2026-10-04. Lets a bot show what it is doing in the HUD without putting any bot's vocabulary into the API |
| Setting a status does not broadcast; it goes out with the next tick | A chatty driver cannot flood the viewers |
| One active controller, switchable via API | Human and bot never fight over the car |
| State = structured objects plus lookahead lane grid | Usable by both simple and smarter bots |
| Action = left / right / none | Bot plays exactly the game a human plays |
| Seeded random spawning, always a passable path | Repeatable layouts, never unwinnable |
| One lane per key press, no auto-repeat | Same rule for human and bot |
| 127.0.0.1 only, no auth, WebSocket origin check | Local toy; stops random web pages driving the game |
| Sample bot as separate Go command | Proves the external API end to end |
| Reset via API or key; best score in memory only | Nothing written to disk |
| Defaults accepted 2026-10-03: 30 Hz tick, state every tick, `-addr 127.0.0.1:8765`, 4x12 grid (row length 20), about 90 s ramp, claim does not reset the run, inputs queued (max 4) one per tick | User accepted all proposed defaults |
| `github.com/coder/websocket` is the only dependency | Standard library has no WebSocket; user accepted |
| git repository initialised, nothing committed | User said yes to `git init`; commit only when asked |
| Client identity is a name string, default `human`; bots claim with a name | Written in `docs/API.md`; implemented in Phase 4 |
| Cross-origin browser POSTs are rejected (Origin vs Host), WebSocket uses the library's same-origin default | Already in place since Phase 2 |
| Obstacles are planned by the tick at which the car meets them (`encTick`), not by road position | The car's speed depends only on the tick, so the meeting moment is known in advance. This makes the fairness rule exact even with moving traffic |
| Fairness rule: a row never blocks all lanes; the next row is met at least 12 ticks after the previous one is fully behind the car | Enough to cross all lanes at one lane per tick. Proven by `TestSolverSurvivesEverySeed` (200 seeds, 210 s each) |
| A moving car is only placed if it would not be seen driving through a barrier in its lane; otherwise that slot becomes a barrier | Resolves the "cars catch up with obstacles" risk. The same test checks that no two obstacles ever overlap |
| All traffic drives at one speed (20), below the start speed (40) | Traffic never interacts with other traffic and is always overtaken |
| Speed 40 to 90 over 90 s, row spacing 60 to 110 units, 15% of rows leave a single open lane, half of obstacles are cars | First values; to be tuned by hand in Phase 7 |
| State gained `tick_rate`, `car_length` and per-obstacle `speed` | A bot needs them to predict when an obstacle is reached |
| `game.Solve` is the reference player: 60-tick lookahead search over the public observation | Used by the fairness test; reuse it for the sample bot in Phase 6 |
| Control rules: a bot claims only from `human` (or repeats its own claim); `human` can always claim back (viewer key H); release or WebSocket disconnect returns control to `human`; anyone may reset | Keeps "one driver at a time" without locking the human out |
| Input or release from a non-controller is 409 over HTTP and an `error` message over WebSocket | A bot learns at once that it lost the car |
| `-addr` must be a loopback address or the server exits | Enforces "127.0.0.1 only, no auth" instead of leaving it to the default |
| The bot plays with `game.Solve` on the obstacle list rather than the lane grid | The obstacle list carries speeds, which the grid does not; the grid stays in the API for simpler bots |
| The bot only moves when the move is right whether it lands on the next tick or one tick late, and never resends a move still in flight | The game is real-time, so a message can miss a tick |
| The bot stops when someone else takes the car, and releases it on Ctrl-C | It should not fight the human for control |
| Random seeds are 32-bit | Larger values lose precision in JavaScript JSON numbers |

## Risks
- **Fairness with moving cars.** Slower cars move forward and can catch up with static obstacles ahead of them, so two rows that were each passable can merge into a wall. Options: give each row a single type and space rows so they cannot merge before passing the player, or despawn a car that reaches an obstacle. Decide in Phase 3 and let the solver test prove it.
- **Real-time only makes runs non-reproducible.** The seed fixes the obstacle layout, not the outcome. Keep all timing out of `internal/game` so logic tests stay deterministic.
- **Speed cap versus lane-change rate.** At one lane per tick the fairness guarantee holds only if max speed is bounded by row spacing. Derive one from the other rather than tuning both by hand.
- **Human reaction time.** A layout a bot can pass may still be unplayable for a person. The ramp and max speed need tuning by hand in Phase 7.

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
| Car crashed when changing lane after the barrier had left the screen: obstacle length 6 stayed hittable until 10 units behind the nose, but the viewer hid it at 5 | 1 | Barrier length set to 1 in the core; viewer keeps a sprite on screen for as long as the core can hit it (`ahead + length > -4`). Regression test `TestPassedObstacleCannotBeHit` |
| Best score was only updated when a state was requested, so a run with no viewer connected did not count | 1 | Found by `TestResetStartsANewRun`. Best score is now tracked on every tick |
| `git commit` refused: "Git commits are disabled by the demo policy plugin" | 1 | Not worked around; it is a deliberate policy in the user's Claude Code setup. User was given the command to run themselves. Retry only if the user says the policy is off |
| Lookahead solver waited until the last possible tick to move (any path that survived counted as equally good), so the bot crashed as soon as a row needed several lane changes | 1 | First tie-break (prefer the neighbouring lane that stays clear longer) was still short-sighted. Fix: head for the lane that stays clear longest overall, as long as the step costs no survival time |
| Bot counted the already-crashed run it found on connecting as its own first run | 1 | Bot ignores the run that existed before its reset |
| A bot that takes the first state it receives as "the run before mine" can hang: the server keeps only the newest state for a slow connection, so the greeting state may be replaced by one from after the bot's own reset. Seen once with `cmd/llmbot` (claimed the car, then never played) | 1 | `cmd/llmbot` now resets only after a state shows it driving and plays the run after that. `cmd/bot` still uses the old way and can hit the same race; not changed yet |
| Greedy reference solver (head for the lane that stays clear longest) crashed on some seeds: it steered through lanes that were about to be blocked | 1 | Not a spawner fault. Replaced with a lookahead search (`internal/game/solver.go`); all 200 seeds pass |
| Still crashed on an invisible barrier (state showed `ahead: -6`, length 1, no overlap with the car). First fix was incomplete | 2 | Real cause: the collision sweep started at the previous tick's rear edge, so a lane change hit a barrier up to one tick behind the car. Sweep now covers only the car's current body. The first regression test steered in one tick too late to catch it; it now steers on the exact tick, across 20 seeds |

## Notes
- Update phase status as you progress: pending → in_progress → complete
- Re-read this plan before major decisions
- Log ALL errors, including failed approaches, so they are not repeated
- Do not start Phase 3 without the user's sign-off on the Phase 2 demo
- Verified 2026-10-03 (second session): H in the browser takes the car from the bot and the bot exits cleanly; curves draw correctly; the bot survives 4 minutes on seed 7 (2.5 of them at top speed 90); Ctrl-C releases the car. Still open: a long human run at top speed, and the bot's `-runs` flag (it counts crashes only, and the bot did not crash in any run, so it never triggered)
- Run the bot with `go run ./cmd/bot` while the server is up (`-seed N`, `-runs N`, `-name X`). `TestBotSurvivesWithAndWithoutLag` checks 60 seeds at 0, 1 and mixed ticks of lag
- State after Phase 4: full game rules and the full API are in place and documented in `docs/API.md`. Server tests call `tick()` by hand, so they do not depend on real time
- 2026-10-03: user asked for better, more realistic graphics, so the wireframe look is dropped. `web/viewer.js` now draws a filled OutRun-style scene (sky, mountains, striped grass and road, visual-only curves, palms, barrier and car sprites, smoothing between ticks). Most of Phase 5 is done early; what remains there is the HUD for bot control and tuning the look of moving cars
- Done in Phase 3: anything alongside the car is marked in grid row 0. A traffic car being overtaken stays drawn at the bottom edge of the screen until it can no longer be hit
- 2026-10-04: a follow-on task (local models Tev and Gemma driving the game, with a `-pace` flag and a driver status text) has its own plan in `task_plan_tev.md`, decisions in `intent_decisions_answers_tev.md`
- Run with `go run ./cmd/outrun` and open http://127.0.0.1:8765. The viewer is embedded at build time, so restart after editing `web/`
