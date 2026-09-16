//go:build windows

package firewall

import (
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

var ErrUnsupported = errors.New("Windows Firewall integration is unsupported")

func Supported() bool { return true }

func Install(ruleName, executable, listen string) error {
	port, err := listenPort(listen)
	if err != nil {
		return err
	}
	if strings.TrimSpace(ruleName) == "" || strings.TrimSpace(executable) == "" {
		return errors.New("Windows Firewall rule name or executable is missing")
	}
	_ = run("advfirewall", "firewall", "delete", "rule", "name="+ruleName)
	if err := run(
		"advfirewall", "firewall", "add", "rule",
		"name="+ruleName,
		"dir=in",
		"action=allow",
		"program="+executable,
		"protocol=TCP",
		"localport="+strconv.Itoa(port),
		"profile=private,domain",
		"enable=yes",
	); err != nil {
		return fmt.Errorf("configure Windows Firewall rule: %w", err)
	}
	return nil
}

func Remove(ruleName string) error {
	if strings.TrimSpace(ruleName) == "" {
		return errors.New("Windows Firewall rule name is missing")
	}
	if err := run("advfirewall", "firewall", "delete", "rule", "name="+ruleName); err != nil {
		return fmt.Errorf("remove Windows Firewall rule: %w", err)
	}
	return nil
}

func listenPort(listen string) (int, error) {
	_, text, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return 0, errors.New("invalid listen address for Windows Firewall")
	}
	port, err := strconv.Atoi(text)
	if err != nil || port < 1 || port > 65535 {
		return 0, errors.New("invalid listen port for Windows Firewall")
	}
	return port, nil
}

func run(args ...string) error {
	command := exec.Command("netsh.exe", args...)
	command.Stdout = nil
	command.Stderr = nil
	return command.Run()
}
