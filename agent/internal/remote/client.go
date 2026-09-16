package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/domain"
)

type Client struct {
	config Config
	http   *http.Client
	now    func() time.Time
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

func NewClient(config Config) (*Client, error) {
	config.normalize()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Client{
		config: config,
		http:   &http.Client{Timeout: config.RequestTimeout},
		now:    time.Now,
	}, nil
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

func (c *Client) postJSON(ctx context.Context, path string, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode hub request: %w", err)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.config.BaseURL+path,
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
