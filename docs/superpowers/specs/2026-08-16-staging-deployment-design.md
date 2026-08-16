# Staging Deployment Design

## Goal

Provide a safe way to test new builds of the EmpireBus service before they reach
production on the motorhome. Two environments:

1. **Local simulated environment on the Mac** — a fake Garmin SERV that
   `empirebusd` connects to as if it were the real
   `ws://172.16.11.7:8888/ws`. Development and command-flow testing happen
   here, safely, without touching real hardware.
2. **Staging environment on the Jones Pi** — a second, parallel service
   instance (own systemd unit, config, port, release dir) that can be deployed
   from the Pi's git checkout before flipping to production.

## Non-goals

- No production service runtime code changes (no new config keys, no
  recording-directory change).
- No GitHub Actions work; the repo deliberately wound that back.
- Staging is not a separate motorhome or SERV; it is a parallel instance on the
  same Pi, talking to the same SERV.

## Part 1: Local simulated environment (Mac)

### `cmd/servsim` — fake Garmin SERV websocket server

New binary under `cmd/servsim/`. Listens for websocket connections and plays
the SERV side of the protocol so `empirebusd` and `cmd/heatingctl` can run
against it.

Flags:

- `-listen :8090` — websocket listen address.
- `-capture <path.ndjson>` — required; the background frame source, a
  `wscapture` or on-demand recording NDJSON file.
- `-loop` — replay the capture repeatedly (default off: play once, then stay
  idle with the echo model still active).
- `-speed <n>` — replay pacing multiplier (default 1).
- `-verbose` — log inbound and outbound frames.

Per-connection behavior:

- Read loop logs inbound frames. Bootstrap (type 96) and heartbeat (type 128)
  messages are read and discarded. Command frames (type 17) feed the echo
  model and are logged.
- Replay loop sends the capture's `receive`-direction frames in order, paced by
  the recorded `at` timestamps, capped at a maximum inter-frame gap so long
  silences do not stall. Raw `message` strings are replayed for byte fidelity
  (falling back to re-marshalling `frame` when `message` is absent). If `-loop`
  is set, restart from the first receive frame after the last.
- `event`-direction records (e.g. `recording_started`) and `send`-direction
  records (the previous session's own outbound frames) are skipped.
- The servsim exit/read loop does not need to respond to the service's type 96
  bootstrap messages; the replay stream supplies the frames the service needs.

### Echo model

A small, per-connection state model so command APIs complete. On a type 17
command frame it emits SERV-shaped state frames:
`{"messagetype":16,"messagecmd":5,"size":8,"data":[<sigLo>,<sigHi>,<value>,0,0,0,0,0]}`.

| Command | Emitted frames |
|---|---|
| signal `101` data `[101,0,3]` (power on) | `101=1` then `102=0` (power on, not busy) |
| signal `101` data `[101,0,5]` (power off) | `101=0` |
| signal `107` (temp up) press+release | target +0.5 °C → `105` frame |
| signal `108` (temp down) press+release | target −0.5 °C → `105` frame |
| signal `4` (open valve) press→`1`, release→`0` | `4=1`, then `4=0` |
| signal `5` (close valve) press→`1`, release→`0` | `5=1`, then `5=0` |
| signal `47` command value `3` | `47=1` |
| signal `48` command value `3` | `48=1` |

Target temperature encoding matches `heating.decodeTargetTemperature`: data
`[105,0,0,22,<b0>,<b1>,<b2>,<b3>]` where the four payload bytes are the
little-endian `int32` of `(celsius + 273.15) * 1000`. If no `105` frame has
been seen yet (replay or echo), the model seeds a 20.0 °C baseline on first
use so `SetTargetTemp`'s `GetTargetTemp` step does not time out.

Frame shapes in the echo table match browser-confirmed real SERV frames: state
frames observed as `messagetype:16, messagecmd:5, size:8` (see
`garmin-ws-20260815T142323Z.ndjson`) and the signal `105` payload layout
confirmed in `heating/heating_test.go` / `Heating.har` captures.

### `config.sim.yaml`

New, committed sample config for the simulated environment:

- `garmin.ws_url: ws://localhost:8090/ws`
- `garmin.heartbeat_interval: 4s`
- `api.listen: 0.0.0.0:8091`
- `location.enabled: false` (tracking off; no RUTX50/timezone contention)
- `automation.timezone: Europe/London`, a single off-schedule heating program
  so the scheduler does not issue commands during development

### `scripts/sim/run-sim.sh`

New convenience script: builds `cmd/servsim` and `cmd/empirebusd`, starts
servsim with `-capture` (default: the newest `captures/*.ndjson`), waits for
the websocket port to accept, then runs empirebusd with `-config
config.sim.yaml`. Ctrl-C tears both down. Prints the UI/API URLs.

### Tests

New Go tests in the `servsim` package:

- Replay parsing: an NDJSON capture becomes an ordered list of (delay,
  message); `event` and `send` records are skipped; pacing math is correct.
- Echo model: heating on/off, temp up/down (including the 20.0 °C seed), valve
  open/close, lights — assert the exact emitted frames.
- One end-to-end test using a gorilla websocket dial: connect, send a command
  frame, assert the echo frames arrive.

## Part 2: Staging environment on Jones Pi

### Layout

| | Production | Staging |
|---|---|---|
| install root | `/opt/xtura` | `/opt/xtura-staging` |
| releases | `/opt/xtura/releases/<sha>` | `/opt/xtura-staging/releases/<sha>` |
| active link | `/opt/xtura/current` | `/opt/xtura-staging/current` |
| config | `/var/lib/xtura/config.yaml` | `/var/lib/xtura-staging/config.yaml` |
| data dir | `/var/lib/xtura` | `/var/lib/xtura-staging` |
| systemd unit | `empirebusd.service` | `empirebusd-staging.service` |
| HTTP | `0.0.0.0:80` | `0.0.0.0:8080` |
| health check | `http://127.0.0.1/v1/health` | `http://127.0.0.1:8080/v1/health` |

Both instances connect to the same SERV. Staging still talks to real hardware
if commands are issued from it; this is an accepted risk (read-only/UI
verification in staging, full command testing in the Mac simulation).

### `ops/systemd/empirebusd-staging.service`

Copy of the prod unit with:

- `WorkingDirectory=/opt/xtura-staging/current`
- `ExecStart=/opt/xtura-staging/current/empirebusd -config /var/lib/xtura-staging/config.yaml`

### `config.staging.example.yaml`

Copy of `config.example.yaml` with:

- `api.listen: 0.0.0.0:8080`
- `location.enabled: false` (no RUTX50 polling or `timedatectl` contention
  with prod)
- tracking absent (requires `location.enabled`)

Setup on the Pi is manual, matching prod: copy to
`/var/lib/xtura-staging/config.yaml` before first deploy.

### `scripts/deploy/deploy-on-pi.sh`

Parameterize with an `ENVIRONMENT` env var (default `prod`; prod behavior is
unchanged when unset). A per-env table drives:

- `INSTALL_ROOT`, `CONFIG_PATH`, `DATA_DIR` (for chown)
- `SERVICE_NAME`, `SERVICE_UNIT_SOURCE`, `SERVICE_UNIT_DEST`
- `HEALTH_URL`

The existing git fetch/checkout, tests, build, install, ws_url migration,
`systemctl` enable/restart, and health-check steps then run unchanged for both
environments.

### `scripts/deploy/run-deploy-from-mac.sh`

Forward `ENVIRONMENT` (default `prod`) into the remote command:

```bash
./scripts/deploy/run-deploy-from-mac.sh                       # prod
ENVIRONMENT=staging ./scripts/deploy/run-deploy-from-mac.sh <sha>   # staging
```

### Known risk

The SERV's tolerance for two concurrent websocket clients is unverified. If
staging's second connection causes the SERV to drop the production connection,
point staging's `garmin.ws_url` at a `servsim` instance instead — that is a
config change only. This must be tested on the Pi when staging first goes up.

## Part 3: Documentation and verification

- `docs/garmin-empirbus-signals.md` — the echo model relies on command→state
  mappings for signals 101, 102, 105, 107/108, 4, 5, 47, 48. Add a short
  "servsim simulation" note, labeled as inference/simulation behavior, with
  `cmd/servsim` as the local source. Keep the existing evidence-based content
  unchanged.
- `README.md` — add "Simulated environment (development)" and a staging
  section (layout, setup, deploy commands, risk note).
- `docs/codex-notes.md` — add repo-map entries for `cmd/servsim`,
  `config.sim.yaml`, and the staging deploy files.

Deploy-script verification: no shell harness exists in the repo; verify by
read-through and one real staging deploy on the Jones Pi with the user.

## Testing summary

- `go test ./...` must pass (new servsim tests included).
- `npm ci` + `rtk lint eslint web/static/app.js` unchanged (no web changes).
- Manual: run `scripts/sim/run-sim.sh`, exercise heating/water/lights
  endpoints against `:8091`, observe echo frames and state transitions.
- Manual (Pi): one staging deploy, confirm `empirebusd-staging` starts on
  `:8080`, prod stays healthy, then verify SERV concurrent-client behavior.
