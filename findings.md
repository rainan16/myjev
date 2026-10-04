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

## Technical Decisions
| Decision | Rationale |
|----------|-----------|
| Digested state is one sentence of coarse words for left, current and right lane | Best probe result on all three models; numbers and absolute lanes failed |
| `instructions` and `criteria` are constant; only `state` changes | Keeps repeat prompts on the fast path |
| Lane words: `clear`, `blocked soon`, `blocked now`, `wall`, `blocked soon, clear lane beyond` | Thresholds live in the bot: 60-tick horizon, "now" is 2 ticks or less |

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
