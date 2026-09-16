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

type ruleProfile struct {
	Suffix  string
	Profile string
}

var serviceProfiles = []ruleProfile{
	{Suffix: "Private", Profile: "private"},
	{Suffix: "Domain", Profile: "domain"},
}

func Install(ruleName, executable, listen string) error {
	port, err := listenPort(listen)
	if err != nil {
		return err
	}
	if strings.TrimSpace(ruleName) == "" || strings.TrimSpace(executable) == "" {
		return errors.New("Windows Firewall rule name or executable is missing")
	}
	_ = Remove(ruleName)
	installed := make([]string, 0, len(serviceProfiles))
	for _, profile := range serviceProfiles {
		name := profileRuleName(ruleName, profile.Suffix)
		if err := run(
			"advfirewall", "firewall", "add", "rule",
			"name="+name,
			"dir=in",
			"action=allow",
			"program="+executable,
			"protocol=TCP",
			"localport="+strconv.Itoa(port),
			"profile="+profile.Profile,
			"enable=yes",
		); err != nil {
			for _, added := range installed {
				_ = deleteRule(added)
			}
			return fmt.Errorf("configure Windows Firewall %s rule: %w", profile.Suffix, err)
		}
		installed = append(installed, name)
	}
	return nil
}

func Remove(ruleName string) error {
	if strings.TrimSpace(ruleName) == "" {
		return errors.New("Windows Firewall rule name is missing")
	}
	var failures []error
	for _, profile := range serviceProfiles {
		name := profileRuleName(ruleName, profile.Suffix)
		if err := deleteRule(name); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", profile.Suffix, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("remove Windows Firewall rules: %w", errors.Join(failures...))
	}
	return nil
}

func profileRuleName(base, suffix string) string {
	return fmt.Sprintf("%s (%s)", strings.TrimSpace(base), suffix)
}

func deleteRule(name string) error {
	return run("advfirewall", "firewall", "delete", "rule", "name="+name)
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
