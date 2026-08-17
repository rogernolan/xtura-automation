# Overview UI Tweaks

Minor visual refinements to the Overview temperature card, chart, and trend display.

## 1. Temperature fields — drop "C", fixed width, right-justify

Remove the `C` suffix from all temperature displays (primary and sub-sensors). The context makes units clear.

Make temperature values fixed-width (`3.5ch`) and right-aligned with `font-variant-numeric: tabular-nums` so digits stay stable as values change. `8`/`9` are the widest digits in system fonts; `3.5ch` fits "88.8" comfortably.

**Files:** `app.js` (`formatTemperature`), `styles.css` (`.overview-temperature-value`, `.overview-sub-sensor-value`)

## 2. Chart axes — minimal gridlines

Add low-contrast axes to the temperature chart. No bounding box or tick marks — just gridlines and small labels.

**Y axis** (3 labels computed from data):
- `yTop = ceil(maxTemp / 5) * 5`
- `yBottom = floor(minTemp / 5) * 5`
- `yMid = (yTop + yBottom) / 2`
- Horizontal gridlines at these values, `rgba(0,0,0,0.12)`, 0.5px.
- Left-aligned labels, `10px` font, same low-contrast color.

**X axis** (2 nearest clock-hour boundaries within the data window):
- Find hour boundaries in `(startTime, endTime)` — at most 2.
- Vertical gridlines (same low-contrast treatment).
- `HH:00` labels below each line, `10px` font.

Adjust chart padding to `left: 36, top: 4, right: 4, bottom: 16` to accommodate labels.

**Files:** `app.js` (`drawTemperatureChart`)

## 3. Trend control — single `^ − v` glyph stack

Replace the single trend icon with a always-rendered vertical stack of three glyphs. The active state is colored; inactive stays low-contrast (same as chart axes).

| State | Glyph lit | Color |
|-------|-----------|-------|
| rising | `^` | `#e74c3c` (red) |
| falling | `v` | `#3b7fdd` (blue) |
| steady | `−` | `#27ae60` (green) |
| unknown | none | all low-contrast |

**Alignment:** The control is `height: 1em` inside the temperature line. The `^` top edge aligns with the temperature text top; the `v` bottom edge aligns with the baseline.

Applies everywhere trend is shown — primary temperature and sub-sensors.

**Files:** `app.js` (new `renderTrendControl`, `getTrendState`; remove `trendSymbol`), `styles.css` (new `.overview-trend-control` rules, remove `.overview-trend-icon`)

## Testing

- Update existing trend/temperature assertions in `app.test.js`.
- Add test for `getTrendState()`.
- Run `npm test && npm run lint` — all green.
