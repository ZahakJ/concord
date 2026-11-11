//go:build !linux

package serialport

// Open is not implemented off Linux yet; links fall back to TCP where the
// hardware offers it (a Meshtastic node on Wi-Fi).
func Open(dev string, baud int) (Port, error) { return nil, ErrUnsupported }
