# Task 3 Report: Extend Battery struct and wire estimator into App

**Status:** DONE

## What I implemented

### Step 1 — Battery struct (`service/domains/overview/types.go`)
Replaced the `Battery` struct per the brief: removed `ETAHours *float64` (`eta_hours`), added `PowerW`, `Mode`, `ChargeState`, `ETASeconds`, `TargetSOC` fields with the exact JSON tags from the brief.

### Step 2 — `batteryEstimate` field (`service/runtime/app.go`)
Added `batteryEstimate *BatteryEstimateSmoothing` to the `App` struct, placed between the `waterHistory` field and the `mu sync.RWMutex` block (same package as the smoother type, so no import needed).

### Step 3 — Smoother initialization (`service/runtime/app.go`)
Added `batteryEstimate: NewBatteryEstimateSmoothing(smoothingHalfLifeDuration, time.Second)` to the `app := &App{...}` literal.

### Step 4 — Estimator wiring in `overviewDocument` (`service/runtime/overview.go`)
- Removed the inline `Battery:` field from the `overview.Document{...}` literal (now set after the estimate).
- Removed the old `if telemetry.BatteryCurrentA != nil { ... }` ETA block (which set `Status` to `"charging"`/`"not_charging"` and computed `ETAHours`).
- Replaced it with `BatteryConfig` construction from the new normalized settings fields, `ComputeBatteryEstimate(...)` call passing `a.batteryEstimate`, a base `doc.Battery = overview.Battery{...}` assignment, and the `Available`/mode mapping exactly as specified:
  - unavailable → `Status = "unavailable"`
  - available → `Status`/`Mode` = `string(est.Mode)`, `PowerW` = `est.PowerW`
  - charging: `topping_off` when `est.SOC >= ReadySOC`, else `ETASeconds` = `est.EstimatedSeconds` + `TargetSOC` = `est.TargetSOC` when `EstimatedSeconds > 0`
  - discharging: `ETASeconds` + `TargetSOC` = `FloorSOC` when `EstimatedSeconds > 0`
- Removed the now-unused `math` import.

### Step 5 — Preserve battery config in `UpdateOverviewSettings`
Already implemented during Task 1's review fixes (`service/runtime/overview.go` lines 142-146): it copies `next.Overview` into `ov`, overrides only the 3 legacy fields, and reassigns `next.Overview = ov`. This matches the brief's intent (battery fields preserved on settings save). No change was made.

## Verification

- `go build ./service/runtime/...` → PASS
- `go build ./service/domains/overview/...` → PASS
- `go vet ./service/runtime/... ./service/domains/overview/...` → FAILS **only** on:
  `vet: service/runtime/overview_test.go:19:17: doc.Battery.ETAHours undefined (type overview.Battery has no field or method ETAHours)`
  This is the expected, deliberate breakage — the test package still references the removed `ETAHours` field and is fixed in Task 5. Per instructions, `go test` was not run.
- `gofmt -w` applied to `types.go`, `app.go`, `overview.go`.

## Files changed (committed)

- `service/domains/overview/types.go`
- `service/runtime/app.go`
- `service/runtime/overview.go`

Commit: `42827d9 feat: wire battery estimate into overview document`

## Self-review findings

- Battery struct matches the brief exactly, JSON tags included (verified via diff).
- `batteryEstimate` field is on `App` and initialized in the construction literal.
- `overviewDocument` uses `ComputeBatteryEstimate` with `a.batteryEstimate`; status/mode/power/eta/target_soc mapping matches the brief; the inline Document-literal `Battery:` field is removed.
- No leftover `ETAHours`/`eta_hours` references in non-test code. The only non-doc reference to `eta_hours` is `web/static/app.js:788` (frontend display), which is outside this task's scope; unchanged.
- `gofmt` also realigned the `Gas` struct field alignment in `types.go` (cosmetic, correct).
- Nothing beyond the task was changed; only the 3 task files were committed. (A pre-existing uncommitted edit to `.superpowers/sdd/task-2-report.md` and an untracked plan doc were left untouched.)

## Fix round 1

Changed charging branch `targetSOC` source from `est.TargetSOC` to `estCfg.ReadySOC` (`service/runtime/overview.go:84`), making it consistent with the discharging branch which reads `targetSOC` from config (`estCfg.FloorSOC`).

- `go build ./service/runtime/...` → PASS (no output)
- `go build ./service/domains/overview/...` → PASS (no output)

Commit: `e79f38d fix: use config ReadySOC for charging target_soc`

## Issues / concerns

1. `go vet` reports the expected `overview_test.go` compile error (Task 5 fix). Vet cannot be fully clean until then.
2. Frontend `web/static/app.js` still renders from `eta_hours`; it will need updating for the new fields (`eta_seconds`, `mode`, `power_w`, `target_soc`) — presumably covered by a later task.