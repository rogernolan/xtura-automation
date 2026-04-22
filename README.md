# EmpireBus Go Heating Service

This repository is now centered on the Go heating client and the EmpireBus service work that wraps it.
The older Python CLI, recorder, and test tooling have been split into a separate peer repository so this repo can stay focused on the Go control path and the Garmin investigation notes.

## Go Heating Client

The Go heating client lives under [`cmd/heatingctl`](/Users/rog/Development/empirebus-tests/cmd/heatingctl/main.go) and [`heating/`](/Users/rog/Development/empirebus-tests/heating).

Run the Go tests:

```bash
cd /Users/rog/Development/empirebus-tests
PATH=/opt/homebrew/bin:/opt/homebrew/opt/go/bin:$PATH go test ./...
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

The service daemon entrypoint lives at [`cmd/empirebusd/main.go`](/Users/rog/Development/empirebus-tests/cmd/empirebusd/main.go).

Start from the sample config in [config.example.yaml](/Users/rog/Development/empirebus-tests/config.example.yaml), then run:

```bash
cd /Users/rog/Development/empirebus-tests
PATH=/opt/homebrew/bin:/opt/homebrew/opt/go/bin:$PATH go run ./cmd/empirebusd -config ./config.example.yaml
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

- [2026-04-21-empirebus-service-design.md](/Users/rog/Development/empirebus-tests/docs/superpowers/specs/2026-04-21-empirebus-service-design.md)
- [2026-04-21-heating-go-client-design.md](/Users/rog/Development/empirebus-tests/docs/superpowers/specs/2026-04-21-heating-go-client-design.md)
- [heating-schedule-api.md](/Users/rog/Development/xtura-automation/docs/heating-schedule-api.md)
- [garmin-empirbus-signals.md](/Users/rog/Development/empirebus-tests/docs/garmin-empirbus-signals.md)

## Python Tooling

The Python CLI, capture recorder, and related tests now live in the peer repo at `/Users/rog/Development/garmin-empirebus-python-tools`.
This repo keeps the Garmin signal investigation docs, HAR captures, and Go implementation work.
