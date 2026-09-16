package commandcode

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Provider struct {
	ID      string
	Name    string
	Kind    string
	Enabled bool
	BaseURL string
	APIKey  string
	Models  []string
}

type rawProvider struct {
	Name                 string                     `json:"name"`
	Kind                 string                     `json:"kind"`
	Enabled              *bool                      `json:"enabled"`
	SystemDisabledReason any                        `json:"systemDisabledReason"`
	BaseURL              string                     `json:"baseURL"`
	Options              rawProviderOptions         `json:"options"`
	Models               map[string]json.RawMessage `json:"models"`
}

type rawProviderOptions struct {
	BaseURL string `json:"baseURL"`
	APIKey  string `json:"apiKey"`
}

type rawConfig struct {
	Provider map[string]rawProvider `json:"provider"`
}

func DefaultConfigPaths() []string {
	if configured := strings.TrimSpace(os.Getenv("HUD_ZCODE_CONFIG")); configured != "" {
		return []string{expandHome(configured)}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".zcode", "v2", "config.json"),
		filepath.Join(home, ".zcode", "cli", "config.json"),
	}
}

func LoadProvider(path, explicitProviderID string) (*Provider, error) {
	data, err := os.ReadFile(expandHome(path))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, errors.New("ZCode provider config could not be read")
	}

	var root rawConfig
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, errors.New("ZCode provider config could not be read")
	}
	if root.Provider == nil {
		return nil, nil
	}

	if explicitProviderID != "" {
		raw, ok := root.Provider[explicitProviderID]
		if !ok {
			return nil, nil
		}
		provider := convertProvider(explicitProviderID, raw)
		return &provider, nil
	}

	for id, raw := range root.Provider {
		provider := convertProvider(id, raw)
		if provider.Enabled && provider.IsOfficialCommandCode() {
			return &provider, nil
		}
	}
	return nil, nil
}

func FindProviderAcrossConfigs(paths []string, explicitProviderID string) (string, *Provider, error) {
	for _, path := range paths {
		provider, err := LoadProvider(path, explicitProviderID)
		if err != nil {
			return "", nil, err
		}
		if provider != nil {
			return path, provider, nil
		}
	}
	return "", nil, nil
}

func (p Provider) Host() string {
	if p.BaseURL == "" {
		return ""
	}
	parsed, err := url.Parse(p.BaseURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func (p Provider) IsOfficialCommandCode() bool {
	return p.Host() == "api.commandcode.ai"
}

func convertProvider(id string, raw rawProvider) Provider {
	enabled := true
	if raw.Enabled != nil {
		enabled = *raw.Enabled
	}
	if raw.SystemDisabledReason != nil {
		switch value := raw.SystemDisabledReason.(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				enabled = false
			}
		default:
			enabled = false
		}
	}
	baseURL := strings.TrimSpace(raw.Options.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(raw.BaseURL)
	}
	models := make([]string, 0, len(raw.Models))
	for model := range raw.Models {
		if model = strings.TrimSpace(model); model != "" {
			models = append(models, model)
		}
	}
	return Provider{
		ID:      truncate(id, 200),
		Name:    truncate(strings.TrimSpace(raw.Name), 200),
		Kind:    truncate(strings.TrimSpace(raw.Kind), 64),
		Enabled: enabled,
		BaseURL: truncate(baseURL, 500),
		APIKey:  truncate(strings.TrimSpace(raw.Options.APIKey), 4096),
		Models:  models,
	}
}

func expandHome(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if len(path) > 1 && (path[1] == '/' || path[1] == '\\') {
		return filepath.Join(home, path[2:])
	}
	return path
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func ValidateProvider(provider *Provider, allowNonOfficial bool) error {
	if provider == nil {
		return errors.New("CommandCode provider not found in ZCode")
	}
	if provider.APIKey == "" {
		return errors.New("CommandCode provider API key missing in ZCode")
	}
	if !provider.IsOfficialCommandCode() && !allowNonOfficial {
		return errors.New("CommandCode provider endpoint is not verified")
	}
	if !provider.Enabled {
		return fmt.Errorf("CommandCode provider %q is disabled", provider.ID)
	}
	return nil
}
