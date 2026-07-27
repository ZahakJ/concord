package bridge

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// The replay: someone with write access to the dist repo re-uploads a genuine,
// correctly-signed OLD asset under a new tag, with that release's own genuine
// SHA256SUMS beside it. No byte is forged, so every check the updater makes
// passes — and the user is downgraded into whatever hole v1.0.0 had, which is
// exactly what signing is supposed to survive. The only thing that can refuse
// it is the version the asset itself is stamped with.
func TestGitHubReplayOfAnOldSignedRelease(t *testing.T) {
	nativeTrack(t, false)
	key, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const asset = "concord-linux-amd64-v1.0.0"
	const tag = "v9.9.9" // attacker-chosen, and the only thing that says "new"
	sums := []byte("aa11  " + asset + "\n")
	sig := ed25519.Sign(sk, sums)

	// Everything the replay gets for free.
	if !semverLess("v1.2.0", tag) {
		t.Fatal("the tag would not even look like an update")
	}
	url, picked := matchAsset("linux", []assetRef{{Name: asset, URL: "https://example.invalid/" + asset}})
	if picked != asset || url == "" {
		t.Fatalf("matchAsset picked %q from %q", picked, url)
	}
	if err := verifyWithKey(key, sums, sig); err != nil {
		t.Fatalf("the old release's signature: %v (it is genuine, it must verify)", err)
	}
	if got, err := sumForAsset(sums, picked); err != nil || got != "aa11" {
		t.Fatalf("sumForAsset = %q, %v (the checksum matches too)", got, err)
	}

	// ...and the check that refuses it.
	if err := vetReleaseAsset(tag, picked); err != errAssetVersionSkew {
		t.Fatalf("a v1.0.0 asset was installable as %s: vetReleaseAsset = %v", tag, err)
	}
	// The same asset under its own tag is the update we do install, whether or
	// not GitHub was handed the leading v.
	for _, honest := range []string{"v1.0.0", "1.0.0"} {
		if err := vetReleaseAsset(honest, picked); err != nil {
			t.Fatalf("genuine %s release refused: %v", honest, err)
		}
	}
	// Nothing to bind against is a refusal, not a pass.
	for _, c := range []struct{ latest, asset string }{
		{tag, "concord-9.9.9-android.apk"}, // no vX.Y.Z stamp in the name
		{"dev", asset},                     // unstamped build
		{"", asset},
	} {
		if err := vetReleaseAsset(c.latest, c.asset); err != errAssetVersionSkew {
			t.Fatalf("vetReleaseAsset(%q, %q) = %v, want a refusal", c.latest, c.asset, err)
		}
	}
}

// Preemption cancels the loser, it does not wait for it, so two runs are alive
// on one executable for as long as it takes the loser to notice. When both
// downloaded to exe+".new", the loser's cleanup deleted the winner's finished
// download — and while both were running, the winner's file was not necessarily
// the bytes it had hashed.
func TestOverlappingUpdateRunsDoNotShareATempFile(t *testing.T) {
	idleUpdater(t)
	b := &Bridge{ctx: context.Background()}
	exe := filepath.Join(t.TempDir(), "concord")

	fromPeer, ok := b.beginUpdate(false)
	if !ok {
		t.Fatal("could not start a peer attempt from idle")
	}
	fromGitHub, ok := b.beginUpdate(true) // preempts, does not join
	if !ok {
		t.Fatal("the GitHub path was blocked by the peer attempt")
	}
	if fromPeer.tempPath(exe) == fromGitHub.tempPath(exe) {
		t.Fatalf("both runs download to %s", fromGitHub.tempPath(exe))
	}

	// The winner's download, complete and about to be verified.
	payload := []byte(strings.Repeat("the new build\n", 500))
	if err := os.WriteFile(fromGitHub.tempPath(exe), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	// The loser noticing its cancellation and tidying up after itself.
	os.Remove(fromPeer.tempPath(exe))

	got, err := hashFile(fromGitHub.tempPath(exe))
	if err != nil {
		t.Fatalf("the cancelled run destroyed the live download: %v", err)
	}
	if want := sha256hex(payload); got != want {
		t.Fatalf("live download hashes %s, want %s", got, want)
	}
}

// What gets installed must be what was verified. Hashing the download as it
// streamed past proved something about memory, not about the file the swap
// picks up — so a file that differs from the verified bytes has to be caught
// here, whatever produced the difference.
func TestVerifyAndInstallHashesTheFileNotTheStream(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "concord")
	running := []byte("the binary this process is running")
	if err := os.WriteFile(exe, running, 0o755); err != nil {
		t.Fatal(err)
	}
	verified := []byte(strings.Repeat("the bytes the signed manifest covers\n", 100))
	want := sha256hex(verified)

	tmp := filepath.Join(dir, "concord.new-1")
	if err := os.WriteFile(tmp, []byte("some other build entirely"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyAndInstall(tmp, exe, want); err != errChecksumMismatch {
		t.Fatalf("verifyAndInstall = %v, want %v", err, errChecksumMismatch)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(running) {
		t.Fatal("unverified bytes were installed over the running binary")
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatal("a rejected download was left on disk")
	}

	// The matching file does install, and what lands is byte-for-byte what the
	// hash was taken over.
	if err := os.WriteFile(tmp, verified, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyAndInstall(tmp, exe, want); err != nil {
		t.Fatalf("verifyAndInstall: %v", err)
	}
	got, err := hashFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("installed binary hashes %s, verified %s", got, want)
	}
}
