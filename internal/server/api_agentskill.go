package server

import (
	"net/http"
	"time"
)

// AgentSkillRow is one bar of the ROI chart, used for both by_agent and
// by_skill slices.
type AgentSkillRow struct {
	Name            string  `json:"name"`
	Sessions        int     `json:"sessions"`
	Ships           int     `json:"ships"`
	SessionsPerShip float64 `json:"sessions_per_ship"`
}

// AgentSkillResponse is the envelope.
type AgentSkillResponse struct {
	WindowDays int             `json:"window_days"`
	ByAgent    []AgentSkillRow `json:"by_agent"`
	BySkill    []AgentSkillRow `json:"by_skill"`
}

func (s *Server) handleAgentSkillROI(w http.ResponseWriter, r *http.Request) {
	days := parseDays(r)
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Unix()

	filter := phantomFilter(r)
	byAgent, err := s.aggregateAgentSkill(r, cutoff, "agent", filter)
	if err != nil {
		writeInternalError(w, r, "agent-skill-roi", err)
		return
	}
	bySkill, err := s.aggregateAgentSkill(r, cutoff, "skill", filter)
	if err != nil {
		writeInternalError(w, r, "agent-skill-roi", err)
		return
	}
	writeJSON(w, http.StatusOK, AgentSkillResponse{
		WindowDays: days,
		ByAgent:    byAgent,
		BySkill:    bySkill,
	})
}

// aggregateAgentSkill runs the per-dimension grouping. Two callers, one
// helper — we accept a small SQL-string-formatting risk for the column
// name (`agent` or `skill`); a switch keeps the input set closed.
func (s *Server) aggregateAgentSkill(r *http.Request, cutoff int64, column, filter string) ([]AgentSkillRow, error) {
	switch column {
	case "agent", "skill":
	default:
		return nil, nil
	}
	// nolint:gosec — column is from a closed set above; filter is from
	// store.PhantomFilterSQL (constant) or empty.
	query := `
		SELECT
			COALESCE(NULLIF(s.` + column + `, ''), '(none)') AS name,
			COUNT(DISTINCT s.id) AS sessions,
			COALESCE(SUM((SELECT COUNT(*) FROM ship_events WHERE session_id = s.id)), 0) AS ships
		FROM sessions s
		WHERE s.start_ts >= ?` + filter + `
		GROUP BY name
		ORDER BY sessions DESC
	`
	rows, err := s.store.DB().QueryContext(r.Context(), query, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentSkillRow
	for rows.Next() {
		var row AgentSkillRow
		if err := rows.Scan(&row.Name, &row.Sessions, &row.Ships); err != nil {
			return nil, err
		}
		if row.Ships > 0 {
			row.SessionsPerShip = float64(row.Sessions) / float64(row.Ships)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
