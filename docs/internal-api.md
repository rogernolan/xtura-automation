# Internal HTTP API

Derived from current Go code only. Public authentication, authorization, and TLS behavior are intentionally not implemented in code; the service is designed for a Tailscale-restricted deployment and binds whatever `api.listen` config says.

## Router

| Path | Method | Handler | App method | Success response | Error responses | Tests |
|---|---:|---|---|---|---|---|
| `/v1/health` | GET | `service/api/httpapi/server.go:handleHealth` | `Health()` | `ServiceHealth` | `405` | `TestHandlerRoutesHealth` |
| `/v1/heating/state` | GET | `handleHeatingState` | `HeatingState()` | `heating.State` | `405` | unknown |
| `/v1/heating/power` | POST | `handleHeatingPower` | `EnsurePower(ctx,state)` | `heating.State` | `400` decode, `502` command, `405` | unknown |
| `/v1/heating/target-temperature` | POST | `handleHeatingTargetTemperature` | `SetTargetTemperature(ctx,celsius)` | `heating.State` | `400` decode, `502` command, `405` | unknown |
| `/v1/heating/mode` | GET | `handleHeatingMode` | `HeatingMode()` | `HeatingRuntimeState` | `405` | `TestHandleHeatingModeGet` |
| `/v1/heating/mode/schedule` | POST | `handleHeatingModeSchedule` | `SetHeatingModeSchedule(ctx)` | `HeatingRuntimeState` | `502`, `405` | unknown |
| `/v1/heating/mode/off` | POST | `handleHeatingModeOff` | `SetHeatingModeOff(ctx)` | `HeatingRuntimeState` | `502`, `405` | unknown |
| `/v1/heating/mode/manual` | POST | `handleHeatingModeManual` | `SetHeatingModeManual(ctx,target)` | `HeatingRuntimeState` | `400` decode, `502`, `405` | unknown |
| `/v1/heating/mode/boost` | POST | `handleHeatingModeBoost` | `SetHeatingModeBoost(ctx,target,duration)` | `HeatingRuntimeState` | `400` decode, `502`, `405` | unknown |
| `/v1/heating/mode/boost/cancel` | POST | `handleHeatingModeBoostCancel` | `CancelHeatingModeBoost(ctx)` | `HeatingRuntimeState` | `502`, `405` | `TestHandleHeatingModeBoostCancel` |
| `/v1/automation/heating-programs` | GET | `handleHeatingPrograms` | `HeatingPrograms(time.Now())` | `[]ProgramStatus` | `405` | `TestHandleHeatingProgramsMethod` |
| `/v1/automation/heating-schedule` | GET | `handleHeatingSchedule` | `HeatingSchedule()` | `HeatingScheduleDocument` | `405` | `TestHandleHeatingScheduleGet` |
| `/v1/automation/heating-schedule` | PUT | `handleHeatingSchedule` | `UpdateHeatingSchedule(ctx,doc)` | `HeatingScheduleDocument` | `400` decode/validation, `409` revision conflict, `502`, `405` | `TestHandleHeatingSchedulePutMethodAndBody` |
| `/v1/lights/state` | GET | `handleLightsState` | `LightsState()` | `lights.State` | `405` | `TestHandleLightsStateGet` |
| `/v1/lights/external/flash` | POST | `handleExteriorFlash` | `FlashExteriorLights(ctx,count)` | `lights.State` | `400` decode/invalid count, `409` busy, `502`, `405` | `TestHandleExteriorFlashRejectsBusy`, `TestHandleExteriorFlashRejectsInvalidCount` |
| `/v1/events` | GET | `handleEvents` | `Broker().Subscribe()` | Server-sent events | `500` if no flusher, `405` | unknown |

All mutating HTTP handlers use a `30s` request context timeout in `service/api/httpapi/server.go`.

## JSON Shapes

| Name | JSON shape | Defined in | Notes |
|---|---|---|---|
| `ServiceHealth` | `{"status":string,"started_at":time,"garmin":{"connected":bool,"last_frame_at"?:time,"last_error"?:string},"scheduler_running":bool,"config_loaded":bool}` | `service/domains/heating/types.go` | `status` is `ok` when Garmin is connected, else `degraded`. |
| `heating.State` | `{"power_state":string,"ready":bool,"target_temperature_c"?:number,"target_temperature_known":bool,"last_updated_at"?:time,"last_command_error"?:string}` | `service/domains/heating/types.go` | Power values: `unknown`, `off`, `on`, `transition`. |
| `HeatingRuntimeState` | `{"mode":string,"manual_target_celsius"?:number,"boost"?:HeatingBoostState,"updated_at":time}` | `service/config/runtime_state.go` | Modes: `schedule`, `off`, `manual`, `boost`. |
| `HeatingBoostState` | `{"target_celsius":number,"expires_at":time,"resume_mode":string,"resume_manual_target_celsius"?:number}` | `service/config/runtime_state.go` | `resume_mode` must not be `boost`. |
| `HeatingScheduleDocument` | `{"timezone":string,"programs":[HeatingScheduleProgramDocument],"revision"?:string}` | `service/config/config.go` | Full replacement document for PUT. |
| `HeatingScheduleProgramDocument` | `{"id":string,"enabled":bool,"days":[string],"periods":[HeatingSchedulePeriodDocument]}` | `service/config/config.go` | Weekday tokens accepted by parser include short and long English forms. |
| `HeatingSchedulePeriodDocument` | `{"start":"HH:MM","mode":"off|heat","target_celsius"?:number}` | `service/config/config.go` | `target_celsius` required for `heat`, forbidden for `off`. |
| `ProgramStatus` | `{"id":string,"enabled":bool,"days":[number],"periods":[HeatingPeriod],"active_period":HeatingPeriod,"next_period":HeatingPeriod,"next_transition_at"?:time,"action":Action}` | `service/runtime/app.go` | Days are Go `time.Weekday` JSON numbers. |
| `HeatingPeriod` | `{"start":{"Hour":number,"Minute":number},"mode":"off|heat","target_celsius"?:number}` | `service/domains/heating/types.go` | `LocalTime` has no custom JSON tags, so field names are `Hour` and `Minute`. |
| `Action` | `{"kind":string,"target_celsius"?:number}` | `service/automation/scheduler/scheduler.go` | Kinds: `noop`, `ensure_on_and_set_target`, `set_target`, `ensure_off`. |
| `lights.State` | `{"external_known":bool,"external_on":bool,"flash_in_progress":bool,"last_command_error"?:string,"last_updated_at"?:time}` | `service/domains/lights/types.go` | Unknown exterior state has `external_known:false`. |
| `Event` | `{"type":string,"timestamp":time,"correlation_id"?:string,"payload"?:any}` | `service/api/events/broker.go` | SSE emits `event: <type>` and `data: <Event JSON>`. |

## Request Bodies

| Endpoint | Body | Validation source | Notes |
|---|---|---|---|
| `POST /v1/heating/power` | `{"state":"on"}` or `{"state":"off"}` | `runtime.App.EnsurePower` | Other strings return an error that HTTP maps to `502`. |
| `POST /v1/heating/target-temperature` | `{"celsius":20.0}` | `runtime.App.SetTargetTemperature`, `heating.Client.SetTargetTemp` | Must be finite, in `0.5C` increments, at least `5.0C`, and less than `25.0C`. |
| `POST /v1/heating/mode/manual` | `{"target_celsius":19.0}` | `config.HeatingRuntimeState.Validate` plus hardware client | `target_celsius` must be finite, in `0.5C` increments, at least `5.0C`, and less than `25.0C`. |
| `POST /v1/heating/mode/boost` | `{"target_celsius":22.0,"duration_minutes":60}` | `runtime.App.SetHeatingModeBoost` | Duration must be greater than zero; `target_celsius` uses the same safe range as manual mode. |
| `PUT /v1/automation/heating-schedule` | `HeatingScheduleDocument` | `config.Config.WithHeatingSchedule`, `Validate` | `revision` is optional; if both current and supplied revisions exist and differ, returns `409`; heat periods use the same safe target range. |
| `POST /v1/lights/external/flash` | `{"count":1}` | `runtime.App.FlashExteriorLights` | Count must be `1..5`. |

## Event Types

| Event type | Publisher | Payload | Notes |
|---|---|---|---|
| `heating.state_changed` | `runtime.App.publishStateLoop` | `heating.State` | Published when current heating state changes. |
| `service.connection_changed` | `runtime.App.publishStateLoop` | `AdapterHealth` | Published when Garmin connected flag changes. |
| `automation.schedule_updated` | `runtime.App.UpdateHeatingSchedule` | `HeatingScheduleDocument` | After config save/reload succeeds. |
| `automation.run_started` | `runtime.App.executeTransition` | map with `program_id`, `next_transition_at`, `action` | Has `correlation_id`. |
| `automation.run_failed` | `runtime.App.executeTransition` | map with `program_id`, `error` | Has same run correlation id. |
| `automation.run_succeeded` | `runtime.App.executeTransition` | map with `program_id`, `action` | Has same run correlation id. |
| `heating.mode_changed` | `runtime.App.setHeatingMode`, `reconcileExpiredBoost` | `HeatingRuntimeState` | Published after mode changes or expired boost collapse. |

## Implementation Map

| Layer | Files | Responsibility |
|---|---|---|
| HTTP | `service/api/httpapi/server.go` | Route registration, JSON decode/encode, HTTP status mapping, SSE stream. |
| Runtime app | `service/runtime/app.go`, `service/runtime/mode.go`, `service/runtime/lights.go` | Service state, scheduler loop, runtime modes, schedule persistence, light flash orchestration. |
| Config | `service/config/config.go`, `service/config/runtime_state.go` | YAML load/save, schedule document conversion, validation, runtime mode state persistence. |
| Domain types | `service/domains/heating/types.go`, `service/domains/lights/types.go` | JSON structs and validation helpers. |
| Scheduler | `service/automation/scheduler/scheduler.go` | Active/next period calculation and action derivation. |
| Garmin adapter | `service/adapters/garmin/adapter.go` | Bridges runtime controllers to the lower-level websocket client. |
| Websocket client | `heating/*.go` | Garmin websocket session, wire frame parsing, command frames, signal-derived heater state. |
| Daemon | `cmd/empirebusd/main.go` | Config load, app startup, HTTP server lifecycle. |

## Known Omissions / Design Choices

| Area | Current choice | Rationale / mitigation |
|---|---|---|
| API auth and TLS | No in-process authentication, authorization, or TLS middleware. | Deployment assumes the HTTP API is reachable only over the operator's Tailscale network. If the Pi is also reachable on Wi-Fi/Ethernet, bind `api.listen` to a Tailscale-only address or put the service behind a Tailscale-facing local proxy. |
| Deploy target validation | `scripts/deploy/deploy-on-pi.sh` is an operator-only helper and does not currently harden arbitrary `TARGET_SHA` input before using it as the release directory name. | Treat the script as trusted local operational tooling. For untrusted callers or automation, resolve the argument with `git rev-parse --verify` and use the resulting full commit hash for filesystem paths. |
