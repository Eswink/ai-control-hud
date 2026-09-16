//go:build !windows

package firewall

import "errors"

var ErrUnsupported = errors.New("Windows Firewall integration is unsupported")

func Supported() bool { return false }

func Install(ruleName, executable, listen string) error { return ErrUnsupported }
func Remove(ruleName string) error                      { return ErrUnsupported }
