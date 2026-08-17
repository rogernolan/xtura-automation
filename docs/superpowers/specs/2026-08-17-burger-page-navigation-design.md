# Burger Page Navigation Design

Date: 2026-08-17

## Purpose

Replace the mobile web UI's persistent bottom tab bar and inline section
pickers with individual pages reached from a burger menu. The change keeps the
existing live data, control actions, safety confirmations, and visual identity
while making each domain directly addressable and easier to understand on a
phone.

## Information architecture

The application has one shared shell and eight canonical pages:

| Page | Hash route | Ownership |
| --- | --- | --- |
| Overview | `#/overview` | Read-only vehicle summary and deep links |
| Heating | `#/heating` | Alde mode, target, boost, and schedule |
| Water | `#/water` | Grey-water valve and schedule |
| Lighting | `#/lighting` | Exterior-light controls |
| Location | `#/location` | GPS trail controls and recorded tracks |
| System | `#/system` | Pi status and deployment information |
| Tools | `#/tools` | WebSocket recording |
| Settings | `#/settings` | Overview comfort and capacity settings |

Each route shows exactly one page content container. The existing grouped
Controls and More section pickers are removed; their panels become independent
pages without changing their APIs or control behavior.

## Navigation and interaction

The header contains the current page title, connection status, and a burger
button with an accessible label and expanded state. Activating the button opens
a navigation drawer containing links to all eight pages. The drawer:

- closes after a page link is selected;
- closes through an explicit close action and Escape;
- closes when the user activates the backdrop;
- marks the current page with `aria-current="page"`; and
- does not perform backend mutations.

The drawer is reachable by keyboard, and focus is moved into it when opened
and returned to the burger button when closed. The page remains usable when the
drawer is closed, including on narrow screens.

Navigation state is derived from and written to the URL hash. Initial load,
refresh, direct links, and browser back/forward restore the corresponding page.
Unknown, incomplete, or nested hashes fall back to `#/overview`. Page changes
must not recreate the EventSource or reset user-entered form state unless the
existing application behavior already requires it.

## Overview deep links

Overview remains read-only. Its cards navigate to their obvious individual
pages:

- Alde main temperature and every temperature panel link to `#/heating`.
- Fresh-water and grey-water cards link to `#/water`.
- Battery and charge-current cards link to `#/system` unless a dedicated
  energy page exists later.
- Gas links to `#/settings` while the Mopeka source remains unconfigured.

Cards must not send control requests. Their links must work with keyboard and
pointer input and preserve the normal browser history behavior.

## Implementation shape

Use the existing static frontend and shared application state. Replace the
current primary-screen and section-selection model with a page route model and
page visibility map. Keep existing element IDs where practical so live
rendering and action binding remain stable. Update the HTML structure so each
domain has its own page container, and add the burger drawer to the shared
shell. Update CSS for the drawer, page header, active link, backdrop, and
mobile-friendly content spacing.

No backend or API changes are required.

## Error handling and compatibility

An invalid route must never leave a blank page. It redirects to the canonical
Overview route. Existing loading, stale, unavailable, error, SSE reconnect,
and control-action error states remain visible within their owning page.

The route parser should accept only the eight canonical page paths. Legacy
`#/controls/...` and `#/more/...` paths are not retained as public routes; they
fall back to Overview so there is one unambiguous URL per page.

## Verification

Automated tests will cover:

- parsing and serialization of all eight page routes;
- fallback for legacy, nested, and unknown routes;
- presence of the burger button, drawer links, page containers, and no section
  picker/tab-bar contract;
- active-page accessibility state and drawer open/close behavior;
- Overview deep links, including both temperature panels to Heating; and
- existing heating, water, lighting, location, recording, system, and settings
  bindings remaining attached.

Run the project's existing checks plus the frontend tests, `go test ./...`,
`go vet ./...`, `go build ./...`, and `git diff --check` before completion.

## Scope boundaries

This work changes navigation and page ownership only. It does not add Energy,
new telemetry, SwitchBot setup, map views, authentication, or a backend API.
