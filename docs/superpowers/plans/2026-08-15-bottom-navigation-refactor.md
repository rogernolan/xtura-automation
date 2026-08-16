# Bottom Navigation Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the four top tabs with the agreed bottom navigation and regroup existing controls without changing service APIs or action behaviour.

**Architecture:** A small pure routing module owns hash parsing and serialization. The embedded static UI becomes four primary panels; `app.js` maps the route to those panels while its existing state loading, SSE subscriptions, and controls remain intact.

**Tech Stack:** Plain HTML/CSS/JavaScript, Node built-in test runner, ESLint, Go `httptest` static-asset tests.

## Global Constraints

- Primary destinations are Overview, Controls, Location, and More, shown in a phone-first fixed bottom bar.
- Controls initially contains Heating, Water, and Lighting. Energy is added only in its own future work.
- More contains System and Tools. Execute the existing Pi-status plan before this one so `piStatusPanel` is available for System.
- Location owns tracking; Tools owns WebSocket recording. Preserve all existing REST, SSE, form, and action behaviour.
- Preserve current branding and background images, while increasing contrast of transparent controls over the background.
- `#/controls` defaults to Heating and `#/more` defaults to System. Unknown routes and sections use `#/overview`.
- No new third-party dependency and no navigation action may mutate service state.

---

### Task 1: Add tested hash routing

**Files:**
- Create: `web/static/navigation.js`
- Create: `web/static/navigation.test.js`
- Modify: `package.json`

**Interfaces:**
- Produces: `XturaNavigation.parse(hash) -> { screen, section }` and `XturaNavigation.toHash(route) -> string`.
- Produces: `npm test` running Node's test runner.

- [ ] **Step 1: Write the failing tests**

Create `web/static/navigation.test.js`:

```js
const assert = require("node:assert/strict");
const test = require("node:test");
const navigation = require("./navigation.js");

test("parses defaults and nested routes", () => {
  assert.deepEqual(navigation.parse("#/overview"), { screen: "overview", section: null });
  assert.deepEqual(navigation.parse("#/controls"), { screen: "controls", section: "heating" });
  assert.deepEqual(navigation.parse("#/controls/water"), { screen: "controls", section: "water" });
  assert.deepEqual(navigation.parse("#/more/tools"), { screen: "more", section: "tools" });
});

test("falls back for unavailable routes", () => {
  assert.deepEqual(navigation.parse("#/controls/energy"), { screen: "overview", section: null });
  assert.deepEqual(navigation.parse("#/unknown"), { screen: "overview", section: null });
});

test("writes canonical hashes", () => {
  assert.equal(navigation.toHash({ screen: "location", section: null }), "#/location");
  assert.equal(navigation.toHash({ screen: "controls", section: "lighting" }), "#/controls/lighting");
});
```

- [ ] **Step 2: Verify the test fails**

Run: `npm test`

Expected: FAIL because `navigation.js` is absent.

- [ ] **Step 3: Implement the route module and test script**

Create `web/static/navigation.js`:

```js
const defaults = Object.freeze({ controls: "heating", more: "system" });
const sections = Object.freeze({ controls: new Set(["heating", "water", "lighting"]), more: new Set(["system", "tools"]) });
const screens = new Set(["overview", "controls", "location", "more"]);
const fallback = () => ({ screen: "overview", section: null });

function parse(hash) {
  const parts = String(hash || "").replace(/^#\/?/, "").split("/").filter(Boolean);
  const [screen, section] = parts;
  if (!screens.has(screen) || parts.length > 2) return fallback();
  if (screen === "overview" || screen === "location") return section ? fallback() : { screen, section: null };
  const selected = section || defaults[screen];
  return sections[screen].has(selected) ? { screen, section: selected } : fallback();
}

function toHash(route) {
  const parsed = parse(route.section ? `#/${route.screen}/${route.section}` : `#/${route.screen}`);
  return parsed.section ? `#/${parsed.screen}/${parsed.section}` : `#/${parsed.screen}`;
}

const api = { parse, toHash };
globalThis.XturaNavigation = api;
if (typeof module !== "undefined") module.exports = api;
```

Replace `package.json` scripts with:

```json
"scripts": {
  "lint": "eslint web/static/app.js web/static/navigation.js",
  "test": "node --test web/static/navigation.test.js"
}
```

- [ ] **Step 4: Verify the route module**

Run: `npm test && npm run lint`

Expected: PASS, three tests and no lint errors.

- [ ] **Step 5: Commit**

Run: `git add package.json web/static/navigation.js web/static/navigation.test.js && git commit -m "web: add hash navigation helpers"`

### Task 2: Add the primary panels, nested panels, and bottom bar

**Files:**
- Modify: `web/static/index.html`
- Modify: `web/static/styles.css`
- Modify: `service/api/httpapi/server_test.go:855-895`

**Interfaces:**
- Produces primary panels: `overviewPanel`, `controlsPanel`, `locationPanel`, `morePanel`.
- Produces nested panels: `controlsHeatingPanel`, `controlsWaterPanel`, `controlsLightingPanel`, `moreSystemPanel`, `moreToolsPanel`.

- [ ] **Step 1: Write the failing served-index test**

Replace old tab expectations in `TestHandlerServesWebIndex` with assertions for:

```go
for _, want := range []string{
  `id="overviewNav" class="primary-nav-item" type="button" data-screen="overview" aria-controls="overviewPanel">Overview</button>`,
  `id="controlsNav" class="primary-nav-item" type="button" data-screen="controls" aria-controls="controlsPanel">Controls</button>`,
  `id="locationNav" class="primary-nav-item" type="button" data-screen="location" aria-controls="locationPanel">Location</button>`,
  `id="moreNav" class="primary-nav-item" type="button" data-screen="more" aria-controls="morePanel">More</button>`,
  `id="controlsHeatingPanel" class="section-panel"`, `id="controlsWaterPanel" class="section-panel" hidden`,
  `id="controlsLightingPanel" class="section-panel" hidden`, `id="moreSystemPanel" class="section-panel"`,
  `id="moreToolsPanel" class="section-panel" hidden`, `src="/static/navigation.js?v=dev"`,
} { if !strings.Contains(rr.Body.String(), want) { t.Fatalf("missing %q", want) } }
```

- [ ] **Step 2: Verify it fails**

Run: `go test ./service/api/httpapi -run TestHandlerServesWebIndex -count=1`

Expected: FAIL because the served page still has the four top tabs.

- [ ] **Step 3: Restructure the HTML without changing control IDs**

Load routing before the app:

```html
<script src="/static/navigation.js" defer></script>
<script src="/static/app.js" defer></script>
```

Replace the old tab navigation and wrappers with four primary sections. Create `overviewPanel` first, with the literal placeholder copy `Vehicle status dashboard is coming next.` and no controls. Wrap the current `heatingPanel`, `waterPanel`, and `lightingPanel` contents in `controlsHeatingPanel`, `controlsWaterPanel`, and `controlsLightingPanel` respectively, under `controlsPanel`; retain every descendant ID unchanged. Put the current `trackingPanel` in `locationPanel`. Put `piStatusPanel` from the prerequisite Pi-status change in `moreSystemPanel`, and put the current `recordingPanel` in `moreToolsPanel`, both under `morePanel`.

Add a four-button `<nav class="primary-nav" aria-label="Primary navigation">` after the primary panels. Its buttons must use exactly the IDs and `data-screen` / `aria-controls` values asserted in Step 1. Add a `section-switch` before the Controls nested panels with button values `heating`, `water`, and `lighting`; add one before the More nested panels with button values `system` and `tools`. Every switcher button uses `class="section-switch-item"`, `data-section-group`, `data-section`, and `aria-controls`.

- [ ] **Step 4: Replace top-tab CSS with bottom navigation CSS**

```css
.primary-panel, .section-panel { display: grid; gap: 12px; }
.section-switch { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 4px; padding: 4px; border-radius: 12px; background: rgba(24, 31, 40, .34); }
.section-switch-item, .primary-nav-item { min-height: 44px; border: 1px solid rgba(255, 255, 255, .38); border-radius: 9px; background: rgba(255, 255, 255, .78); color: var(--text); font-weight: 800; }
.section-switch-item.is-active, .primary-nav-item.is-active { background: var(--accent); border-color: var(--accent); color: #fff; }
.primary-nav { position: sticky; bottom: 10px; z-index: 2; display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 4px; padding: 6px; border: 1px solid rgba(255, 255, 255, .42); border-radius: 14px; background: rgba(231, 226, 216, .88); box-shadow: var(--shadow); backdrop-filter: blur(12px); }
```

Add `padding-bottom: 88px` to `.app-shell`.

- [ ] **Step 5: Verify and commit layout**

Run: `go test ./service/api/httpapi -run 'TestHandlerServesWebIndex|TestHandlerServesPortraitMobileBackgroundStyle' -count=1`

Expected: PASS.

Run: `git add web/static/index.html web/static/styles.css service/api/httpapi/server_test.go && git commit -m "web: group controls under bottom navigation"`

### Task 3: Apply route state to the existing UI

**Files:**
- Modify: `web/static/app.js:175-245,1062-1160`
- Modify: `service/api/httpapi/server_test.go:898-920`

**Interfaces:**
- Consumes: `XturaNavigation.parse` / `toHash`.
- Produces: `applyRoute(route)`, `navigate(screen, section)`, and `hashchange` restoration.

- [ ] **Step 1: Write a failing served-JS marker test**

Add `"applyRoute", "navigate", "XturaNavigation.parse", "hashchange"` to the required strings in `TestHandlerServesStaticJavaScript`.

- [ ] **Step 2: Verify it fails**

Run: `go test ./service/api/httpapi -run TestHandlerServesStaticJavaScript -count=1`

Expected: FAIL because route integration is absent.

- [ ] **Step 3: Implement route-driven visibility**

Replace `activeTab` with `route: { screen: "overview", section: null }`. Replace `setActiveTab` with `applyRoute`, which toggles all four `${screen}Nav` / `${screen}Panel` pairs and all nested section panels. Add:

```js
function navigate(screen, section = null) {
  const hash = XturaNavigation.toHash({ screen, section });
  if (window.location.hash === hash) applyRoute(XturaNavigation.parse(hash));
  else window.location.hash = hash;
}
```

Bind `[data-screen]` clicks to `navigate(button.dataset.screen)` and `[data-section-group]` clicks to `navigate(button.dataset.sectionGroup, button.dataset.section)`. Bind `hashchange` to `applyRoute(XturaNavigation.parse(window.location.hash))`; in `boot`, replace `setActiveTab("lighting")` with `applyRoute(XturaNavigation.parse(window.location.hash))`.

- [ ] **Step 4: Verify all checks and smoke test**

Run: `npm test && npm run lint && go test ./... && go vet ./... && go build ./... && git diff --check`

Expected: all commands exit 0.

Manually verify phone-sized `#/controls/water`, `#/more/tools`, refresh restoration, back/forward navigation, unchanged Heating/Water/Lighting controls, tracking in Location, recording in Tools, Pi status in System, and readable controls over the background image.

- [ ] **Step 5: Commit behaviour**

Run: `git add web/static/app.js service/api/httpapi/server_test.go && git commit -m "web: synchronize panels with hash routes"`

## Plan Self-Review

- Covers the four-destination bottom bar, grouped existing panels, route restoration, contrast, and preservation of existing live actions.
- Excludes Overview cards and Energy deliberately: their live data and controls have separate approved rollout work.
- Requires Pi-status implementation first because it is not present in the current checkout.
- Uses only `screen` and `section` route fields consistently, with implemented sections defined in one module.
