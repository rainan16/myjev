# Running the game and the sample bot

How to start and play is in the [README](../README.md#play). This page has the flags.

## Game server

To build a binary you can copy anywhere (the viewer is embedded in it):

```sh
go build -o outrun ./cmd/outrun
./outrun
```

`go run ./cmd/outrun` and the binary take these flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-addr` | `127.0.0.1:8765` | Address to listen on. Must be a loopback address, because the API has no authentication |
| `-seed` | random | Seed of the first run. The same seed always gives the same obstacle layout |
| `-pace` | 1 | Speed of the game's clock, above 0 up to 1. At `-pace 0.25` every tick lasts four times as long, for you and for bots alike. The game is the same tick for tick, so scores stay comparable. The viewer shows the pace next to the seed |

## Sample bot

`go run ./cmd/bot` takes these flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-addr` | `127.0.0.1:8765` | Address of the game server |
| `-name` | `gobot` | Name to drive under |
| `-seed` | random | Use this seed for every run |
| `-runs` | 0 | Stop after this many runs (0 = keep playing) |
| `-pause` | `2s` | Wait after a crash before restarting |
