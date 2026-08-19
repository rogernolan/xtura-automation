# EmpireBus Service

A small go service to monitor a garmin empire bus event stream and convert it to a sensible Rest api. includes some automation such as a heating schedule.

the system is designed to run on a raspberry pi and is tested (and to be fair) developed for a EuraMobil stura. Eventual target is a pi zero2w.

The service assumes it can reach the Garmin SERV/WDU web socket. On the EuraMobil the SERV connects over the motorhome's internal Ethernet at `172.16.11.7`: the web UI is at `http://172.16.11.7:8888/` and the web socket at `ws://172.16.11.7:8888/ws`. The SERV no longer exposes WiFi (its bootstrap message reports `hasWifi:false`), so connect the Pi to that Ethernet network rather than expecting a SERV WiFi network. The IP may move if the internal network reassigns it, and the Origin header is not required (the SERV only rejects a wrong one). Xtura is served to clients through Tailscale HTTPS; the Go API binds only to loopback and is not exposed as plain HTTP.

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
- `GET /v1/pi/state`
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
- `GET /v1/tracking/settings`
- `PUT /v1/tracking/settings`
- `GET /v1/tracking/state`
- `GET /v1/tracks`
- `GET /v1/tracks/{name}`
- `DELETE /v1/tracks/{name}`
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

### GPS trails

The Settings tab also controls GPS trail recording. When enabled, the service samples the location provider (the RUTX50 router GPS position endpoint) and writes the points as GeoJSON (RFC 7946) track files to `/var/lib/xtura/tracks/`. Two modes are available:

- **Only when engine is on** (the default): a track file per engine on → off session, using signal `11` from the Garmin frame stream as the engine-running indication. Nothing is recorded until the first engine frame is seen.
- **Continuous**: a track file per UTC day (`track-2026-08-13.geojson`), resumed on restart so a calendar day stays one file.

Sample every N seconds is configurable from `1` to `3600` (default `5`). The switches and interval apply immediately on change; live state and errors stream over the `tracking.state_changed` event on `GET /v1/events`. Tracks are listed, downloaded, and deleted through the `/v1/tracks` API. See [gps-tracking.md](docs/gps-tracking.md) for the track file format and API, which is written for consumers such as the InstaBlog agent.

The location service defaults to the Teltonika RUTX50 GPS position endpoint at `http://192.168.51.1/api/gps/position/status` when `location.enabled` is true. It exposes the latest longitude, latitude, and timezone at `GET /v1/location/state`; see [location-service.md](docs/location-service.md) for the RUTX50 endpoint config, timezone lookup, and Pi timezone update setup.

### Pi status

The Tools tab (previously Settings) also shows a **Pi status** panel between
GPS trails and WebSocket recording. The service samples the host every
`host.sample_interval` seconds (default `5s`) and streams the snapshot over the
`pi.state_changed` event on `GET /v1/events`, so the panel stays live. It shows
CPU model and core count, the 1/5/15-minute load averages, memory usage, disk
usage for the root and `/var/lib/xtura` filesystems, CPU temperature, uptime,
and power quality (under-voltage / throttling from `vcgencmd get_throttled`,
falling back to the `rpi_hwmon` sysfs alarms). Metrics that cannot be read on a
given host (for example when running the service on a non-Pi machine) are
reported as unavailable rather than failing the panel.

Current design notes live in:

- [2026-04-21-empirebus-service-design.md](docs/superpowers/specs/2026-04-21-empirebus-service-design.md)
- [2026-04-21-heating-go-client-design.md](docs/superpowers/specs/2026-04-21-heating-go-client-design.md)
- [heating-schedule-api.md](docs/heating-schedule-api.md)
- [garmin-empirbus-signals.md](docs/garmin-empirbus-signals.md) — source-backed Garmin WDU WebSocket protocol, signal catalogue, and capture evidence
- [location-service.md](docs/location-service.md)
- [gps-tracking.md](docs/gps-tracking.md) — GPS trail track file format, lifecycle, and API for consumers

## Simulated environment (Mac)

For development without touching the real motorhome, run the service against a
fake Garmin SERV (`cmd/servsim`) that replays a recorded NDJSON capture and
echoes command state for the heater, valves, and lights:

```bash
./scripts/sim/run-sim.sh                  # uses the newest captures/garmin-ws-*.ndjson
./scripts/sim/run-sim.sh captures/my.ndjson
```

This starts `servsim` on `ws://localhost:8090/ws` and `empirebusd` on
`http://localhost:8091` with `config.sim.yaml`. Exercise command flows there:
power on/off, set target temperature, grey-valve open/close, and the exterior
light flash. `servsim -help` lists options (`-loop` replays the capture
repeatedly; `-speed` changes replay pacing). Because the fake SERV echo is
simulation behavior, not browser-confirmed evidence, see the simulation note in
[garmin-empirbus-signals.md](docs/garmin-empirbus-signals.md).

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

### Staging environment (Jones Pi)

A second, parallel service instance on the Jones Pi that shares the SERV with
production but runs on its own port, config, and systemd unit:

- releases in `/opt/xtura-staging/releases/<git-sha>`, active link at
  `/opt/xtura-staging/current`
- writable service config at `/var/lib/xtura-staging/config.yaml`
- `empirebusd-staging.service`, HTTP on `:8080`
- Tailscale Serve exposes production HTTPS on port 443 to the production loopback backend and staging HTTPS on port 8443 to the staging loopback backend.

Setup once on the Pi (mirrors production):

```bash
sudo mkdir -p /opt/xtura-staging /var/lib/xtura-staging
sudo cp ~/development/xtura-automation/config.staging.example.yaml /var/lib/xtura-staging/config.yaml
sudo chown -R xtura:xtura /opt/xtura-staging /var/lib/xtura-staging
```

Deploy a build to staging (from the Pi's git checkout) or trigger it from the
Mac:

```bash
ENVIRONMENT=staging ./scripts/deploy/deploy-on-pi.sh            # on the Pi
ENVIRONMENT=staging ./scripts/deploy/run-deploy-from-mac.sh <sha>   # from the Mac
```

Verify the backend locally with `curl http://127.0.0.1:8080/v1/health`; clients
should use the Tailscale HTTPS hostname rather than the loopback HTTP port. The
Pi deploy script configures `tailscale serve` automatically and fails if the
Tailscale CLI is unavailable. To promote a verified build to production, deploy the
same SHA with the default environment.

**Staging talks to the real SERV.** Commands issued from the staging UI affect
the real heater and valves; use it for read/build verification and the Mac
simulation for command testing. The SERV's tolerance for two concurrent
websocket clients is unverified — if staging's connection ever drops
production's, point staging's `garmin.ws_url` at a `servsim` instance instead.

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
