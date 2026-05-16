package server

import (
	"net/http"
	"time"
)

// ReplanCell is one (project, hour-of-day) cell.
type ReplanCell struct {
	Project      string  `json:"project"`
	Hour         int     `json:"hour"`
	SessionCount int     `json:"session_count"`
	MeanScore    float64 `json:"mean_score"`
}

// ReplanHeatmapResponse is the envelope.
type ReplanHeatmapResponse struct {
	WindowDays int          `json:"window_days"`
	Cells      []ReplanCell `json:"cells"`
	// Projects is the distinct project list so the dashboard can render
	// rows in a stable order without resorting client-side.
	Projects []string `json:"projects"`
}

func (s *Server) handleReplanHeatmap(w http.ResponseWriter, r *http.Request) {
	days := parseDays(r)
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Unix()

	// SQLite's strftime can derive the hour-of-day from a unix epoch.
	rows, err := s.store.DB().QueryContext(r.Context(), `
		SELECT
			COALESCE(s.project, '(unassigned)') AS project,
			CAST(strftime('%H', s.start_ts, 'unixepoch') AS INTEGER) AS hour,
			COUNT(*) AS session_count,
			AVG(s.replan_score) AS mean_score
		FROM sessions s
		WHERE s.start_ts >= ?
		GROUP BY project, hour
		ORDER BY project ASC, hour ASC
	`, cutoff)
	if err != nil {
		writeInternalError(w, r, "replan-heatmap", err)
		return
	}
	defer rows.Close()

	projectsSet := map[string]struct{}{}
	out := ReplanHeatmapResponse{
		WindowDays: days,
		Cells:      []ReplanCell{},
	}
	for rows.Next() {
		var c ReplanCell
		if err := rows.Scan(&c.Project, &c.Hour, &c.SessionCount, &c.MeanScore); err != nil {
			writeInternalError(w, r, "replan-heatmap", err)
			return
		}
		out.Cells = append(out.Cells, c)
		projectsSet[c.Project] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		writeInternalError(w, r, "replan-heatmap", err)
		return
	}
	for p := range projectsSet {
		out.Projects = append(out.Projects, p)
	}
	writeJSON(w, http.StatusOK, out)
}
