// Package concord is the gomobile binding surface for the Android and iOS
// shells. `gomobile bind ./mobile` compiles the whole Concord core (identity,
// MLS, libp2p networking, encrypted store, the Service and its JSON bridge)
// into an .aar / .xcframework the native apps embed.
//
// The binding is deliberately tiny: Start boots the core and serves the same
// loopback HTTP (/rpc) + SSE (/events) surface the desktop builds use, so the
// Capacitor webview talks to it with the exact same frontend transport
// (frontend/src/lib/api.js) — just pointed at 127.0.0.1:<Port> with a bearer
// token. Native code that needs the core while the webview is gone
// (notifications, background mailbox drains) calls DispatchJSON directly.
//
// gomobile only supports a restricted type set (strings, ints, bools, []byte,
// error, exported structs/interfaces with such methods), which is why
// everything here is strings.
package concord

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	// gomobile requires the bind package in the module graph; nothing imports
	// it directly, so keep it pinned with a blank import (the documented idiom).
	_ "golang.org/x/mobile/bind"

	appsvc "github.com/ZahakJ/concord/internal/app"
	"github.com/ZahakJ/concord/internal/bridge"
	"github.com/ZahakJ/concord/internal/httpapi"
	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
	"github.com/ZahakJ/concord/internal/version"
)

// webviewOrigins are the Origin values Capacitor webviews present:
// capacitor://localhost on iOS, and http(s)://localhost on Android depending
// on the androidScheme config. The server answers CORS for exactly these.
var webviewOrigins = []string{
	"capacitor://localhost",
	"https://localhost",
	"http://localhost",
}

// EventSink receives bridge events natively, in parallel with the SSE stream.
// The shells register one to post local notifications while the webview is
// torn down. data is the event payload as JSON ("null" for signal-only events).
type EventSink interface {
	OnEvent(name, data string)
}

// Node is a running Concord core with its loopback API server.
type Node struct {
	cancel  context.CancelFunc
	srv     *httpapi.Server
	httpSrv *http.Server
	port    int
	token   string
}

// Start boots the core, storing all data under dataDir (the app sandbox:
// Context.getFilesDir()/concord on Android, Application Support/concord on
// iOS — the caller must exclude it from OS backups, since restoring MLS
// ratchet state onto another install forks the group state). It serves the
// RPC/SSE API on a random loopback port, guarded by a fresh bearer token:
// loopback on Android is reachable by every app on the device, so possession
// of the token — not the socket — is what authorizes a client.
func Start(dataDir string) (*Node, error) {
	if dataDir != "" {
		// DataDir() (internal/app/paths.go) honors CONCORD_HOME everywhere the
		// core resolves paths, so the override reaches keystore, DB and MLS state.
		if err := os.Setenv("CONCORD_HOME", dataDir); err != nil {
			return nil, err
		}
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(raw)

	ctx, cancel := context.WithCancel(context.Background())
	srv := httpapi.New(ctx, false,
		httpapi.WithAuthToken(token),
		httpapi.WithCORSOrigins(webviewOrigins),
	)
	srv.WireSSE()

	mux := http.NewServeMux()
	mux.HandleFunc("/rpc", srv.HandleRPC)
	mux.HandleFunc("/events", srv.HandleEvents)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		srv.Bridge().Close()
		return nil, fmt.Errorf("loopback listen: %w", err)
	}

	httpSrv := &http.Server{Handler: mux}
	go func() { _ = httpSrv.Serve(ln) }()

	return &Node{
		cancel:  cancel,
		srv:     srv,
		httpSrv: httpSrv,
		port:    ln.Addr().(*net.TCPAddr).Port,
		token:   token,
	}, nil
}

// Port is the loopback TCP port serving /rpc and /events.
func (n *Node) Port() int { return n.port }

// Token is the bearer token required on every API request (Authorization
// header on /rpc, ?token= query parameter on /events).
func (n *Node) Token() string { return n.token }

// DispatchJSON invokes a bridge method directly, bypassing HTTP — for native
// code paths (notification rendering, background drains) that run while no
// webview exists. Same method surface as POST /rpc.
func (n *Node) DispatchJSON(method, argsJSON string) string {
	return n.srv.Bridge().DispatchJSON(method, argsJSON)
}

// Fingerprint unlock. These are the only way U (the secret the phone's
// biometric-bound Keystore key releases) reaches the core: in process, from
// native code. None of them is an RPC method, so script in the page can
// neither read U nor open the App Lock gate.

// UseDeviceSecret unlocks with the fingerprint slot slotID, or, with a
// session open, checks U and opens the App Lock gate. The caller zeroes u
// afterwards; the core zeroes everything it derived from it.
func (n *Node) UseDeviceSecret(slotID, u []byte) error {
	return n.srv.Bridge().UseDeviceSecret(slotID, u)
}

// The outcomes of UnlockWithDeviceSecret. Typed, so the phone never has to
// read an error's words to decide whether an enrolment is dead.
const (
	// DeviceSecretOK: unlocked, or the gate opened.
	DeviceSecretOK = 0
	// DeviceSecretDead: U was unwrapped and still does not open the slot.
	// This enrolment can never work again; the phone deletes its half.
	DeviceSecretDead = 1
	// DeviceSecretFailed: anything else (no slot, a link in progress, a
	// disk error). Nothing is deleted on it.
	DeviceSecretFailed = 2
)

// UnlockWithDeviceSecret is UseDeviceSecret answering with one of the
// DeviceSecret codes.
func (n *Node) UnlockWithDeviceSecret(slotID, u []byte) int {
	err := n.srv.Bridge().UseDeviceSecret(slotID, u)
	switch {
	case err == nil:
		return DeviceSecretOK
	case errors.Is(err, identity.ErrDeviceSlotMismatch):
		return DeviceSecretDead
	}
	return DeviceSecretFailed
}

// AppLockState is App Lock as the core holds it: 1 on, 0 off, -1 unknown (no
// session). The phone's FLAG_SECURE and the tray's Reply follow it, rather
// than what the page last said.
func (n *Node) AppLockState() int {
	on, known := n.srv.Bridge().AppLockActive()
	switch {
	case !known:
		return -1
	case on:
		return 1
	}
	return 0
}

// NotifyContextJSON is what the native notifier needs, as JSON
// {"self": fingerprint, "dms": {channelID: name}}, or "" with no session.
// In process only, and answered behind the App Lock gate: notifications keep
// coming while the app is locked, which the page's Identity and Guilds calls,
// refused behind the gate, cannot serve.
func (n *Node) NotifyContextJSON() string {
	nc, err := n.srv.Bridge().NotifyContext()
	if err != nil {
		return ""
	}
	b, err := json.Marshal(nc)
	if err != nil {
		return ""
	}
	return string(b)
}

// VerifyDeviceSecret is UseDeviceSecret for an open session: the App Lock
// gate's check.
func (n *Node) VerifyDeviceSecret(slotID, u []byte) error {
	return n.srv.Bridge().UseDeviceSecret(slotID, u)
}

// EnrolDeviceSecret turns fingerprint unlock on with the phone's new U and
// enrolment id. passphrase "" means "the passphrase was typed to unlock a
// moment ago" (the core checks, and uses that up).
func (n *Node) EnrolDeviceSecret(slotID, u []byte, passphrase string) error {
	return n.srv.Bridge().EnrolDeviceSecret(slotID, u, passphrase)
}

// DropDeviceSlot turns fingerprint unlock off in the keystore.
func (n *Node) DropDeviceSlot() error { return n.srv.Bridge().DropDeviceSlot(nil) }

// DeviceSlotID is the enrolment id of the keystore's fingerprint slot, or
// nil. It reads nothing secret; the lock screen uses it to tell whether the
// phone's wrapped U and the slot belong together.
func (n *Node) DeviceSlotID() []byte {
	dir, err := appsvc.DataDir()
	if err != nil {
		return nil
	}
	return appsvc.DeviceSlotID(dir)
}

// SetDeviceWrapper tells the core which enrolment the phone holds a wrapped
// U for (nil or empty: none), so a slot nothing can open is dropped at the
// next passphrase unlock.
func (n *Node) SetDeviceWrapper(slotID []byte) { n.srv.Bridge().SetDeviceWrapper(slotID) }

// EngageAppLock closes the App Lock gate if fingerprint unlock and App Lock
// are on. The phone calls it when the app goes to the background.
func (n *Node) EngageAppLock() bool { return n.srv.Bridge().EngageAppLock() }

// SetEventSink registers sink to receive every bridge event alongside the SSE
// stream. Pass nil to detach. Must be called after Start (it chains the hooks
// WireSSE installed).
func (n *Node) SetEventSink(sink EventSink) {
	b := n.srv.Bridge()
	emit := func(name string, v any) {
		if sink == nil {
			return
		}
		payload, _ := json.Marshal(v)
		sink.OnEvent(name, string(payload))
	}

	prevMessage := b.OnMessage
	b.OnMessage = func(m bridge.MessageView) {
		if prevMessage != nil {
			prevMessage(m)
		}
		emit("message", m)
	}
	prevPresence := b.OnPresence
	b.OnPresence = func() {
		if prevPresence != nil {
			prevPresence()
		}
		emit("presence", nil)
	}
	prevVoicePresence := b.OnVoicePresence
	b.OnVoicePresence = func(v bridge.VoicePresence) {
		if prevVoicePresence != nil {
			prevVoicePresence(v)
		}
		emit("voice-presence", v)
	}
	prevVoiceSignal := b.OnVoiceSignal
	b.OnVoiceSignal = func(v bridge.VoiceSignal) {
		if prevVoiceSignal != nil {
			prevVoiceSignal(v)
		}
		emit("voice-signal", v)
	}
	prevTyping := b.OnTyping
	b.OnTyping = func(t bridge.TypingInfo) {
		if prevTyping != nil {
			prevTyping(t)
		}
		emit("typing", t)
	}
	prevGuildUpdate := b.OnGuildUpdate
	b.OnGuildUpdate = func() {
		if prevGuildUpdate != nil {
			prevGuildUpdate()
		}
		emit("guild-updated", nil)
	}
	prevReadState := b.OnReadState
	b.OnReadState = func(r bridge.ReadStateView) {
		if prevReadState != nil {
			prevReadState(r)
		}
		emit("read-state", r)
	}
	prevReceipts := b.OnReceipts
	b.OnReceipts = func(r bridge.ReceiptsView) {
		if prevReceipts != nil {
			prevReceipts(r)
		}
		emit("receipts", r)
	}
	prevHidden := b.OnHidden
	b.OnHidden = func(h bridge.HiddenView) {
		if prevHidden != nil {
			prevHidden(h)
		}
		emit("message-hidden", h)
	}
	prevChatPrefs := b.OnChatPrefs
	b.OnChatPrefs = func(m map[string]store.ChatPref) {
		if prevChatPrefs != nil {
			prevChatPrefs(m)
		}
		emit("chat-prefs", m)
	}
	prevStory := b.OnStory
	b.OnStory = func(u bridge.StoryUpdate) {
		if prevStory != nil {
			prevStory(u)
		}
		emit("story", u)
	}
	prevImport := b.OnChronicleImport
	b.OnChronicleImport = func(p appsvc.ChatImportProgress) {
		if prevImport != nil {
			prevImport(p)
		}
		emit("chronicle-import", p)
	}
}

// Nudge asks the core to hurry reconnection after the OS resumes the app:
// re-dial bootstrap peers, drain the mailbox, kick sync. Safe to call whether
// or not the identity is unlocked.
func (n *Node) Nudge() {
	n.srv.Bridge().Nudge()
}

// SetForeground reports whether the app is on screen (Activity onStart/onStop
// on Android). Off screen, the core slows its periodic discovery/sync loops to
// one shared multi-minute beat so the phone's radio can sleep, while keeping
// every connection (and therefore message delivery) alive. Safe to call
// whether or not the identity is unlocked.
func (n *Node) SetForeground(fg bool) {
	_ = n.srv.Bridge().SetForeground(fg)
}

// SetMetered reports whether the OS says this connection is billed by the byte
// (ConnectivityManager's default network without NET_CAPABILITY_NOT_METERED on
// Android). On a metered link the core holds its periodic peer-discovery loops
// to a gentler floor even with the app on screen; connections, message
// delivery, mailbox drains and sync are untouched. Call it once with the
// current answer at startup and again on every change. Safe to call whether or
// not the identity is unlocked.
func (n *Node) SetMetered(metered bool) {
	_ = n.srv.Bridge().SetMetered(metered)
}

// Stop shuts the API server and the core down. The Node cannot be restarted;
// call Start again for a fresh one.
func (n *Node) Stop() {
	_ = n.httpSrv.Close()
	n.srv.Bridge().Close()
	n.cancel()
}

// Version reports the release tag stamped at build time ("dev" when unstamped).
func Version() string { return version.Version }
