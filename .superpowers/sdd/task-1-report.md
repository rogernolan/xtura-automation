# Task 1 Report

Status: DONE

Work completed:
- Added received EmpirBus signal transition-edge tracking to `heating/session.go`.
- Added focused coverage in `heating/heating_test.go` for baseline receive behavior, later state transitions, drain semantics, and ignoring send frames.
- Kept the implementation aligned with the brief: first receive per signal is baseline only, only receive-side on/off changes enqueue edges, and draining clears the queue.

Implementation commit:
- `53b8a8a` - `feat: track received EmpirBus signal edges`

Tests:
- Initial red check:
  - `rtk test go test ./heating -run 'TestSession.*Signal.*Edge'`
  - Output: `FAIL	empirebus-tests/heating [build failed]`
- Verification after implementation:
  - `rtk test go test ./heating -run 'TestSession.*Signal.*Edge'`
  - Output: `ok  	empirebus-tests/heating	(cached)`
  - `rtk test go test ./heating`
  - Output: `ok  	empirebus-tests/heating	0.268s`

Concerns:
- None.

---

# Task 1 Fix Report

Status: DONE

Fix applied:
- Restricted `Session.ingest` so only received 0->1 transitions append a `SignalEdge`.
- Kept received 1->0 transitions as baseline state updates only, with no queued edge.
- Updated `heating/heating_test.go` to assert the off transition is absent while preserving drain idempotence coverage.

Fix commit:
- `6a1d8ab` - `fix: restrict received signal edges to on transitions`

Tests:
- `rtk test go test ./heating -run 'TestSession.*Signal.*Edge'`
  - `ok  	empirebus-tests/heating	0.636s`
- `rtk test go test ./heating`
  - `ok  	empirebus-tests/heating	0.480s`

Concerns:
- None.
