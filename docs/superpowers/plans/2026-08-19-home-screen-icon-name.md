# Xtura Home Screen Icon and Name Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make iOS Home Screen installations use the approved Xtura icon and the fixed installed name `Xtura`.

**Architecture:** Keep the existing embedded static web UI. Add one square PNG to `web/static/` and declare it from `web/static/index.html` with Apple touch-icon and standard browser icon metadata; use `application-name` and the existing `Xtura` title for the app name. No manifest, service worker, route, or Go embedding changes.

**Tech Stack:** HTML metadata, PNG asset, Node built-in test runner, Go embed/tests.

## Global Constraints

- The icon must remain local to `web/static/` and be embedded by the existing `//go:embed static/*` declaration.
- Preserve the plain embedded web UI; do not add a web manifest, service worker, or PWA framework.
- The source icon must be square and 1024 by 1024 pixels.
- The installed app name must be exactly `Xtura`.

---

### Task 1: Add regression coverage for Home Screen metadata

**Files:**
- Modify: `web/static/app.test.js`
- Test: `web/static/index.html` read directly from the test

**Interfaces:**
- Consumes: the static HTML file at `web/static/index.html`.
- Produces: a test that fails while the empty favicon and missing iOS metadata remain.

- [ ] **Step 1: Write the failing test**

Add a Node test near the top-level static UI tests:

```js
test("home screen metadata fixes the icon and installed app name", () => {
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");

  assert.match(html, /<meta name="application-name" content="Xtura">/);
  assert.match(html, /<link rel="apple-touch-icon" href="\/static\/xtura-home-screen\.png">/);
  assert.match(html, /<link rel="icon" type="image\/png" href="\/static\/xtura-home-screen\.png">/);
  assert.doesNotMatch(html, /<link rel="icon" href="data:,">/);
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
rtk npm test -- web/static/app.test.js
```

Expected: FAIL because `index.html` still contains the empty `data:,` favicon and does not contain the required metadata.

### Task 2: Add the approved icon asset and HTML metadata

**Files:**
- Create: `web/static/xtura-home-screen.png`
- Modify: `web/static/index.html` in the `<head>` metadata block

**Interfaces:**
- Consumes: the approved source artwork at `/Users/rog/Development/JonesControl/AppIcon.icon/Assets/joes.png`.
- Produces: `/static/xtura-home-screen.png`, referenced by both icon links in the HTML.

- [ ] **Step 1: Create the square 1024px PNG**

Use the approved artwork as the source, resize/crop it to a square 1024 by 1024 PNG, and save it as `web/static/xtura-home-screen.png`. Verify with:

```bash
rtk proxy sips -g pixelWidth -g pixelHeight web/static/xtura-home-screen.png
```

Expected: both dimensions are `1024`.

- [ ] **Step 2: Replace the empty favicon metadata**

In `web/static/index.html`, replace:

```html
<link rel="icon" href="data:,">
```

with:

```html
<meta name="application-name" content="Xtura">
<link rel="apple-touch-icon" href="/static/xtura-home-screen.png">
<link rel="icon" type="image/png" href="/static/xtura-home-screen.png">
```

- [ ] **Step 3: Run the focused regression test**

Run:

```bash
rtk npm test -- web/static/app.test.js
```

Expected: PASS, including `home screen metadata fixes the icon and installed app name`.

### Task 3: Verify embedding and complete the change

**Files:**
- Verify: `web/static/index.html`
- Verify: `web/static/xtura-home-screen.png`
- Verify: `web/web.go`

**Interfaces:**
- Consumes: the metadata and asset from Tasks 1–2.
- Produces: a verified Go build/runtime asset set with no unrelated changes.

- [ ] **Step 1: Run the full Node suite**

Run:

```bash
rtk npm test
```

Expected: all Node tests pass.

- [ ] **Step 2: Run the Go suite**

Run:

```bash
rtk go test ./...
```

Expected: all Go packages pass, confirming the new embedded PNG is accepted by `web/web.go`.

- [ ] **Step 3: Review the final diff**

Run:

```bash
rtk git diff --check
rtk git status --short
```

Expected: only the intended metadata, test, icon asset, and plan files are changed; no whitespace errors are reported.

- [ ] **Step 4: Commit the implementation**

```bash
rtk git add web/static/index.html web/static/app.test.js web/static/xtura-home-screen.png docs/superpowers/plans/2026-08-19-home-screen-icon-name.md
rtk git commit -m "fix: set Xtura home screen icon and name"
```
