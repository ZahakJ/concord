package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/identity"
	"github.com/zahak/concord/internal/link"
	cnet "github.com/zahak/concord/internal/net"
)

// Device linking (Phase 4), app layer. An already-unlocked device (the issuer)
// shows a QR/code carrying a single-use secret + its address. A new device (the
// joiner) scans it, dials over /concord/link/1.0.0, and both prove knowledge of
// the secret. The issuer then hands over the account seed, a device certificate
// it signs for the joiner's device key, and an invite code per guild so the new
// device can join every existing group. The joiner rewrites its keystore with
// the received account seed (keeping its own device seed), drops a device marker
// (device.go), and restarts in linked mode.
//
// Security: the libp2p stream is Noise-encrypted; the mutual HMAC proof over the
// out-of-band secret authenticates both ends, so a network attacker can neither
// impersonate the issuer (and feed a bogus account) nor the joiner. The secret
// is single-use and short-lived (see internal/link).

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

// LinkOffer starts an issuer-side linking session: it mints a single-use offer,
// remembers its secret, and returns the code to render as a QR. Only one offer
// is active at a time (a new call supersedes the previous).
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
	s.linkSecret = off.Secret
	s.linkMu.Unlock()
	return off.Encode(), nil
}

// CancelLinkOffer clears any active offer (the user closed the QR).
func (s *Service) CancelLinkOffer() {
	s.linkMu.Lock()
	s.linkSecret = nil
	s.linkMu.Unlock()
}

// handleLinkRequest is the issuer-side stream responder: verify the joiner's
// proof, then return the account material with a freshly-issued device cert.
func (s *Service) handleLinkRequest(_ context.Context, _ peer.ID, reqBytes []byte) ([]byte, error) {
	var req linkRequest
	if json.Unmarshal(reqBytes, &req) != nil {
		return nil, errors.New("app: bad link request")
	}
	s.linkMu.Lock()
	secret := s.linkSecret
	s.linkMu.Unlock()
	if secret == nil {
		return nil, errors.New("app: no active link offer")
	}
	if len(req.DevicePub) != 32 || !link.VerifyProof(secret, link.RoleJoiner, req.JoinerNonce, req.JoinerProof) {
		return nil, errors.New("app: link proof failed")
	}

	issuerNonce, err := link.Nonce()
	if err != nil {
		return nil, err
	}
	cert := s.id.IssueDeviceCertFor(req.DevicePub, req.DeviceName, time.Now().Unix())
	// Keep a copy: we are the only peer that can state this mapping from
	// authority rather than hearsay, and without it the new device is only
	// recognisable through the group rosters it manages to join. See
	// ownDevicesKey.
	s.rememberOwnDevice(cert)

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
	// Single-use: burn the secret so the offer can't be redeemed twice.
	s.linkMu.Lock()
	s.linkSecret = nil
	s.linkMu.Unlock()
	return json.Marshal(resp)
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
	for _, g := range s.Guilds() {
		if code, err := s.InviteCode(g.ID); err == nil {
			codes = append(codes, code)
			continue
		}
		if code, ok := s.ownerInviteCode(g); ok {
			codes = append(codes, code)
			continue
		}
		missing = append(missing, g.Name)
	}
	return codes, missing
}

// ownerInviteCode mints an invite for a guild we belong to but do not
// administer, addressed to the guild's real owner. False when we have no idea
// how to reach that owner, which is the only case in which a guild is genuinely
// unhandable.
func (s *Service) ownerInviteCode(g domain.Guild) (string, bool) {
	ownerFpr := identity.FingerprintOf(g.OwnerID)
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
		for _, a := range s.host.DialableAddrs(pid) {
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

// RedeemLink performs the joiner side of device linking: it dials the issuer
// named in the scanned code over a temporary libp2p host (keyed by a fresh
// device key), proves the shared secret, receives and MITM-verifies the account
// material, then writes this install's keystore with the received account seed
// (keeping its own device seed) and drops a device marker so the next login
// starts in linked mode. It returns the guild invites, profile, and verified
// fingerprints for the caller to apply after logging in, so the new device
// joins every existing group and knows who the account has verified.
func RedeemLink(ctx context.Context, dataDir, code, passphrase string) (LinkResult, error) {
	if passphrase == "" {
		return LinkResult{}, errors.New("app: passphrase required")
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
	if existing, lerr := identity.LoadKeystore(keystorePathIn(dataDir), passphrase); lerr == nil {
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
	reqBytes, _ := json.Marshal(linkRequest{
		JoinerNonce: joinerNonce,
		JoinerProof: link.Proof(off.Secret, link.RoleJoiner, joinerNonce),
		DevicePub:   joiner.DevicePublicKey(),
		DeviceName:  defaultDeviceName(),
	})
	// Generous overall budget: RequestLink retries the connect/hole-punch with
	// backoff inside this window, so a first-attempt miss (common off-LAN) doesn't
	// surface as a failure — it just tries again over a freshly-formed path.
	dialCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	respBytes, err := host.RequestLink(dialCtx, issuerAI, reqBytes)
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

	// Adopt the account: keystore = received account seed + our device seed;
	// marker = the issuer-signed cert. Next login runs in linked mode.
	linked, err := identity.FromSeeds(accountSeed, joiner.DeviceSeed())
	if err != nil {
		return LinkResult{}, err
	}
	if err := identity.SaveKeystore(keystorePathIn(dataDir), passphrase, linked); err != nil {
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
		return nil, errors.New("app: link rejected: " + resp.Error)
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
