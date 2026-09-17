package main

import (
	"flag"
	"fmt"

	"github.com/Eswink/ai-control-hud/agent/internal/collector/commandcode"
	"github.com/Eswink/ai-control-hud/agent/internal/platform/secretstore"
)

func prepareCommandCodeSecret(
	flags *flag.FlagSet,
	providerConfig string,
	secretPath string,
) (imported bool, err error) {
	if flagProvided(flags, "provider-config") {
		providerPath := absolute(providerConfig)
		if !fileExists(providerPath) {
			return false, fmt.Errorf("explicit CommandCode provider import file is unavailable: %s", providerPath)
		}
		provider, loadErr := commandcode.LoadProvider(providerPath, "")
		if loadErr != nil {
			return false, loadErr
		}
		if err := commandcode.ValidateProvider(provider, false); err != nil {
			return false, err
		}
		record := secretstore.Record{
			ProviderID: provider.ID,
			BaseURL:    provider.BaseURL,
			APIKey:     provider.APIKey,
		}
		if err := secretstore.Write(secretPath, record); err != nil {
			return false, fmt.Errorf("write platform SecretStore: %w", err)
		}
		return true, nil
	}

	record, readErr := secretstore.Read(secretPath)
	if readErr != nil {
		return false, fmt.Errorf(
			"CommandCode SecretStore is not configured; run `commandcode configure --api-key-file PATH` or pass --provider-config explicitly: %w",
			readErr,
		)
	}
	if err := commandcode.ValidateProvider(providerFromSecret(record), false); err != nil {
		return false, err
	}
	return false, nil
}
