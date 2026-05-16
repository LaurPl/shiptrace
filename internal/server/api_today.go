package server

import (
	"net/http"
	"time"
)

// TodaySession is the row shape returned by /api/today.
type TodaySession struct {
	ID             string  `json:"id"`
	Label          string  `json:"label,omitempty"`
	Project        string  `json:"project,omitempty"`
	Provider       string  `json:"provider"`
	Agent          string  `json:"agent,omitempty"`
	Skill          string  `json:"skill,omitempty"`
	Model          string  `json:"model,omitempty"`
	StartTs        int64   `json:"start_ts"`
	EndTs          int64   `json:"end_ts,omitempty"`
	PromptCount    int     `json:"prompt_count"`
	ToolCallCount  int     `json:"tool_call_count"`
	ReplanScore    float64 `json:"replan_score"`
	ShipCount      int     `json:"ship_count"`
}

// TodayResponse is the envelope.
type TodayResponse struct {
	AsOf     string         `json:"as_of"`
	Sessions []TodaySession `json:"sessions"`
}

func (s *Server) handleToday(w http.ResponseWriter, r *http.Request) {
	// "Today" really means "the last 24 hours" so a session that started
	// at 11 PM and ran until 1 AM still shows up in one place.
	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour).Unix()

	rows, err := s.store.DB().QueryContext(r.Context(), `
		SELECT
			s.id, COALESCE(s.label, ''), COALESCE(s.project, ''), s.provider,
			COALESCE(s.agent, ''), COALESCE(s.skill, ''), COALESCE(s.model, ''),
			s.start_ts, COALESCE(s.end_ts, 0),
			s.prompt_count, s.tool_call_count, s.replan_score,
			(SELECT COUNT(*) FROM ship_events WHERE session_id = s.id) AS ship_count
		FROM sessions s
		WHERE s.start_ts >= ? OR (s.end_ts IS NULL AND s.start_ts >= ?)
		ORDER BY s.start_ts DESC
	`, cutoff, cutoff)
	if err != nil {
		writeInternalError(w, r, "today", err)
		return
	}
	defer rows.Close()

	out := TodayResponse{
		AsOf:     now.Format(time.RFC3339),
		Sessions: []TodaySession{},
	}
	for rows.Next() {
		var ts TodaySession
		if err := rows.Scan(
			&ts.ID, &ts.Label, &ts.Project, &ts.Provider,
			&ts.Agent, &ts.Skill, &ts.Model,
			&ts.StartTs, &ts.EndTs,
			&ts.PromptCount, &ts.ToolCallCount, &ts.ReplanScore,
			&ts.ShipCount,
		); err != nil {
			writeInternalError(w, r, "today", err)
			return
		}
		out.Sessions = append(out.Sessions, ts)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, r, "today", err)
		return
	}
	// COALESCE'd end_ts of 0 is the dashboard's "still running" sentinel.
	writeJSON(w, http.StatusOK, out)
}
