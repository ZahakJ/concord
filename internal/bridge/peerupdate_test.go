package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	appsvc "github.com/zahak/concord/internal/app"
)

// armSigning gives the build under test a real embedded key, since most peer
// checks are no-ops on a keyless build (which is itself the point of
// TestPeerUpdateRefusedWithoutKey).
func armSigning(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	saved := releasePubKeyFile
	t.Cleanup(func() { releasePubKeyFile = saved })
	releasePubKeyFile = hex.EncodeToString(pub) + "\n"
	return pub, priv
}

func nativeTrack(t *testing.T, native bool) {
	t.Helper()
	saved := NativeBuild
	t.Cleanup(func() { NativeBuild = saved })
	NativeBuild = native
}

// The peer path's whole safety argument is the signature, so each way it can be
// dodged gets a case. Unlike the GitHub path there is NO checksum-only mode: a
// peer supplies both the manifest and the bytes, so an unverified manifest
// proves nothing about either.
func TestPeerTrustedSum(t *testing.T) {
	key, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	asset := "concord-linux-amd64-v1.2.3"
	sums := []byte("aa11  " + asset + "\nbb22  SHA256SUMS.sig\n")
	sig := ed25519.Sign(sk, sums)

	got, err := peerTrustedSum(key, sums, sig, asset)
	if err != nil || got != "aa11" {
		t.Fatalf("genuine manifest: got %q, %v", got, err)
	}
	// Hash swapped after signing — the attack the signature exists to stop.
	tampered := []byte("dead  " + asset + "\n")
	if _, err := peerTrustedSum(key, tampered, sig, asset); err != errBadReleaseSignature {
		t.Fatalf("tampered manifest accepted: %v", err)
	}
	// Signed, but by a key that isn't ours.
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := peerTrustedSum(key, sums, ed25519.Sign(other, sums), asset); err != errBadReleaseSignature {
		t.Fatalf("foreign signature accepted: %v", err)
	}
	// Signature simply omitted. Must not degrade to "checksums are enough".
	if _, err := peerTrustedSum(key, sums, nil, asset); err != errUnsignedRelease {
		t.Fatalf("missing signature accepted: %v", err)
	}
	// A build with no key: permissive on the GitHub path, a hard stop here.
	if _, err := peerTrustedSum(nil, sums, sig, asset); err != errPeerNoKey {
		t.Fatalf("keyless build accepted peer bytes: %v", err)
	}
	// Correctly signed manifest that just doesn't cover the asset on offer.
	if _, err := peerTrustedSum(key, sums, sig, "concord-linux-arm64-v1.2.3"); err == nil {
		t.Fatal("uncovered asset accepted")
	}
}

// A keyless build must never install from a peer at all, however plausible the
// offer looks.
func TestPeerUpdateRefusedWithoutKey(t *testing.T) {
	nativeTrack(t, false)
	saved := releasePubKeyFile
	t.Cleanup(func() { releasePubKeyFile = saved })
	releasePubKeyFile = "# no key in this build\n"

	o := appsvc.ReleaseOffer{
		PeerID:  "peerA",
		Version: "v1.3.0",
		Asset:   "concord-linux-amd64-v1.3.0",
		Size:    1 << 20,
	}
	if err := vetPeerOffer("linux", "amd64", "v1.2.0", o); err != errPeerNoKey {
		t.Fatalf("keyless build accepted an offer: %v", err)
	}
}

func TestVetPeerOfferDowngrade(t *testing.T) {
	armSigning(t)
	nativeTrack(t, false)

	cases := []struct {
		name    string
		current string
		offer   appsvc.ReleaseOffer
		want    error
	}{
		{
			"newer is fine", "v1.2.0",
			appsvc.ReleaseOffer{Version: "v1.3.0", Asset: "concord-linux-amd64-v1.3.0", Size: 1 << 20},
			nil,
		},
		{
			"same version is not an update", "v1.2.0",
			appsvc.ReleaseOffer{Version: "v1.2.0", Asset: "concord-linux-amd64-v1.2.0", Size: 1 << 20},
			errPeerDowngrade,
		},
		{
			"older is a downgrade attack", "v1.2.0",
			appsvc.ReleaseOffer{Version: "v1.1.9", Asset: "concord-linux-amd64-v1.1.9", Size: 1 << 20},
			errPeerDowngrade,
		},
		{
			// 10 > 9 numerically; a string compare would call this a downgrade.
			"numeric, not lexical", "v1.9.0",
			appsvc.ReleaseOffer{Version: "v1.10.0", Asset: "concord-linux-amd64-v1.10.0", Size: 1 << 20},
			nil,
		},
		{
			// The replay: a genuinely signed OLD release, relabelled. The
			// signature over its SHA256SUMS would verify perfectly, so the only
			// thing standing in the way is the tag inside the asset filename.
			"old release relabelled as new", "v1.2.0",
			appsvc.ReleaseOffer{Version: "v9.9.9", Asset: "concord-linux-amd64-v1.1.0", Size: 1 << 20},
			errPeerVersionSkew,
		},
		{
			"unparseable version", "v1.2.0",
			appsvc.ReleaseOffer{Version: "v1.3", Asset: "concord-linux-amd64-v1.3.0", Size: 1 << 20},
			errPeerNotSemver,
		},
		{
			"dev build has nothing to compare against", "dev",
			appsvc.ReleaseOffer{Version: "v1.3.0", Asset: "concord-linux-amd64-v1.3.0", Size: 1 << 20},
			errPeerNotSemver,
		},
		{
			"implausible size", "v1.2.0",
			appsvc.ReleaseOffer{Version: "v1.3.0", Asset: "concord-linux-amd64-v1.3.0", Size: 1 << 40},
			errPeerSize,
		},
		{
			// Under the old 256 MiB ceiling this was accepted, so a peer could
			// bill us four times the largest binary we have ever published.
			"twice what we ship is already too much", "v1.2.0",
			appsvc.ReleaseOffer{Version: "v1.3.0", Asset: "concord-linux-amd64-v1.3.0", Size: 200 << 20},
			errPeerSize,
		},
		{
			"a real asset still fits", "v1.2.0",
			appsvc.ReleaseOffer{Version: "v1.3.0", Asset: "concord-linux-amd64-v1.3.0", Size: 70 << 20},
			nil,
		},
		{
			"zero size", "v1.2.0",
			appsvc.ReleaseOffer{Version: "v1.3.0", Asset: "concord-linux-amd64-v1.3.0", Size: 0},
			errPeerSize,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := vetPeerOffer("linux", "amd64", c.current, c.offer); err != c.want {
				t.Fatalf("vetPeerOffer = %v, want %v", err, c.want)
			}
		})
	}
}

// A peer's build is only useful to us if it is for our exact OS, architecture
// and build track. matchAsset may fall back to a best-available asset when it
// is choosing within one release; here there is nothing to fall back to.
func TestAssetForThisMachine(t *testing.T) {
	web := []struct {
		goos, goarch, name string
		want               bool
	}{
		{"linux", "amd64", "concord-linux-amd64-v0.9.0", true},
		{"linux", "arm64", "concord-linux-arm64-v0.9.0", true},
		{"linux", "arm64", "concord-linux-amd64-v0.9.0", false}, // wrong arch, no fallback
		{"linux", "amd64", "concord-macos-arm64-v0.9.0", false}, // wrong OS
		{"linux", "amd64", "concord-desktop-linux-v0.9.0", false},
		{"windows", "amd64", "concord-windows-v0.9.0.exe", true},
		{"windows", "arm64", "concord-windows-v0.9.0.exe", false}, // we only ship amd64 for Windows
		{"windows", "amd64", "concord-windows-v0.9.0", false},     // must be executable
		{"windows", "amd64", "WINDOWS.md", false},                 // docs ride along in the release
		{"windows", "amd64", "Concord-Setup-v0.9.0.exe", false},   // installer, not the app
		{"darwin", "amd64", "concord-macos-intel-v0.9.0", true},   // amd64 macs are "intel"
		{"darwin", "arm64", "concord-macos-arm64-v0.9.0", true},
		{"darwin", "arm64", "concord-macos-intel-v0.9.0", false},
		{"android", "arm64", "concord-0.9.0-android.apk", false}, // the OS installs this, not us
		{"plan9", "amd64", "concord-linux-amd64-v0.9.0", false},
		{"linux", "amd64", "SHA256SUMS", false},
	}
	nativeTrack(t, false)
	for _, c := range web {
		if got := assetForThisMachine(c.goos, c.goarch, c.name); got != c.want {
			t.Errorf("web assetForThisMachine(%s/%s, %q) = %v, want %v", c.goos, c.goarch, c.name, got, c.want)
		}
	}

	native := []struct {
		goos, goarch, name string
		want               bool
	}{
		{"linux", "amd64", "concord-desktop-linux-v0.9.0", true},
		// Desktop assets carry no arch token, and every one we publish is built
		// on the maintainer's amd64 machine — so a name that says nothing says
		// amd64, and an arm64 desktop must refuse it rather than install an
		// executable it cannot run.
		{"linux", "arm64", "concord-desktop-linux-v0.9.0", false},
		{"linux", "arm64", "concord-desktop-linux-arm64-v0.9.0", true}, // ...unless it does say
		{"linux", "amd64", "concord-desktop-linux-arm64-v0.9.0", false},
		{"linux", "amd64", "concord-linux-amd64-v0.9.0", false}, // web asset would replace the window with a server
		{"windows", "amd64", "concord-desktop-windows-v0.9.0.exe", true},
		{"windows", "arm64", "concord-desktop-windows-v0.9.0.exe", false},
		{"darwin", "arm64", "concord-desktop-macos-v0.9.0.zip", false}, // a bundle can't be swapped in place
	}
	NativeBuild = true
	for _, c := range native {
		if got := assetForThisMachine(c.goos, c.goarch, c.name); got != c.want {
			t.Errorf("native assetForThisMachine(%s/%s, %q) = %v, want %v", c.goos, c.goarch, c.name, got, c.want)
		}
	}
}

func TestAssetVersion(t *testing.T) {
	for name, want := range map[string]string{
		"concord-linux-amd64-v0.9.0":          "v0.9.0",
		"concord-desktop-windows-v1.10.2.exe": "v1.10.2",
		"concord-0.9.0-android.apk":           "", // no v-prefixed tag: not peer-installable
		"SHA256SUMS":                          "",
	} {
		if got := assetVersion(name); got != want {
			t.Errorf("assetVersion(%q) = %q, want %q", name, got, want)
		}
	}
}

// The install loop takes offers in order, so ordering is a correctness
// property, not cosmetics: the best candidate must be tried first and the
// unusable ones must never be tried at all.
func TestAcceptablePeerOffers(t *testing.T) {
	armSigning(t)
	nativeTrack(t, false)

	offers := []appsvc.ReleaseOffer{
		{PeerID: "a", Version: "v1.3.0", Asset: "concord-linux-amd64-v1.3.0", Size: 1 << 20},
		{PeerID: "b", Version: "v1.9.0", Asset: "concord-linux-arm64-v1.9.0", Size: 1 << 20}, // wrong arch
		{PeerID: "c", Version: "v1.4.0", Asset: "concord-linux-amd64-v1.4.0", Size: 1 << 20},
		{PeerID: "d", Version: "v1.1.0", Asset: "concord-linux-amd64-v1.1.0", Size: 1 << 20}, // older
		{PeerID: "e", Version: "v1.4.0", Asset: "concord-linux-amd64-v1.4.0", Size: 1 << 20},
	}
	got := acceptablePeerOffers("linux", "amd64", "v1.2.0", offers)
	want := []string{"c", "e", "a"}
	if len(got) != len(want) {
		t.Fatalf("got %d offers, want %d: %+v", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i].PeerID != id {
			t.Fatalf("offer %d = %s, want %s (order: %+v)", i, got[i].PeerID, id, got)
		}
	}
	// Nothing on offer for a machine that is already current.
	if got := acceptablePeerOffers("linux", "amd64", "v9.0.0", offers); len(got) != 0 {
		t.Fatalf("current machine got %d offers", len(got))
	}
}

// One click must not become one download per peer who fancies being asked.
// Both halves matter: a peer answering twice would otherwise eat the whole
// attempt budget by itself.
func TestPeerAttemptsAreCapped(t *testing.T) {
	armSigning(t)
	nativeTrack(t, false)

	var mob []appsvc.ReleaseOffer
	for i := range 50 {
		mob = append(mob, appsvc.ReleaseOffer{
			PeerID:  string(rune('a' + i%5)), // five peers, ten answers each
			Version: "v1.3.0",
			Asset:   "concord-linux-amd64-v1.3.0",
			Size:    maxPeerBinary,
		})
	}
	offers := acceptablePeerOffers("linux", "amd64", "v1.2.0", mob)
	if len(offers) != 5 {
		t.Fatalf("acceptablePeerOffers kept %d of 50 answers from 5 peers, want 5", len(offers))
	}
	tries := peerAttempts(offers)
	if len(tries) != maxPeerAttempts {
		t.Fatalf("would attempt %d downloads, want at most %d", len(tries), maxPeerAttempts)
	}
	// The bytes a mob can bill us for, worst case, is what actually changed:
	// 50 x 256 MiB before, 3 x 128 MiB now.
	if worst := int64(len(tries)) * maxPeerBinary; worst > 384<<20 {
		t.Fatalf("worst-case download is %d MiB", worst>>20)
	}
}

// Install-then-record, replayed exactly as installFromPeer and runUpdate do it.
// The trap is that installBinary renames the running executable out of the way
// first, so anything that re-derives its own path afterwards — os.Executable()
// is a live readlink of /proc/self/exe on Linux — measures the OLD binary. That
// record makes the node advertise the new version while serving the old bytes,
// and once it restarts the size never matches again, so it never seeds at all.
func TestRecordServableReleaseMeasuresTheInstalledBinary(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CONCORD_HOME", data)

	install := t.TempDir()
	exe := filepath.Join(install, "concord")
	old := []byte(strings.Repeat("old binary\n", 1000))
	fresh := []byte(strings.Repeat("NEW BINARY\n", 3000))
	if err := os.WriteFile(exe, old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe+".new", fresh, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := installBinary(exe+".new", exe); err != nil {
		t.Fatalf("installBinary: %v", err)
	}
	recordServableRelease(exe, "v1.3.0", "concord-linux-amd64-v1.3.0",
		releaseSums{sums: []byte("aa11  concord-linux-amd64-v1.3.0\n"), sig: []byte("sig")})

	m := appsvc.LoadReleaseManifest(data)
	if m.Size != int64(len(fresh)) {
		t.Fatalf("release.json records Size=%d for an asset of size %d (old binary was %d)",
			m.Size, len(fresh), len(old))
	}
	if m.Version != "v1.3.0" || m.Asset != "concord-linux-amd64-v1.3.0" {
		t.Fatalf("recorded %s/%s", m.Version, m.Asset)
	}
}

// adoptOwnRelease runs on every login and ends by recording version.Version —
// which, between an update installing and the process restarting, is still the
// OLD version while release.json already names the new one. Whoever writes last
// wins, and the write it would undo is the one that lets this node seed the
// release it actually holds.
func TestSeedRecordNeverGoesBackwards(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CONCORD_HOME", data)

	exe := filepath.Join(t.TempDir(), "concord")
	if err := os.WriteFile(exe, []byte("installed binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	sums := releaseSums{sums: []byte("aa11  concord-linux-amd64-v1.3.0\n"), sig: []byte("sig")}
	recordServableRelease(exe, "v1.3.0", "concord-linux-amd64-v1.3.0", sums)

	// What a login-time adopt would write for the version still stamped in this
	// process, and its "this build isn't published" marker.
	recordServableRelease(exe, "v1.2.0", "concord-linux-amd64-v1.2.0", sums)
	saveServable(data, appsvc.ReleaseManifest{Version: "v1.2.0"})

	if m := appsvc.LoadReleaseManifest(data); m.Version != "v1.3.0" || m.Asset == "" {
		t.Fatalf("the record for the installed release was clobbered: %+v", m)
	}
	// A record for the same version may be replaced — that is adopt turning its
	// own negative marker into a real one.
	saveServable(data, appsvc.ReleaseManifest{Version: "v1.3.0", Asset: "a", Size: 1, Sums: []byte("s")})
	if m := appsvc.LoadReleaseManifest(data); m.Asset != "a" {
		t.Fatalf("same-version record refused: %+v", m)
	}
}

// Login fires adoptOwnRelease in the background while the user is free to hit
// Update, so the two writers really do overlap. Run under -race.
func TestSeedRecordWritesAreSerialized(t *testing.T) {
	data := t.TempDir()
	t.Setenv("CONCORD_HOME", data)

	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			saveServable(data, appsvc.ReleaseManifest{
				Version: fmt.Sprintf("v1.%d.0", i),
				Asset:   fmt.Sprintf("concord-linux-amd64-v1.%d.0", i),
				Size:    int64(i + 1),
				Sums:    []byte("aa11  x\n"),
			})
		}()
	}
	wg.Wait()
	if m := appsvc.LoadReleaseManifest(data); m.Version != "v1.19.0" {
		t.Fatalf("concurrent writers left %+v, want the newest version", m)
	}
}

// idleUpdater isolates a test from the process-wide progress slot.
func idleUpdater(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		updMu.Lock()
		updNow = UpdateProgress{Phase: "idle"}
		updAbort = func() {}
		updMu.Unlock()
	})
	updMu.Lock()
	updNow = UpdateProgress{Phase: "idle"}
	updMu.Unlock()
}

// The design goal is that a peer who withholds an update cannot silence it. A
// peer doesn't have to withhold anything to do that: it can simply stay in
// phase "downloading", and if the GitHub path deferred to whatever is running,
// the user would have no route left to install a fix until a restart. So GitHub
// takes the slot, the interrupted attempt is cancelled, and — the part that is
// easy to get wrong — its remaining progress reports are dropped instead of
// overwriting the run that replaced it.
func TestGitHubUpdatePreemptsAStuckPeerAttempt(t *testing.T) {
	idleUpdater(t)
	b := &Bridge{ctx: context.Background()}

	stuck, ok := b.beginUpdate(false)
	if !ok {
		t.Fatal("could not start a peer attempt from idle")
	}
	stuck.set(UpdateProgress{Phase: "downloading", Percent: 1, Version: "v1.3.0"})

	if _, ok := b.beginUpdate(false); ok {
		t.Fatal("a second peer attempt started alongside the first")
	}
	github, ok := b.beginUpdate(true)
	if !ok {
		t.Fatal("the GitHub path was blocked by a stuck peer attempt")
	}
	if stuck.ctx.Err() == nil {
		t.Fatal("the preempted peer attempt was left running")
	}

	// The loser is still executing: its next report must go nowhere.
	stuck.fail("peer gave up")
	github.set(UpdateProgress{Phase: "downloading", Percent: 40, Version: "v1.3.0"})
	stuck.set(UpdateProgress{Phase: "ready", Percent: 100})
	if got := b.UpdateState(); got.Phase != "downloading" || got.Percent != 40 {
		t.Fatalf("superseded attempt overwrote live progress: %+v", got)
	}
}

func TestSumForAsset(t *testing.T) {
	sums := []byte("aa11  concord-linux-amd64-v1.2.3\nbb22 *concord-windows-v1.2.3.exe\n\n")
	for asset, want := range map[string]string{
		"concord-linux-amd64-v1.2.3": "aa11",
		"concord-windows-v1.2.3.exe": "bb22", // sha256sum's binary-mode '*' prefix
	} {
		got, err := sumForAsset(sums, asset)
		if err != nil || got != want {
			t.Errorf("sumForAsset(%q) = %q, %v; want %q", asset, got, err, want)
		}
	}
	if _, err := sumForAsset(sums, "concord-macos-arm64-v1.2.3"); err == nil {
		t.Error("absent asset should be an error, not an empty hash")
	}
}
