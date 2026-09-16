package remote

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultAgentID          = "desktop-main"
	defaultRequestTimeout   = 4 * time.Second
	defaultSnapshotInterval = 5 * time.Second
	defaultHeartbeatInterval = 10 * time.Second
	defaultMaxBackoff       = 30 * time.Second
)

type Config struct {
	BaseURL           string
	AgentID           string
	Token             string
	RequestTimeout    time.Duration
	SnapshotInterval  time.Duration
	HeartbeatInterval time.Duration
	MaxBackoff        time.Duration
}

func FromEnvironment() (Config, bool, error) {
	baseURL := strings.TrimSpace(os.Getenv("AI_CONTROL_HUB_URL"))
	token := strings.TrimSpace(os.Getenv("AI_CONTROL_HUB_TOKEN"))
	if baseURL == "" && token == "" {
		return Config{}, false, nil
	}

	config := Config{
		BaseURL:           baseURL,
		AgentID:           strings.TrimSpace(os.Getenv("AI_CONTROL_HUB_AGENT_ID")),
		Token:             token,
		RequestTimeout:    defaultRequestTimeout,
		SnapshotInterval:  defaultSnapshotInterval,
		HeartbeatInterval: defaultHeartbeatInterval,
		MaxBackoff:        defaultMaxBackoff,
	}
	if config.AgentID == "" {
		config.AgentID = defaultAgentID
	}
	if err := config.Validate(); err != nil {
		return Config{}, false, err
	}
	return config, true, nil
}

func (c *Config) normalize() {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.AgentID = strings.TrimSpace(c.AgentID)
	c.Token = strings.TrimSpace(c.Token)
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = defaultRequestTimeout
	}
	if c.SnapshotInterval <= 0 {
		c.SnapshotInterval = defaultSnapshotInterval
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = defaultHeartbeatInterval
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = defaultMaxBackoff
	}
}

func (c Config) Validate() error {
	c.normalize()
	if c.BaseURL == "" {
		return errors.New("AI_CONTROL_HUB_URL is required when remote hub upload is enabled")
	}
	parsed, err := url.Parse(c.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("AI_CONTROL_HUB_URL must be an absolute http/https URL")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("AI_CONTROL_HUB_URL must not contain a query or fragment")
	}
	if c.AgentID == "" {
		return errors.New("AI_CONTROL_HUB_AGENT_ID must not be empty")
	}
	if c.Token == "" {
		return errors.New("AI_CONTROL_HUB_TOKEN is required when remote hub upload is enabled")
	}
	if c.RequestTimeout <= 0 || c.SnapshotInterval <= 0 || c.HeartbeatInterval <= 0 || c.MaxBackoff <= 0 {
		return errors.New("remote hub timing values must be positive")
	}
	return nil
}

func NewConfig(baseURL, agentID, token string) (Config, error) {
	config := Config{
		BaseURL:           baseURL,
		AgentID:           agentID,
		Token:             token,
		RequestTimeout:    defaultRequestTimeout,
		SnapshotInterval:  defaultSnapshotInterval,
		HeartbeatInterval: defaultHeartbeatInterval,
		MaxBackoff:        defaultMaxBackoff,
	}
	config.normalize()
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}
