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
- **Status:** built and run, waiting for the user's sign-off
- **Started:** 2026-10-04
- Actions taken:
  - Wrote `cmd/llmbot`: WebSocket client with the manners of `cmd/bot`, digest builder, Tev caller, per-decision log line
  - Ran `tev1:0.8b` on seed 7 three times and `tev1:4b` once, at normal speed
  - Probed 15 sentences on both Tev models after the 0.8b crashes
- Files created/modified:
  - `cmd/llmbot/main.go`, `cmd/llmbot/digest.go`, `cmd/llmbot/tev.go`, `cmd/llmbot/llmbot_test.go`
  - `.gitignore`, `AGENTS.md` (path table), `task_plan_tev.md`, `findings.md`, `progress.md`

## Test Results
| Test | Input | Expected | Actual | Status |
|------|-------|----------|--------|--------|
| Digested probe, `tev1:4b` | 120 states | survivable move | 120 safe, 45 of 46 moved when needed | pass |
| Digested probe, `tev1:0.8b` | 120 states | survivable move | 118 safe, 32 of 46 moved when needed | open: Phase 2 run decides |
| Digested probe, `gemma4:e2b` | 120 states | survivable move | 119 safe, 36 of 46 moved when needed | pass |
| `go test ./...`, `go vet ./...`, `gofmt -l .` | after Phase 2 | pass, no output | pass, no output | pass |
| `tev1:0.8b` drives seed 7 | 3 runs, pace 1.0 | drives | crashed at tick 432 each time | works end to end; judgment fails |
| `tev1:4b` drives seed 7 | 1 run, pace 1.0 | drives | 1,956 ticks, score 3,789 | works; digest and latency limits seen |
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
| Where am I? | Phase 2 built, waiting for sign-off on the run, the log format and the digest shape |
| Where am I going? | Phase 3 harness, 4 pace, 5 status text, 6 quick look |
| What's the goal? | `cmd/llmbot` lets Tev and Gemma drive so speed, RAM and harness can be compared |
| What have I learned? | See findings.md |
| What have I done? | See above |

---
*Update after completing each phase or encountering errors*
