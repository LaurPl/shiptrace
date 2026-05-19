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
// Callers splice this into queries where the sessions table is aliased
// as `s`. It begins with " AND " so it can be appended to any existing
// WHERE clause without re-parenthesizing.
const PhantomFilterSQL = ` AND NOT (s.end_ts IS NOT NULL AND s.start_ts = s.end_ts AND s.prompt_count = 0 AND s.tool_call_count = 0)`
