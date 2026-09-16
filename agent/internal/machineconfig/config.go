package machineconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/Eswink/ai-control-hud/agent/internal/platform/fileacl"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/sourceaccess"
)

const schemaVersion = 1

type Config struct {
	SchemaVersion     int    `json:"schemaVersion"`
	Listen            string `json:"listen"`
	ZCodeRuntimeDB    string `json:"zcodeRuntimeDb"`
	ZCodeTaskIndexDB  string `json:"zcodeTaskIndexDb"`
	CommandCodeSecret string `json:"commandCodeSecret"`
}

func New(listen, runtimeDB, taskIndexDB, secretPath string) Config {
	return Config{
		SchemaVersion:     schemaVersion,
		Listen:            strings.TrimSpace(listen),
		ZCodeRuntimeDB:    cleanAbsolute(runtimeDB),
		ZCodeTaskIndexDB:  cleanAbsolute(taskIndexDB),
		CommandCodeSecret: cleanAbsolute(secretPath),
	}
}

func (c Config) Validate() error {
	if c.SchemaVersion != schemaVersion {
		return fmt.Errorf("unsupported machine config schema version %d", c.SchemaVersion)
	}
	listen := strings.TrimSpace(c.Listen)
	if listen == "" {
		return errors.New("machine config listen address is missing")
	}
	if _, portText, err := net.SplitHostPort(listen); err != nil {
		return errors.New("machine config listen address is invalid")
	} else if port, parseErr := strconv.Atoi(portText); parseErr != nil || port < 1 || port > 65535 {
		return errors.New("machine config listen port is invalid")
	}
	if c.ZCodeRuntimeDB == "" && c.ZCodeTaskIndexDB == "" {
		return errors.New("machine config has no ZCode database path")
	}
	if c.CommandCodeSecret == "" {
		return errors.New("machine config CommandCode secret path is missing")
	}
	for label, value := range map[string]string{
		"zcode runtime database": c.ZCodeRuntimeDB,
		"zcode task index":       c.ZCodeTaskIndexDB,
		"CommandCode secret":     c.CommandCodeSecret,
	} {
		if value != "" && !filepath.IsAbs(value) {
			return fmt.Errorf("machine config %s path must be absolute", label)
		}
	}
	return nil
}

func Load(path string) (Config, error) {
	var config Config
	data, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("read machine config: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return config, errors.New("machine config is invalid")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return config, errors.New("machine config contains trailing data")
	}
	if err := config.Validate(); err != nil {
		return config, err
	}
	return config, nil
}

func Save(path string, config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if sourceaccess.Supported() {
		if err := sourceaccess.EnsureServiceReadable(config.ZCodeRuntimeDB, config.ZCodeTaskIndexDB); err != nil {
			return fmt.Errorf("prepare ZCode service access: %w", err)
		}
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create machine config directory: %w", err)
	}
	if fileacl.Supported() {
		if err := fileacl.Protect(directory); err != nil {
			return fmt.Errorf("protect machine config directory: %w", err)
		}
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode machine config: %w", err)
	}
	data = append(data, '\n')
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return fmt.Errorf("write machine config: %w", err)
	}
	if fileacl.Supported() {
		if err := fileacl.Protect(temporary); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("protect temporary machine config: %w", err)
		}
	}
	backup := path + ".bak"
	_ = os.Remove(backup)
	hadExisting := false
	if _, statErr := os.Stat(path); statErr == nil {
		if err := os.Rename(path, backup); err != nil {
			_ = os.Remove(temporary)
			return fmt.Errorf("stage existing machine config: %w", err)
		}
		hadExisting = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = os.Remove(temporary)
		return fmt.Errorf("inspect existing machine config: %w", statErr)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		if hadExisting {
			_ = os.Rename(backup, path)
		}
		return fmt.Errorf("commit machine config: %w", err)
	}
	_ = os.Remove(backup)
	if fileacl.Supported() {
		if err := fileacl.Protect(path); err != nil {
			return fmt.Errorf("protect machine config: %w", err)
		}
	}
	return nil
}

func DefaultPath() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("AI_CONTROL_MACHINE_CONFIG")); configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return "", err
		}
		return filepath.Clean(absolute), nil
	}
	if runtime.GOOS == "windows" {
		programData := strings.TrimSpace(os.Getenv("ProgramData"))
		if programData == "" {
			return "", errors.New("ProgramData is unavailable")
		}
		return filepath.Join(programData, "AIControlHUD", "agent.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ai-control-hud", "agent.json"), nil
}

func DefaultSecretPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "commandcode.dpapi")
}

// DefaultHubSecretPath deliberately derives the remote-hub credential path
// from the machine config instead of adding a new field to schema v1. Existing
// agent.json files remain byte-for-byte compatible while the optional hub
// credential gains platform SecretStore protection.
func DefaultHubSecretPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "hub.dpapi")
}

func cleanAbsolute(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return filepath.Clean(value)
	}
	return filepath.Clean(absolute)
}
