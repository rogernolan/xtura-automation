# Water tank litre calibration design

## Goal

Convert Garmin fresh- and grey-water percentages into usable litres remaining using measured, non-linear tank calibration points. Show the converted value on both the Overview page and the Water page while preserving the existing percentage display and water-history behaviour.

The first calibration is for the fresh tank. Its measured usable capacity is 138.9 L; the advertised 150 L is not used as the live capacity because the measured full point is the useful operational value.

## Configuration

Add optional calibration points below `water_history` in YAML:

```yaml
water_history:
  threshold_percent: 5
  prediction_threshold_percent: 10
  settling_period: 10m
  grouping_window: 1h
  calibration:
    fresh:
      - percent: 0
        litres: 0
      - percent: 17
        litres: 15.5
      # ... measured points ...
      - percent: 100
        litres: 138.9
    # Add grey points after a separate measured grey-tank run.
    # grey:
    #   - percent: 0
    #     litres: 0
```

The maximum litres value at the 100% point is the usable capacity reported by the API. There is no separate capacity field that could disagree with the curve.

Each configured curve must either be absent or contain at least two points. A present curve must:

- start at 0% and end at 100%;
- use percentages strictly increasing from 0 to 100;
- use litres non-negative and strictly increasing;
- contain finite numeric values.

Invalid calibration configuration fails normal config validation with a field-specific error. An absent fresh or grey curve is valid and means that tank continues to expose percentage only.

## Calibration engine

Create a small pure calibration component that owns point validation, usable-capacity derivation, and conversion. It performs piecewise-linear interpolation between adjacent configured points and clamps percentages to the curve endpoints. It does not smooth or fit a new curve at runtime: the measured points remain inspectable and reproducible in configuration.

The component is independent of Garmin, HTTP, and browser code so its boundary cases can be unit-tested directly. Future measured fills update the YAML point list; no code change is needed.

## API model and runtime flow

Extend the overview document with optional fields:

- `fresh_water_litres`
- `fresh_water_capacity_litres`
- `grey_water_litres`
- `grey_water_capacity_litres`

The fields are omitted when the corresponding calibration is absent or the corresponding percentage is unavailable. Existing percentage fields and status semantics remain unchanged.

`runtime.overviewDocument` snapshots the water calibration configuration under the existing app mutex, converts the current telemetry percentages through the pure calibration component, and populates the optional overview fields. The water percentage remains the Garmin source value; the litre value is explicitly derived from it.

## Browser UI

The Overview fresh- and grey-water cards retain their percentage as the primary value and add a detail line such as `100.0 L remaining` when a calibration is available. An uncalibrated tank keeps its current percentage-only presentation.

The Water page gains a tank-level detail area showing the same current percentage and litre values for each calibrated tank. This area uses the overview state already loaded by the browser, so both pages share one API representation and cannot develop separate conversion logic. The existing grey-valve controls, history chart, and event summaries remain unchanged.

## Testing

Add coverage for:

- valid and invalid curve configuration;
- exact-point lookup, interpolation, endpoint clamping, and usable-capacity derivation;
- overview JSON with fresh calibration, with grey calibration, and with no calibration;
- Overview rendering of calibrated and uncalibrated tanks;
- Water-page rendering of the same values;
- the supplied fresh calibration points, including the non-linear result that 100.2 L corresponds to 81% and full corresponds to 138.9 L.

Run the existing Go suite and browser test suite, plus lint for the changed JavaScript. No production or staging configuration is changed as part of the code change; deployment can add the fresh curve to the active Pi config after the implementation is verified.

