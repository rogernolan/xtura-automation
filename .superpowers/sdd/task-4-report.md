# Task 4 report

Status: implemented and browser tests passing; full ESLint gate remains blocked by three pre-existing errors.

## Changes

- `web/static/index.html`: adds fresh/grey detail paragraphs below the Overview percentage bars and a Tank levels panel on Water, using existing styling.
- `web/static/app.js`: adds one finite-number litre formatter and a focused `renderWaterLevels` helper. Overview and Water read the same optional overview litre fields. Water renders levels before the valve-state loading return.
- `web/static/app.test.js`: extends the DOM fixture and adds coverage for calibrated values, rounding, percentage/bar preservation, clearing absent/invalid values, zero litres, shared values, and missing overview/valve state.
- `web/static/navigation.test.js`: adds Water panel ID ownership assertions and checks the Overview detail IDs within its nested markup.
- This report is the only additional task artifact. Existing Task 1/3 reports and the untracked plan were preserved.

## RED evidence

Before changing production files, ran `rtk npm test -- --test-name-pattern='water|litre|volume'`: exit 1, 56 tests, 53 passing, 3 failing. Overview and Water expected `100.2 L remaining` but received an empty string; markup ownership failed because `waterLevelsPanel` did not exist.

On this Node/npm combination, the brief's appended filter did not restrict the suite. Also ran `rtk proxy node --test --test-name-pattern='water|litre|volume' web/static/app.test.js web/static/navigation.test.js` with the option before the files. After strengthening the clearing test with pre-existing detail text, all three new rendering tests failed for missing rendering/clearing behaviour: exit 1, 9 reported tests, 6 passing, 3 failing. The markup test was verified in the first full run.

## GREEN evidence

`rtk npm test`: exit 0, 56 tests passing, 0 failures.

Focused direct Node invocation: exit 0, 9 reported tests passing, 0 failures.

`rtk git diff --check`: exit 0.

An initial ownership assertion used the existing helper, which stops at the first closing section; Overview has nested sections. Changed that new assertion to inspect the Overview range before Heating. The complete markup/navigation suite then passed.

## Lint evidence and concerns

Initially ESLint was absent. Installed the existing locked dependencies using `rtk npm ci --ignore-scripts` after a sandbox retry; no package manifests or lockfiles changed.

`rtk lint eslint web/static/app.js web/static/navigation.js`: exit 1, three errors, zero warnings. Direct ESLint output identifies unused `overviewCurrentState`, unused `field` in water history, and an existing empty notification catch. Running `git show HEAD:web/static/app.js | node_modules/.bin/eslint --stdin --stdin-filename web/static/app.js` reproduced the same three errors on the untouched baseline. No new lint errors were introduced. Left those unrelated issues unchanged.

Dependency installation reports one high-severity audit finding in the existing dependency set; no dependency upgrade was performed.

No simulator or visual browser inspection was performed. Verification uses the repository's browser VM/DOM fixture tests and markup assertions; physical layout remains unverified.

## Self-review

Reviewed the four browser-file diffs against the brief. The implementation preserves percentage text and bars, last-seen handling, history rendering, valve controls, and navigation routes. One formatter uses `Number.isFinite` and one decimal place, rejects missing/null/non-finite/string values, and preserves numeric zero. Re-rendering clears prior litre detail when optional fields disappear. Water levels remain available independently of valve state. The overview event handler uses the existing shared render path, so both pages update from overview events.

Capacity fields are intentionally not displayed: the brief specifies remaining litres only. No backend, configuration, navigation implementation, or stylesheet changes were needed.

## Commit

Commit message: `feat: show calibrated water litres in UI`. Commit contains the four named browser files and this requested report. The commit identifier is returned in the task response.
