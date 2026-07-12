package bridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/zahak/concord/internal/version"
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
)

func setUpd(p UpdateProgress) {
	updMu.Lock()
	updNow = p
	updMu.Unlock()
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
// polls UpdateState for progress. Returns immediately.
func (b *Bridge) ApplyUpdate() error {
	updMu.Lock()
	if updNow.Phase == "downloading" || updNow.Phase == "verifying" {
		updMu.Unlock()
		return nil // already running
	}
	updNow = UpdateProgress{Phase: "downloading"}
	updMu.Unlock()
	go b.runUpdate()
	return nil
}

func failUpd(format string, args ...any) {
	setUpd(UpdateProgress{Phase: "error", Error: fmt.Sprintf(format, args...)})
}

func (b *Bridge) runUpdate() {
	// Re-resolve the latest release fresh (don't trust a stale banner).
	cv, err := b.CheckForUpdate()
	if err != nil || !cv.Available {
		failUpd("no update available")
		return
	}
	if cv.Download == "" {
		failUpd("no downloadable build for this platform — grab it from the release page")
		return
	}
	if strings.HasSuffix(strings.ToLower(cv.Asset), ".zip") {
		failUpd("this build updates via the release page (bundle, not a bare binary)")
		return
	}
	if !b.CanSelfUpdate() {
		failUpd("this installation can't update itself (read-only location or dev build)")
		return
	}
	setUpd(UpdateProgress{Phase: "downloading", Version: cv.Latest})

	exe, err := os.Executable()
	if err != nil {
		failUpd("locating executable: %v", err)
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	tmp := exe + ".new"

	sum, err := downloadWithProgress(cv.Download, tmp, cv.Latest)
	if err != nil {
		os.Remove(tmp)
		failUpd("download: %v", err)
		return
	}

	// Verify against the release's SHA256SUMS — fail closed.
	setUpd(UpdateProgress{Phase: "verifying", Percent: 100, Version: cv.Latest})
	want, err := fetchExpectedSum(cv.Asset)
	if err != nil {
		os.Remove(tmp)
		failUpd("verify: %v", err)
		return
	}
	if !strings.EqualFold(sum, want) {
		os.Remove(tmp)
		failUpd("checksum mismatch — update aborted")
		return
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		failUpd("chmod: %v", err)
		return
	}

	// Atomic-ish swap: park the running binary as .old (allowed even while it
	// runs, on every OS), slide the new one into place. Cleaned up on next boot.
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		failUpd("swap (park old): %v", err)
		return
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe) // restore
		os.Remove(tmp)
		failUpd("swap (install new): %v", err)
		return
	}
	setUpd(UpdateProgress{Phase: "ready", Percent: 100, Version: cv.Latest})
}

// downloadWithProgress streams url into path, updating the global progress,
// and returns the hex sha256 of what was written.
func downloadWithProgress(url, path, version string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "concord-updater")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return "", werr
			}
			h.Write(buf[:n])
			done += int64(n)
			pct := 0
			if total > 0 {
				pct = int(done * 100 / total)
			}
			setUpd(UpdateProgress{Phase: "downloading", Percent: pct, Version: version})
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fetchExpectedSum pulls SHA256SUMS from the latest release and returns the
// hash recorded for asset.
func fetchExpectedSum(asset string) (string, error) {
	req, err := http.NewRequest(http.MethodGet,
		"https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "concord-updater")
	resp, err := updateClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	sumsURL := ""
	for _, a := range rel.Assets {
		if a.Name == "SHA256SUMS" {
			sumsURL = a.URL
			break
		}
	}
	if sumsURL == "" {
		return "", fmt.Errorf("release has no SHA256SUMS")
	}
	sreq, _ := http.NewRequest(http.MethodGet, sumsURL, nil)
	sreq.Header.Set("User-Agent", "concord-updater")
	client := &http.Client{Timeout: 30 * time.Second}
	sresp, err := client.Do(sreq)
	if err != nil {
		return "", err
	}
	defer sresp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(sresp.Body, 64<<10))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
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
