# Overview Dashboard Design

Date: 2026-08-15

## Purpose

Define a read-only, single-glance `#/overview` dashboard for the vehicle. It
summarises temperature, electricity, water, and gas without duplicating the
controls owned by other areas of the UI.

The dashboard uses a balanced temperature-led layout: temperature is the first
signal but Power and Supplies retain comparable visual weight. It is a new,
separate feature following the navigation refactor specified in
`2026-08-15-ui-information-architecture-design.md`.

## Layout and Navigation

Overview is the default route and keeps the persistent four-item bottom bar.
It presents, in order:

1. a compact Alde main-temperature card;
2. a two-card Power group; and
3. a three-card Supplies group for fresh water, grey water, and gas.

Cards are status and navigation only. They must not operate equipment. Each
card deep-links to its relevant Controls section when one exists; a card that
needs configuration can navigate to `#/more/settings`.

## Temperature

The first delivery uses the Alde temperature as its only temperature reading
and as the main card's fixed source. It does not include humidity or sensor
selection.

SwitchBot setup is outside the scope of this work. A later, separately scoped
feature may configure three SwitchBot temperature-and-humidity sensors with
stable identities and user-facing names, select one as the primary source, and
show SwitchBot humidity alongside temperature. This future work must not be
required for the Alde-only Overview to ship.

The main card is colour-coded against configurable comfort bands. Defaults are
provided for cold, comfortable, warm, and hot conditions. Settings validation
requires the bands to be ordered and non-overlapping.

A temperature-history store retains timestamped Alde samples for enough time to
derive a 30-minute change, including after a
data-feed reconnect. If there is not enough usable history, the UI shows
`Trend unavailable`; it must not show a fabricated zero trend.

## Electricity

Overview shows the one aggregate Victron/Garmin battery state of charge and
the live charge/discharge current. Per-battery or bank-level display is
explicitly deferred until a feed becomes available.

During positive charging current, the dashboard shows a linear, explicitly
estimated time to full:

`remaining usable Ah / positive charge current A`

where `remaining usable Ah` is usable aggregate capacity multiplied by the
remaining state-of-charge fraction. This is intentionally a temporary
approximation; it does not model Victron's tapering charge profile. Replacing
it with a Victron charge-profile-aware/native estimate is follow-up work.

When the battery is not charging, Overview shows the live current and `Not
charging`, with no time-to-full estimate. A missing, zero, or invalid capacity
leaves the available battery reading visible and displays a configuration
prompt instead of an estimate.

## Water and Gas

Fresh and grey water display their available percentage values initially.
Litres for water are deferred until tank sizes/calibration have been measured.

Gas displays a percentage at all times. When a positive gas-tank capacity in
litres is configured, it also displays calculated litres. Missing or invalid
capacity leaves the percentage visible and offers a Settings prompt.

## Settings

Add a `Settings` section under More. It owns:

- ordered temperature comfort thresholds;
- usable aggregate battery capacity in Ah; and
- gas-tank capacity in litres.

Settings are persisted and validated. Comfort bands must remain ordered and
capacity values must be positive before derived values can be rendered.

## Data and Error States

A dedicated Overview state adapter combines temperature, battery, water, and
gas feeds into display-ready values. Every reading is represented as one of
`loading`, `available`, `stale`, `unavailable`, or `error` rather than leaving
the UI to infer state from a missing number.

Unavailable, stale, and failed values are visually labelled and must not be
presented as zero, off, normal, or healthy. The rest of Overview stays usable
when one source is unavailable. A configuration-dependent derived value never
hides a source reading that is otherwise available.

## Scope and Delivery

Existing navigation work establishes Overview as the default screen and its
deep-link contract. This feature then requires separately bounded work for:

1. Alde temperature-history storage;
2. Victron/Garmin aggregate state-of-charge and current mapping;
3. fresh, grey, and gas tank mappings;
4. Settings persistence and validation; and
5. Overview UI rendering and deep links.

The dashboard may ship with partial feeds as long as their state is explicit.
It must not wait for SwitchBot setup, future per-battery data, water litre
calibration, or a Victron-native time-to-full estimate.

## Verification

Tests cover:

- comfort-band validation and colour mapping;
- 30-minute trend calculation, reconnection history, and insufficient history;
- linear time-to-full for charging, full, zero-current, missing-capacity, and
  invalid-capacity cases;
- gas percentage/litre conversion and absent capacity;
- explicit loading, stale, unavailable, and error rendering;
- Overview deep links and confirmation that cards do not send control actions.

Manual mobile verification confirms the chosen balanced layout remains
glanceable, all four bottom destinations remain reachable, and partial live
data never appears as normal telemetry.
