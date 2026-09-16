//go:build !windows

package sourceaccess

import "errors"

var ErrUnsupported = errors.New("Windows service source ACL preparation is unsupported")

func Supported() bool { return false }

func EnsureServiceReadable(paths ...string) error {
	return ErrUnsupported
}
