package server

import (
	"net/http"
	"time"
)

// DistributionProject is one row of the sessions-to-ship distribution.
type DistributionProject struct {
	Name            string  `json:"name"`
	Sessions        int     `json:"sessions"`
	Ships           int     `json:"ships"`
	SessionsPerShip float64 `json:"sessions_per_ship"`
	MeanReplanScore float64 `json:"mean_replan_score"`
}

// DistributionResponse is the envelope.
type DistributionResponse struct {
	WindowDays int                   `json:"window_days"`
	Projects   []DistributionProject `json:"projects"`
}

func (s *Server) handleDistribution(w http.ResponseWriter, r *http.Request) {
	days := parseDays(r)
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Unix()

	rows, err := s.store.DB().QueryContext(r.Context(), `
		SELECT
			COALESCE(s.project, '(unassigned)') AS project,
			COUNT(DISTINCT s.id) AS sessions,
			COALESCE(SUM((SELECT COUNT(*) FROM ship_events WHERE session_id = s.id)), 0) AS ships,
			AVG(s.replan_score) AS mean_replan_score
		FROM sessions s
		WHERE s.start_ts >= ?`+phantomFilter(r)+`
		GROUP BY project
		ORDER BY sessions DESC
	`, cutoff)
	if err != nil {
		writeInternalError(w, r, "distribution", err)
		return
	}
	defer rows.Close()

	out := DistributionResponse{
		WindowDays: days,
		Projects:   []DistributionProject{},
	}
	for rows.Next() {
		var p DistributionProject
		if err := rows.Scan(&p.Name, &p.Sessions, &p.Ships, &p.MeanReplanScore); err != nil {
			writeInternalError(w, r, "distribution", err)
			return
		}
		if p.Ships > 0 {
			p.SessionsPerShip = float64(p.Sessions) / float64(p.Ships)
		} else {
			p.SessionsPerShip = 0 // dashboard renders "n/a"
		}
		out.Projects = append(out.Projects, p)
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, r, "distribution", err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
