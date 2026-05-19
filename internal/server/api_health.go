package server

import (
	"net/http"
)

// HealthResponse is the shape returned by /api/health. Cheap signal the
// dashboard uses to decide whether to render the "no ships yet" banner.
type HealthResponse struct {
	HasShips bool `json:"has_ships"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var exists int
	err := s.store.DB().QueryRowContext(r.Context(),
		`SELECT EXISTS(SELECT 1 FROM ship_events LIMIT 1)`).Scan(&exists)
	if err != nil {
		writeInternalError(w, r, "health", err)
		return
	}
	writeJSON(w, http.StatusOK, HealthResponse{HasShips: exists == 1})
}
