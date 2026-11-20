package app

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	lcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/link"
	cnet "github.com/ZahakJ/concord/internal/net"
)

// Device linking, app layer. An already-unlocked device (the issuer) shows a
// QR/code carrying a single-use secret + its address. A new device (the joiner)
// scans it, dials over /concord/link/2.0.0 from a host keyed by its device key,
// and proves knowledge of the secret. The issuer takes the code for that one
// request, then asks its owner: it shows the requesting device's name and a
// six-digit code (link.SAS), the joiner shows the same six digits, and only on
// the owner's yes, within linkConfirmWindow, does the issuer hand over the
// account seed, a device certificate it signs for the joiner's device key, and
// an invite code per guild. The joiner rewrites its keystore with the received
// account seed (keeping its own device seed), drops a device marker
// (device.go), and restarts in linked mode.
//
// Security. The libp2p stream is Noise-encrypted, and the joiner dials the
// PeerID in the code, so nobody on the network can stand in for the issuer.
// The secret's HMAC proofs show knowledge of the code; they do not stop
// somebody else who saw it (a photographed QR, the clipboard) from asking.
// What stops them is the rest of the issuer side:
//   - the device key to certify must be the dialler's own key (its PeerID), so
//     the six digits the issuer shows are about the device that is connected;
//   - the code is taken in the same critical section that checks it, so one
//     request per code gets that far, and the joiner shows its digits only
//     after its request was the one taken;
//   - the issuer refuses a code older than link.OfferTTL by its own clock;
//   - the seed leaves only after the owner confirms on the issuer.
// The pre-0.61 protocol, /concord/link/1.0.0, is answered only with a refusal
// telling the older device to update (refuseOldLinkRequest).

// linkRequest is the joiner→issuer frame.
type linkRequest struct {
	JoinerNonce []byte `json:"jn"`
	JoinerProof []byte `json:"jp"` // Proof(secret, RoleJoiner, JoinerNonce)
	DevicePub   []byte `json:"dp"` // joiner's device public key (to be certified)
	DeviceName  string `json:"dn"`
}

// linkResponse is the issuer→joiner frame carrying the account material.
type linkResponse struct {
	IssuerNonce  []byte               `json:"in"`
	IssuerProof  []byte               `json:"ip"` // Proof(secret, RoleIssuer, IssuerNonce)
	AccountSeed  []byte               `json:"seed"`
	Cert         *identity.DeviceCert `json:"cert"` // account-signed for the joiner's device key
	Profile      Profile              `json:"profile"`
	Bootstrap    []string             `json:"bootstrap"`
	GuildInvites []string             `json:"guildInvites"`
	// MissingGuilds names the guilds we could NOT hand over (see
	// linkGuildInvites). Reported rather than dropped: a device that looks
	// linked but is silently missing servers is the worst possible outcome.
	MissingGuilds []string `json:"missingGuilds,omitempty"`
	// Fingerprints this account has human-verified. Verification is the
	// account holder's knowledge ("I compared safety numbers with them"), so
	// it travels with the account to every linked device.
	Verified []string `json:"verified,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// LinkResult is what the joiner gets out of RedeemLink: everything the caller
// applies after logging in as the linked device.
type LinkResult struct {
	GuildInvites []string // one invite per shared guild, redeem after login
	Profile      Profile  // the account's profile (name/avatar/…)
	Verified     []string // fingerprints the account has verified
	// MissingGuilds names guilds the issuer holds but could not hand over — the
	// owner was unreachable and unknown to us. The caller should tell the user
	// which servers to re-join by code instead of leaving them to notice.
	MissingGuilds []string
}

// linkConfirmWindow is how long the owner has to answer on the issuer once a
// device has asked. The code's own two minutes bound when a device may ask.
const linkConfirmWindow = 60 * time.Second

// issuedOffer is the issuer's record of the code it is showing.
type issuedOffer struct {
	secret []byte
	peerID string    // our PeerID as written into the code
	made   time.Time // our clock: the code dies OfferTTL after this
}

// expired: the code is past OfferTTL by either clock. The monotonic reading
// is immune to the wall clock being set back; it also stands still while the
// machine sleeps on Linux and macOS, so the wall clock is checked as well, or
// a code shown before a suspend would come back alive after it.
func (o *issuedOffer) expired() bool {
	return time.Since(o.made) > link.OfferTTL || time.Now().Round(0).Sub(o.made.Round(0)) > link.OfferTTL
}

// linkAsk is a device that proved the code and waits for the owner's answer.
type linkAsk struct {
	id       string
	name     string
	sas      string
	deadline time.Time
	answer   chan bool // buffered: the first answer is the one that counts
	auto     bool      // tests only: answer yes without a person
}

// linkWait is the body of the issuer's wait frame.
type linkWait struct {
	Seconds int `json:"s"` // how long the owner has to answer
}

// LinkPrompt is the question the issuer's dialog puts to its owner.
type LinkPrompt struct {
	ID          string `json:"id"`
	DeviceName  string `json:"deviceName"`
	SAS         string `json:"sas"` // "123 456"; the new device shows the same
	SecondsLeft int    `json:"secondsLeft"`
}

// LinkStatus is what the issuer's dialog shows besides the QR code.
type LinkStatus struct {
	// Prompt is a device waiting for the owner's yes, or nil.
	Prompt *LinkPrompt `json:"prompt,omitempty"`
	// Used: a device took the code and nothing is waiting any more (it was
	// linked, declined, or not answered in time). The code no longer works.
	Used bool `json:"used,omitempty"`
	// Expired: the code is past its two minutes and no longer works.
	Expired bool `json:"expired,omitempty"`
	// OldDevice: a device with this code tried to link over the pre-0.61
	// protocol and was refused; it needs updating.
	OldDevice bool `json:"oldDevice,omitempty"`
}

// LinkWaiting is what the new device shows while the issuer asks its owner.
type LinkWaiting struct {
	SAS        string `json:"sas"`
	DeviceName string `json:"deviceName"`
	Seconds    int    `json:"seconds"`
}

// The issuer's refusals, sent to the joiner as the response's Error. The
// front end matches on these words (frontend/src/lib/errors.js).
const (
	linkErrBadRequest = "bad link request"
	linkErrNoOffer    = "no active link offer"
	linkErrProof      = "link proof failed"
	linkErrUsed       = "that code was already used by another device"
	linkErrExpired    = "code expired"
	linkErrKey        = "the link request did not come from the device it asks to add"
	linkErrDeclined   = "the other device declined this link"
	linkErrUnanswered = "nobody confirmed this link on the other device in time"
	// linkErrOldJoiner is all a pre-0.61 device gets. Its front end shows
	// any refusal as "already used or closed", so the issuer's dialog says
	// it too (LinkStatus.OldDevice).
	linkErrOldJoiner = "this device runs an older Concord: linking now asks for a yes on the device that shows the code. Update Concord on this device, then scan a new code"
)

// testLinkAutoConfirm answers yes for the owner, for tests that link devices
// without a person. Only a _test.go file sets it; Service.linkManual opts a
// test's issuer out.
var testLinkAutoConfirm bool

// LinkOffer starts an issuer-side linking session: it mints a single-use offer,
// remembers its secret and when it made it, and returns the code to render as
// a QR. Only one offer is active at a time: a new call supersedes the previous
// one and drops any question still open about it.
func (s *Service) LinkOffer() (string, error) {
	ai := s.host.AddrInfo()
	// Same address set as an invite code, including relay paths for the
	// reservations we hold — the QR stays small because Encode ranks and caps,
	// and drops loopback/link-local a second device can't reach anyway.
	addrs := codeAddrs(ai.Addrs, LoadNetConfig(s.dataDir).Bootstrap)
	off, err := link.NewOffer(ai.ID.String(), addrs)
	if err != nil {
		return "", err
	}
	off.Bootstrap = LoadNetConfig(s.dataDir).Bootstrap
	s.linkMu.Lock()
	s.linkOffer = &issuedOffer{secret: off.Secret, peerID: off.PeerID, made: time.Now()}
	s.linkSpent = nil
	s.linkOldDevice = false
	s.dropLinkAskLocked()
	s.linkMu.Unlock()
	return off.Encode(), nil
}

// CancelLinkOffer clears any active offer (the user closed the QR), and says
// no to a device still waiting.
func (s *Service) CancelLinkOffer() {
	s.linkMu.Lock()
	s.linkOffer = nil
	s.linkSpent = nil
	s.linkOldDevice = false
	s.dropLinkAskLocked()
	s.linkMu.Unlock()
}

// dropLinkAskLocked answers no to the device waiting, if any. linkMu held.
func (s *Service) dropLinkAskLocked() {
	if a := s.linkAsk; a != nil {
		select {
		case a.answer <- false:
		default:
		}
		s.linkAsk = nil
	}
}

// LinkStatus reports the issuer's linking state for its dialog.
func (s *Service) LinkStatus() LinkStatus {
	s.linkMu.Lock()
	defer s.linkMu.Unlock()
	var st LinkStatus
	if a := s.linkAsk; a != nil {
		left := int((time.Until(a.deadline) + time.Second - 1) / time.Second)
		st.Prompt = &LinkPrompt{ID: a.id, DeviceName: a.name, SAS: a.sas, SecondsLeft: max(left, 0)}
	} else if s.linkOffer == nil && s.linkSpent != nil {
		st.Used = true
	}
	if o := s.linkOffer; o != nil && o.expired() {
		st.Expired = true
	}
	st.OldDevice = s.linkOldDevice
	return st
}

// ConfirmLink is the owner's answer on the issuer to the device in the prompt
// with this id: yes releases the account to it, no refuses it. A yes after the
// window, or for a device no longer waiting, is refused.
func (s *Service) ConfirmLink(id string, yes bool) error {
	s.linkMu.Lock()
	defer s.linkMu.Unlock()
	a := s.linkAsk
	if a == nil || a.id != id {
		return errors.New("app: that device is no longer waiting; make a new code")
	}
	if yes && time.Now().After(a.deadline) {
		return errors.New("app: too late to confirm that device; make a new code")
	}
	select {
	case a.answer <- yes:
	default:
	}
	return nil
}

// linkCertTime is the issue time written into a device certificate. A
// variable only so a test can make two links of one device carry different
// times without waiting for the clock's second to turn.
var linkCertTime = func() int64 { return time.Now().Unix() }

// linkRefusal is a final answer that refuses the link, with the reason.
func linkRefusal(reason string) ([]byte, error) {
	return json.Marshal(linkResponse{Error: reason})
}

// diallerIs reports whether the dialler's PeerID is the Ed25519 key pub.
func diallerIs(from peer.ID, pub []byte) bool {
	pk, err := from.ExtractPublicKey()
	if err != nil {
		return false
	}
	raw, err := pk.Raw()
	return err == nil && pk.Type() == lcrypto.Ed25519 && subtle.ConstantTimeCompare(raw, pub) == 1
}

// linkDeviceLabel is the requesting device's name as the prompt shows it.
func linkDeviceLabel(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if r := []rune(name); len(r) > 64 {
		name = string(r[:64]) + "…"
	}
	if name == "" {
		return "A new device"
	}
	return name
}

// takeLinkOffer checks a request against the code on show and, if it proves
// the code, takes the code for it, all in one hold of linkMu: of any number of
// requests for one code, exactly one gets past here. It returns the question
// to put to the owner, or the reason for refusing.
func (s *Service) takeLinkOffer(req linkRequest) (*linkAsk, []byte, string) {
	id, err := link.Nonce()
	if err != nil {
		return nil, nil, linkErrBadRequest
	}
	s.linkMu.Lock()
	defer s.linkMu.Unlock()
	window := s.linkWindow
	if window <= 0 {
		window = linkConfirmWindow
	}
	o := s.linkOffer
	if o == nil {
		if s.linkSpent != nil && link.VerifyProof(s.linkSpent, link.RoleJoiner, req.JoinerNonce, req.JoinerProof) {
			return nil, nil, linkErrUsed
		}
		return nil, nil, linkErrNoOffer
	}
	if !link.VerifyProof(o.secret, link.RoleJoiner, req.JoinerNonce, req.JoinerProof) {
		return nil, nil, linkErrProof
	}
	// Past its two minutes the code stays on record only to be refused as
	// expired, to this request and any later one, and so the dialog keeps
	// saying it expired.
	if o.expired() {
		return nil, nil, linkErrExpired
	}
	s.linkOffer = nil
	s.linkSpent = o.secret
	s.dropLinkAskLocked()
	a := &linkAsk{
		id:       hex.EncodeToString(id[:16]),
		name:     linkDeviceLabel(req.DeviceName),
		sas:      link.SAS(o.secret, o.peerID, req.DevicePub),
		deadline: time.Now().Add(window),
		answer:   make(chan bool, 1),
		auto:     testLinkAutoConfirm && !s.linkManual,
	}
	s.linkAsk = a
	return a, o.secret, ""
}

// endLinkAsk clears the question once its request is answered.
func (s *Service) endLinkAsk(a *linkAsk) {
	s.linkMu.Lock()
	if s.linkAsk == a {
		s.linkAsk = nil
	}
	s.linkMu.Unlock()
}

// handleLinkRequest is the issuer side of /concord/link/2.0.0: check the
// request, take the code for it, tell the joiner to wait, ask the owner, and
// on a yes return the account material with a freshly issued device cert.
// Every refusal is a final answer naming the reason, so the new device can
// say what happened.
func (s *Service) handleLinkRequest(ctx context.Context, from peer.ID, reqBytes []byte, wait func([]byte) error) ([]byte, error) {
	var req linkRequest
	if json.Unmarshal(reqBytes, &req) != nil || len(req.DevicePub) != ed25519.PublicKeySize {
		return linkRefusal(linkErrBadRequest)
	}
	// The key to certify must be the key of this connection. Then the digits
	// shown to the owner are about the device actually connected, and the
	// device the owner holds shows the same digits only if it is that device.
	if !diallerIs(from, req.DevicePub) {
		return linkRefusal(linkErrKey)
	}
	ask, secret, refused := s.takeLinkOffer(req)
	if refused != "" {
		return linkRefusal(refused)
	}
	defer s.endLinkAsk(ask)

	body, _ := json.Marshal(linkWait{Seconds: int(time.Until(ask.deadline).Round(time.Second) / time.Second)})
	if err := wait(body); err != nil {
		return nil, err
	}
	if ask.auto {
		select {
		case ask.answer <- true:
		default:
		}
	}
	timer := time.NewTimer(time.Until(ask.deadline))
	defer timer.Stop()
	var yes bool
	select {
	case yes = <-ask.answer:
	case <-timer.C:
		// ConfirmLink takes a yes only before the deadline, so an answer
		// already waiting here was given in time.
		select {
		case yes = <-ask.answer:
		default:
			return linkRefusal(linkErrUnanswered)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if !yes {
		return linkRefusal(linkErrDeclined)
	}
	resp, err := s.linkHandover(secret, req)
	if err != nil {
		return nil, err
	}
	return json.Marshal(resp)
}

// refuseOldLinkRequest answers the pre-0.61 protocol. That protocol handed the
// seed to anyone who knew the code, so it never links now: a device on it is
// told to update. A request that proves the code on show also tells the
// owner's dialog, because the older device's own screen will only say the
// code was used or closed. The code itself is left as it was.
func (s *Service) refuseOldLinkRequest(_ context.Context, _ peer.ID, reqBytes []byte) ([]byte, error) {
	var req linkRequest
	if json.Unmarshal(reqBytes, &req) == nil {
		s.linkMu.Lock()
		if o := s.linkOffer; o != nil && link.VerifyProof(o.secret, link.RoleJoiner, req.JoinerNonce, req.JoinerProof) {
			s.linkOldDevice = true
		}
		s.linkMu.Unlock()
	}
	return linkRefusal(linkErrOldJoiner)
}

// linkHandover builds the account material for a confirmed request.
func (s *Service) linkHandover(secret []byte, req linkRequest) (linkResponse, error) {
	issuerNonce, err := link.Nonce()
	if err != nil {
		return linkResponse{}, err
	}
	cert := s.id.IssueDeviceCertFor(req.DevicePub, req.DeviceName, linkCertTime())
	// Keep a copy: we are the only peer that can state this mapping from
	// authority rather than hearsay, and without it the new device is only
	// recognisable through the group rosters it manages to join. See
	// ownDevicesKey.
	s.rememberOwnDevice(cert)

	// Notes is created lazily, the first time anything asks for it — which for
	// most accounts is AFTER the second device exists. A device linked before
	// that inherits no Notes group, then mints its own the first time its owner
	// opens the scratchpad, and the account is left holding two groups both
	// called Notes with nothing to merge them. Two consequences, both silent:
	// notes written on one device never appear on the other, and read state
	// stops converging at all — the Notes meta topic is the only wire it
	// travels on, so each device publishes markers into a group the other
	// cannot decrypt.
	//
	// Minting it here, once, at the moment of handover, means there is exactly
	// one to hand over and the joiner adopts it instead of inventing a rival.
	// It belongs on THIS path and not inside linkGuildInvites, which is also
	// what a hello to an already-linked device calls — doing it there would
	// have every device create a Notes the first time it greets one of its own.
	_, _ = s.NotesDM()

	// One invite code per guild so the new device can join every existing group
	// after it restarts in linked mode.
	invites, missing := s.linkGuildInvites()

	// Verified fingerprints travel with the account (sorted for determinism).
	var verified []string
	for f := range s.VerifiedFingerprints() {
		verified = append(verified, f)
	}
	sort.Strings(verified)

	resp := linkResponse{
		IssuerNonce: issuerNonce,
		IssuerProof: link.Proof(secret, link.RoleIssuer, issuerNonce),
		AccountSeed: s.id.Seed(),
		Cert:        cert,
		// The STORED profile, edit stamp included: the joiner adopts it with
		// AdoptLinkedProfile, and the stamp is what keeps a later hello from
		// this issuer from reading as an older copy (see selfStoredProfile).
		Profile:       s.selfStoredProfile(),
		Bootstrap:     LoadNetConfig(s.dataDir).Bootstrap,
		GuildInvites:  invites,
		MissingGuilds: missing,
		Verified:      verified,
	}
	return resp, nil
}

// linkGuildInvites builds the invite codes handed to a newly linked device: one
// per guild the account belongs to, so the new device ends up in everything the
// account is already in. The second return names the guilds we could not hand
// over at all.
//
// A guild we administer mints a code pointing at us, and we admit the new device
// ourselves. A guild we are merely a MEMBER of cannot work that way: InviteCode
// refuses, because a code naming us as the owner advances the joiner onto a
// private epoch fork the moment it is redeemed. That refusal used to be a
// dropped error, so every server the user had merely joined was silently absent
// from the new device — an account that looked linked and was missing most of
// its groups, with nothing anywhere to say so. Address the code at the REAL
// owner instead: the new device then joins the way anybody joins, through a peer
// that is actually authorized to commit.
//
// This is not a way around the permission gate. Any member already knows the
// guild ID and the owner's addresses and could assemble these same bytes by
// hand; the gate exists to stop an honest client minting a code it cannot
// honour, and a code naming the owner is not one of those. The recipient is
// another device of an account the group already contains.
func (s *Service) linkGuildInvites() (codes, missing []string) {
	codes, left := s.ownGuildOffers()
	for _, g := range left {
		missing = append(missing, g.Name)
	}
	return codes, missing
}

// ownGuildOffers is linkGuildInvites with the guilds it could not hand over
// returned whole.
func (s *Service) ownGuildOffers() (codes []string, missing []domain.Guild) {
	for _, g := range s.Guilds() {
		if code, err := s.InviteCode(g.ID); err == nil {
			codes = append(codes, code)
			continue
		}
		if code, ok := s.ownerInviteCode(g); ok {
			codes = append(codes, code)
			continue
		}
		missing = append(missing, g)
	}
	return codes, missing
}

// ownerInviteCode mints an invite for a guild we belong to but do not
// administer, addressed to the guild's real owner. False when we have no idea
// how to reach that owner, which is the only case in which a guild is genuinely
// unhandable.
func (s *Service) ownerInviteCode(g domain.Guild) (string, bool) {
	// The EFFECTIVE owner: after a transfer, only the new owner (or a
	// manage-members holder) can actually commit the linked device in — a code
	// addressed to the dethroned founder would dial someone whose commits
	// honest peers refuse.
	if withholdOwnerCodes.Load() {
		return "", false
	}
	ownerFpr := s.effectiveOwner(g.ID)
	if ownerFpr == "" || ownerFpr == s.id.Fingerprint() {
		return "", false // our own guild — InviteCode already had its say
	}
	pid, addrs, ok := s.ownerAddrs(ownerFpr)
	if !ok {
		return "", false
	}
	bootstrap := LoadNetConfig(s.dataDir).Bootstrap
	// Relay paths as well as direct addresses. The owner is behind the same NAT
	// story as everyone else, and a circuit through a rendezvous we both use is
	// often the only way a device that has never been on this LAN can reach them.
	// codeAddrs is no help here: the circuits it appends are for OUR reservations.
	for _, b := range bootstrap {
		if b = strings.TrimSpace(b); b != "" && relayID(b) != "" {
			addrs = append(addrs, b+"/p2p-circuit")
		}
	}
	if len(addrs) == 0 {
		return "", false // a peer ID with nowhere to dial it is not an invite
	}
	return encodeInviteCode(inviteCode{
		GuildID:   g.ID,
		GuildName: g.Name,
		OwnerID:   pid.String(),
		OwnerAddr: addrs,
		Bootstrap: bootstrap,
	}), true
}

// ownerAddrs resolves an account fingerprint to the peer ID an invite code
// should name, plus the direct addresses we know for it. A live connection is
// the best source. Failing that, the contacts table remembers the peer ID an
// account was last seen under and the peer cache its addresses — which is what
// lets a device be linked on an evening when the server's owner is asleep.
func (s *Service) ownerAddrs(fingerprint string) (peer.ID, []string, bool) {
	if pid, ok := s.peerForFingerprint(fingerprint); ok {
		var addrs []string
		for _, a := range s.host.HandOnAddrs(pid) {
			addrs = append(addrs, a.String())
		}
		return pid, addrs, true
	}
	if s.store == nil {
		return "", nil, false
	}
	contacts, err := s.store.Contacts()
	if err != nil {
		return "", nil, false
	}
	cached := map[string][]string{}
	if s.peers != nil {
		for _, pi := range s.peers.AddrInfos() {
			for _, a := range pi.Addrs {
				cached[pi.ID.String()] = append(cached[pi.ID.String()], a.String())
			}
		}
	}
	var fallback peer.ID
	var found bool
	for _, c := range contacts {
		if c.Fingerprint != fingerprint {
			continue
		}
		pid, err := peer.Decode(c.PeerID)
		if err != nil {
			continue // the "fpr:" placeholder rows, and anything else undialable
		}
		if addrs := cached[c.PeerID]; len(addrs) > 0 {
			return pid, addrs, true // addresses beat a bare ID
		}
		fallback, found = pid, true
	}
	// No cached address, but the relay paths the caller appends can still make a
	// bare peer ID redeemable.
	return fallback, nil, found
}

// errLinkRaced: the keystore changed between the link reading it and the
// link writing it. Nothing on this device was changed.
var errLinkRaced = errors.New("this device's account changed while it was being linked (another Concord window, or a passphrase change) — nothing was changed; link again")

// testHookLinkBeforeWrite runs after the link has its answer from the issuer
// and has decided what to keep or erase, before it writes. Nil outside tests.
var testHookLinkBeforeWrite func()

// RedeemLink performs the joiner side of device linking: it dials the issuer
// named in the scanned code over a temporary libp2p host (keyed by a fresh
// device key), proves the shared secret, receives and MITM-verifies the account
// material, then writes this install's keystore with the received account seed
// (keeping its own device seed) and drops a device marker so the next login
// starts in linked mode. It returns the guild invites, profile, and verified
// fingerprints for the caller to apply after logging in, so the new device
// joins every existing group and knows who the account has verified.
//
// confirm is the ConfirmToken of the replacement the user agreed to (the
// incoming account, over the keystore they were shown); "" when they have
// agreed to nothing. A different account is refused before anything is written, with a
// *ReplaceRefusedError naming both accounts.
func RedeemLink(ctx context.Context, dataDir, code, passphrase, confirm string) (LinkResult, error) {
	return RedeemLinkWatch(ctx, dataDir, code, passphrase, confirm, nil)
}

// RedeemLinkWatch is RedeemLink that reports the wait for the owner's yes:
// onWait, if set, is called once the issuer has taken the code for this device
// and is asking its owner, with the six digits this device must show (the
// issuer shows the same). It is not called before that point, so a device
// whose request was not the one taken never shows digits.
//
// An issuer from before 0.61 is refused with net.ErrLinkPeerTooOld: it would
// hand the account over without asking, and this device does not fall back
// to that.
func RedeemLinkWatch(ctx context.Context, dataDir, code, passphrase, confirm string, onWait func(LinkWaiting)) (LinkResult, error) {
	if passphrase == "" {
		return LinkResult{}, errors.New("app: passphrase required")
	}
	// An erase of this device that did not finish is finished first: the
	// device being linked is the erased one, not what the erase left.
	if _, err := finishInterruptedErase(dataDir); err != nil {
		return LinkResult{}, err
	}
	off, err := link.DecodeOffer(code)
	if err != nil {
		return LinkResult{}, err
	}
	issuerID, err := peer.Decode(off.PeerID)
	if err != nil {
		return LinkResult{}, fmt.Errorf("app: bad issuer id: %w", err)
	}
	addrs := make([]multiaddr.Multiaddr, 0, len(off.Addrs))
	for _, a := range off.Addrs {
		if ma, merr := multiaddr.NewMultiaddr(a); merr == nil {
			addrs = append(addrs, ma)
		}
	}
	issuerAI := peer.AddrInfo{ID: issuerID, Addrs: addrs}

	// Our device identity. REUSE this install's existing device seed if it has
	// one (a re-link), so we keep a stable device identity instead of adding a
	// fresh device leaf to every group each time — that piled up phantom
	// members. Only the account seed is (re-)adopted from the issuer.
	joiner, err := identity.Generate()
	if err != nil {
		return LinkResult{}, err
	}
	// snap is the keystore as it is now. Everything decided below — reuse its
	// device seed, keep or erase what is here — is decided on these bytes, and
	// the write at the end goes through only if the file is still them: the
	// link can spend a minute and a half on the network in between, and a
	// passphrase changed in that time (another window, another process) must
	// not be sealed back to this one.
	ksPath := keystorePathIn(dataDir)
	snap, serr := identity.ReadKeystore(ksPath)
	seen := dataIdentity(dataDir)
	if serr != nil && !os.IsNotExist(serr) {
		return LinkResult{}, serr
	}
	if existing, lerr := identity.OpenKeystoreBytes(snap, passphrase); snap != nil && lerr == nil {
		if ds := existing.DeviceSeed(); ds != nil {
			if reused, ferr := identity.FromSeeds(joiner.Seed(), ds); ferr == nil {
				joiner = reused
			}
		}
	}
	bootstrap, _ := parseBootstrapPeers(off.Bootstrap)
	host, err := cnet.New(ctx, cnet.Config{
		Identity:       joiner,
		HostKey:        joiner.DeviceKey(),
		EnableMDNS:     true,
		EnableDHT:      len(bootstrap) > 0,
		BootstrapPeers: bootstrap,
	})
	if err != nil {
		return LinkResult{}, fmt.Errorf("app: link host: %w", err)
	}
	defer host.Close()
	// Let relay/hole-punch paths form when the issuer is off-LAN.
	if len(bootstrap) > 0 {
		time.Sleep(2 * time.Second)
	}

	joinerNonce, err := link.Nonce()
	if err != nil {
		return LinkResult{}, err
	}
	deviceName := defaultDeviceName()
	reqBytes, _ := json.Marshal(linkRequest{
		JoinerNonce: joinerNonce,
		JoinerProof: link.Proof(off.Secret, link.RoleJoiner, joinerNonce),
		DevicePub:   joiner.DevicePublicKey(),
		DeviceName:  deviceName,
	})
	// Generous budget for reaching the issuer: RequestLinkConfirmed retries the
	// connect/hole-punch with backoff inside this window, so a first-attempt
	// miss (common off-LAN) doesn't surface as a failure — it just tries again
	// over a freshly-formed path. The wait for the owner's yes, once the
	// issuer has said it is asking, runs on the stream's own deadline.
	dialCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	respBytes, err := host.RequestLinkConfirmed(dialCtx, issuerAI, reqBytes, func(body []byte) {
		var w linkWait
		_ = json.Unmarshal(body, &w)
		if onWait != nil {
			onWait(LinkWaiting{
				SAS:        link.SAS(off.Secret, issuerID.String(), joiner.DevicePublicKey()),
				DeviceName: deviceName,
				Seconds:    w.Seconds,
			})
		}
	})
	if err != nil {
		return LinkResult{}, err
	}
	var resp linkResponse
	if json.Unmarshal(respBytes, &resp) != nil {
		return LinkResult{}, errors.New("app: bad link response")
	}
	accountSeed, err := verifyLinkResponse(off.Secret, joiner.DevicePublicKey(), &resp)
	if err != nil {
		return LinkResult{}, err
	}

	// Whose account is this? Only now can we know — the code does not say —
	// and nothing has been written yet. The same account keeps this device's
	// data, as re-linking always has. Another account would erase it, so that
	// needs the user's word for exactly this account; and its data goes, all
	// of it, before the new keystore lands. Keeping the database is what used
	// to publish the erased account's profile over the real one: its edit
	// stamp was newer, so the handover lost to it and every device adopted it.
	keep, err := replaceGate(dataDir, accountSeed, confirm, resp.Profile.Name, snap)
	if err != nil {
		return LinkResult{}, err
	}
	linked, err := identity.FromSeeds(accountSeed, joiner.DeviceSeed())
	if err != nil {
		return LinkResult{}, err
	}
	// With no keystore whatever else is here belongs to nobody that can open
	// it, and its settings are readable: unless kept, they must not become
	// the new account's. They go only once the new keystore is safely beside
	// the old one, in the same hold of the keystore lock, so a failure before
	// it is in place leaves the old one there.
	var eraseOld func() error
	if !keep {
		eraseOld = eraseIfUnchanged(dataDir, seen)
	}
	if testHookLinkBeforeWrite != nil {
		testHookLinkBeforeWrite()
	}

	// Adopt the account: keystore = received account seed + our device seed;
	// marker = the issuer-signed cert. Next login runs in linked mode.
	err = identity.ReplaceKeystoreWith(ksPath, passphrase, linked, snap, eraseOld)
	endReplaceErase(dataDir, err)
	if err != nil {
		if errors.Is(err, identity.ErrKeystoreChanged) || errors.Is(err, identity.ErrKeystoreExists) {
			return LinkResult{}, errLinkRaced
		}
		return LinkResult{}, err
	}
	if err := saveDeviceMarker(dataDir, resp.Cert); err != nil {
		return LinkResult{}, err
	}
	if len(resp.Bootstrap) > 0 {
		_ = SaveBootstrap(dataDir, resp.Bootstrap)
	}
	return LinkResult{
		GuildInvites:  resp.GuildInvites,
		Profile:       resp.Profile,
		Verified:      resp.Verified,
		MissingGuilds: resp.MissingGuilds,
	}, nil
}

// verifyLinkResponse checks an issuer's response on the joiner side: the issuer
// proved the secret, and the account seed the issuer sent is the one its cert
// certifies (so a tampered response can't graft a mismatched account onto our
// device key). Returns the account seed on success.
func verifyLinkResponse(secret, devicePub []byte, resp *linkResponse) ([]byte, error) {
	if resp.Error != "" {
		// The issuer's own words (the linkErr* reasons): it was authenticated
		// by Noise as the PeerID in the code.
		return nil, errors.New("app: " + resp.Error)
	}
	if !link.VerifyProof(secret, link.RoleIssuer, resp.IssuerNonce, resp.IssuerProof) {
		return nil, errors.New("app: issuer proof failed (possible MITM)")
	}
	if resp.Cert == nil || !resp.Cert.Verify() {
		return nil, errors.New("app: invalid device certificate")
	}
	// The cert must certify OUR device key…
	if !ed25519Equal(resp.Cert.DevicePub, devicePub) {
		return nil, errors.New("app: certificate is for a different device")
	}
	// …and the account seed must derive to the cert's account key.
	acct, err := identity.FromSeed(resp.AccountSeed)
	if err != nil {
		return nil, errors.New("app: bad account seed")
	}
	if !ed25519Equal(acct.PublicKey(), resp.Cert.AccountPub) {
		return nil, errors.New("app: account seed does not match certificate")
	}
	return resp.AccountSeed, nil
}
