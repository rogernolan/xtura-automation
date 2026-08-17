# Burger Page Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the mobile bottom tab bar and section pickers with eight canonical pages reachable through an accessible burger menu, while preserving all existing controls and live updates.

**Architecture:** Keep the existing static single-page app and shared SSE/state model. Replace the nested `screen/section` route with a single page route, move each existing domain panel into its own page container, and use a header drawer for navigation. Overview cards continue to navigate through the hash without issuing control requests.

**Tech Stack:** Static HTML, CSS, browser JavaScript, Node's built-in test runner, Go backend and existing `npm`/`go` checks.

## Global Constraints

- Use the existing isolated worktree: `/Users/rog/Development/xtura-automation/.worktrees/codex-overview-dashboard`.
- Canonical routes are exactly `#/overview`, `#/heating`, `#/water`, `#/lighting`, `#/location`, `#/system`, `#/tools`, and `#/settings`.
- Legacy `#/controls/...` and `#/more/...` routes fall back to Overview.
- No backend or API changes are required.
- Preserve existing element IDs, live rendering, SSE behavior, action safety, and form behavior where practical.
- Overview is read-only; its cards only navigate.

---

### Task 1: Replace nested navigation with canonical page routes

**Files:**
- Modify: `/Users/rog/Development/xtura-automation/.worktrees/codex-overview-dashboard/web/static/navigation.js`
- Test: `/Users/rog/Development/xtura-automation/.worktrees/codex-overview-dashboard/web/static/navigation.test.js`

**Interfaces:**
- Produces `XturaNavigation.pages`, `parse(hash) -> { page: string }`, and `toHash({ page }) -> string`.
- `parse` returns `{ page: "overview" }` for empty, unknown, nested, legacy, or incomplete routes.

- [ ] **Step 1: Write failing route tests**

Add tests for every canonical page plus invalid and legacy fallback:

```js
test("parses every canonical page", () => {
  for (const page of navigation.pages) {
    assert.deepEqual(navigation.parse(`#/${page}`), { page });
  }
});

test("falls back for legacy, nested, and unknown routes", () => {
  for (const hash of ["", "#", "#/controls/heating", "#/more/tools", "#/unknown", "#/heating/extra"]) {
    assert.deepEqual(navigation.parse(hash), { page: "overview" });
  }
});

test("writes canonical page hashes", () => {
  assert.equal(navigation.toHash({ page: "heating" }), "#/heating");
  assert.equal(navigation.toHash({ page: "overview" }), "#/overview");
  assert.equal(navigation.toHash({ page: "not-a-page" }), "#/overview");
});
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run `node --test web/static/navigation.test.js` from the worktree root. Expected: FAIL because the current parser returns `screen`/`section` and does not expose the eight page routes.

- [ ] **Step 3: Implement the minimal route model**

Replace the nested constants and parser with:

```js
const pages = Object.freeze(["overview", "heating", "water", "lighting", "location", "system", "tools", "settings"]);
const pageSet = new Set(pages);
const fallback = () => ({ page: "overview" });

function parse(hash) {
  const parts = String(hash || "").replace(/^#\/?/, "").split("/").filter(Boolean);
  if (parts.length !== 1 || !pageSet.has(parts[0])) return fallback();
  return { page: parts[0] };
}

function toHash(route) {
  const parsed = parse(route && `#/${route.page}`);
  return `#/${parsed.page}`;
}

const navigationApi = { pages, parse, toHash };
```

- [ ] **Step 4: Run the focused test and verify it passes**

Run `node --test web/static/navigation.test.js`. Expected: PASS for all route tests; update only the old navigation assertions that still reference `screen`/`section`.

- [ ] **Step 5: Commit the route contract**

Run `rtk git add web/static/navigation.js web/static/navigation.test.js && rtk git commit -m "refactor: use canonical page routes"`.

### Task 2: Restructure the HTML into pages and add the burger drawer

**Files:**
- Modify: `/Users/rog/Development/xtura-automation/.worktrees/codex-overview-dashboard/web/static/index.html`
- Modify: `/Users/rog/Development/xtura-automation/.worktrees/codex-overview-dashboard/web/static/navigation.test.js`

**Interfaces:**
- Produces page containers with IDs `overviewPanel`, `heatingPanel`, `waterPanel`, `lightingPanel`, `locationPanel`, `systemPanel`, `toolsPanel`, and `settingsPanel`.
- Produces `menuButton`, `navigationDrawer`, `navigationBackdrop`, and eight `data-page` links.

- [ ] **Step 1: Write failing markup assertions**

Extend the markup test with:

```js
for (const page of navigation.pages) {
  assert.match(html, new RegExp(`id="${page}Panel"`));
  assert.match(html, new RegExp(`data-page="${page}"`));
}
assert.match(html, /id="menuButton"[^>]*aria-expanded="false"/);
assert.match(html, /id="navigationDrawer"/);
assert.doesNotMatch(html, /data-section-group=/);
assert.doesNotMatch(html, /class="primary-nav/);
```

- [ ] **Step 2: Run the markup test and verify it fails**

Run `node --test web/static/navigation.test.js`. Expected: FAIL because the current HTML has grouped panels, section selects, and the bottom primary navigation.

- [ ] **Step 3: Move existing panel markup into page containers**

Keep all control element IDs and inner markup, but rename the ownership containers as follows:

```html
<section id="overviewPanel" class="page-panel overview-layout">...</section>
<section id="heatingPanel" class="page-panel">...</section>
<section id="waterPanel" class="page-panel">...</section>
<section id="lightingPanel" class="page-panel">...</section>
<section id="locationPanel" class="page-panel">...</section>
<section id="systemPanel" class="page-panel">...</section>
<section id="toolsPanel" class="page-panel">...</section>
<section id="settingsPanel" class="page-panel">...</section>
```

Remove both section `<select>` elements, `.section-panel` wrappers, and the bottom `.primary-nav`. Change Overview routes to `#/heating`, `#/water`, `#/system`, and `#/settings`; change both temperature-card routes to `#/heating`.

Add this shared header/drawer markup:

```html
<button id="menuButton" class="menu-button" type="button" aria-label="Open navigation" aria-controls="navigationDrawer" aria-expanded="false">☰</button>
<div id="navigationBackdrop" class="navigation-backdrop" hidden></div>
<aside id="navigationDrawer" class="navigation-drawer" aria-label="Site navigation" aria-hidden="true" hidden>
  <div class="navigation-drawer-heading"><h2>Pages</h2><button id="closeMenuButton" type="button" aria-label="Close navigation">×</button></div>
  <nav>
    <a data-page="overview" href="#/overview">Overview</a>
    <a data-page="heating" href="#/heating">Heating</a>
    <a data-page="water" href="#/water">Water</a>
    <a data-page="lighting" href="#/lighting">Lighting</a>
    <a data-page="location" href="#/location">Location</a>
    <a data-page="system" href="#/system">System</a>
    <a data-page="tools" href="#/tools">Tools</a>
    <a data-page="settings" href="#/settings">Settings</a>
  </nav>
</aside>
```

- [ ] **Step 4: Run markup and existing static tests**

Run `node --test web/static/navigation.test.js web/static/app.test.js`. Expected: markup assertions pass; app tests may fail until Task 3 updates route binding and page IDs.

- [ ] **Step 5: Commit the page markup**

Run `rtk git add web/static/index.html web/static/navigation.test.js && rtk git commit -m "feat: add burger menu page structure"`.

### Task 3: Implement page visibility, drawer behavior, and deep links

**Files:**
- Modify: `/Users/rog/Development/xtura-automation/.worktrees/codex-overview-dashboard/web/static/app.js`
- Modify: `/Users/rog/Development/xtura-automation/.worktrees/codex-overview-dashboard/web/static/app.test.js`

**Interfaces:**
- `state.route` becomes `{ page: "overview" }`.
- `applyRoute({ page })` updates the title, `hidden` page panels, active drawer link, and canonical route fallback.
- `navigate(page)` writes `XturaNavigation.toHash({ page })`.
- `openNavigation()` and `closeNavigation({ restoreFocus = true })` control the drawer.

- [ ] **Step 1: Add failing route and drawer interaction tests**

Update the test DOM stubs to include the eight panels, menu button, close button, backdrop, and drawer links, then add assertions shaped like:

```js
test("page links navigate and close the drawer", () => {
  const { bindActions, window, elements } = loadApp({ hash: "#/overview" });
  bindActions();
  elements.menuButton.dispatchEvent({ type: "click" });
  assert.equal(elements.navigationDrawer.hidden, false);
  elements.heatingLink.dispatchEvent({ type: "click", preventDefault() {} });
  assert.equal(window.location.hash, "#/heating");
  assert.equal(elements.navigationDrawer.hidden, true);
});

test("overview temperature cards deep link to heating", () => {
  const { bindActions, window, elements } = loadApp({ hash: "#/overview" });
  bindActions();
  elements.aldeCard.dispatchEvent({ type: "click" });
  assert.equal(window.location.hash, "#/heating");
});
```

- [ ] **Step 2: Run focused app tests and verify they fail**

Run `node --test web/static/app.test.js`. Expected: FAIL because `app.js` currently expects `screen`/`section`, bottom-nav buttons, and section selectors.

- [ ] **Step 3: Implement route application and drawer actions**

Use a single page map and preserve the existing render/action functions:

```js
const pageTitles = { overview: "Overview", heating: "Heating", water: "Water", lighting: "Lighting", location: "Location", system: "System", tools: "Tools", settings: "Settings" };
const pageIds = XturaNavigation.pages.map((page) => `${page}Panel`);

function applyRoute(route) {
  state.route = route;
  const page = route.page;
  byId("pageTitle").textContent = pageTitles[page];
  document.title = pageTitles[page];
  XturaNavigation.pages.forEach((name) => {
    byId(`${name}Panel`).hidden = name !== page;
    const link = document.querySelector(`[data-page="${name}"]`);
    if (link) {
      if (name === page) link.setAttribute("aria-current", "page");
      else link.removeAttribute("aria-current");
    }
  });
}

function navigate(page) {
  const hash = XturaNavigation.toHash({ page });
  if (window.location.hash === hash) applyRoute(XturaNavigation.parse(hash));
  else window.location.hash = hash;
}
```

Bind drawer links with `preventDefault()`, `navigate(link.dataset.page)`, and `closeNavigation()`. Bind the burger, close button, backdrop, and document Escape handler. Move focus to the first drawer link when opening and back to `menuButton` when closing. Remove all section-selector and `data-screen` binding code. Change Overview card navigation to `navigate(route.page)`.

- [ ] **Step 4: Update test stubs and run the focused tests**

Run `node --test web/static/app.test.js web/static/navigation.test.js`. Expected: PASS, including drawer close, route changes, page visibility, and deep-link assertions. Confirm no test relies on a removed selector.

- [ ] **Step 5: Commit the behavior**

Run `rtk git add web/static/app.js web/static/app.test.js && rtk git commit -m "feat: navigate pages from burger menu"`.

### Task 4: Style the page shell and drawer for mobile

**Files:**
- Modify: `/Users/rog/Development/xtura-automation/.worktrees/codex-overview-dashboard/web/static/styles.css`

**Interfaces:**
- Provides visible, keyboard-usable styles for `.menu-button`, `.navigation-drawer`, `.navigation-backdrop`, active page links, and the page panels.

- [ ] **Step 1: Add the drawer and page-shell CSS**

Add styles using existing color variables and responsive conventions:

```css
.menu-button { min-width: 44px; min-height: 44px; }
.navigation-drawer { position: fixed; inset: 0 auto 0 0; z-index: 20; width: min(84vw, 320px); padding: 1rem; background: rgba(31, 35, 34, .98); transform: translateX(0); }
.navigation-drawer[hidden], .navigation-backdrop[hidden] { display: none; }
.navigation-backdrop { position: fixed; inset: 0; z-index: 19; background: rgba(0, 0, 0, .45); }
.navigation-drawer a { display: block; min-height: 44px; padding: .75rem; color: var(--text); }
.navigation-drawer a[aria-current="page"] { background: rgba(255, 255, 255, .16); }
```

Keep the existing background and card treatments, remove bottom-nav spacing/rules, and ensure content does not sit underneath the header on narrow screens.

- [ ] **Step 2: Run lint and inspect the rendered static page**

Run `npm test` and `npm run lint` (or the exact scripts listed by `rtk json package.json`). Expected: no lint/test failures. Open the local UI if available and check the drawer at phone width, desktop width, keyboard focus, and active-link styling.

- [ ] **Step 3: Commit the responsive styling**

Run `rtk git add web/static/styles.css && rtk git commit -m "style: make page navigation mobile friendly"`.

### Task 5: Run full verification and review the diff

**Files:**
- Modify: none unless verification finds a regression.

- [ ] **Step 1: Run frontend tests and lint**

Run `npm test` and the project's configured lint command. Expected: all frontend tests pass with zero failures.

- [ ] **Step 2: Run Go checks**

Run `go test ./...`, `go vet ./...`, and `go build ./...`. Expected: all commands exit successfully.

- [ ] **Step 3: Check the final diff**

Run `rtk git diff --check`, `rtk git status --short --branch`, and `rtk git log --oneline -6`. Expected: no whitespace errors, only intended frontend/spec/plan changes, and the worktree remains on `codex/overview-dashboard-layout`.

- [ ] **Step 4: Perform manual route verification**

Verify direct navigation and refresh for all eight hashes, browser back/forward, invalid-route fallback, drawer close via link/close button/backdrop/Escape, active `aria-current`, and Overview card targets. Confirm each existing control still operates on its own page and the EventSource remains connected across page changes.

- [ ] **Step 5: Commit any final verification fixes**

If fixes are required, run `rtk git add <specific-files> && rtk git commit -m "fix: complete burger page navigation verification"`; otherwise leave the existing task commits intact.
