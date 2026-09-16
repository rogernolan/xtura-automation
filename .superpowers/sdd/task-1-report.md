# Task 1 Report: Add battery config fields to OverviewConfig

## What I implemented

Applied all four edits from the brief verbatim:

1. **Struct fields** — `OverviewConfig` (`service/config/config.go`) gained six fields: `BatteryCapacityAh`, `BatteryNominalVoltage`, `BatteryFloorSOC`, `BatteryReadySOC`, `MultiplusMaxChargeCurrentA`, `ChargeEfficiency`, each with the exact yaml tags from the brief.
2. **Defaults** — `normalizeOverview` sets defaults after the existing `Comfort` block: capacity 660, voltage 12.8, floor SOC 20, ready SOC 95, max charge current 120 A, efficiency 0.99.
3. **Validation** — `Config.Validate` gained five checks: battery_capacity_ah and battery_nominal_voltage not negative; floor/ready SOC in [0,100]; charge_efficiency in [0,1].
4. **Example yaml** — `config.example.yaml` overview section extended with the Victron battery bank block.

## What I tested and results

```
$ go test ./service/config/...
ok  	empirebus-tests/service/config	0.235s

$ go test ./service/runtime/... ./service/api/httpapi/...
ok  	empirebus-tests/service/runtime	2.747s
ok  	empirebus-tests/service/api/httpapi	0.276s
```

`gofmt -l service/config/config.go` returns nothing after the fix commit.

## Files changed

- `service/config/config.go`
- `config.example.yaml`

## Commits

- `132b6f7` feat: add battery estimate config fields to OverviewConfig
- `16ebe27` style: gofmt align OverviewConfig battery fields

## Self-review findings

- **Completeness:** all four edits applied; checked against the brief line-by-line. Field names, yaml tags, default values, and validation messages match the brief exactly.
- **gofmt:** the brief's struct literal was not gofmt-canonical (struct field alignment). `gofmt -l` flagged the file, so I ran `gofmt -w` in a follow-up commit. Whitespace-only change; no semantic difference. File is now gofmt-clean.
- **Discipline:** nothing beyond the brief added. No reordering of unrelated code, no extra fields, no changes to other structs.
- **Regression:** all three affected packages pass.

## Issues / concerns

- The brief's struct block needed gofmt realignment; the diff from `gofmt` is purely whitespace. Since the self-review checklist requires gofmt-clean output, this was a necessary deviation from the verbatim text (only in alignment columns).

## Fix round 1

### What I changed

- **`service/config/config.go`** — tightened the charge-efficiency validation from `ChargeEfficiency < 0 || > 1` to `ChargeEfficiency <= 0 || > 1`. An explicit `charge_efficiency: 0` is now rejected with `overview.charge_efficiency must be greater than zero and at most one` instead of being silently normalized to 0.99.
- **`service/config/config_test.go`** — added `TestOverviewBatteryConfigDefaults` (asserts all six new fields normalize from zero: capacity 660, voltage 12.8, floor SOC 20, ready SOC 95, max charge current 120, efficiency 0.99, plus the pre-existing usable_battery_capacity_ah default of 100) and `TestValidateRejectsInvalidBatteryConfig` (table-driven; each bad value rejects with the expected `overview.*` fragment, including `ChargeEfficiency: 0` and `ChargeEfficiency: 1.5`). Conformed base test configs (`validTestConfig`, `trackingBaseConfig`, and the YAML/inline fixtures) to carry `charge_efficiency: 0.99` since the tightened check rejects the zero value that most fixtures previously left implicit.
- **`service/runtime/overview.go`** — `UpdateOverviewSettings` rebuilt `next.Overview` from scratch, dropping `ChargeEfficiency` (and the other battery fields) and producing a config that the new validator rejects on `SaveFile`. It now copies the existing `Overview` and only replaces `Comfort`/`UsableBatteryCapacityAh`/`GasTankCapacityLitres`, preserving the battery fields.
- **`service/runtime/overview_test.go`, `recording_test.go`, `tracking_test.go`** — conformed the `App`/`New` fixtures to include `charge_efficiency: 0.99`.
- **`config.sim.yaml`, `config.staging.example.yaml`** — added the Victron battery bank block (matching `config.example.yaml`) so local sim and staging example configs still validate.

### Tests run

```
$ go test -count=1 ./service/config/... ./service/runtime/... ./service/api/httpapi/...
ok  	empirebus-tests/service/config	0.140s
ok  	empirebus-tests/service/runtime	2.605s
ok  	empirebus-tests/service/api/httpapi	0.144s

$ go test -count=1 ./...
ok  	empirebus-tests/service/config	0.083s
ok  	empirebus-tests/service/runtime	3.184s
(more packages ok)
```

`gofmt -l` on the changed Go files reports nothing.

### Notes

- The `== 0` default pattern for `BatteryFloorSOC`/`BatteryReadySOC` was left untouched as instructed.
- Charge efficiency differs from capacity/voltage: because it now rejects zero (the Go zero value), every config that flows through `Validate` must carry an explicit positive value. The example/sim/staging configs were updated accordingly. All config fixtures were conformed; no deployment-only config was changed beyond the two in this repo.

## Fix round 2

### What I changed

- **`service/config/config.go`** — reverted the charge-efficiency validation back to rejecting only values outside the meaningful range:
  `if c.Overview.ChargeEfficiency < 0 || c.Overview.ChargeEfficiency > 1`, message `overview.charge_efficiency must be between 0 and 1`. `0` is the Go zero value / project-wide "unset" sentinel and is now again a valid input that `normalizeOverview` defaults to 0.99. This restores backward compatibility for existing deployments that never configured a battery section.
- **`service/config/config_test.go`** — updated the battery validation tests to the corrected semantics:
  - `ChargeEfficiency: -0.5` → error containing `overview.charge_efficiency`.
  - `ChargeEfficiency: 1.5` → error containing `overview.charge_efficiency`.
  - Removed the previous "charge efficiency zero" rejection case.
  - Added `TestValidateAllowsUnsetChargeEfficiency` asserting that `ChargeEfficiency: 0` (the unset sentinel) produces no charge_efficiency validation error.
  - `TestOverviewBatteryConfigDefaults` kept: capacity 660, voltage 12.8, floor 20, ready 95, max charge current 120, efficiency 0.99 when zeroed.
- **Reverted test fixtures** that had only been changed to satisfy the now-reverted `<= 0` check: the `charge_efficiency: 0.99` field added to `validTestConfig`, `trackingBaseConfig`, the inline struct fixtures in `config_test.go`, the YAML `overview:` blocks in `TestLoadFileAndNormalize` / `TestTrackingSectionRoundTrip`, and the `Overview` structs added to `service/runtime/overview_test.go`, `recording_test.go`, `tracking_test.go`.

### Kept (per follow-up brief)

- **`service/runtime/overview.go`** — `UpdateOverviewSettings` still copies `next.Overview` and replaces only `Comfort`/`UsableBatteryCapacityAh`/`GasTankCapacityLitres` (preserves battery fields; required by Task 3). Not reverted.
- **`config.sim.yaml`, `config.staging.example.yaml`** — Victron battery bank blocks left in place as optional documentation. Not reverted.
- `config.example.yaml` is untouched (already carried the battery block).

### Tests run

```
$ go test -count=1 ./service/config/... ./service/runtime/... ./service/api/httpapi/...
ok  	empirebus-tests/service/config	0.268s
ok  	empirebus-tests/service/runtime	2.857s
ok  	empirebus-tests/service/api/httpapi	0.253s

$ go vet ./service/config/...
vet-ok

$ go test -count=1 ./...
ok  	empirebus-tests/service/config	0.093s
ok  	empirebus-tests/service/runtime	3.241s
ok  	empirebus-tests/service/api/httpapi	0.143s
(all other packages ok / no test files)
```

`gofmt -l` on the changed Go files reports nothing. `service/config/config.go` is byte-identical to the pre-fix commit `16ebe27`.

### Notes

- The full-repo `go test -count=1 ./...` passes, confirming backward compatibility: configs that previously lacked any battery/charge-efficiency config now validate again.