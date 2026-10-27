# Alde shore-power warning implementation plan

**Goal:** Push one warning after Alde's selected electric setting has exceeded
the configured shore allowance for 60 seconds, including disconnected shore.

**Architecture:** Decode the capture-confirmed Garmin indications into a heating
domain supply snapshot. A separate notification evaluator owns the timer and
episode latch; the existing runtime loop and Web Push sender deliver its output.
Settings persist an optional enable flag, enabled when absent for existing installs.

**Execution:** Inline in this chat using executing-plans and test-driven-development.
User approval: “OK, implement this please”, following the recorded design and
choice to warn from selected power even while the thermostat is idle.

## Constraints

- Delay is 60 seconds; one notification per continuous shortfall.
- Compare selected watts with input-current limit × valid input voltage.
- Confirmed absent input has zero allowance; lost/unknown telemetry does not.
- Selected Off/1/2/3 kW uses 89/88/87/97; 1 kW retains catalogue-only evidence.
- Preserve heater-on intent through the captured 101=129 transition.
- Unknown data resets the pending timer but does not rearm a sent episode.
- New Garmin sessions require a fresh timer and fresh session-local signals.
- Existing temperature settings and notification payloads remain compatible.
- No deployment or physical-device commands are part of implementation testing.

## Tasks

### 1. Decode the electric setting and shore allowance

Files: new `service/domains/heating/electric_supply.go`,
`service/adapters/garmin/electric_supply.go`, and
`service/adapters/garmin/electric_supply_test.go`; adapter session generation
is owned by `service/adapters/garmin/adapter.go`.

Interface: `ElectricSupplyState(now time.Time) heating.ElectricSupplyState`.
The domain snapshot carries Known, HeaterOn, ElectricWatts, ShoreConnected,
InputLimitA, InputVoltageV, ShoreWatts, and a session generation.

- [x] Write local WebSocket tests for connected 3.1 A/233 V, disconnected
  `[28,0,128]` plus unavailable zero input, 2/3 kW selection, unsupported
  113–115 indicators, heater transition/off, invalid frames, silent sessions,
  and replacement sessions with no cached state.
- [x] Run `rtk go test ./service/adapters/garmin -run ElectricSupply`; confirm
  failure for the missing feature.
- [x] Implement the domain snapshot and strict decoder. Accept only received
  status families, expected scalar types, byte-valid payloads and supported
  flags. Require a live session receiving frames within 15 seconds. Treat the
  paired unplug indications as absent input, not arbitrary unavailability.
- [x] Rerun decoder tests to green.

### 2. Evaluate and deliver the warning

Files: new `service/notifications/alde_power.go` and tests; update
`alerts.go`, `service/config/config.go`, `service/runtime/app.go`,
`service/runtime/notifications.go`, and runtime tests.

Interface: `AldePowerEvaluator.Evaluate(state, enabled, now) []Notification`.
Notification adds optional kind/title/body fields for the new message;
temperature clients retain their original fields. Subscription format version
2 is persisted only after the activated worker confirms it over MessageChannel.
New alerts wait for a compatible phone; known recovery still re-arms them while
there is no eligible phone. Session liveness uses receive-only timestamps.

- [x] Write fake-clock tests for 59/60 seconds, connected low allowance,
  unplug, sufficient allowance, electric/heater off, disable/re-enable,
  unknown data, session changes, one delivery, and confirmed recovery rearm.
- [x] Run `rtk go test ./service/notifications -run AldePower`; confirm red.
- [x] Implement the evaluator and default-enabled optional
  `alde_power_alert_enabled` setting. Preserve that field on older clients'
  temperature-only settings updates. Wire the existing one-second runtime
  loop to a provider and evaluator, then the existing sender.
- [x] Test config persistence and runtime integration; run focused packages.

### 3. Settings UI, push rendering, documentation and verification

Files: `web/static/index.html`, `app.js`, `sw.js`, corresponding JS tests,
`package.json`, `README.md`, `config.example.yaml`, and the signal reference.

- [x] Write service-worker execution tests for the new power notification and
  legacy temperature messages; add a Settings toggle test that preserves
  temperature alerts and enablement during other alert edits.
- [x] Run `rtk npm test` to confirm new tests fail, then implement rendering
  and settings changes. Register/update the worker on ordinary supported page
  loads so existing subscriptions can receive the new message format.
- [x] Document the rule, default, limits, signal dependencies and enable path.
- [x] Run `rtk go test ./...`, focused race tests, `rtk npm test`,
  `rtk npm run lint`, and `rtk git diff --check`.
- [x] Run `rtk ./scripts/sim/run-sim.sh captures/garmin-ws-sim.ndjson`;
  verify served settings round-trip and the new UI/worker assets through the
  local API. Stop the sim using its script session.
- [x] Review the final diff for timer lifecycle, reconnect state, persistence,
  compatibility and test gaps; fix material findings and rerun affected checks.

## Verification evidence

- Full Go suite: 525 tests passed across 35 packages.
- Focused race run: 208 tests passed across heating, notifications, Garmin and runtime.
- JavaScript: 63 tests passed; ESLint and diff whitespace checks passed.
- Sim rebuilt using run-sim.sh; health, embedded UI/worker, enable/disable persistence, and legacy settings updates passed through localhost:8091.
- Required architecture review found three lifecycle/compatibility issues; all were fixed with regression coverage and the follow-up review found no remaining material issues.
- The optional real-browser check stalled in the browser tool. Actual phone delivery and the live 1 kW selection were not verified. No Pi deployment or hardware commands were performed.
- Existing Mac sim BLE/history-path warnings are unrelated to this feature; API/asset verification succeeded.
- Work remains on codex/alde-shore-power-alert in the original checkout; existing user documentation edits and deletions were preserved.
