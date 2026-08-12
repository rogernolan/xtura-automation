# Static Cache and Tab Layout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent HTML and JavaScript version mismatches after deploys and keep all four tabs compact on one row.

**Architecture:** The static handler sends revalidation cache control for each embedded asset, so browsers fetch a fresh response after deployment. The existing four-column CSS grid stays unchanged while tab labels are shortened.

**Tech Stack:** Go `net/http` tests, embedded HTML, CSS, ESLint.

## Global Constraints

- Do not add an icon library or SF Symbol dependency.
- Keep all four tab controls in the existing four-column grid.
- Every `/static/` response must revalidate after deployment rather than remain immutable for a year.

---

### Task 1: Static cache revalidation and compact tab labels

**Files:**
- Modify: `service/api/httpapi/static.go`
- Modify: `service/api/httpapi/server_test.go`
- Modify: `web/static/index.html`

- [ ] **Step 1: Write the failing HTTP and static assertions**

```go
if cacheControl := rr.Header().Get("Cache-Control"); cacheControl != "no-cache" {
    t.Fatalf("unexpected cache control %q", cacheControl)
}
```

Assert the index includes `Light`, `Water`, `Heat`, and `Settings` tab labels.

- [ ] **Step 2: Run the focused test and observe failure**

Run: `rtk test go test ./service/api/httpapi -run 'TestHandlerServesStaticJavaScript|TestWebIndex' -count=1`

Expected: FAIL because the static handler still sets `public, max-age=31536000, immutable` and labels are still long.

- [ ] **Step 3: Implement the minimal fix**

```go
w.Header().Set("Cache-Control", "no-cache")
```

Change only the visible tab text to `Light`, `Water`, `Heat`, and `Settings`; retain IDs, controls, and the four-column grid.

- [ ] **Step 4: Verify and commit**

Run: `rtk lint eslint web/static/app.js && rtk test go test ./... && rtk git diff --check`

Expected: all commands exit 0.

Run: `rtk git add service/api/httpapi/static.go service/api/httpapi/server_test.go web/static/index.html && rtk git commit -m "fix: revalidate static assets after deploy"`
