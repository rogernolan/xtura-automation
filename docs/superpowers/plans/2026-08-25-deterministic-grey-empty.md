# Deterministic Grey-Tank Empty Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace grey-tank level-drop event detection with the received EmpirBus open-then-close discharge sequence, while retaining the fresh-water fill heuristic and logging unexpected downward grey-level changes.

**Architecture:** The Garmin adapter will emit one-shot discharge edge events only for newly received signal-4 opens and signal-5 closes. Runtime will feed those edges into `waterhistory.Store`; the store will persist a pending discharge cycle and create the grey-empty event at close. Percentage samples remain the source for chart/current-level data, fresh fills, and anomaly diagnostics.

**Tech Stack:** Go, existing `testing` package, Garmin EmpirBus session, NDJSON/JSON water-history persistence.

## Global Constraints

- Signal `4` starts a grey discharge cycle; signal `5` completes it.
- A close without a preceding open is ignored.
- Quick open/close sequences are preserved; there is no one-minute suppression rule.
- Grey percentage movement alone never creates an empty event.
- Downward grey movement at or above `WaterHistoryConfig.ThresholdPercent` without a corresponding open logs an error; upward filling does not.
- Preserve the existing untracked capture file and do not modify unrelated worktree changes.

## File Map

- Modify `heating/session.go` and `heating/heating_test.go`: expose received signal transition edges without replaying stale initial state.
- Modify `service/adapters/garmin/adapter.go` and `service/adapters/garmin/adapter_test.go`: translate signal-4/5 edges into one-shot grey-discharge events.
- Modify `service/runtime/app.go`, `service/runtime/sensors.go`, and `service/runtime/sensors_test.go`: consume adapter events before water samples and pass them to history.
- Modify `service/waterhistory/types.go`, `service/waterhistory/store.go`, and `service/waterhistory/store_test.go`: persist discharge-cycle state, create deterministic empty events, and diagnose unpaired downward changes.
- The approved design is recorded in `docs/superpowers/specs/2026-08-25-deterministic-grey-empty-design.md`; implementation must not modify it.

### Task 1: Add transition-edge tracking to the EmpirBus session

**Files:**
- Modify: `heating/session.go`
- Test: `heating/heating_test.go`

**Interfaces:**
- Produces `func (s *Session) DrainReceivedSignalEdges() []SignalEdge`.
- `SignalEdge` contains `Signal int`, `At time.Time`, and `On bool`.
- Only received signal updates that change the signal’s on/off state produce an edge; the first received state is baseline only.

- [ ] **Step 1: Write the failing tests**

Add tests that ingest signal 4 and signal 5 frames and assert the first state for each signal produces no edge, a later state change produces one edge with the receive timestamp, draining empties the queue, and send frames do not produce edges.

- [ ] **Step 2: Run the focused tests and verify the expected failure**

Run: `rtk test go test ./heating -run 'TestSession.*Signal.*Edge'`

Expected: FAIL because `SignalEdge` and `DrainReceivedSignalEdges` do not exist.

- [ ] **Step 3: Implement minimal edge tracking**

Add a `signalKnown map[int]bool`, `signalOn map[int]bool`, and `signalEdges []SignalEdge` to `Session`. In `ingest`, for received frames with at least three data bytes, compare `frame.Wire.Data[2]&1 != 0` with the previous state; enqueue a `SignalEdge` only when a known state changes. Add a mutex-protected drain method that returns a copy and clears the queue.

- [ ] **Step 4: Run the focused tests and verify they pass**

Run: `rtk test go test ./heating -run 'TestSession.*Signal.*Edge'`

Expected: PASS.

- [ ] **Step 5: Commit**

Run: `rtk git add heating/session.go heating/heating_test.go && rtk git commit -m "feat: track received EmpirBus signal edges"`.

### Task 2: Translate discharge edges in the Garmin adapter

**Files:**
- Modify: `service/adapters/garmin/adapter.go`
- Test: `service/adapters/garmin/adapter_test.go`

**Interfaces:**
- Produces `type GreyWaterDischargeEvent struct { Kind string; At time.Time }` in the Garmin adapter package.
- Produces `func (a *Adapter) DrainGreyWaterDischargeEvents() []GreyWaterDischargeEvent`.
- The adapter emits `KindOpen` for signal 4 on-edges and `KindClose` for signal 5 on-edges; off-edges are ignored.

- [ ] **Step 1: Write failing adapter tests**

Add tests using a session-backed adapter fixture: signal 4 on creates one open event, signal 5 on creates one close event, off transitions create none, repeated status frames create no duplicates, and the drain method returns each event once.

- [ ] **Step 2: Run the focused tests and verify the expected failure**

Run: `rtk test go test ./service/adapters/garmin -run 'Test.*GreyWater.*Discharge'`

Expected: FAIL because the adapter event type and drain method do not exist.

- [ ] **Step 3: Implement adapter event translation**

In `pollState`, drain session edges after state polling and append only signal-4/signal-5 on-edges to an adapter-owned queue protected by `Adapter.mu`. Add the event constants and the drain method. Reset the queue when the adapter is newly constructed; do not synthesize events from the session’s initial state.

- [ ] **Step 4: Run adapter tests and the existing adapter suite**

Run: `rtk test go test ./service/adapters/garmin`

Expected: PASS.

- [ ] **Step 5: Commit**

Run: `rtk git add service/adapters/garmin/adapter.go service/adapters/garmin/adapter_test.go && rtk git commit -m "feat: expose grey water discharge edges"`.

### Task 3: Add deterministic discharge recording and anomaly diagnostics

**Files:**
- Modify: `service/waterhistory/types.go`
- Modify: `service/waterhistory/store.go`
- Test: `service/waterhistory/store_test.go`

**Interfaces:**
- Add `Options.Logf func(format string, args ...interface{})` for diagnostic logging; nil means no logging.
- Add `func (s *Store) RecordGreyDischargeOpen(at time.Time) (bool, error)`.
- Add `func (s *Store) RecordGreyEmpty(at time.Time) (bool, error)`.
- `RecordGreyDischargeOpen` persists a pending open timestamp and is idempotent for the same timestamp.
- `RecordGreyEmpty` creates one `TankGrey`/`KindEmpty` event only when a pending open exists, then clears it; unmatched close returns `(false, nil)`.

- [ ] **Step 1: Write failing store tests**

Add tests for unmatched close, open then close, repeated close idempotence, persistence/reload of a pending open, grey level drop without open logging an error and creating no event, upward grey movement producing no error, and fresh fill detection still requiring its configured threshold and settling period.

- [ ] **Step 2: Run the focused tests and verify the expected failures**

Run: `rtk test go test ./service/waterhistory -run 'Test.*Grey|TestFillRequires'`

Expected: FAIL because the deterministic methods, persisted pending-open state, and anomaly logging do not exist.

- [ ] **Step 3: Implement deterministic store methods**

Extend persisted state with a pending grey discharge-open timestamp. Keep `Observe`’s fresh-water call to `observeTank`; remove its grey-water call. In grey sample handling, compare only downward movement against the configured threshold and call `Logf` when no pending discharge is active; do not create a candidate or event. `RecordGreyEmpty` should use the latest known grey level as `From`, use `0` as the deterministic empty `To`, set `Used` to the observed `From`, append the event, persist state/events, and clear the pending open.

- [ ] **Step 4: Run focused and full water-history tests**

Run: `rtk test go test ./service/waterhistory`

Expected: PASS with no unexpected output; the existing grey heuristic test must be replaced by a test proving level movement alone is not an event.

- [ ] **Step 5: Commit**

Run: `rtk git add service/waterhistory/types.go service/waterhistory/store.go service/waterhistory/store_test.go && rtk git commit -m "feat: record grey empties from discharge completion"`.

### Task 4: Wire adapter events into runtime and verify integration

**Files:**
- Modify: `service/runtime/app.go`
- Modify: `service/runtime/sensors.go`
- Test: `service/runtime/sensors_test.go`

**Interfaces:**
- Add optional `GreyWaterDischargeProvider` with `DrainGreyWaterDischargeEvents() []garmin.GreyWaterDischargeEvent`.
- Runtime consumes open events before water samples and close events after the matching open state is recorded.
- Runtime passes `cfg.WaterHistory.ThresholdPercent` and `logger.Printf` to the water-history store.

- [ ] **Step 1: Write failing runtime tests**

Add a fake provider that returns an open event followed by a close event, invoke the water observation path twice, and assert one grey-empty event. Add a test where grey falls by the threshold with no provider open event and assert the logger receives an error while the history remains event-free.

- [ ] **Step 2: Run the focused runtime tests and verify the expected failure**

Run: `rtk test go test ./service/runtime -run 'Test.*Grey|Test.*WaterHistory'`

Expected: FAIL because runtime does not define the provider, consume discharge events, or configure diagnostic logging.

- [ ] **Step 3: Implement runtime wiring**

Capture the optional provider from the adapter during `New`. Add a helper called at the start of each publish-loop tick that drains events: call `RecordGreyDischargeOpen` for open events and `RecordGreyEmpty` for close events, publishing `water.history_changed` when either changes history. Configure `waterhistory.Options.Logf` with `logger.Printf` when constructing the store. Keep `observeWaterTelemetry` responsible for samples and fresh detection only.

- [ ] **Step 4: Run package and repository verification**

Run: `rtk test go test ./heating ./service/adapters/garmin ./service/waterhistory ./service/runtime`

Then run: `rtk test go test ./...`

Expected: both commands PASS. Confirm `rtk git diff --check` is clean and the untracked capture remains untouched.

- [ ] **Step 5: Commit**

Run: `rtk git add service/runtime/app.go service/runtime/sensors.go service/runtime/sensors_test.go && rtk git commit -m "feat: wire deterministic grey tank empty events"`.
