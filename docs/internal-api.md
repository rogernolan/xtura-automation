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
| `/v1/water/state` | GET | `handleWaterState` | `WaterState()` | `water.State` | `405` | `TestHandleWaterStateGet` |
| `/v1/water/grey-valve/open` | POST | `handleGreyWaterValveOpen` | `OpenGreyWaterValve(ctx)` | `water.State` | `409` busy, `502`, `405` | `TestHandleGreyWaterValveOpenRejectsBusy` |
| `/v1/water/grey-valve/close` | POST | `handleGreyWaterValveClose` | `CloseGreyWaterValve(ctx)` | `water.State` | `409` busy, `502`, `405` | unknown |
| `/v1/water/grey-valve/schedule` | POST | `handleGreyWaterSchedule` | `ScheduleGreyWaterOpening(ctx,time,duration)` | `water.State` | `400` decode/validation, `502`, `405` | `TestHandleGreyWaterSchedulePost` |
| `/v1/water/grey-valve/schedule/cancel` | POST | `handleGreyWaterScheduleCancel` | `CancelGreyWaterOpening(ctx)` | `water.State` | `502`, `405` | `TestHandleGreyWaterScheduleCancel` |
| `/v1/location/state` | GET | `handleLocationState` | `LocationState()` | `location.State` | `405` | `TestHandleLocationStateGet` |
| `/v1/pi/state` | GET | `handlePiStatus` | `HostStatus()` | `host.Metrics` | `405` | `TestHandlerServesPiStatus` |
| `/v1/tracking/settings` | GET | `handleTrackingSettings` | `TrackingSettings()` | `tracking.Settings` | `405` | `TestTrackingSettingsRoutes` |
| `/v1/tracking/settings` | PUT | `handleTrackingSettings` | `UpdateTrackingSettings(ctx,settings)` | `tracking.Settings` | `400` decode/validation, `502`, `405` | `TestTrackingSettingsRoutes`, `TestTrackingSettingsUpdateRejectsValidationError` |
| `/v1/tracking/start` | POST | `handleTrackingStart` | `StartTracking(ctx)` | `tracking.State` | `409` engine mode, `502`, `405` | `TestTrackingStartStopRoutes` |
| `/v1/tracking/stop` | POST | `handleTrackingStop` | `StopTracking(ctx)` | `tracking.State` | `409` engine mode, `502`, `405` | `TestTrackingStartStopRoutes` |
| `/v1/tracking/state` | GET | `handleTrackingState` | `TrackingState()` | `tracking.State` | `405` | `TestTrackingStateRoute` |
| `/v1/sensors/settings` | GET | `handleSensorSettings` | `SensorSettings()` | `sensors.Settings` | `405` | unknown |
| `/v1/sensors/settings` | PUT | `handleSensorSettings` | `UpdateSensorSettings(ctx,settings)` | `sensors.Settings` | `400` decode/validation, `502`, `405` | unknown |
| `/v1/sensors/discover` | GET | `handleSensorDiscover` | `SensorDiscover(ctx)` | `[]switchbot.SeenDevice` | `502`, `405` | unknown |
| `/v1/sensors/history/{id}` | GET | `handleSensorHistory` | `SensorHistory(id,720)` | `[]sensors.Sample` | `500`, `405` | unknown |
| `/v1/tracks` | GET | `handleTracks` | `TrackList()` | `[]tracking.FileInfo` | `500`, `405` | `TestTracksRoutes` |
| `/v1/tracks/{name}` | GET | `handleTrack` | `TrackRead(name)` | Raw GeoJSON track file | `400` invalid name, `404` missing, `500`, `405` | `TestTracksRoutes`, `TestTrackRouteRejectsTraversalName` |
| `/v1/tracks/{name}` | DELETE | `handleTrack` | `TrackDelete(name)` | `204` | `400` invalid name, `404` missing, `500`, `405` | `TestTracksRoutes`, `TestTrackDeleteRejectsTraversalName` |
| `/v1/events` | GET | `handleEvents` | `Broker().Subscribe()` | Server-sent events | `500` if no flusher, `405` | unknown |

Most mutating HTTP handlers use a `30s` request context timeout in `service/api/httpapi/server.go`; grey-water valve commands use `12s` around the five-second hold.

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
| `water.State` | `{"valve_known":bool,"valve_moving":bool,"valve_direction"?:string,"command_in_progress":bool,"last_command_error"?:string,"scheduled_opening"?:ScheduledOpening,"last_schedule_message"?:string,"last_schedule_completed_at"?:time,"last_updated_at"?:time}` | `service/domains/water/types.go` | `valve_direction` is `opening` or `closing` while an open/close control signal is active. No final open/closed valve position is inferred. |
| `ScheduledOpening` | `{"open_at":time,"local_time":"HH:MM","timezone":string,"duration_minutes":number,"status":"pending|open","opened_at"?:time}` | `service/domains/water/types.go`, `service/config/water_runtime_state.go` | `open_at` is the fixed UTC instant derived from the next occurrence of `local_time` in the current automation timezone. |
| `location.State` | `{"configured":bool,"known":bool,"provider"?:string,"latitude":number,"longitude":number,"is_moving":bool,"movement_meters"?:number,"timezone"?:string,"system_timezone"?:string,"timezone_updated_at"?:time,"last_updated_at"?:time,"last_error"?:string,"last_error_at"?:time,"timezone_update_mode"?:string}` | `service/domains/location/types.go` | Unconfigured state has `configured:false`. Configured but not yet polled has `known:false`. `is_moving` is inferred from cumulative GPS displacement over the configured movement window. |
| `host.Metrics` | `{"sampled_at":time,"model"?:"string","cores":number,"load":[number,number,number],"memory":{"total_bytes":number,"available_bytes":number,"used_percent":number},"disk"?:[{"mount":"string","total_bytes":number,"used_percent":number}],"temperature_c"?:"number","uptime_seconds"?:"number","power":{"status":"ok|warning|unavailable","under_voltage"?:"bool","throttled"?:"bool","frequency_capped"?:"bool","soft_temp_limit"?:"bool","occurred_since_boot"?:["string"],"raw_throttle"?:"string"},"last_error"?:"string","last_error_at"?:"time"}` | `service/host/metrics.go` | `sampled_at` is when the snapshot was last read. `disk` lists root and `/var/lib/xtura` deduplicated by device; each entry has no free-bytes field. Power booleans, `occurred_since_boot`, and `raw_throttle` are omitted when the power source is unavailable. |
| `tracking.Settings` | `{"when_engine_on":bool,"sample_interval_seconds":number,"directory":string}` | `service/api/httpapi/server.go` (wire DTO), `service/tracking/manager.go` | Wire shape for `GET`/`PUT /v1/tracking/settings`. `directory` is fixed at construction, read-only, and ignored on PUT. `sample_interval_seconds` is a float number. |
| `sensors.Settings` | `{"enabled":bool,"hci_device":string,"sensors":[{"name":string,"mac":string,"primary"?:bool}]}` | `service/domains/sensors/types.go` | Wire shape for `GET`/`PUT /v1/sensors/settings`. `hci_device` defaults to `hci0`; sensor ids are the lowercased, colon-stripped MAC. |
| `switchbot.SeenDevice` | `{"mac":string,"dev_type":number,"last_seen":time,"rssi":number}` | `service/adapters/switchbot/adapter.go` | `dev_type` is the raw SwitchBot device-type byte (`0x54`/`0x74` Meter, `0x77` Outdoor). `GET /v1/sensors/discover` returns the rolling seen table. |
| `sensors.Sample` | `{"t":time,"temp":number,"hum"?:number}` | `service/domains/sensors/types.go` | Per-sensor history line and `GET /v1/sensors/history/{id}` payload, newest last, capped at 720 entries. |
| `tracking.State` | `{"when_engine_on":bool,"sample_interval_seconds":number,"engine_known":bool,"engine_on":bool,"tracking":bool,"current_file"?:string,"point_count":number,"last_sample_at"?:time,"last_error"?:string,"last_error_at"?:time}` | `service/tracking/manager.go` | `tracking` is true while a session is active; `current_file` and `point_count` describe the active session. Omitted fields are absent until set. |
| `tracking.FileInfo` | `{"name":string,"bytes":number,"start_time"?:time,"end_time"?:time,"point_count":number}` | `service/tracking/manager.go` | `start_time`, `end_time`, and `point_count` are parsed from each track file; `start_time`/`end_time` are omitted when the file has no points or does not parse, `point_count` is always present (`0` when nothing parsed). |
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
| `POST /v1/water/grey-valve/open` | none | `runtime.App.OpenGreyWaterValve` | Sends a five-second grey-water open button hold. |
| `POST /v1/water/grey-valve/close` | none | `runtime.App.CloseGreyWaterValve` | Sends a five-second grey-water close button hold. |
| `POST /v1/water/grey-valve/schedule` | `{"target_time":"03:00","duration_minutes":30}` | `runtime.App.ScheduleGreyWaterOpening`, `config.ParseClockTime`, `config.WaterRuntimeState.Validate` | Stores a one-off grey-water opening in `<config>.water-runtime.yaml`. The target is resolved in the current automation timezone and persisted as UTC so timezone changes do not move it. |
| `POST /v1/water/grey-valve/schedule/cancel` | none | `runtime.App.CancelGreyWaterOpening` | Clears any pending or open scheduled grey-water event from persisted water runtime state. |
| `PUT /v1/tracking/settings` | `{"when_engine_on":bool,"sample_interval_seconds":number}` | `config.Config.Validate` | `sample_interval_seconds` must be between `1` and `3600`; a `tracking` section requires `location.enabled`. Saved to config and applied live. `directory` is ignored if supplied. |
| `PUT /v1/sensors/settings` | `sensors.Settings` | `sensors.Settings.Validate`, `config.Config.Validate` | Names must be unique, MACs unique and colon-separated, at most one `primary`. Saved to the config file and applied live; toggling `enabled` starts/stops the BLE scan. |

## Event Types

| Event type | Publisher | Payload | Notes |
|---|---|---|---|
| `heating.state_changed` | `runtime.App.publishStateLoop` | `heating.State` | Published when current heating state changes. |
| `service.connection_changed` | `runtime.App.publishStateLoop` | `AdapterHealth` | Published when Garmin connected flag changes. |
| `automation.schedule_updated` | `runtime.App.UpdateHeatingSchedule` | `HeatingScheduleDocument` | After config save/reload succeeds. |
| `location.state_changed` | `runtime.App.publishStateLoop` | `location.State` | Published when location state changes. |
| `water.state_changed` | `runtime.App.publishStateLoop`, water schedule updates | `water.State` | Published when water valve state, command status, or persisted grey-water schedule state changes. |
| `tracking.state_changed` | `runtime.App` tracking manager change callback | `tracking.State` | Published after every sample and lifecycle transition (engine on/off, settings changes, poll errors). |
| `overview.state_changed` | `runtime.App.publishStateLoop`, sensor settings update | `overview.Document` | Published when the overview document (including the temperature panel) changes. |
| `pi.state_changed` | `runtime.App` host manager change callback | `host.Metrics` | Published whenever the sampled host metrics change (including the first sample). |
| `automation.run_started` | `runtime.App.executeTransition` | map with `program_id`, `next_transition_at`, `action` | Has `correlation_id`. |
| `automation.run_failed` | `runtime.App.executeTransition` | map with `program_id`, `error` | Has same run correlation id. |
| `automation.run_succeeded` | `runtime.App.executeTransition` | map with `program_id`, `action` | Has same run correlation id. |
| `heating.mode_changed` | `runtime.App.setHeatingMode`, `reconcileExpiredBoost` | `HeatingRuntimeState` | Published after mode changes or expired boost collapse. |

## Implementation Map

| Layer | Files | Responsibility |
|---|---|---|
| HTTP | `service/api/httpapi/server.go` | Route registration, JSON decode/encode, HTTP status mapping, SSE stream. |
| Runtime app | `service/runtime/app.go`, `service/runtime/mode.go`, `service/runtime/lights.go`, `service/runtime/water.go` | Service state, scheduler loop, runtime modes, schedule persistence, light flash orchestration, grey-water valve commands, location polling. |
| Config | `service/config/config.go`, `service/config/runtime_state.go` | YAML load/save, schedule document conversion, validation, runtime mode state persistence. |
| Domain types | `service/domains/heating/types.go`, `service/domains/lights/types.go`, `service/domains/location/types.go`, `service/domains/water/types.go` | JSON structs and validation helpers. |
| Scheduler | `service/automation/scheduler/scheduler.go` | Active/next period calculation and action derivation. |
| Garmin adapter | `service/adapters/garmin/adapter.go` | Bridges runtime controllers to the lower-level websocket client; supplies Alde overview telemetry. |
| SwitchBot adapter | `service/adapters/switchbot/*.go` | Raw HCI socket passive scan (linux), SwitchBot thermometer decode, rolling seen table, reading callback. |
| Sensor runtime | `service/runtime/sensors.go` | Sensor state, Alde history observation, temperature panel assembly and primary promotion, live settings apply. |
| Sensor history | `service/history/store.go` | Per-sensor NDJSON persistence, 2h ring buffer, tail-on-start, 7-day retention compaction. |
| switchbotctl | `cmd/switchbotctl/main.go` | Local HTTP API CLI for `discover`, `status`, `settings`. |
| Teltonika adapter | `service/adapters/teltonika/rutx50.go` | Polls the RUTX50 JSON GPS status endpoint for coordinates. |
| Host metrics | `service/host/*.go` | Samples Pi host metrics (load, memory, disk, temperature, uptime, power) on a ticker and publishes changes. |
| GeoTimeZone adapter | `service/adapters/geotimezone/resolver.go` | Resolves coordinates to an IANA timezone using a configurable HTTP endpoint. |
| Websocket client | `heating/*.go` | Garmin websocket session, wire frame parsing, command frames, signal-derived heater state. |
| Daemon | `cmd/empirebusd/main.go` | Config load, app startup, HTTP server lifecycle. |

## Known Omissions / Design Choices

| Area | Current choice | Rationale / mitigation |
| BLE scanning | Raw HCI user-channel socket (`CAP_NET_ADMIN`, `CAP_NET_RAW`), passive scan. | No extra BLE daemon dependency; the systemd unit grants the two capabilities. Not available off-Linux (`hci_other.go` returns `ErrUnsupported`). |
| WoSensorTHO battery byte | Decoder reads the last byte of the `0xFD3D` service data payload (`& 0x7F`). | Confirmed by real captures (`3dfd770041`→65%, `3dfd7700e4`→100%) and HA Community `ble_monitor` issue #1204; final offset still flagged for live-capture confirmation once hardware is available. |
|---|---|---|
| API auth and TLS | No in-process authentication, authorization, or TLS middleware. | Deployment assumes the HTTP API is reachable only over the operator's Tailscale network. If the Pi is also reachable on Wi-Fi/Ethernet, bind `api.listen` to a Tailscale-only address or put the service behind a Tailscale-facing local proxy. |
| Deploy target validation | `scripts/deploy/deploy-on-pi.sh` is an operator-only helper and does not currently harden arbitrary `TARGET_SHA` input before using it as the release directory name. | Treat the script as trusted local operational tooling. For untrusted callers or automation, resolve the argument with `git rev-parse --verify` and use the resulting full commit hash for filesystem paths. |
