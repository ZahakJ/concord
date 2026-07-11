// push.go adds optional push-notification wakes to the rendezvous mailbox. When
// a deposit lands for an offline member, the node sends a CONTENTLESS wake to
// their registered devices so the app foregrounds and drains the real (still
// end-to-end-encrypted) message. The node never sees plaintext; a push carries
// no message content, only "you have mail".
//
// Tokens are keyed by the opaque 16-byte mailbox tag — never by identity — so
// this preserves the mailbox's privacy model. Push is entirely optional: a node
// with no credentials configured simply doesn't wake anyone (delivery still
// happens over live sockets and drain-on-reconnect).
//
// This is the one component that stops being "untrusted commodity infra": it
// holds APNs/FCM credentials. Self-hosters without them get a working mailbox
// with no push, which is a supported configuration.
package mailbox

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// DeviceToken is one registered push endpoint. Platform is "apns" or "fcm".
type DeviceToken struct {
	Platform string `json:"platform"`
	Token    string `json:"token"`
	Added    int64  `json:"added"` // unix seconds, for staleness pruning
}

// Notifier sends a contentless wake to a mailbox's registered devices. Notify is
// called (in its own goroutine) after a deposit; implementations must be
// self-rate-limiting and non-blocking-tolerant.
type Notifier interface {
	Notify(mailboxID string, tokens []DeviceToken)
}

// PushStore persists mailbox→tokens. Unlike the envelope store (in-memory, fine
// to lose), tokens must survive node restarts — a lost token means a silently
// missed wake — so this is backed by a small JSON file on the node's volume.
type PushStore struct {
	mu    sync.Mutex
	path  string
	byBox map[string][]DeviceToken
}

// staleTokenTTL forgets tokens not refreshed in this long (the client re-registers
// on every login, so a live device stays fresh; a retired one ages out).
const staleTokenTTL = 60 * 24 * time.Hour

// OpenPushStore loads (or creates) a token store at path. A load error yields an
// empty store rather than failing — push is best-effort.
func OpenPushStore(path string) *PushStore {
	ps := &PushStore{path: path, byBox: map[string][]DeviceToken{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &ps.byBox)
	}
	return ps
}

func (ps *PushStore) saveLocked() {
	if ps.path == "" {
		return
	}
	b, err := json.Marshal(ps.byBox)
	if err != nil {
		return
	}
	tmp := ps.path + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, ps.path)
	}
}

// Register binds a device token to a mailbox (de-duped by token, timestamp
// refreshed).
func (ps *PushStore) Register(mailboxID string, tok DeviceToken) {
	tok.Added = time.Now().Unix()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	list := ps.byBox[mailboxID]
	for i, t := range list {
		if t.Token == tok.Token {
			list[i] = tok // refresh platform + timestamp
			ps.byBox[mailboxID] = list
			ps.saveLocked()
			return
		}
	}
	ps.byBox[mailboxID] = append(list, tok)
	ps.saveLocked()
}

// Unregister drops a token from a mailbox (e.g. on logout or a provider
// "unregistered" error).
func (ps *PushStore) Unregister(mailboxID, token string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	list := ps.byBox[mailboxID]
	kept := list[:0]
	for _, t := range list {
		if t.Token != token {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(ps.byBox, mailboxID)
	} else {
		ps.byBox[mailboxID] = kept
	}
	ps.saveLocked()
}

// Tokens returns the non-stale tokens for a mailbox.
func (ps *PushStore) Tokens(mailboxID string) []DeviceToken {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	cutoff := time.Now().Add(-staleTokenTTL).Unix()
	var out []DeviceToken
	for _, t := range ps.byBox[mailboxID] {
		if t.Added >= cutoff {
			out = append(out, t)
		}
	}
	return out
}

// pushNotifier is the concrete Notifier: it fans a wake out to APNs and FCM,
// rate-limited per mailbox so a burst of deposits collapses to one wake.
type pushNotifier struct {
	apns *apnsSender
	fcm  *fcmSender

	mu    sync.Mutex
	last  map[string]time.Time
	every time.Duration
}

// NewNotifier builds a Notifier from whatever credentials are present in the
// environment; returns (nil,nil) when none are configured (push disabled):
//
//	APNS_AUTH_KEY_P8   PEM of the APNs .p8 signing key
//	APNS_KEY_ID        the key's Key ID
//	APNS_TEAM_ID       Apple developer Team ID
//	APNS_TOPIC         the app bundle ID (e.g. app.concord.mobile)
//	APNS_PRODUCTION    "1" for the production APNs host (default sandbox)
//	FCM_SERVICE_ACCOUNT_JSON  a Firebase service-account JSON (for FCM HTTP v1)
func NewNotifier() (Notifier, error) {
	n := &pushNotifier{last: map[string]time.Time{}, every: 30 * time.Second}
	var configured bool

	if key := os.Getenv("APNS_AUTH_KEY_P8"); key != "" {
		a, err := newAPNSSender(key, os.Getenv("APNS_KEY_ID"), os.Getenv("APNS_TEAM_ID"),
			os.Getenv("APNS_TOPIC"), os.Getenv("APNS_PRODUCTION") == "1")
		if err != nil {
			return nil, fmt.Errorf("apns: %w", err)
		}
		n.apns = a
		configured = true
	}
	if sa := os.Getenv("FCM_SERVICE_ACCOUNT_JSON"); sa != "" {
		f, err := newFCMSender(sa)
		if err != nil {
			return nil, fmt.Errorf("fcm: %w", err)
		}
		n.fcm = f
		configured = true
	}
	if !configured {
		return nil, nil
	}
	return n, nil
}

func (n *pushNotifier) Notify(mailboxID string, tokens []DeviceToken) {
	// Collapse bursts: at most one wake per mailbox per window.
	n.mu.Lock()
	if t, ok := n.last[mailboxID]; ok && time.Since(t) < n.every {
		n.mu.Unlock()
		return
	}
	n.last[mailboxID] = time.Now()
	n.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, tok := range tokens {
		switch tok.Platform {
		case "apns":
			if n.apns != nil {
				_ = n.apns.send(ctx, tok.Token)
			}
		case "fcm":
			if n.fcm != nil {
				_ = n.fcm.send(ctx, tok.Token)
			}
		}
	}
}

// ---- APNs (token-based auth, HTTP/2) ----

type apnsSender struct {
	key    *ecdsa.PrivateKey
	keyID  string
	teamID string
	topic  string
	host   string
	client *http.Client

	mu      sync.Mutex
	jwt     string
	jwtTime time.Time
}

func newAPNSSender(pemKey, keyID, teamID, topic string, production bool) (*apnsSender, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an ECDSA key")
	}
	host := "https://api.sandbox.push.apple.com"
	if production {
		host = "https://api.push.apple.com"
	}
	return &apnsSender{
		key: ec, keyID: keyID, teamID: teamID, topic: topic, host: host,
		// net/http negotiates HTTP/2 automatically over TLS, which APNs requires.
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// token returns a cached APNs provider JWT, refreshed at most hourly (Apple
// rejects tokens older than 1h and rate-limits frequent regeneration).
func (a *apnsSender) token() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.jwt != "" && time.Since(a.jwtTime) < 50*time.Minute {
		return a.jwt, nil
	}
	header := b64url(`{"alg":"ES256","kid":"` + a.keyID + `"}`)
	claims := b64url(fmt.Sprintf(`{"iss":"%s","iat":%d}`, a.teamID, time.Now().Unix()))
	signing := header + "." + claims
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, a.key, sum[:])
	if err != nil {
		return "", err
	}
	// APNs wants the raw r||s (each padded to 32 bytes), not ASN.1.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	a.jwt = signing + "." + base64.RawURLEncoding.EncodeToString(sig)
	a.jwtTime = time.Now()
	return a.jwt, nil
}

func (a *apnsSender) send(ctx context.Context, deviceToken string) error {
	jwt, err := a.token()
	if err != nil {
		return err
	}
	// A background (content-available) wake with a generic fallback alert, since
	// iOS drops silent pushes when the app is force-quit. The client decrypts the
	// real message on drain and replaces the notification.
	body := []byte(`{"aps":{"content-available":1,"alert":{"title":"Concord","body":"New encrypted message"},"sound":"default"}}`)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.host+"/3/device/"+deviceToken, strings.NewReader(string(body)))
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", a.topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("apns status %d", resp.StatusCode)
	}
	return nil
}

// ---- FCM (HTTP v1, OAuth2 via a service-account JWT) ----

type fcmSender struct {
	clientEmail string
	privateKey  *rsa.PrivateKey
	projectID   string
	client      *http.Client

	mu       sync.Mutex
	token    string
	tokenExp time.Time
}

func newFCMSender(serviceAccountJSON string) (*fcmSender, error) {
	var sa struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		ProjectID   string `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(serviceAccountJSON), &sa); err != nil {
		return nil, err
	}
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("invalid private_key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rk, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private_key is not RSA")
	}
	return &fcmSender{
		clientEmail: sa.ClientEmail, privateKey: rk, projectID: sa.ProjectID,
		client: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// accessToken exchanges a service-account JWT for a short-lived OAuth2 access
// token (cached until near expiry). Avoids the golang.org/x/oauth2 dependency.
func (f *fcmSender) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.token != "" && time.Until(f.tokenExp) > 2*time.Minute {
		return f.token, nil
	}
	now := time.Now()
	header := b64url(`{"alg":"RS256","typ":"JWT"}`)
	claims := b64url(fmt.Sprintf(
		`{"iss":"%s","scope":"https://www.googleapis.com/auth/firebase.messaging","aud":"https://oauth2.googleapis.com/token","iat":%d,"exp":%d}`,
		f.clientEmail, now.Unix(), now.Add(time.Hour).Unix()))
	signing := header + "." + claims
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.privateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	assertion := signing + "." + base64.RawURLEncoding.EncodeToString(sig)

	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("no access_token (status %d)", resp.StatusCode)
	}
	f.token = tr.AccessToken
	f.tokenExp = now.Add(time.Duration(tr.ExpiresIn) * time.Second)
	return f.token, nil
}

func (f *fcmSender) send(ctx context.Context, deviceToken string) error {
	at, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	// A high-priority DATA message (no notification payload): the Android app
	// wakes, drains the mailbox, decrypts, and posts a real local notification.
	msg := map[string]any{
		"message": map[string]any{
			"token": deviceToken,
			"data":  map[string]string{"wake": "1"},
			"android": map[string]any{
				"priority": "high",
			},
		},
	}
	body, _ := json.Marshal(msg)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://fcm.googleapis.com/v1/projects/"+f.projectID+"/messages:send",
		strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+at)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("fcm status %d", resp.StatusCode)
	}
	return nil
}

// ---- small helpers ----

func b64url(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
