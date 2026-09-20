package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/store"
)

const (
	zcodeAdapterKind       = "zcode.sqlite"
	commandCodeAdapterKind = "commandcode.provider-http"
)

type OutboxDiagnosticsProvider func(context.Context, time.Time) domain.OutboxDiagnostics
type ZCodeStorageDiagnosticsProvider func() domain.ZCodeStorageDiagnostics

type Server struct {
	store             *store.SnapshotStore
	version           string
	started           time.Time
	now               func() time.Time
	outboxDiagnostics       OutboxDiagnosticsProvider
	zcodeStorageDiagnostics ZCodeStorageDiagnosticsProvider
}

func New(store *store.SnapshotStore, version string, started time.Time) *Server {
	return &Server{
		store:   store,
		version: version,
		started: started,
		now:     time.Now,
	}
}

// SetOutboxDiagnosticsProvider wires optional local operational metadata. It is
// configured before the HTTP server begins serving requests; callers should not
// mutate the provider after Handler traffic starts.
func (s *Server) SetOutboxDiagnosticsProvider(provider OutboxDiagnosticsProvider) {
	s.outboxDiagnostics = provider
}


// SetZCodeStorageDiagnosticsProvider wires path-free source binding metadata.
// The provider may inspect configured source accessibility but must never put
// private filesystem paths into the returned diagnostics object.
func (s *Server) SetZCodeStorageDiagnosticsProvider(provider ZCodeStorageDiagnosticsProvider) {
	s.zcodeStorageDiagnostics = provider
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/state", s.handleState)
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/diagnostics", s.handleDiagnostics)
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

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	now := s.now().UTC()
	state := s.store.Get()
	payload := domain.DiagnosticsResponse{
		DiagnosticsVersion: domain.DiagnosticsVersion,
		StateSchemaVersion: domain.SchemaVersion,
		Role:               "agent",
		Version:            s.version,
		Time:               now,
		UptimeSeconds:      max(0, int64(now.Sub(s.started).Seconds())),
		Sources: domain.DiagnosticsSources{
			ZCode:       sourceDiagnostics(now, zcodeAdapterKind, state.ZCode.Health),
			CommandCode: sourceDiagnostics(now, commandCodeAdapterKind, state.CommandCode.Health),
		},
	}
	if s.zcodeStorageDiagnostics != nil {
		storage := s.zcodeStorageDiagnostics()
		payload.ZCodeStorage = &storage
	}
	if s.outboxDiagnostics != nil {
		outbox := s.outboxDiagnostics(r.Context(), now)
		payload.Outbox = &outbox
	}
	writeJSON(w, http.StatusOK, payload)
}

func sourceDiagnostics(now time.Time, adapterKind string, health domain.SourceHealth) domain.SourceDiagnostics {
	diagnostics := domain.SourceDiagnostics{
		Enabled:            health.Status != domain.SourceDisabled,
		AdapterKind:        adapterKind,
		Status:             health.Status,
		ObservedAgeSeconds: ageSeconds(now, health.ObservedAt),
		SchemaSupport:      schemaSupport(health),
	}
	if health.LastSuccessAt != nil && !health.LastSuccessAt.IsZero() {
		age := ageSeconds(now, *health.LastSuccessAt)
		diagnostics.LastSuccessAgeSeconds = &age
	}
	return diagnostics
}

func schemaSupport(health domain.SourceHealth) domain.SchemaSupport {
	if health.Status == domain.SourceDisabled {
		return domain.SchemaNotConfigured
	}
	if health.Message != nil && strings.HasPrefix(strings.TrimSpace(*health.Message), "Unsupported ") {
		return domain.SchemaUnsupported
	}
	if health.LastSuccessAt != nil && !health.LastSuccessAt.IsZero() {
		return domain.SchemaSupported
	}
	return domain.SchemaUnknown
}

func ageSeconds(now, observed time.Time) int64 {
	if observed.IsZero() {
		return 0
	}
	return max(0, int64(now.Sub(observed).Seconds()))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}
