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

The game does not wait for the model. One question is open at a time, unless `-questions` allows more. On the digested state the model is not asked about the road as it is but as it will be when the answer arrives: the bot knows how old its model's answers usually are and moves every obstacle that many ticks on. A move is checked once more before it is sent: if the lane it leads to reads worse on the newest state than on the one the model was asked about, the move is held and the model is asked again. An answer is not dropped for its age alone; a late move that passes that check still goes out. The viewer shows the model's name as the driver and, under it, how long the last answer took and how sure the model was. To give a slow model a chance, slow the game's clock instead: `go run ./cmd/outrun -pace 0.5`.

| Flag | Default | Meaning |
|------|---------|---------|
| `-model` | `tev1:0.8b`, `jev-1.13.0` on `typesafe`, `claude-haiku-4-5` on `anthropic` | Model that drives |
| `-state` | `digested` | What the model is shown: `digested` or `raw` (see below) |
| `-max-stale` | 6 | Count answers older than this many ticks as late (-1 counts none). It no longer drops them |
| `-look-ahead` | -1 | Ticks ahead of now that the digested state describes: -1 follows the age of the model's answers, 0 turns it off |
| `-questions` | 1 | Questions open at a time. Above 1 they are spread over the time one answer takes, so the model is asked more often; each is one more call. Only one answer can move the car until that move shows up |
| `-min-confidence` | 0 | Send no move the model is less sure of than this. Only decision models report a confidence |
| `-api` | `auto` | `systemone` or `chat`; `auto` asks Ollama whether the model is a decision model. Only for `ollama`; the hosted backends set it themselves |
| `-warm` | off | Ask every digested state once before driving |
| `-v` | off | Log every decision, not only the changes |
| `-ollama` | `http://127.0.0.1:11434` | Base URL of Ollama |
| `-backend` | `ollama` | Where the model runs: `ollama`, `typesafe` for hosted Jev, or `anthropic` for Claude Haiku (see below) |
| `-addr`, `-name`, `-seed`, `-runs`, `-pause` | | As for the sample bot; the name defaults to the model name |

At the end of each run it prints ticks survived, rows passed, score, decisions per second, latency, how many answers were late or held because the lane got worse, and the memory of Ollama's model runner.

### Claude Haiku

Claude Haiku 4.5 is a general-purpose LLM like Gemma, but hosted, so nothing has to be loaded locally:

```sh
export ANTHROPIC_API_KEY=...   # or put that line, without export, in a .env file here
go run ./cmd/llmbot -backend anthropic -seed 7
```

It is called on `POST /v1/messages` with the same user message as Gemma and pinned down the same way: temperature 0 and a JSON schema (`output_config.format`) that leaves room for nothing but the move. Thinking is off on this model unless asked for. Like Gemma it reports no confidence, so `-min-confidence` does nothing.

A key that is not tied to a workspace is refused with a 400 unless the workspace is named: set `ANTHROPIC_WORKSPACE_ID` the same way as the key.

This mode sends the game state to `api.anthropic.com` on every decision; the key is read and guarded as for hosted Jev below, and the timeout, the retries on a busy reply and the summary are the same. Another Claude model can be named with `-model`, but the newer ones refuse `temperature` and think on every call, so the request would need changing for them.

One run on seed 7 at pace 1.0 (2026-10-04): crashed at tick 1,594 with 33 rows passed and a score of 3,441. It answered 62 times, 1.2 per second, median 813 ms, so every answer was late (21 to 32 ticks old); 10 were moves and all 10 were still sent, none unusable. 15,880 input tokens, under $0.02. That is the tick `tev1:4b` crashed at on the same seed and pace; Gemma got to 318 and Jev to 1,735. A single run, and the same setup varies from run to run.

With more questions open it did worse on that seed: `-questions 2` and `-questions 3` both crashed at tick 764 (14 rows, score 1,453), at 2.2 and 3.3 answers per second. Asked around tick 695 on `left: blocked soon, current: blocked soon, right: blocked now` it answered `left`, and that move went out; the `right` towards a clear lane that came a few ticks later was not sent because the car was already changing lane. In the left-most lane it then answered `left` again, into the wall. With one question that earlier sentence was never asked.

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

#### Looking ahead, and sending late moves

Two changes to the harness made after the runs above, for every model: the digested state is moved ahead to when the answer will arrive, and a late move is sent if its lane has not got worse. Before, every answer older than 6 ticks was dropped. One run each on seed 7:

| Model | Pace 1.0 before | Pace 1.0 after | Pace 0.5 after |
|-------|-----------------|----------------|----------------|
| `jev-1.13.0` | 255, never steered | 1,735, 37 rows, 16 moves | passed 2,700 as before |
| `tev1:4b` | 357 | 357 | passed 2,700 as before |
| `gemma4:e2b` | 255, never steered | 255 | not run again |

- **Jev now drives at full pace.** 98% of its answers were late by the old rule, and 15 of the 16 moves it made were. It crashed at tick 1,735 at speed 82, where a row leaves one lane open two lanes away and comes into sight 21 ticks ahead: two answers of 8 to 11 ticks do not fit.
- **`tev1:4b` gained nothing at full pace.** Its answer to a new sentence takes 13 ticks, the bot allows for that when it says whether a clear lane beyond can still be reached, and so it was told `blocked soon` and stayed.
- **Gemma still did not steer**, now by judgment: with `left: clear, current: blocked soon, right: clear` it answered `none`.

A third change followed. On the state moved ahead, the first move no longer costs the model's answer time when the bot works out whether a clear lane beyond can be reached: by then the answer is there and the move only has to land. Before, that time was counted twice, once in moving the state ahead and once in the sentence. One run each on seed 7 at pace 1.0; pace 0.5 and Gemma were not run again:

| Model | Before | After |
|-------|--------|-------|
| `jev-1.13.0` | 1,735, 37 rows, 16 moves | 1,735, 37 rows, 18 moves |
| `tev1:4b` | 357, 1 move | 1,594, 33 rows, 12 moves |

- **`tev1:4b` got further once, and not again.** It was told `clear lane beyond` and took it, and crashed at tick 1,594 at speed 80. Three more runs with the same code crashed at 357, as did four after the later change to the check before sending: see the last table of this section. Its answer to a new sentence takes about 450 ms, which is 13 ticks in one run and 14 in the next, and around tick 325 that one tick decides whether both moves get out.
- **Jev crashed on the same row.** The sentence would have read `clear lane beyond` for a question asked between ticks 1,705 and 1,711. Jev's questions went out at 1,704 and 1,713, one answer apart. What is left is how often it is asked, not what it is told.

So the fourth change lets more than one question be open: `-questions 3` asks Jev every 3 ticks, not every 7 to 9. While a move is on its way, a second answer cannot move the car again. The default stays at 1. One run each on seed 7 at pace 1.0:

| Model | One question | Several |
|-------|--------------|---------|
| `jev-1.13.0`, `-questions 3` | 1,735, 37 rows, 18 moves | 2,999, 78 rows, 44 moves |
| `jev-1.13.0`, `-questions 2` | | 1,735, 37 rows, 18 moves |
| `tev1:4b`, `-questions 2` | 1,594, 33 rows, 12 moves | 357, 5 rows, 1 move |

- **Jev passed 2,700 ticks at full pace.** On the row at 1,735 it was asked at tick 1,706, told `clear lane beyond`, and made both moves. It crashed at tick 2,999 at speed 106, after a `left` at confidence 0.58 into a lane that read `blocked soon`. The run used 367,083 input tokens, about $0.015, against 74,602 with one question.
- **Two questions were not enough for Jev in the one run made.** It was asked at tick 1,707 and answered `right` towards the clear lane beyond, but the answer was 9 ticks old and the move was held: by then the room left no longer covered a second answer as slow as its slowest recent one. The service was also slower in that run, 302 ms median against 262.
- **It does not help a local model.** Two calls at once share the one runner: `tev1:4b`'s median went from 55 to 102 ms and a new sentence took 15 ticks, not 13.

The fifth change is in the check before a move is sent. It allowed for a second answer as slow as the slowest of the last 9; it now goes by the median. The sentence the model reads still allows for the slowest. Jev on seed 7 at pace 1.0, one run each, the three-question run made twice because the service stalled in the first:

| `-questions` | Check by the slowest answer | Check by the median |
|--------------|-----------------------------|---------------------|
| 1 | 1,735, 37 rows | 1,735, 37 rows |
| 2 | 1,735, 37 rows | 2,570, 63 rows |
| 3 | 2,999, 78 rows | 2,999, 78 rows; before that 1,355 with three calls unanswered for 2 s |

- **With two questions the held move now goes out**, and Jev passed the row at 1,735. It crashed at 2,570 with a `right` towards a clear lane beyond held, 6 ticks old.
- **With one question nothing changed.** It was asked at tick 1,706, inside the window, but one answer shortly before had been 15 ticks old, so the sentence read `blocked soon`.
- **A stall of the service ends a run.** Three open calls without a reply are 60 ticks without a question.

All four models once more with every change above in, seed 7, pace 1.0, one question at a time, each local model alone in Ollama:

| Model | Result | Moves | Median latency |
|-------|--------|-------|----------------|
| `jev-1.13.0` | 1,735, 37 rows (2,999 with `-questions 3`) | 16 | 321 ms |
| `tev1:4b` | 357, 5 rows, in 4 of 4 runs | 2 | 55 ms, about 450 ms on a new sentence |
| `tev1:0.8b` | 357, 5 rows | 1 | 21 ms |
| `gemma4:e2b` | 318, 4 rows | 2 | 257 ms |

- **Gemma steered this time**, twice, with answers 11 ticks old. Its last move was a `left` into a lane that read `blocked now`.
- **`tev1:0.8b` is unchanged**: 338 answers, none late, one move.
- **`tev1:4b` with the check on the slowest answer again** also crashed at 357 in 3 of 3 runs, with the first move held where the median lets it through and holds the second. So the 1,594 above was the exception, and the change to the median is not what cost it.

#### Five more seeds

Everything above is seed 7. The same harness on seeds 1 to 5 at pace 1.0, one run per cell, ticks survived, each run stopped after 100 seconds (3,000 ticks):

| Seed | Jev, `-questions 3` | Jev, one question | `tev1:4b` | `gemma4:e2b` | `tev1:0.8b` |
|------|------|------|------|------|------|
| 1 | 1,536 | 1,841 | 1,578 | 347 | 257 |
| 2 | 1,360 | 1,360 | 1,863 | 291 | 341 |
| 3 | 1,915 | 1,915 | 2,992, still driving | 265 | 153 |
| 4 | 2,573 | 2,573 | 2,710 | 215 | 154 |
| 5 | 1,956 | 830 | 2,992, still driving | 404 | 180 |
| Median | 1,915 | 1,841 | 2,710 | 291 | 180 |

- **`tev1:4b` is ahead of Jev on four of the five seeds** and passed 2,700 ticks on three. Seed 7, where it crashes at 357, is its bad seed, not the rule.
- **Three open questions did not help Jev here as they did on seed 7.** Same crash tick on three seeds, better on one, worse on one. On seed 11 Jev with three questions crashed at tick 1,452 in four runs of four.
- **Jev was slower than in the seed 7 runs**: median 302 to 336 ms against 262, so its answers were about two ticks older.
- **Gemma and `tev1:0.8b` stay under 410 ticks on every seed.**

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
