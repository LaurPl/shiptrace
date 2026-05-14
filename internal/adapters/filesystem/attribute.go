// Package filesystem is the ship adapter that watches user-configured
// `ship_paths` and emits ship events when a file lands there. Attribution
// runs in precedence order:
//
//  1. file_overlap — the session whose tool_events recently touched this
//     exact file. Strongest signal.
//  2. time_window  — the most recent session that ended within
//     DefaultTimeWindow of the file's mtime.
//  3. none         — emit an unattributed ship event; the user can
//     correct later via `shiptrace tag`.
package filesystem

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LaurPl/shiptrace/internal/store"
)

// AttributionMethod names the resolved method for a particular file land.
type AttributionMethod string

const (
	MethodFileOverlap AttributionMethod = "file_overlap"
	MethodTimeWindow  AttributionMethod = "time_window"
	MethodNone        AttributionMethod = ""
)

// DefaultTimeWindow is the fallback window for time-based attribution.
// Pick a value short enough that an unrelated session yesterday doesn't
// steal credit; long enough to span a quick "stop CC → drag file to
// published/" workflow.
const DefaultTimeWindow = 30 * time.Minute

// FileOverlapLookback bounds how far back we look in tool_events for a
// file match. Without a bound a long-running install with millions of
// tool_events rows could scan forever; with one we cap query time.
const FileOverlapLookback = 24 * time.Hour

// Attribution is the outcome of resolving a single file_landed event to a
// session. SessionID may be empty when Method is MethodNone.
type Attribution struct {
	SessionID string
	Method    AttributionMethod
}

// AttributeFile resolves which session a file land should be credited to.
// fileMtime is the OS-reported mtime; ext is whatever the caller wants to
// stash in metadata (kept opaque here).
//
// We deliberately query SQLite read-only here. The architectural
// invariant "only internal/ingest writes to store" still holds — this
// path only reads.
func AttributeFile(ctx context.Context, s *store.Store, absPath string, fileMtime time.Time) (Attribution, error) {
	if s == nil {
		return Attribution{}, errors.New("filesystem: nil store")
	}
	if absPath == "" {
		return Attribution{}, errors.New("filesystem: empty path")
	}

	// 1. file_overlap — JSON1's json_each handles the array shape we
	//    stored in tool_events.files_touched.
	id, err := queryFileOverlap(ctx, s, absPath, fileMtime)
	if err != nil {
		return Attribution{}, err
	}
	if id != "" {
		return Attribution{SessionID: id, Method: MethodFileOverlap}, nil
	}

	// 2. time_window — most recent session that ended within the window
	//    OR is still active (end_ts NULL means still active, so any
	//    file landed while a session is in progress attributes to it).
	id, err = queryTimeWindow(ctx, s, fileMtime, DefaultTimeWindow)
	if err != nil {
		return Attribution{}, err
	}
	if id != "" {
		return Attribution{SessionID: id, Method: MethodTimeWindow}, nil
	}

	return Attribution{Method: MethodNone}, nil
}

func queryFileOverlap(ctx context.Context, s *store.Store, absPath string, fileMtime time.Time) (string, error) {
	cutoff := fileMtime.Add(-FileOverlapLookback).Unix()
	row := s.DB().QueryRowContext(ctx, `
		SELECT te.session_id
		FROM tool_events te, json_each(te.files_touched) j
		WHERE te.ts >= ?
		  AND (j.value = ? OR j.value = ?)
		ORDER BY te.ts DESC
		LIMIT 1
	`,
		cutoff,
		absPath,
		// Tool inputs may carry a path with leading "./" or other normalizations;
		// we strip common prefixes for the match. Cheap belt-and-braces.
		strings.TrimPrefix(absPath, "./"),
	)
	var id sql.NullString
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("filesystem: query file_overlap: %w", err)
	}
	if !id.Valid {
		return "", nil
	}
	return id.String, nil
}

func queryTimeWindow(ctx context.Context, s *store.Store, fileMtime time.Time, window time.Duration) (string, error) {
	// "Within window" means end_ts is in [mtime-window, mtime] OR end_ts
	// is NULL and the session was started no earlier than (mtime - window
	// - some grace) so a still-active session naturally claims the file.
	lower := fileMtime.Add(-window).Unix()
	upper := fileMtime.Unix()
	row := s.DB().QueryRowContext(ctx, `
		SELECT id
		FROM sessions
		WHERE
			(end_ts IS NOT NULL AND end_ts BETWEEN ? AND ?)
			OR
			(end_ts IS NULL AND start_ts BETWEEN ? AND ?)
		ORDER BY COALESCE(end_ts, start_ts) DESC
		LIMIT 1
	`, lower, upper, lower, upper)
	var id string
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("filesystem: query time_window: %w", err)
	}
	return id, nil
}
