# EmpireBus Service Design

## Summary

Build a Go daemon that hides Garmin websocket traffic behind a stable local API for heating control and future vehicle automation.
The first proof of concept should expose REST endpoints plus a simple event stream, keep one normalized in-memory state model, and execute one config-defined daily heating program:

- `00:00` to `05:30`: heating off
- `05:30` to `08:00`: heating on with target temperature `20.0 C`
- `08:00` to `24:00`: heating off

The daemon should reuse the existing Go heating client as the first hardware adapter rather than inventing a second heater control path.

## Goals

- Provide a stable HTTP API that is easier for iOS clients and tools like Node-RED to consume than the raw Garmin websocket.
- Contain Garmin websocket bootstrap, heartbeat, and frame-noise handling inside one long-running service.
- Maintain a normalized in-memory service state for heating and service health.
- Support one file-configured schedule executed by the service itself.
- Expose a simple event stream of normalized state and automation events.
- Preserve a clean extension path for future data sources such as BLE tank sensors, weather, mains power status, and vehicle motion.

## Non-Goals

- Full multi-domain rule automation in the first version.
- User-managed schedule creation or editing through the API.
- Persistent database storage in the first version.
- Authentication, multi-user access control, or internet-facing deployment in the first version.
- A complete domain model for all Garmin or EmpirBus signals.

## Context

The repo already contains a Go heating client and websocket session implementation in the `heating` package, plus a thin CLI in `cmd/heatingctl/main.go`.
That client already handles:

- Garmin websocket connection and bootstrap
- heartbeat traffic
- heater power-on behavior
- target-temperature reads and stepwise writes
- verbose tracing around heater operations

This service should build on that work by wrapping it in a long-lived daemon with a stable control plane.

## Recommended Architecture

Use a long-running Go daemon with explicit boundaries between:

- transport adapters for noisy live protocols
- domain services for normalized state and command intent
- automation for schedules and future rules
- API surfaces for synchronous commands and streaming updates

The first daemon can live at `cmd/empirebusd`.

Illustrative package layout:

- `cmd/empirebusd`: process entrypoint
- `service/config`: file loading and validation
- `service/runtime`: process wiring and lifecycle
- `service/domains/heating`: normalized heating state and command intent, currently backed by Garmin
- `service/adapters/garmin`: bridge to the existing `heating` package
- `service/automation`: schedule loading and execution
- `service/api/http`: REST handlers
- `service/api/events`: server-sent event publisher

This package split is intentionally modest. It should create clean seams without over-engineering an internal platform before the POC has proven itself.

## Why This Approach

This design was chosen over two alternatives:

1. A thin HTTP wrapper directly around the existing heater client
2. A fully event-sourced internal bus with reducers and subscribers

The thin-wrapper approach is fast but would entangle Garmin behavior, API routing, and scheduling logic almost immediately.
The fully event-sourced approach is attractive for a larger system, but it adds more abstraction than the first schedule-driven proof of concept needs.

The recommended service-with-adapters approach keeps the POC small while preserving the boundaries needed for future sensors and rules.

## Service Responsibilities

The daemon should own these concerns:

- maintaining the Garmin websocket session through the adapter
- projecting noisy live Garmin traffic into a stable current-state model
- accepting normalized commands from HTTP clients
- executing scheduled actions
- emitting normalized events for observers
- reporting service health and adapter connectivity

The daemon should not expose Garmin frame structure directly in its public API.
That detail belongs behind the adapter boundary.

## Domain Model

In the POC, heating is not an independent hardware integration.
It is a service-level model of heating intent and heating state that is currently implemented entirely through Garmin.
The separation is by responsibility, not by upstream source count.

That means:

- the Garmin adapter owns websocket behavior, Garmin signal IDs, frame decoding, and press or release command mechanics
- the heating domain owns meanings such as `power_state=on`, `target_temperature_c=20.0`, `ready=true`, and intents such as `ensure_off`

This boundary is still useful even with only one upstream because it gives clients and future rules a stable language that does not depend on Garmin protocol details.
It will become more valuable as soon as the service also incorporates other sources such as cabin temperature sensors, weather, motion, or mains status.

The initial normalized state should include two broad areas:

### Heating State

- current power state
- busy or ready status when known
- current target temperature when known
- last successful update timestamp
- last command outcome or error if relevant

### Service Health

- Garmin adapter connection state
- last successful Garmin frame time
- scheduler status
- configuration load status
- service start time

This state should be safe for clients to reason about without any knowledge of Garmin message families, bootstrapping, or press-and-release semantics.
For example, a client should ask for `set target temperature to 20.0 C`, not for repeated Garmin writes on signal `107`.

## Public API

The POC should expose HTTP JSON endpoints plus one server-sent event stream.

### REST Endpoints

- `GET /v1/health`
- `GET /v1/heating/state`
- `POST /v1/heating/power`
- `POST /v1/heating/target-temperature`
- `GET /v1/automation/heating-programs`

Suggested request and response shapes:

`GET /v1/health`

```json
{
  "status": "ok",
  "started_at": "2026-04-21T10:00:00Z",
  "garmin": {
    "connected": true,
    "last_frame_at": "2026-04-21T10:15:32Z"
  },
  "scheduler": {
    "running": true
  }
}
```

`GET /v1/heating/state`

```json
{
  "power_state": "on",
  "ready": true,
  "target_temperature_c": 20.0,
  "target_temperature_known": true,
  "last_updated_at": "2026-04-21T10:15:32Z"
}
```

`POST /v1/heating/power`

```json
{
  "state": "on"
}
```

`POST /v1/heating/target-temperature`

```json
{
  "celsius": 20.0
}
```

`GET /v1/automation/heating-programs`

Returns the heating programs loaded from config, along with the currently active period and next transition metadata derived at runtime.

### Event Stream

Expose `GET /v1/events` as Server-Sent Events.

The initial event types should include:

- `heating.state_changed`
- `automation.run_started`
- `automation.run_succeeded`
- `automation.run_failed`
- `service.connection_changed`

Each event should include:

- event type
- timestamp
- a small payload of normalized state or execution metadata
- a correlation identifier for scheduled actions or API-triggered commands

SSE is preferred over WebSockets for the public API because it is simpler for the POC, works well for one-way updates, and is easier to consume from iOS and Node-RED.

## Adapter Design

The Garmin adapter should encapsulate the existing `heating.Session` and `heating.Client`.

Its responsibilities:

- establish and maintain the Garmin websocket session
- replay bootstrap and heartbeat behavior through the existing code
- observe Garmin-backed heating state from the session
- expose Garmin-backed implementations of normalized heating commands to the domain layer
- translate Garmin-specific errors into service-level errors

The heating domain should call an interface owned by the service, not the raw `heating.Client` directly.
In the first version, that interface will have only one implementation: the Garmin-backed adapter.
The point is to hide Garmin protocol details from the rest of the service, not to pretend the heating domain is already source-independent.

Illustrative interface:

```go
type HeatingAdapter interface {
    CurrentState() HeatingSnapshot
    EnsureOn(ctx context.Context) error
    EnsureOff(ctx context.Context) error
    SetTargetTemperature(ctx context.Context, celsius float64) error
    Events() <-chan HeatingSnapshot
    Health() AdapterHealth
}
```

The missing piece relative to the current repo is `EnsureOff`.
That should be added to the existing heating client because the first schedule needs a clean heater-off operation.

## Automation Model

The POC automation system should support file-configured daily heating programs rather than isolated on and off events.

Each heating program should contain:

- a stable identifier
- applicable days
- an ordered list of periods covering the full 24-hour day
- optional enabled or disabled status

Each period should contain:

- a `start` time in local wall-clock time
- a `mode` of `off` or `heat`
- a required `target_celsius` when `mode` is `heat`

The model is intentionally total.
Each day must be fully defined rather than leaving gaps or implied behavior between schedule entries.

Validation rules:

- every heating program must start with a period at `00:00`
- period start times must be strictly increasing within the day
- the final period implicitly runs until `24:00`
- adjacent periods with the same effective state should be rejected as redundant
- `heat` periods must specify `target_celsius`
- a program day with no explicit heating should still be represented as a single `00:00` `off` period

This model allows adjacent `heat` periods with different temperatures.
Those transitions should change setpoint without cycling heater power off and on.

### Daylight Saving Time

Heating programs are defined in local wall-clock time in the configured timezone, for example `Europe/London`.
The scheduler must handle daylight saving transitions explicitly rather than relying on accidental runtime behavior.

Policy for the POC:

- on spring-forward days, if a configured transition falls in a nonexistent local time, run it at the first valid local time after the gap
- on fall-back days, if a configured transition falls in the repeated hour, run it only once at the first occurrence of that local time
- a program day still conceptually begins at local `00:00`, even though the elapsed duration of that day may be 23 or 25 hours

Examples in `Europe/London`:

- if a transition is configured for `01:30` on the spring DST change day, it should execute when the clock first reaches a valid time after the skipped interval
- if a transition is configured for `01:30` on the autumn DST change day, it should execute once at the first `01:30`, not twice

This keeps the schedule aligned to human local time rather than elapsed UTC intervals, which is the correct mental model for heating programs.

At runtime the scheduler should:

1. calculate the active period for the current local time and day
2. calculate the next transition time in the configured timezone
3. wake at each transition boundary
4. derive the required domain action from the previous and next periods
5. emit execution events and logs
6. continue scheduling after success or failure

Transition rules for the first POC:

- `off -> heat(target)` means `EnsureOn` and then `SetTargetTemperature(target)`
- `heat(a) -> heat(b)` means keep heater power on and call only `SetTargetTemperature(b)`
- `heat(*) -> off` means `EnsureOff`
- `off -> off` should never appear in a valid program because it is redundant

For the first POC, a simple in-process scheduler is preferred over shelling out to system cron.

### Why Not External Cron

System cron is a valid future operational detail, but it should not be the primary automation boundary.
If cron owns the schedule, the service loses a coherent model of planned automation, next runs, and execution outcomes.

The service should instead own heating programs as first-class domain objects.
If needed later, the scheduler implementation can be backed by cron-like expressions or even delegated externally without changing the public API.

## Configuration

Use a file-based configuration format such as YAML.
The config should be explicit, human-editable, and suitable for a van-local deployment.

Illustrative POC config:

```yaml
garmin:
  ws_url: ws://192.168.1.1:8888/ws
  heartbeat_interval: 4s

automation:
  timezone: Europe/London
  heating_programs:
    - id: daily-default
      days: ["mon", "tue", "wed", "thu", "fri", "sat", "sun"]
      periods:
        - start: "00:00"
          mode: "off"
        - start: "05:30"
          mode: "heat"
          target_celsius: 20.0
        - start: "08:00"
          mode: "off"

api:
  listen: 0.0.0.0:80
```

The config schema should be generic enough to absorb future domains without needing a redesign.
The UI can hide some of this explicitness later, but the stored configuration should remain total and unambiguous.

## Command Semantics

Public commands should be idempotent where practical:

- asking for heating on when it is already on should succeed
- asking for heating off when it is already off should succeed
- setting the target temperature to the already-current setpoint should succeed

Commands should fail clearly when:

- the Garmin adapter is disconnected
- heater readiness cannot be established in time
- the current target temperature is unknown when stepwise control requires it
- the command times out or diverges from the expected result

Responses should be human-readable and machine-usable.
Errors should include enough context to help debug live behavior without exposing raw protocol details by default.

## State Flow

The runtime flow should look like this:

1. Garmin adapter connects and begins receiving frames
2. adapter projects those frames into a normalized heating snapshot
3. heating domain stores the latest snapshot in memory
4. REST handlers read from the domain snapshot and issue domain commands
5. scheduler also issues domain commands
6. state changes and automation outcomes are published to the SSE stream

This keeps both API-triggered actions and scheduled actions flowing through the same service boundary, which is important for observability and future rule evaluation.

## Observability

The daemon should produce structured logs for:

- service startup and shutdown
- configuration load success or failure
- Garmin connection changes
- incoming scheduled runs
- outgoing domain commands
- command success and failure
- scheduler calculation issues

The event stream should not replace logs.
Logs are for operators.
The event stream is for clients that want current behavior updates.

## Failure Handling

The POC should handle these failure classes gracefully:

- Garmin websocket unavailable at startup
- Garmin websocket disconnect during normal operation
- heater command timeout
- schedule execution failure
- malformed configuration file

Recommended behavior:

- the service should still start if configuration is valid but Garmin is temporarily unavailable
- health should report degraded status until Garmin reconnects
- scheduled actions should fail and emit events if the adapter is unavailable
- the scheduler should continue to run future actions after a failed run

## Security Assumption

For the first proof of concept, assume the service is reachable only on a trusted local network inside or near the van.
Do not add authentication in the first implementation.
However, keep routing and middleware structured so local auth can be added later without rewriting handlers.

## Future Expansion

This design intentionally prepares for additional signal and data domains:

- vehicle motion for safety interlocks
- mains hookup state
- hot water source selection
- BLE tank or gas sensors
- forecast-driven heating

Those should arrive as new adapters and new domain services, not by pushing more Garmin-specific logic into HTTP handlers.

The eventual rule examples discussed so far fit this direction:

- if the van is moving, turn the water pump off
- if forecast temperature is below `1 C` overnight, get the van to `21 C` by `06:00`
- if mains is connected, use mains to heat water

The first POC should not solve those rules yet, but it should avoid closing off the path to them.
One likely early example is combining Garmin-backed heater control with a non-Garmin temperature or weather input while keeping the public heating API unchanged.

## Implementation Notes

The first implementation pass should likely include these concrete steps:

1. add `EnsureOff` support to the existing Go heating client
2. create a Garmin heating adapter around the existing client
3. define normalized heating and health state models
4. add the daemon entrypoint and lifecycle wiring
5. add REST handlers
6. add the SSE publisher
7. add config loading and validation
8. add the in-process scheduler with one config-defined schedule
9. add tests for config loading, schedule execution, and API contract

## Testing Strategy

Testing should focus on service boundaries, not only the Garmin protocol details already covered by the heater package.

Recommended coverage:

- unit tests for config parsing and validation
- unit tests for next-run calculation in the scheduler
- unit tests for ordered action execution
- HTTP handler tests for request and response contracts
- SSE publisher tests for event formatting
- adapter tests using mocked heating client behavior

Where useful, reuse the existing heating fixtures and session behavior rather than duplicating Garmin frame knowledge in the service layer.

## Open Questions Deferred

These are intentionally deferred beyond the POC:

- whether schedules should later become API-managed resources
- whether future rules should use a small DSL, config-only predicates, or compiled Go rules
- whether long-term storage should be SQLite or another embedded store
- whether the public API should eventually grow WebSocket push in addition to SSE

Deferring these choices now keeps the POC focused while preserving room to grow.

## Recommendation

Proceed with a Go daemon that:

- wraps the existing heating client as an adapter
- exposes REST plus SSE
- owns a file-configured in-process scheduler
- supports the two morning heating actions as the first automation proof of concept

This is the smallest design that proves the desired system boundary:
Garmin websocket complexity stays inside the daemon, while clients interact with a stable and understandable service API.
