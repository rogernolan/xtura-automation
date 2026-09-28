# Task 2 report: YAML configuration and validation

Status: complete.

Implemented `WaterHistoryConfig.Calibration` with optional fresh and grey point slices. `Config.Validate` passes each non-empty slice to Task 1's `watercalibration.New` and prefixes errors with the corresponding YAML field path. `normalizeWaterHistory` already copies the entire input struct (`out := in`), so it preserves both slices unchanged without additional code or defaults.

## Changed files

- `service/config/config.go`: calibration types, YAML tags, and fresh/grey validation.
- `service/config/config_test.go`: acceptance and preservation of both curves; field-specific rejection through Validate and Normalize; omitted, empty-block, and empty-slice compatibility; loading both curves through LoadFile.
- `config.example.yaml`: all 15 specified measured fresh points, in the exact order and with the specified values, ending at 138.9 litres.
- `.superpowers/sdd/task-2-report.md`: this requested handover report.

## RED evidence

Added the tests before production changes. Ran `rtk go test ./service/config -run 'TestWaterCalibration'`; exit 1, package build failed because `cfg.WaterHistory.Calibration` and `WaterCalibrationConfig` did not exist. This matches the brief's explicitly required compilation failure. Representative output:

```
cfg.WaterHistory.Calibration undefined (type WaterHistoryConfig has no field or method Calibration)
undefined: WaterCalibrationConfig
```

## GREEN evidence

- `rtk go test ./service/config -run 'TestWaterCalibration'`: exit 0, 6 passed in 1 package.
- After gofmt, `rtk go test ./service/config`: exit 0, 65 passed in 1 package.
- `rtk go test ./...`: exit 0, 465 passed in 34 packages.
- `rtk git diff --check`: exit 0.

The first sandboxed gofmt attempt could not create temporary files in the specified worktree; the authorized escalated retry succeeded. Tests completed successfully after formatting.

## Self-review

Reviewed the complete scoped diff against the brief. Both curves use the existing calibration validator, empty calibration remains optional, and normalization preserves configured point order and values. YAML loading uses the existing loader and Point tags. The sample contains every required measurement. No runtime, overview, browser, or signal-reference files were modified. The existing Task 1 report modification and untracked plan were left untouched and excluded from staging.

## Concerns

None for this task. Runtime consumption is outside Task 2. Normalization retains the existing shallow struct-copy behavior; no new defensive-copy semantics are introduced.

## Commit

Configuration support and this report are committed together with message `feat: configure measured water tank curves`. The final response identifies the resulting commit hash.
