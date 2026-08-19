# Temperature Web Push Alerts

## Goal

Allow the user to configure multiple temperature alerts and receive browser push
notifications when a selected sensor crosses a high or low limit. Alerts must
continue to work when the dashboard is closed.

## Product behavior

Each alert is an independent persisted record with:

- a stable id and user-facing name;
- one selected sensor id;
- an optional high limit in Celsius;
- an optional low limit in Celsius; and
- delivery mode: `crossing` or `repeat`.

At least one limit is required. If both limits are present, low must be less
than high. Multiple records may select the same sensor.

`crossing` sends once when entering a high or low violation and re-arms that
side after the value returns to the safe range. `repeat` sends on entry and
again every five minutes while that side remains violated. High and low sides
have independent runtime state. A missing, stale, or invalid reading does not
trigger or re-arm an alert.

The sensor picker includes Alde and configured SwitchBot sensors. Unknown or
removed sensors make the settings update invalid rather than silently changing
the alert target.

## Architecture

The Go runtime owns threshold evaluation. Sensor samples are passed to an
alert evaluator after they are accepted into the existing sensor runtime. The
evaluator maintains per-alert, per-side state in memory and emits notification
jobs. Configuration changes replace the alert definitions and clear runtime
state for changed alerts.

Browser subscriptions are registered through the Web Push API. The service
stores subscriptions in a separate runtime data file, because they are browser
installation state rather than product configuration. A subscription contains
the endpoint and encrypted-key material required by Web Push. Failed or
expired subscriptions are removed when the push provider reports them as
invalid; transient failures are logged without disabling the alert.

VAPID public/private credentials are server configuration. The private key is
never returned by an API or included in the web bundle. The public key is
returned by a read-only push capabilities endpoint so the browser can create a
subscription.

The static web app gains a service worker that receives push payloads and
displays notifications. Notification clicks open or focus the dashboard and
navigate to the relevant sensor/settings view when possible.

## API and persistence

Add a notification settings endpoint for reading and replacing the alert list,
plus endpoints for push capabilities, subscription registration, and
subscription removal. Settings updates use the existing config persistence and
validation path. Push subscriptions use an atomic JSON runtime file alongside
the existing service runtime state.

The notification payload includes the alert name, sensor name, observed Celsius
value, violated side, configured limit, and timestamp. It contains no private
VAPID material.

## Web UI

Add a Notifications section to More > Settings containing:

- browser notification permission and subscription status;
- subscribe/unsubscribe controls;
- a list of configured alerts;
- add, edit, and remove controls;
- sensor picker, high/low limit fields, and delivery mode; and
- clear validation and save errors.

The UI requests permission only after the user explicitly chooses to enable
push. It refreshes the alert list and sensor list after successful changes.

## Testing

Unit tests cover alert validation, high/low crossing transitions, re-arming,
five-minute repeat scheduling, independent high and low state, invalid readings,
and duplicate sensor targets. Runtime tests cover sample-to-evaluator wiring,
settings persistence, subscription persistence, and cleanup of invalid push
subscriptions. HTTP tests cover all new routes and ensure private VAPID data is
not exposed. JavaScript tests cover subscription flow and alert form rendering.

The existing Go and web test suites remain passing, and the simulator is used
for final UI/API verification because static web assets are embedded at build
time.

## Deployment constraints

Browser push requires HTTPS outside localhost. Staging deployment must provide
stable VAPID credentials and a writable runtime data directory. The deploy and
simulator documentation will call out the required configuration and the
permission/HTTPS limitation.
