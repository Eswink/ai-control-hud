package secretstore

import (
	"errors"
	"net/url"
	"strings"
)

var ErrUnsupported = errors.New("platform secret store is unsupported")

type Record struct {
	ProviderID string `json:"providerId"`
	BaseURL    string `json:"baseUrl"`
	APIKey     string `json:"apiKey"`
}

func (r Record) Validate() error {
	if strings.TrimSpace(r.ProviderID) == "" {
		return errors.New("secret provider id is missing")
	}
	if strings.TrimSpace(r.APIKey) == "" {
		return errors.New("secret API key is missing")
	}
	parsed, err := url.Parse(strings.TrimSpace(r.BaseURL))
	if err != nil || parsed.Scheme != "https" || strings.TrimSpace(parsed.Hostname()) == "" {
		return errors.New("secret provider base URL is invalid")
	}
	return nil
}

// HubRecord is a protected remote-hub credential. It is stored separately from
// CommandCode credentials so the validation rules and rotation lifecycle stay
// independent. Private-overlay deployments may intentionally use HTTP; public
// exposure remains out of scope and should use HTTPS.
type HubRecord struct {
	AgentID string `json:"agentId"`
	BaseURL string `json:"baseUrl"`
	Token   string `json:"token"`
}

func (r HubRecord) Validate() error {
	if strings.TrimSpace(r.AgentID) == "" {
		return errors.New("hub agent id is missing")
	}
	if strings.TrimSpace(r.Token) == "" {
		return errors.New("hub token is missing")
	}
	parsed, err := url.Parse(strings.TrimSpace(r.BaseURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.TrimSpace(parsed.Host) == "" {
		return errors.New("hub base URL must be an absolute http/https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("hub base URL must not contain user info, query, or fragment")
	}
	return nil
}
