package bridge

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/version"
)

// Updating from a PEER instead of from GitHub.
//
// GitHub can be unreachable, rate-limited, blocked, or simply down on the day a
// security fix ships. A peer we are already connected to may well be running
// the newer build already, and can hand us the exact same bytes. What makes
// that acceptable is signing: the release manifest is signed with an offline
// key whose public half is compiled into this binary, so a peer is a courier,
// not an authority. It cannot forge a release; the worst it can do is refuse to
// serve one, which is why GitHub stays as a source rather than being replaced —
// a peer who withholds an update must not be able to silence it.
//
// Everything here fails closed. The differences from the GitHub path are all in
// the strict direction:
//
//   - a build with no embedded key refuses peer updates outright. The GitHub
//     path keeps a checksum-only fallback for builds made before signing
//     existed, because there the transport is still TLS to a known host. From a
//     peer the checksum and the bytes come from the same untrusted place, so
//     checksum-only would prove nothing at all.
//   - the asset must match this machine exactly (assetForThisMachine), with no
//     "best available" fallback.
//
// The other rule this path pioneered — the version stamped in the asset
// FILENAME must equal the version claimed, which is what stops a
// genuinely-signed older release being replayed under a newer number — is no
// longer a difference: the GitHub path enforces it too (vetReleaseAsset).

var (
	errPeerNoKey       = errors.New("this build has no release key, so peer updates can't be verified")
	errPeerDowngrade   = errors.New("peer offers an older or equal version")
	errPeerNotSemver   = errors.New("peer offers an unrecognizable version")
	errPeerPlatform    = errors.New("peer's build is for a different platform")
	errPeerVersionSkew = errors.New("peer's asset name does not match the version it claims")
	errPeerSize        = errors.New("peer claims an implausible download size")
)

// The offer is an unauthenticated claim, so these are the only things standing
// between one click and however much traffic a clique of peers fancies. Sized
// against reality rather than generously: our biggest asset today is the
// unstripped Windows build at ~63 MiB, so maxPeerBinary is roughly double the
// largest thing we ship, and maxPeerAttempts means a mob of colluding peers
// costs the same as three of them.
const (
	maxPeerBinary   = 128 << 20 // 128 MiB
	maxPeerAttempts = 3
)

// peerDownloadBudget is the wall clock one peer gets to hand over one asset.
// Each chunk request already has its own timeout, but a hundred-odd of them at
// a minute apiece is a couple of hours of "downloading" — long enough to matter
// even with the GitHub path able to preempt it. Same ten minutes the GitHub
// client allows for the same bytes, so it is no stricter about slow links than
// the path it is standing in for.
const peerDownloadBudget = 10 * time.Minute

// PeerUpdateView is the UI payload for "a peer on your network has a newer
// build". Deliberately shaped like UpdateView's essentials, minus release
// notes and links — a peer serves bytes, not a web page.
type PeerUpdateView struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Asset     string `json:"asset"`
	Size      int64  `json:"size"`
	Peers     int    `json:"peers"` // how many peers can serve it
}

// vetPeerOffer decides whether an offer is even worth a round trip. Every check
// is a refusal to install, not a preference — see the file comment.
func vetPeerOffer(goos, goarch, current string, o appsvc.ReleaseOffer) error {
	if !releaseSigned() {
		return errPeerNoKey
	}
	if !isSemver(current) || !isSemver(o.Version) {
		return errPeerNotSemver
	}
	if !semverLess(current, o.Version) {
		return errPeerDowngrade
	}
	if !assetForThisMachine(goos, goarch, o.Asset) {
		return errPeerPlatform
	}
	if assetVersion(o.Asset) != o.Version {
		return errPeerVersionSkew
	}
	if o.Size <= 0 || o.Size > maxPeerBinary {
		return errPeerSize
	}
	return nil
}

// acceptablePeerOffers filters offers down to ones we would install and orders
// them newest first, so the caller tries the best candidate before any fallback.
// One offer per peer: the install loop is capped at a handful of attempts, and
// a peer that could spend them all by answering twice would make the cap
// meaningless.
func acceptablePeerOffers(goos, goarch, current string, offers []appsvc.ReleaseOffer) []appsvc.ReleaseOffer {
	var ok []appsvc.ReleaseOffer
	seen := map[string]bool{}
	for _, o := range offers {
		if seen[o.PeerID] {
			continue
		}
		if vetPeerOffer(goos, goarch, current, o) == nil {
			seen[o.PeerID] = true
			ok = append(ok, o)
		}
	}
	sort.SliceStable(ok, func(i, j int) bool { return semverLess(ok[j].Version, ok[i].Version) })
	return ok
}

// peerAttempts is the slice of offers runPeerUpdate will actually try. Every
// attempt is a whole binary pulled on nothing but a peer's word about its size,
// so a crowd must not be able to turn one click into a crowd's worth of
// downloads. Offers arrive newest first, so the cap spends what it has on the
// best candidates and then gives up rather than working through the queue.
func peerAttempts(offers []appsvc.ReleaseOffer) []appsvc.ReleaseOffer {
	if len(offers) > maxPeerAttempts {
		return offers[:maxPeerAttempts]
	}
	return offers
}

// peerTrustedSum is the whole trust decision for the peer path in one place:
// verify the signature over the manifest, THEN read the asset's hash out of it.
// A nil key is a hard stop here — unlike verifyReleaseSums, which stays
// permissive for keyless builds fetching over TLS from a known host.
func peerTrustedSum(key ed25519.PublicKey, sums, sig []byte, asset string) (string, error) {
	if key == nil {
		return "", errPeerNoKey
	}
	if err := verifyWithKey(key, sums, sig); err != nil {
		return "", err
	}
	return sumForAsset(sums, asset)
}

// CheckPeerUpdate reports whether any connected peer holds a newer build this
// machine could install. Like CheckForUpdate it is a soft no-op when there is
// nothing to say — locked, offline, or a dev build.
func (b *Bridge) CheckPeerUpdate() (PeerUpdateView, error) {
	view := PeerUpdateView{Current: version.Version}
	svc, err := b.service()
	if err != nil {
		return view, nil
	}
	offers := acceptablePeerOffers(runtime.GOOS, runtime.GOARCH, view.Current, svc.PeerReleaseOffers(b.ctx))
	if len(offers) == 0 {
		return view, nil
	}
	best := offers[0]
	for _, o := range offers {
		if o.Version == best.Version {
			view.Peers++
		}
	}
	view.Available = true
	view.Latest = best.Version
	view.Asset = best.Asset
	view.Size = best.Size
	return view, nil
}

// ApplyPeerUpdate installs a newer build fetched from a peer. It shares
// UpdateProgress with the GitHub path — only one update can be in flight, and
// the UI polls the same place either way.
func (b *Bridge) ApplyPeerUpdate() error {
	// Never preempts: the peer path is the fallback, so it waits its turn rather
	// than interrupting a GitHub download that is already most of the way there.
	run, ok := b.beginUpdate(false)
	if !ok {
		return nil // already running
	}
	go b.runPeerUpdate(run)
	return nil
}

func (b *Bridge) runPeerUpdate(run *updateRun) {
	svc, err := b.service()
	if err != nil {
		run.fail("log in first — peer updates need the network running")
		return
	}
	if !b.CanSelfUpdate() {
		run.fail("this installation can't update itself (read-only location or dev build)")
		return
	}
	// Re-ask rather than trusting whatever the UI last saw: offers go stale as
	// peers come and go, and this is the list we actually install from.
	offers := acceptablePeerOffers(runtime.GOOS, runtime.GOARCH, version.Version, svc.PeerReleaseOffers(run.ctx))
	if len(offers) == 0 {
		run.fail("no peer is offering a newer build for this machine")
		return
	}
	exe, err := os.Executable()
	if err != nil {
		run.fail("locating executable: %v", err)
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	lastErr := errors.New("no peer completed the transfer")
	for _, o := range peerAttempts(offers) {
		if err := b.installFromPeer(run, svc, o, exe); err != nil {
			lastErr = err
			continue // a peer that lies or dies costs us one attempt, not the update
		}
		run.set(UpdateProgress{Phase: "ready", Percent: 100, Version: o.Version})
		return
	}
	run.fail("peer update: %v", lastErr)
}

// installFromPeer runs one candidate end to end. The signature is checked
// BEFORE the binary is fetched: if a peer can't produce a manifest we trust,
// there is no point spending tens of megabytes finding out.
func (b *Bridge) installFromPeer(run *updateRun, svc *appsvc.Service, o appsvc.ReleaseOffer, exe string) error {
	run.set(UpdateProgress{Phase: "verifying", Version: o.Version})
	ms, err := svc.PeerReleaseManifest(run.ctx, o.PeerID)
	if err != nil {
		return err
	}
	want, err := peerTrustedSum(releasePubKey(), ms.Sums, ms.Sig, o.Asset)
	if err != nil {
		return err
	}

	tmp := run.tempPath(exe)
	if err := b.downloadFromPeer(run, svc, o, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	run.set(UpdateProgress{Phase: "verifying", Percent: 100, Version: o.Version})
	if err := verifyAndInstall(tmp, exe, want); err != nil {
		return err
	}
	// We now hold bytes we verified ourselves, so we can pass them on. exe is
	// the path we just wrote, not one re-derived after the swap — see
	// recordServableRelease.
	recordServableRelease(exe, o.Version, o.Asset, releaseSums{sums: ms.Sums, sig: ms.Sig})
	return nil
}

// downloadFromPeer streams the offered asset into path.
//
// Two rules keep a hostile peer from parking here forever. Every reply must be
// exactly as long as the request asked for, which is what an honest seeder
// always sends (serveRelease clamps to the chunk size and the remaining bytes,
// nothing else) — without it a peer can answer a byte at a time and turn
// a 128 MiB asset into 134 million round trips that never error and never
// finish. And the whole transfer shares one budget, so a peer that is merely
// glacial is also finite.
func (b *Bridge) downloadFromPeer(run *updateRun, svc *appsvc.Service, o appsvc.ReleaseOffer, path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(run.ctx, peerDownloadBudget)
	defer cancel()

	var done int64
	for done < o.Size {
		chunk, err := svc.PeerReleaseChunk(ctx, o.PeerID, done, o.Size-done)
		if err != nil {
			return err
		}
		if _, err := f.Write(chunk); err != nil {
			return err
		}
		done += int64(len(chunk))
		run.set(UpdateProgress{
			Phase:   "downloading",
			Percent: int(done * 100 / o.Size),
			Version: o.Version,
		})
	}
	return nil
}

// --- becoming a source ------------------------------------------------------

// seedMu serializes the read-modify-write of release.json. Login fires
// adoptOwnRelease in the background and the user can hit Update at any moment,
// so the two really do overlap.
var seedMu sync.Mutex

// recordServableRelease notes that the binary at exe is a specific,
// signature-covered release asset, which is what lets this node serve it to
// peers. Best-effort: failing to become a source must never fail an update.
//
// exe is a parameter and not os.Executable() because the only caller that
// matters here has just renamed the running binary out of the way. On Linux
// os.Executable() is a live readlink of /proc/self/exe, so after installBinary
// it resolves to exe.old — recording the OLD binary's size against the NEW
// version. The node then advertises the new release while serving the old
// bytes (every peer downloads the lot and fails the checksum), and after the
// restart the size never matches again, so it never seeds at all.
func recordServableRelease(exe, ver, asset string, rs releaseSums) {
	dir, err := appsvc.DataDir()
	if err != nil {
		return
	}
	fi, err := os.Stat(exe)
	if err != nil {
		return
	}
	saveServable(dir, appsvc.ReleaseManifest{
		Version: ver,
		Asset:   asset,
		Size:    fi.Size(),
		Sums:    rs.sums,
		Sig:     rs.sig,
	})
}

// saveServable writes the seed record unless what is already on disk names a
// NEWER release. That single rule settles an ordering that is otherwise very
// easy to lose: adoptOwnRelease runs on every login and spends seconds hashing
// the executable, while version.Version stays at the OLD value from an update
// until the process restarts — so between install and restart it would
// cheerfully conclude "we can seed <old version>" and overwrite the record the
// updater just wrote for the binary actually on disk. Equal versions may
// overwrite: that is adoptOwnRelease upgrading its own "nothing to seed"
// marker into a real record.
func saveServable(dir string, m appsvc.ReleaseManifest) {
	seedMu.Lock()
	defer seedMu.Unlock()
	if on := appsvc.LoadReleaseManifest(dir); semverLess(m.Version, on.Version) {
		return
	}
	_ = appsvc.SaveReleaseManifest(dir, m)
}

// adoptOwnRelease lets a node that installed by hand — downloaded the binary,
// never ran the updater — still seed its peers. It fetches the manifest for the
// version it is running and records nothing unless the executable's own hash
// matches what that manifest says, so a locally-built or tampered binary
// quietly declines to become a source rather than poisoning one.
//
// It does NOT require this build to carry a release key. A keyless build is
// still a perfectly good courier: it passes on the signature it fetched from
// GitHub alongside the bytes, and whoever installs them does their own
// verification. Refusing to relay would cost availability and buy nothing.
//
// Best-effort, and it needs GitHub — which is exactly what a blocked node does
// not have. Those become sources the moment they take an update from a peer
// instead (see installFromPeer).
func adoptOwnRelease() {
	cur := version.Version
	if !isSemver(cur) {
		return
	}
	dir, err := appsvc.DataDir()
	if err != nil {
		return
	}
	// The record is keyed by version, and a version-only record means "we
	// looked, this isn't a published build" — a conclusion, so we don't re-ask
	// GitHub at every login. Only reached after GitHub actually answered;
	// network failures leave nothing behind and are retried.
	//
	// A record for a NEWER version is an update that has installed but not
	// restarted yet: the binary on disk is already that release, so there is
	// nothing here to work out and hashing it would only produce a wrong answer.
	if m := appsvc.LoadReleaseManifest(dir); m.Version == cur || semverLess(cur, m.Version) {
		return
	}
	rs, err := fetchReleaseSumsFor("tags/" + cur)
	if err != nil {
		return
	}
	nothingToSeed := func() { saveServable(dir, appsvc.ReleaseManifest{Version: cur}) }

	asset, want := assetFromSums(rs.sums)
	if asset == "" || assetVersion(asset) != cur {
		nothingToSeed()
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	sum, err := hashFile(exe)
	if err != nil {
		return
	}
	if !strings.EqualFold(sum, want) {
		nothingToSeed()
		return
	}
	recordServableRelease(exe, cur, asset, rs)
}

// assetFromSums finds this machine's asset in a checksum manifest. SHA256SUMS
// lists every file in the release, so it doubles as the asset index and saves a
// second API call.
func assetFromSums(sums []byte) (asset, sum string) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if assetForThisMachine(runtime.GOOS, runtime.GOARCH, name) {
			return name, fields[0]
		}
	}
	return "", ""
}
