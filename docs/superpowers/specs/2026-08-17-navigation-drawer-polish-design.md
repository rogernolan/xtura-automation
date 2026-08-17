# Navigation Drawer Polish Design

## Goal

Make the mobile navigation drawer visually consistent with the existing Xtura
interface and give it a quick, unobtrusive entrance and exit animation.

## Design

- Reuse the app's existing surface, border, text, muted, accent, and shadow
  tokens instead of introducing a separate dark navigation theme.
- Keep the drawer fixed over the app with the existing backdrop and focus
  management, but use the same rounded panel treatment as the rest of the UI.
- Animate the drawer and backdrop with opacity and a small horizontal offset.
- Keep the drawer mounted while its closing animation runs, then apply the
  existing hidden state after the transition completes.
- Disable motion under `prefers-reduced-motion: reduce`.

## Verification

- Preserve current keyboard, focus-trap, backdrop-click, and Escape behavior.
- Run the browser unit tests, lint, and the simulator harness smoke check.
