# Domain Model

Derived from current Go code only. Signal meanings are documented only where current code names or uses them.

## Heating

| Concept | Values / fields | Source | Semantics |
|---|---|---|---|
| Power state | `unknown`, `off`, `on`, `transition` | `service/domains/heating/types.go`, `heating/state.go` | Derived from Garmin signal `101` values `0`, `1`, `129`; command values are separate. |
| Ready | `power_state == on && busy_known && !busy` | `heating/state.go` | Used by `EnsureOn` to decide if the heater is usable. |
| Target temperature | `target_temperature_c`, `target_temperature_known` | `heating/state.go` | Decoded from signal `105`; known sample tests cover `8.0`, `10.0`, `13.0`, `20.0`. |
| Heating command state | `last_command_error` | `service/domains/heating/types.go`, `service/adapters/garmin/adapter.go` | Cleared after successful adapter command, set on command failure. |
| Runtime mode | `schedule`, `off`, `manual`, `boost` | `service/config/runtime_state.go`, `service/runtime/mode.go` | Persisted to `<config>.runtime.yaml`; mode controls whether schedule or explicit command path applies. |
| Boost | `target_celsius`, `expires_at`, `resume_mode`, optional resume manual target | `service/config/runtime_state.go` | Temporarily applies heat target, then resumes previous non-boost mode when canceled or expired. |

## Heating Schedule

| Concept | Rule | Source | Tests |
|---|---|---|---|
| Program | Requires non-empty `id`, at least one day, at least one period | `HeatingProgram.Validate` | `TestLoadFileAndNormalize` |
| Day ownership | Enabled programs may not overlap days | `Config.Validate` | `TestValidateRejectsOverlappingProgramDays` |
| Weekdays | `sun/sunday`, `mon/monday`, `tue/tues/tuesday`, `wed/wednesday`, `thu/thur/thurs/thursday`, `fri/friday`, `sat/saturday` | `weekdayByName` | `TestValidateRejectsOverlappingProgramDays` uses `mon` and `monday`. |
| Period start | `HH:MM`; first period must start `00:00`; later periods must increase | `parseLocalTime`, `HeatingProgram.Validate` | `TestLoadFileAndNormalize` |
| Period mode | `off` or `heat` | `parseMode`, `HeatingPeriod.Validate` | `TestValidateRejectsMissingHeatTarget` |
| `off` period | Must not set `target_celsius` | `HeatingPeriod.Validate` | unknown direct test |
| `heat` period | Must set `target_celsius` | `HeatingPeriod.Validate` | `TestValidateRejectsMissingHeatTarget` |
| Adjacent matching periods | Consecutive periods with same effective state are allowed; runtime scheduling skips no-op transitions | `HeatingProgram.Validate`, `SameEffectiveState`, `nextDistinctTransition` | `TestValidateAllowsAdjacentPeriodsWithSameEffectiveState` |
| Revision | File modtime formatted RFC3339Nano | `readConfigRevision` | `TestHeatingScheduleDocumentRoundTrip` covers document flow, not file modtime. |

## Scheduling Semantics

| Behavior | Source | Tests |
|---|---|---|
| `Calculate` finds the active period and next distinct transition for one program in configured timezone. | `service/automation/scheduler/scheduler.go` | `TestCalculateSkipsDaysWithoutProgramAndFindsNextHeatTransition` |
| Disabled or non-applicable days behave as an all-day `off` period for that program. | `periodsForDay` | Inferred from scheduler code; no direct named test. |
| Spring-forward gaps resolve to the first valid local time at or after the requested `HH:MM`. | `resolveLocalTime` | `TestCalculateSpringForwardTransitionsToFirstValidTimeAfterGap` |
| Fall-back repeated hour uses the first occurrence only, then later transitions continue normally. | `resolveLocalTime` | `TestCalculateFallBackUsesFirstOccurrenceOnly` |
| `off -> heat` action is `ensure_on_and_set_target`; `heat -> heat` is `set_target`; `heat -> off` is `ensure_off`; equal state is skipped. | `deriveAction`, `nextDistinctTransition` | Partly covered by scheduler tests and sample config comments. |

## Runtime Mode Semantics

| Mode | Apply behavior | Persistence | Notes |
|---|---|---|---|
| `schedule` | Reconcile current schedule period, then scheduler loop drives future transitions. | Saved to runtime state file. | On schedule update, app reconciles current state and wakes scheduler. |
| `off` | Calls adapter `EnsureOff`; scheduler waits until mode changes. | Saved. | Ignores schedule while active. |
| `manual` | Calls `EnsureOn`, then `SetTargetTemperature(manual_target_celsius)`; scheduler waits. | Saved. | Requires `manual_target_celsius`. |
| `boost` | Calls `EnsureOn`, then `SetTargetTemperature(boost.target_celsius)`; scheduler waits until expiry or mode wake. | Saved. | Expired boost collapses to saved resume mode on startup or scheduler reconcile. |

## Lights

| Concept | Values / fields | Source | Semantics |
|---|---|---|---|
| Exterior state | `external_known`, `external_on`, `last_updated_at` | `service/domains/lights/types.go`, `service/adapters/garmin/adapter.go` | Tracks latest received on/off confirmation from signal `47` or `48`. |
| Exterior on command | signal `47`, value `3` | `Adapter.EnsureExteriorOn` | Waits for received signal `47` to become on after send timestamp. |
| Exterior off command | signal `48`, value `3` | `Adapter.EnsureExteriorOff` | Waits for received signal `48` to become on after send timestamp. |
| Flash | Count `1..5`, 500ms on/off interval | `service/runtime/lights.go` | Serializes with `flash_in_progress`; restores previous known on state, otherwise restores off. |
| Stale confirmations | Ignored for exterior commands | `WaitForSignalIsOnAfter`, adapter tests | Confirmation timestamp must be after command send time. |

## Garmin Wire Semantics Used

| Signal | Code use | Meaning in code | Values used |
|---:|---|---|---|
| `101` | Heating power state and commands | Alde heating power | State: `0` off, `1` on, `129` transition; command: `3` on, `5` off. |
| `102` | Heating busy state | Busy flag | `0` false, `1` true. |
| `105` | Target temperature decode | Heating target temperature | Encoded payload bytes; see `decodeTargetTemperature`. |
| `107` | Temperature up button | Press/release command | Sent with `messagecmd:1`, value `1` then `0`. |
| `108` | Temperature down button | Press/release command | Sent with `messagecmd:1`, value `1` then `0`. |
| `119` | Pump running state | Pump flag | `0` false, `1` true. |
| `47` | Exterior on | Inferred from adapter/tests as exterior on command/confirmation | Command value `3`; received value `1` means on confirmation. |
| `48` | Exterior off | Inferred from adapter/tests as exterior off command/confirmation | Command value `3`; received value `1` means off confirmation. |

## Unknowns

| Area | Unknown |
|---|---|
| Temperature bounds | Code enforces `0.5C` increments but no min/max range. |
| API auth | No auth is implemented in HTTP server code. Deployment/network access policy is outside code. |
| Non-exterior lights | Signal labels exist in captures/docs, but current service API only controls exterior flash/on/off through signals `47`/`48`. |
| Hardware failure recovery | Adapter records command errors and reconnects websocket sessions; deeper retry/backoff semantics beyond 1-second loop are not modeled. |
