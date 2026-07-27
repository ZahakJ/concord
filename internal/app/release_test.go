package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// runningExecutable is what the responder serves, so the tests describe it the
// same way the responder finds it.
func runningExecutable(t *testing.T) (path string, size int64) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("no executable path on this platform: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	fi, err := os.Stat(exe)
	if err != nil {
		t.Fatal(err)
	}
	return exe, fi.Size()
}

// ask goes straight to serveRelease: these cases are about what a member is
// answered with. Who counts as a member is TestReleaseOffersNeedAGuildInCommon.
func ask(t *testing.T, s *Service, req releaseRequest) []byte {
	t.Helper()
	b, _ := json.Marshal(req)
	return s.serveRelease(b)
}

func TestServeRelease(t *testing.T) {
	dir := t.TempDir()
	exe, size := runningExecutable(t)
	s := &Service{dataDir: dir}

	// Nothing recorded: this node has nothing it can prove is a real release.
	if got := ask(t, s, releaseRequest{Op: "offer"}); len(got) != 0 {
		t.Fatalf("offered a release with no manifest: %q", got)
	}
	// A version-only record is the updater's "we looked, this build isn't a
	// published release" marker. It must stay just as silent.
	if err := SaveReleaseManifest(dir, ReleaseManifest{Version: "v9.9.9"}); err != nil {
		t.Fatal(err)
	}
	if got := ask(t, s, releaseRequest{Op: "offer"}); len(got) != 0 {
		t.Fatalf("offered a release from a negative marker: %q", got)
	}

	m := ReleaseManifest{
		Version: "v9.9.9",
		Asset:   "concord-linux-amd64-v9.9.9",
		Size:    size,
		Sums:    []byte("aa11  concord-linux-amd64-v9.9.9\n"),
		Sig:     []byte("signature-bytes"),
	}
	if err := SaveReleaseManifest(dir, m); err != nil {
		t.Fatal(err)
	}

	var offer ReleaseOffer
	if err := json.Unmarshal(ask(t, s, releaseRequest{Op: "offer"}), &offer); err != nil {
		t.Fatal(err)
	}
	if offer.Version != m.Version || offer.Asset != m.Asset || offer.Size != size {
		t.Fatalf("offer = %+v, want %s/%s/%d", offer, m.Version, m.Asset, size)
	}

	var sums ReleaseSignedSums
	if err := json.Unmarshal(ask(t, s, releaseRequest{Op: "manifest"}), &sums); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sums.Sums, m.Sums) || !bytes.Equal(sums.Sig, m.Sig) {
		t.Fatal("manifest served back altered")
	}

	// A chunk must be the executable's actual bytes at that offset.
	const off, n = 1024, 512
	want := make([]byte, n)
	f, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.ReadAt(want, off); err != nil {
		t.Fatal(err)
	}
	if got := ask(t, s, releaseRequest{Op: "chunk", Offset: off, Length: n}); !bytes.Equal(got, want) {
		t.Fatalf("chunk mismatch: got %d bytes", len(got))
	}
	// The tail is short, not padded and not an error.
	tail := ask(t, s, releaseRequest{Op: "chunk", Offset: size - 10, Length: 4096})
	if len(tail) != 10 {
		t.Fatalf("tail chunk = %d bytes, want 10", len(tail))
	}
	// Out-of-range reads answer "nothing", never a slice of something else.
	for _, req := range []releaseRequest{
		{Op: "chunk", Offset: size, Length: 16},
		{Op: "chunk", Offset: -1, Length: 16},
		{Op: "chunk", Offset: 0, Length: 0},
		{Op: "wat"},
	} {
		if got := ask(t, s, req); len(got) != 0 {
			t.Fatalf("%+v served %d bytes", req, len(got))
		}
	}
}

// If the binary on disk is no longer the one the manifest was written for, we
// go silent rather than cost a peer a download that can only fail its checksum.
func TestServeReleaseRefusesMismatchedBinary(t *testing.T) {
	dir := t.TempDir()
	_, size := runningExecutable(t)
	s := &Service{dataDir: dir}

	if err := SaveReleaseManifest(dir, ReleaseManifest{
		Version: "v9.9.9",
		Asset:   "concord-linux-amd64-v9.9.9",
		Size:    size + 1,
		Sums:    []byte("aa11  concord-linux-amd64-v9.9.9\n"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := servableRelease(dir); ok {
		t.Fatal("served a binary that no longer matches its manifest")
	}
	for _, op := range []string{"offer", "manifest", "chunk"} {
		if got := ask(t, s, releaseRequest{Op: op, Length: 16}); len(got) != 0 {
			t.Fatalf("op %q served %d bytes despite a size mismatch", op, len(got))
		}
	}
}

// The real thing: one Service fetches another's release over libp2p, offer to
// manifest to bytes.
func TestPeerReleaseTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seed := startService(t, ctx)
	client := startService(t, ctx)

	exe, size := runningExecutable(t)
	m := ReleaseManifest{
		Version: "v9.9.9",
		Asset:   "concord-linux-amd64-v9.9.9",
		Size:    size,
		Sums:    []byte("aa11  concord-linux-amd64-v9.9.9\n"),
		Sig:     []byte("detached-signature"),
	}
	if err := SaveReleaseManifest(seed.dataDir, m); err != nil {
		t.Fatal(err)
	}
	shareAGuild(t, ctx, seed, client)

	offers := client.PeerReleaseOffers(ctx)
	if len(offers) != 1 || offers[0].Version != m.Version || offers[0].Size != size {
		t.Fatalf("offers = %+v, want one %s of %d bytes", offers, m.Version, size)
	}
	if offers[0].PeerID != seed.host.PeerID().String() {
		t.Fatalf("offer credited to %s, want %s", offers[0].PeerID, seed.host.PeerID())
	}

	sums, err := client.PeerReleaseManifest(ctx, offers[0].PeerID)
	if err != nil {
		t.Fatalf("PeerReleaseManifest: %v", err)
	}
	if !bytes.Equal(sums.Sums, m.Sums) || !bytes.Equal(sums.Sig, m.Sig) {
		t.Fatal("signed manifest did not survive the round trip")
	}

	// Pull the first stretch of the binary and compare against the source.
	f, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []byte
	for int64(len(got)) < size && len(got) < 3<<20 {
		chunk, err := client.PeerReleaseChunk(ctx, offers[0].PeerID, int64(len(got)), size-int64(len(got)))
		if err != nil {
			t.Fatalf("PeerReleaseChunk at %d: %v", len(got), err)
		}
		got = append(got, chunk...)
	}
	want := make([]byte, len(got))
	if _, err := f.ReadAt(want, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("transferred %d bytes but they differ from the source", len(got))
	}
}

// shareAGuild puts two services in one guild and leaves them connected, which
// is what the release responder now requires of an asker.
func shareAGuild(t *testing.T, ctx context.Context, host, joiner *Service) {
	t.Helper()
	g, err := host.CreateGuild("release")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := host.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := joiner.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, host, joiner)
	if err := joiner.host.Connect(ctx, host.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
}

// The bytes are a published release, but the ANSWER names the version, OS and
// architecture of the node giving it. Handed to anyone who connects — a DHT
// routing peer, any IPFS node once the public-DHT opt-in is on — that is a way
// to enumerate everyone still running a version with a known hole. So a
// stranger gets silence, on every op: the signed manifest names the release as
// plainly as the offer does.
func TestReleaseOffersNeedAGuildInCommon(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seed := startService(t, ctx)
	stranger := startService(t, ctx)

	_, size := runningExecutable(t)
	if err := SaveReleaseManifest(seed.dataDir, ReleaseManifest{
		Version: "v9.9.9",
		Asset:   "concord-linux-amd64-v9.9.9",
		Size:    size,
		Sums:    []byte("aa11  concord-linux-amd64-v9.9.9\n"),
		Sig:     []byte("detached-signature"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := stranger.host.Connect(ctx, seed.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if offers := stranger.PeerReleaseOffers(ctx); len(offers) != 0 {
		t.Fatalf("a connected stranger was told our version: %+v", offers)
	}
	seedID := seed.host.PeerID().String()
	if ms, err := stranger.PeerReleaseManifest(ctx, seedID); err == nil && len(ms.Sums) != 0 {
		t.Fatalf("a connected stranger was served the signed manifest: %q", ms.Sums)
	}
	if _, err := stranger.PeerReleaseChunk(ctx, seedID, 0, size); err == nil {
		t.Fatal("a connected stranger was served binary bytes")
	}

	// Same node, same request, once they share a guild.
	shareAGuild(t, ctx, seed, stranger)
	if offers := stranger.PeerReleaseOffers(ctx); len(offers) != 1 {
		t.Fatalf("a fellow member got %d offers, want 1", len(offers))
	}
}

// A peer that answers every chunk request with one byte never errors and never
// finishes: at 128 MiB claimed that is 134 million round trips, all of them
// spent in phase "downloading". Nothing about the reply is malformed, so the
// only thing that can catch it is insisting the reply be the length we asked
// for — which is what an honest seeder always sends.
func TestPeerReleaseChunkRejectsDripFeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	seed := startService(t, ctx)
	client := startService(t, ctx)
	// Replace the real responder: this peer is hostile, not misconfigured.
	seed.host.HandleRelease(func(context.Context, peer.ID, []byte) ([]byte, error) {
		return []byte{0x42}, nil
	})
	if err := client.host.Connect(ctx, seed.host.AddrInfo()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	_, err := client.PeerReleaseChunk(ctx, seed.host.PeerID().String(), 0, 128<<20)
	if err == nil {
		t.Fatal("a one-byte answer to a 1 MiB request was accepted as progress")
	}
}

func TestReleaseManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if m := LoadReleaseManifest(dir); m.Version != "" {
		t.Fatal("missing file should read as an empty manifest")
	}
	want := ReleaseManifest{Version: "v1.2.3", Asset: "a", Size: 7, Sums: []byte("s"), Sig: []byte("g")}
	if err := SaveReleaseManifest(dir, want); err != nil {
		t.Fatal(err)
	}
	if got := LoadReleaseManifest(dir); got.Version != want.Version || got.Size != want.Size ||
		!bytes.Equal(got.Sums, want.Sums) || !bytes.Equal(got.Sig, want.Sig) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	// Corrupt on disk reads as "nothing to offer", not as a crash.
	if err := os.WriteFile(releaseManifestPath(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if m := LoadReleaseManifest(dir); m.Version != "" {
		t.Fatal("corrupt manifest should read as empty")
	}
}
