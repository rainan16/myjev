# outrun

A small OutRun-style lane dodger with a local API. You drive a car down a four-lane road and switch lanes to avoid barriers and slower traffic. A person can play it in the browser, and a program can play it over HTTP or WebSocket while you watch.

It is one Go binary: the game runs headless inside it, and the browser page is just another client of the same API.

It is also a test bench for models that have to decide 30 times a second. Six models have driven it so far, Tev in two builds, best first:

| Model | Kind | Runs on | Answer time | Ticks survived at full pace |
|-------|------|---------|-------------|-----------------------------|
| `nimble-v3:9b-mxfp8` | Decision model | Local, Ollama, 13 GB | 3 ms on a sentence it has seen, about 900 ms on a new one | Past 3,000 on all six seeds tried, with `-warm` |
| `tev1:4b`, new build | Decision model | Local, Ollama, MLX, 11.4 GB | 5 ms on a sentence it has seen, about 430 ms on a new one | Past 3,400 on all six seeds tried, with `-min-confidence 0.3`; median 763 on seeds 1 to 5 without |
| `tev1:4b`, old build | Decision model | Local, Ollama, 4.7 GB | 55 ms on a repeated sentence, about 450 ms on a new one | Median 2,710 on seeds 1 to 5; 357 on seed 7 |
| `jev-1.13.0` | Decision model | Hosted, TypeSafe | 250 to 340 ms | Median 1,841 on seeds 1 to 5; up to 3,202 on seed 7 |
| `claude-haiku-4-5` | General-purpose LLM | Hosted, Anthropic | about 810 ms | 1,594 on seed 7 |
| `gemma4:e2b` | General-purpose LLM | Local, Ollama, 5.0 GB | about 250 ms | Median 291 on seeds 1 to 5 |
| `tev1:0.8b` | Decision model | Local, Ollama, 1.0 GB (new build: MLX, 2.0 GB) | 21 ms (new build: 5 ms) | Median 180 on seeds 1 to 5; the new build 357 on seed 7 |

A tick is 1/30 of a second, so 3,000 ticks is 100 seconds of driving at a speed that keeps rising. Every figure is a single run. Ollama 0.40.5 no longer loads the old Tev builds, and `ollama pull` now fetches the new ones; see [Model runs](docs/model-runs.md#tev-pulled-again). How the models are asked is in [Let a model drive](#let-a-model-drive), and the details are in [Four kinds of model at the wheel](docs/models.md).

## Play

Needs Go 1.26 or newer.

```sh
go run ./cmd/outrun
```

Then open http://127.0.0.1:8765.

| Key | Action |
|-----|--------|
| **K** | One lane left |
| **L** | One lane right |
| **Space** | Restart after a crash |
| **H** | Take the car back from a bot |

Each key press moves exactly one lane; holding a key does nothing more. The car speeds up on its own: it starts at 50, reaches 100 after 90 seconds and keeps getting faster for as long as you survive. Any hit ends the run, and the score is the distance survived.

To fix the layout, slow the clock or build a single binary, see [Running the game](docs/running.md).

## Watch a bot play

With the server running, in a second terminal:

```sh
go run ./cmd/bot
```

The viewer shows `DRIVER gobot` and the car steers itself. After a crash the bot waits two seconds and starts a new run. Press **H** in the viewer to take over, or Ctrl-C in the bot's terminal; both hand the car back to you. The bot's flags are in [Running the game](docs/running.md#sample-bot).

## Let a model drive

`cmd/llmbot` lets a language model steer instead. It runs a local model through [Ollama](https://ollama.com), or calls hosted Jev or Claude Haiku:

```sh
go run ./cmd/llmbot -model tev1:4b -seed 7      # local, Ollama must be running
go run ./cmd/llmbot -backend typesafe -seed 7   # hosted Jev, needs TYPESAFE_API_KEY
go run ./cmd/llmbot -backend anthropic -seed 7  # Claude Haiku, needs ANTHROPIC_API_KEY
```

The hosted backends send the game state to their provider on every decision. Setup, flags and how the bot asks the model are in [Let a model drive](docs/llmbot.md).

The game is also a test bench: three decision models (Jev, its local version Tev, and Nimble) and two general-purpose LLMs (Gemma and Claude Haiku) were given the same question. In short:

- **The harness decides more than the model.** Given the raw obstacle list no model got past the first few rows. Given one sentence worked out in Go, such as `Lane to the left: blocked soon. Current lane: clear. Lane to the right: wall.`, they drive.
- **Speed and judgment are separate.** The fastest model, `tev1:0.8b`, almost never moves. Haiku is the slowest by far and still got about as far on seed 7 as Jev usually does.
- **Nimble, warmed up, and the new `tev1:4b` with a confidence gate drove furthest**: both passed 3,000 ticks on every seed they were run on. The new `tev1:4b` moves into a blocked lane when no lane is open, but only when it is unsure, so `-min-confidence 0.3` holds those moves back. A decision model needs less code around the call than a general-purpose LLM, which answers in text that has to be pinned down and parsed.

The comparison table and the requests side by side are in [Four kinds of model at the wheel](docs/models.md), and every run behind it in [Model runs](docs/model-runs.md). It is a quick look, not a benchmark: one run per figure.

## Write your own bot

Any program that speaks HTTP or WebSocket can drive:

```sh
curl -s localhost:8765/state
curl -s -X POST localhost:8765/control/claim -d '{"name":"mybot"}'
curl -s -X POST localhost:8765/input         -d '{"action":"left","name":"mybot"}'
```

Start with [Write your own bot](docs/write-a-bot.md); the full contract is in [docs/API.md](docs/API.md).

## Develop

```sh
go test ./...          # a few seconds
go test -race ./...    # about a minute
go vet ./...
```

| Path | Contents |
|------|----------|
| `cmd/outrun` | The game server binary |
| `cmd/bot` | The sample bot |
| `cmd/llmbot` | The bot that lets a model drive |
| `internal/game` | The headless core: rules, spawner, observation, reference solver. No networking and no clock |
| `internal/server` | HTTP and WebSocket API, the real-time loop, who is driving |
| `web` | The viewer (HTML canvas), embedded into the binary |
| `docs` | The API contract and the pages linked above |
| `docs/planning` | Plans, decisions and progress logs from building it |

The viewer is embedded at build time, so restart the server after editing anything in `web/`.

The difficulty settings (speeds, row spacing, how often a row leaves a single open lane) are in `DefaultConfig` in `internal/game/game.go`.
