package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zahak/concord/internal/identity"
	"github.com/zahak/concord/internal/version"
)

// The app half of /concord/hello (see net/hello.go for the wire and for why the
// protocol exists at all).
//
// Two rules decide everything here, and they are worth stating plainly because
// the naive version of each is wrong.
//
// WHO WE INTRODUCE OURSELVES TO. Only peers we can already place: our own
// account, a guild member, a contact. Not "everyone we connect to" — a linked
// device's account is not otherwise derivable from its PeerID, so volunteering
// the certificate to every DHT routing peer and every stranger answering the
// rendezvous key would publish "this anonymous-looking node belongs to account
// X" to exactly the audience the device key was protecting us from. Introducing
// ourselves only to peers we have identified leaks nothing they did not already
// know.
//
// That rule is also sufficient, which is the part that isn't obvious. The device
// that needs recognising is the one with the un-placeable PeerID — a linked
// phone — and the peer it needs recognising by is one it can place itself (the
// desktop's PeerID is the account key; a guild member is in the roster it holds).
// So the introduction always flows from the side that has the problem, and the
// far end answers with its own credential in the same round trip. Neither side
// ever has to ask a stranger who they are.
//
// WHAT WE DO WHEN ONE LANDS. Verify the certificate against the key the libp2p
// connection is already authenticated to (credentialBoundToPeer), then treat the
// peer as newly resolved: fire the presence event the gate had been swallowing,
// record and protect the connection, and run the catch-up the connect tail gave
// up on. Learning the mapping and leaving it at that is the bug this replaces —
// nothing re-ran the work that had been skipped, so the device stayed silent
// until something unrelated happened to shake the tree.

// helloFrame is one side of the exchange.
type helloFrame struct {
	// Credential is our MLS leaf credential: a device certificate (linked) or the
	// bare account key (original device). Empty means "I can't place you, so I'm
	// not telling you who I am" — a valid, deliberate answer.
	Credential []byte `json:"cred,omitempty"`
	// Name is a device label, shown only in the owning account's own device
	// diagnostics. Cosmetic; never trusted for anything.
	Name string `json:"name,omitempty"`
	// AppVersion is what this device is RUNNING, for the same diagnostics panel.
	// It exists because a fix "shipped to the phone" three times while nothing
	// proved the phone was running any of them — sideloaded Android builds do
	// not self-update, and there was no way to see that from the other device.
	AppVersion string `json:"appVersion,omitempty"`
	// Revoked, when present, tells the peer that the device it just introduced
	// has been unlinked from this account. Only ever sent to a device of OUR
	// account, and only carrying a revocation OUR account key signed — see
	// unlink.go for exactly how much that does and does not promise.
	Revoked *identity.DeviceRevocation `json:"revoked,omitempty"`
	// GuildInvites lets a device of this account into servers it is not in yet.
	//
	// Invites used to be handed over exactly once, during linking, so a guild
	// created or joined afterwards never reached an already-linked device. That
	// device then had no MLS group for it and no topic subscription — which
	// presents not as "a server is missing" but as "my messages don't reach my
	// other device" and "joining voice on my phone doesn't show on my desktop",
	// because there is no channel for any of it to travel on.
	//
	// ONLY ever sent to a peer proven to be a device of OUR OWN account (see
	// ownDevice), because an invite code grants entry to the guild. That is the
	// same trust level the link flow already applies to the same codes.
	GuildInvites []string `json:"guildInvites,omitempty"`
}

// helloTimeout bounds one exchange. A hello is an optimization, not a step in
// any user-visible flow, so it fails fast and silently.
const helloTimeout = 15 * time.Second

// greeted records the peers we have already introduced ourselves to on their
// current connection, so a reconnect greets again and a chatty notifier doesn't.
type greetSet struct {
	mu sync.Mutex
	m  map[peer.ID]bool
}

func (g *greetSet) claim(p peer.ID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil {
		g.m = map[peer.ID]bool{}
	}
	if g.m[p] {
		return false
	}
	g.m[p] = true
	return true
}

func (g *greetSet) release(p peer.ID) {
	g.mu.Lock()
	delete(g.m, p)
	g.mu.Unlock()
}

// canPlace reports whether we can already say which account a peer belongs to.
// It is the gate on introducing ourselves, and the gate on answering.
func (s *Service) canPlace(p peer.ID) bool {
	pp := s.presence(p)
	if pp.Fingerprint == "" {
		return false
	}
	// Our own account counts even though knownContact is about other people:
	// this is precisely the case of your own phone and your own desktop.
	return pp.Fingerprint == s.id.Fingerprint() || s.knownContact(pp.Fingerprint)
}

// greet introduces us to a peer we can place, and folds in whatever it says
// back. Safe to call for anyone: it costs one map lookup for a peer we can't
// place, and one small stream for a peer we can.
func (s *Service) greet(p peer.ID) {
	if p == s.host.PeerID() || s.isMailboxNode(p) {
		return // ourselves, and infrastructure that has no account to trade
	}
	// Claim BEFORE asking whether we can place them: canPlace runs knownContact,
	// which decodes every guild's roster, and this is called for every connection
	// the host makes — the rendezvous, DHT routing peers, relay candidates. One
	// evaluation per peer per connection is the budget; the claim is what holds
	// it to that. Released again on a "no" so a later membership change gets a
	// fresh look (rememberMembers re-greets for exactly that reason).
	if !s.greeted.claim(p) {
		return
	}
	if !s.canPlace(p) {
		s.greeted.release(p)
		return
	}
	req, err := json.Marshal(helloFrame{
		Credential:   s.myCredential,
		Name:         s.deviceLabel(),
		AppVersion:   version.Version,
		GuildInvites: s.guildInvitesFor(p),
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, helloTimeout)
	defer cancel()
	resp, err := s.host.SayHello(ctx, p, req)
	if err != nil {
		// Let a later connect try again — an old peer with no handler, or a
		// connection that died mid-stream, are both worth one more attempt.
		s.greeted.release(p)
		return
	}
	s.ingestHello(p, resp)
}

// solicitHello asks a peer we CANNOT place to please introduce itself, while
// introducing nothing of ours: the frame is empty, which the protocol already
// defines as "I'm not telling you who I am". The responder's own canPlace gate
// decides whether to answer with a credential, so the exchange leaks nothing
// in either direction that the other side wasn't already entitled to.
//
// greet cannot serve this case by design — its gate is that WE can place THEM.
// The caller here is the opposite situation: a signal arrived from a peer we
// cannot name (an unlearned linked device typing before we applied its
// add-commit), and the fix is their certificate, not ours. One attempt per
// peer per connection: an outsider gossiping into a topic would otherwise
// cost us a stream per keystroke.
func (s *Service) solicitHello(p peer.ID) {
	if p == s.host.PeerID() || s.isMailboxNode(p) {
		return
	}
	if !s.solicited.claim(p) {
		return
	}
	req, err := json.Marshal(helloFrame{})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, helloTimeout)
	defer cancel()
	resp, err := s.host.SayHello(ctx, p, req)
	if err != nil {
		s.solicited.release(p) // dead stream; a later signal may retry
		return
	}
	s.ingestHello(p, resp)
}

// handleHello is the responder: someone introduced themselves to us.
func (s *Service) handleHello(_ context.Context, from peer.ID, req []byte) ([]byte, error) {
	// One answer per connection. Composing an answer costs a knownContact, which
	// decodes the MLS roster of every guild — cheap once per peer, and not
	// something to let an anonymous peer trigger in a loop by reopening a stream.
	// The request itself is still ingested every time: it is signature-checked
	// and idempotent, and refusing to read it would be the wrong thing to
	// harden.
	s.ingestHello(from, req)
	if !s.answered.claim(from) {
		return json.Marshal(helloFrame{})
	}

	// Answer only someone we can place — which, thanks to the request we just
	// ingested, now includes the linked device that could not be placed a
	// microsecond ago. A peer we still can't place gets an empty frame: a clear
	// "nothing for you" rather than a dropped stream they'd retry against.
	var out helloFrame
	// The revocation goes out regardless of whether we would otherwise talk to
	// this peer: telling a device we unlinked that it has been unlinked is the
	// one message it is still owed, and it is signed, so it proves itself.
	out.Revoked = s.revocationFor(from)
	// The other half of the exchange: whichever side answers also offers. The
	// requester learned nothing about us until this frame, so its own offer may
	// have been empty; this is what makes the sync symmetric.
	out.GuildInvites = s.guildInvitesFor(from)
	if out.Revoked == nil && s.canPlace(from) {
		out.Credential, out.Name = s.myCredential, s.deviceLabel()
		out.AppVersion = version.Version
	}
	return json.Marshal(out)
}

// ingestHello verifies and applies one side of the exchange.
// ownDevice reports whether a peer is a device of this very account. presence()
// maps a device key to the account it was certified under, so this is true only
// after that device has proved itself with an account-signed certificate.
func (s *Service) ownDevice(p peer.ID) bool {
	if p == s.host.PeerID() {
		return false
	}
	return s.presence(p).Fingerprint == s.id.Fingerprint()
}

// guildInvitesFor returns invite codes to hand a device of ours, or nil for
// anyone else. The guard is the whole security of this: these codes admit the
// bearer to the guild.
func (s *Service) guildInvitesFor(p peer.ID) []string {
	if !s.ownDevice(p) {
		return nil
	}
	codes, _ := s.linkGuildInvites()
	return codes
}

// redeemOfferedInvites joins guilds this device is not in yet. Codes are only
// acted on when they came from a device of our own account; anything else is
// ignored outright rather than merely deprioritised.
func (s *Service) redeemOfferedInvites(from peer.ID, codes []string) {
	if len(codes) == 0 || !s.ownDevice(from) {
		return
	}
	have := map[string]bool{}
	for _, g := range s.Guilds() {
		have[g.ID] = true
	}
	for _, code := range codes {
		// Skip guilds this device already holds — and the check has to happen
		// HERE, because JoinViaInvite does not refuse a redundant join. It
		// round-trips to the owner, whose invite handler treats an
		// already-a-member joiner as a stale retry: it REMOVES our current leaf
		// and re-adds it, two epoch-advancing commits every member must apply
		// gaplessly. And since a successful join re-greets our own devices
		// (offerAfter), which hands the same codes back, the old version of this
		// loop — which redeemed every offered code unconditionally — fed itself:
		// two linked devices re-joined each other's guilds in a permanent
		// ping-pong, churning the group's epoch several times a second. Every
		// OTHER member had to keep up with that commit storm over gossip; each
		// dropped frame left them stranded at a dead epoch, unable to decrypt
		// anything newer. That was the reported "sometimes instant, sometimes
		// nothing arrives for minutes".
		ic, err := decodeInviteCode(strings.TrimSpace(code))
		if err != nil || have[ic.GuildID] {
			continue
		}
		have[ic.GuildID] = true
		// joinOfferedInvite re-checks "already a member" under the per-guild
		// join lock, which is what closes the race this map cannot see: a join
		// already in flight (the link flow's own redemption) when this hello
		// landed.
		s.joinOfferedInvite(code)
	}
}

// offerGuildsToOwnDevices re-greets every connected device of this account, so a
// guild created or joined right now reaches them immediately instead of on the
// next reconnect. The greet claim is released first because it is per-connection
// and would otherwise swallow the second hello.
func (s *Service) offerGuildsToOwnDevices() {
	for _, p := range s.host.Peers() {
		if !s.ownDevice(p) {
			continue
		}
		s.greeted.release(p)
		go s.greet(p)
	}
}

func (s *Service) ingestHello(from peer.ID, raw []byte) {
	var f helloFrame
	if json.Unmarshal(raw, &f) != nil {
		return
	}
	// Deliberately after the credential checks below would be too late: those
	// return early for a frame carrying no certificate, and a re-greet from a
	// device we already know is exactly that. ownDevice is what gates it.
	defer func() { go s.redeemOfferedInvites(from, f.GuildInvites) }()
	// A revocation naming THIS device is the one thing we act on before anything
	// else, since acting on it means we stop existing.
	if f.Revoked != nil {
		s.applyRevocation(f.Revoked)
	}
	if len(f.Credential) == 0 {
		return
	}
	// The proof. The connection is Noise-authenticated to `from`'s key, so
	// requiring the certificate to name that key means a peer can only ever claim
	// its own account-signed cert. Without this check a hello would be a way to
	// impersonate any account by replaying its public certificate.
	if !credentialBoundToPeer(f.Credential, from) {
		return
	}
	cert, isDevice := identity.ParseDeviceCert(f.Credential)
	// A device we unlinked stays recognisable to US — we still hold its record,
	// which is how the revocation reaches it — but it must not be re-adopted as
	// this account by the very hello that brings it back. Without this, unlinking
	// undid itself the moment the device reconnected.
	if isDevice && s.deviceIsRevoked(cert.DevicePub) {
		return
	}
	before := s.presence(from).Fingerprint
	s.learnDeviceCert(f.Credential)
	if isDevice {
		// If it's one of OURS, write it down — so this recognition survives
		// restarts even when the device's leaf is in no roster we can read.
		// noteOwnDevice ignores anything signed by another account.
		s.noteOwnDevice(cert, f.Name, f.AppVersion, true)
	}
	if after := s.presence(from).Fingerprint; after != before {
		s.peerResolved(from)
	}
}

// deviceLabel is the human name this install goes by in its own account's device
// list. It comes off our own certificate; an install that was never linked has
// none, and LinkedDevices names that row itself ("Original device").
func (s *Service) deviceLabel() string {
	if cert, ok := identity.ParseDeviceCert(s.myCredential); ok && cert.DeviceName != "" {
		return cert.DeviceName
	}
	return ""
}

// peerResolved runs everything that was skipped while a peer was unplaceable.
//
// This is the half that the certificate exchange alone would not fix. The
// connect path makes three judgements at connect time — is this presence worth
// showing, is this peer worth remembering, is this peer worth syncing from — and
// all three answer "no" for a device we cannot place. Nothing re-ran them, so a
// mapping learned thirty seconds later changed nothing until the next connect.
func (s *Service) peerResolved(p peer.ID) {
	pp := s.presence(p)
	if !s.knownContact(pp.Fingerprint) {
		return
	}
	go func() {
		s.emitPeerUp(pp)                // the presence event the gate swallowed
		s.recordPeer(p, pp.Fingerprint) // protect + remember: they're one of ours
		s.announceProfileAll()
		s.reannounceVoice()
		s.syncFromPeer(p) // the catch-up the connect tail declined to attempt
		s.healStrandedGuilds()
	}()
}
