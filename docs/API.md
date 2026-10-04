# API contract

The game server listens on `127.0.0.1:8765` by default (`-addr` flag). Everything is JSON. The web viewer uses exactly this API, so anything the viewer can do, a program can do.

The server refuses to start on anything but a loopback address, because the API has no authentication. Browser requests from other sites are rejected (403).

## Concepts

- **Lanes** are numbered `0` (leftmost) to `3` (rightmost).
- **Distances** are in world units along the road. `ahead` is measured from the front of the player's car.
- **Tick**: the game advances 30 times per game second, which is a real second unless the server was started with a lower pace (see `pace` below). At most one lane change is applied per tick. Extra inputs are queued (up to 4) and applied on the following ticks.
- **Actions**: `left`, `right`, `none`. One action moves the car one lane. Moving into the road edge does nothing.
- **Controller**: one named client drives at a time. The default is `human` (the viewer's keyboard). Input from anyone else is rejected. See "Taking control" below.

## State object

```json
{
  "tick": 412,
  "run_id": 3,
  "seed": 42,
  "tick_rate": 30,
  "pace": 1,
  "controller": "human",
  "game_over": false,
  "lanes": 4,
  "lane": 1,
  "car_length": 4,
  "speed": 47.6,
  "distance": 601.5,
  "score": 601,
  "best_score": 1210,
  "obstacles": [
    {"id": 17, "lane": 2, "ahead": 30.7, "length": 1, "speed": 0, "type": "obstacle"},
    {"id": 19, "lane": 0, "ahead": 88.2, "length": 4, "speed": 20, "type": "car"}
  ],
  "grid": {
    "row_length": 20,
    "rows": [[0, 0, 0, 0], [0, 0, 1, 0], [0, 0, 0, 0], [0, 0, 0, 0], [2, 0, 0, 0]]
  }
}
```

| Field | Meaning |
|-------|---------|
| `tick` | Ticks since the run started |
| `run_id` | Increases by one on every reset |
| `seed` | Seed of the obstacle layout for this run. The same seed always gives the same layout |
| `tick_rate` | Ticks per second of game time. Speeds are per game second too |
| `pace` | How fast the game's clock runs against the wall clock, above 0 up to 1. It is set with the server's `-pace` flag and fixed for the life of the server. A tick lasts `1 / (tick_rate * pace)` seconds: 33 ms at pace 1, 133 ms at 0.25. Nothing else changes: the same seed and inputs give the same run, tick for tick, at any pace, so scores can be compared across paces |
| `controller` | Name of who is driving: `human` or the name a bot claimed with |
| `game_over` | `true` after a collision. The state is frozen until a reset |
| `lane` | The car's lane |
| `car_length` | Length of the player's car in world units |
| `speed` | The car's speed in world units per game second. It rises from 40 to 90 over the first 90 game seconds (2,700 ticks), then holds. Steering does not affect it |
| `distance`, `score` | Distance survived. `score` is `distance` rounded down |
| `best_score` | Best score since the server started (in memory only) |
| `obstacles` | Everything within 300 units that is not yet behind the car, nearest first |
| `grid` | Lookahead grid, 12 rows of `row_length` units. `rows[0]` is the stretch directly ahead and also holds anything alongside the car. Each row has one cell per lane: `0` empty, `1` obstacle, `2` car |

### Obstacles

| Field | Meaning |
|-------|---------|
| `id` | Stable for the life of the obstacle |
| `lane` | Its lane. Obstacles never change lane |
| `ahead` | From the car's front edge to the obstacle's rear edge. Negative once the car's nose is past that edge |
| `length` | 1 for a barrier, 4 for a car |
| `speed` | 0 for a barrier (`type: "obstacle"`). Moving traffic (`type: "car"`) drives in the same direction, always slower than the player |
| `type` | `obstacle` or `car` |

An obstacle in the car's lane is a crash while `-(length + car_length) < ahead < 0`. Once `ahead + length <= -car_length` it is behind the car and drops out of the list. The gap closes by `(speed - obstacle.speed) / tick_rate` per tick.

### Fairness

Obstacles come in rows that are reached at the same moment. A row never blocks every lane, and after one row is fully behind the car there are at least 12 ticks before the next one arrives, enough to reach any lane. So every layout can be survived.

## Taking control

A bot picks a name (1 to 32 printable characters, no spaces) and claims the car with it. From then on only input sent under that name is accepted, and the viewer shows the name as the driver.

- A bot can claim only while `human` is the controller, or to repeat its own claim. If another bot is driving, the claim fails.
- The controller hands the car back with a release. Over WebSocket, closing the connection does the same.
- The human can always take the car back: claim with the name `human` (the viewer does this on the **H** key).
- Changing the controller does not reset the run. Moves the previous driver had queued are dropped. Call reset for a clean run.
- Anyone may reset, whoever is driving.

## HTTP

| Method and path | Body | Result |
|-----------------|------|--------|
| `GET /` | | The web viewer |
| `GET /state` | | State object |
| `POST /input` | `{"action": "left", "name": "mybot"}` | `{"ok": true}` |
| `POST /reset` | `{}` or `{"seed": 42}` | State object of the new run |
| `POST /control/claim` | `{"name": "mybot"}` | `{"ok": true, "controller": "mybot"}` |
| `POST /control/release` | `{"name": "mybot"}` | `{"ok": true, "controller": "human"}` |

`name` in `/input` is optional and defaults to `human`.

Errors come back as `{"ok": false, "error": "..."}`:

| Status | Meaning |
|--------|---------|
| 400 | Malformed body, unknown action or invalid name |
| 403 | Browser request from another origin |
| 409 | Not allowed right now: input or release from someone who is not driving, or a claim while another bot drives |

```sh
curl -s localhost:8765/state
curl -s -X POST localhost:8765/input -d '{"action":"right"}'
curl -s -X POST localhost:8765/reset -d '{"seed":42}'

curl -s -X POST localhost:8765/control/claim   -d '{"name":"mybot"}'
curl -s -X POST localhost:8765/input           -d '{"action":"left","name":"mybot"}'
curl -s -X POST localhost:8765/control/release -d '{"name":"mybot"}'
```

## WebSocket

Connect to `ws://127.0.0.1:8765/ws`. Browser connections are only accepted from the viewer's own origin.

Server to client, once per tick and whenever the controller or run changes:

```json
{"type": "state", "state": { ... }}
```

Client to server:

```json
{"type": "input", "action": "left"}
{"type": "reset", "seed": 42}
{"type": "claim", "name": "mybot"}
{"type": "release"}
```

`seed` is optional. A connection steers as `human` until it claims; after a claim its inputs count under the claimed name, so no `name` is needed on `input`. On a rejected or malformed message the server replies `{"type": "error", "error": "..."}` and keeps the connection open. There is no reply on success; the next state shows the result.

States come every `1 / (tick_rate * pace)` seconds. A slow client does not slow the game down: if it falls behind, older states are dropped and it receives the most recent one.
