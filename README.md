# EmpireBus Go Heating Service

This repository is now centered on the Go heating client and the EmpireBus service work that wraps it.
The older Python CLI, recorder, and test tooling have been split into a separate peer repository so this repo can stay focused on the Go control path and the Garmin investigation notes.

## Go Heating Client

The Go heating client lives under `cmd/heatingctl/main.go` and `heating/`.

Run the Go tests:

```bash
go test ./...
```

Build and run the CLI:

```bash
PATH=/opt/homebrew/bin:/opt/homebrew/opt/go/bin:$PATH go run ./cmd/heatingctl ensure-on --verbose
PATH=/opt/homebrew/bin:/opt/homebrew/opt/go/bin:$PATH go run ./cmd/heatingctl get-target-temp --verbose
PATH=/opt/homebrew/bin:/opt/homebrew/opt/go/bin:$PATH go run ./cmd/heatingctl set-target-temp --value 20.0 --verbose
```

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

The sample config includes:

- the everyday morning heating schedule from `05:30` to `08:00`
- a commented short test pattern you can edit for quick live verification

Current HTTP endpoints:

- `GET /v1/health`
- `GET /v1/heating/state`
- `GET /v1/heating/mode`
- `POST /v1/heating/mode/schedule`
- `POST /v1/heating/mode/off`
- `POST /v1/heating/mode/manual`
- `POST /v1/heating/mode/boost`
- `POST /v1/heating/power`
- `POST /v1/heating/target-temperature`
- `GET /v1/automation/heating-programs`
- `GET /v1/automation/heating-schedule`
- `PUT /v1/automation/heating-schedule`
- `GET /v1/events`

Current design notes live in:

- [2026-04-21-empirebus-service-design.md](docs/superpowers/specs/2026-04-21-empirebus-service-design.md)
- [2026-04-21-heating-go-client-design.md](docs/superpowers/specs/2026-04-21-heating-go-client-design.md)
- [heating-schedule-api.md](docs/heating-schedule-api.md)
- [garmin-empirbus-signals.md](docs/garmin-empirbus-signals.md)

## Deployment

The current preferred deployment path is Pi-local build/test/deploy, run as a local user with passwordless sudo on the Pi host, not GitHub Actions.

Useful files:

- Pi-local deploy script: [deploy-on-pi.sh](scripts/deploy/deploy-on-pi.sh)
- Mac helper to trigger deploy remotely: [run-deploy-from-mac.sh](scripts/deploy/run-deploy-from-mac.sh)
- `systemd` unit: [empirebusd.service](ops/systemd/empirebusd.service)

Expected host layout:

- repo checkout for a local user with passwordless sudo, for example `/home/local-user/src/xtura-automation`
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

## Python Tooling

The Python CLI, capture recorder, and related tests now live in a peer repository for the Python tooling.
This repo keeps the Garmin signal investigation docs, HAR captures, and Go implementation work.
