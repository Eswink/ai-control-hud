//go:build linux || darwin

package discovery

import (
	"net"

	"golang.org/x/sys/unix"
)

func enableBroadcast(conn *net.UDPConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		controlErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return controlErr
}
