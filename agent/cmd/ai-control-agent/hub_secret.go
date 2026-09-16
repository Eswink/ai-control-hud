package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Eswink/ai-control-hud/agent/internal/machineconfig"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
)

const defaultHubAgentID = "desktop-main"

type hubSecretFlags struct {
	url       *string
	agentID   *string
	tokenFile *string
}

func addHubSecretFlags(flags *flag.FlagSet) hubSecretFlags {
	return hubSecretFlags{
		url:       flags.String("hub-url", "", "remote hub URL stored in platform SecretStore"),
		agentID:   flags.String("hub-agent-id", defaultHubAgentID, "remote hub agent ID stored in platform SecretStore"),
		tokenFile: flags.String("hub-token-file", "", "one-time plaintext file containing the remote hub bearer token"),
	}
}

func configureProtectedHubSecret(
	flags *flag.FlagSet,
	configPath string,
	options hubSecretFlags,
) (path string, configured bool, importedToken bool, err error) {
	path = machineconfig.DefaultHubSecretPath(configPath)
	provided := flagProvided(flags, "hub-url") ||
		flagProvided(flags, "hub-agent-id") ||
		flagProvided(flags, "hub-token-file")

	existing, exists, readErr := readExistingHubSecret(path)
	if readErr != nil {
		return path, false, false, readErr
	}
	if !provided {
		return path, exists, false, nil
	}

	record := existing
	if !exists {
		record.AgentID = defaultHubAgentID
	}
	if flagProvided(flags, "hub-url") {
		record.BaseURL = strings.TrimSpace(*options.url)
	}
	if flagProvided(flags, "hub-agent-id") {
		record.AgentID = strings.TrimSpace(*options.agentID)
	}
	if flagProvided(flags, "hub-token-file") {
		token, tokenErr := readTokenFile(*options.tokenFile)
		if tokenErr != nil {
			return path, false, false, tokenErr
		}
		record.Token = token
		importedToken = true
	}

	if !exists && !flagProvided(flags, "hub-url") {
		return path, false, false, errors.New("--hub-url is required when creating the protected hub credential")
	}
	if !exists && !flagProvided(flags, "hub-token-file") {
		return path, false, false, errors.New("--hub-token-file is required when creating the protected hub credential")
	}
	if err := record.Validate(); err != nil {
		return path, false, false, err
	}
	if err := secretstore.WriteHub(path, record); err != nil {
		return path, false, false, fmt.Errorf("write protected hub credential: %w", err)
	}
	return path, true, importedToken, nil
}

func readExistingHubSecret(path string) (secretstore.HubRecord, bool, error) {
	var zero secretstore.HubRecord
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, fmt.Errorf("inspect protected hub credential: %w", err)
	}
	if info.IsDir() {
		return zero, false, errors.New("protected hub credential path is a directory")
	}
	record, err := secretstore.ReadHub(path)
	if err != nil {
		return zero, false, fmt.Errorf("read protected hub credential: %w", err)
	}
	return record, true, nil
}

func readTokenFile(path string) (string, error) {
	resolved := absolute(path)
	if resolved == "" {
		return "", errors.New("hub token file path is required")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect hub token file: %w", err)
	}
	if info.IsDir() {
		return "", errors.New("hub token file path is a directory")
	}
	if info.Size() > 16*1024 {
		return "", errors.New("hub token file is unexpectedly large")
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", fmt.Errorf("read hub token file: %w", err)
	}
	defer zeroBytes(data)
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("hub token file is empty")
	}
	if strings.ContainsAny(token, "\r\n") {
		return "", errors.New("hub token must be a single line")
	}
	return token, nil
}

func zeroBytes(data []byte) {
	for i := range data {
		data[i] = 0
	}
}
