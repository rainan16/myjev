# Task Plan: Hosted Jev driving the OutRun game

<!--
  A follow-up for later, not started. It depends on ./task_plan_tev.md: the bot (`cmd/llmbot`), the digested
  state and the comparison harness are built there first. This plan only adds hosted Jev as a further driver.
  Source: the TypeSafe docs (https://docs.typesafe.ai, index at /llms.txt), read 2026-10-04.
  The game's own plan is ./task_plan.md. Its rules (and AGENTS.md) still apply here.
-->

## Goal
`cmd/llmbot` can drive the game with hosted Jev (`jev-1.13.0` on `api.typesafe.ai`) in place of a local Tev model, so the user can put the hosted decision model next to `tev1:0.8b`, `tev1:4b` and `gemma4:e2b` on the same seed and pace.

A quick look, like the Tev plan. Not a benchmark.

## Current Phase
Phases 1 to 3 done on 2026-10-04 except the walk-through with the user and the `raw` state, which was not run with Jev. Phase 2 was built before Phase 1 because the key came later.

## Phases

### Phase 1: Probe the hosted API
No bot code. The aim is to know how Jev answers the questions Tev already answers, and what a call costs in time.
- [x] User creates an API key at https://console.typesafe.ai/keys and exports `TYPESAFE_API_KEY`
- [x] `GET /v1/models` to confirm the key works and which names are offered
- [ ] Replay the 120 probe states from the Tev plan against `jev-1.13.0`: not done, the captured states were not kept. Done instead: all 72 digested sentences. `raw` not probed
- [x] Measure round-trip latency on changing states and on a repeated state (Key Question 1); record the token usage per call
- [x] Check option order: ask with `left, none, right` and with the order reversed (Key Question 3)
- [x] Record results under Notes and dead ends under Errors Encountered
- **Status:** pending

### Phase 2: Jev caller in `cmd/llmbot`
- [x] A `-backend` choice (or a base-URL flag) so the existing Tev caller can point at `https://api.typesafe.ai` with `Authorization: Bearer` and a `model` field; plain `net/http`, no SDK (Key Question 2)
- [x] Key read from `TYPESAFE_API_KEY` only; never logged, never written to disk, never committed; the bot exits with a clear message when it is missing
- [x] 429 and 529: back off and count them; a decision that waited on a retry is subject to the same stale rule as any other
- [x] Per-request timeout, so a slow call is dropped and not queued. It is 2 s, not under the stale threshold: see Decisions Made
- [x] Tests against an `httptest` stand-in: auth header, request shape, 401, 422, 429, 529, timeout. No live calls, no sleeping
- [x] README: how to run the bot against Jev, and that this mode sends the game state to a third party
- **Status:** built and tested against a stand-in, 2026-10-04; not yet run against the real API

### Phase 3: Quick look and write-up
- [x] Same seed and run length as the Tev plan, `digested`, pace 1.0 and 0.5. `raw` not run
- [x] Add a Jev column to the README comparison: latency, decisions per second, share dropped as stale, cost per run. RAM does not apply to a hosted model; say so
- [ ] Walk through the result with the user
- **Status:** runs and write-up done; walk-through open

## Key Questions
Proposed answers in brackets.

1. Is the round trip fast enough to drive at pace 1.0? The docs claim about 100 to 150 ms and their cookbooks measured 111 to 114 ms, which is in the range of `tev1:0.8b` on an unseen prompt. Network distance from here is unknown, and so is whether the hosted API answers a repeated prompt faster the way Ollama does. [Measure in Phase 1]
2. One caller or two? Local Tev is already called on `POST /v1/systemone` with `state` and `questions`, the same shape as the hosted API. [One caller; base URL, auth header and model name differ]
3. Does Jev's lean towards the first Choice option matter with three options? [Test both orders in Phase 1; if answers differ, note it and keep `none` first]
4. Pin the model or use the alias? [Pin `jev-1.13.0`; `jev-latest` can move and change the answers]
5. Settled as a probe, 2026-10-04: see Notes, "Fan-out probe". Background: use more than one question per call? The docs recommend many narrow questions per request at almost no extra latency, for example one Noul per lane ("is this lane clear") combined in code. [Keep the single Choice for the comparison, so all models answer the same thing; try the fan-out once as a separate look]
6. Is the rate limit a concern? One call at a time is far below 80 requests per second. [No]

## Decisions Made
| Decision | Rationale |
|----------|-----------|
| Jev is an extra driver in `cmd/llmbot`, not a replacement that removes local Tev | The comparison is the point; the caller is switched by a flag |
| Plain `net/http`, no SDK | There is no Go SDK, and AGENTS.md allows no new dependency without asking |
| The game server is not touched | The bot makes the outbound call. The server stays loopback only |
| Same question, `instructions` and `criteria` as the Tev plan | Keeps the models comparable |
| `-backend typesafe` on `llmbot`; the URL is fixed in the code, the model defaults to `jev-1.13.0`, `-api` is forced to `systemone` | The key must go to one host only, so the URL is not a flag. `auto` asks Ollama's `/api/show`, which the hosted API does not have |
| The hosted call times out after 2 s, and a timeout is counted with the unusable replies and the bot asks again | The stale threshold is 200 ms at pace 1.0; a timeout that tight before the round trip is measured would drop every call. Revisit after Phase 1 |
| Up to 3 retries on 429 and 529, waiting 100, 200, 400 ms; the wait counts as latency. 401 and 422 stop the bot | The answer is that much older when it comes, so the stale rule sees it |
| The summary prints input tokens and retries, and "does not apply" for memory | Cost per run is tokens times the price; there is no runner to measure |
| The key may also sit in a `.env` file in the directory the bot is started in, as `TYPESAFE_API_KEY=...`; `.env` is in `.gitignore` (user, 2026-10-04) | The user's choice, replacing "environment only". The environment wins when both are set |
| The model stays pinned to `jev-1.13.0` | The name is accepted although `/v1/models` lists only `jev-latest` and `jev-preview`; all three answered as `jev-1.13.0` |
| The fan-out is a third state form, `-state fanout`, with the rule in Go (`towardsClear`): stay on a lane called clear (0.5 or more), otherwise the nearest such lane, the surer one on a tie (user asked for the mode, 2026-10-04) | The same rule as in the probe, so the run can be read against the probe's 104 of 110 |
| The fan-out is out of `llmbot` again; Jev drives on the digested sentence (user, 2026-10-04) | Three variants, none drove as well as the digest: see Errors Encountered and Notes. The findings are kept, the code is not |
| On the digested state the model is asked about the road as it will be when its answer arrives (`ahead` in `prompt.go`); `-look-ahead` sets the ticks, -1 follows the median age of the last 9 answers, 0 is off (user, 2026-10-04) | The game is predictable a few ticks on, so this is arithmetic for the harness. The car's own gain in speed over those ticks is left out |
| An answer is no longer dropped for its age; a late move goes out if it passes the recheck. `-max-stale` only counts late answers now (user, 2026-10-04) | The age rule is why Jev and Gemma never steered at pace 1.0. The recheck compares against the state the model was asked about, which is now the predicted one |
| The answer time allowed for in `clear lane beyond` is the oldest of the last 9 answers, no longer the oldest ever acted on | Without the age limit one hung call would have set it for the rest of the run |
| On the state moved ahead, the first move counts as decided in `clear lane beyond` (`decided` 1 in `digested` when the look-ahead is above 0); the recheck compares against the same count (user, 2026-10-04) | The answer time of the first move was counted twice: once by `ahead`, once in `leadsToClear`. With the look-ahead off nothing changes. `TestFirstMoveIsDecidedOnTheStateTheAnswerArrivesIn` |
| `-questions N` lets N questions be open at once, default 1. Above 1 a question goes out every `ceil(typical / N)` ticks (`spacing`), where `typical` is the median age of the last 9 answers. While a sent move has not shown up, a further move is not sent (`moving` in `judge`, counted with "after a lane change") (user, 2026-10-04) | The window in which a question gets `clear lane beyond` can be as narrow as one answer takes. Spreading the questions keeps the answers from arriving in a bunch. Inputs are queued by the server, so without the guard two answers about the same lane would move two lanes. `TestSecondAnswerDoesNotMoveTheCarWhileAMoveIsOnItsWay`, `TestQuestionsAreSpreadOverTheTimeOneAnswerTakes` |
| The recheck before sending allows for the median age of the last 9 answers (`typical`), no longer the oldest; the sentence still uses the oldest (user, 2026-10-04) | With `-questions 2` a `right` towards a clear lane beyond was held on the 1,735 row because one recent answer had been slow. The change is one argument in the loop and has no test of its own: `judge` takes the number, the loop picks it |
| This plan lives in `task_plan_jev.md` | It is a separate, later task; the Tev plan stays as it is |

## Errors Encountered
| Error | Attempt | Resolution |
|-------|---------|------------|
| The key was in `.env` as `typesave-key=...` | 1 | Not a name a shell can export. Renamed in place to `TYPESAFE_API_KEY` |
| Fan-out rule that moves to a clear neighbour only and never crosses a lane called blocked (user agreed to try it, 2026-10-04) | 1 probe, 1 run | Worse, taken out again. Probe, same answers scored with both rules: 100 of 110 against 104. Run, seed 7, pace 0.5: crashed at tick 764 (14 rows) against 2,570. It sat in lane 0 with the answers 0.1, 0.3, 0.7, 0.8 and did not move: lane 1 was blocked in a while and could have been crossed. One yes/no per lane does not tell "blocked in a while" from "blocked now", which is what the digest's `clear lane beyond` is for. A second question per lane was tried next; see the next row |
| A second yes/no per lane, `Is an obstacle in lanes.lane_N level with the car right now, or about to be?`, with the rule crossing any lane not called blocked now (user asked to try it, 2026-10-04) | 1 probe, 1 run | No gain, taken out again. Probe on the 120 states: 103 of 110 moves right with it, 101 on the same answers without (the first probe had 104). The question has no false alarms (440 of 440 other lanes called not now) and finds half the lanes that are blocked now (20 of 40). Run, seed 7, pace 0.5: crashed at tick 2,570 again, 63 rows, 631 decisions, median 255 ms, 6 stale, 704,948 input tokens (about $0.030, 1,112 a call against 728). At the crash the answer for lane 1 was 0.1 on both questions: not clear, and not blocked now, so the car crossed it. Before that it had gone left to lane 0 on a 0.5 and back right on a 0.4: the answers hover around the threshold there. The time did not grow with eight questions |
| Jev at pace 1.0, seed 7, digested: crashed at tick 255 with all 27 answers stale | 1 run | Speed, not judgment: answers were 7 to 12 ticks old. Same as Gemma at pace 1.0. Use pace 0.5 |

## Notes
- **Endpoint.** `POST https://api.typesafe.ai/v1/systemone`, `Authorization: Bearer <key>`, body `{"state": ..., "model": "jev-1.13.0", "questions": {"<id>": {"type": "choice", "instructions": ..., "criteria": {"left": "...", "none": "...", "right": "..."}}}}`. The answer has `choice`, `probabilities` and `confidence`. `GET /v1/models` lists the models
- **Price and limits** (models page, may change without notice): $0.042 per million input tokens, output free; 80 requests and 100K tokens per second; 64k tokens per request. The raw probe state used about 200 input tokens, so a three-minute run at ten calls a second is roughly 360K tokens, under two cents
- **Errors.** 401 bad key, 422 malformed body, 429 rate limit, 529 overloaded. Retry the last two with backoff
- **Known weaknesses of `jev-1.13`** that bear on this game: it reads literally; it does not count, compare numbers or do arithmetic reliably (the docs say to compute in code and pass a named bucket, which is what the digested state already does); a Choice can lean towards its first option; unrelated content in the state lowers accuracy
- **Other request features** not used so far: a Noul question returns one probability and has no `confidence`; a question can point at part of a JSON state with a backticked path such as `` `lanes.left` ``; `instructions` and `criteria` may be objects, with field names of our choosing
- **Probe (2026-10-04).** All 72 digested sentences, each new, same `instructions` and `criteria` as Tev: median 244 ms, p95 378 ms, 56 of 56 right by the rule used for the short-instruction probe in `task_plan_tev.md`, median confidence 0.98, 379 input tokens per call (38 output). Options reversed: 257 ms, 56 of 56, the count of each move differs by one in 72. The same sentence 30 times: 260 ms, so no fast path for a repeated prompt. `jev-preview`: same scores
- **Runs (2026-10-04, seed 7, digested, one each).** Pace 0.5: stopped at tick 2,754, 70 rows, score 6,931, 699 decisions at 3.8 per second, median 247 ms, p95 343 ms, 36 moves, 3 stale, 0 retries, 264,722 input tokens (about $0.011). Pace 1.0: crashed at tick 255, 27 decisions, all stale. Same server and day at pace 0.5: `tev1:4b` stopped at 2,760 (71 rows, 55 ms), `gemma4:e2b` crashed at 1,181 (245 ms), `tev1:0.8b` at 357 (25 ms); `tev1:4b` at pace 1.0 crashed at 357. Also at pace 1.0: `tev1:0.8b` crashed at 357 (343 decisions, 28.8 per second, median 20 ms, 0 stale, 1 move), `gemma4:e2b` at 255 (32 decisions, all stale, median 251 ms)
- **Fan-out probe (2026-10-04, Key Question 5).** 120 states: 30 layouts captured on seed 7 with the sample bot driving (ticks 104 to 2,550), the car placed in each lane; 110 have an expected move by the rule of the sentence probes. Hosted Jev, one call per state and variant:

| Asked | Move right | Lanes judged right | Median | Tokens per call |
|-------|------------|--------------------|--------|-----------------|
| Digested sentence, one Choice (what the bot does) | 110 of 110 | | 270 ms | 379 |
| Raw obstacle list, one Choice | 67 of 110 | | 264 ms | 450 |
| Raw obstacles grouped by lane, one yes/no per lane, rule in code | 104 of 110 | 432 of 480 | 266 ms | 728 |
| One word per lane, one yes/no per lane, rule in code | 110 of 110 | 480 of 480 | 270 ms | 359 |

  The yes/no question per lane was `Is lanes.lane_N free of obstacles the car will reach soon?` (path in backticks) with a `true` and a `false` description. The code then stays if the car's lane is called clear (0.5 or more) and otherwise heads for the nearest lane called clear. The state for that variant is `car_speed` and, per lane, the same `ahead` and `speed` numbers as the raw state: regrouped, not calculated
- **What it shows.** Narrow questions rescue most of the raw state: 104 against 67. The six misses are the model calling a clear lane blocked (229 of 264 clear lanes right, 203 of 216 blocked ones), which in five cases made the car leave a lane it could have kept. On words the fan-out is perfect, but there the model only reads a word back, so it adds nothing over the code
- **Time does not grow with the number of questions.** The same Choice 1, 4, 12 and 40 times in one call: 251, 306, 292 and 302 ms median. Tokens do grow: 379, 676, 1,468 and 4,240 per call, about 100 per question
- **Slow calls happen.** In about 700 probe calls two took longer than the 2 s timeout
- The probe code was a temporary test file and is not in the repo
- **`-state fanout` in `llmbot` (2026-10-04, removed the same day).** The probe's state, questions and rule, built into the bot: `fanout` in `prompt.go`, the rule `towardsClear`, the call in `tev.decide`. Decision models only; a chat model is refused. No confidence is reported (a yes/no answer has none), so `-min-confidence` does nothing in this mode. The log shows the four answers (`clear? 0.7 0.1 0.4 0.7`). The stale rule and the recheck before sending apply as in every mode; the recheck is arithmetic in the harness
- **Fan-out run (seed 7, pace 0.5, one run).** Crashed at tick 2,570, 63 rows, score 6,322, speed 98. 568 decisions at 3.3 per second, median 288 ms, p95 402 ms, 34 moves, 14 stale, 4 held by the recheck, 416,524 input tokens (about $0.017). The crash: at tick 2,557 in lane 0 the answers were just under 0.5, 0.1, 0.5, 0.7; the rule headed right for lane 2 and so into lane 1, which the model had called blocked. A rule that does not cross a lane called blocked was tried afterwards and drove worse; see Errors Encountered
- **Look-ahead and late moves (2026-10-04, seed 7, one run each).** Pace 1.0: Jev crashed at tick 1,735 (37 rows, score 3,821, speed 82; 202 decisions, median 271 ms, 16 moves, 197 answers late, 15 of them moves that were sent, 0 held) against 255 before. `tev1:4b` 357 as before (1 move; answers to new sentences 13 to 14 ticks old, so no `clear lane beyond`). `gemma4:e2b` 255 as before, 30 answers, all `none`, the last on `left: clear, current: blocked soon, right: clear`. Pace 0.5: Jev stopped at 2,755 (70 rows, 2 held, 2 late), `tev1:4b` at 2,764 (71 rows, 26 late, 8 of them sent)
- **Jev's crash at 1,735** is the row that leaves only lane 2 open and comes into sight 21 ticks ahead, with the car in lane 0: the sentence read `blocked soon` for both lanes, since two answers of 8 to 11 ticks do not fit into that. `tev1:4b` crashed on the same row at pace 0.5 earlier in the day
- **First move decided on the predicted state (2026-10-04, seed 7, pace 1.0, one run each).** `tev1:4b` crashed at tick 1,594 (33 rows, score 3,441, speed 80; 589 decisions, median 55 ms, p95 449 ms, 12 moves, 50 late, 10 of them sent, 1 held) against 357 before. Its crash: asked at 1,564 it answered `left` towards a clear lane beyond, the answer was 13 ticks old and held as the lane had got worse. Jev crashed at 1,735 again (37 rows, score 3,821; 197 decisions, median 269 ms, 18 moves, 192 late, 17 of them sent, 1 held, 1 unusable, 74,602 input tokens). Pace 0.5 and Gemma were not run again
- **Why Jev still crashed at 1,735.** Worked out offline on the same states: with a look-ahead of 7 to 9 and answers 9 to 10 ticks old, the sentence reads `clear lane beyond` to the right for a question asked between ticks 1,705 and 1,711 (from 1,703 with a look-ahead of 9). Before that the lane to the right reads `blocked now` on the predicted state, as the row before is still being passed; after it there is no room for the second answer. Jev was asked at 1,697, 1,704 and 1,713: the window is about as wide as one answer takes, and the questions fell on either side of it
- **Several questions open (2026-10-04, seed 7, pace 1.0, one run each).** Jev with `-questions 3`: crashed at tick 2,999 (78 rows, score 7,775, speed 106; 969 decisions at 9.7 per second, median 262 ms, p95 323 ms, 44 moves, 88 after a lane change, 2 held, 865 late, 41 of them sent, 367,083 input tokens, about $0.015, 0 retries). On the 1,735 row: asked at 1,706, `clear lane beyond`, `right` sent 7 ticks later; asked at 1,715 from lane 1, `right: clear`, sent. The crash: at 2,972 in lane 1 it answered `left` at confidence 0.58 on `left: blocked soon, current: blocked soon, right: blocked now`; the `right` asked 3 ticks later on `right: clear` was not sent, as the car had changed lane or was about to. `tev1:4b` with `-questions 2`: crashed at tick 357 (5 rows, 1 move; median 102 ms, p95 500 ms, new sentences 14 to 15 ticks old) against 1,594 with one question: the two calls share one runner
- **Jev with `-questions 2` (same day, seed 7, pace 1.0, one run).** Crashed at tick 1,735 again (37 rows; 345 decisions at 6.0 per second, median 302 ms, p95 380 ms, 18 moves, 18 after a lane change, 1 held, 343 late, 130,654 input tokens). On the row it was asked at 1,707, read `clear lane beyond` and answered `right`, 9 ticks old; the recheck held it. At tick 1,716 lane 1 was free for 16 ticks, so the hold means the slowest of the last 9 answers was 13 ticks or more (worked out from the rule, not logged). The next question, at 1,713, was too late. Answers were slower than in the `-questions 3` run (262 ms median), so the two runs differ in more than the count
- **Median in the recheck (same day, Jev, seed 7, pace 1.0, one run each).** `-questions 2`: crashed at 2,570 (63 rows, score 6,322, speed 98; 522 decisions, median 299 ms, 33 moves, 1 held, 197,678 input tokens) against 1,735. The crash: asked at 2,540, `right` towards a clear lane beyond, 6 ticks old, held as the lane got worse. `-questions 3`: 2,999 (78 rows, 44 moves, 0 held, median 289 ms, 346,213 tokens), the same tick and the same last moves as before the change. A first `-questions 3` run crashed at 1,355: three calls asked at ticks 1,254 to 1,260 got no reply within 2 s, so no question was open for about 60 ticks; not counted. `-questions 1`: 1,735 again (16 moves, median 321 ms, p95 437 ms); asked at 1,706 the sentence read `blocked soon`, as an answer at 1,671 had been 15 ticks old and the sentence allows for the oldest
- **All local models again with every change in (same day, seed 7, pace 1.0, `-questions 1`, the other models stopped with `ollama stop`).** `gemma4:e2b`: 318 ticks, 4 rows, 38 answers, 2 moves, all late (11 ticks old), median 257 ms; it had made no move before. Last move `left` on `left: blocked now, current: blocked soon, right: wall`. `tev1:0.8b`: 357, 338 answers, 1 move, median 21 ms, as before. `tev1:4b`: 357 in 4 of 4 runs, 2 moves, 1 held: `left` towards a clear lane beyond asked at 325 and sent, the next `left` asked at 339 or 340 held 13 ticks later
- **The 1,594 of `tev1:4b` did not repeat.** A binary with the recheck on the slowest answer again, 3 runs: 357 each, 1 move, the `left` asked at 324 or 325 held. So 1 run in 4 with that code got through, and 0 in 4 with the median. In the run that got through, the answers at ticks 311 to 352 were 13 ticks old where the others had one of 14: the 450 ms of a new sentence is 13.5 ticks. The numbers are too few to say the median is worse for Tev
- **Seeds 1 to 5 (same day, pace 1.0, one run per cell, stopped after 100 s; Jev on one server, the local models one at a time on another, both at once).** Ticks survived:

| Seed | Jev, `-questions 3` | Jev, one question | `tev1:4b` | `gemma4:e2b` | `tev1:0.8b` |
|------|------|------|------|------|------|
| 1 | 1,536 | 1,841 | 1,578 | 347 | 257 |
| 2 | 1,360 | 1,360 | 1,863 | 291 | 341 |
| 3 | 1,915 | 1,915 | 2,992, still driving | 265 | 153 |
| 4 | 2,573 | 2,573 | 2,710 | 215 | 154 |
| 5 | 1,956 | 830 | 2,992, still driving | 404 | 180 |
| Median | 1,915 | 1,841 | 2,710 | 291 | 180 |

  Moves sent, seeds 1 to 5: Jev with 3 questions 15, 20, 36, 42, 30; with one 22, 18, 34, 36, 8; `tev1:4b` 16, 35, 46, 49, 51; Gemma 1, 2, 6, 1, 6; `tev1:0.8b` 1, 1, 0, 0, 0. Jev median latency 302 to 336 ms; 1 call without a reply with 3 questions on seed 5, 2 with one question on seed 3. Jev input tokens: 1,014,598 with 3 questions, 326,243 with one, about $0.056 together. `tev1:4b` held no move on any of the five seeds
- **Seed 11, Jev, `-questions 3`, pace 1.0:** crashed at tick 1,452 (30 rows) in 4 runs of 4
- Not looked at: why Jev crashes where it does on these seeds, and whether the same rows stop `tev1:4b`
- Not tried: `-questions 4` for Jev; the median in the sentence as well, which is what the one-question run now fails on; a shorter timeout than 2 s for the hosted call, so a stalled call frees its slot sooner; more than one run per setting; pace 0.5 with several questions; a confidence floor (`-min-confidence`) against moves like the one at 2,972
- Ollama reports `usage` for local Tev as well, so the summary's token line shows for those runs too
- The TypeSafe agent skill is installed in Claude Code (`/typesafe:typesafe-ai`); use it when writing the questions
- Sending the state to the hosted API means the game state leaves the machine. It holds nothing private, but the README should say so
