package main

// A TURN relay, so a Concord call can hide the participants' IP addresses from
// each other.
//
// WebRTC is peer-to-peer: by default the two ends connect directly, which means
// each learns the other's IP. For a call between friends that's fine. For a
// MEETING LINK you paste somewhere public, it is a deanonymization vector — a
// stranger who clicks it would otherwise see your home IP, and you theirs.
//
// TURN fixes this the way Signal does: when a client uses `iceTransportPolicy:
// "relay"`, it offers ONLY relayed candidates, so its real address never leaves
// its machine. If BOTH ends relay, the media flows client → TURN → client and
// neither peer sees the other's IP. The relay does (it has to, to forward), but
// the relay is already untrusted infrastructure that never sees plaintext:
// media stays DTLS-SRTP encrypted end-to-end. It moves the metadata from "any
// stranger with a link" to "the one relay you already bootstrap through".
//
// Auth is time-windowed HMAC (the coturn "REST" scheme, RFC 8489 §9.2, which
// pion/turn's NewLongTermAuthHandler validates): the credential endpoint issues
// short-lived username/password pairs derived from a shared secret, so the
// secret itself never ships to any client.

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v4"
)

// turnCredTTL is how long an issued TURN credential is valid. Short: a client
// fetches fresh creds when it starts a call, so a leaked pair is stale within
// the hour.
const turnCredTTL = time.Hour

// serveTURN starts the TURN relay when CONCORD_TURN_SECRET is set (no secret =
// feature off, and clients fall back to plain peer-to-peer). It listens on
// CONCORD_TURN_PORT (default 3478) over TCP — TCP works through the restrictive
// networks that most need a relay, and fly.io forwards TCP cleanly (its UDP
// path is finicky). realm/publicIP describe how clients reach it.
func serveTURN(ctx context.Context) *turnServer {
	secret := os.Getenv("CONCORD_TURN_SECRET")
	if secret == "" {
		return nil
	}
	public := os.Getenv("CONCORD_PUBLIC_HOST") // the fly hostname clients dial
	port := os.Getenv("CONCORD_TURN_PORT")
	if port == "" {
		port = "3478"
	}

	ts := &turnServer{secret: secret, public: public, port: port}

	// The relay hands out ephemeral relayed transport addresses. We bind the
	// listener on all interfaces but ADVERTISE the public host, so a client
	// behind NAT is told an address it can actually reach.
	relayIP := os.Getenv("CONCORD_TURN_RELAY_IP") // the machine's own routable IP
	if relayIP == "" {
		relayIP = "0.0.0.0"
	}

	tcpListener, err := net.Listen("tcp", "0.0.0.0:"+port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "turn: listen:", err)
		return nil
	}

	realm := public
	if realm == "" {
		realm = "concord"
	}
	server, err := turn.NewServer(turn.ServerConfig{
		Realm: realm,
		// Time-windowed credentials: the same HMAC(secret, username) the
		// credential endpoint below issues.
		AuthHandler: turn.NewLongTermAuthHandler(secret, logging.NewDefaultLoggerFactory().NewLogger("turn")),
		ListenerConfigs: []turn.ListenerConfig{{
			Listener: tcpListener,
			RelayAddressGenerator: &turn.RelayAddressGeneratorStatic{
				RelayAddress: net.ParseIP(relayIP),
				Address:      "0.0.0.0",
			},
			// CRITICAL: without a PermissionHandler, pion admits ALL peer
			// addresses — turning this internet-exposed relay into an SSRF pivot
			// (a client could Allocate then relay UDP to 127.0.0.1 or an RFC1918
			// neighbour, reaching internal services). A media relay only ever
			// needs to reach other public peers, so we refuse everything else.
			PermissionHandler: publicPeersOnly,
		}},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "turn: start:", err)
		_ = tcpListener.Close()
		return nil
	}
	ts.server = server
	fmt.Println("TURN relay listening on :" + port + " (tcp) realm=" + realm)

	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	return ts
}

type turnServer struct {
	server *turn.Server
	secret string
	public string
	port   string
}

// publicPeersOnly is the TURN permission filter: relay ONLY toward globally
// routable addresses. It denies loopback, unspecified, link-local (incl. cloud
// metadata 169.254.169.254), multicast, RFC1918/ULA private ranges, and RFC6598
// CGNAT space — every address class an attacker could use to pivot from this
// public relay into a private network or the relay host itself.
//
// CONCORD_TURN_ALLOW_PRIVATE=1 disables the filter for LOCAL development only,
// where peers are 127.0.0.1. Never set it in production.
var allowPrivatePeers = os.Getenv("CONCORD_TURN_ALLOW_PRIVATE") == "1"

func publicPeersOnly(_ net.Addr, peerIP net.IP) bool {
	if allowPrivatePeers {
		return true
	}
	if peerIP == nil || peerIP.IsLoopback() || peerIP.IsUnspecified() ||
		peerIP.IsPrivate() || peerIP.IsLinkLocalUnicast() ||
		peerIP.IsLinkLocalMulticast() || peerIP.IsMulticast() {
		return false
	}
	// RFC 6598 shared/CGNAT space (100.64.0.0/10) — not covered by IsPrivate.
	if v4 := peerIP.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}

// credentials mints a fresh time-windowed username/password. Username is the
// expiry unix time (what NewLongTermAuthHandler parses); password is the HMAC
// over it, exactly as the handler recomputes to verify.
func (t *turnServer) credentials() (user, pass string, ttl int) {
	exp := time.Now().Add(turnCredTTL).Unix()
	user = strconv.FormatInt(exp, 10)
	mac := hmac.New(sha1.New, []byte(t.secret))
	_, _ = mac.Write([]byte(user))
	pass = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return user, pass, int(turnCredTTL.Seconds())
}

// iceServer is the RTCIceServer shape a browser expects.
type iceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type turnCredsResponse struct {
	IceServers []iceServer `json:"iceServers"`
	TTL        int         `json:"ttl"`
}

// handleTURNCreds serves fresh ICE-server config (a public STUN server plus this
// TURN relay with ephemeral creds) to any caller. It is intentionally open: the
// credentials are short-lived and the relay exists to be usable by unauthed
// guests joining a meeting link. It reveals nothing — the secret stays here.
func (t *turnServer) handleTURNCreds(w http.ResponseWriter, r *http.Request) {
	user, pass, ttl := t.credentials()
	host := t.public
	if host == "" {
		// Dev / direct-IP: derive the hostname from the request, WITHOUT the
		// gateway's port (the TURN relay listens on its own port). turn:host:port
		// must carry the TURN port, not the HTTPS one.
		host = r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	resp := turnCredsResponse{
		TTL: ttl,
		IceServers: []iceServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{
				// TURN over TCP (turns: would need the relay behind TLS; plain
				// turn: is fine here — the media inside is already DTLS-SRTP).
				URLs:       []string{"turn:" + host + ":" + t.port + "?transport=tcp"},
				Username:   user,
				Credential: pass,
			},
		},
	}
	// Same-origin fetch from the guest page (connect-src 'self'); the member app
	// fetches server-side, so a permissive CORS header here is harmless and lets
	// the browser guest read it.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}
