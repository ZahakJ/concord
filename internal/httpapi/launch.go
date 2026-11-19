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
//
// Only the page should ever redeem its code, so a code redeemed twice means
// the first redemption was not the page's. Nothing stops that first
// redemption: /launch refuses a request without an Origin, but any program
// can send a loopback Origin header. What the server can do is notice. A
// spent code is remembered for a while; redeeming it again is answered 409
// rather than 403, and the server then rotates the token and runs its revoke
// hooks (the browser build cancels any open device-link offer), so whoever
// redeemed first loses further use of the API, and the page tells the user
// what happened. Rotation cannot undo what was done with the token before
// it: the page's notice asks the user to check the linked devices.
//
// The trade-off: a program that read the code but lost the race to the page
// can replay it after the page is in, and force a rotation that signs out
// every open tab (they get 401s, with no notice). That is a denial of
// service, not a leak; before, the same program could take the token.

const (
	launchCodeTTL  = 2 * time.Minute
	maxLaunchCodes = 16
	// spentCodeTTL is how long a spent code is remembered, so that a page
	// which boots slowly still learns that someone redeemed its code first.
	spentCodeTTL  = 15 * time.Minute
	maxSpentCodes = 64
)

// redeemResult is what redeem made of a code.
type redeemResult int

const (
	codeUnknown  redeemResult = iota // never minted, expired, or evicted
	codeRedeemed                     // live, and now spent by this call
	codeSpent                        // already spent by an earlier call
)

type launchCodes struct {
	mu         sync.Mutex
	codes      map[string]time.Time // code -> minted at
	order      []string             // oldest first, for the cap
	spent      map[string]time.Time // code -> spent at
	spentOrder []string             // oldest first, for the cap
	now        func() time.Time
}

func newLaunchCodes() *launchCodes {
	return &launchCodes{codes: map[string]time.Time{}, spent: map[string]time.Time{}, now: time.Now}
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

// redeem spends code if it is live, and otherwise says whether it was spent
// before (codeSpent) or was never good (codeUnknown).
func (l *launchCodes) redeem(code string) redeemResult {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune()
	if code == "" {
		return codeUnknown
	}
	if _, ok := l.spent[code]; ok {
		return codeSpent
	}
	if _, ok := l.codes[code]; !ok {
		return codeUnknown
	}
	delete(l.codes, code)
	for i, c := range l.order {
		if c == code {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	for len(l.spentOrder) >= maxSpentCodes {
		delete(l.spent, l.spentOrder[0])
		l.spentOrder = l.spentOrder[1:]
	}
	l.spent[code] = l.now()
	l.spentOrder = append(l.spentOrder, code)
	return codeRedeemed
}

// dropLive forgets every unspent code. After a rotation, a code that was
// minted before it must not buy the new token for whoever else has seen it.
func (l *launchCodes) dropLive() {
	l.mu.Lock()
	defer l.mu.Unlock()
	clear(l.codes)
	l.order = l.order[:0]
}

// prune drops expired codes and spent codes old enough to forget. Caller
// holds mu.
func (l *launchCodes) prune() {
	now := l.now()
	cut := now.Add(-launchCodeTTL)
	keep := l.order[:0]
	for _, c := range l.order {
		if l.codes[c].After(cut) {
			keep = append(keep, c)
		} else {
			delete(l.codes, c)
		}
	}
	l.order = keep
	cut = now.Add(-spentCodeTTL)
	keepSpent := l.spentOrder[:0]
	for _, c := range l.spentOrder {
		if l.spent[c].After(cut) {
			keepSpent = append(keepSpent, c)
		} else {
			delete(l.spent, c)
		}
	}
	l.spentOrder = keepSpent
}

func (l *launchCodes) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.codes)
}

// MintLaunchCode returns a fresh single-use code for the page to trade for the
// token, or "" when this server has no token to protect.
func (s *Server) MintLaunchCode() string {
	if s.token() == "" {
		return ""
	}
	return s.launch.mint()
}

// WithTokenRotated has fn told the new token each time the server rotates it
// (see HandleLaunch). The browser build uses it to keep a token file current.
func WithTokenRotated(fn func(token string)) Option {
	return func(s *Server) { s.onRotate = fn }
}

// WithRevoke adds fn to what a rotation runs once the new token is in force:
// anything the old token could have set up that outlives it (a device-link
// offer, say) is withdrawn there.
func WithRevoke(fn func()) Option {
	return func(s *Server) { s.revokers = append(s.revokers, fn) }
}

// SetLaunchCommand names the command that prints a fresh launch code (curl
// on the launch socket), which the 409 answer passes on so the page can
// offer it instead of a restart. "" when there is none.
func (s *Server) SetLaunchCommand(cmd string) {
	s.tokenMu.Lock()
	s.launchCmd = cmd
	s.tokenMu.Unlock()
}

// rotateToken replaces the bearer token with a fresh random one, ends every
// event stream opened under the old one, forgets unspent launch codes, and
// runs the revoke hooks. Requests made with the old token are refused from
// here on.
func (s *Server) rotateToken() {
	s.rotateMu.Lock() // one at a time, so onRotate hears the tokens in order
	defer s.rotateMu.Unlock()
	var b [32]byte
	_, _ = rand.Read(b[:]) // never fails (crypto/rand.Read crashes the process instead)
	token := hex.EncodeToString(b[:])
	s.tokenMu.Lock()
	if s.authToken == "" { // no token, nothing to protect: never turn auth on or off here
		s.tokenMu.Unlock()
		return
	}
	s.authToken = token
	close(s.revoked)
	s.revoked = make(chan struct{})
	s.launch.dropLive()
	s.tokenMu.Unlock()
	for _, revoke := range s.revokers {
		revoke()
	}
	if s.onRotate != nil {
		s.onRotate(token)
	}
}

// HandleLaunch serves POST /launch {"code": "..."}: a live code buys the
// token, once. A code that was already spent is answered 409 and rotates the
// token, because the only way a page meets its own code spent is that someone
// else redeemed it first. Everything else is 403 and spends nothing, so a
// foreign page guessing cannot burn the launcher's code.
//
// The request must carry a loopback Origin. The page's fetch always sends
// one (browsers send Origin on every POST), so a request without one is
// refused before it can spend anything. That turns away only clients that
// send none: a program can set the header, and the defence against one that
// does is the 409 and the rotation, not this check.
func (s *Server) HandleLaunch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	origin := r.Header.Get("Origin")
	if s.token() == "" || origin == "" || !localOrigin(origin) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body); err != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// Redeemed and read under one lock, so a rotation cannot slip between
	// them: a code spent before a rotation buys only the token it kills.
	s.tokenMu.RLock()
	res := s.launch.redeem(body.Code)
	token := s.authToken
	s.tokenMu.RUnlock()
	switch res {
	case codeRedeemed:
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]string{"token": token})
	case codeSpent:
		s.rotateToken()
		s.tokenMu.RLock()
		fresh := s.launchCmd
		s.tokenMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "launch code already used; the access token has been changed",
			"fresh": fresh,
		})
	default:
		http.Error(w, "forbidden", http.StatusForbidden)
	}
}
