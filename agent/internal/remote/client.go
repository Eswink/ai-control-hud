package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/discovery"
	"github.com/Eswink/ai-control-hud/agent/internal/domain"
	"github.com/Eswink/ai-control-hud/agent/internal/events"
)

type Client struct {
	config Config
	http   *http.Client
	now    func() time.Time

	resolveMu       sync.Mutex
	resolvedBaseURL string
	discover        func(context.Context) (string, error)
}

type stateEnvelope struct {
	AgentID string          `json:"agentId"`
	SentAt  time.Time       `json:"sentAt"`
	State   domain.HudState `json:"state"`
}

type heartbeatEnvelope struct {
	AgentID      string    `json:"agentId"`
	SentAt       time.Time `json:"sentAt"`
	AgentVersion string    `json:"agentVersion,omitempty"`
}

type eventEnvelope struct {
	AgentID string         `json:"agentId"`
	SentAt  time.Time      `json:"sentAt"`
	Events  []events.Event `json:"events"`
}

func NewClient(config Config) (*Client, error) {
	config.normalize()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	client := &Client{
		config: config,
		http:   &http.Client{Timeout: config.RequestTimeout},
		now:    time.Now,
	}
	if !config.IsAutoDiscover() {
		client.resolvedBaseURL = config.BaseURL
	}
	client.discover = func(ctx context.Context) (string, error) {
		result, err := discovery.Discover(ctx, discovery.DefaultPort)
		if err != nil {
			return "", err
		}
		return result.BaseURL, nil
	}
	return client, nil
}

func (c *Client) ResolvedBaseURL() string {
	c.resolveMu.Lock()
	defer c.resolveMu.Unlock()
	return c.resolvedBaseURL
}

func (c *Client) UploadState(ctx context.Context, state domain.HudState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("refuse invalid state upload: %w", err)
	}
	return c.postJSON(ctx, "/api/v1/agent/state", stateEnvelope{
		AgentID: c.config.AgentID,
		SentAt:  c.now().UTC(),
		State:   state,
	})
}

func (c *Client) Heartbeat(ctx context.Context, version string) error {
	return c.postJSON(ctx, "/api/v1/agent/heartbeat", heartbeatEnvelope{
		AgentID:      c.config.AgentID,
		SentAt:       c.now().UTC(),
		AgentVersion: version,
	})
}

func (c *Client) UploadEvents(ctx context.Context, batch []events.Event) error {
	if len(batch) == 0 {
		return nil
	}
	if len(batch) > 100 {
		return fmt.Errorf("event upload batch exceeds 100 events")
	}
	for i, event := range batch {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("refuse invalid event upload at index %d: %w", i, err)
		}
	}
	return c.postJSON(ctx, "/api/v1/agent/events", eventEnvelope{
		AgentID: c.config.AgentID,
		SentAt:  c.now().UTC(),
		Events:  batch,
	})
}

func (c *Client) postJSON(ctx context.Context, path string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode hub request: %w", err)
	}
	baseURL, err := c.resolveBaseURL(ctx)
	if err != nil {
		return fmt.Errorf("resolve hub address: %w", err)
	}
	firstErr := c.postJSONAt(ctx, baseURL, path, payload)
	if firstErr == nil || !c.config.IsAutoDiscover() {
		return firstErr
	}

	c.invalidateResolvedBaseURL(baseURL)
	rediscovered, discoverErr := c.resolveBaseURL(ctx)
	if discoverErr != nil {
		return fmt.Errorf("%w; hub rediscovery failed: %v", firstErr, discoverErr)
	}
	if rediscovered == baseURL {
		return firstErr
	}
	if err := c.postJSONAt(ctx, rediscovered, path, payload); err != nil {
		return fmt.Errorf("hub request failed after rediscovery from %s to %s: %w", baseURL, rediscovered, err)
	}
	return nil
}

func (c *Client) postJSONAt(ctx context.Context, baseURL, path string, payload []byte) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+path,
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("create hub request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+c.config.Token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "ai-control-agent")

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("hub request failed: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("hub %s returned HTTP %d", path, response.StatusCode)
	}
	return nil
}

func (c *Client) resolveBaseURL(ctx context.Context) (string, error) {
	if !c.config.IsAutoDiscover() {
		return c.config.BaseURL, nil
	}
	c.resolveMu.Lock()
	defer c.resolveMu.Unlock()
	if c.resolvedBaseURL != "" {
		return c.resolvedBaseURL, nil
	}
	discoverCtx, cancel := context.WithTimeout(ctx, minDuration(c.config.RequestTimeout, 2*time.Second))
	defer cancel()
	baseURL, err := c.discover(discoverCtx)
	if err != nil {
		return "", err
	}
	c.resolvedBaseURL = baseURL
	return baseURL, nil
}

func (c *Client) invalidateResolvedBaseURL(expected string) {
	c.resolveMu.Lock()
	defer c.resolveMu.Unlock()
	if c.resolvedBaseURL == expected {
		c.resolvedBaseURL = ""
	}
}

func minDuration(left, right time.Duration) time.Duration {
	if left <= 0 {
		return right
	}
	if right <= 0 || left < right {
		return left
	}
	return right
}
