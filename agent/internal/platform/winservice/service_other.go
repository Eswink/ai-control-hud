//go:build !windows

package winservice

import (
	"context"
	"errors"
)

var ErrUnsupported = errors.New("Windows service lifecycle is unsupported")

type Runner func(context.Context) error

type Info struct {
	Installed bool
	State     string
	ProcessID uint32
}

func Install(name, displayName, description, executable, configPath string) error { return ErrUnsupported }
func Start(name string) error                                                    { return ErrUnsupported }
func Stop(name string) error                                                     { return ErrUnsupported }
func Restart(name string) error                                                  { return ErrUnsupported }
func Remove(name string) error                                                   { return ErrUnsupported }
func Status(name string) (Info, error)                                           { return Info{}, ErrUnsupported }
func Run(name string, runner Runner) error                                       { return ErrUnsupported }
