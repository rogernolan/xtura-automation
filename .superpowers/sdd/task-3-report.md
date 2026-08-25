# Task 3 Report: deterministic grey empty recording and anomaly diagnostics

## Status

Complete.

Task 3 replaces heuristic grey-empty event creation with deterministic discharge tracking:

- `Store.RecordGreyDischargeOpen(at)` persists a pending grey discharge-open timestamp and is idempotent for the same timestamp.
- `Store.RecordGreyEmpty(at)` records exactly one deterministic `TankGrey` / `KindEmpty` event when a pending open exists, then clears the pending state.
- unmatched grey close calls return `(false, nil)` and create no event.
- grey level drops seen without a pending discharge open no longer create heuristic events; they emit one diagnostic log when the drop meets the configured threshold.
- upward grey movement is treated as normal filling and does not log.
- fresh fill detection still uses the configured threshold and settling period.

## Files changed

- `service/waterhistory/types.go`
- `service/waterhistory/store.go`
- `service/waterhistory/store_test.go`

## Verification

### RED

Command:

```bash
rtk test go test ./service/waterhistory -run 'Test.*Grey|TestFillRequires'
```

Result:

- `FAIL    empirebus-tests/service/waterhistory [build failed]`
- compiler failures matched the missing Task 3 surface:
  - `store.RecordGreyDischargeOpen undefined`
  - `store.RecordGreyEmpty undefined`
  - `unknown field Logf in struct literal of type Options`

### GREEN

Commands:

```bash
rtk test go test ./service/waterhistory -run TestGrey
rtk test go test ./service/waterhistory -run TestFillRequires
```

Result:

- `ok  	empirebus-tests/service/waterhistory	0.568s`
- `ok  	empirebus-tests/service/waterhistory	0.356s`

### Package verification

Command:

```bash
rtk test go test ./service/waterhistory
```

Result:

- `ok  	empirebus-tests/service/waterhistory	1.350s`
- rerun after `gofmt`: `ok  	empirebus-tests/service/waterhistory	(cached)`

### Patch hygiene

Commands:

```bash
rtk proxy gofmt -w service/waterhistory/types.go service/waterhistory/store.go service/waterhistory/store_test.go
rtk git diff --check
```

Result:

- formatting applied cleanly
- diff check clean

## Notes

- The store now persists pending grey discharge-open state in `state.json`, so a restart between open and close still allows one deterministic empty event to be recorded.
- `RecordGreyEmpty` sets the event `From` from the latest known grey level, writes `To=0` and `Used=From`, clears the pending open, and resets the in-memory grey state to empty so summary calculations reflect an actual empty tank.
- Existing tests that previously depended on heuristic grey empty detection were rewritten to use the deterministic open/close API or to assert that level movement alone is not an event.

## Concerns

- The focused regex from the brief reproduced the intended RED compiler failure, but on later reruns the shell wrapper parsed the `|`; GREEN verification used separate focused commands instead.
