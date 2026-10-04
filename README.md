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

Each key press moves exactly one lane; holding a key does nothing more. The car speeds up on its own, from 40 to 90 over the first 90 seconds. Any hit ends the run, and the score is the distance survived.

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
