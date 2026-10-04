# Progress Log

Plan: `task_plan_tev.md`. Branch: `tev-vs-gemma`.

## Session: 2026-10-04

### Phase 1: Orient and finish the probe
- **Status:** complete, approved by the user
- **Started:** 2026-10-04
- Actions taken:
  - Confirmed Ollama 0.35.1 lists `tev1:0.8b`, `tev1:4b` and `gemma4:e2b`
  - Captured 60 states from a seed 7 run driven by the sample bot
  - Probed raw and three digested forms on seven hand-picked states, then six wordings, then five wordings on 120 states with one model loaded at a time
  - Measured latency on repeated and on changing prompts
  - Chose the digested shape and the wording; recorded both in `task_plan_tev.md`
- Files created/modified:
  - `task_plan_tev.md`, `findings.md`, `progress.md`
  - No code changed

### Phase 2: Tracer bullet, one model driving end to end
- **Status:** complete, signed off by the user after watching both Tev models
- **Started:** 2026-10-04
- Actions taken:
  - Wrote `cmd/llmbot`: WebSocket client with the manners of `cmd/bot`, digest builder, Tev caller, per-decision log line
  - Ran `tev1:0.8b` on seed 7 three times and `tev1:4b` once, at normal speed
  - Probed 15 sentences on both Tev models after the 0.8b crashes
- Files created/modified:
  - `cmd/llmbot/main.go`, `cmd/llmbot/digest.go`, `cmd/llmbot/tev.go`, `cmd/llmbot/llmbot_test.go`
  - `.gitignore`, `AGENTS.md` (path table), `task_plan_tev.md`, `findings.md`, `progress.md`

### Phase 3: The comparison harness
- **Status:** complete, approved by the user with the wording put back to Phase 2 and `-warm` off by default
- **Started:** 2026-10-04
- Actions taken:
  - Probed 13 variants on 140 sentences for `tev1:0.8b` and `left`; none worked
  - The user declined the same probe for `tev1:4b` and Gemma and chose to build Phase 3 with `blocked later` untested
  - Added the Gemma caller, the common `decider`, raw and digested prompt builders, `-state`, `-max-stale`, `-min-confidence`, `-api`, `-warm`, `-v`, the change-only log and the run summary with runner memory
  - Ran each model once on seed 7, digested, pace 1.0
- Files created/modified:
  - `cmd/llmbot/main.go`, `cmd/llmbot/prompt.go` (was `digest.go`), `cmd/llmbot/model.go` (was `tev.go`), `cmd/llmbot/stats.go`, `cmd/llmbot/llmbot_test.go`
  - `task_plan_tev.md`, `findings.md`, `progress.md`

### Phase 4: Clock pace in the server
- **Status:** complete, approved by the user
- **Started:** 2026-10-04
- Actions taken:
  - `-pace` flag, `Server.SetPace`, `tickInterval`, `pace` in the state, viewer smoothing and HUD, docs
  - Checked the tick rate at pace 0.25 on a second port and let Gemma drive there
- Files created/modified:
  - `cmd/outrun/main.go`, `internal/server/server.go`, `internal/server/server_test.go`, `web/viewer.js`, `docs/API.md`, `README.md`
  - `cmd/llmbot/main.go`, `cmd/llmbot/stats.go`, `cmd/llmbot/llmbot_test.go` (pace in the summary)

### Phase 5: Status text in the HUD
- **Status:** complete, approved by the user
- **Started:** 2026-10-04
- Actions taken:
  - `status` WebSocket message, `POST /control/status`, `controller_status` in the state, cleared on any change of controller
  - Viewer draws it under the driver name; `llmbot` publishes state form, latency and confidence
- Files created/modified:
  - `internal/server/server.go`, `internal/server/server_test.go`, `web/viewer.js`, `docs/API.md`, `README.md`
  - `cmd/llmbot/main.go`, `cmd/llmbot/llmbot_test.go`

### Phase 6: Quick look and write-up
- **Status:** runs and write-up done, walk-through with the user open
- **Started:** 2026-10-04
- Actions taken:
  - 19 runs on seed 7: three models, raw and digested, pace 1.0, 0.5 and 0.25, plus one gated run
  - Measured runner memory separately on freshly loaded models
  - Fixed a race in how `llmbot` recognises its own run; the summary now counts the runners it adds up
  - README section, decisions and errors moved into `task_plan.md`
- Files created/modified:
  - `README.md`, `task_plan.md`, `task_plan_tev.md`, `findings.md`, `progress.md`
  - `cmd/llmbot/main.go`, `cmd/llmbot/stats.go`, `cmd/llmbot/llmbot_test.go`

### Walk-through and follow-up (same day)
- **Status:** the walk-through item stays open at the user's request. The user was shown the result and the implementation, watched Gemma and both Tev models drive, and asked for the changes below
- Actions taken:
  - Found why Tev and Gemma both crashed at tick 836 and fixed the harness in four steps: recheck before sending, answer time in `clear lane beyond`, relative `clear`, and a recheck that counts only the answers still to come
  - Game, at the user's request: no upper speed limit; start speed 50
  - README: suggested pace 0.5; the rules described; the old table marked as predating the changes; a second table with the runs on the new road
  - Runs on seed 7 at pace 0.5 and 0.7 before and after each step; see `findings.md`
- Files created/modified:
  - `cmd/llmbot/prompt.go`, `cmd/llmbot/stats.go`, `cmd/llmbot/main.go`, `cmd/llmbot/llmbot_test.go`
  - `internal/game/game.go`, `internal/game/game_test.go`, `cmd/bot/main_test.go`
  - `README.md`, `docs/API.md`, `AGENTS.md`, `task_plan.md`, `task_plan_tev.md`, `findings.md`, `progress.md`

## Test Results
| Test | Input | Expected | Actual | Status |
|------|-------|----------|--------|--------|
| Digested probe, `tev1:4b` | 120 states | survivable move | 120 safe, 45 of 46 moved when needed | pass |
| Digested probe, `tev1:0.8b` | 120 states | survivable move | 118 safe, 32 of 46 moved when needed | open: Phase 2 run decides |
| Digested probe, `gemma4:e2b` | 120 states | survivable move | 119 safe, 36 of 46 moved when needed | pass |
| `go test ./...`, `go vet ./...`, `gofmt -l .` | after Phase 2 | pass, no output | pass, no output | pass |
| `tev1:0.8b` drives seed 7 | 3 runs, pace 1.0 | drives | crashed at tick 432 each time | works end to end; judgment fails |
| `tev1:4b` drives seed 7 | 1 run, pace 1.0 | drives | 1,956 ticks, score 3,789 | works; digest and latency limits seen |
| `go test ./...`, `go vet ./...`, `gofmt -l .` | after Phase 3 | pass, no output | pass, no output | pass |
| Three models drive seed 7, digested | 1 run each, pace 1.0 | summary printed | 0.8b 311 ticks, 4b 386 ticks, Gemma 311 ticks and 100% stale | harness works; nobody survives |
| `-state raw`, `-min-confidence` | live | drive | not run live; covered by unit tests only | open for Phase 6 |
| `go test -race ./...`, `go vet ./...`, `gofmt -l .` | after Phase 4 | pass | pass | pass |
| Tick rate at `-pace 0.25` | 4 s on a live server | 7.5 per second | 7.5 per second | pass |
| Bad `-pace` | 0, 1.5, -1 | refused at startup | refused | pass |
| Gemma drives at pace 0.25 | seed 7, digested | answers arrive in time | 1,347 ticks, 23 rows, 0 stale | pass |
| Viewer at low pace | browser | smooth motion, pace on HUD | not looked at by the agent | open: user to check |
| `go test -race ./...`, `go vet ./...`, `gofmt -l .` | after Phase 6 | pass | pass | pass |
| Quick look | 19 runs, seed 7 | a pace at which each model survives | only `tev1:4b` digested at 0.25 survived | done; see README |
| Raw probe, all three | 7 states | survivable move | `none` every time, 4 of 7 | fails, as expected |

| `go test -race ./...`, `go vet ./...`, `gofmt -l .` | after the walk-through session | pass | pass | pass |
| Layouts for the first 2,300 ticks, 60 seeds | before and after removing the speed cap | identical | identical | pass |
| Solver with no speed limit | 40 seeds, 60 minutes of game time | survives | survived, speed about 2,040 | pass (one-off probe) |
| `tev1:4b` on the new road, every fix | seed 7, pace 0.5 | reaches tick 2,700 | still driving at 2,763 | pass, one run |
| `gemma4:e2b` on the new road, every fix | seed 7, pace 0.5 | reaches tick 2,700 | crashed at 767, moved into `blocked now` | fails on the model's move |
| `tev1:0.8b` on the new road, every fix | seed 7, pace 0.5 | reaches tick 2,700 | crashed at 357, never answers `left` | fails on the model's move |

## Error Log
| Timestamp | Error | Attempt | Resolution |
|-----------|-------|---------|------------|
| 2026-10-04 | Number digests no better than raw | 1 | Use words |
| 2026-10-04 | Bare JSON word digest: 0.8b always `none` | 1 | Sentence form plus a rule in the instruction |
| 2026-10-04 | Latency figures were cache hits | 1 | Measure on changing states |

## 5-Question Reboot Check
| Question | Answer |
|----------|--------|
| Where am I? | Phase 6 done except the walk-through with the user, which the user left open |
| Where am I going? | The walk-through; `task_plan_jev.md` is a separate follow-up. Open by choice: re-running the README table of 19 runs |
| What's the goal? | `cmd/llmbot` lets Tev and Gemma drive so speed, RAM and harness can be compared |
| What have I learned? | See findings.md |
| What have I done? | See above |

---
*Update after completing each phase or encountering errors*
