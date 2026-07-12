package bridge

import (
	"encoding/json"
	"github.com/zahak/concord/internal/version"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// updateRepo is the PUBLIC distribution repo — it holds only release binaries,
// no source. The app polls its "latest release" UNAUTHENTICATED at startup;
// GitHub's 60 req/hr/IP unauthenticated budget is ample for a launch-time check.
// Keeping this separate from the (private) source repo means no token is ever
// embedded in the shipped binary.
const updateRepo = "ZahakJ/concord-dist"

// NativeBuild is set by the Wails desktop main. A native build must never be
// offered the zero-dep WEB binary as its update — that silently "upgrades" a
// real windowed app into the browser-served variant (exactly what happened
// when a release shipped without desktop assets). When no desktop asset
// exists, the banner falls back to the release page instead.
var NativeBuild bool

var updateClient = &http.Client{Timeout: 6 * time.Second}

// UpdateView is the notifier payload the UI renders as a dismissible banner.
type UpdateView struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`   // release html_url (opened in the browser)
	Notes     string `json:"notes"` // release body (markdown)
	// Download is a DIRECT link to the release asset for THIS machine's OS (so
	// the banner can offer a one-click download of the right binary). Empty if
	// no matching asset was found — the UI falls back to the release page (URL).
	Download string `json:"download"`
	// Asset is the matched asset's filename, shown so the user knows what they're
	// getting (it carries the version, e.g. concord-desktop-windows-v0.6.0.exe).
	Asset string `json:"asset"`
}

// CheckForUpdate polls the public dist repo's latest release and compares its
// tag against the build-stamped version. It needs no session (works on the login
// screen), and any network/parse/non-200 error is a soft no-op (Available=false)
// so it is invisible offline or when rate-limited.
func (b *Bridge) CheckForUpdate() (UpdateView, error) {
	cur := version.Version
	if !isSemver(cur) {
		// Unstamped dev/local builds never nag.
		return UpdateView{Available: false, Current: cur}, nil
	}

	req, err := http.NewRequest(http.MethodGet,
		"https://api.github.com/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return UpdateView{Available: false, Current: cur}, nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "concord-updater")

	resp, err := updateClient.Do(req)
	if err != nil {
		return UpdateView{Available: false, Current: cur}, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return UpdateView{Available: false, Current: cur}, nil
	}

	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return UpdateView{Available: false, Current: cur}, nil
	}

	dl, asset := matchAsset(runtime.GOOS, func() []assetRef {
		out := make([]assetRef, len(rel.Assets))
		for i, a := range rel.Assets {
			out[i] = assetRef{Name: a.Name, URL: a.URL}
		}
		return out
	}())

	return UpdateView{
		Available: semverLess(cur, rel.TagName),
		Current:   cur,
		Latest:    rel.TagName,
		URL:       rel.HTMLURL,
		Notes:     rel.Body,
		Download:  dl,
		Asset:     asset,
	}, nil
}

type assetRef struct{ Name, URL string }

// matchAsset picks the release asset for the running OS **and build track**:
// a native (Wails) build only ever matches desktop assets, and a web build
// only ever matches web assets — self-update swaps the binary in place, so
// crossing tracks would silently turn a windowed app into a browser-server
// (or hand a webkit-dependent binary to a machine without webkit). Web assets
// are further narrowed by architecture. Matches on keywords, not exact names,
// so version-stamped filenames still resolve. Returns ("","") if nothing fits.
func matchAsset(goos string, assets []assetRef) (url, name string) {
	// os keyword -> the token our release assets carry (see .github/workflows).
	osKey := map[string]string{
		"windows": "windows",
		"linux":   "linux",
		"darwin":  "macos",
	}[goos]
	if osKey == "" {
		return "", ""
	}
	// Web assets carry an arch token (linux-amd64, macos-intel/arm64); Windows
	// ships amd64 only, so anything matching the OS is fine there.
	archKey := runtime.GOARCH // "amd64" | "arm64"
	if goos == "darwin" && archKey == "amd64" {
		archKey = "intel"
	}
	var deskURL, deskName, archURL, archName, anyURL, anyName string
	for _, a := range assets {
		n := strings.ToLower(a.Name)
		if !strings.Contains(n, osKey) {
			continue
		}
		if strings.Contains(n, "desktop") {
			if deskURL == "" {
				deskURL, deskName = a.URL, a.Name
			}
			continue // never counts as a web-track candidate
		}
		if anyURL == "" {
			anyURL, anyName = a.URL, a.Name
		}
		if strings.Contains(n, archKey) && archURL == "" {
			archURL, archName = a.URL, a.Name
		}
	}
	if NativeBuild {
		return deskURL, deskName // "" when absent: never hand a native app the web exe
	}
	if archURL != "" {
		return archURL, archName
	}
	return anyURL, anyName
}

// --- tiny semver, strictly vMAJOR.MINOR.PATCH -----------------------------
// A dedicated dep (golang.org/x/mod/semver) is an option but overkill: our tags
// are strictly vX.Y.Z. Pre-release / build metadata is intentionally rejected
// (parses to "not semver" -> treated as "no update", which is the safe default).

func isSemver(v string) bool { _, ok := parseSemver(v); return ok }

func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// semverLess reports whether a < b (both vX.Y.Z). Anything unparseable => false,
// so a malformed tag or a "dev" current version never triggers an update prompt.
func semverLess(a, b string) bool {
	pa, ok1 := parseSemver(a)
	pb, ok2 := parseSemver(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}
