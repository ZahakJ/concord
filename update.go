package main

import (
	"encoding/json"
	"net/http"
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

var updateClient = &http.Client{Timeout: 6 * time.Second}

// UpdateView is the notifier payload the UI renders as a dismissible banner.
type UpdateView struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	URL       string `json:"url"`   // release html_url (opened in the browser)
	Notes     string `json:"notes"` // release body (markdown)
}

// CheckForUpdate polls the public dist repo's latest release and compares its
// tag against the build-stamped version. It needs no session (works on the login
// screen), and any network/parse/non-200 error is a soft no-op (Available=false)
// so it is invisible offline or when rate-limited.
func (b *bridge) CheckForUpdate() (UpdateView, error) {
	cur := version
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
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return UpdateView{Available: false, Current: cur}, nil
	}

	return UpdateView{
		Available: semverLess(cur, rel.TagName),
		Current:   cur,
		Latest:    rel.TagName,
		URL:       rel.HTMLURL,
		Notes:     rel.Body,
	}, nil
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
