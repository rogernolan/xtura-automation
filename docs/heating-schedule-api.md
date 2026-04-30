# Heating Schedule API

This note is the current handoff contract for clients that edit the heating schedule and control heating runtime modes, including the iOS schedule editor.

## Summary

The service exposes the heating schedule as one editable document:

- `GET /v1/automation/heating-schedule`
- `PUT /v1/automation/heating-schedule`

The client should:

1. fetch the full document
2. edit it locally
3. send the full replacement document back with the last seen `revision`

The server persists the updated schedule back to the YAML config file and reloads the in-memory scheduler state without a process restart.

The service also exposes runtime heating modes:

- `schedule`: follow the persisted schedule
- `off`: force heating off and ignore the schedule
- `manual`: force a target temperature indefinitely and ignore the schedule
- `boost`: temporarily force a target temperature, then return to the previous mode

Runtime mode state is persisted separately in a YAML state file, so `off`, `manual`, and active `boost` survive restarts. If a boost has already expired by the time the service starts again, it is immediately collapsed back to its saved prior mode.

## Schedule Document

```json
{
  "timezone": "Europe/London",
  "programs": [
    {
      "id": "weekday-morning",
      "enabled": true,
      "days": ["mon", "tue", "wed", "thu", "fri"],
      "periods": [
        { "start": "00:00", "mode": "off" },
        { "start": "05:30", "mode": "heat", "target_celsius": 20.0 },
        { "start": "08:00", "mode": "off" }
      ]
    }
  ],
  "revision": "2026-04-22T09:31:45.123456Z"
}
```

## Field Rules

- `timezone`: IANA timezone name, for example `Europe/London`
- `programs`: full list of heating programs
- `revision`: optimistic concurrency token returned by the server
- `id`: unique program identifier
- `enabled`: whether the program participates in scheduling
- `days`: weekday tokens accepted by the backend, typically short lowercase forms like `mon`, `tue`, `wed`, `thu`, `fri`, `sat`, `sun`
- `periods`: full ordered list of daily periods for that program
- `start`: local wall-clock time in `HH:MM`
- `mode`: currently `off` or `heat`
- `target_celsius`: required for `heat`, omitted for `off`

## Integration Wiring

Assume the service is running on a host reachable over Tailscale.

Recommended client configuration:

- base URL: `http://<tailscale-hostname-or-ip>:8080`
- example: `http://vanpi.tail1234.ts.net:8080`
- authentication: none currently, assuming access is restricted by the Tailscale network
- transport: plain HTTP over the private Tailscale network

Recommended client wrapper surface:

- `fetchHeatingSchedule() async throws -> HeatingScheduleDocument`
- `saveHeatingSchedule(_ document: HeatingScheduleDocument) async throws -> HeatingScheduleDocument`
- `fetchHeatingMode() async throws -> HeatingRuntimeMode`
- `setHeatingModeSchedule() async throws -> HeatingRuntimeMode`
- `setHeatingModeOff() async throws -> HeatingRuntimeMode`
- `setHeatingModeManual(targetCelsius: Double) async throws -> HeatingRuntimeMode`
- `setHeatingModeBoost(targetCelsius: Double, durationMinutes: Int) async throws -> HeatingRuntimeMode`
- `fetchLightsState() async throws -> LightsState`
- `flashExteriorLights(count: Int) async throws -> LightsState`

Recommended editor flow:

1. load the editor by calling `GET /v1/automation/heating-schedule`
2. keep the full returned document as the local editable draft
3. preserve the returned `revision`
4. on save, send the full edited document to `PUT /v1/automation/heating-schedule`
5. on success, replace local state with the full response body
6. on `409`, refetch and ask the user to retry
7. on `400 validation_failed`, surface the returned messages inline

The editor does not need to subscribe to `GET /v1/events` to work correctly. A simple fetch-on-open plus save-on-submit flow is enough for the first version.

Recommended mode-control flow:

1. load the current mode by calling `GET /v1/heating/mode`
2. render the effective mode in the UI as one of `schedule`, `off`, `manual`, or `boost`
3. for schedule resume, call `POST /v1/heating/mode/schedule`
4. for off mode, call `POST /v1/heating/mode/off`
5. for manual mode, call `POST /v1/heating/mode/manual`
6. for boost, call `POST /v1/heating/mode/boost`
7. after any successful mode change, replace local mode state with the full response body

Recommended exterior-light flash flow:

1. load current light state with `GET /v1/lights/state`
2. call `POST /v1/lights/external/flash` with a `count` between `1` and `5`
3. while a flash is running, treat `409 flash_in_progress` as a temporary busy state
4. after success, replace local light state with the response body
5. if the previous exterior state was unknown, the service restores to off after flashing

## iOS Networking Notes

- if the iOS app talks directly to `http://...:8080` over Tailscale, App Transport Security settings may need an exception for this private HTTP endpoint
- if you later put the service behind HTTPS or a local proxy, the API contract can stay the same and only the base URL changes
- the client should treat network failures as transport errors, separate from `400` validation and `409` revision conflicts

## GET Example

Request:

```http
GET /v1/automation/heating-schedule
```

Response:

```json
{
  "timezone": "Europe/London",
  "programs": [
    {
      "id": "everyday-default",
      "enabled": true,
      "days": ["mon", "tue", "wed", "thu", "fri", "sat", "sun"],
      "periods": [
        { "start": "00:00", "mode": "off" },
        { "start": "05:30", "mode": "heat", "target_celsius": 20.0 },
        { "start": "08:00", "mode": "off" }
      ]
    }
  ],
  "revision": "2026-04-22T09:31:45.123456Z"
}
```

## Heating Mode Document

Example `GET /v1/heating/mode` response:

```json
{
  "mode": "schedule",
  "updated_at": "2026-04-22T10:15:00Z"
}
```

Manual example:

```json
{
  "mode": "manual",
  "manual_target_celsius": 19.0,
  "updated_at": "2026-04-22T10:20:00Z"
}
```

Boost example:

```json
{
  "mode": "boost",
  "boost": {
    "target_celsius": 22.0,
    "expires_at": "2026-04-22T11:20:00Z",
    "resume_mode": "manual",
    "resume_manual_target_celsius": 19.0
  },
  "updated_at": "2026-04-22T10:20:00Z"
}
```

## Heating Mode Endpoints

- `GET /v1/heating/mode`
- `POST /v1/heating/mode/schedule`
- `POST /v1/heating/mode/off`
- `POST /v1/heating/mode/manual`
- `POST /v1/heating/mode/boost`
- `POST /v1/heating/mode/boost/cancel`

### Schedule Mode

Request:

```http
POST /v1/heating/mode/schedule
```

Response:

```json
{
  "mode": "schedule",
  "updated_at": "2026-04-22T10:30:00Z"
}
```

### Off Mode

Request:

```http
POST /v1/heating/mode/off
```

Response:

```json
{
  "mode": "off",
  "updated_at": "2026-04-22T10:31:00Z"
}
```

### Manual Mode

Request:

```http
POST /v1/heating/mode/manual
Content-Type: application/json
```

```json
{
  "target_celsius": 19.0
}
```

Response:

```json
{
  "mode": "manual",
  "manual_target_celsius": 19.0,
  "updated_at": "2026-04-22T10:32:00Z"
}
```

### Boost Mode

Request:

```http
POST /v1/heating/mode/boost
Content-Type: application/json
```

```json
{
  "target_celsius": 22.0,
  "duration_minutes": 60
}
```

Response:

```json
{
  "mode": "boost",
  "boost": {
    "target_celsius": 22.0,
    "expires_at": "2026-04-22T11:33:00Z",
    "resume_mode": "schedule"
  },
  "updated_at": "2026-04-22T10:33:00Z"
}
```

### Cancel Boost

Cancels an active boost and returns heating to the boost's saved resume mode, using the same restore behavior as boost expiry.

Request:

```http
POST /v1/heating/mode/boost/cancel
Content-Type: application/json
```

Response:

```json
{
  "mode": "schedule",
  "updated_at": "2026-04-22T10:45:00Z"
}
```

## PUT Example

Request:

```http
PUT /v1/automation/heating-schedule
Content-Type: application/json
```

```json
{
  "timezone": "Europe/London",
  "programs": [
    {
      "id": "weekday-morning",
      "enabled": true,
      "days": ["mon", "tue", "wed", "thu", "fri"],
      "periods": [
        { "start": "00:00", "mode": "off" },
        { "start": "05:30", "mode": "heat", "target_celsius": 20.0 },
        { "start": "08:00", "mode": "off" }
      ]
    },
    {
      "id": "weekend-morning",
      "enabled": true,
      "days": ["sat", "sun"],
      "periods": [
        { "start": "00:00", "mode": "off" },
        { "start": "07:00", "mode": "heat", "target_celsius": 19.0 },
        { "start": "09:30", "mode": "off" }
      ]
    }
  ],
  "revision": "2026-04-22T09:31:45.123456Z"
}
```

Success response:

```json
{
  "timezone": "Europe/London",
  "programs": [
    {
      "id": "weekday-morning",
      "enabled": true,
      "days": ["mon", "tue", "wed", "thu", "fri"],
      "periods": [
        { "start": "00:00", "mode": "off" },
        { "start": "05:30", "mode": "heat", "target_celsius": 20.0 },
        { "start": "08:00", "mode": "off" }
      ]
    },
    {
      "id": "weekend-morning",
      "enabled": true,
      "days": ["sat", "sun"],
      "periods": [
        { "start": "00:00", "mode": "off" },
        { "start": "07:00", "mode": "heat", "target_celsius": 19.0 },
        { "start": "09:30", "mode": "off" }
      ]
    }
  ],
  "revision": "2026-04-22T09:37:11.000000Z"
}
```

## Save Flow

- Always start from a fresh `GET`
- Preserve the returned `revision`
- Send the full document in `PUT`
- After a successful save, replace the client-side model with the server response, including the new `revision`

## Conflict Flow

If the client sends an outdated `revision`, the server returns `409 Conflict`.

Example:

```json
{
  "error": "schedule revision conflict"
}
```

Recommended client behavior:

1. show that the schedule changed elsewhere
2. refetch with `GET /v1/automation/heating-schedule`
3. let the user re-apply or retry their edits

## Validation Flow

Invalid schedules return `400 Bad Request` with `error=validation_failed`.

Example:

```json
{
  "error": "validation_failed",
  "details": [
    {
      "message": "automation.heating_programs[0]: heat periods must set target_celsius"
    }
  ]
}
```

The `details` array currently contains message-only objects. Clients should display the messages directly and should not assume a stable field-path schema yet.

Mode endpoints currently return ordinary error payloads in the simpler shape:

```json
{
  "error": "boost duration must be greater than zero"
}
```

## Lights API

The first lights API slice exposes exterior-light state and a flash command for the exterior work lights.

### Get Lights State

Request:

```http
GET /v1/lights/state
```

Response:

```json
{
  "external_known": true,
  "external_on": false,
  "flash_in_progress": false
}
```

Fields:

- `external_known`: whether the service has seen exterior light state from Garmin
- `external_on`: latest known exterior-light state; only meaningful when `external_known` is true
- `flash_in_progress`: whether a flash sequence is currently running
- `last_command_error`: optional text from the last failed lights command
- `last_updated_at`: optional timestamp for the latest exterior-light state update

### Flash Exterior Lights

Request:

```http
POST /v1/lights/external/flash
Content-Type: application/json
```

```json
{
  "count": 3
}
```

Rules:

- `count` must be between `1` and `5`
- each flash turns the exterior lights on for `0.5s`, then off for `0.5s`
- the service rejects overlapping flash requests with `409 Conflict`
- after flashing, the service attempts to restore the previous exterior state
- if the previous exterior state was unknown, restore defaults to off

Success response:

```json
{
  "external_known": true,
  "external_on": false,
  "flash_in_progress": false
}
```

Busy response:

```json
{
  "error": "flash_in_progress"
}
```

Invalid count response:

```json
{
  "error": "invalid flash count"
}
```

## Current Validation Rules

The backend currently enforces these schedule rules:

- timezone must be valid
- at least one program must exist
- program ids must be unique
- enabled programs must not overlap on weekday ownership
- each program must have at least one day
- each program must have at least one period
- the first period must start at `00:00`
- periods must be strictly increasing by time
- consecutive periods may have the same effective state
- `off` periods must not include `target_celsius`
- `heat` periods must include `target_celsius`

## Notes For iOS

- This is a full-document API, not patch semantics
- The safest editor model is a full in-memory draft plus explicit save
- `days` are better modeled as a set in the UI and serialized as a sorted array
- `start` should be formatted back to zero-padded `HH:MM`
- `target_celsius` should be serialized as a JSON number
- If the app needs local diffing or unsaved-change detection, compare against the last fetched document excluding `revision`
- The mode screen can be modeled independently from the schedule editor
- `boost` should be shown as a temporary mode with an expiry timestamp, not as a schedule change
- Returning to schedule mode is explicit through `POST /v1/heating/mode/schedule`
- Lights flash is runtime control only; it does not change the heating schedule or persisted heating mode

## Related Server Files

- `service/api/httpapi/server.go`
- `service/runtime/app.go`
- `service/config/config.go`
- `service/config/runtime_state.go`
- `service/runtime/mode.go`
- `service/runtime/lights.go`
- `service/domains/lights/types.go`
