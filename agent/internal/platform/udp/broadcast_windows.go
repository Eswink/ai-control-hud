//go:build windows

package udp

import (
	"net"

	"golang.org/x/sys/windows"
)

func EnableBroadcast(conn *net.UDPConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		controlErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_BROADCAST, 1)
	}); err != nil {
		return err
	}
	return controlErr
}
