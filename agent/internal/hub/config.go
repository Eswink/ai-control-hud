package hub

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultHTTPPort            = 8787
	DefaultDiscoveryPort       = 8788
	DefaultAgentID             = "desktop-main"
	DefaultHubID               = "central-hub"
	DefaultStaleAfter          = 45 * time.Second
	DefaultEventRetentionDays  = 90
	DefaultEventRetentionMin   = 10_000
	DefaultEventRetentionMax   = 100_000
	DefaultMaintenanceInterval = 6 * time.Hour
)

type Config struct {
	DatabasePath        string
	PrimaryAgentID      string
	AgentToken          string
	StaleAfter          time.Duration
	HubID               string
	DiscoveryEnabled    bool
	DiscoveryPort       int
	HTTPScheme          string
	HTTPPort            int
	EventRetentionDays  int
	EventRetentionMin   int
	EventRetentionMax   int
	MaintenanceInterval time.Duration
}

func FromEnvironment() (Config, error) {
	config := Config{
		DatabasePath:        strings.TrimSpace(os.Getenv("HUD_HUB_DB")),
		PrimaryAgentID:      strings.TrimSpace(os.Getenv("HUD_HUB_AGENT_ID")),
		AgentToken:          strings.TrimSpace(os.Getenv("HUD_HUB_AGENT_TOKEN")),
		HubID:               strings.TrimSpace(os.Getenv("HUD_HUB_ID")),
		HTTPScheme:          strings.ToLower(strings.TrimSpace(os.Getenv("HUD_HUB_HTTP_SCHEME"))),
		StaleAfter:          DefaultStaleAfter,
		DiscoveryPort:       DefaultDiscoveryPort,
		HTTPPort:            DefaultHTTPPort,
		DiscoveryEnabled:    false,
		EventRetentionDays:  DefaultEventRetentionDays,
		EventRetentionMin:   DefaultEventRetentionMin,
		EventRetentionMax:   DefaultEventRetentionMax,
		MaintenanceInterval: DefaultMaintenanceInterval,
	}
	if config.DatabasePath == "" {
		config.DatabasePath = ".local/hub.sqlite3"
	}
	if config.PrimaryAgentID == "" {
		config.PrimaryAgentID = DefaultAgentID
	}
	if config.HubID == "" {
		config.HubID = DefaultHubID
	}
	if config.HTTPScheme == "" {
		config.HTTPScheme = "http"
	}

	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_STALE_AFTER_SECONDS")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_STALE_AFTER_SECONDS must be an integer: %w", err)
		}
		config.StaleAfter = time.Duration(seconds) * time.Second
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_DISCOVERY_ENABLED")); raw != "" {
		value, err := parseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_DISCOVERY_ENABLED: %w", err)
		}
		config.DiscoveryEnabled = value
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_DISCOVERY_PORT")); raw != "" {
		port, err := parsePort(raw)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_DISCOVERY_PORT: %w", err)
		}
		config.DiscoveryPort = port
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_HTTP_PORT")); raw != "" {
		port, err := parsePort(raw)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_HTTP_PORT: %w", err)
		}
		config.HTTPPort = port
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_EVENT_RETENTION_DAYS")); raw != "" {
		value, err := parseIntegerAtLeast(raw, 1)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_EVENT_RETENTION_DAYS: %w", err)
		}
		config.EventRetentionDays = value
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_EVENT_RETENTION_MIN")); raw != "" {
		value, err := parseIntegerAtLeast(raw, 0)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_EVENT_RETENTION_MIN: %w", err)
		}
		config.EventRetentionMin = value
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_EVENT_RETENTION_MAX")); raw != "" {
		value, err := parseIntegerAtLeast(raw, 1)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_EVENT_RETENTION_MAX: %w", err)
		}
		config.EventRetentionMax = value
	}
	if raw := strings.TrimSpace(os.Getenv("HUD_HUB_MAINTENANCE_INTERVAL_HOURS")); raw != "" {
		hours, err := parseIntegerAtLeast(raw, 1)
		if err != nil {
			return Config{}, fmt.Errorf("HUD_HUB_MAINTENANCE_INTERVAL_HOURS: %w", err)
		}
		config.MaintenanceInterval = time.Duration(hours) * time.Hour
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabasePath) == "" {
		return errors.New("HUD_HUB_DB must not be empty")
	}
	if strings.TrimSpace(c.PrimaryAgentID) == "" {
		return errors.New("HUD_HUB_AGENT_ID must not be empty")
	}
	if strings.TrimSpace(c.AgentToken) == "" {
		return errors.New("HUD_HUB_AGENT_TOKEN is required")
	}
	if c.StaleAfter < 5*time.Second {
		return errors.New("HUD_HUB_STALE_AFTER_SECONDS must be at least 5")
	}
	if strings.TrimSpace(c.HubID) == "" {
		return errors.New("HUD_HUB_ID must not be empty")
	}
	if c.HTTPScheme != "http" && c.HTTPScheme != "https" {
		return errors.New("HUD_HUB_HTTP_SCHEME must be http or https")
	}
	if c.DiscoveryPort < 1 || c.DiscoveryPort > 65535 {
		return errors.New("HUD_HUB_DISCOVERY_PORT must be within 1..65535")
	}
	if c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return errors.New("HUD_HUB_HTTP_PORT must be within 1..65535")
	}
	retentionDays, retentionMin, retentionMax, maintenanceInterval := c.effectiveRetention()
	if retentionDays < 1 {
		return errors.New("HUD_HUB_EVENT_RETENTION_DAYS must be at least 1")
	}
	if retentionMin < 0 {
		return errors.New("HUD_HUB_EVENT_RETENTION_MIN must be non-negative")
	}
	if retentionMax < 1 {
		return errors.New("HUD_HUB_EVENT_RETENTION_MAX must be at least 1")
	}
	if retentionMax < retentionMin {
		return errors.New("HUD_HUB_EVENT_RETENTION_MAX must be greater than or equal to HUD_HUB_EVENT_RETENTION_MIN")
	}
	if maintenanceInterval < time.Minute {
		return errors.New("Hub maintenance interval must be at least 1 minute")
	}
	return nil
}

// effectiveRetention keeps older programmatic Config literals source-compatible:
// when every new retention field is left at its zero value, use the production
// defaults. Environment-derived Config values are populated explicitly, so an
// operator can still set HUD_HUB_EVENT_RETENTION_MIN=0 intentionally.
func (c Config) effectiveRetention() (days, minimum, maximum int, interval time.Duration) {
	if c.EventRetentionDays == 0 && c.EventRetentionMin == 0 && c.EventRetentionMax == 0 && c.MaintenanceInterval == 0 {
		return DefaultEventRetentionDays, DefaultEventRetentionMin, DefaultEventRetentionMax, DefaultMaintenanceInterval
	}
	return c.EventRetentionDays, c.EventRetentionMin, c.EventRetentionMax, c.MaintenanceInterval
}

func parseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, errors.New("must be a boolean")
	}
}

func parsePort(raw string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, errors.New("must be an integer")
	}
	if port < 1 || port > 65535 {
		return 0, errors.New("must be within 1..65535")
	}
	return port, nil
}

func parseIntegerAtLeast(raw string, minimum int) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, errors.New("must be an integer")
	}
	if value < minimum {
		return 0, fmt.Errorf("must be at least %d", minimum)
	}
	return value, nil
}
