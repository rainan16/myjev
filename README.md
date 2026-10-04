# outrun

A small OutRun-style lane dodger with a local API. You drive a car down a four-lane road and switch lanes to avoid barriers and slower traffic. A person can play it in the browser, and a program can play it over HTTP or WebSocket while you watch.

It is one Go binary: the game runs headless inside it, and the browser page is just another client of the same API.

## Run

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

To build a binary you can copy anywhere (the viewer is embedded in it):

```sh
go build -o outrun ./cmd/outrun
./outrun
```

Flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-addr` | `127.0.0.1:8765` | Address to listen on. Must be a loopback address, because the API has no authentication |
| `-seed` | random | Seed of the first run. The same seed always gives the same obstacle layout |
| `-pace` | 1 | Speed of the game's clock, above 0 up to 1. At `-pace 0.25` every tick lasts four times as long, for you and for bots alike. The game is the same tick for tick, so scores stay comparable. The viewer shows the pace next to the seed |

## Watch a bot play

With the server running, in a second terminal:

```sh
go run ./cmd/bot
```

The viewer shows `DRIVER gobot` and the car steers itself. After a crash the bot waits two seconds and starts a new run. Press **H** in the viewer to take over, or Ctrl-C in the bot's terminal; both hand the car back to you.

| Flag | Default | Meaning |
|------|---------|---------|
| `-addr` | `127.0.0.1:8765` | Address of the game server |
| `-name` | `gobot` | Name to drive under |
| `-seed` | random | Use this seed for every run |
| `-runs` | 0 | Stop after this many runs (0 = keep playing) |
| `-pause` | `2s` | Wait after a crash before restarting |

## Let a local model drive

`cmd/llmbot` hands the wheel to a model running in [Ollama](https://ollama.com). It was written to see, hands-on, how a decision model (Tev, `tev1:0.8b` and `tev1:4b`) and a general-purpose LLM (`gemma4:e2b`) differ when a decision is needed 30 times a second. It is a quick look, not a benchmark.

With the server and Ollama running:

```sh
go run ./cmd/llmbot -model tev1:4b -seed 7
```

The game does not wait for the model. One question is open at a time, always about the newest state, and an answer that arrives too late is dropped. A move is checked once more before it is sent: if the lane it leads to reads worse on the newest state than on the one the model was asked about, the move is held and the model is asked again. The viewer shows the model's name as the driver and, under it, how long the last answer took and how sure the model was. To give a slow model a chance, slow the game's clock instead: `go run ./cmd/outrun -pace 0.5`.

| Flag | Default | Meaning |
|------|---------|---------|
| `-model` | `tev1:0.8b`, or `jev-1.13.0` on `typesafe` | Model that drives |
| `-state` | `digested` | What the model is shown: `digested` or `raw` (see below) |
| `-max-stale` | 6 | Drop answers older than this many ticks (-1 keeps all) |
| `-min-confidence` | 0 | Send no move the model is less sure of than this. Only decision models report a confidence |
| `-api` | `auto` | `systemone` or `chat`; `auto` asks Ollama whether the model is a decision model |
| `-warm` | off | Ask every digested state once before driving |
| `-v` | off | Log every decision, not only the changes |
| `-ollama` | `http://127.0.0.1:11434` | Base URL of Ollama |
| `-backend` | `ollama` | Where the model runs: `ollama`, or `typesafe` for hosted Jev (see below) |
| `-addr`, `-name`, `-seed`, `-runs`, `-pause` | | As for the sample bot; the name defaults to the model name |

At the end of each run it prints ticks survived, rows passed, score, decisions per second, latency, how many answers were dropped as stale or held because the lane got worse, and the memory of Ollama's model runner.

### Hosted Jev

Jev is the hosted decision model that Tev is the local version of. It takes the same call as Tev, so the bot only needs to be pointed at it:

```sh
export TYPESAFE_API_KEY=...   # from https://console.typesafe.ai/keys; or put that line, without export, in a .env file here
go run ./cmd/llmbot -backend typesafe -seed 7
```

This mode sends the game state (the digested sentence, or the raw obstacle list) to `api.typesafe.ai` on every decision. Nothing else leaves the machine, and the game server stays loopback only: it is the bot that makes the outbound call. The key is read from the environment, or from a `.env` file in the directory the bot is started in (`.env` is in `.gitignore`). It is sent only to that host and never logged.

A call that gets no reply within 2 seconds is given up and counted with the unusable replies. A busy reply (429 or 529) is tried again up to three times with a growing wait, and that wait counts as latency. The run summary reports the input tokens used and the retries, and no memory, because there is no local runner to measure.

One run each on seed 7 (2026-10-04), next to the local models on the same server and the same day. 2,700 ticks counts as surviving, and the runs that got there were stopped shortly after:

| Model | Pace 0.5 | Pace 1.0 | Median latency | Decisions per second at pace 0.5 | Memory |
|-------|----------|----------|----------------|----------------------|--------|
| `jev-1.13.0` (hosted) | survived, 70 rows | 255, all answers stale | 247 ms | 3.8 | none locally |
| `tev1:4b` | survived, 71 rows | 357 | 55 ms, about 420 ms when the sentence changes | 12.3 | about 4.7 GB |
| `gemma4:e2b` | 1,181 | 255, all answers stale | 245 ms | 3.8 | about 5.0 GB |
| `tev1:0.8b` | 357 | 357 | 25 ms | 14.9 | about 1.0 GB |

- **Jev judges like `tev1:4b` and is as slow as Gemma.** On the 72 possible sentences it gave the expected move in all 56 that have one, as `tev1:4b` did, with a median confidence of 0.98 against 0.84. With the options in reverse order the scores were the same and the count of each move differed by one in 72.
- **Its time is the round trip, and it is the same for every prompt.** About 250 ms whether the sentence is new or the same one 30 times over. `tev1:4b` is five times faster on a repeated sentence and slower on a new one.
- **So at full pace it never steers.** 250 ms is 7 to 9 ticks, over the stale limit of 6, exactly as with Gemma. At pace 0.5 its answers were 3 to 4 ticks old and 3 of 699 were dropped.
- **A run costs about a cent.** A call is 379 input tokens; the surviving run used 264,722, which at $0.042 per million is $0.011.
- `jev-latest`, `jev-preview` and `jev-1.13.0` all answered as `jev-1.13.0` on that day.

#### One question per lane: tried, and taken out

The TypeSafe docs suggest asking many narrow questions in one call. That was tried as a third state form: the raw distances and speeds grouped by lane, one yes/no question per lane (`Is lanes.lane_2 free of obstacles the car will reach soon?`), and the rule in Go instead of in the instruction: stay on a lane the model calls clear, otherwise head for the nearest lane it calls clear. The harness calculates nothing for the prompt.

| Jev, seed 7, pace 0.5 | 110 probe states, move right | Driving | Input tokens |
|---|---|---|---|
| Digested sentence, one Choice | 110 | passed 2,700 | 264,722 |
| Raw list, one Choice | 67 | not run | |
| One yes/no per lane, nearest clear lane | 104 | crashed at 2,570 | 416,524 |
| One yes/no per lane, clear neighbour only | 100 | crashed at 764 | |
| Two yes/no per lane (clear, blocked now) | 103 | crashed at 2,570 | 704,948 |

- **Narrow questions rescue most of the raw state**: 104 against 67. Jev reads "clear or not" off the numbers of one lane right about nine times in ten.
- **Extra questions cost tokens and no time**: 1, 4, 12 and 40 questions in one call all took 250 to 310 ms.
- **It still does not drive as well as the digest.** What is missing is how soon a lane is blocked: a lane blocked in a while can be crossed, one blocked now cannot. A stricter rule got the car stuck, and a second question for "blocked now" found only half of those lanes. The digest answers that with arithmetic in Go.

So the bot is back to the digested sentence and the fan-out is not in the code. One run per row.

Single runs, as everywhere in this file. The memory column is the freshly loaded figure from the table further down, not from these runs.

### Two ways to ask

A decision model takes a state and a typed question and returns one of the options with probabilities. There is nothing to parse:

```sh
curl -s localhost:11434/v1/systemone -d '{
  "model": "tev1:4b",
  "state": "Lane to the left: blocked soon. Current lane: clear. Lane to the right: wall.",
  "questions": {"move": {
    "type": "choice",
    "instructions": "Which move keeps the car on a clear lane? Stay if the current lane is clear. Otherwise move to a clear lane, or towards a clear lane beyond.",
    "criteria": {
      "left": "move one lane to the left",
      "none": "stay in the current lane",
      "right": "move one lane to the right"}}}
}'
# {"answers": {"move": {"choice": "none", "probabilities": {...}, "confidence": 0.98}}}
```

A general-purpose model answers in text, so the harness has to pin it down to a move:

```sh
curl -s localhost:11434/api/chat -d '{
  "model": "gemma4:e2b",
  "stream": false,
  "think": false,
  "options": {"temperature": 0},
  "format": {"type": "object", "required": ["move"],
    "properties": {"move": {"type": "string", "enum": ["left", "none", "right"]}}},
  "messages": [{"role": "user", "content": "State: Lane to the left: blocked soon. Current lane: clear. Lane to the right: wall.\n\nWhich move keeps the car on a clear lane? ...\n- left: move one lane to the left\n- none: stay in the current lane\n- right: move one lane to the right"}]
}'
# {"message": {"content": "{\"move\": \"none\"}"}}
```

What Gemma needed and Tev did not: thinking switched off (out of the box one answer took about 15 seconds and came as a paragraph), temperature 0, a JSON schema with the three moves as an enum, and a parser for the reply. Asking for a single token instead returned an empty string. In return Gemma gives no confidence. Each model works only on its own endpoint: Tev called on `/api/chat` thought for 21 seconds and returned nothing, and Gemma is refused on `/v1/systemone`.

### The model judges, the harness calculates

`-state raw` gives the model the car's lane and speed and the nearest eight obstacles with lane, distance and speed. Working out which lane is blocked when takes arithmetic, and none of the three models did it: on seven probe states all three answered `none` every time.

`-state digested` does the arithmetic in Go and gives the model one sentence about the lane each move leads to, such as `Lane to the left: blocked soon. Current lane: clear. Lane to the right: wall.` The words are `clear`, `blocked soon`, `blocked now`, `wall`, and `blocked soon, clear lane beyond`. A lane is `clear` when it stays free for 60 ticks, or when it outlasts the nearest blockage on the road by at least 12 ticks: at speed the next row is always in sight, and the lane to head for is the one that is free the longest. A lane only counts as leading to a clear lane beyond if it stays free long enough for this model to cross it: each further lane change costs as many ticks as the model's slowest usable answer so far, plus one. Numbers (ticks until blocked) worked no better than the raw state; words did.

### What was seen

One run per cell on seed 7, on an M2 Pro with 32 GB, each model loaded alone. A cell shows the ticks survived; 2,700 ticks (90 seconds of game time) counts as surviving. When these runs were made the car started at 40 and the speed stopped rising at 90 there; the start is now 50 and there is no limit, so seed 7 gives a different road today. The first row fills lane 2, where the car starts, at tick 311.

| Model | State | Pace 1.0 | Pace 0.5 | Pace 0.25 | Median latency |
|-------|-------|----------|----------|-----------|----------------|
| `tev1:0.8b` | raw | 311 | 311 | 311 | 115 ms |
| `tev1:0.8b` | digested | 432 | 432 | 432 | 20 ms |
| `tev1:4b` | raw | 311, all answers stale | 311, 95% stale | 704 | 555 ms |
| `tev1:4b` | digested | 1,926 | 836 | survived | 50 ms, 410 ms when the sentence changes |
| `gemma4:e2b` | raw | 311, all answers stale | 246 | 247 | 415 ms |
| `gemma4:e2b` | digested | 311, all answers stale | 1,347 | 1,347 | 236 ms |

| Model | Memory of the model runner, freshly loaded | What `/api/ps` reports |
|-------|-------------------------------------------|------------------------|
| `tev1:0.8b` (Q8) | about 1.0 GB | 852 MB |
| `tev1:4b` (Q8) | about 4.7 GB | 4,468 MB |
| `gemma4:e2b` (Q4, with a vision projector) | about 5.0 GB | 364 MB |

What that shows:

- **The digested state is what makes a model drive.** On the raw state nobody got past the first few rows at any pace.
- **Speed and judgment are separate.** `tev1:0.8b` is the only model fast enough for every tick, and it crashes at the same tick at every pace: it almost never answers `left`, whatever the wording or the order of the options. Slowing the clock does not help a model that judges wrongly.
- **Gemma is too slow at full pace and steers at half pace.** At pace 1.0 every answer was 7 to 9 ticks old and was dropped, so it never steered. At 0.5 and 0.25 it passed 23 rows and crashed in the same place both times, on a wrong move.
- **`tev1:4b` judges best but is not uniformly fast.** It answers in about 50 ms when the prompt equals the previous one and in about 410 ms when it differs, and the sentence differs exactly when the situation changes. On the raw state, which changes every tick, it is the slowest of the three. It survived the whole ramp only at pace 0.25.
- **Confidence is informative.** For `tev1:0.8b` the median confidence was 0.55 on good answers and 0.20 on bad ones. One run of `tev1:4b` at pace 0.25 with `-min-confidence 0.5` held back 10 moves and crashed at tick 1,926, where the ungated run survived. Staying in lane is not a safe fallback in this game, so a gate can turn a doubtful move into a crash by inaction.

After those runs the harness and the game were changed (see below), and seed 7 is a different road now. One run each on the new road at pace 0.5:

| Model | Ticks | Rows passed | Median latency | How it ended |
|-------|-------|-------------|----------------|--------------|
| `tev1:0.8b` | 357 | 5 | 27 ms | Stayed in a blocked lane with the way out on its left |
| `gemma4:e2b` | 767 | 14 | 253 ms | Moved into a lane that read `blocked now` |
| `tev1:4b` | 2,763, stopped | 71 | 55 ms | Still driving at speed 101 |

How far to trust it: every cell is a single run. The first table was made before three changes to the harness: a move whose lane got worse is now held, `clear lane beyond` now allows for the model's answer time, and `clear` is now relative to the nearest blockage. Both `tev1:4b` and `gemma4:e2b` crashed at tick 836 at pace 0.5 on a move those changes prevent, so the cells of the first table say more about the harness than about the models. The car now also starts at 50 and has no speed limit, which moves every row, so the two tables are not the same road. The same model, seed and pace gave different results on different runs (`tev1:4b`, digested, pace 1.0: 387, 1,926 and 1,956 ticks), because the outcome depends on which tick an answer happens to land on. That is also why pace 0.5 looks worse than pace 1.0 for `tev1:4b`. Latency and memory are the dependable numbers. The memory figures compare different quantizations, and a runner grows while it works: `tev1:4b` stood at 6.9 GB after a run.

## Write your own bot

The full contract is in [docs/API.md](docs/API.md). The short version:

```sh
# What the game looks like right now
curl -s localhost:8765/state

# Take the car, steer, give it back
curl -s -X POST localhost:8765/control/claim   -d '{"name":"mybot"}'
curl -s -X POST localhost:8765/input           -d '{"action":"left","name":"mybot"}'
curl -s -X POST localhost:8765/control/release -d '{"name":"mybot"}'

# Start a new run, optionally with a fixed seed
curl -s -X POST localhost:8765/reset -d '{"seed":42}'
```

For real-time play, connect to `ws://127.0.0.1:8765/ws`. The server pushes the state 30 times a second (slower if it was started with a lower `-pace`), and you send:

```json
{"type": "claim", "name": "mybot"}
{"type": "input", "action": "left"}
{"type": "status", "text": "thinking"}
{"type": "reset"}
{"type": "release"}
```

Things worth knowing before you start:

- **Actions** are `left`, `right` and `none`. One action moves one lane, and at most one is applied per tick.
- **The state** lists every obstacle within view with its lane, distance ahead, length and speed, plus a 4 x 12 lookahead grid for simpler bots.
- **One driver at a time.** Input from anyone who is not the controller is rejected.
- **Status text.** The driver can publish one line about itself (up to 80 printable characters). The viewer shows it under the driver's name, and it is cleared when the driver changes.
- **The game is real time.** A move sent in reply to one state lands on the next tick, or a tick later if the message is slow. Seeds fix the layout, not the outcome.
- **Every layout can be survived.** A row never blocks all lanes, and there is always time to reach the open one.

`cmd/bot/main.go` is a complete example in about 250 lines, and `internal/game/solver.go` is the strategy it uses.

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
| `internal/game` | The headless core: rules, spawner, observation, reference solver. No networking and no clock |
| `internal/server` | HTTP and WebSocket API, the real-time loop, who is driving |
| `web` | The viewer (HTML canvas), embedded into the binary |
| `docs/API.md` | The API contract |

The viewer is embedded at build time, so restart the server after editing anything in `web/`.

The difficulty settings (speeds, row spacing, how often a row leaves a single open lane) are in `DefaultConfig` in `internal/game/game.go`.
