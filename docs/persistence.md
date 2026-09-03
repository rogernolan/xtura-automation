# Persistence and crash recovery

The Pi can lose power without warning. Every new durable data store must be
designed for interrupted writes and must not make the daemon unable to start
because one historical record is damaged.

## Write requirements

- For append-only formats such as NDJSON, serialize one complete record and
  newline as a single write. Check for both write errors and short writes.
- Call `Sync` before reporting a persisted record as successful. This limits
  the amount of acknowledged data lost by an unexpected power-off.
- For snapshots and rewrites, write a temporary file in the same directory,
  flush and close it, then rename it over the destination. Never rewrite the
  live file in place.
- Keep temporary and recovery filenames in the same directory so rename is
  atomic on the target filesystem.

## Startup recovery requirements

- A malformed historical record must be logged as an error, skipped, and
  removed by an atomic rewrite that preserves all valid records.
- An unterminated final record must not be concatenated with the next append.
  Clean it before opening the append writer.
- A malformed small runtime-state file should be logged as an error, moved to
  a timestamped `.corrupt-*` backup, and replaced with a safe default. The
  backup must remain recoverable.
- File I/O errors and failed cleanup/quarantine operations must remain visible
  in the logs. Do not silently discard a failure to repair a file.
- Corruption in optional historical data should degrade that feature rather
  than prevent the heating service and API from starting. Configuration errors
  that make safe operation ambiguous may remain startup-fatal.

## Tests required for a new store

Add tests covering valid data surviving:

- a malformed record in the middle of a file;
- a truncated or unterminated final record followed by a new append;
- a restart after the cleanup; and
- a failed rewrite or quarantine where practical.

The existing implementations in `service/history`, `service/waterhistory`,
and `service/tracking` are the reference patterns.
