package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// Launch codes open the browser build without the bearer token ever leaving
// the process. The token used to ride the URL the binary printed and opened,
// so it landed in the service journal and in the browser's command line
// (/proc/<pid>/cmdline, which every user on the machine can read), and stayed
// valid there for the life of the process.
//
// A code is what rides those places instead. The page trades it once, over
// POST /launch, for the token, which then lives in the page's own storage as
// before. A code is single use and dies after launchCodeTTL, so a copy found
// in a log, a terminal's scrollback or a process list later is worthless.
//
// This does not stop a process that runs as the same user and asks for a
// fresh code at the moment it wants one (main_web.go's launch socket answers
// anything running as this user, by design: the launcher is such a process).
// Nothing file-based can; what it removes is a long-lived secret at rest.

const (
	launchCodeTTL  = 2 * time.Minute
	maxLaunchCodes = 16
)

type launchCodes struct {
	mu    sync.Mutex
	codes map[string]time.Time // code -> minted at
	order []string             // oldest first, for the cap
	now   func() time.Time
}

func newLaunchCodes() *launchCodes {
	return &launchCodes{codes: map[string]time.Time{}, now: time.Now}
}

func (l *launchCodes) mint() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	code := hex.EncodeToString(b[:])
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune()
	for len(l.order) >= maxLaunchCodes {
		delete(l.codes, l.order[0])
		l.order = l.order[1:]
	}
	l.codes[code] = l.now()
	l.order = append(l.order, code)
	return code
}

// redeem spends code, reporting whether it was live.
func (l *launchCodes) redeem(code string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune()
	if _, ok := l.codes[code]; !ok || code == "" {
		return false
	}
	delete(l.codes, code)
	for i, c := range l.order {
		if c == code {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	return true
}

// prune drops expired codes. Caller holds mu.
func (l *launchCodes) prune() {
	cut := l.now().Add(-launchCodeTTL)
	keep := l.order[:0]
	for _, c := range l.order {
		if l.codes[c].After(cut) {
			keep = append(keep, c)
		} else {
			delete(l.codes, c)
		}
	}
	l.order = keep
}

func (l *launchCodes) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.codes)
}

// MintLaunchCode returns a fresh single-use code for the page to trade for the
// token, or "" when this server has no token to protect.
func (s *Server) MintLaunchCode() string {
	if s.authToken == "" {
		return ""
	}
	return s.launch.mint()
}

// HandleLaunch serves POST /launch {"code": "..."}: a live code buys the
// token, once. Everything else is 403 and spends nothing, so a foreign page
// guessing cannot burn the launcher's code.
func (s *Server) HandleLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.authToken == "" || !localOrigin(r.Header.Get("Origin")) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil || !s.launch.redeem(body.Code) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]string{"token": s.authToken})
}
