package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/box"

	"github.com/zahak/concord/internal/identity"
	"github.com/zahak/concord/internal/mailbox"
)

// Client-side mailbox integration. When a group member is offline we deposit
// an X25519-sealed envelope (the same MLS ciphertext that would have gone over
// gossipsub, wrapped for the recipient) into their mailbox on the rendezvous
// node. On reconnect we drain our own mailbox and feed the envelopes through
// the normal receive path. The node learns nothing: envelopes are sealed to
// the recipient and addressed by an opaque tag derived from their account key.

// mbxPayload is what a sealed envelope carries — the group and its ciphertext,
// exactly what a live gossipsub delivery would be.
type mbxPayload struct {
	GroupID []byte `json:"g"`
	CT      []byte `json:"c"`
}

// deriveMailboxKeys returns this identity's X25519 keypair for sealing mailbox
// envelopes, derived deterministically from the seed (reproduced on restart,
// never stored).
func deriveMailboxKeys(id *identity.Identity) (priv, pub [32]byte) {
	r := hkdf.New(sha256.New, id.Seed(), nil, []byte("concord-mbx-enc-v1"))
	_, _ = io.ReadFull(r, priv[:])
	// X25519 clamping is applied internally by curve25519; derive the public.
	pubSlice, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err == nil {
		copy(pub[:], pubSlice)
	}
	return priv, pub
}

// MailboxPub returns this peer's mailbox public key (shared over E2EE profiles
// so others can seal offline envelopes to us).
func (s *Service) MailboxPub() []byte {
	return append([]byte(nil), s.mbxPub[:]...)
}

// sealEnvelope encrypts a payload to a recipient's mailbox public key using an
// ephemeral sender key (anonymous to the node). Layout: ephemeralPub(32) ||
// nonce(24) || box.
func sealEnvelope(recipientPub [32]byte, payload []byte) ([]byte, error) {
	epk, esk, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	ct := box.Seal(nil, payload, &nonce, &recipientPub, esk)
	out := make([]byte, 0, 32+24+len(ct))
	out = append(out, epk[:]...)
	out = append(out, nonce[:]...)
	out = append(out, ct...)
	return out, nil
}

// openEnvelope reverses sealEnvelope with our mailbox private key.
func (s *Service) openEnvelope(env []byte) ([]byte, bool) {
	if len(env) < 32+24+box.Overhead {
		return nil, false
	}
	var epk [32]byte
	var nonce [24]byte
	copy(epk[:], env[:32])
	copy(nonce[:], env[32:56])
	return box.Open(nil, env[56:], &nonce, &epk, &s.mbxPriv)
}

// depositForOffline seals the ciphertext to any group members who are not
// currently connected and deposits it in their mailbox on each rendezvous node.
// Best-effort and asynchronous — live delivery still goes over gossipsub.
func (s *Service) depositForOffline(groupID []byte, ct []byte) {
	nodes := s.mailboxNodes()
	if len(nodes) == 0 {
		return
	}
	// Which members are offline right now?
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil {
		return
	}
	online := map[string]bool{s.id.Fingerprint(): true}
	for _, p := range s.host.Peers() {
		online[s.presence(p).Fingerprint] = true
	}

	payload, _ := json.Marshal(mbxPayload{GroupID: groupID, CT: ct})
	for _, cred := range creds {
		fpr := accountFingerprintOf(cred)
		if online[fpr] {
			continue
		}
		pub, ok := s.mailboxPubFor(fpr)
		if !ok {
			continue // we don't know their mailbox key yet
		}
		env, err := sealEnvelope(pub, payload)
		if err != nil {
			continue
		}
		target := mailbox.MailboxID(mailboxKeyOf(cred))
		for _, node := range nodes {
			s.mbxDeposit(node, target, env)
		}
	}
}

// registerMailbox tells a rendezvous node we own our mailbox so it will accept
// deposits for us (anti-spam gate).
func (s *Service) registerMailbox(node peer.ID) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	_, _ = mailbox.RequestOn(ctx, s.host.Libp2p(), node, mailbox.Request{Op: "register"})
}

// RegisterPush binds a device push token to our mailbox on every connected
// rendezvous node, so a deposit that lands while we're offline triggers a
// contentless wake. platform is "apns" or "fcm". Best-effort and idempotent;
// the mobile shell calls it after login and on token rotation. A no-op when no
// rendezvous node is connected (re-registration happens on the next connect via
// the mailbox-register path is per-node; push tokens are pushed here explicitly).
func (s *Service) RegisterPush(platform, token string) {
	if platform == "" || token == "" {
		return
	}
	for _, node := range s.mailboxNodes() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		_, _ = mailbox.RequestOn(ctx, s.host.Libp2p(), node, mailbox.Request{
			Op: "register-push", Platform: platform, Token: token,
		})
		cancel()
	}
}

// UnregisterPush removes a device token from our mailbox on all connected nodes
// (e.g. on logout).
func (s *Service) UnregisterPush(token string) {
	if token == "" {
		return
	}
	for _, node := range s.mailboxNodes() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		_, _ = mailbox.RequestOn(ctx, s.host.Libp2p(), node, mailbox.Request{
			Op: "unregister-push", Token: token,
		})
		cancel()
	}
}

func (s *Service) mbxDeposit(node peer.ID, target string, env []byte) {
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	_, _ = mailbox.RequestOn(ctx, s.host.Libp2p(), node, mailbox.Request{
		Op: "deposit", Target: target, Envelope: env,
		TTLSeconds: int64((14 * 24 * time.Hour).Seconds()),
	})
}

// drainMailbox pulls our pending envelopes from a node, applies them through
// the normal receive path, and acks the ones we processed so the node deletes
// them.
func (s *Service) drainMailbox(node peer.ID) {
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()
	resp, err := mailbox.RequestOn(ctx, s.host.Libp2p(), node, mailbox.Request{Op: "drain"})
	if err != nil || len(resp.Envelopes) == 0 {
		return
	}
	var acked []string
	for _, e := range resp.Envelopes {
		plain, ok := s.openEnvelope(e.Data)
		if !ok {
			acked = append(acked, e.ID) // undecryptable/foreign — drop it
			continue
		}
		var p mbxPayload
		if json.Unmarshal(plain, &p) == nil && len(p.GroupID) > 0 {
			s.receiveCiphertext(p.GroupID, p.CT)
		}
		acked = append(acked, e.ID)
	}
	ackCtx, ackCancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer ackCancel()
	_, _ = mailbox.RequestOn(ackCtx, s.host.Libp2p(), node, mailbox.Request{Op: "ack", AckIDs: acked})
}

// isMailboxNode reports whether a peer is one of our configured rendezvous
// nodes (which host the mailbox).
func (s *Service) isMailboxNode(p peer.ID) bool {
	for _, pi := range s.bootstrap {
		if pi.ID == p {
			return true
		}
	}
	return false
}

// mailboxNodes returns the peer IDs of the configured rendezvous/relay nodes
// that are currently connected (mailbox lives on them).
func (s *Service) mailboxNodes() []peer.ID {
	var out []peer.ID
	connected := map[peer.ID]bool{}
	for _, p := range s.host.Peers() {
		connected[p] = true
	}
	for _, pi := range s.bootstrap {
		if connected[pi.ID] {
			out = append(out, pi.ID)
		}
	}
	return out
}

// mailboxPubFor returns a peer's mailbox public key from their learned profile.
func (s *Service) mailboxPubFor(fingerprint string) ([32]byte, bool) {
	s.mu.RLock()
	p, ok := s.profiles[fingerprint]
	s.mu.RUnlock()
	var pub [32]byte
	if !ok || len(p.MailboxPub) != 32 {
		return pub, false
	}
	copy(pub[:], p.MailboxPub)
	return pub, true
}
