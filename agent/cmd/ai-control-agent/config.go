package main

import (
	"errors"
	"flag"
	"fmt"

	"github.com/Eswink/ai-control-hud/agent/internal/machineconfig"
)

func runConfigCommand(args []string) error {
	if len(args) == 0 || args[0] != "get" {
		return errors.New("config command required: get")
	}
	flags := flag.NewFlagSet("config get", flag.ContinueOnError)
	defaultConfig, err := machineconfig.DefaultPath()
	if err != nil {
		return err
	}
	configPath := flags.String("config", defaultConfig, "machine configuration path")
	field := flags.String("field", "", "field: listen, zcode-runtime, zcode-task-index, command-code-secret")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if *field == "" {
		return errors.New("config get --field is required")
	}
	config, err := machineconfig.Load(absolute(*configPath))
	if err != nil {
		return err
	}
	var value string
	switch *field {
	case "listen":
		value = config.Listen
	case "zcode-runtime":
		value = config.ZCodeRuntimeDB
	case "zcode-task-index":
		value = config.ZCodeTaskIndexDB
	case "command-code-secret":
		value = config.CommandCodeSecret
	default:
		return fmt.Errorf("unknown config field %q", *field)
	}
	fmt.Println(value)
	return nil
}
