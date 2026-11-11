//go:build linux && !android

package ble

import (
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// lePrivacy reports whether the controller at index has LE privacy on — that
// is, whether it advertises and connects from a resolvable private address
// that rotates, rather than its fixed public address. BlueZ does not expose
// this on D-Bus, so it is read from the kernel's Bluetooth management socket.
// Read Controller Information is one of the commands the kernel allows an
// unprivileged socket; it transmits nothing.
func lePrivacy(index uint16) (bool, error) {
	const (
		opReadInfo      = 0x0004
		evCmdComplete   = 0x0001
		evCmdStatus     = 0x0002
		settingPrivacy  = 0x00002000
		hdrLen          = 6
		hciDevNone      = 0xffff
		hciChanControl  = 3
		maxMgmtMessages = 64
	)
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.BTPROTO_HCI)
	if err != nil {
		return false, fmt.Errorf("mgmt socket: %w", err)
	}
	defer unix.Close(fd)
	if err := unix.Bind(fd, &unix.SockaddrHCI{Dev: hciDevNone, Channel: hciChanControl}); err != nil {
		return false, fmt.Errorf("mgmt bind: %w", err)
	}
	tv := unix.Timeval{Sec: 2}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return false, err
	}
	req := make([]byte, hdrLen)
	binary.LittleEndian.PutUint16(req[0:], opReadInfo)
	binary.LittleEndian.PutUint16(req[2:], index)
	if _, err := unix.Write(fd, req); err != nil {
		return false, fmt.Errorf("mgmt write: %w", err)
	}
	buf := make([]byte, 1024)
	for range maxMgmtMessages {
		n, err := unix.Read(fd, buf)
		if err != nil {
			return false, fmt.Errorf("mgmt read: %w", err)
		}
		if n < hdrLen+3 {
			continue
		}
		ev := binary.LittleEndian.Uint16(buf[0:])
		idx := binary.LittleEndian.Uint16(buf[2:])
		op := binary.LittleEndian.Uint16(buf[hdrLen:])
		status := buf[hdrLen+2]
		if idx != index || op != opReadInfo {
			continue
		}
		switch ev {
		case evCmdStatus:
			return false, fmt.Errorf("mgmt read info: status %d", status)
		case evCmdComplete:
			if status != 0 {
				return false, fmt.Errorf("mgmt read info: status %d", status)
			}
			// address 6, version 1, manufacturer 2, supported 4, current 4
			p := buf[hdrLen+3 : n]
			if len(p) < 17 {
				return false, errors.New("mgmt read info: short reply")
			}
			return binary.LittleEndian.Uint32(p[13:17])&settingPrivacy != 0, nil
		}
	}
	return false, errors.New("mgmt read info: no reply")
}
