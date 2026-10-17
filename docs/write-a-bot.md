# Write your own bot

The full contract is in [API.md](API.md). The short version:

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
