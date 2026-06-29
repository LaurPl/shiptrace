package store

// PhantomFilterSQL is a WHERE-clause fragment that excludes phantom
// sessions from aggregate queries.
//
// A phantom session is a row materialized by a session_stop event with
// no preceding session_start: end_ts equals start_ts (because the
// ingester used the stop timestamp for both), and prompt/tool counts
// are zero (no work was ever recorded). The recorder no longer emits
// these (see internal/hooks/claudecode/handler.go HandleStop), but rows
// generated before that fix still sit in users' SQLite stores.
//
// In-progress sessions (end_ts IS NULL) are preserved because their
// zero counts are normal — they just haven't done work yet.
//
// The `end_ts_inferred = 0` clause is load-bearing: it scopes "phantom" to
// real-stop-derived rows. A session finalized by the staleness sweep
// (end_ts_inferred = 1) can also have end_ts == start_ts and zero counts — a
// session that started, did nothing observable, and was abandoned — but it had
// a *real* session_start (so start_ts is its true birth, not a stop-timestamp
// artifact). That is a real, if empty, session, not a phantom; without this
// clause it would be silently dropped from every aggregate. Provenance, not the
// timestamp coincidence, decides — which is exactly what the marker column
// exists to encode.
//
// Callers splice this into queries where the sessions table is aliased
// as `s`. It begins with " AND " so it can be appended to any existing
// WHERE clause without re-parenthesizing.
const PhantomFilterSQL = ` AND NOT (s.end_ts IS NOT NULL AND s.end_ts_inferred = 0 AND s.start_ts = s.end_ts AND s.prompt_count = 0 AND s.tool_call_count = 0)`
