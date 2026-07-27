package app

import "testing"

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

	if err := SaveBootstrap(dir, []string{addr, "  "}); err != nil {
		t.Fatalf("SaveBootstrap: %v", err)
	}
	c := LoadNetConfig(dir)
	if !c.PublicDHT {
		t.Fatal("rendezvous write cleared the public-DHT opt-in")
	}
	if len(c.Bootstrap) != 1 {
		t.Fatalf("want blank addresses dropped, got %+v", c.Bootstrap)
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
}
