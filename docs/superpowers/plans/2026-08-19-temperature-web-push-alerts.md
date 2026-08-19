# Temperature Web Push Alerts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add persisted multi-sensor temperature alerts with server-side browser Web Push delivery, crossing/repeat modes, and Settings UI controls.

**Architecture:** Add a focused `service/notifications` package for alert definitions, validation, state transitions, subscriptions, and push delivery. The runtime feeds accepted sensor samples into the evaluator and exposes notification settings/subscription operations through the existing HTTP API. The static app registers a service worker and manages permission, subscription, and alert forms.

**Tech Stack:** Go 1.25, existing YAML/JSON persistence and HTTP API, Web Push/VAPID library, vanilla JavaScript/CSS, service-worker Push API.

## Global Constraints

- Alerts are evaluated server-side so they work when the dashboard is closed.
- Each alert has a sensor, optional high limit, optional low limit, and `crossing` or `repeat` delivery mode.
- Repeat notifications occur every five minutes while the same side remains violated.
- Missing, stale, or invalid readings never trigger or re-arm an alert.
- Browser push requires HTTPS outside localhost.
- Preserve the existing untracked capture file and unrelated worktree changes.

---

### Task 1: Alert domain and evaluator

**Files:**
- Create: `service/notifications/alerts.go`
- Test: `service/notifications/alerts_test.go`

**Interfaces:**
- Produces `Alert`, `Settings`, `DeliveryMode`, `Notification`, and `Evaluator.Evaluate(sensorID, sensorName string, temp float64, at time.Time) []Notification`.
- `Settings.Validate(sensorIDs map[string]struct{}) error` validates limits, mode, ids, and known sensors.

- [ ] Write failing tests for validation, high/low crossing, independent sides, re-arming, repeat timing, and invalid temperatures.
- [ ] Run `go test ./service/notifications`; confirm failures are due to missing types/behavior.
- [ ] Implement the smallest evaluator with per-alert/per-side runtime state and a five-minute repeat interval.
- [ ] Run the focused tests until green, then run `go test ./service/notifications` again.
- [ ] Commit `feat: add temperature alert evaluator`.

### Task 2: Config and subscription persistence

**Files:**
- Modify: `service/config/config.go`
- Create: `service/notifications/subscriptions.go`
- Test: `service/config/config_test.go`
- Test: `service/notifications/subscriptions_test.go`

**Interfaces:**
- `config.Config.Notifications` stores VAPID public/private keys and notification alert settings.
- `notifications.SubscriptionStore` loads, atomically saves, registers, and removes browser subscriptions from a runtime JSON file.

- [ ] Add failing config tests for alert round-tripping and validation against Alde plus configured SwitchBot ids.
- [ ] Add failing subscription-store tests for atomic persistence, replacement by endpoint, removal, and reload.
- [ ] Run both focused test files and confirm expected failures.
- [ ] Add config types/validation and the subscription store without exposing private VAPID data in JSON DTOs.
- [ ] Run focused tests and all config/notification tests.
- [ ] Commit `feat: persist notification settings and subscriptions`.

### Task 3: Web Push sender

**Files:**
- Modify: `go.mod`
- Create: `service/notifications/push.go`
- Test: `service/notifications/push_test.go`

**Interfaces:**
- `PushSender.Send(ctx context.Context, subscription Subscription, notification Notification) error`.
- `PushSender` uses configured VAPID credentials and an injectable HTTP client for tests.

- [ ] Add a failing sender test asserting payload fields, VAPID configuration use, and invalid-subscription classification.
- [ ] Run the focused test and confirm it fails before the dependency/implementation exists.
- [ ] Add the Web Push dependency and implement payload encoding, VAPID signing, and response classification.
- [ ] Run focused notification tests and verify no private key is present in the payload or capability response type.
- [ ] Commit `feat: send temperature web push notifications`.

### Task 4: Runtime integration and HTTP API

**Files:**
- Modify: `service/runtime/app.go`
- Modify: `service/runtime/sensors.go`
- Modify: `service/api/httpapi/server.go`
- Modify: `service/api/httpapi/server_test.go`
- Modify: `service/runtime/sensors_test.go`
- Create: `service/runtime/notifications.go`

**Interfaces:**
- Runtime methods expose notification settings, push capabilities, subscription registration/removal, and sample evaluation to the HTTP layer.
- Add routes for notification settings, push capabilities, and subscription lifecycle with the existing GET/PUT/POST/DELETE method conventions.

- [ ] Add failing runtime/API tests for settings persistence, route methods, private-key omission, subscription registration, and threshold-triggered sends through a fake sender.
- [ ] Run targeted Go tests and confirm failures reflect missing runtime/API behavior.
- [ ] Wire evaluator calls after accepted Alde and SwitchBot samples, configure the sender/store at app startup, and publish/log delivery errors without blocking sensor processing.
- [ ] Implement DTOs, routes, validation mapping, and invalid-subscription cleanup.
- [ ] Run `go test ./service/runtime ./service/api/httpapi ./service/notifications`.
- [ ] Commit `feat: expose temperature notification API`.

### Task 5: Service worker and Settings UI

**Files:**
- Create: `web/static/sw.js`
- Modify: `web/static/index.html`
- Modify: `web/static/app.js`
- Modify: `web/static/app.css`
- Modify: `web/static/app.test.js`
- Modify: `web/static/navigation.test.js`

**Interfaces:**
- Browser calls the push capability/subscription endpoints and the notification settings endpoint.
- Service worker handles `push` and `notificationclick` events.

- [ ] Add failing JS tests for rendering the notification section, add/edit/remove alert rows, repeat mode, and subscription state.
- [ ] Run `npm test -- --runInBand` or the repository’s equivalent focused test command and confirm expected failures.
- [ ] Add service-worker registration, explicit permission request, subscribe/unsubscribe flow, alert form serialization, sensor picker population, and validation/error rendering.
- [ ] Add the worker payload display/focus behavior and preserve existing navigation selectors.
- [ ] Run both intended web test files and static checks.
- [ ] Commit `feat: add temperature alert settings UI`.

### Task 6: Documentation and end-to-end verification

**Files:**
- Modify: `config.example.yaml`
- Modify: `config.sim.yaml`
- Modify: `README.md`
- Modify: `docs/internal-api.md`

- [ ] Add documentation for VAPID configuration, writable subscription state, HTTPS/localhost requirements, and all new API routes.
- [ ] Run `go test ./...` and `npm test`.
- [ ] Run `./scripts/sim/run-sim.sh`, verify `/v1/health`, notification settings, service-worker availability, and the Settings UI in the simulator.
- [ ] Run `git diff --check` and review the final diff for unrelated changes.
- [ ] Commit `docs: document temperature web push alerts`.

