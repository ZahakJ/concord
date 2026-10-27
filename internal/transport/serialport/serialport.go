// Package serialport opens a USB serial device (a Meshtastic node, a RockBLOCK
// modem) in raw mode at a fixed baud rate, and finds candidate devices by USB
// vendor ID without opening them.
//
// Nothing here is called until the user enables a link: Find only reads
// sysfs, and Open is the first time a device is touched.
package serialport

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Port is an open serial device. Reads honour SetReadDeadline, and Close
// unblocks a pending Read.
type Port interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
	SetReadDeadline(t time.Time) error
}

// ErrUnsupported is returned by Open on platforms without a serial driver.
var ErrUnsupported = errors.New("serialport: serial devices are not supported on this platform")

// Find lists /dev/ttyUSB* and /dev/ttyACM* devices whose USB vendor ID (from
// sysfs) is in vendors, sorted. An empty vendors matches any. It never opens a
// device. On platforms without sysfs it returns nothing.
func Find(vendors ...string) []string {
	var out []string
	for _, pat := range []string{"/dev/ttyUSB*", "/dev/ttyACM*"} {
		matches, _ := filepath.Glob(pat)
		for _, dev := range matches {
			if len(vendors) == 0 || hasVendor(VendorID(dev), vendors) {
				out = append(out, dev)
			}
		}
	}
	sort.Strings(out)
	return out
}

// VendorID is the 4-hex-digit USB vendor ID behind a tty device, or "" when
// sysfs does not say.
func VendorID(dev string) string {
	// /sys/class/tty/ttyUSB0/device is the USB interface; idVendor lives on
	// the USB device one or two levels up depending on the driver.
	base := filepath.Join("/sys/class/tty", filepath.Base(dev), "device")
	for _, rel := range []string{"idVendor", "../idVendor", "../../idVendor"} {
		raw, err := os.ReadFile(filepath.Join(base, rel))
		if err == nil {
			return strings.ToLower(strings.TrimSpace(string(raw)))
		}
	}
	return ""
}

func hasVendor(v string, vendors []string) bool {
	for _, w := range vendors {
		if v != "" && strings.EqualFold(v, w) {
			return true
		}
	}
	return false
}

// Exists reports whether a device path is present, without opening it.
func Exists(dev string) bool {
	_, err := os.Stat(dev)
	return err == nil
}
