package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// NetConfig holds non-secret connection settings, stored in plaintext (unlike
// the identity/messages, which are encrypted) so they can be read and edited
// *before* the user unlocks — e.g. to point the app at a rendezvous node on the
// login screen. This is what makes "play with a friend over the internet"
// setup a paste-one-address step instead of an env-var chore.
type NetConfig struct {
	// Bootstrap is a list of rendezvous/relay multiaddrs used for internet-wide
	// discovery. Empty = LAN-only (mDNS).
	Bootstrap []string `json:"bootstrap"`
}

func netConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "netconfig.json")
}

// LoadNetConfig reads the connection config, returning an empty one if absent.
func LoadNetConfig(dataDir string) NetConfig {
	var c NetConfig
	if b, err := os.ReadFile(netConfigPath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

// SaveNetConfig persists the connection config.
func SaveNetConfig(dataDir string, c NetConfig) error {
	// Normalize: trim blanks, drop empties.
	var clean []string
	for _, a := range c.Bootstrap {
		if a = strings.TrimSpace(a); a != "" {
			clean = append(clean, a)
		}
	}
	c.Bootstrap = clean
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(netConfigPath(dataDir), b, 0o600)
}
