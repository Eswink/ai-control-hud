package main

import (
	"context"
	"encoding/json"
	"errors"
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
		if readableRegularFile(path) {
			evidence.ProviderConfigsReadable++
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(evidence)
}
