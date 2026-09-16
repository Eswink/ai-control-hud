package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

type Server struct {
	store   *store.SnapshotStore
	version string
	started time.Time
	now     func() time.Time
}

func New(store *store.SnapshotStore, version string, started time.Time) *Server {
	return &Server{
		store:   store,
		version: version,
		started: started,
		now:     time.Now,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/state", s.handleState)
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	return mux
}

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	now := s.now().UTC()
	state := s.store.Get()
	state.Server.Version = s.version
	state.Server.Time = now
	state.Server.UptimeSeconds = max(0, int64(now.Sub(s.started).Seconds()))
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	now := s.now().UTC()
	state := s.store.Get()
	payload := domain.HealthResponse{
		Status:        "ok",
		SchemaVersion: domain.SchemaVersion,
		Time:          now,
		Sources: domain.HealthSources{
			ZCode:       state.ZCode.Health.Status,
			CommandCode: state.CommandCode.Health.Status,
		},
	}
	writeJSON(w, http.StatusOK, payload)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
