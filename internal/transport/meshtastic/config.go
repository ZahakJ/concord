package meshtastic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Config says which node to talk to. It comes from <dataDir>/meshtastic.json,
// and CONCORD_MESHTASTIC overrides the device:
//
//	{"device": "/dev/ttyACM0"}             USB serial
//	{"device": "tcp://192.168.4.1:4403"}   a node on Wi-Fi (port optional)
//	{"device": ""}                         auto-detect a USB node
//
// channel is the node's channel index to send on (0 = primary); port is the
// Data.portnum Concord uses (default PRIVATE_APP, 256), which must match on
// every Concord node; hopLimit is the mesh rebroadcast budget (default 3).
type Config struct {
	Device   string `json:"device"`
	Channel  uint32 `json:"channel"`
	Port     uint32 `json:"port"`
	HopLimit uint32 `json:"hopLimit"`
	// Allow433 says the user is where 433 MHz (EU_433 and the other 433
	// regions) is license-exempt ISM — ITU Region 1 and parts of Region 3.
	// In the Americas 433 MHz is amateur-only, so it is refused by default.
	Allow433 bool `json:"allow433"`
}

// EnvDevice overrides Config.Device.
const EnvDevice = "CONCORD_MESHTASTIC"

// LoadConfig reads dataDir/meshtastic.json (absent is fine) and the
// environment.
func LoadConfig(dataDir string) Config {
	var c Config
	if dataDir != "" {
		if raw, err := os.ReadFile(filepath.Join(dataDir, "meshtastic.json")); err == nil {
			_ = json.Unmarshal(raw, &c)
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvDevice)); v != "" {
		c.Device = v
	}
	return c.withDefaults()
}

func (c Config) withDefaults() Config {
	if c.Port == 0 {
		c.Port = PortPrivateApp
	}
	if c.HopLimit == 0 || c.HopLimit > 7 {
		c.HopLimit = 3
	}
	if c.Channel > 7 {
		c.Channel = 0
	}
	return c
}

// tcpAddr reports whether device names a TCP node, and its host:port.
func tcpAddr(device string) (string, bool) {
	d := device
	switch {
	case strings.HasPrefix(d, "tcp://"):
		d = strings.TrimPrefix(d, "tcp://")
	case strings.HasPrefix(d, "tcp:"):
		d = strings.TrimPrefix(d, "tcp:")
	case strings.HasPrefix(d, "/"), d == "":
		return "", false
	}
	if !strings.Contains(d, ":") {
		d += ":4403"
	}
	return d, true
}

// usbVendors are the USB-serial bridges Meshtastic boards ship with:
// Espressif native USB (ESP32-S3: Heltec V3 USB-C on some, T-Beam Supreme),
// Silicon Labs CP210x (T-Beam, Heltec V2), WCH CH340/CH9102 (Heltec V3,
// T-Beam 1.2), Adafruit/RAK nRF52 (RAK4631, T-Echo), Raspberry Pi RP2040.
// FTDI (0403) is deliberately absent: that is the RockBLOCK, and one
// physical port must never be claimed by two links.
var usbVendors = []string{"303a", "10c4", "1a86", "239a", "2e8a"}
