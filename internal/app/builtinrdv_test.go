package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/version"
)

// The rendezvous built in to a copy of Concord is where a fresh or restored
// install starts, and what the user sets replaces it — including setting none.
func TestBuiltInRendezvousIsAStartNotAnOverride(t *testing.T) {
	defer func(v string) { version.Rendezvous = v }(version.Rendezvous)
	built := "/ip4/127.0.0.1/tcp/4001/p2p/12D3KooWCzonwxmETSLwgDY9JAe9WczUHssnYTrSpCqkp6UXZg1q"
	version.Rendezvous = built
	dir := t.TempDir()
	if got := LoadNetConfig(dir).Bootstrap; !reflect.DeepEqual(got, []string{built}) {
		t.Fatalf("fresh install starts from %q, want the built-in list", got)
	}
	if HasNetConfig(dir) {
		t.Fatal("reading the built-in list saved it")
	}
	mine := "/dns4/mine.example.com/tcp/4001/p2p/12D3KooWAbc"
	if err := SaveBootstrap(dir, []string{mine}); err != nil {
		t.Fatal(err)
	}
	if got := LoadNetConfig(dir).Bootstrap; !reflect.DeepEqual(got, []string{mine}) {
		t.Fatalf("after the user set one: %q", got)
	}
	if err := SaveBootstrap(dir, nil); err != nil {
		t.Fatal(err)
	}
	if got := LoadNetConfig(dir).Bootstrap; len(got) != 0 {
		t.Fatalf("a cleared list came back as %q", got)
	}

	version.Rendezvous = ""
	if got := LoadNetConfig(t.TempDir()).Bootstrap; len(got) != 0 {
		t.Fatalf("a build with none started from %q", got)
	}
}

// TestRestoreWithBuiltInRendezvousNeedsNoAddress: the account's only device is
// gone, a guild-mate is online, and the restored install is given no address
// at all — only the one built in to it. It must still come back as itself.
func TestRestoreWithBuiltInRendezvousNeedsNoAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	defer func(v string) { version.Rendezvous = v }(version.Rendezvous)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	owner := startServiceOn(t, ctx, t.TempDir(), boot)
	friend := startServiceOn(t, ctx, t.TempDir(), boot)
	g, err := owner.CreateGuild("Built In")
	if err != nil {
		t.Fatal(err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := friend.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	want := sentinelProfile("restored with nothing typed")
	if err := owner.SetProfile(want); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 60*time.Second, func() bool { return profileMatches(friend.ProfileOf(owner.Fingerprint()), want) },
		"the friend never learned the profile")
	phrase, _ := owner.Mnemonic()
	time.Sleep(2 * time.Second)
	_ = owner.Close()

	version.Rendezvous = boot
	dir := t.TempDir()
	if err := RestoreIdentity(dir, phrase, "test-pass"); err != nil {
		t.Fatal(err)
	}
	restored, err := Start(ctx, Config{DataDir: dir, Passphrase: "test-pass", DisableMDNS: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restored.Close() })
	waitUntil(t, 90*time.Second, func() bool { return profileMatches(restored.SelfProfile(), want) },
		"a restore given no address never reached anyone through the built-in rendezvous")
}
