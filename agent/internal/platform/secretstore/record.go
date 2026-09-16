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
