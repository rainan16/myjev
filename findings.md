# Findings & Decisions

Plan: `task_plan_tev.md`. Detailed figures are in its Notes; this file is the running list of discoveries.

## Requirements
- Three local Ollama models (`tev1:0.8b`, `tev1:4b`, `gemma4:e2b`) drive the OutRun game through its public API, from a new `cmd/llmbot`
- Compare decision speed, RAM footprint and the harness each model needs. A quick look, not a benchmark
- `AGENTS.md` applies: game core stays pure, loopback only, no new dependency, API docs move with API changes

## Research Findings

### Phase 1 (probe, 2026-10-04)
- The raw obstacle list does not make any of the three drive: on seven probe states all three answered `none` every time (4 of 7 safe by luck)
- Ticks-until-blocked as numbers is no better than raw, whether keyed by absolute lane or by move. Gemma answered `right` on every state with the absolute form
- Coarse words relative to the car work. As bare JSON they still left `tev1:0.8b` answering `none`; as one sentence naming the lanes, with an instruction that states the rule, all three choose well
- On 120 states: `tev1:4b` 120 safe and moved in 45 of 46 when its lane was not clear; `gemma4:e2b` 119 and 36; `tev1:0.8b` 118 and 32
- Tev's confidence separates good from bad answers on 0.8b (median 0.55 when safe, 0.20 when not). 4b sits at 0.92
- A neighbour lane that is blocked but leads to a clear lane needs its own wording (`blocked soon, clear lane beyond`), or a car two lanes from safety sees nothing worth moving for
- Ollama answers a Tev prompt it has seen before in about 20 ms (0.8b) or 55 ms (4b). A new prompt costs about 90 ms and 420 ms digested, 127 ms and 627 ms raw. The earlier 21 ms and 52 ms figures were repeat calls
- So on the raw state `tev1:4b` is slower than Gemma (627 ms against 485 ms), and only `tev1:0.8b` is anywhere near one tick
- Putting the lane words into `criteria` instead of `state` scores the same but makes every prompt new, so it loses the fast path
- Runner RSS (`llama-server`): 0.8b 2,911 MB, 4b 9,562 MB, Gemma 5,365 MB during the probe. Higher for Tev than first reported; measure again in Phase 3
- The sample bot steers early, so states captured from its run rarely have the car in a blocked lane. The probe placed the car in each lane of the captured layouts to get such states

### Phase 2 (tracer bullet, 2026-10-04)
- The slice works end to end with `tev1:0.8b`: about 28 decisions per second, median 21 ms, answers 0 to 2 ticks old
- `tev1:0.8b` crashes at tick 432 on seed 7 every run, by judgment and not by speed. It stays in a lane that is `blocked soon` when the way out is on the left. A 15-sentence probe showed it almost never answers `left`, while the mirrored sentences get `right`
- `tev1:4b` answered all 15 of those sentences correctly and drove 1,956 ticks on seed 7 (score 3,789) before crashing at speed 76
- At high speed several rows fit in the 60-tick horizon, every lane reads `blocked soon`, and the sentence stops saying which lane is blocked first. The digest needs a way to rank distance
- A sentence the model has not seen costs `tev1:4b` about 420 ms, which is 12 to 14 ticks at pace 1.0. New sentences appear exactly when the situation changes, so the slow answers come at the worst moment
- Without a guard, a move that has not landed yet is asked about again and sent twice. The bot waits for the lane to change (or 4 ticks) after sending, as the sample bot does

### Phase 3 (comparison harness, 2026-10-04)
- `tev1:0.8b` never answers `left`: 0 of 29 across 13 wordings, key orders and sentence orders. Mentioning `right` first makes it stop answering `right` too. It is a position habit of the small model, not a wording problem
- Ollama's fast path differs by model. `tev1:0.8b` answers any of the 140 sentences fast once it has seen it. `tev1:4b` is fast only when the prompt equals the previous one; warm-up over all sentences bought nothing and cost 68 s
- So for `tev1:4b` every change of situation costs 420 to 500 ms, 13 to 15 ticks at pace 1.0, and that answer is dropped as stale. It then asks again and gets the answer in 52 ms. A change is acted on about 16 ticks late
- Gemma at pace 1.0: 248 ms per answer is 7 to 9 ticks, so with `-max-stale 6` every answer is dropped. It never steered
- `blocked later` with the longer instruction did not help: 0.8b stopped moving at all, 4b moved out of a `blocked later` lane into a `blocked soon, clear lane beyond` one and crashed there
- The schema keeps Gemma to a move: no unusable reply in 140 warm-up questions and 41 decisions
- Runner memory is the sum over all `llama-server` processes, so a model that is still unloading inflates it. Unload and wait before measuring

## Technical Decisions
| Decision | Rationale |
|----------|-----------|
| Digested state is one sentence of coarse words for left, current and right lane | Best probe result on all three models; numbers and absolute lanes failed |
| `instructions` and `criteria` are constant; only `state` changes | Keeps repeat prompts on the fast path |
| After a move is sent, no new question until the lane changes or 4 ticks pass | Same rule as `cmd/bot`; stops one answer becoming two lane changes |
| `none` is not sent to the server | Nothing to do; keeps the input queue empty |
| Caller chosen from the `decision` capability reported by `/api/show` | No guessing from the name |
| Unusable Gemma reply is counted, not fatal | The reply text is the model's own |
| Log prints a decision only when lane, answer, sentence or outcome differ from the last line; `-v` prints all | 213 KB of log for 45 s was unreadable |
| Lane words: `clear`, `blocked soon`, `blocked now`, `wall`, `blocked later`, `blocked soon, clear lane beyond` | Thresholds live in the bot: 60-tick horizon, "later" is beyond 20 ticks, "now" is 2 ticks or less |

## Issues Encountered
| Issue | Resolution |
|-------|------------|
| First latency figures were cache hits | Measure on changing states, one model loaded |
| `ps` filter for `ollama runner` found nothing | The runner process is `llama-server` |

## Resources
- Ollama at `localhost:11434`: `POST /v1/systemone` (Tev), `POST /api/chat` (Gemma)
- `intent_decisions_answers_tev.md`: the working calls and the reasoning behind each decision
- Probe scripts and captured states were kept in the session scratchpad, not in the repo

## Visual/Browser Findings
-

---
*Update this file after every 2 view/browser/search operations*
