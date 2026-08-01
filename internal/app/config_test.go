package app

import (
	"os"
	"testing"
)

// SaveNetConfig writes the whole struct, so the field-at-a-time helpers are the
// only safe way to touch one setting. This is the regression that would
// otherwise show up as "changing the rendezvous silently turned my other
// settings off".
func TestNetConfigWritersPreserveSiblings(t *testing.T) {
	dir := t.TempDir()
	addr := "/ip4/198.51.100.7/tcp/4001/p2p/12D3KooWPx4quLj6rDB4b9H9ywbvqepNHVCUt1g27vLiSYbMpcWh"

	if err := SaveBootstrap(dir, []string{addr}); err != nil {
		t.Fatalf("SaveBootstrap: %v", err)
	}
	if err := SavePublicDHT(dir, true); err != nil {
		t.Fatalf("SavePublicDHT: %v", err)
	}
	if c := LoadNetConfig(dir); !c.PublicDHT || len(c.Bootstrap) != 1 || c.Bootstrap[0] != addr {
		t.Fatalf("public-DHT write dropped the rendezvous: %+v", c)
	}

	if err := SaveListenPort(dir, 4001); err != nil {
		t.Fatalf("SaveListenPort: %v", err)
	}
	if c := LoadNetConfig(dir); c.ListenPort != 4001 || !c.PublicDHT || len(c.Bootstrap) != 1 {
		t.Fatalf("listen-port write dropped a sibling: %+v", c)
	}

	if err := SaveBootstrap(dir, []string{addr, "  "}); err != nil {
		t.Fatalf("SaveBootstrap: %v", err)
	}
	c := LoadNetConfig(dir)
	if !c.PublicDHT {
		t.Fatal("rendezvous write cleared the public-DHT opt-in")
	}
	if c.ListenPort != 4001 {
		t.Fatal("rendezvous write cleared the pinned listen port")
	}
	if len(c.Bootstrap) != 1 {
		t.Fatalf("want blank addresses dropped, got %+v", c.Bootstrap)
	}
}

// A port the host cannot bind would leave the app unable to start, with the
// setting that broke it only reachable from inside the app. Refuse it here.
func TestSaveListenPortRejectsUnbindable(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []int{80, 1023, -1, 65536} {
		if err := SaveListenPort(dir, p); err == nil {
			t.Fatalf("port %d should be refused", p)
		}
	}
	if err := SaveListenPort(dir, 0); err != nil {
		t.Fatalf("0 means automatic and must be accepted: %v", err)
	}
	if c := LoadNetConfig(dir); c.ListenPort != 0 {
		t.Fatalf("a refused port must not be written: %+v", c)
	}
}

// The file is plaintext and hand-editable, so the writers' validation is not
// the last line of defence. A negative port builds an unparseable multiaddr and
// the host never starts — leaving the only screen that could fix the setting
// behind a login that cannot happen.
func TestLoadNetConfigRepairsAnUnusablePort(t *testing.T) {
	for _, raw := range []string{`{"listenPort":-1}`, `{"listenPort":70000}`, `{"listenPort":80}`} {
		dir := t.TempDir()
		if err := os.WriteFile(netConfigPath(dir), []byte(raw), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if got := LoadNetConfig(dir).ListenPort; got != 0 {
			t.Fatalf("%s: want the port ignored, got %d", raw, got)
		}
	}
}

func TestLoadNetConfigDefaultsToOff(t *testing.T) {
	c := LoadNetConfig(t.TempDir())
	if c.PublicDHT {
		t.Fatal("the public DHT must be off until the user asks for it")
	}
	if len(c.Bootstrap) != 0 {
		t.Fatalf("want no rendezvous by default, got %+v", c.Bootstrap)
	}
	if c.ListenPort != 0 {
		t.Fatalf("want an automatic port by default, got %d", c.ListenPort)
	}
}

// TestNetConfigSurvivesATruncatedWrite pins the failure that took a user's node
// off the network: os.WriteFile truncates in place, the process died between
// truncate and write (a Ctrl-C mid-save), and the next launch read a 0-byte
// file as "no config" — silently booting with no rendezvous at all.
func TestNetConfigSurvivesATruncatedWrite(t *testing.T) {
	dir := t.TempDir()
	want := NetConfig{Bootstrap: []string{"/dns/example.com/tcp/4001/p2p/12D3KooWTest"}}
	if err := SaveNetConfig(dir, want); err != nil {
		t.Fatal(err)
	}
	// A second save creates the .bak of the first; the kill happens during it.
	if err := SaveNetConfig(dir, want); err != nil {
		t.Fatal(err)
	}
	// The truncation: exactly what an interrupted os.WriteFile leaves behind.
	if err := os.WriteFile(netConfigPath(dir), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got := LoadNetConfig(dir)
	if len(got.Bootstrap) != 1 || got.Bootstrap[0] != want.Bootstrap[0] {
		t.Fatalf("a truncated netconfig lost the rendezvous: got %+v — the node "+
			"boots alone and the user sees 'no bootstrap node reachable'", got)
	}

	// And no stray .tmp is left around by the atomic path.
	if _, err := os.Stat(netConfigPath(dir) + ".tmp"); err == nil {
		t.Fatal("SaveNetConfig left its temp file behind")
	}
}
