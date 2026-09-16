//go:build !windows

package fileacl

import "errors"

var ErrUnsupported = errors.New("protected Windows file ACL is unsupported")

func Supported() bool { return false }

func Protect(path string) error {
	return ErrUnsupported
}
