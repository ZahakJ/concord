package app

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	cnet "github.com/ZahakJ/concord/internal/net"
)

// TestGameSearchDefault enumerates the migration. This is the part that can
// only be got wrong once, on somebody else's machine: an install that has been
// using the collection editor for months must not have the feature removed by
// an upgrade, and a fresh install must not have it handed to them.
func TestGameSearchDefault(t *testing.T) {
	cases := []struct {
		name        string
		pref, games string
		wantOn      bool
		wantPersist bool
	}{
		{"explicitly on", "1", "", true, false},
		// A recorded answer always beats the evidence: somebody who turned it
		// off keeps it off no matter how many games they own.
		{"explicitly off", "0", `[{"name":"Portal"}]`, false, false},
		{"fresh install", "", "", false, true},
		{"existing user with a collection", "", `[{"name":"Portal"}]`, true, true},
		{"emptied collection is not evidence", "", `[]`, false, true},
		{"unparsable leftovers are not evidence", "", `{`, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			on, persist := gameSearchDefault(tc.pref, tc.games)
			if on != tc.wantOn {
				t.Errorf("on = %v, want %v", on, tc.wantOn)
			}
			if persist != tc.wantPersist {
				t.Errorf("persist = %v, want %v", persist, tc.wantPersist)
			}
		})
	}
}

// countingTransport fails the test if anything asks it to make a request.
type countingTransport struct{ n atomic.Int64 }

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, http.ErrUseLastResponse
}

// TestGameSearchOffMakesNoRequest is the claim PRIVACY.md is allowed to make.
// Not "the UI does not call it" — the transport is watched, so a cached answer
// or a stray code path would show up as a request that should not exist.
func TestGameSearchOffMakesNoRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())

	var tr countingTransport
	prev := gameSearchClient.Transport
	gameSearchClient.Transport = &tr
	t.Cleanup(func() { gameSearchClient.Transport = prev })

	// A fresh keystore has no game collection, so the migration has already
	// decided this install starts closed.
	if s.GameSearchEnabled() {
		t.Fatal("a fresh install must start with game search off")
	}
	if got := s.SearchGames("portal"); got != nil {
		t.Fatalf("switched off, SearchGames returned %d results", len(got))
	}
	if n := tr.n.Load(); n != 0 {
		t.Fatalf("switched off, %d request(s) still went out", n)
	}
}

// TestGameSearchMigrationKeepsAWorkingFeature: an account that already has a
// collection is an account that has used the editor, so the upgrade must not
// take it away.
func TestGameSearchMigrationKeepsAWorkingFeature(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())

	if err := s.SetGames([]Game{{Name: "Portal"}}); err != nil {
		t.Fatalf("SetGames: %v", err)
	}
	// Undo the decision the fresh-install migration already wrote, so the next
	// load faces the same settings an upgrading install would.
	if err := s.store.SetSetting(gameSearchPrefKey, ""); err != nil {
		t.Fatalf("clear pref: %v", err)
	}
	s.loadOffDeviceSearchPrefs()

	if !s.GameSearchEnabled() {
		t.Fatal("an install with a game collection must keep game search on")
	}
	// And the decision must be written down, not re-derived: removing the last
	// game later is not consent to lose the editor.
	if err := s.SetGames(nil); err != nil {
		t.Fatalf("SetGames(nil): %v", err)
	}
	s.loadOffDeviceSearchPrefs()
	if !s.GameSearchEnabled() {
		t.Fatal("emptying the collection must not retract the recorded answer")
	}
}

// TestGifSearchOffNeverReachesTheRendezvous covers both entry points, plus the
// media funnel that sending and saving share.
func TestGifSearchOffNeverReachesTheRendezvous(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startServiceInDir(t, ctx, t.TempDir())

	if !s.GifSearchEnabled() {
		t.Fatal("GIF search defaults ON — it reaches only the rendezvous the user already chose")
	}
	if err := s.SetGifSearchEnabled(false); err != nil {
		t.Fatalf("SetGifSearchEnabled: %v", err)
	}

	fakeProxy(t, func(_ context.Context, req cnet.GifRequest) (cnet.GifResponse, string, error) {
		t.Errorf("switched off, a %q request still went to the rendezvous", req.Op)
		return cnet.GifResponse{Status: cnet.GifStatusOK}, "12D3KooWfake", nil
	})

	if got := s.GifSearchAvailable(ctx); got.Status != GifSearchOff {
		t.Errorf("probe status = %q, want %q", got.Status, GifSearchOff)
	}
	if got := s.SearchGifs(ctx, "cat", ""); got.Status != GifSearchOff {
		t.Errorf("search status = %q, want %q", got.Status, GifSearchOff)
	} else if got.Results == nil {
		t.Error("Results must be an empty slice, never nil — the picker iterates it")
	}
	// A handle minted before the switch was flipped must not still redeem.
	if _, err := s.GifSearchMedia(ctx, "some-handle", false); err == nil {
		t.Error("switched off, GifSearchMedia resolved a handle anyway")
	}

	// Back on, and the same calls reach the proxy again — a switch that cannot
	// be switched back is a removal.
	if err := s.SetGifSearchEnabled(true); err != nil {
		t.Fatalf("SetGifSearchEnabled(true): %v", err)
	}
	var asked atomic.Int64
	fakeProxy(t, func(_ context.Context, _ cnet.GifRequest) (cnet.GifResponse, string, error) {
		asked.Add(1)
		return cnet.GifResponse{Status: cnet.GifStatusOK, Source: "Giphy"}, "12D3KooWfake", nil
	})
	if got := s.GifSearchAvailable(ctx); got.Status != cnet.GifStatusOK {
		t.Errorf("switched back on, probe status = %q", got.Status)
	}
	if asked.Load() == 0 {
		t.Error("switched back on, nothing reached the rendezvous")
	}
}
