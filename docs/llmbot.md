# Let a model drive

`cmd/llmbot` hands the wheel to a model running in [Ollama](https://ollama.com), or to a hosted one: [Claude Haiku](#claude-haiku) or [Jev](#hosted-jev). It was written to see, hands-on, how a decision model (Tev, `tev1:0.8b` and `tev1:4b`) and a general-purpose LLM (`gemma4:e2b`) differ when a decision is needed 30 times a second. It is a quick look, not a benchmark.

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

## Claude Haiku

Claude Haiku 4.5 is a general-purpose LLM like Gemma, but hosted, so nothing has to be loaded locally:

```sh
export ANTHROPIC_API_KEY=...   # or put that line, without export, in a .env file here
go run ./cmd/llmbot -backend anthropic -seed 7
```

It is called on `POST /v1/messages` with the same user message as Gemma and pinned down the same way: temperature 0 and a JSON schema (`output_config.format`) that leaves room for nothing but the move. Thinking is off on this model unless asked for. Like Gemma it reports no confidence, so `-min-confidence` does nothing.

A key that is not tied to a workspace is refused with a 400 unless the workspace is named: set `ANTHROPIC_WORKSPACE_ID` the same way as the key.

This mode sends the game state to `api.anthropic.com` on every decision; the key is read and guarded as for hosted Jev below, and the timeout, the retries on a busy reply and the summary are the same. Another Claude model can be named with `-model`, but the newer ones refuse `temperature` and think on every call, so the request would need changing for them.

The runs are in [Model runs](model-runs.md#claude-haiku).

## Hosted Jev

Jev is the hosted decision model that Tev is the local version of. It takes the same call as Tev, so the bot only needs to be pointed at it:

```sh
export TYPESAFE_API_KEY=...   # from https://console.typesafe.ai/keys; or put that line, without export, in a .env file here
go run ./cmd/llmbot -backend typesafe -seed 7
```

This mode sends the game state (the digested sentence, or the raw obstacle list) to `api.typesafe.ai` on every decision. Nothing else leaves the machine, and the game server stays loopback only: it is the bot that makes the outbound call. The key is read from the environment, or from a `.env` file in the directory the bot is started in (`.env` is in `.gitignore`). It is sent only to that host and never logged.

A call that gets no reply within 2 seconds is given up and counted with the unusable replies. A busy reply (429 or 529) is tried again up to three times with a growing wait, and that wait counts as latency. The run summary reports the input tokens used and the retries, and no memory, because there is no local runner to measure.

The runs are in [Model runs](model-runs.md#hosted-jev).

## Two ways to ask

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

## The model judges, the harness calculates

`-state raw` gives the model the car's lane and speed and the nearest eight obstacles with lane, distance and speed. Working out which lane is blocked when takes arithmetic, and none of the three models did it: on seven probe states all three answered `none` every time.

`-state digested` does the arithmetic in Go and gives the model one sentence about the lane each move leads to, such as `Lane to the left: blocked soon. Current lane: clear. Lane to the right: wall.` The words are `clear`, `blocked soon`, `blocked now`, `wall`, and `blocked soon, clear lane beyond`. A lane is `clear` when it stays free for 60 ticks, or when it outlasts the nearest blockage on the road by at least 12 ticks: at speed the next row is always in sight, and the lane to head for is the one that is free the longest. A lane only counts as leading to a clear lane beyond if it stays free long enough for this model to cross it: each further lane change costs as many ticks as the model's slowest usable answer so far, plus one. Numbers (ticks until blocked) worked no better than the raw state; words did.
