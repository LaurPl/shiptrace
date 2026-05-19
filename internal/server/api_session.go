package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

// SessionInfo is the session-level metadata block in the detail response.
// Mirrors store.Session but with JSON-friendly omitempty for nullable
// fields so the dashboard sees absence rather than "" / 0.
type SessionInfo struct {
	ID                string `json:"id"`
	Label             string `json:"label,omitempty"`
	Provider          string `json:"provider"`
	ProviderSessionID string `json:"provider_session_id,omitempty"`
	Project           string `json:"project,omitempty"`
	StartTs           int64  `json:"start_ts"`
	EndTs             int64  `json:"end_ts,omitempty"`
	Model             string `json:"model,omitempty"`
	Agent             string `json:"agent,omitempty"`
	Skill             string `json:"skill,omitempty"`
}

// SessionToolEvent is the per-tool entry in the detail response.
// FilesTouched is included when non-empty; ToolInputHash lets the user
// correlate against the original JSONL line.
type SessionToolEvent struct {
	Ts            int64    `json:"ts"`
	Tool          string   `json:"tool"`
	ToolInputHash string   `json:"tool_input_hash,omitempty"`
	FilesTouched  []string `json:"files_touched,omitempty"`
}

// SessionReplanSignal carries the kind/weight pair plus any per-kind
// metadata the recorder stashed at write time. Metadata is passed
// through as raw JSON so the dashboard sees the same shape as the
// JSONL line.
type SessionReplanSignal struct {
	Ts       int64           `json:"ts"`
	Kind     string          `json:"kind"`
	Weight   float64         `json:"weight"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// SessionShipEvent carries one ship attribution.
type SessionShipEvent struct {
	Ts                int64           `json:"ts"`
	Kind              string          `json:"kind"`
	Ref               string          `json:"ref,omitempty"`
	AttributionMethod string          `json:"attribution_method,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
}

// SessionDetailResponse is the envelope for /api/session/{id}. Four
// time-ordered slices: the session header plus its three event streams.
// The dashboard drawer renders these as a unified timeline.
type SessionDetailResponse struct {
	Session       SessionInfo           `json:"session"`
	ToolEvents    []SessionToolEvent    `json:"tool_events"`
	ReplanSignals []SessionReplanSignal `json:"replan_signals"`
	ShipEvents    []SessionShipEvent    `json:"ship_events"`
}

// handleSession returns the full event stream for one session. 404 when
// the id is unknown, 400 when the id is empty. Path is /api/session/{id}
// using Go 1.22+ ServeMux path parameters.
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}

	ctx := r.Context()
	sess, err := s.store.GetSession(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeInternalError(w, r, "session", err)
		return
	}

	tools, err := s.store.ListToolEvents(ctx, id)
	if err != nil {
		writeInternalError(w, r, "session/tools", err)
		return
	}
	signals, err := s.store.ListReplanSignals(ctx, id)
	if err != nil {
		writeInternalError(w, r, "session/replan", err)
		return
	}
	ships, err := s.store.ListShipEvents(ctx, id)
	if err != nil {
		writeInternalError(w, r, "session/ships", err)
		return
	}

	out := SessionDetailResponse{
		Session: SessionInfo{
			ID:                sess.ID,
			Label:             sess.Label.String,
			Provider:          sess.Provider,
			ProviderSessionID: sess.ProviderSessionID.String,
			Project:           sess.Project.String,
			StartTs:           sess.StartTs,
			EndTs:             sess.EndTs.Int64,
			Model:             sess.Model.String,
			Agent:             sess.Agent.String,
			Skill:             sess.Skill.String,
		},
		ToolEvents:    make([]SessionToolEvent, 0, len(tools)),
		ReplanSignals: make([]SessionReplanSignal, 0, len(signals)),
		ShipEvents:    make([]SessionShipEvent, 0, len(ships)),
	}
	for _, t := range tools {
		out.ToolEvents = append(out.ToolEvents, SessionToolEvent{
			Ts:            t.Ts,
			Tool:          t.Tool,
			ToolInputHash: t.ToolInputHash,
			FilesTouched:  t.FilesTouched,
		})
	}
	for _, sig := range signals {
		out.ReplanSignals = append(out.ReplanSignals, SessionReplanSignal{
			Ts:       sig.Ts,
			Kind:     sig.Kind,
			Weight:   sig.Weight,
			Metadata: sig.Metadata,
		})
	}
	for _, sh := range ships {
		out.ShipEvents = append(out.ShipEvents, SessionShipEvent{
			Ts:                sh.Ts,
			Kind:              sh.Kind,
			Ref:               sh.Ref,
			AttributionMethod: sh.AttributionMethod,
			Metadata:          sh.Metadata,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
