package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cnet "github.com/ZahakJ/concord/internal/net"
)

// The client half is tested against a fake proxy rather than a real rendezvous,
// and NEVER against the real Tenor API: a test that reaches the open web is a
// test that fails when the network does, and this one would also be sending
// somebody's API quota into a CI run.
//
// The seam is gifProxyRoundTrip (see gifsearch.go). Swapping it exercises
// everything above the wire — status mapping, the client's own size ceiling,
// subtype validation, and the seal-and-send path — while leaving the transport
// itself to the browser end-to-end run.

// fakeProxy installs a stand-in rendezvous for the duration of a test.
func fakeProxy(t *testing.T, fn func(ctx context.Context, req cnet.GifRequest) (cnet.GifResponse, string, error)) {
	t.Helper()
	prev := gifProxyRoundTrip
	gifProxyRoundTrip = func(_ *Service, ctx context.Context, req cnet.GifRequest) (cnet.GifResponse, string, error) {
		return fn(ctx, req)
	}
	t.Cleanup(func() { gifProxyRoundTrip = prev })
}

// TestGifSearchUnavailable: a rendezvous with no API key must produce a
// specific, explainable status — not an error that reads like a bug, and not an
// empty grid.
func TestGifSearchUnavailable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)

	fakeProxy(t, func(_ context.Context, _ cnet.GifRequest) (cnet.GifResponse, string, error) {
		return cnet.GifResponse{
			Status: cnet.GifStatusUnavailable,
			Detail: "this rendezvous has no GIF API key configured",
		}, "12D3KooWfake", nil
	})

	got := a.SearchGifs(ctx, "cat", "")
	if got.Status != cnet.GifStatusUnavailable {
		t.Fatalf("status = %q, want %q", got.Status, cnet.GifStatusUnavailable)
	}
	if got.Detail == "" {
		t.Fatal("an unavailable proxy must come with a reason the UI can show")
	}
	if got.Results == nil {
		t.Fatal("Results must be an empty slice, never nil — the UI iterates it")
	}

	// The tab's pre-flight check reports the same thing without a query.
	if av := a.GifSearchAvailable(ctx); av.Status != cnet.GifStatusUnavailable {
		t.Fatalf("GifSearchAvailable status = %q, want unavailable", av.Status)
	}
}

// TestGifSearchNoRendezvous: with nothing configured to proxy through, the
// client must say so itself rather than time out pretending to try.
func TestGifSearchNoRendezvous(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)

	// No fake: startService configures no bootstrap peers, so the real
	// askRendezvous takes its "nothing to ask" branch.
	got := a.SearchGifs(ctx, "cat", "")
	if got.Status != GifSearchNoRendezvous {
		t.Fatalf("status = %q, want %q", got.Status, GifSearchNoRendezvous)
	}
	if !strings.Contains(got.Detail, "rendezvous") {
		t.Fatalf("detail = %q, want it to name the missing rendezvous", got.Detail)
	}
}

// TestGifSearchEmptyResults: a search that matched nothing is a SUCCESS with no
// hits, and must not be confused with a broken proxy.
func TestGifSearchEmptyResults(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)

	fakeProxy(t, func(_ context.Context, _ cnet.GifRequest) (cnet.GifResponse, string, error) {
		return cnet.GifResponse{Status: cnet.GifStatusOK, Source: "Tenor"}, "12D3KooWfake", nil
	})

	got := a.SearchGifs(ctx, "asdfqwerzxcv", "")
	if got.Status != cnet.GifStatusOK {
		t.Fatalf("status = %q, want ok", got.Status)
	}
	if len(got.Results) != 0 || got.Results == nil {
		t.Fatalf("Results = %#v, want an empty non-nil slice", got.Results)
	}
	if got.Source != "Tenor" {
		t.Fatalf("Source = %q — the UI has to be able to name where results come from", got.Source)
	}
}

// TestGifSearchNormalResults covers the happy path end to end below the wire:
// results carry opaque handles and no URL, thumbnails come back as data URLs,
// and sending posts an ordinary attachment token.
func TestGifSearchNormalResults(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	const imgBytes = "GIF89a-not-really-but-opaque"
	var sawOps []string
	fakeProxy(t, func(_ context.Context, req cnet.GifRequest) (cnet.GifResponse, string, error) {
		sawOps = append(sawOps, req.Op)
		switch req.Op {
		case "search":
			return cnet.GifResponse{
				Status: cnet.GifStatusOK, Source: "Tenor", Next: "20",
				Results: []cnet.GifHit{{
					ID: "abc", Title: "cat vibing",
					Preview: "cHJldg.c2ln", Full: "ZnVsbA.c2ln",
					Width: 320, Height: 240,
				}},
			}, "12D3KooWfake", nil
		case "media":
			return cnet.GifResponse{
				Status: cnet.GifStatusOK, Subtype: "gif", Media: []byte(imgBytes),
			}, "12D3KooWfake", nil
		}
		return cnet.GifResponse{Status: cnet.GifStatusBadRequest}, "12D3KooWfake", nil
	})

	got := a.SearchGifs(ctx, "cat", "")
	if got.Status != cnet.GifStatusOK || len(got.Results) != 1 {
		t.Fatalf("search = %+v, want one ok result", got)
	}
	hit := got.Results[0]
	if got.Next != "20" {
		t.Fatalf("Next = %q, want the proxy's cursor passed through", got.Next)
	}
	// The privacy property, asserted at the type level: a result carries
	// handles, and there is nowhere for a URL to hide.
	if strings.Contains(hit.Preview, "://") || strings.Contains(hit.Full, "://") {
		t.Fatalf("a result handle looks like a URL (%q/%q) — the client must have nothing fetchable", hit.Preview, hit.Full)
	}

	// A thumbnail arrives as a data URL, so the browser makes no request.
	src, err := a.GifSearchMedia(ctx, hit.Preview, false)
	if err != nil {
		t.Fatalf("GifSearchMedia: %v", err)
	}
	if !strings.HasPrefix(src, "data:image/gif;base64,") {
		t.Fatalf("preview = %.40q…, want an inline data URL", src)
	}

	// Sending goes out as the ordinary v1 attachment token — the same one the
	// guild pack emits — so recipients need no new code and fetch nothing from
	// Tenor themselves.
	msg, err := a.SendSearchedGif(ctx, g.Channels[0].ID, hit.Full, "", hit.Width, hit.Height)
	if err != nil {
		t.Fatalf("SendSearchedGif: %v", err)
	}
	if !strings.HasPrefix(msg.Content, "![image](concord://attach/v1/") ||
		!strings.HasSuffix(msg.Content, "/gif/320x240)") {
		t.Fatalf("token = %q, want a plain v1 image attachment token", msg.Content)
	}
	if strings.Contains(msg.Content, "tenor") {
		t.Fatalf("token = %q leaks the source into every recipient's message body", msg.Content)
	}

	// Saving joins the guild pack, after which the proxy is not needed again.
	saved, err := a.SaveSearchedGif(ctx, g.ID, "Cat Vibing", []string{"cat"}, hit.Full, hit.Width, hit.Height)
	if err != nil {
		t.Fatalf("SaveSearchedGif: %v", err)
	}
	list, err := a.GuildGifs(g.ID)
	if err != nil || len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("pack = %+v (err %v), want the saved GIF", list, err)
	}
	if list[0].Name != "Cat Vibing" || strings.Join(list[0].Tags, ",") != "cat" {
		t.Fatalf("saved record = %+v, want the name and tags we passed", list[0])
	}

	if len(sawOps) == 0 || sawOps[0] != "search" {
		t.Fatalf("ops = %v, want the search first", sawOps)
	}
}

// TestGifSearchOversizedMedia: the node has its own cap, but the client must
// not depend on it. A modified rendezvous sending a full frame has to be
// refused here, before the bytes are sealed into a blob that could never be
// posted anyway.
func TestGifSearchOversizedMedia(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	g, err := a.CreateGuild("g")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}

	fakeProxy(t, func(_ context.Context, _ cnet.GifRequest) (cnet.GifResponse, string, error) {
		return cnet.GifResponse{
			Status: cnet.GifStatusOK, Subtype: "gif",
			Media: make([]byte, maxGifPlain+1),
		}, "12D3KooWfake", nil
	})

	if _, err := a.GifSearchMedia(ctx, "ref.sig", true); err == nil {
		t.Fatal("an over-cap image was accepted")
	}
	if _, err := a.SendSearchedGif(ctx, g.Channels[0].ID, "ref.sig", "", 0, 0); err == nil {
		t.Fatal("an over-cap image was sent")
	}
	// Nothing was posted, and in particular nothing was sealed into the store.
	msgs, err := a.Messages(g.Channels[0].ID, 50)
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "concord://attach/") {
			t.Fatalf("an over-cap GIF was posted anyway: %q", m.Content)
		}
	}
}

// TestGifSearchBadSubtype: the subtype ends up in a token every other member
// renders, so a node claiming something outside the four supported types is
// refused rather than passed along.
func TestGifSearchBadSubtype(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)

	fakeProxy(t, func(_ context.Context, _ cnet.GifRequest) (cnet.GifResponse, string, error) {
		return cnet.GifResponse{
			Status: cnet.GifStatusOK, Subtype: "svg+xml", Media: []byte("<svg/>"),
		}, "12D3KooWfake", nil
	})
	if _, err := a.GifSearchMedia(ctx, "ref.sig", false); err == nil {
		t.Fatal("an unsupported image type was accepted")
	}
}

// TestGifSearchTimeout: a rendezvous that accepts the request and then says
// nothing must end in a definite, explained state. The forbidden outcome is a
// spinner that spins forever.
func TestGifSearchTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)

	fakeProxy(t, func(rctx context.Context, _ cnet.GifRequest) (cnet.GifResponse, string, error) {
		// Stand in for the real deadline, which lives on the libp2p stream.
		tctx, tcancel := context.WithTimeout(rctx, 50*time.Millisecond)
		defer tcancel()
		<-tctx.Done()
		return cnet.GifResponse{}, "", errors.New("net: open gif search stream: deadline exceeded")
	})

	done := make(chan GifSearchResult, 1)
	go func() { done <- a.SearchGifs(ctx, "cat", "") }()
	select {
	case got := <-done:
		if got.Status != GifSearchUnreachable {
			t.Fatalf("status = %q, want %q", got.Status, GifSearchUnreachable)
		}
		if got.Detail == "" {
			t.Fatal("an unreachable proxy must come with a reason the UI can show")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SearchGifs never returned — this is the spinner-forever failure")
	}
}

// TestGifSearchEmptyQuery: the box is empty, so nothing is sent anywhere. The
// check has to be local — a round trip to ask the rendezvous "did I type
// anything" would hand it a request it did not need to see.
func TestGifSearchEmptyQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)

	asked := false
	fakeProxy(t, func(_ context.Context, _ cnet.GifRequest) (cnet.GifResponse, string, error) {
		asked = true
		return cnet.GifResponse{Status: cnet.GifStatusOK}, "12D3KooWfake", nil
	})
	got := a.SearchGifs(ctx, "   ", "")
	if asked {
		t.Fatal("an empty query was sent to the rendezvous")
	}
	if got.Status != cnet.GifStatusBadRequest {
		t.Fatalf("status = %q, want bad_request", got.Status)
	}
}
