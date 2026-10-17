# Four kinds of model at the wheel

The game is also a test bench. `cmd/llmbot` lets a model drive, and the same question was put to two decision models (Jev, hosted, and its local version Tev in two sizes) and two general-purpose LLMs (Gemma, local, and Claude Haiku, hosted).

How it works:

- **The game does not wait.** It ticks 30 times a second. The bot asks the model one question, `left`, `none` or `right`, and sends the answer when it comes.
- **The model judges, the harness calculates.** The model is not shown distances and speeds but one sentence worked out in Go, such as `Lane to the left: blocked soon. Current lane: clear. Lane to the right: wall.`
- **The harness allows for the model's answer time.** The sentence describes the road as it will be when the answer arrives, and a move is checked against the newest state before it is sent. A late answer is still used if its lane has not got worse.

| | `jev-1.13.0` | `tev1:4b` | `tev1:0.8b` | `gemma4:e2b` | `claude-haiku-4-5` |
|---|---|---|---|---|---|
| Kind | Decision model | Decision model | Decision model | General-purpose LLM | General-purpose LLM |
| Runs | Hosted (TypeSafe) | Local (Ollama) | Local (Ollama) | Local (Ollama) | Hosted (Anthropic) |
| How it is asked | State and a typed choice | State and a typed choice | State and a typed choice | Chat message, pinned to a move by a JSON schema, temperature 0, thinking off | Chat message, pinned to a move by a JSON schema, temperature 0 |
| Reports a confidence | Yes | Yes | Yes | No | No |
| Median latency | 250 to 340 ms, the same for every prompt | 55 ms on a repeated sentence, about 450 ms on a new one | 21 ms | about 250 ms | about 810 ms |
| Seed 7, full pace, ticks survived | 1,735; 3,202 in a later run (2,999 with three questions open) | 357 | 357 | 318 | 1,594 (764 with two or three questions open) |
| Seeds 1 to 5, full pace, median ticks | 1,841 | 2,710 | 180 | 291 | not run |
| Memory or cost | none locally; 1 to 2 cents a run at most | about 4.7 GB | about 1.0 GB | about 5.0 GB | none locally; under 2 cents a run |

Ticks survived are single runs with the current harness and one question open at a time; 2,700 ticks is 90 seconds of game time.

The same question, asked both ways. A decision model (Jev, Tev) takes the state, the question and the options as separate fields:

```json
{
  "model": "jev-1.13.0",
  "state": "Lane to the left: blocked soon. Current lane: clear. Lane to the right: wall.",
  "questions": {"move": {
    "type": "choice",
    "instructions": "Which move keeps the car on a clear lane? Stay if the current lane is clear. Otherwise move to a clear lane, or towards a clear lane beyond.",
    "criteria": {
      "left": "move one lane to the left",
      "none": "stay in the current lane",
      "right": "move one lane to the right"}}}
}
```

and answers with one of the options, how likely each was and how sure it is. The numbers here are an example:

```json
{"answers": {"move": {
  "choice": "none",
  "probabilities": {"left": 0.01, "none": 0.98, "right": 0.01},
  "confidence": 0.98}}}
```

A general-purpose LLM (Claude Haiku here, Gemma likewise on Ollama's `/api/chat`) gets the same three parts as one chat message, and the request has to fence the answer in:

```json
{
  "model": "claude-haiku-4-5",
  "max_tokens": 64,
  "temperature": 0,
  "messages": [{"role": "user", "content": "State: Lane to the left: blocked soon. Current lane: clear. Lane to the right: wall.\n\nWhich move keeps the car on a clear lane? Stay if the current lane is clear. Otherwise move to a clear lane, or towards a clear lane beyond.\n- left: move one lane to the left\n- none: stay in the current lane\n- right: move one lane to the right"}],
  "output_config": {"format": {"type": "json_schema", "schema": {
    "type": "object",
    "properties": {"move": {"type": "string", "enum": ["left", "none", "right"]}},
    "required": ["move"],
    "additionalProperties": false}}}
}
```

It answers in text, which holds the move as JSON and has to be parsed a second time. There is no probability and no confidence:

```json
{"content": [{"type": "text", "text": "{\"move\": \"none\"}"}], "stop_reason": "end_turn"}
```

What stands out:

- **The harness decides more than the model.** On the raw obstacle list no model got past the first few rows. With the digested sentence, the look-ahead and late moves, even Haiku at 0.8 seconds an answer drove for 1,594 ticks.
- **Speed and judgment are separate.** `tev1:0.8b` is the only model fast enough for every tick and almost never moves. Haiku is the slowest by far and still got about as far on seed 7 as Jev usually does.
- **`tev1:4b` judges best overall.** It leads on four of five seeds and passed 2,700 ticks on three. Seed 7 is its bad seed: one answer to a new sentence takes 13 to 14 ticks, and there that one tick decides.
- **Jev judges like `tev1:4b` and costs a round trip.** Its latency does not depend on the prompt, so asking it three questions at once helped on seed 7; it did not on the other seeds.
- **A decision model needs less harness around the call.** It takes a state and a typed question and returns an option with a confidence. Gemma and Haiku answer in text and have to be pinned to a move by a schema and parsed.
- **Gemma and `tev1:0.8b` stay under 410 ticks on every seed**, Gemma on wrong moves and late answers, `tev1:0.8b` by staying in lane.
- **More open questions are not a general fix.** They slow a local model, because the calls share one runner, and they made Haiku worse: asked more often, it moved when no lane was clear and steered into the wall.


This is a quick look, not a benchmark: one run per figure, and the same setup can give a different result on the next run. How the bot works is in [Let a model drive](llmbot.md), and the runs behind each figure are in [Model runs](model-runs.md).
