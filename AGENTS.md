# AGENTS.md

## Project overview

An OutRun-style four-lane dodger in Go. One binary runs the game headless and serves an HTTP + WebSocket API on 127.0.0.1. The browser viewer and the sample bot are both ordinary clients of that API.

| Path | Contents |
|------|----------|
| `cmd/outrun` | Server binary: flags, loopback check, wiring |
| `cmd/bot` | Sample bot, a separate binary |
| `cmd/llmbot` | Bot that lets a local Ollama model, hosted Jev or Claude Haiku drive; plans in `docs/planning/task_plan_tev.md` and `docs/planning/task_plan_jev.md` |
| `internal/game` | Headless core: rules (`game.go`), observation (`state.go`), reference solver (`solver.go`) |
| `internal/server` | HTTP and WebSocket handlers, real-time tick loop, controller arbitration |
| `web` | Viewer (`index.html`, `viewer.js`), embedded with `go:embed` |
| `docs/API.md` | The API contract |
| `docs/*.md` | Details linked from `README.md`: running and flags, writing a bot, the model bot, the model comparison and its runs |
| `docs/planning/task_plan.md` | Decisions with their rationale, and a log of approaches that already failed |
| `docs/planning/` | The other plans, intent and decision notes, findings and progress logs |

How to play is in `README.md`, which stays short and links to the details in `docs/`. Read `docs/planning/task_plan.md` before a larger change, and record new decisions and failed approaches there.

## Setup commands

- Needs Go 1.26 or newer. There is nothing to install; `go` fetches the one dependency.
- Start the server and viewer: `go run ./cmd/outrun`, then open http://127.0.0.1:8765
- Start the sample bot (server must be running): `go run ./cmd/bot`
- Build everything: `go build ./...`
- The viewer is embedded at build time, so restart the server after editing anything in `web/`.

## Testing instructions

- Run all tests: `go test ./...` (a few seconds)
- Run with the race detector after touching `internal/server`: `go test -race ./...` (about a minute)
- Run one test: `go test ./internal/game -run TestSolverSurvivesEverySeed`
- Static checks: `go vet ./...` and `gofmt -l .` (must print nothing)
- All of the above must pass before a change is done.
- A bug fix comes with a regression test named after the behaviour, for example `TestPassedObstacleCannotBeHit`.
- Server tests call `tick()` by hand and use `httptest`. Do not add tests that sleep or depend on real time.
- Any change to the spawner, speeds or spacing must keep `TestSolverSurvivesEverySeed` (200 seeds) and `TestBotSurvivesWithAndWithoutLag` green.

## Architecture rules

- `internal/game` stays pure: no networking, no `time.Now`, no goroutines. The game advances only through `Step(action)`. The same seed must always give the same obstacle layout. All timing lives in `internal/server`.
- Every layout is survivable: a row never blocks all lanes, and the next row is met at least 12 ticks after the previous one is fully behind the car. Obstacles are planned by the tick at which the car meets them (`encTick`), not by road position.
- One driver at a time. A bot may claim only from `human`; `human` can always claim back; release or WebSocket disconnect returns control to `human`. Input or release from a non-controller is a 409 over HTTP and an `error` message over WebSocket.
- At most one lane change per tick. Inputs are queued (`maxQueuedActions = 4`) and applied one per tick. Human and bot play by the same rule.
- `cmd/bot` talks to the server only over the public API. It may import `internal/game` for `Solve` and the state types, never `internal/server`.
- Nothing is written to disk. Best score is kept in memory only.
- Seeds are 32-bit, because larger values lose precision as JavaScript JSON numbers.

## Code style

- Go: `gofmt` formatting, standard library first. `github.com/coder/websocket` is the only dependency; ask before adding another.
- Viewer: plain JavaScript on a canvas, no build step, no libraries.
- The viewer keeps a sprite on screen for as long as the core can still hit it. Hiding it earlier makes crashes look unfair.
- Difficulty settings live in `DefaultConfig` in `internal/game/game.go`. Speed has no upper limit. Rows are kept apart in ticks (`MinGapTicks`) as well as in road distance, which is what keeps a fast run survivable, so change speed, spacing and tick rate together.
- Do not retune the difficulty defaults unprompted. That is waiting on feedback from a human playing.
- When endpoints, message types or the state JSON change, update `docs/API.md`, `docs/write-a-bot.md` and `README.md` in the same change. The viewer, the sample bot and outside bots all depend on that shape.

## Security considerations

- The API has no authentication, so it is loopback only. The server exits if `-addr` is not a loopback address.
- Cross-origin POSTs are rejected (Origin vs Host), and the WebSocket keeps the library's same-origin check.
- Do not loosen any of these, and do not add a way to bind to other interfaces.

## Commit guidelines

- Commit only when asked.
- Subject line in the imperative, sentence case, no prefix, for example `Add OutRun-style lane dodger with a local bot API`.
- Run `go vet ./...` and `go test ./...` before committing.
