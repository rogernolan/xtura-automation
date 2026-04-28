# Mobile Web UI Design

## Goal

Add a small mobile-first web UI for the existing EmpireBus service. The UI is disposable and intentionally modest: enough for LAN/Tailscale use by at most three concurrent users, with no Node runtime, no client polling, and no new durable frontend architecture.

The initial UI has two tabs:

- Lighting: flash the exterior lights
- Heating: edit the shared daily schedule, set common targets, adjust target by `0.5C`, and start a `21C` one-hour boost

## Selected Direction

Use the approved "Compact Control Panel" mockup direction from the brainstorming companion.

Characteristics:

- phone-first single-column layout
- two tabs: Lighting and Heating
- quiet utility styling rather than a marketing page
- compact panels with large touch targets
- use `assets/XturaBackground.png` as a subtle background texture
- plain HTML, CSS, and JavaScript
- visible product text uses `Xtura`, not `XTura`

This should feel like a practical van control panel, not a finished product surface. It should be easy to delete and replace later.

## Constraints

- Serve from the existing Go daemon process unless a later operational reason makes a second Go daemon preferable.
- Do not require Node, npm, bundlers, or frontend build tooling.
- Avoid external dependencies.
- Static site assets should be embedded or served directly by Go.
- Use the same `/v1/...` HTTP API consumed by the iOS client.
- Avoid polling. Use one initial fetch per needed resource and subscribe to `GET /v1/events` with `EventSource` for live updates.
- Assume access is LAN-only or Tailscale-only; do not add web auth in this slice.
- Optimize for constrained hardware over animation or rich UI.

## Approaches Considered

### 1. Recommended: static assets served by the existing daemon

Add static files under a small web directory and mount them from `service/api/httpapi.Server.Handler`.

Pros:

- one process to deploy and supervise
- same host and origin as the API, so no CORS work
- simplest operational model on a Pi Zero 2 W
- easy to remove later

Cons:

- the API package also owns static serving for now

### 2. Separate Go web daemon

Build a second tiny Go binary that serves the static UI and proxies or calls the API.

Pros:

- clearer process separation
- can be restarted independently

Cons:

- more deployment and systemd complexity
- CORS or proxying becomes a consideration
- unnecessary for three local users

### 3. Static files served by an external web server

Serve the UI from nginx, Caddy, or another local web server.

Pros:

- well-known static serving behavior

Cons:

- adds another moving part
- violates the preference for no extra runtime or service dependency

## Recommended Architecture

Add:

- `web/static/index.html`
- `web/static/styles.css`
- `web/static/app.js`
- `web/static/xtura-background.png`, copied or moved from `assets/XturaBackground.png`
- optional small SVG favicon or no favicon

Use Go `embed` for these assets so the deployed binary contains the UI. Mount routes:

- `GET /` returns the static app entrypoint
- `GET /ui` redirects to `/` or serves the same app
- `GET /static/...` serves CSS/JS assets
- existing `/v1/...` API routes keep their current behavior

No frontend build step is introduced.

## Client Data Flow

On page load:

1. Fetch `GET /v1/lights/state`.
2. Fetch `GET /v1/heating/mode`.
3. Fetch `GET /v1/automation/heating-schedule`.
4. Open `EventSource("/v1/events")`.

On server-sent events:

- `lights.state_changed`: replace local lights state from `payload`
- `heating.mode_changed`: replace local heating mode from `payload`
- `automation.schedule_updated`: replace local schedule from `payload`
- `heating.state_changed`: update displayed heating state if the UI chooses to show current power/target
- connection events may update a small status indicator

If the SSE connection drops, the browser-native `EventSource` reconnect behavior is enough for this disposable first version. The UI should show a small disconnected/reconnecting status, but it should not add its own polling loop.

## Lighting Tab

The Lighting tab contains:

- current exterior lights status when known
- current flash busy state
- one primary button: `Flash exterior lights`

Button behavior:

- call `POST /v1/lights/external/flash` with `{ "count": 1 }`
- disable while `flash_in_progress` is true or while the request is in flight
- on success, replace local lights state with the response body
- on `409`, show a brief busy message and wait for the next state update
- on failure, show the returned error text

## Heating Tab

The Heating tab contains:

- current mode summary
- target controls
- boost button
- schedule editor

Target controls:

- quick target buttons: `5C`, `18C`, `21C`
- decrement and increment buttons that adjust by `0.5C`
- setting a target calls `POST /v1/heating/mode/manual` with `{ "target_celsius": value }`

Boost behavior:

- `Boost 21C for 1 hour` calls `POST /v1/heating/mode/boost` with `{ "target_celsius": 21, "duration_minutes": 60 }`
- if mode is `boost`, show the expiry time and offer `Cancel boost`
- cancelling calls `POST /v1/heating/mode/boost/cancel`

Schedule editor:

- display one shared daily schedule from the schedule document
- edit up to four visible slots
- each slot has start time, mode, and target temperature when mode is `heat`
- save sends a full replacement document to `PUT /v1/automation/heating-schedule`, preserving `revision`
- use all seven days for the shared schedule
- keep hidden midnight coverage implicit in the document shape: the first period may be `00:00 off`, and the final slot continues until midnight by scheduler semantics

For v1, the editor should normalize to a single enabled program that covers every day. If the fetched document already contains multiple enabled programs, the UI should show an unsupported-shape message rather than trying to edit it incorrectly.

## Error Handling

Use a small status area at the bottom of each tab:

- `Saved`
- `Busy`
- `Disconnected`
- returned validation messages from `400 validation_failed`
- revision conflict message for `409` schedule saves: refetch happened, review and save again

The UI should favor clear text over complex inline validation.

## Accessibility And Mobile Behavior

- all controls are native buttons, inputs, or selects
- touch targets are at least `44px` tall
- text never relies on hover
- layout works from narrow phone width upward
- desktop view remains a centered phone-like control surface rather than a separate dashboard
- no viewport-scaled font sizes

## Testing

Add Go HTTP tests for:

- `/` returns `text/html`
- `/static/app.js` returns JavaScript
- existing `/v1/...` routes still work after static route mounting

Manual verification:

- run `go test ./...`
- run `go run ./cmd/empirebusd -config ./config.example.yaml`
- open the UI in a browser
- verify both tabs render on mobile and desktop widths
- verify controls call the documented API endpoints
- verify no client polling timers are present

## Out Of Scope

- PWA manifest and service worker
- offline mode
- authentication
- HTTPS termination
- multi-day schedule editing
- rich charts or history
- full design system
- frontend bundling

## Success Criteria

This slice is complete when:

- the daemon serves the web UI without Node or other runtimes
- the UI uses the same `/v1/...` API as the iOS client
- the UI does not poll
- Lighting can flash exterior lights
- Heating can save the shared daily schedule
- Heating can set manual targets to `5C`, `18C`, `21C`, and `0.5C` increments
- Heating can start a `21C` one-hour boost
- tests prove static serving does not break existing API routes
