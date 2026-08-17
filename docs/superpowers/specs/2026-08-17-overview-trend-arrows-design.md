# Overview Trend Arrow Alignment

## Goal

Align the stacked up/flat/down trend arrows with the visible temperature glyphs: the up arrow reaches the visible text top and the down arrow reaches the visible text baseline. The alignment must follow the rendered font metrics and remain correct as the viewport or font size changes.

## Design

Keep the existing `overview-temperature-group` and `overview-sensor-group` wrappers and the `overview-trend-control` markup. After temperature markup is rendered, measure the value text in the browser using a `Range` over the value element's text. Convert the measured text rectangle into coordinates relative to its group wrapper, then write CSS custom properties on the trend control for its top and bottom offsets. The control remains absolutely positioned horizontally beside the value, but its vertical extent is driven by the measured text bounds rather than the line box.

Run the synchronization after each temperature render, when document fonts become ready, and on viewport resize. The synchronization should tolerate missing elements and avoid changing unrelated overview rendering. The same logic applies to the primary value and sub-sensor values.

## CSS contract

The trend control uses the measured `--trend-top` and `--trend-bottom` values for its vertical position. Existing active/inactive colors, SVG sizing, horizontal gaps, and accessibility markup remain unchanged.

## Testing and verification

Add focused tests for the alignment hook and its CSS custom-property output using the existing app test harness. Run the complete web test command, inspect the resulting diff, and rebuild the simulator with `scripts/sim/run-sim.sh` using the handover recording. Confirm the rebuilt simulator starts successfully and serves the updated static assets.

## Alternatives rejected

- Flex stretching aligns to the flex line box, not visible glyph bounds.
- Absolute `top: 0; bottom: 0` aligns to the wrapper/line box and was visually worse.
- Fixed CSS offsets or inherited SVG font sizing do not track responsive font metrics.
- Canvas metrics duplicate layout assumptions and are less direct than measuring the rendered DOM text.
