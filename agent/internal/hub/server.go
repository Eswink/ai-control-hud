package hub

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

const maxRequestBody = 2 << 20

type Server struct {
	config    Config
	store     *Store
	version   string
	now       func() time.Time
	startedAt time.Time
	handler   http.Handler
}

func NewServer(config Config, store *Store, version string, now func() time.Time) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("hub store is required")
	}
	if strings.TrimSpace(version) == "" {
		version = "dev"
	}
	if now == nil {
		now = time.Now
	}
	s := &Server{
		config:    config,
		store:     store,
		version:   version,
		now:       now,
		startedAt: now().UTC(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agent/state", s.handleAgentState)
	mux.HandleFunc("/api/v1/agent/heartbeat", s.handleAgentHeartbeat)
	mux.HandleFunc("/api/v1/agent/events", s.handleAgentEvents)
	mux.HandleFunc("/api/v1/state", s.handleState)
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/events", s.handleEvents)
	s.handler = mux
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) handleAgentState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "invalid agent token")
		return
	}
	var envelope StateEnvelope
	if err := decodeRequest(r, &envelope); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := envelope.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	receivedAt := s.utcnow()
	if err := s.store.RecordState(r.Context(), envelope.AgentID, envelope.SentAt, envelope.State, receivedAt); err != nil {
		writeError(w, http.StatusInternalServerError, "persist state failed")
		return
	}
	writeJSON(w, http.StatusOK, AgentReceipt{Status: "accepted", AgentID: envelope.AgentID, ReceivedAt: receivedAt})
}

func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "invalid agent token")
		return
	}
	var heartbeat Heartbeat
	if err := decodeRequest(r, &heartbeat); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := heartbeat.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	receivedAt := s.utcnow()
	if err := s.store.RecordHeartbeat(r.Context(), heartbeat.AgentID, heartbeat.SentAt, receivedAt, heartbeat.AgentVersion); err != nil {
		writeError(w, http.StatusInternalServerError, "persist heartbeat failed")
		return
	}
	writeJSON(w, http.StatusOK, AgentReceipt{Status: "accepted", AgentID: heartbeat.AgentID, ReceivedAt: receivedAt})
}

func (s *Server) handleAgentEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "invalid agent token")
		return
	}
	var batch EventBatch
	if err := decodeRequest(r, &batch); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := batch.Validate(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	receivedAt := s.utcnow()
	accepted, duplicates, err := s.store.RecordEvents(r.Context(), batch.AgentID, batch.SentAt, batch.Events, receivedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "persist events failed")
		return
	}
	writeJSON(w, http.StatusOK, EventReceipt{
		Status:     "accepted",
		AgentID:    batch.AgentID,
		ReceivedAt: receivedAt,
		Accepted:   accepted,
		Duplicates: duplicates,
	})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	state, lastSeen, found, err := s.store.LoadState(r.Context(), s.config.PrimaryAgentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load state failed")
		return
	}
	if !found {
		writeError(w, http.StatusServiceUnavailable, "primary agent has not uploaded a state snapshot")
		return
	}
	writeJSON(w, http.StatusOK, s.projectState(state, lastSeen, s.utcnow()))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	now := s.utcnow()
	state, lastSeen, found, err := s.store.LoadState(r.Context(), s.config.PrimaryAgentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load health failed")
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, domain.HealthResponse{
			Status:        "ok",
			SchemaVersion: domain.SchemaVersion,
			Time:          now,
			Sources: domain.HealthSources{
				ZCode:       domain.SourceError,
				CommandCode: domain.SourceError,
			},
		})
		return
	}
	projected := s.projectState(state, lastSeen, now)
	writeJSON(w, http.StatusOK, domain.HealthResponse{
		Status:        "ok",
		SchemaVersion: domain.SchemaVersion,
		Time:          now,
		Sources: domain.HealthSources{
			ZCode:       projected.ZCode.Health.Status,
			CommandCode: projected.CommandCode.Health.Status,
		},
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	after, err := parseInt64Query(r, "after", 0, 0, int64(^uint64(0)>>1))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	limit64, err := parseInt64Query(r, "limit", 100, 1, 100)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	items, latest, err := s.store.ListEvents(r.Context(), s.config.PrimaryAgentID, after, int(limit64))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load events failed")
		return
	}
	nextAfter := after
	if len(items) > 0 {
		nextAfter = items[len(items)-1].Seq
	}
	writeJSON(w, http.StatusOK, EventPage{
		SchemaVersion: EventSchemaVersion,
		Events:        items,
		NextAfter:     nextAfter,
		LatestSeq:     latest,
	})
}

func (s *Server) authorized(r *http.Request) bool {
	expected := "Bearer " + s.config.AgentToken
	actual := r.Header.Get("Authorization")
	if len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func (s *Server) utcnow() time.Time {
	return s.now().UTC()
}

func (s *Server) projectState(state domain.HudState, lastSeen, now time.Time) domain.HudState {
	age := now.Sub(lastSeen.UTC())
	if age < 0 {
		age = 0
	}
	if age > s.config.StaleAfter {
		message := fmt.Sprintf("agent heartbeat stale; last seen %ds ago", int(age.Seconds()))
		if state.ZCode.Health.Status == domain.SourceOK {
			state.ZCode.Health = staleHealth(state.ZCode.Health, now, message)
		}
		if state.CommandCode.Health.Status == domain.SourceOK {
			state.CommandCode.Health = staleHealth(state.CommandCode.Health, now, message)
		}
	}
	state.Server.Version = s.version
	state.Server.Time = now
	uptime := now.Sub(s.startedAt)
	if uptime < 0 {
		uptime = 0
	}
	state.Server.UptimeSeconds = int64(uptime / time.Second)
	state.Overall.Status = domain.OverallDegraded
	if state.ZCode.Health.Status == domain.SourceOK && state.CommandCode.Health.Status == domain.SourceOK {
		state.Overall.Status = domain.OverallLive
	}
	return state
}

func staleHealth(health domain.SourceHealth, observedAt time.Time, message string) domain.SourceHealth {
	health.Status = domain.SourceStale
	health.ObservedAt = observedAt
	health.Message = &message
	return health
}

func decodeRequest(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody))
	// Match the original Pydantic/FastAPI protocol behavior: unknown fields are
	// ignored so a newer Agent may add optional fields without breaking an older
	// Hub. Required fields and semantic invariants are still checked by Validate.
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("invalid JSON body: multiple values")
		}
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func parseInt64Query(r *http.Request, name string, fallback, minimum, maximum int64) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be within %d..%d", name, minimum, maximum)
	}
	return value, nil
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
