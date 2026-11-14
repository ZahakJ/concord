package version

import (
	"reflect"
	"testing"
)

func TestRendezvousListAndHosts(t *testing.T) {
	defer func(v string) { Rendezvous = v }(Rendezvous)
	Rendezvous = ""
	if RendezvousList() != nil || RendezvousHosts() != nil {
		t.Fatal("a build with no rendezvous names one")
	}
	Rendezvous = " /dns4/rdv.example.com/tcp/4001/p2p/12D3KooWA , /ip4/203.0.113.7/tcp/4001/p2p/12D3KooWB,"
	if got, want := RendezvousList(), []string{"/dns4/rdv.example.com/tcp/4001/p2p/12D3KooWA", "/ip4/203.0.113.7/tcp/4001/p2p/12D3KooWB"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("list = %q", got)
	}
	if got, want := RendezvousHosts(), []string{"rdv.example.com", "203.0.113.7"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts = %q", got)
	}
}
