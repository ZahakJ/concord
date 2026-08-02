package app

import (
	"context"
	"testing"
)

// A linked device's profile must never DESTROY this device's game collection
// just by not mentioning it. Games is an `omitempty` slice, so "I have no
// games" and "I didn't send games" arrive identically as nil; AdoptLinkedProfile
// used to write "" for both, which silently wiped a real collection the moment
// any other device's profile won the stamp race. Only a sender that is
// authoritative about games (GamesSet, as SelfProfile always is) may clear them.
func TestAdoptedProfileDoesNotClobberGames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())

	mine := []Game{{Name: "Portal"}, {Name: "The Talos Principle"}}
	if err := s.SetGames(mine); err != nil {
		t.Fatalf("SetGames: %v", err)
	}
	if got := s.SelfProfile().Games; len(got) != 2 {
		t.Fatalf("setup: want 2 games, got %d", len(got))
	}
	base := s.profileStamp()

	// A device that says nothing about games (an older build, or any profile
	// edit that didn't carry the collection) must leave them alone.
	silent := s.SelfProfile()
	silent.Games = nil
	silent.GamesSet = false
	silent.Status = "from the quiet device"
	silent.UpdatedAt = base + 1
	if !s.AdoptLinkedProfile(silent) {
		t.Fatal("expected the newer profile to be adopted")
	}
	if got := s.SelfProfile().Games; len(got) != 2 {
		t.Fatalf("REGRESSION: a silent profile wiped the collection — want 2 games, got %d", len(got))
	}
	if s.SelfProfile().Status != "from the quiet device" {
		t.Fatal("the rest of the profile should still have been adopted")
	}

	// A device that IS authoritative and genuinely holds none still clears —
	// deleting your last game on one device must propagate to the others.
	explicit := s.SelfProfile()
	explicit.Games = nil
	explicit.GamesSet = true
	explicit.UpdatedAt = base + 2
	if !s.AdoptLinkedProfile(explicit) {
		t.Fatal("expected the newer profile to be adopted")
	}
	if got := s.SelfProfile().Games; len(got) != 0 {
		t.Fatalf("an explicit empty collection should clear — got %d games", len(got))
	}

	// And a device carrying games replaces the list as usual.
	theirs := s.SelfProfile()
	theirs.Games = []Game{{Name: "Subnautica"}}
	theirs.UpdatedAt = base + 3
	if !s.AdoptLinkedProfile(theirs) {
		t.Fatal("expected the newer profile to be adopted")
	}
	if got := s.SelfProfile().Games; len(got) != 1 || got[0].Name != "Subnautica" {
		t.Fatalf("want the adopted list, got %+v", got)
	}
}

// SelfProfile must always mark itself authoritative — that flag is what lets
// a real "I cleared my games" survive the guard above.
func TestSelfProfileIsAuthoritativeAboutGames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())
	if !s.SelfProfile().GamesSet {
		t.Fatal("SelfProfile must set GamesSet so an intentional clear propagates")
	}
}
