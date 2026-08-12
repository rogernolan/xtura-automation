# Static Cache and Tab Layout Design

## Problem

After a deployment, Safari can receive updated HTML that contains the Settings
tab while retaining a cached older `/static/app.js`. The older script has no
Settings click handler, so the tab button is visible but does not switch
panels.

## Design

Set conservative revalidation headers for embedded static assets served by
the HTTP API. Browsers must validate `/static/app.js`, `/static/styles.css`,
and image assets against the service after a deployment before reuse. This
keeps stable asset URLs while preventing an HTML/script version mismatch.

Keep the existing four-column tab grid and change the tab labels to `Light`,
`Water`, `Heat`, and `Settings`. Use compact, non-wrapping tab text so all
four buttons remain on one row at iPhone widths. Do not add SF Symbols: this
plain web UI does not have a bundled Symbol font or icon system, and shorter
labels solve the size requirement without a new dependency.

## Verification

Add HTTP tests for static revalidation headers and static UI assertions for
the compact labels and four-column layout. Run ESLint, `go test ./...`, and
`git diff --check`.
