package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/LaurPl/shiptrace/internal/events"
)

// InsertToolEvent records a tool_use event. tool_events is append-only:
// the eventlog checkpoint prevents duplicate ingestion, so we don't need
// an idempotency key.
func (s *Store) InsertToolEvent(ctx context.Context, e events.Event) error {
	if e.SessionID == "" {
		return fmt.Errorf("store: tool_use missing session_id")
	}
	var filesJSON any
	if len(e.FilesTouched) > 0 {
		b, err := json.Marshal(e.FilesTouched)
		if err != nil {
			return fmt.Errorf("store: marshal files_touched: %w", err)
		}
		filesJSON = string(b)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tool_events (session_id, ts, tool, tool_input_hash, files_touched)
		VALUES (?, ?, ?, ?, ?)
	`,
		e.SessionID,
		e.Ts.Unix(),
		e.Tool,
		nullableString(e.ToolInputHash),
		filesJSON,
	)
	if err != nil {
		return fmt.Errorf("store: insert tool_event: %w", err)
	}
	// Roll the count up onto the sessions row so the dashboard doesn't
	// need a join for the common "tool calls per session" query.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET tool_call_count = tool_call_count + 1 WHERE id = ?`,
		e.SessionID,
	); err != nil {
		return fmt.Errorf("store: bump tool_call_count: %w", err)
	}
	return nil
}

// InsertReplanSignal records a replan_signal event. metadata is stored as
// JSON so day 4 can read kind-specific fields (TodoWrite status counts,
// pivot phrase, etc.) without a schema change here.
func (s *Store) InsertReplanSignal(ctx context.Context, e events.Event) error {
	if e.SessionID == "" {
		return fmt.Errorf("store: replan_signal missing session_id")
	}
	kind := extractString(e.Metadata, "kind")
	if kind == "" {
		kind = "unknown"
	}
	weight := 1.0
	if v, ok := e.Metadata["weight"].(float64); ok {
		weight = v
	}

	// Stash the metadata sans the keys we promoted to columns.
	stash := cloneMetadata(e.Metadata)
	delete(stash, "kind")
	delete(stash, "weight")
	metaJSON, err := json.Marshal(stash)
	if err != nil {
		return fmt.Errorf("store: marshal replan metadata: %w", err)
	}
	if string(metaJSON) == "null" || string(metaJSON) == "{}" {
		metaJSON = nil
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO replan_signals (session_id, ts, kind, weight, metadata)
		VALUES (?, ?, ?, ?, ?)
	`,
		e.SessionID,
		e.Ts.Unix(),
		kind,
		weight,
		nullableBytes(metaJSON),
	)
	if err != nil {
		return fmt.Errorf("store: insert replan_signal: %w", err)
	}
	return nil
}

// BumpSessionPromptCount increments prompt_count on the sessions row. The
// ingester calls this on every prompt event so the dashboard can show
// per-session prompt counts cheaply.
func (s *Store) BumpSessionPromptCount(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET prompt_count = prompt_count + 1 WHERE id = ?`,
		sessionID,
	)
	return err
}
