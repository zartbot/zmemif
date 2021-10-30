package zmemif

import (
	"os"
	"syscall"
)

func min16(a uint16, b uint16) uint16 {
	if a < b {
		return a
	}
	return b
}

func min8(a uint8, b uint8) uint8 {
	if a < b {
		return a
	}
	return b
}

// eventFd returns an eventfd (SYS_EVENTFD2)
func eventFd() (efd int, err error) {
	u_efd, _, errno := syscall.Syscall(syscall.SYS_EVENTFD2, uintptr(0), uintptr(efd_nonblock), 0)
	if errno != 0 {
		return -1, os.NewSyscallError("eventfd", errno)
	}
	return int(u_efd), nil
}
