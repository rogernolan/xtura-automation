# Repository Instructions

## Local Development (Sim)

The simulated Mac environment is the primary way to test UI and API changes locally.
Never manually rebuild or restart individual binaries — use the sim script:

    ./scripts/sim/run-sim.sh              # uses newest captures/garmin-ws-*.ndjson
    ./scripts/sim/run-sim.sh captures/my.ndjson

This builds both `servsim` and `empirebusd` from the current checkout, starts
`servsim` on `ws://localhost:8090/ws` with a recorded capture, and `empirebusd`
on `http://localhost:8091` with `config.sim.yaml`.

Environment variable `XTURA_SIM_SWITCHBOT=1` feeds synthetic SwitchBot BLE
readings when the service starts (set in `config.sim.yaml`).

The Go binary embeds static web assets at compile time (`//go:embed` in `web/web.go`).
Any change to `web/static/` requires a full rebuild via `run-sim.sh`.

## Staging Deploy

Deploy the current checkout to the Pi staging environment:

    ENVIRONMENT=staging ./scripts/deploy/run-deploy-from-mac.sh HEAD

This runs tests, builds the binary, copies it to the Pi, installs the systemd
service, and restarts it. The staging service listens on port 8080.

## Garmin EmpirBus Signal Reference

Keep [docs/garmin-empirbus-signals.md](/Users/rog/Development/empirebus-tests/docs/garmin-empirbus-signals.md) up to date whenever any of these change:

- a new HAR or NDJSON capture reveals a command or state mapping
- a script in this repo starts relying on a new signal or command shape
- a domain grouping changes
- the Code Red module provenance or GitHub source becomes known

When updating that document:

- prefer browser-confirmed facts over inference
- label inferred mappings explicitly
- include the local file or external source that supports each new claim
- add dates or capture filenames when they matter
- keep a section for each domain and list known commands plus their arguments

If the exact Code Red module GitHub URL becomes known, add it to the signal reference document immediately and separate Code Red-derived knowledge from repo-local capture evidence.
