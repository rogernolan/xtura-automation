# Long-term sensor history design

## Goal

Retain approximately ten-minute sensor samples for 30 days and hourly samples
indefinitely for every temperature sensor and for both water tanks. Keep the
existing file-backed approach; SQLite is not required.

## Storage model

Temperature history remains one NDJSON stream per sensor. Water history remains
its existing store, with fresh and grey values in each point. Compaction will
maintain two tiers:

- a recent tier containing samples no older than 30 days, reduced to at most
  one representative sample per ten-minute bucket;
- an hourly archive containing one representative sample per hour, retained
  indefinitely.

Archive files are partitioned by sensor and calendar month so startup and
range reads do not require scanning one unbounded file. Existing files remain
readable and are migrated during compaction.

## Runtime and API behavior

Live state and overview rendering remain bounded to the existing recent window.
History endpoints gain optional range/resolution behavior only if needed by the
existing UI; the default response remains bounded. Water event summaries remain
separate from sampled history, and existing event behavior is preserved.

## Migration and failure handling

Compaction is idempotent. It reads existing NDJSON, buckets samples by time,
writes replacement files atomically, and removes no source data until the
replacement is safely written. A restart during compaction must leave either
the old or new readable file set.

## Testing

Use deterministic timestamps and temporary directories to test:

- ten-minute reduction and 30-day cutoff;
- hourly archive creation and preservation across repeated compactions;
- both temperature and water data, including sparse/missing tank values;
- loading legacy files and bounded default API responses;
- restart/reload behavior after compaction.

No hardware or long-running wall-clock test is required.
