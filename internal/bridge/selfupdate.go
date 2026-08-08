package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ZahakJ/concord/internal/version"
)

// Full self-update: download the matching release binary, verify it against
// the release's SHA256SUMS, and atomically swap it over the running
// executable. The running process keeps its (deleted/renamed) image until
// restart — RestartApp finishes the job. Works for the bare-binary builds
// (Linux/Windows web + desktop); the macOS .app zip and mobile stores are
// out of scope and fall back to the download page.

// UpdateProgress is the UI-pollable state of an in-flight self-update.
type UpdateProgress struct {
	Phase   string `json:"phase"` // idle | downloading | verifying | ready | error
	Percent int    `json:"percent"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

var (
	updMu  sync.Mutex
	updNow UpdateProgress = UpdateProgress{Phase: "idle"}
	// updGen names the attempt that currently owns updNow, and updAbort cancels
	// it. Never nil while an attempt is running (see beginUpdate).
	updGen   uint64
	updAbort context.CancelFunc = func() {}
)

// updateRun is one attempt's claim on the shared progress slot. Only one is
// meant to be running — they all end in the same rename over the same
// executable — but the claim is revocable, and a revoked run's progress is
// dropped rather than written, so cancelling an attempt can never leave it
// scribbling over the one that replaced it.
type updateRun struct {
	gen uint64
	ctx context.Context
}

// tempPath is where this run downloads to, before verification and the swap.
//
// Per-run, and not a shared exe+".new", because preemption cancels the loser
// without waiting for it: for as long as it takes to notice, two runs are alive
// on the same executable. Sharing one path let the loser's cleanup os.Remove
// the winner's finished download, and let both write the same file, so the
// bytes hashed were not necessarily the bytes installed.
func (r *updateRun) tempPath(exe string) string {
	return fmt.Sprintf("%s.new-%d", exe, r.gen)
}

// beginUpdate claims the progress slot, or reports that another attempt holds
// it.
//
// preempt is the whole difference between the two callers, and it is what stops
// a peer from silencing an update. A peer that drip-feeds the download keeps the
// phase at "downloading" for as long as its budget allows; if the GitHub path
// deferred to that, one hostile peer would leave the user no route at all to
// install a fix. So GitHub takes the slot from whatever is running, and the peer
// path — the fallback, not the authority — never does.
func (b *Bridge) beginUpdate(preempt bool) (*updateRun, bool) {
	updMu.Lock()
	defer updMu.Unlock()
	if updNow.Phase == "downloading" || updNow.Phase == "verifying" {
		if !preempt {
			return nil, false
		}
		updAbort()
	}
	ctx, cancel := context.WithCancel(b.ctx)
	updGen++
	updAbort = cancel
	updNow = UpdateProgress{Phase: "downloading"}
	return &updateRun{gen: updGen, ctx: ctx}, true
}

// set publishes progress, unless this run has already been superseded.
func (r *updateRun) set(p UpdateProgress) {
	updMu.Lock()
	defer updMu.Unlock()
	if r.gen == updGen {
		updNow = p
	}
}

func (r *updateRun) fail(format string, args ...any) {
	r.set(UpdateProgress{Phase: "error", Error: fmt.Sprintf(format, args...)})
}

// UpdateState reports self-update progress (poll while ApplyUpdate runs).
func (b *Bridge) UpdateState() UpdateProgress {
	updMu.Lock()
	defer updMu.Unlock()
	return updNow
}

// CanSelfUpdate reports whether this build can replace itself in place: a
// stamped release build, running from a bare binary we can write next to.
func (b *Bridge) CanSelfUpdate() bool {
	if !isSemver(version.Version) {
		return false // dev builds have nothing to update to
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// Inside a macOS .app bundle the release asset is a zip — not swappable.
	if runtime.GOOS == "darwin" && strings.Contains(exe, ".app/") {
		return false
	}
	// Probe writability of the binary's directory.
	probe := filepath.Join(filepath.Dir(exe), ".concord-upd-probe")
	f, err := os.Create(probe)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(probe)
	return true
}

// ApplyUpdate kicks off the download-verify-swap in the background; the UI
// polls UpdateState for progress. Returns immediately. It preempts an in-flight
// peer attempt (see beginUpdate) — GitHub is the route that must always be
// available.
func (b *Bridge) ApplyUpdate() error {
	run, ok := b.beginUpdate(true)
	if !ok {
		return nil // already running
	}
	go b.runUpdate(run)
	return nil
}

func (b *Bridge) runUpdate(run *updateRun) {
	// Re-resolve the latest release fresh (don't trust a stale banner).
	cv, err := b.CheckForUpdate()
	if err != nil || !cv.Available {
		run.fail("no update available")
		return
	}
	if cv.Download == "" {
		run.fail("no downloadable build for this platform — grab it from the release page")
		return
	}
	if strings.HasSuffix(strings.ToLower(cv.Asset), ".zip") {
		run.fail("this build updates via the release page (bundle, not a bare binary)")
		return
	}
	if err := vetReleaseAsset(cv.Latest, cv.Asset); err != nil {
		run.fail("%v", err)
		return
	}
	if !b.CanSelfUpdate() {
		run.fail("this installation can't update itself (read-only location or dev build)")
		return
	}
	run.set(UpdateProgress{Phase: "downloading", Version: cv.Latest})

	exe, err := os.Executable()
	if err != nil {
		run.fail("locating executable: %v", err)
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	tmp := run.tempPath(exe)

	if err := downloadWithProgress(run, cv.Download, tmp, cv.Latest); err != nil {
		os.Remove(tmp)
		run.fail("download: %v", err)
		return
	}

	// Verify the release's SIGNED SHA256SUMS, then the file against it — fail
	// closed at both steps.
	run.set(UpdateProgress{Phase: "verifying", Percent: 100, Version: cv.Latest})
	rs, err := fetchReleaseSums()
	if err != nil {
		os.Remove(tmp)
		run.fail("verify: %v", err)
		return
	}
	want, err := sumForAsset(rs.sums, cv.Asset)
	if err != nil {
		os.Remove(tmp)
		run.fail("verify: %v", err)
		return
	}
	if err := verifyAndInstall(tmp, exe, want); err != nil {
		run.fail("%v", err)
		return
	}
	recordServableRelease(exe, cv.Latest, cv.Asset, rs)
	run.set(UpdateProgress{Phase: "ready", Percent: 100, Version: cv.Latest})
}

// errAssetVersionSkew is a release whose asset is stamped with a different
// version from the one the release claims.
var errAssetVersionSkew = errors.New("release asset does not match the version it claims — refusing to install")

// vetReleaseAsset binds the download to the tag it arrives under.
//
// A signature proves who built an asset, not which release it belongs to. So
// anyone with write access to the dist repo can take a genuine, correctly
// signed OLD asset, upload it under a new tag with its own (equally genuine)
// SHA256SUMS, and every check downstream passes: the tag is newer, the asset
// matches this platform, the signature verifies, the checksum matches. The user
// is silently downgraded into whatever hole that version had — precisely the
// attack signing exists to survive.
//
// Every swappable asset carries its version in the filename, so the two claims
// can be compared, and a release that disagrees with itself is not installable.
// The peer path has always done this (vetPeerOffer); this is the same rule for
// GitHub, which is the path that actually runs today.
//
// Compared as parsed triples, not as strings: the tag is whatever GitHub was
// given, so "1.4.0" and "v1.4.0" name the same release and refusing that would
// break real updates rather than attacks. An asset with no version in its name
// (the APK) parses to nothing and is refused.
func vetReleaseAsset(latest, asset string) error {
	tag, ok1 := parseSemver(latest)
	stamped, ok2 := parseSemver(assetVersion(asset))
	if !ok1 || !ok2 || tag != stamped {
		return errAssetVersionSkew
	}
	return nil
}

// errChecksumMismatch is a download that isn't the bytes the signed manifest
// covers — a corrupt transfer, or a source substituting its own build.
var errChecksumMismatch = errors.New("download does not match the signed checksum — update aborted")

// verifyAndInstall is the last mile both update paths share: hash the FILE that
// is about to be installed, refuse it unless that is the hash the signed
// manifest names, and only then swap.
//
// Hashing the file rather than the stream that produced it is the whole point.
// A stream hash proves what went past in memory; the thing we are about to make
// the user run is what is on disk, and those parted company as soon as two runs
// could write one path. Re-reading costs one pass over ~60 MiB and removes the
// gap entirely.
func verifyAndInstall(tmp, exe, want string) error {
	sum, err := hashFile(tmp)
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("verify: %w", err)
	}
	if !strings.EqualFold(sum, want) {
		os.Remove(tmp)
		return errChecksumMismatch
	}
	return installBinary(tmp, exe)
}

// hashFile returns the hex sha256 of a file's contents.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// installMu serializes the swap. A preempted attempt is cancelled, not killed,
// so it may still be between its two renames when the attempt that replaced it
// arrives — and two interleaved park-then-slides can leave exe.old holding the
// NEW binary, which is what CleanupOldBinary deletes at next boot.
var installMu sync.Mutex

// installBinary swaps a verified download over the running executable. The
// park-then-slide dance is what makes it safe on every OS: renaming a running
// binary is allowed everywhere, deleting it is not, and the .old copy is both
// the rollback if the second rename fails and the thing CleanupOldBinary
// removes at next boot.
func installBinary(tmp, exe string) error {
	installMu.Lock()
	defer installMu.Unlock()
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("chmod: %w", err)
	}
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("swap (park old): %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe) // restore
		os.Remove(tmp)
		return fmt.Errorf("swap (install new): %w", err)
	}
	return nil
}

// downloadWithProgress streams url into path, updating the global progress.
// What was written is hashed later, off the file itself (see verifyAndInstall).
func downloadWithProgress(run *updateRun, url, path, version string) error {
	req, err := http.NewRequestWithContext(run.ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "concord-updater")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			pct := 0
			if total > 0 {
				pct = int(done * 100 / total)
			}
			run.set(UpdateProgress{Phase: "downloading", Percent: pct, Version: version})
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return nil
}

// fetchSmall downloads a small release asset (the checksum manifest or its
// signature), capped so a hostile or broken server can't stream forever.
func fetchSmall(url string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "concord-updater")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 64<<10))
}

// releaseSums is a release's checksum manifest and its detached signature —
// the pair that has to travel together for either half to mean anything.
type releaseSums struct{ sums, sig []byte }

// fetchReleaseSums pulls the SIGNED SHA256SUMS from the latest release and
// verifies it. Returning the raw pair (not just one hash) is what lets the
// same manifest be re-served to peers afterwards.
func fetchReleaseSums() (releaseSums, error) {
	return fetchReleaseSumsFor("latest")
}

// fetchReleaseSumsFor fetches the manifest of a specific release: "latest", or
// "tags/vX.Y.Z" to name one.
func fetchReleaseSumsFor(ref string) (releaseSums, error) {
	req, err := http.NewRequest(http.MethodGet,
		"https://api.github.com/repos/"+updateRepo+"/releases/"+ref, nil)
	if err != nil {
		return releaseSums{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "concord-updater")
	resp, err := updateClient.Do(req)
	if err != nil {
		return releaseSums{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseSums{}, fmt.Errorf("release lookup: HTTP %d", resp.StatusCode)
	}
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return releaseSums{}, err
	}
	sumsURL, sigURL := "", ""
	for _, a := range rel.Assets {
		switch a.Name {
		case "SHA256SUMS":
			sumsURL = a.URL
		case "SHA256SUMS.sig":
			sigURL = a.URL
		}
	}
	if sumsURL == "" {
		return releaseSums{}, fmt.Errorf("release has no SHA256SUMS")
	}
	body, err := fetchSmall(sumsURL)
	if err != nil {
		return releaseSums{}, err
	}
	var sig []byte
	if sigURL != "" {
		if sig, err = fetchSmall(sigURL); err != nil {
			return releaseSums{}, fmt.Errorf("fetch signature: %w", err)
		}
	}

	// Authenticity before integrity. A signed build refuses a release it can't
	// verify, including one that simply has no signature attached — otherwise
	// stripping the signature would be all it takes to defeat this.
	if releaseSigned() {
		if sigURL == "" {
			return releaseSums{}, errUnsignedRelease
		}
		if err := verifyReleaseSums(body, sig); err != nil {
			return releaseSums{}, err
		}
	}
	return releaseSums{sums: body, sig: sig}, nil
}

// sumForAsset returns the hash SHA256SUMS records for one asset. The caller
// must have established that the manifest is trustworthy first — this is the
// integrity half, and it is meaningless on its own.
func sumForAsset(sums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		// sha256sum writes "<hash>  <name>", with a '*' before the name in
		// binary mode.
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("asset %s not in SHA256SUMS", asset)
}

// CleanupOldBinary removes the parked .old binary from a previous update.
// Called once at startup (best-effort).
func CleanupOldBinary() {
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		os.Remove(exe + ".old")
	}
}
