//go:build linux

package serialport

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

var bauds = map[int]uint32{
	9600: unix.B9600, 19200: unix.B19200, 38400: unix.B38400,
	57600: unix.B57600, 115200: unix.B115200, 230400: unix.B230400,
}

// Open opens dev raw, 8N1, no flow control, at baud.
func Open(dev string, baud int) (Port, error) {
	speed, ok := bauds[baud]
	if !ok {
		return nil, fmt.Errorf("serialport: unsupported baud %d", baud)
	}
	fd, err := unix.Open(dev, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("serialport: open %s: %w", dev, err)
	}
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("serialport: %s is not a serial device: %w", dev, err)
	}
	// cfmakeraw, then 8N1 with the receiver on and modem lines ignored.
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON | unix.IXOFF | unix.IXANY
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB | unix.CSTOPB | unix.CRTSCTS | unix.CBAUD | unix.HUPCL
	t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL | speed
	t.Ispeed, t.Ospeed = speed, speed
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, t); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("serialport: configure %s: %w", dev, err)
	}
	// O_NONBLOCK keeps the fd on Go's poller, so deadlines and Close work.
	return os.NewFile(uintptr(fd), dev), nil
}
