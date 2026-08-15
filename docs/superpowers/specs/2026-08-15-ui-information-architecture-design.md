# UI Information Architecture Design

Date: 2026-08-15

## Purpose

Replace the current four independent top-level tabs with a phone-first,
bottom-navigation structure that separates everyday vehicle control from
specialist and diagnostic functions. Add a status-first Overview as the default
screen. This document defines the navigation and screen ownership only; it does
not implement the future Energy domain, Location map/list views, or Overview's
service data.

## Information Architecture

The persistent bottom navigation has four destinations:

| Destination | Purpose | Sections |
| --- | --- | --- |
| Overview | Read-first summary of vehicle status and direct links to the relevant area. | None. |
| Controls | Everyday vehicle systems and their controls. | Heating, Water, Lighting, Energy (future). |
| Location | Vehicle position and journey logging. | None initially; recordings list and map view are later additions. |
| More | Lower-frequency operational and diagnostic functions. | System, Tools. |

`Controls` and `More` use the same compact, inline section switcher pattern.
They are grouping destinations rather than separate catch-all screens. The
selected section is retained while navigating away and back. Controls defaults
to Heating; More defaults to System.

### Screen ownership

#### Overview

Overview is the default route and is deliberately read-only. It will show:

- battery level;
- fresh/grey water level as available;
- gas level;
- indoor and outdoor temperature; and
- a compact energy status.

Each summary card links to the appropriate destination and section. Examples:
water links to `Controls › Water`, indoor temperature links to `Controls ›
Heating`, and energy/battery links to `Controls › Energy` once that section is
available. Location status links to Location when shown. Overview must not
duplicate system control widgets.

#### Controls

Controls contains the existing Heating, Water, and Lighting content, moved
without changing their current APIs or safety behaviour. Energy is a reserved
fourth section. Its future work is out of scope here: it will provide energy
monitoring, alerting, and automation policies such as switching the inverter
off while moving with no 240 V load.

#### Location

Location owns the current location status and logging controls. It will later
own the recording list and the map viewer; those additions must not move into
Tools or Overview.

#### More

More contains two sections:

- **System** owns Pi status and service health.
- **Tools** owns WebSocket recording and future diagnostics.

## Interaction Model

- The four-item bottom bar remains visible while navigating between primary
  destinations and indicates the active destination.
- Inline switchers indicate the active Controls or More section.
- URLs encode the destination and optional section with hashes, for example
  `#/overview`, `#/controls/water`, `#/location`, and `#/more/tools`.
- Initial load, browser refresh, back/forward, and direct links restore the
  requested valid destination and section. Invalid or incomplete hashes fall
  back to `#/overview`.
- Overview cards navigate rather than operate equipment. Existing direct
  controls retain their current confirmation/error behaviour.
- All state retains its live event-driven behaviour. A loading, stale,
  unavailable, or failed value is visibly labelled; a missing reading must not
  be rendered as a zero, off state, or normal condition.

## Implementation Shape

This reorganisation is a frontend navigation refactor plus the already planned
Overview work; it does not require new APIs for Heating, Water, Lighting,
recording, tracking, or Pi status.

In the static UI:

- Replace the current top tab markup with a semantic four-item bottom
  navigation.
- Create primary screen containers for Overview, Controls, Location, and More.
- Move existing panels into the relevant screen/section: heating, water, and
  lighting under Controls; Pi status under More/System; WebSocket recording
  under More/Tools; tracking and location state under Location.
- Add a navigation state model consisting of an active primary destination plus
  remembered active sections for Controls and More.
- Parse and write the location hash in navigation actions and on `hashchange`.
- Make Overview deep-links set both the primary destination and target section.

The upcoming Overview implementation supplies and renders its own live data;
this navigation design only establishes its card locations and deep-link
contract. The future Energy implementation supplies its own state, rules, and
alerts under `Controls › Energy`.

## Error Handling

- An unrecognised route or section cannot leave a blank interface: route to
  Overview.
- A known screen with an unavailable backend state renders an explicit
  unavailable/loading/error message in that component, while the rest of the
  screen remains usable.
- SSE reconnection and existing action errors keep their current behaviour;
  regrouping panels must not conceal them.
- Navigation itself performs no mutating request.

## Tests and Verification

The navigation refactor should add or update tests for:

- HTML structure: four bottom destinations, Controls and More switchers, and
  moved existing panels;
- route parsing and serialization for each valid primary destination and
  section, including invalid-hash fallback;
- Overview deep-links selecting the correct Controls section;
- remembered active Controls and More sections after navigation;
- existing heating, water, lighting, tracking, recording, and Pi status UI
  actions remaining bound and functional.

Run the existing project checks after implementation:

```bash
go test ./...
go vet ./...
go build ./...
rtk lint eslint web/static/app.js
git diff --check
```

Manual mobile verification should confirm bottom-bar reachability, route
restoration after reload, live updates in every moved panel, and that no
unavailable metric is presented as a normal value.

## Rollout

1. Implement the navigation refactor and regroup current content.
2. Implement Overview next as the default dashboard, with live data and
   deep-links.
3. Add Location's recording list and map viewer separately.
4. Implement the future Controls/Energy domain separately.

Each stage is independently deployable and does not require the later stages to
make the navigation coherent.
