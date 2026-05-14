package server

import (
	"net/http"
	"time"
)

// ProviderRow is per-provider session/ship counts.
type ProviderRow struct {
	Name            string  `json:"name"`
	Sessions        int     `json:"sessions"`
	Ships           int     `json:"ships"`
	SessionsPerShip float64 `json:"sessions_per_ship"`
}

// ProviderMixResponse is the envelope.
type ProviderMixResponse struct {
	WindowDays int           `json:"window_days"`
	Providers  []ProviderRow `json:"providers"`
}

func (s *Server) handleProviderMix(w http.ResponseWriter, r *http.Request) {
	days := parseDays(r)
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Unix()

	rows, err := s.store.DB().QueryContext(r.Context(), `
		SELECT
			s.provider,
			COUNT(DISTINCT s.id) AS sessions,
			COALESCE(SUM((SELECT COUNT(*) FROM ship_events WHERE session_id = s.id)), 0) AS ships
		FROM sessions s
		WHERE s.start_ts >= ?
		GROUP BY s.provider
		ORDER BY sessions DESC
	`, cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	out := ProviderMixResponse{
		WindowDays: days,
		Providers:  []ProviderRow{},
	}
	for rows.Next() {
		var p ProviderRow
		if err := rows.Scan(&p.Name, &p.Sessions, &p.Ships); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if p.Ships > 0 {
			p.SessionsPerShip = float64(p.Sessions) / float64(p.Ships)
		}
		out.Providers = append(out.Providers, p)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
