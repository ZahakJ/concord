package app

import (
	"encoding/json"
	"fmt"
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

	// PublicDHT opts into bootstrapping off the public IPFS DHT nodes as well as
	// (or instead of) the rendezvous above. It is the only fallback that works
	// between two peers who have never met and have no server of their own, and
	// it is the only one with a privacy cost: this node's peer ID and addresses
	// become visible on a public network. Off unless the user asks for it.
	PublicDHT bool `json:"publicDHT"`

	// ListenPort pins the port the node listens on so it can be forwarded on
	// the user's router — the one setting that buys a direct connection with no
	// relay and no rendezvous in the path. 0 (the default) takes a fresh
	// ephemeral port every launch, which no forwarding rule can follow.
	ListenPort int `json:"listenPort,omitempty"`
}

// validListenPort reports whether p is a port a user can pin. 0 means "off".
// Ports below 1024 need privileges the app doesn't have on Linux or macOS, so
// they are refused up front rather than at the bind that fails on next launch.
func validListenPort(p int) bool { return p == 0 || (p >= 1024 && p <= 65535) }

func netConfigPath(dataDir string) string {
	return filepath.Join(dataDir, "netconfig.json")
}

// LoadNetConfig reads the connection config, returning an empty one if absent.
func LoadNetConfig(dataDir string) NetConfig {
	var c NetConfig
	if b, err := os.ReadFile(netConfigPath(dataDir)); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	// SaveListenPort refuses a bad port, but this file is plaintext and meant to
	// be hand-editable, so one can still arrive from an editor, a sync conflict
	// or a truncated write — and it does not merely get ignored downstream: a
	// negative port builds a multiaddr that will not parse, the host never
	// starts, and the only screen that could change the setting back is behind
	// that host. Nothing written here may be able to brick startup.
	if !validListenPort(c.ListenPort) {
		c.ListenPort = 0
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

// SaveBootstrap rewrites only the rendezvous list. SaveNetConfig writes the
// whole struct, so a caller that builds a NetConfig literal silently erases
// every other setting in the file; go through here instead.
func SaveBootstrap(dataDir string, addrs []string) error {
	c := LoadNetConfig(dataDir)
	c.Bootstrap = addrs
	return SaveNetConfig(dataDir, c)
}

// SavePublicDHT flips the public-DHT opt-in, preserving the rest of the file.
// It takes effect on the next launch: the DHT's bootstrap set is fixed when the
// host starts.
func SavePublicDHT(dataDir string, on bool) error {
	c := LoadNetConfig(dataDir)
	c.PublicDHT = on
	return SaveNetConfig(dataDir, c)
}

// SaveListenPort pins (or, with 0, unpins) the listen port, preserving the rest
// of the file. It takes effect on the next launch: the sockets are bound when
// the host starts.
func SaveListenPort(dataDir string, port int) error {
	if !validListenPort(port) {
		return fmt.Errorf("app: listen port must be 0 (automatic) or between 1024 and 65535")
	}
	c := LoadNetConfig(dataDir)
	c.ListenPort = port
	return SaveNetConfig(dataDir, c)
}
