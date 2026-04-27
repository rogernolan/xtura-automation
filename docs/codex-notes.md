# Codex Notes

Derived from current repo files only.

## Run/Test

| Task | Command | Notes |
|---|---|---|
| Run all tests | `go test ./...` | Current tests are designed to avoid live hardware. |
| Run daemon with sample config | `go run ./cmd/empirebusd -config ./config.example.yaml` | This can connect to the configured Garmin websocket and control hardware if reachable. |
| Run heating CLI ensure on | `go run ./cmd/heatingctl ensure-on --verbose` | Live hardware command unless `--ws-url` points to a test server. |
| Get target temp | `go run ./cmd/heatingctl get-target-temp --verbose` | Live websocket read. |
| Set target temp | `go run ./cmd/heatingctl set-target-temp --value 20.0 --verbose` | Live command; 0.5C increments only. |

## Deploy

| File | Role | Notes |
|---|---|---|
| `scripts/deploy/deploy-on-pi.sh` | Pi-local deploy | Fetches/pulls or checks out target SHA, runs `go test ./...`, builds `cmd/empirebusd`, installs under `/opt/xtura/releases/<sha>`, restarts systemd, curls health. |
| `scripts/deploy/run-deploy-from-mac.sh` | Remote deploy trigger | SSHes to `${PI_USER}@${PI_HOST}` and runs the Pi deploy script in `${REMOTE_REPO}`. |
| `ops/systemd/empirebusd.service` | Service unit | Runs as `xtura:xtura`, working directory `/opt/xtura/current`, config `/var/lib/xtura/config.yaml`, restart on failure. |
| Runtime state | `/var/lib/xtura/config.yaml.runtime.yaml` | Inferred from `runtimeStatePath(configPath)`. |

## Repo Map

| Path | Purpose |
|---|---|
| `cmd/empirebusd` | Service daemon entrypoint. |
| `cmd/heatingctl` | Manual heating CLI for websocket operations. |
| `service/api/httpapi` | Internal REST/SSE API. |
| `service/runtime` | Application orchestration, scheduler loop, modes, light flash. |
| `service/config` | YAML config, schedule document, runtime state validation/persistence. |
| `service/automation/scheduler` | Schedule calculations. |
| `service/adapters/garmin` | Runtime-to-Garmin adapter. |
| `service/domains` | JSON/domain structs. |
| `heating` | Low-level Garmin websocket session/client/frame parsing. |
| `docs/garmin-empirbus-signals.md` | Signal reference that must stay current when mappings change. |
| `docs/heating-schedule-api.md` | Existing client-facing schedule/mode API note. |

## Future Codex Safety Checklist

| Before doing this | Check |
|---|---|
| Running tests | `go test ./...` is acceptable; it should not use hardware. |
| Running daemon or CLI | Confirm target `ws_url`/`--ws-url`; default/sample values may reach real hardware. |
| Editing schedule/runtime behavior | Update `docs/internal-api.md`, `docs/domain-model.md`, and `docs/control-safety.md` if structs, endpoints, modes, or safety semantics change. |
| Adding/relying on signals | Update `docs/garmin-empirbus-signals.md` with source/capture evidence per `AGENTS.md`. |
| Changing HTTP behavior | Update endpoint table with method, body, response, handler, and tests. |
| Adding production exported Go identifiers | Add comments only if needed for Go lint/doc expectations; this doc task did not require production code changes. |

## Known Code-Derived Gaps

| Gap | Current state |
|---|---|
| API authentication | Unknown/not implemented in code. |
| OpenAPI/schema generation | None discovered. |
| HTTP tests for several endpoints | Some routes lack direct tests, especially power/target temperature/mode POST success paths and SSE. |
| Temperature bounds | Unknown beyond `0.5C` increments. |
| Dry-run mode | None discovered for daemon/CLI hardware commands. |

