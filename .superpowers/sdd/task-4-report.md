# Task 4 Report: runtime grey discharge wiring

## Status

Complete.

Task 4 wires Task 2's Garmin discharge-event queue and Task 3's deterministic
water-history store into the runtime polling path:

- runtime now captures an optional `GreyWaterDischargeProvider` from the Garmin
  adapter during `New`.
- runtime passes `logger.Printf` into `waterhistory.New(...)` so unexpected
  grey-level drops surface through the normal runtime logger.
- each water-history tick now drains Garmin open/close events before sampling
  overview telemetry, so a discharge open suppresses false anomaly logs on the
  following grey drop.
- the tick publishes exactly one `water.history_changed` event after either a
  discharge event or a telemetry sample changes history.
- `observeWaterTelemetry` remains responsible for sample ingestion and fresh
  fill detection only.

## Files changed

- `service/runtime/app.go`
- `service/runtime/sensors.go`
- `service/runtime/sensors_test.go`

## Verification

### RED

Command:

```bash
go test ./service/runtime -run TestGreyWaterHistoryDrainsProviderEventsBeforeSamplesAndPublishesClose
```

Result:

- `FAIL    empirebus-tests/service/runtime [build failed]`
- compiler failures matched the missing Task 4 surface:
  - `unknown field greyWaterDischarge in struct literal of type App`
  - `app.observeWaterHistory undefined`

### GREEN

Command:

```bash
go test ./service/runtime -run 'TestGreyWaterHistoryDrainsProviderEventsBeforeSamplesAndPublishesClose|TestObserveWaterTelemetryLogsGreyDropWithoutDischargeOpen|TestObserveWaterTelemetryKeepsFreshFillDetection'
```

Result:

- `ok      empirebus-tests/service/runtime  0.697s`

### Required package verification

Command:

```bash
rtk test go test ./heating ./service/adapters/garmin ./service/waterhistory ./service/runtime
```

Result:

- `ok   empirebus-tests/heating (cached)`
- `ok   empirebus-tests/service/adapters/garmin (cached)`
- `ok   empirebus-tests/service/waterhistory (cached)`
- `ok   empirebus-tests/service/runtime 2.503s`

### Full repository verification

Command:

```bash
rtk test go test ./...
```

Result:

- repo-wide test run passed
- included:
  - `ok   empirebus-tests/cmd/servsim 2.827s`
  - `ok   empirebus-tests/heating (cached)`
  - `ok   empirebus-tests/service/runtime (cached)`
  - `ok   empirebus-tests/service/waterhistory (cached)`

### Patch hygiene

Commands:

```bash
rtk proxy gofmt -w service/runtime/app.go service/runtime/sensors.go service/runtime/sensors_test.go
rtk git diff --check
```

Result:

- formatting applied cleanly
- diff check clean

## Notes

- The runtime helper is `observeWaterHistory()`: it drains discharge events
  first, then samples telemetry, then publishes once if either side changed the
  water-history document.
- The runtime tests cover:
  - open-before-sample ordering via the absence of a false grey-drop log
  - deterministic close-to-empty event recording
  - runtime logging when grey drops without an open event
  - preservation of fresh fill detection through `observeWaterTelemetry`
- A pre-existing modification to `.superpowers/sdd/task-2-report.md` was left
  untouched and is not part of Task 4.

## Concerns

None for this task. The requested runtime wiring is in place, focused tests are
green, package verification passed, and the full repository suite passed.
