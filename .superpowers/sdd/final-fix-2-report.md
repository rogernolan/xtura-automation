# Final Fix 2 Report

Status: DONE

Code commit:
- `5c1609f` - `fix: preserve deterministic grey discharge state`

Findings fixed:
- Preserve queued Garmin signal-4/signal-5 open-close edges across reconnect replacement by draining the failed session before the new session takes over, with a production-loop regression in `service/adapters/garmin/adapter_test.go`.
- Make duplicate grey-empty close replay timestamp-idempotent without wiping newer grey samples or a newer pending open, with regressions for both cases in `service/waterhistory/store_test.go`.
- Make grey-drop diagnostics cumulative so `80 -> 77 -> 74` crosses the configured threshold once, while upward grey movement remains normal and silent.

Tests and output:
- Focused RED/GREEN:
  - `rtk proxy go test ./service/adapters/garmin -run 'Test(AdapterLoopPreservesQueuedGreyWaterEdgesAcrossReconnect|DrainGreyWaterDischargeEvents.*)' -count=1`
    - RED before fix: `FAIL ... timed out waiting for queued discharge events`
    - GREEN after fix: `ok   empirebus-tests/service/adapters/garmin 1.591s`
  - `rtk proxy go test ./service/waterhistory -run 'TestGreyEmptyReplayDoesNotClearNewerGreySample|TestGreyEmptyReplayDoesNotClearNewerPendingOpen|TestGreyLevelCumulativeDropWithoutOpenLogsOnceAtThreshold' -count=1`
    - RED before fix:
      - `FAIL ... replayed close should preserve newer grey state`
      - `FAIL ... replayed close should preserve newer pending open`
      - `FAIL ... expected one cumulative anomaly log, got []`
    - GREEN after fix: `ok   empirebus-tests/service/waterhistory 0.559s`
- Covering package tests:
  - `rtk test go test ./heating ./service/adapters/garmin ./service/waterhistory ./service/runtime`
    - `ok   empirebus-tests/heating (cached)`
    - `ok   empirebus-tests/service/adapters/garmin 1.300s`
    - `ok   empirebus-tests/service/waterhistory 1.323s`
    - `ok   empirebus-tests/service/runtime 4.830s`
- Required full suite:
  - `rtk test go test -count=1 ./...`
    - passed; included `ok` for `cmd/servsim`, `heating`, `service/adapters/garmin`, `service/api/httpapi`, `service/runtime`, and `service/waterhistory`
- Required race suite:
  - `rtk test go test -race ./heating ./service/adapters/garmin ./service/waterhistory ./service/runtime`
    - `ok   empirebus-tests/heating (cached)`
    - `ok   empirebus-tests/service/adapters/garmin 3.844s`
    - `ok   empirebus-tests/service/waterhistory 4.081s`
    - `ok   empirebus-tests/service/runtime 7.090s`
- Vet and diff hygiene:
  - Requested `rtk vet go vet ./...` wrapper was unavailable in this environment (`[rtk: No such file or directory (os error 2)]`), so I ran `rtk proxy go vet ./...` instead: passed with no output.
  - `rtk git diff --check`: passed with no output.

Concerns:
- Pre-existing modification `.superpowers/sdd/task-2-report.md` was left untouched and is not part of this fix commit.
