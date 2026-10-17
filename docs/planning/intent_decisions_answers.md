# Intent, Decisions and Open Questions: Raw OutRun-style lane dodger with bot API

_Generated: 2026-10-03_

---

## Refined Intent

Build a deliberately raw take on the retro game OutRun (inspired by ZgzInfinity/OutRun and agavinm/OutRun) in **Go**, as a **single local binary**. The player's only job is to switch lanes with **K (left)** and **L (right)** and avoid other cars and obstacles. Graphics are raw wireframe/outline pseudo-3D on an HTML canvas. The game core is headless and exposes a **local API** (WebSocket stream plus HTTP endpoints) so that a human (via an embedded web viewer) or an external program (a bot) can play, with the live state visible both on screen and on the API in real time. A simple Go sample bot is included so you can play as a human and also watch a bot play.

---

## Key Decisions

| # | Area | Decision |
|---|------|----------|
| 1 | Architecture | Headless game core plus separate viewer; the viewer is just another client of the API |
| 2 | Language | Go, single binary, small footprint |
| 3 | Viewer | Embedded web viewer served by the Go binary on localhost, canvas wireframe pseudo-3D |
| 4 | Speed | Constant forward speed that ramps up over time; the only inputs are K and L |
| 5 | Collision | Hit = game over; score is distance survived; restartable |
| 6 | Road | Pseudo-3D with curves, no hills; curves are visual only |
| 7 | Steering | Discrete lanes, key press = one lane change |
| 8 | Lanes | 4 lanes |
| 9 | API style | WebSocket state stream and command channel, plus HTTP endpoints (e.g. state, input, reset) |
| 10 | Time model | Real-time fixed tick loop only (no step mode) |
| 11 | Input control | One active controller at a time (human by default), switchable via the API; viewer shows who is driving |
| 12 | Observation | JSON: car lane, speed, distance, score, game-over flag, obstacle list (lane, distance ahead, type), plus a lookahead lane grid |
| 13 | Action space | Per-tick event: left / right / none; same semantics as K / L |
| 14 | Spawning | Seeded random, always leaves a passable path; two types: moving slower cars and static obstacles |
| 15 | Human keys | One lane per keypress, no auto-repeat |
| 16 | Network | Bind to 127.0.0.1 only, no auth; WebSocket origin check |
| 17 | Sample bot | Simple Go bot in the same repo (e.g. `cmd/bot`), connects over the WebSocket, picks a safe lane by lookahead |
| 18 | Restart | Reset via API (optional seed) or key press in the viewer; best score kept in memory only, nothing written to disk |

### Decision notes

**#6 / #7 (resolved conflict).** Curves were first proposed as drifting the car outward, which needs a continuous position. You chose discrete lanes, so curves are cosmetic perspective that give the OutRun look without affecting physics.

**#10 (trade-off accepted).** Real-time only means bot latency changes outcomes and runs are not exactly reproducible, even with a seed. This is accepted for v1. Obstacle layout is still seed-determined.

**#11.** Avoids a human and a bot fighting over the car; a bot claims control explicitly.

**#13.** Keeping the action identical to the human's means a bot plays the same game, with no absolute "go to lane N" shortcut.

---

## Open Questions

| # | Question | Impact |
|---|----------|--------|
| 1 | Tick rate (e.g. 30 vs 60 Hz) and the state broadcast rate | Feel of play and bot reaction time |
| 2 | Speed ramp: start speed, max speed, ramp curve | Difficulty and playable window for a human |
| 3 | Minimum spacing between obstacle rows so a lane change is always possible in time at max speed | Fairness guarantee in #14 |
| 4 | Exact endpoint names and JSON schema (state, input, reset, claim/release control) | Bot compatibility; should be documented |
| 5 | Lookahead depth for the lane grid (number of rows, row size) | Bot usefulness and payload size |
| 6 | Default port and the flag to change it | Minor, practical |
| 7 | Whether a bot taking over mid-run should reset the run or continue it | Cleanliness of bot scoring |
| 8 | Whether to add a step mode later | Would help deterministic testing and training; deliberately out of v1 |

---

## Suggested Next Steps

1. Define the API contract first: endpoints, WebSocket message formats, state JSON, and the controller claim/release flow (resolves open questions 4, 5 and 7).
2. Implement the headless core in Go: fixed-rate tick loop, 4-lane model, ramping speed, seeded spawning with the passable-path guarantee, collision and game over.
3. Add the HTTP and WebSocket server bound to 127.0.0.1, with the origin check and controller arbitration.
4. Build the embedded web viewer: wireframe pseudo-3D road with visual curves, K/L handling (one lane per press), who-is-driving indicator, score, and a restart key.
5. Write the sample bot (`cmd/bot`) with simple lookahead lane selection, and verify it end to end while watching in the viewer.
6. Tune tick rate, speed ramp and spawn spacing by playing as a human; add a few tests for the spawner fairness and the collision logic.
