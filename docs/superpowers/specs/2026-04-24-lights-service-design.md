# Lights Service Design

## Goal

Add a new lights service parallel to heating, with an initial externally useful capability:

- flash the exterior work lights a requested number of times

This first version should be deliberately narrow, but it should establish the service boundary and in-memory state model needed for future grouped and individual light control.

## Scope

Included in this slice:

- a new lights service/runtime path parallel to heating
- in-memory lights state for the exterior group
- Garmin command support for exterior lights on/off
- an API endpoint to flash exterior lights `1..5` times
- restore the prior exterior state after flashing
- a busy/in-progress error if a second flash request arrives during an active flash
- API documentation updates

Explicitly out of scope for this slice:

- individual light control
- dimmer control
- internal light groups
- persisted lighting mode/state
- scheduling or automation for lights

## Evidence And Assumptions

From the current Garmin signal reference:

- exterior lights ON command is browser-confirmed as `{"messagetype":17,"messagecmd":0,"size":3,"data":[47,0,3]}`
- exterior lights OFF command is browser-confirmed as `{"messagetype":17,"messagecmd":0,"size":3,"data":[48,0,3]}`
- receive signal `47` indicates exterior on
- receive signal `48` indicates exterior off

This design assumes Garmin state frames are seen on connect and during later state changes, so the in-memory lights state can converge from live traffic. If state is temporarily unknown, restore behavior remains best-effort based on the last known exterior state.

## Approaches Considered

### 1. Recommended: small lights service with narrow first capability

Add a proper lights domain, runtime state, API surface, and Garmin adapter methods, but keep the first feature limited to exterior flash.

Pros:

- clean boundary for future light controls
- explicit in-memory state model
- easy to add future endpoints without reshaping the service

Cons:

- slightly more upfront code than a one-off handler

### 2. One-off flash endpoint

Add a single HTTP handler that sends Garmin on/off commands directly and manages its own mutex.

Pros:

- fastest initial implementation

Cons:

- poor reuse
- no good home for future light state
- pushes protocol details into API code

## Recommended Design

### Service Structure

Add a lights service parallel to heating with:

- a domain state type for lights
- a Garmin-backed control interface for light commands and current light state
- runtime logic for flash sequencing and busy protection
- HTTP handlers under `/v1/lights/...`

The heating service remains unchanged except for shared application wiring.

### State Model

The first lights state should only track the exterior group:

- `external_on`
- `flash_in_progress`
- `last_command_error`
- `last_updated_at`

This is intentionally narrow. Future grouped and individual controls can expand the state shape without replacing the service boundary.

### Garmin Integration

The Garmin adapter should gain:

- exterior lights on command
- exterior lights off command
- current lights state snapshot

It should interpret receive signals:

- `47` as exterior on
- `48` as exterior off

If the adapter is disconnected, the flash endpoint should fail the request rather than pretending the sequence completed.

### Flash Behavior

API contract:

- `flash <n>` where `n` is `1..5`

HTTP shape:

- `POST /v1/lights/external/flash`
- request body: `{ "count": 3 }`

Behavior:

1. Validate `count` is in `1..5`.
2. Reject with a busy-style error if a flash is already active.
3. Snapshot the current `external_on` state.
4. For each flash cycle:
   - turn exterior lights on
   - wait `500ms`
   - turn exterior lights off
   - wait `500ms` between flashes, except after the final off before restore
5. Restore the snapped exterior state after the flash sequence completes.
6. Clear `flash_in_progress` even if the sequence fails partway through.

Restore semantics:

- if the prior state was on, restore to on
- if the prior state was off, restore to off
- if state was unknown because no receive state has been observed yet, default restore to off and report/log that restore was best-effort

### Concurrency

Only one flash request may run at a time.

If another flash request arrives while a flash is active, the service should return:

- HTTP `409 Conflict`
- structured error such as `{ "error": "flash_in_progress" }`

This avoids overlapping on/off sequences and keeps behavior predictable.

### Error Handling

- invalid `count` returns `400 Bad Request`
- in-progress flash returns `409 Conflict`
- Garmin command or connectivity failures return `502 Bad Gateway`

Logs should include:

- flash requested with count
- flash started with snapped prior state
- each command failure with enough context to see whether it happened during flash or restore
- flash completed and restored state

### Events

The first version should publish SSE events for lights state changes, mirroring the heating style:

- `lights.state_changed`

An explicit `lights.flash_started` or `lights.flash_completed` event is optional for this slice. It is useful but not required if state changes and logs already provide enough observability.

### Testing

Tests should cover:

- request validation for count range
- busy rejection when a flash is already running
- successful flash sequence order
- restore-to-on behavior
- restore-to-off behavior
- cleanup of `flash_in_progress` after failures
- API handler status codes and response shapes

Mocked adapter tests are enough for the new flash runtime behavior. Existing Garmin integration tests do not need to become end-to-end for this slice.

## API Documentation Changes

Update the API docs to include:

- `GET /v1/lights/state`
- `POST /v1/lights/external/flash`

Document:

- request body with `count`
- accepted range `1..5`
- `409 flash_in_progress`
- best-effort restore behavior when live prior state is unknown

## Success Criteria

This slice is complete when:

- the service exposes a lights state endpoint
- the service can flash exterior lights `1..5` times
- concurrent flash requests are rejected cleanly
- the exterior state is restored after flashing
- logs make flash requests and failures understandable
- API docs describe the new endpoint and its error behavior
