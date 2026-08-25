# Deterministic Grey-Tank Empty Detection

## Goal

Record a grey-tank empty event from the EmpirBus discharge signals instead of
inferring it from a falling grey-water percentage. Fresh-water fill detection
continues to use the existing percentage heuristic.

## Approved behavior

- Signal `4` (`Tank Discharge Open`) starts a discharge cycle.
- Signal `5` (`Tank Discharge Close`) completes the cycle and records one grey
  `empty` event.
- A close signal without a preceding open signal is ignored.
- Quick open/close sequences are preserved as user behavior; there is no
  one-minute collapse or suppression rule.
- Grey percentage samples remain available for the current level and chart,
  but a level change alone cannot create a grey-empty event.

## Design

The Garmin adapter will maintain the discharge-cycle edge state from received
signal updates. It will expose a completed close timestamp only after a signal
4 open has been followed by a signal 5 close. Repeated status frames and
runtime polling must not expose the same completion more than once.

The runtime water-history loop will consume each completed discharge and call a
dedicated deterministic method on `waterhistory.Store`. The existing sample
observation path will continue to process fresh-water percentage movement and
store both percentage series. Grey percentage movement will no longer call the
generic tank movement detector.

The deterministic event will retain the latest known grey percentage as event
metadata where available. The event trigger and event timestamp come from the
signal sequence, not from a threshold or settling period.

## Persistence and recovery

The water-history store will persist deterministic events through the existing
event log/state mechanism. Recording must be idempotent for the same signal
completion timestamp. On adapter reconnect, a stale signal state must not be
treated as a new close transition; only a newly received open-to-close sequence
can create a completion.

## Error handling

- Malformed or unavailable signal frames do not create a discharge event.
- An unmatched close is ignored without affecting fresh-water detection.
- Existing percentage validation and persistence errors remain unchanged.
- A water-history recording error is logged by the runtime loop and does not
  stop telemetry polling.

## Testing

Add focused tests for:

1. signal 5 without signal 4 produces no event;
2. signal 4 followed by signal 5 produces one grey-empty event;
3. repeated signal status updates and runtime polling do not duplicate it;
4. a grey level drop alone produces no event;
5. fresh-water fills retain the existing threshold and settling behavior;
6. reconnect/reset does not turn a stale close state into a new event;
7. event metadata uses the latest known grey percentage when present.

