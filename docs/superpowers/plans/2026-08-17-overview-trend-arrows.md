# Overview Trend Arrow Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Align overview trend arrows to the rendered temperature text bounds across responsive font sizes.

**Architecture:** Keep the existing HTML wrappers and trend SVGs. Add a small DOM synchronization function in `web/static/app.js` that measures each value's text range relative to its group and writes CSS custom properties on the corresponding trend control. CSS consumes those properties for vertical placement; render, font readiness, and resize trigger synchronization.

**Tech Stack:** Browser DOM APIs, vanilla JavaScript, CSS custom properties, Node test runner.

## Global Constraints

- Preserve the existing `overview-trend-control` markup and accessibility label.
- Apply the same behavior to primary and sub-sensor temperature values.
- Do not use fixed pixel offsets to represent glyph bounds.
- Rebuild the simulator after web static assets change.

### Task 1: Add the regression test

**Files:**
- Modify: `web/static/app.test.js`

**Interfaces:**
- Consumes: `renderTemperature` and the existing fake DOM harness.
- Produces: a failing assertion that trend controls receive measured CSS custom properties.

- [ ] Add a test that gives the fake value/group elements `getBoundingClientRect()` results and a fake `Range`, renders a primary sensor, calls the alignment function exposed by `loadApp`, and asserts the control receives `--trend-top` and `--trend-bottom` values derived from the rectangles.
- [ ] Run `node --test web/static/app.test.js` and confirm the test fails because the alignment function/properties do not yet exist.

### Task 2: Implement measured alignment

**Files:**
- Modify: `web/static/app.js`
- Modify: `web/static/styles.css`

**Interfaces:**
- Consumes: `.overview-temperature-group`, `.overview-sensor-group`, `.overview-*-value`, and `.overview-trend-control` elements.
- Produces: `syncTrendControlAlignment()` that synchronizes all matching controls and is callable by tests.

- [ ] Implement range measurement relative to the group rectangle, with a no-op when a value, group, control, or text range is unavailable.
- [ ] Write `--trend-top` and `--trend-bottom` in pixels on each control, and invoke synchronization after `renderTemperature()` updates `innerHTML`.
- [ ] Register one resize listener and a `document.fonts.ready` continuation that call the same synchronization function.
- [ ] Change CSS from `top: 0; bottom: 0` to `top: var(--trend-top); bottom: var(--trend-bottom)` for both group selectors.
- [ ] Run the focused test and confirm it passes, then run `node --test web/static/app.test.js`.

### Task 3: Verify the integrated simulator

**Files:**
- No source changes expected.

- [ ] Run the complete available web test command from the repository and inspect failures.
- [ ] Run `./scripts/sim/run-sim.sh /Users/rog/Development/xtura-automation/garmin-ws-20260815T142323Z.ndjson` from this worktree.
- [ ] Confirm the simulator binary builds and starts on port 8091; stop it after the smoke check if it remains running.
- [ ] Run `git diff --check` and inspect `git status` to ensure only the intended implementation plus pre-existing handover changes are present.
