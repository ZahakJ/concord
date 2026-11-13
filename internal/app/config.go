package app

import (
	"encoding/json"
	"fmt"
	"log"
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
//
// A file that EXISTS but cannot be read as JSON is not "no config" — it is a
// interrupted write, and treating it as empty is how a user's node silently
// forgot its rendezvous and came up alone: os.WriteFile truncates in place, the
// process died between the truncate and the write (a Ctrl-C during the
// update-bootstrap-from-peers save), and the next launch read 0 bytes without a
// word of complaint. The .bak written on every successful save is the recovery.
func LoadNetConfig(dataDir string) NetConfig {
	var c NetConfig
	b, err := os.ReadFile(netConfigPath(dataDir))
	if os.IsNotExist(err) {
		// Never configured — a fresh install, or one restored from its phrase,
		// which carries no settings. Both start from the build's default.
		c.Bootstrap = BuiltInRendezvous()
	}
	if err == nil {
		if len(b) == 0 || json.Unmarshal(b, &c) != nil {
			if bb, berr := os.ReadFile(netConfigPath(dataDir) + ".bak"); berr == nil {
				_ = json.Unmarshal(bb, &c)
				log.Printf("concord/app: netconfig.json was corrupt (%d bytes); restored from backup", len(b))
			} else {
				log.Printf("concord/app: netconfig.json is corrupt (%d bytes) and no backup exists — running without a configured rendezvous", len(b))
			}
		}
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
	path := netConfigPath(dataDir)
	// Keep the last good file as .bak, then install the new one by RENAME.
	// os.WriteFile truncates the target before writing, so a process killed in
	// that window leaves 0 bytes — which is exactly how a node lost its
	// rendezvous and booted alone. A rename either happens or does not; there is
	// no state in between, and the .bak covers corruption from any other source.
	if cur, rerr := os.ReadFile(path); rerr == nil && len(cur) > 0 && json.Valid(cur) {
		_ = os.WriteFile(path+".bak", cur, 0o600)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

// DefaultRendezvous is the rendezvous list compiled into this build, comma
// separated, set at link time:
//
//	-ldflags "-X github.com/ZahakJ/concord/internal/app.DefaultRendezvous=/dns4/…/p2p/…"
//
// Empty unless a build sets it, and then an install that has never saved a
// connection setting starts from it. Which node, if any, a released build names
// is a decision about who that node gets to see (DESIGN §6.3), so it is made
// by whoever builds the release, not here.
var DefaultRendezvous = ""

// BuiltInRendezvous is DefaultRendezvous as a list.
func BuiltInRendezvous() []string {
	var out []string
	for _, a := range strings.Split(DefaultRendezvous, ",") {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// RendezvousFromInput reads what someone pasted where a rendezvous is asked
// for: one or more rendezvous addresses (one per line or comma separated), or
// an invite code, whose rendezvous list is taken and nothing else — pasting
// an invite there configures the connection, it does not join the guild. An
// address that does not parse is refused here rather than saved, because a
// saved address that cannot be dialled is a node that comes up alone.
func RendezvousFromInput(text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	if !strings.HasPrefix(text, "/") {
		ic, err := decodeInviteCode(text)
		if err != nil {
			return nil, fmt.Errorf("app: that is neither a rendezvous address nor an invite code")
		}
		if len(ic.Bootstrap) == 0 {
			return nil, fmt.Errorf("app: that invite names no rendezvous")
		}
		return ic.Bootstrap, nil
	}
	var out []string
	for _, a := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == ',' || r == ' ' }) {
		if a = strings.TrimSpace(a); a == "" {
			continue
		}
		if _, err := parseBootstrapPeers([]string{a}); err != nil {
			return nil, fmt.Errorf("app: %q is not a rendezvous address (it should end in /p2p/<id>)", a)
		}
		out = append(out, a)
	}
	return out, nil
}
