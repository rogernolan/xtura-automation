# Task 2 Report: Restructure the HTML into pages and add the burger drawer

## Implementation summary

Restructured `web/static/index.html` from grouped `controls` and `more` sections into eight page containers: `overviewPanel`, `heatingPanel`, `waterPanel`, `lightingPanel`, `locationPanel`, `systemPanel`, `toolsPanel`, and `settingsPanel`. Added the shared burger navigation controls (`menuButton`, `navigationBackdrop`, `navigationDrawer`, `closeMenuButton`) and the eight `data-page` links exactly as specified. Removed the section `<select>` controls, removed `.section-panel` ownership wrappers, removed the bottom `.primary-nav`, and updated overview card routes to `#/heating`, `#/water`, `#/system`, and `#/settings`.

Updated `web/static/navigation.test.js` to assert the new page-panel IDs, drawer markup, page links, route targets, and removal of legacy grouped-navigation markup.

## Files changed

- `web/static/index.html`
- `web/static/navigation.test.js`

## RED/GREEN TDD evidence

### RED

Command:

```bash
node --test web/static/navigation.test.js
```

Result:

- Failed in `overview markup uses page panels and drawer navigation`
- First failing assertion was missing `data-page="overview"` in the old HTML
- The failure matched the expected pre-change state: legacy grouped panels and no drawer navigation

### GREEN

Command:

```bash
node --test web/static/navigation.test.js
```

Result:

- 5 tests passed
- 0 tests failed

## Test commands and results

### Focused Task 2 test

```bash
node --test web/static/navigation.test.js
```

- Pass: 5
- Fail: 0

### Relevant static regression check

```bash
node --test web/static/navigation.test.js web/static/app.test.js
```

- Pass: 11
- Fail: 2
- Expected failures:
  - `changing the controls dropdown navigates to the selected section`
  - `changing the more dropdown navigates to the selected section`
- Both failures are due to Task 2 intentionally removing the grouped dropdown navigation while `web/static/app.js` and its tests are still on the pre-Task-3 routing model

## Self-review

- Verified only the two requested files were changed for implementation.
- Confirmed all required page-panel IDs and drawer link `data-page` attributes exist.
- Confirmed the legacy `data-section-group` markup and bottom `.primary-nav` were removed.
- Confirmed existing control element IDs and inner control markup were preserved while moving them into the new page containers.
- Confirmed no changes were made to `web/static/app.js` or `web/static/styles.css`.

## Concerns

- `web/static/app.test.js` still contains assertions for the removed controls/more dropdown model, so the static suite is not fully green until Task 3 updates route binding and test expectations.

## Review fix evidence

Addressed the Task 2 review findings in `web/static/navigation.test.js` by tightening the markup assertions to:

- pair each drawer link `data-page` with its exact canonical `href`
- assert `navigationBackdrop` exists
- assert the exact Overview card ID to `data-overview-route` mappings:
  - `aldeCard` -> `#/heating`
  - `freshWaterCard` -> `#/water`
  - `greyWaterCard` -> `#/water`
  - `batterySocCard` -> `#/system`
  - `batteryCurrentCard` -> `#/system`
  - `gasCard` -> `#/settings`

Command:

```bash
node --test web/static/navigation.test.js
```

Output:

```text
✔ exposes the canonical page list (0.73325ms)
✔ parses every canonical page (0.182833ms)
✔ falls back for legacy, nested, and unknown routes (0.099167ms)
✔ writes canonical page hashes (0.0915ms)
✔ overview markup uses page panels and drawer navigation (1.18325ms)
ℹ tests 5
ℹ suites 0
ℹ pass 5
ℹ fail 0
ℹ cancelled 0
ℹ skipped 0
ℹ todo 0
ℹ duration_ms 60.585875
```
