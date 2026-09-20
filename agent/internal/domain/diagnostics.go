package domain

import "time"

const DiagnosticsVersion = 1

type SchemaSupport string

const (
	SchemaSupported     SchemaSupport = "supported"
	SchemaUnsupported   SchemaSupport = "unsupported"
	SchemaUnknown       SchemaSupport = "unknown"
	SchemaNotConfigured SchemaSupport = "not-configured"
)

type SourceDiagnostics struct {
	Enabled               bool          `json:"enabled"`
	AdapterKind           string        `json:"adapterKind"`
	Status                SourceStatus  `json:"status"`
	ObservedAgeSeconds    int64         `json:"observedAgeSeconds"`
	LastSuccessAgeSeconds *int64        `json:"lastSuccessAgeSeconds"`
	SchemaSupport         SchemaSupport `json:"schemaSupport"`
}

type DiagnosticsSources struct {
	ZCode       SourceDiagnostics `json:"zcode"`
	CommandCode SourceDiagnostics `json:"commandCode"`
}

type ZCodeStorageDiagnostics struct {
	BindingMode             string `json:"bindingMode"`
	LayoutSource            string `json:"layoutSource"`
	RuntimeDatabaseReadable bool   `json:"runtimeDatabaseReadable"`
	TaskIndexReadable       bool   `json:"taskIndexReadable"`
	TurnLogReadable         bool   `json:"turnLogReadable"`
	RefreshRecommended      bool   `json:"refreshRecommended"`
}

type OutboxDiagnostics struct {
	Status                  string `json:"status"`
	PendingEvents           int    `json:"pendingEvents"`
	TaskBaselineRows        int    `json:"taskBaselineRows"`
	CompactedTaskRows       int    `json:"compactedTaskRows"`
	OldestPendingAgeSeconds *int64 `json:"oldestPendingAgeSeconds"`
	ReusableBytes           int64  `json:"reusableBytes"`
}

type DiagnosticsResponse struct {
	DiagnosticsVersion int                 `json:"diagnosticsVersion"`
	StateSchemaVersion int                 `json:"stateSchemaVersion"`
	Role               string              `json:"role"`
	Version            string              `json:"version"`
	Time               time.Time           `json:"time"`
	UptimeSeconds      int64               `json:"uptimeSeconds"`
	Sources            DiagnosticsSources         `json:"sources"`
	ZCodeStorage       *ZCodeStorageDiagnostics   `json:"zcodeStorage,omitempty"`
	Outbox             *OutboxDiagnostics         `json:"outbox,omitempty"`
}
