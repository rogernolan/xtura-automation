# Long-term Sensor History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Retain 10-minute sensor samples for 30 days and hourly samples indefinitely for temperature and water without introducing SQLite.

**Architecture:** Extend the existing file-backed history stores with a recent tier and partitioned hourly archive files. Keep live memory and default API responses bounded; compact deterministically and atomically. Preserve legacy NDJSON input during migration.

**Tech Stack:** Go, standard library, NDJSON, existing HTTP/SSE APIs, Go tests.

## Global Constraints

- Do not add SQLite or a third-party storage dependency.
- Preserve current live telemetry decoding and event detection behavior.
- Use UTC timestamps and deterministic temporary-directory tests.
- Do not modify the unrelated untracked capture file.

### Task 1: Temperature history tiers

**Files:**
- Modify: `service/history/store.go`
- Test: `service/history/store_test.go`

- [ ] Add failing tests proving compaction keeps 30-day 10-minute samples, writes older samples into an hourly archive partition, preserves the archive across repeated compactions, and reloads both tiers.
- [ ] Run `rtk test go test ./service/history -run 'Test.*Retention|Test.*Archive'` and confirm the new tests fail for missing archive behavior.
- [ ] Implement constants and compaction helpers for a 30-day recent retention, 10-minute recent buckets, and monthly hourly archive files. Keep the in-memory window at two hours and make `Recent` unchanged.
- [ ] Make archive writes atomic and make startup load only the recent tail for live use; archive reads remain separate from `Recent`.
- [ ] Run `rtk test go test ./service/history` and confirm the package passes.

### Task 2: Water history tiers

**Files:**
- Modify: `service/waterhistory/store.go`
- Test: `service/waterhistory/store_test.go`

- [ ] Add failing tests proving water samples are reduced to 10-minute recent buckets, older points become indefinite hourly archive points for fresh and grey values, and legacy sample files still load.
- [ ] Run `rtk test go test ./service/waterhistory -run 'Test.*Retention|Test.*Archive'` and confirm expected failures.
- [ ] Implement the same tiering policy in the water store while preserving fill/empty event detection and current summaries. Keep event records independent from sampled history.
- [ ] Ensure compaction does not delete hourly archives and that missing fresh/grey values remain missing rather than becoming zero.
- [ ] Run `rtk test go test ./service/waterhistory` and confirm the package passes.

### Task 3: Runtime/API compatibility

**Files:**
- Modify: `service/runtime/app.go`, `service/api/httpapi/server.go` only if response bounding needs adjustment
- Test: `service/runtime/sensors_test.go`, `service/api/httpapi/server_test.go`

- [ ] Add focused tests proving default overview/history responses remain bounded after archive data exists and that hourly compaction still runs.
- [ ] Run the focused tests and confirm they fail only if the runtime/API contract needs changes.
- [ ] Wire the new stores without changing telemetry cadence, deduplication, stale handling, or endpoint defaults.
- [ ] Run `rtk test go test ./service/runtime ./service/api/httpapi`.

### Task 4: Full verification and documentation

**Files:**
- Modify: `docs/garmin-empirbus-signals.md` only if storage behavior needs documenting

- [ ] Run `rtk test go test ./...`.
- [ ] Inspect the diff and verify the unrelated capture remains unmodified.
- [ ] Document the resulting retention policy in the appropriate repository documentation.
- [ ] Run the final full test command again and report any environment-limited checks explicitly.
