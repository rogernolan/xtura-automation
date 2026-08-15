# UX Polish Design

Date: 2026-08-15

## Purpose

Refine the phone-first bottom-navigation UI shipped in the information
architecture refactor (PR #6). Real-usage feedback found the navigation bar
clings to the content instead of the screen, the page title never changes, the
Controls/More sub-section switcher reads as a second tab system, the bottom bar
is not distinct enough from content, the deployment message sits in the wrong
place, and the background appears to change between screens.

This document defines the visual and interaction refinements only; it keeps the
routing model, section retention, defaults, and all control behaviour from the
previous design unchanged.

## Changes

### 1. Fixed bottom bar (P2)

The `.primary-nav` becomes a fixed, full-width bar pinned to the bottom of the
viewport:

- `position: fixed; left: 0; right: 0; bottom: 0`.
- Opaque backdrop with a top border and elevated shadow so it reads as system
  UI rather than a floating card.
- The four buttons stay constrained to the centred 430px content column
  (`max-width: 430px; margin-inline: auto`), matching the mockup option the user
  approved.
- `.app-shell` bottom padding increases so the last panel is never hidden behind
  the bar, and the bar adds `env(safe-area-inset-bottom)` padding for iPhone
  home-indicator overlap.
- Desktop behaviour is unchanged apart from the bar: the content column remains
  vertically centred on wide screens.

### 2. Dynamic title (P2)

- Remove the static "Xtura" eyebrow from `.app-header`.
- The page `<h1>` shows the active primary destination: Overview, Controls,
  Location, or More.
- `document.title` mirrors the same value so the browser tab stays in sync.
- Both are set when a route is applied, in `applyRoute`.

### 3. Dropdown sub-section switcher (P2)

Controls and More sections switch via a styled dropdown instead of the current
secondary tab row:

- Replace each `.section-switch` button row with a single `<select>` labelled
  by its destination, e.g. Controls: Heating / Water / Lighting; More: System /
  Tools.
- `change` on the select calls `navigate(screen, value)`.
- `applyRoute` syncs the select's value from the route, so `#/controls/water`
  and `#/more/tools` deep-links, back/forward, and refresh still restore the
  correct section.
- Section retention and defaults (Controls → Heating, More → System) are
  unchanged.
- De-duplication: a section heading that repeats the selected section name is
  removed or renamed. Heating's panel heading becomes "Mode"; Water ("Grey water
  valve"), Lighting ("Exterior lights"), System ("Pi status"), and Tools
  ("WebSocket recording") already differ from their section names and are
  unchanged.

### 4. Distinct bottom bar (P3)

Covered by the fixed full-width treatment in change 1: opaque backdrop, top
border, and shadow clearly separate the bar from content. The active item keeps
the accent fill used today.

### 5. Deployment message relocation (P4)

- Move the "Deployed `<date>` · `<sha>`" text out of the page-bottom `#buildInfo`
  footer into the More › System panel, rendered as a small muted line below the
  Pi status details.
- Remove the `#buildInfo` footer element from the page.

### 6. Stable background (P4)

- Anchor the background image to the viewport instead of the document so it
  renders identically on every screen regardless of scroll position or content
  height. Implement as a fixed viewport layer rather than relying on
  `background-attachment: fixed`, which iOS does not honour reliably.
- The semi-transparent overlay and per-viewport mobile/desktop image selection
  are preserved.

## Non-Changes

- Hash routing and route parsing (`#/overview`, `#/controls/water`, `#/more/tools`).
- Remembered active sections and defaults.
- Control widgets, their APIs, safety behaviour, and SSE live updates.
- The `<title>` element is now dynamic but the static fallback in the HTML
  stays as a no-JS default.

## Implementation Shape

Frontend-only. Files: `web/static/index.html`, `web/static/styles.css`,
`web/static/app.js`. The `navigation.js` module is unchanged.

## Tests and Verification

- Update `service/api/httpapi/server_test.go` structure assertions:
  - no `.section-switch` tabs; a `select` per destination instead;
  - `#buildInfo` footer removed; deployment line inside the System panel;
  - header no longer contains an eyebrow with the brand name.
- Update the jsdom interaction tests:
  - selecting a dropdown value navigates to the matching hash;
  - route restore and deep-links still select the right section;
  - the `<h1>` and `document.title` update with the active screen.
- Keep `navigation.test.js` unchanged.
- Run the project checks:

```bash
go test ./...
go vet ./...
go build ./...
npm run lint
npm test
git diff --check
```

Manual mobile verification should confirm the bar pins to the viewport bottom,
no content hides behind it, the background is stable across screens, and the
dropdown and deep-links behave as described.
