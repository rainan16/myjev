# Intent, Decisions and Open Questions: Tev versus Gemma driving the OutRun game

_Generated: 2026-10-04 12:34, revised 2026-10-04 after reading the Jev articles and probing the local models, then for a speed-scale decision, which was replaced the same day by a clock pace_

---

## Refined Intent

Let three local Ollama models drive the existing OutRun lane dodger through its public API and watch how they behave: the decision models `tev1:0.8b` and `tev1:4b` (Tev, an open-weight model served through a Jev-compatible API) against the general-purpose LLM `gemma4:e2b`.

The purpose is learning, not benchmarking. The user wants to see, hands-on, how a "System One" decision model is called and how it compares with a traditional LLM on **RAM footprint** and **decision speed** in a real-time task, and what the harness has to do for each. The central lesson to observe: a decision model judges, it does not calculate, so the same model is run on a raw state and on a state the harness has digested.

---

## What the sources and the probe established

Sources: the LangChain post "Building a harness with Jev", the Medium guide (PDF in `docs/`), a news summary of Ollama 0.35, and direct calls to the local Ollama 0.35.1 on the M2 Pro (32 GB).

**About Jev (hosted, TypeSafe AI)**

- A "System One" model: it takes a `state` and typed `questions` and returns typed values with probabilities in one parallel pass. It generates no text.
- Three question types: `choice` (up to 255 options), `score` (2 to 10 ordered levels), `noul` (yes/no probability).
- `choice` and `score` answers carry a calibrated `confidence`. The guide calls it the most important field: act on it with thresholds that rise with the cost of being wrong.
- Documented limits: no text, no arithmetic, no date comparison, no explanation. It reads instructions literally. Accuracy drops when the state holds material the question does not need.
- Recommended pattern: "keep the loop, the safety and the arithmetic in ordinary code, and use Jev for the narrow judgment in the middle". The Mario example feeds compact object-centric JSON built by the harness; the drone example runs Jev at about 2.5 Hz as advisory only, with safety in code.
- Hosted Jev is waitlisted, so it is out of scope here.

**About Tev (local)**

- Made by Together AI, sizes 4B and 0.8B. Trained for `choice` only; the `noul` and `score` shapes come from Ollama's candidate scoring.
- Reported accuracy on 13 public datasets: 73.3% (4B), 63.5% (0.8B).
- Served on `POST /v1/systemone`. Limits: 64 questions and 64 KiB per request, no streaming. Local `num_ctx` is 2050.

**The working call**

```sh
curl -s localhost:11434/v1/systemone -d '{
  "model": "tev1:0.8b",
  "state": "{\"lane\":1,\"speed\":47.6,\"obstacles\":[...]}",
  "questions": {
    "move": {
      "type": "choice",
      "instructions": "Which move avoids hitting an obstacle?",
      "criteria": {
        "left":  "move one lane to the left",
        "none":  "stay in the current lane",
        "right": "move one lane to the right"
      }
    }
  }
}'
```

```json
{"model": "tev1:0.8b",
 "answers": {"move": {"type": "choice", "choice": "none",
   "probabilities": {"left": 0.075, "none": 0.809, "right": 0.116},
   "confidence": 0.439}},
 "usage": {"input_tokens": 204, "output_tokens": 1}}
```

`criteria` must be a map of option key to description (or null); a list is rejected. `gemma4:e2b` is rejected on this endpoint ("does not support decision"). Calling Tev through `/api/chat` is wrong: it thought for 21 s and returned nothing.

**Probe results on one game state** (car in lane 1, barrier ahead in lane 1, car ahead in lane 0; `right` is the only safe move)

| Model and setup | Answer | Warm latency | Repeatable | Process RAM |
|-----------------|--------|--------------|------------|-------------|
| `tev1:0.8b`, systemone, raw state | `none` (0.81), confidence 0.44. Wrong | about 21 ms | 6 of 6 identical to six decimals | 894 MB reported |
| `tev1:4b`, systemone, raw state | `left` (0.53), confidence 0.08. Wrong | about 52 ms | 6 of 6 identical | 4,686 MB reported |
| `gemma4:e2b`, JSON enum, thinking off, temp 0 | `right`. Correct | about 230 ms | 4 of 4 identical | 5,011 MB process RSS |
| `gemma4:e2b`, one-token letter | empty string | about 55 ms | | |
| `gemma4:e2b`, out of the box | `right`, inside a paragraph of prose | about 15 s | | |

One state is not evidence of general behaviour. It does match the documented limit: the raw state needs distance and speed arithmetic.

---

## Key Decisions

| # | Area | Decision | Status |
|---|------|----------|--------|
| 1 | Meaning of "memory" | RAM footprint of the loaded model | unchanged |
| 2 | Tev variants | Both `tev1:0.8b` and `tev1:4b` | unchanged |
| 3 | Baseline LLM | `gemma4:e2b` | unchanged |
| 4 | Timing | Real time; the game is unchanged, and the wall clock can be slowed by a server flag | revised three times |
| 5 | Choice per call | One `choice` question: `left`, `none`, `right` | revised: option keys, not letters |
| 6 | State given to the model | Both `raw` and `digested`, switched by a flag | revised |
| 7 | Gemma configuration | JSON enum output, thinking off, temperature 0 | revised |
| 8 | Determinism | No separate test | unchanged, but already observed in the probe |
| 9 | Call policy | One call at a time on the newest state; stale answers dropped | unchanged |
| 10 | Run protocol | One seed, a quick look, watched in the viewer | unchanged |
| 11 | Output | Terminal log and summary, plus status in the viewer HUD | unchanged |
| 12 | HUD data path | Generic `status` text sent by the current driver | unchanged |
| 13 | Learning approach | Probe first, then build | probe largely done |
| 14 | Confidence | Logged and shown; optional gate by flag, off by default | new |
| 15 | Call paths | Tev through `/v1/systemone`, Gemma through `/api/chat` | new, forced |
| 16 | Slow-down | Slow the clock (`-pace`) until a model can drive, and show the pace on the HUD; the pace each model needs is a finding | new, replaces the speed scale |

### Decisions 4 and 16: slow the clock, not the car

A server flag, for example `-pace 0.25`, stretches the real-time loop: a tick lasts `1 / (tick_rate × pace)` seconds, so 33 ms at pace 1.0 and 133 ms at 0.25. There is no step mode; the game still runs on its own clock and does not wait for the driver.

The game core is not touched. Speeds, spacing, `tick_rate` (30 game ticks per game second) and `DefaultConfig` stay as they are, and the same seed gives the same layout at any pace. Only the ticker interval in `internal/server` changes.

The user first chose to scale the car's speed and then replaced that with the pace, for these reasons:

- Every model plays the identical game, so a crash is either "too slow" or "wrong judgment", and the pace separates the two.
- Scores stay comparable across paces, because distance per tick does not change.
- A model's latency shrinks in ticks as the pace drops. Gemma's 230 ms is about 7 ticks at pace 1.0 and about 2 ticks at 0.25, so the stale threshold needs no special handling.
- No fairness risk: the spawner and the 200-seed test are unaffected.

What it costs:

- Runs take longer in wall time. The 90-second ramp lasts 6 minutes at pace 0.25.
- Showing the pace on the HUD needs a `pace` field in the state JSON, so `docs/API.md` and `README.md` change with it.
- The viewer's smoothing between ticks must use the real tick length, or motion will stutter at low pace.
- The pace applies to a server run, so the human plays in slow motion while it is set.

Expected shape of the finding: Tev drives (or fails for reasons of judgment) at pace 1.0, while Gemma needs a lower pace before its answers arrive in time.

### Decision 5: one choice question

The model answers the single question the game needs, on the question type Tev was trained for. Multi-question requests (extra `noul` per lane) were considered and left out so Tev and Gemma answer the same thing.

### Decision 6: raw and digested state

A flag, for example `-state raw|digested`, selects the prompt builder.

- `raw`: lane, speed and the nearest obstacles with `lane`, `ahead`, `speed`, as first decided.
- `digested`: the bot computes, per lane, how many ticks until that lane is blocked (or that it is clear), and passes only that plus the car's lane. The arithmetic is in Go; the model only judges.

Running the same model on both is the experiment. It applies to Gemma too, so the two axes (model, state form) can be compared.

### Decision 7: Gemma through structured output

`/api/chat` with `think: false`, `temperature: 0` and a `format` JSON schema whose `move` is an enum of `left`, `none`, `right`. The reply is parsed as JSON. This is the "ask for language, then claw the decision back" pattern the guide describes, kept as small as it will go. The one-token setup decided earlier does not work, and the out-of-the-box profile is not run.

### Decision 9: stale answers, in the light of the measured speeds

At 21 to 52 ms a Tev answer is one or two ticks old, so few should be dropped. Gemma at about 230 ms is about 7 ticks old at pace 1.0, so there most of its answers are dropped; that is the visible sign that it is too slow. At a lower pace the same latency is fewer ticks and the answers get through. The threshold stays a flag and the dropped share is reported.

### Decision 14: confidence

Tev's `confidence` and the winning probability are logged per decision and included in the HUD status text. A flag, for example `-min-confidence`, makes the bot send nothing below the given value. It is off by default so the ungated model is seen first. Gemma has no confidence value; that is recorded as a difference, not papered over.

Note for interpreting it: staying in lane is not a safe fallback in this game, so a gate can turn a wrong move into a crash by inaction.

### Decision 15: two call paths

The bot has one small function per model family behind a common "decide(state) → action, confidence, latency" shape. The difference between the two functions is part of what the README shows side by side.

### Defaults taken without asking

- The model bot is a new Go command, `cmd/llmbot`, next to `cmd/bot`. It talks to the game only over the public API and to Ollama over plain `net/http`. No new dependency.
- One warm-up call before the run, so model load time (3 to 6 s) is not counted as decision latency.
- Models run one after another, with the others unloaded (`ollama stop`).
- RAM is read from the Ollama runner process (RSS), not from `/api/ps`, which reported 383 MB for Gemma against 5,011 MB of real process memory.

---

## Open Questions

| # | Question | Impact |
|---|----------|--------|
| 1 | Does the digested state actually make Tev drive? Not yet probed | If not, the wording of instructions and criteria needs work before the bot is worth watching |
| 2 | Exact shape of the digested state: ticks per lane as numbers, or coarse words such as "blocked soon" / "clear" | Numbers still ask for a comparison; words move the threshold into the bot |
| 3 | Stale threshold. Proposed default 6 ticks | Counted in ticks, so it adapts to the pace by itself. At pace 1.0 it drops Gemma's answers, at 0.25 it lets them through |
| 4 | How many obstacles go into the raw state? Proposed: the nearest 8 | Must fit `num_ctx` 2050; the probe state with 2 obstacles used 204 input tokens |
| 5 | Is process RSS comparable across the three (Tev Q8, Gemma Q4 with a vision projector loaded)? | Decides how the RAM figures may be read |
| 6 | Which seed, and how long does a run last before it is stopped by hand? | Only matters for comparing the three by eye |
| 8 | Which paces are allowed? Proposed: any value above 0 up to 1.0 | No fairness concern either way; faster than 1.0 is not needed |
| 9 | Settled: the state JSON reports `pace` | Needed for the HUD and for the viewer's smoothing |
| 7 | The status text carries model-specific content (confidence). Keep it free text, as decided | No API vocabulary for this experiment enters the contract |

---

## Suggested Next Steps

1. **Finish the probe.** Build a digested state by hand for two or three real game states and send it to all three models. Settle open questions 1 and 2 before writing bot code.
2. **Pace.** Add the `-pace` flag to `cmd/outrun`, stretch the ticker interval in `internal/server`, add `pace` to the state, show it on the HUD, update `docs/API.md` and `README.md`.
3. **Status channel.** Add the `status` message and `controller_status` field to `internal/server`, show it in the viewer HUD, update `docs/API.md` and `README.md`, add server tests. Run `go test -race ./...`.
4. **`cmd/llmbot`.** Claim under the model name; two prompt builders (`raw`, `digested`); two callers (systemone for Tev, chat with JSON enum for Gemma); one call at a time, stale answers dropped; optional confidence gate; latency and confidence published as status; log and summary in the terminal.
5. **Quick look.** Each model in turn on one seed with the viewer open, first `raw`, then `digested`. Start at pace 1.0 and step down until the model survives; note that pace.
6. **Write down what was seen.** A README section with the Tev and Gemma calls side by side and the observed numbers. Record the new decisions and the failed approaches (Tev over `/api/chat`, one-token Gemma, `/api/ps` for RAM) in `task_plan.md`.

For a phased plan, hand this file to `plan-it`.
