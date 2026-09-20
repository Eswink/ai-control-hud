package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	"github.com/Eswink/ai-control-hud/agent/internal/collector/zcode"
	"github.com/Eswink/ai-control-hud/agent/internal/zcodepath"
)

func runZCodeCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("zcode command required: evidence")
	}
	switch args[0] {
	case "evidence":
		return runZCodeEvidence(args[1:])
	default:
		return errors.New("unknown zcode command")
	}
}

func runZCodeEvidence(args []string) error {
	if len(args) != 0 {
		return errors.New("zcode evidence does not accept positional arguments")
	}
	layout, err := zcodepath.Resolve()
	if err != nil {
		return err
	}
	collector := zcode.New(layout.RuntimeDB, layout.TaskIndexDB)
	collector.LogDir = layout.LogDir

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	evidence := collector.CollectCompatibilityEvidence(ctx)
	evidence.LayoutSource = sanitizeBindingLabel(layout.Source, "unknown")
	evidence.ProviderConfigCandidates = len(layout.ProviderConfigPaths)
	for _, path := range layout.ProviderConfigPaths {
		if !readableRegularFile(path) {
			continue
		}
		evidence.ProviderConfigsReadable++
		if parsed, entries := providerConfigEvidence(path); parsed {
			evidence.ProviderConfigsParsed++
			evidence.ProviderEntriesFound += entries
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(evidence)
}


const maxProviderEvidenceBytes = int64(1024 * 1024)

func providerConfigEvidence(path string) (parsed bool, entries int) {
	file, err := os.Open(path)
	if err != nil {
		return false, 0
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxProviderEvidenceBytes {
		return false, 0
	}
	data, err := io.ReadAll(io.LimitReader(file, maxProviderEvidenceBytes+1))
	if err != nil || int64(len(data)) > maxProviderEvidenceBytes {
		return false, 0
	}

	var root struct {
		Provider map[string]json.RawMessage `json:"provider"`
	}
	if zcodepath.DecodeJSONC(data, &root) != nil || root.Provider == nil {
		return false, 0
	}
	return true, len(root.Provider)
}
