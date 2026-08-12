# Jones Home Screen Icon Design

## Goal

Give the Xtura mobile web app the approved JonesControl-style icon when it is added to an iPhone or iPad Home Screen.

## Approach

Add one 1024 by 1024 PNG to `web/static/`. It uses the approved preview: the Jones motorhome artwork centred on a warm muted-gold gradient with a subtle shadow and glass-like finish. The web server already embeds every `web/static/*` file, so no Go server change is needed.

Replace the empty data-URL favicon in `web/static/index.html` with icon metadata that points to the static PNG. Include `apple-touch-icon` for iOS Home Screen installation and a normal `icon` relationship for browsers. iOS applies the rounded-square mask itself, so the source PNG remains square and full-bleed.

## Scope and Constraints

- Reuse the approved preview generated from `../JonesControl/AppIcon.icon/Assets/joes.png`.
- Keep the asset local to this repository under `web/static/`; do not serve it from JonesControl.
- Preserve the existing plain embedded web UI; do not add a web manifest, service worker, or PWA framework.
- Do not change HTTP routes or Go embedding code.

## Validation

- Confirm the new image is a square PNG with 1024-pixel dimensions.
- Confirm the HTML contains the Apple touch-icon and standard icon links, both referencing the embedded static asset.
- Run `go test ./...` to confirm the embedded static assets still build and all Go tests pass.
