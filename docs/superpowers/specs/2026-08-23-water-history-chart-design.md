# Water history and usage chart

## Goal

Track at least seven days of fresh-water and grey-water percentage history,
identify sustained fresh fills and grey empties, report percentage usage since
the latest event for each tank, and show both histories on the Water page.
Litre-based usage is intentionally deferred until the tank calibration is
available.

## Product behavior

The Water page will show one seven-day chart with a fixed 100% to 0% vertical
scale and time on the horizontal axis. Fresh water is a blue line; grey water
is a dark-grey line. Event markers are shown on the chart. A fresh fill and a
grey empty committed within one hour of one another are displayed as one
combined marker, while the underlying tank events remain independent.

Below the chart the page shows, for example:

```
2 days since last fresh water fill, used 22%
1 day since last grey water empty, used 18%
```

The usage value is percentage points from the committed event level to the
current level. Before the first qualifying event, the corresponding line says
that no fill or empty has been recorded.

## Data and persistence

Add a focused water-history component, following the existing file-backed
NDJSON history conventions. It owns:

- timestamped fresh and grey percentage samples;
- seven-day retention and startup loading of the retained tail;
- event detection and committed event records; and
- latest-event usage summaries.

Samples are accepted only from valid telemetry and only when the source
timestamp is newer than the last accepted timestamp. Repeated polling of the
same telemetry frame must not create duplicate samples. History storage and
event state must survive service restarts.

The service cannot observe changes while it is offline. If it reconnects and
first sees a level that qualifies as a settled fill or empty, the event is
timestamped at the time the service first observes it, rather than inferred
from an unavailable interval.

The retention period is seven days or more. Compaction must remove older
records without affecting the retained chart window or the latest event needed
for the usage summary.

## Event detection

The initial configurable defaults are:

- movement threshold: 5 percentage points;
- settling period: 10 minutes;
- display grouping window: 1 hour.

The detector keeps a settled baseline for each tank. A fresh fill candidate is
created when fresh water rises by at least the threshold from its settled
baseline. A grey empty candidate is created when grey water falls by at least
the threshold. Further readings in the same direction belong to the candidate
instead of creating additional events, which handles fills and empties taking
several minutes.

An active candidate is committed after the level has remained settled for the
settling period. The final level and commit time are recorded, the tank’s
settled baseline is moved to that final level, and the candidate is cleared.
Opposite-direction movement closes the candidate rather than extending it.
Only committed events affect the “since last” summaries or chart markers.

The detector must ignore invalid, missing, stale, and out-of-range readings.
It must not interpret startup state as an event. A candidate that has not
reached the threshold must not be committed as a fill or empty.

## Backend API and live updates

Add a dedicated GET water-history endpoint returning:

- the seven-day sample series for both tanks;
- independent committed fill and empty events;
- display-ready grouped markers; and
- the latest-event summary for each tank, including elapsed time and used
  percentage points where available.

The existing water state endpoint remains unchanged. The runtime publishes a
water-history update when an accepted sample or committed event changes the
history response. The browser listens through the existing server-sent event
connection and refreshes the history view without introducing polling.

## UI and rendering

Use a dependency-free SVG chart integrated into the existing Water page.
Implement the chart with explicit scales and accessible labels rather than
depending on a new charting library. The chart must:

- keep 0% and 100% visible and correctly oriented;
- cover exactly the latest seven-day window;
- label the time axis sufficiently for a week view;
- render the fresh series blue and grey series dark grey;
- show grouped event markers without duplicating a marker for paired events;
- leave unavailable gaps empty instead of drawing misleading zero values; and
- retain a textual summary for users who cannot interpret the graphic.

## Testing and acceptance

Backend tests will cover valid sample acceptance, duplicate timestamps,
invalid readings, seven-day retention, restart loading, threshold filtering,
multi-minute movements, the ten-minute settling rule, opposite-direction
handling, independent summaries, and one-hour marker grouping.

HTTP tests will cover the new endpoint, method handling, and its JSON shape.
Browser tests will cover API loading/live refresh, fixed chart bounds and
series colors, grouped markers, summary text, and no-event states using
rendered DOM/SVG assertions.

The feature is complete when a simulated service can accumulate a week of
water samples, restart without losing the retained history or last events, and
the Water page displays both lines, grouped markers, and percentage usage
since the last qualifying event.
