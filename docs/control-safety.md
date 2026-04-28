# Control Safety

Derived from current Go code only. This service can send hardware commands over the Garmin websocket; tests should prefer fakes or local httptest websocket servers.

## Safety Rules

| Area | Rule | Source |
|---|---|---|
| Tests | `go test ./...` should be safe to run without hardware; current tests use temp files, fake controllers, HAR replay, or `httptest` websocket servers. | `*_test.go` files |
| Live commands | Do not run `cmd/heatingctl` or `cmd/empirebusd` against `ws://192.168.1.1:8888/ws` unless intentionally controlling the vehicle hardware. | `heating/session.go`, `config.example.yaml` |
| HTTP mutators | Treat POST/PUT endpoints as live controls when service is pointed at the real Garmin websocket. | `service/api/httpapi/server.go` |
| Schedule edits | `PUT /v1/automation/heating-schedule` rewrites the YAML config and reconciles current schedule state. | `runtime.App.UpdateHeatingSchedule` |
| Runtime mode edits | Mode changes immediately apply hardware commands for `off`, `manual`, and `boost`, then persist to `<config>.runtime.yaml` after apply succeeds. | `service/runtime/mode.go` |
| Temperature changes | Target changes require a finite setpoint in `0.5C` increments where `5.0C <= target < 25.0C`, then call `EnsureOn` before stepping temperature. | `domainheating.ValidateTargetCelsius`, `heating.Client.SetTargetTemp` |
| Exterior flash | Flash sends hardware commands repeatedly and restores previous known on/off state when possible. | `runtime.App.FlashExteriorLights` |
| Websocket confirmations | Exterior commands wait for post-send received confirmation, not just command write success. | `Adapter.ensureExteriorState`, `WaitForSignalIsOnAfter` |

## Hardware Command Paths

| User/API action | Internal path | Hardware frames/signals | Safety behavior |
|---|---|---|---|
| `POST /v1/heating/power {"state":"on"}` | HTTP -> `App.EnsurePower` -> `Adapter.EnsureOn` -> `Client.EnsureOn` | `SignalHeatingPower` (`101`) command value `3` if not already on | Waits up to 20s for `Ready()`. |
| `POST /v1/heating/power {"state":"off"}` | HTTP -> `App.EnsurePower` -> `Adapter.EnsureOff` -> `Client.EnsureOff` | `SignalHeatingPower` (`101`) command value `5` | Idempotent if already off; waits up to 20s for off state. |
| `POST /v1/heating/target-temperature` | HTTP -> `App.SetTargetTemperature` -> `Client.SetTargetTemp` | `107` or `108` press/release steps | Requires a finite target in `0.5C` increments where `5.0C <= target < 25.0C`; waits after each step and detects overshoot. |
| Schedule transition to heat | Scheduler -> `applyPeriod` | `EnsureOn`, then `SetTargetTemperature` | Requires `target_celsius`; unsupported modes error. |
| Schedule transition to off | Scheduler -> `applyPeriod` | `EnsureOff` | No target change. |
| `POST /v1/heating/mode/manual` | HTTP -> `SetHeatingModeManual` -> `applyRuntimeMode` | `EnsureOn`, then target set | Applies hardware command first, then persists runtime mode only after apply succeeds. |
| `POST /v1/heating/mode/boost` | HTTP -> `SetHeatingModeBoost` -> `applyRuntimeMode` | `EnsureOn`, then target set | Rejects non-positive duration; applies hardware command first; resumes previous mode on cancel/expiry. |
| `POST /v1/lights/external/flash` | HTTP -> `FlashExteriorLights` -> adapter light commands | Signal `47` command value `3`, signal `48` command value `3` | Count limited to 1..5; rejects concurrent flash; 500ms interval; attempts restore. |

## Validation and Guards

| Guard | Behavior | Source |
|---|---|---|
| Unsupported power state | Returns error from runtime; HTTP currently maps it to `502`. | `App.EnsurePower`, `handleHeatingPower` |
| Invalid JSON | Returns `400 {"error":"decode request: ..."}`. | HTTP handlers |
| Schedule validation | Returns `400 {"error":"validation_failed","details":[...]}` for recognized validation text. | `handleHeatingSchedule`, `isValidationError` |
| Schedule revision conflict | Returns `409 {"error":"schedule revision conflict"}`. | `UpdateHeatingSchedule`, `handleHeatingSchedule` |
| Heating target range | Direct commands, manual/boost modes, and schedule heat periods reject targets that are not finite, are outside `5.0C <= target < 25.0C`, or are not in `0.5C` increments. | `domainheating.ValidateTargetCelsius` |
| Invalid flash count | Returns `400 {"error":"invalid flash count"}` and records last light command error. | `FlashExteriorLights`, `handleExteriorFlash` |
| Flash while busy | Returns `409 {"error":"flash_in_progress"}` and records last light command error. | `FlashExteriorLights`, `handleExteriorFlash` |
| Adapter not connected | Command path returns `"garmin adapter not connected"` and records command error. | `Adapter.withClient`, `ensureExteriorState` |

## Test Safety Inventory

| Test area | Files | Hardware interaction |
|---|---|---|
| HTTP routing/status | `service/api/httpapi/server_test.go` | None; fake app only. |
| Config validation/runtime state | `service/config/*_test.go` | None; temp files only. |
| Scheduler calendar behavior | `service/automation/scheduler/scheduler_test.go` | None. |
| Runtime modes/lights | `service/runtime/*_test.go` | None; fake adapters/controllers. |
| Garmin adapter lights | `service/adapters/garmin/adapter_test.go` | None; local `httptest` websocket. |
| Heating client/wire decode | `heating/heating_test.go` | None; HAR replay and local `httptest` websocket. |

## Operational Cautions

| Situation | Caution |
|---|---|
| Editing `config.example.yaml` into a live config | Short test patterns can trigger real schedule transitions quickly. |
| Running daemon locally on a network with Garmin device | Default websocket URL is the real-looking `ws://192.168.1.1:8888/ws`; override before experiments. |
| Testing API with curl against deployed service | `POST` and `PUT` routes are not dry-run operations. |
| Changing signal mappings | Update `docs/garmin-empirbus-signals.md` as required by repository instructions. |

## Known Omissions / Design Choices

| Area | Current choice | Operational note |
|---|---|---|
| API auth and TLS | The daemon does not implement auth/TLS itself. | This is accepted for the current Tailscale-only deployment. Keep `api.listen` bound only where tailnet members can reach it, or add auth/proxying before exposing it to any broader LAN. |
| Deploy helper hardening | The Pi deploy helper assumes a trusted operator-supplied `TARGET_SHA`. | Do not expose the deploy script as a remote API. Harden it with `git rev-parse --verify` before using untrusted ref input. |
