# EmpireBus Service

A small go service to monitor a garmin empire bus event stream and convert it to a sensible Rest api. includes some automation such as a heating schedule.

the system is designed to run on a raspberry pi and is tested (and to be fair) developed for a EuraMobil stura. Eventual target is a pi zero2w.

The service assumes it can reach the Garmin SERV/WDU web socket. On the EuraMobil the SERV connects over the motorhome's internal Ethernet at `172.16.11.7`: the web UI is at `http://172.16.11.7:8888/` and the web socket at `ws://172.16.11.7:8888/ws`. The SERV no longer exposes WiFi (its bootstrap message reports `hasWifi:false`), so connect the Pi to that Ethernet network rather than expecting a SERV WiFi network. The IP may move if the internal network reassigns it, and the Origin header is not required (the SERV only rejects a wrong one). I run the whole thing over Tailscale but thats not a requirement.

## Go Heating Client

The Go heating client lives under `cmd/heatingctl/main.go` and `heating/`.

Run the Go tests:

```bash
go test ./...
```

Lint the static web UI JavaScript:

```bash
npm ci
rtk lint eslint web/static/app.js
```

Build and run the CLI:

```bash
PATH=/opt/homebrew/bin:/opt/homebrew/opt/go/bin:$PATH go run ./cmd/heatingctl ensure-on --verbose
PATH=/opt/homebrew/bin:/opt/homebrew/opt/go/bin:$PATH go run ./cmd/heatingctl get-target-temp --verbose
PATH=/opt/homebrew/bin:/opt/homebrew/opt/go/bin:$PATH go run ./cmd/heatingctl set-target-temp --value 20.0 --verbose
```

Capture Garmin websocket traffic without browser developer tools:

```bash
go run ./cmd/wscapture -out captures/grey-water.ndjson
```

Leave it running while pressing the Garmin UI controls, then stop it with `Ctrl-C`. The capture is newline-delimited JSON with timestamps, direction, parsed frame fields, signal id, value, and the raw websocket message.

The Go client currently:

- replays the Garmin bootstrap and heartbeat traffic
- tracks heater state from websocket messages
- decodes target temperature from the observed `signal 105` payloads
- uses press and release semantics for temperature up and down
- supports explicit heater power-off via the browser-confirmed `signal 101` off command
- prints relevant heater frames in verbose mode during an operation and for a short window afterwards

## EmpireBus Service

The service daemon entrypoint lives at `cmd/empirebusd/main.go`.

Start from the sample config in `config.example.yaml`, then run:

```bash
go run ./cmd/empirebusd -config ./config.example.yaml
```

The daemon also serves a small mobile-first web UI from the same process:

- `GET /`
- `GET /ui`
- `GET /static/...`

The UI is plain embedded HTML/CSS/JavaScript, uses the same `/v1/...` API as other clients, and listens to `GET /v1/events` rather than polling.

The sample config includes:

- the everyday morning heating schedule from `05:30` to `08:00`
- a commented short test pattern you can edit for quick live verification
- optional RUTX50-backed GPS location polling and timezone updates

Current HTTP endpoints:

- `GET /v1/health`
- `GET /v1/build`
- `GET /v1/location/state`
- `GET /v1/heating/state`
- `GET /v1/heating/mode`
- `POST /v1/heating/mode/schedule`
- `POST /v1/heating/mode/off`
- `POST /v1/heating/mode/manual`
- `POST /v1/heating/mode/boost`
- `POST /v1/heating/mode/boost/cancel`
- `POST /v1/heating/power`
- `POST /v1/heating/target-temperature`
- `GET /v1/automation/heating-programs`
- `GET /v1/automation/heating-schedule`
- `PUT /v1/automation/heating-schedule`
- `GET /v1/lights/state`
- `POST /v1/lights/external/flash`
- `GET /v1/water/state`
- `POST /v1/water/grey-valve/open`
- `POST /v1/water/grey-valve/close`
- `GET /v1/recording/state`
- `POST /v1/recording/start`
- `POST /v1/recording/stop`
- `GET /v1/events`

### WebSocket recording

The Settings tab controls an on-demand recording of the daemon's existing Garmin WebSocket session. Choose whether to start immediately or wait for an engine-on, heating-on, or Victron-inverter-on indication, enter a duration in whole minutes, then press **Start recording**. The status and remaining duration update through the `recording.state_changed` event on `GET /v1/events`; press **Stop recording** to cancel either an armed or active recording.

`POST /v1/recording/start` accepts this JSON body and returns the current recording state:

```json
{"wait_for":"immediate","duration_minutes":1}
```

`wait_for` may be `immediate`, `engine_on`, `heating_on`, or `victron_on`. A delayed recording only begins when the daemon receives an on-frame after it is armed: signal `11` for engine running, signal `101` for heating on, or signal `197` for the Victron inverter on. `GET /v1/recording/state` and `POST /v1/recording/stop` also return that state. Stop is idempotent and takes priority over both an armed wait condition and a running duration timer.

Recordings are written to `/var/lib/xtura/recordings/` as unique UTC filenames such as `garmin-ws-20260812T153045Z.ndjson` (a numeric suffix is added if needed). Each newline-delimited JSON record has `at`, `direction`, `message`, and `message_len`; parsed Garmin frames additionally include `frame`, `signal`, and `value` when available, while unparsable frames include `error`. Lifecycle records use `direction: "event"` and an `event` value: `recording_started`, `timeout`, `stopped`, or `service_shutdown`.

A duration of `0` records until stopped or the service restarts. Armed and active recordings are not restored after restart; service shutdown cancels them and appends a `service_shutdown` lifecycle record to an active trace where possible.

The location service defaults to the Teltonika RUTX50 GPS position endpoint at `http://192.168.51.1/api/gps/position/status` when `location.enabled` is true. It exposes the latest longitude, latitude, and timezone at `GET /v1/location/state`; see [location-service.md](docs/location-service.md) for the RUTX50 endpoint config, timezone lookup, and Pi timezone update setup.

Current design notes live in:

- [2026-04-21-empirebus-service-design.md](docs/superpowers/specs/2026-04-21-empirebus-service-design.md)
- [2026-04-21-heating-go-client-design.md](docs/superpowers/specs/2026-04-21-heating-go-client-design.md)
- [heating-schedule-api.md](docs/heating-schedule-api.md)
- [garmin-empirbus-signals.md](docs/garmin-empirbus-signals.md) — source-backed Garmin WDU WebSocket protocol, signal catalogue, and capture evidence
- [location-service.md](docs/location-service.md)

## Deployment

The current deployment path is Pi-local build/test/deploy, run as a local user with passwordless sudo on the Pi host, i wound back work on GitHub Actions because complexity.

Useful files:

- Pi-local deploy script: [deploy-on-pi.sh](scripts/deploy/deploy-on-pi.sh)
- Mac helper to trigger deploy remotely: [run-deploy-from-mac.sh](scripts/deploy/run-deploy-from-mac.sh)
- `systemd` unit: [empirebusd.service](ops/systemd/empirebusd.service)

Expected host layout:

- repo checkout for a local user with passwordless sudo, for example `/home/local-user/development/xtura-automation`
- deployed releases in `/opt/xtura/releases/<git-sha>`
- active symlink at `/opt/xtura/current`
- writable service config at `/var/lib/xtura/config.yaml`
- runtime mode state at `/var/lib/xtura/config.yaml.runtime.yaml`

Typical deploy flow on the Pi:

```bash
cd /home/local-user/src/xtura-automation
./scripts/deploy/deploy-on-pi.sh
```

Typical remote trigger from the Mac:

```bash
./scripts/deploy/run-deploy-from-mac.sh
```

On the Raspberry Pi, stop the running service without changing boot startup:

```bash
sudo systemctl stop empirebusd.service
```

Stop the service and prevent it from starting on boot:

```bash
sudo systemctl disable --now empirebusd.service
```

Start the service again and re-enable boot startup:

```bash
sudo systemctl enable --now empirebusd.service
```

### GitHub Actions Attempt

The GitHub Actions deployment attempt was preserved up to commit `99c9c73fe8932255e3b60caa37cc96e275b77124`.

State reached there:

- GitHub Actions workflow could build and start the Tailscale join flow
- Tailscale OAuth/tag setup was partially working after switching to lowercase `tag:xtura-ci`
- the CI runner could reach the Pi over Tailscale DNS
- SSH auth still fell through to normal `publickey,password`, which meant the setup still needed more Tailscale SSH policy or key-based SSH work

Known lessons from that attempt:

- Tailscale tags must match exactly, including case
- OAuth client permissions needed both device write and auth key write
- `scp` uses `-P` for port while `ssh` uses `-p`
- the extra CI-to-tailnet auth and Tailscale SSH policy work was more setup than wanted for on-the-road fixes

That workflow-based path has now been removed from the repo in favor of the simpler Pi-local deploy flow.
