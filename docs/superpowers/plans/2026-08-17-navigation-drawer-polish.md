# Navigation Drawer Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the mobile navigation drawer match the existing Xtura visual system and animate in and out without compromising accessibility.

**Architecture:** Keep the existing drawer/backdrop DOM and focus-management logic. Add a transient closing state in `app.js`, style the drawer and backdrop with existing design tokens and CSS transitions, and finalize hiding after `transitionend` with a fallback timer.

**Tech Stack:** Vanilla JavaScript, CSS, Node test suite, simulator harness.

## Global Constraints

- Reuse existing `--surface`, `--border`, `--text`, `--muted`, `--accent`, and `--shadow` tokens.
- Preserve keyboard, focus-trap, backdrop-click, and Escape behavior.
- Disable motion under `prefers-reduced-motion: reduce`.

---

### Task 1: Add drawer transition state and styling

**Files:**
- Modify: `web/static/app.js` (`openNavigation` and `closeNavigation`)
- Modify: `web/static/styles.css` (navigation drawer and backdrop rules)

**Interfaces:**
- `openNavigation()` makes the drawer visible and adds the open state.
- `closeNavigation()` removes the open state and hides the drawer after the transition, while immediately hiding when reduced motion is enabled.

- [ ] **Step 1: Add the visible/open state to the existing drawer lifecycle.**

  Set `hidden = false` before opening, add `is-open` to the drawer and backdrop, and on close remove `is-open`, then hide both after `transitionend` or a short fallback timeout. Keep the existing ARIA and focus restoration behavior.

- [ ] **Step 2: Restyle the drawer with the existing app tokens.**

  Replace the opaque dark surface and white-only link treatment with the app's translucent surface, border, radius, text colors, accent current-page state, and existing shadow. Add opacity and translate transitions for both layers.

- [ ] **Step 3: Add reduced-motion CSS.**

  Under `@media (prefers-reduced-motion: reduce)`, set drawer and backdrop transition durations to `0ms` so the same lifecycle remains accessible without animation.

- [ ] **Step 4: Run the focused UI tests and syntax checks.**

  Run `npm test` and `npm run lint`; expect all tests and lint checks to pass.

- [ ] **Step 5: Verify the live simulator.**

  Run `./scripts/sim/run-sim.sh /Users/rog/Development/xtura-automation/garmin-ws-20260815T142323Z.ndjson`, open `http://localhost:8091/#/overview`, and verify the drawer visually matches the app, animates in/out, and still closes via backdrop and Escape.

- [ ] **Step 6: Commit the implementation.**

  Run `git add web/static/app.js web/static/styles.css && git commit -m "fix: polish navigation drawer styling"`.
