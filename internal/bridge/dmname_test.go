package bridge

import (
	"context"
	"testing"

	appsvc "github.com/ZahakJ/concord/internal/app"
)

// A DM opened to a contact who is offline holds only the invite until they
// come online. It was called "Direct message" everywhere until then, though
// the person was picked by name; it is that person's conversation at once.
func TestDMToAnOfflineContactIsTheirsAtOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a full node")
	}
	svc, err := appsvc.Start(context.Background(), appsvc.Config{DataDir: t.TempDir(), Passphrase: "dm-name", DisableMDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	friend := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	dm, err := svc.StartDM(friend)
	if err != nil {
		t.Fatal(err)
	}
	br := &Bridge{ctx: context.Background(), svc: svc}
	views, err := br.Guilds()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.ID != dm.ID {
			continue
		}
		if v.DMPeer != friend {
			t.Fatalf("dmPeer = %q, want the contact picked", v.DMPeer)
		}
		if v.Name == "Direct message" {
			t.Fatal(`still named "Direct message" while the contact is offline`)
		}
		return
	}
	t.Fatal("the DM is not listed")
}
