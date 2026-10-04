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
| Raw probe, all three | 7 states | survivable move | `none` every time, 4 of 7 | fails, as expected |

## Error Log
| Timestamp | Error | Attempt | Resolution |
|-----------|-------|---------|------------|
| 2026-10-04 | Number digests no better than raw | 1 | Use words |
| 2026-10-04 | Bare JSON word digest: 0.8b always `none` | 1 | Sentence form plus a rule in the instruction |
| 2026-10-04 | Latency figures were cache hits | 1 | Measure on changing states |

## 5-Question Reboot Check
| Question | Answer |
|----------|--------|
| Where am I? | Phase 3 built, waiting for approval and a decision on the wording |
| Where am I going? | 4 pace, 5 status text, 6 quick look |
| What's the goal? | `cmd/llmbot` lets Tev and Gemma drive so speed, RAM and harness can be compared |
| What have I learned? | See findings.md |
| What have I done? | See above |

---
*Update after completing each phase or encountering errors*
