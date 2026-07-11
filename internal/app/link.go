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
}

// LinkOffer starts an issuer-side linking session: it mints a single-use offer,
// remembers its secret, and returns the code to render as a QR. Only one offer
// is active at a time (a new call supersedes the previous).
func (s *Service) LinkOffer() (string, error) {
	ai := s.host.AddrInfo()
	addrs := make([]string, 0, len(ai.Addrs))
	for _, a := range ai.Addrs {
		// Keep the QR small and scannable: drop loopback and link-local addrs a
		// second device can't reach anyway; keep routable LAN/public addrs.
		if as := a.String(); !strings.Contains(as, "127.0.0.1") &&
			!strings.Contains(as, "/ip6/::1") && !strings.Contains(as, "fe80:") {
			addrs = append(addrs, as)
		}
	}
	// Carry our rendezvous nodes as circuit addresses too, so a joiner on a
	// different network can still reach us through the relay (mirrors InviteCode).
	for _, b := range LoadNetConfig(s.dataDir).Bootstrap {
		if b = strings.TrimSpace(b); b != "" {
			addrs = append(addrs, b+"/p2p-circuit")
		}
	}
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

	// One invite code per guild so the new device can join every existing group
	// after it restarts in linked mode. Issuer-owned guilds join immediately;
	// guilds where we're only a member point at the real owner.
	var invites []string
	for _, g := range s.Guilds() {
		if code, err := s.InviteCode(g.ID); err == nil {
			invites = append(invites, code)
		}
	}

	// Verified fingerprints travel with the account (sorted for determinism).
	var verified []string
	for f := range s.VerifiedFingerprints() {
		verified = append(verified, f)
	}
	sort.Strings(verified)

	resp := linkResponse{
		IssuerNonce:  issuerNonce,
		IssuerProof:  link.Proof(secret, link.RoleIssuer, issuerNonce),
		AccountSeed:  s.id.Seed(),
		Cert:         cert,
		Profile:      s.SelfProfile(),
		Bootstrap:    LoadNetConfig(s.dataDir).Bootstrap,
		GuildInvites: invites,
		Verified:     verified,
	}
	// Single-use: burn the secret so the offer can't be redeemed twice.
	s.linkMu.Lock()
	s.linkSecret = nil
	s.linkMu.Unlock()
	return json.Marshal(resp)
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
		DeviceName:  "New device",
	})
	dialCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	respBytes, err := host.RequestLink(dialCtx, issuerAI, reqBytes)
	if err != nil {
		return LinkResult{}, fmt.Errorf("app: reach linking device: %w", err)
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
		_ = SaveNetConfig(dataDir, NetConfig{Bootstrap: resp.Bootstrap})
	}
	return LinkResult{GuildInvites: resp.GuildInvites, Profile: resp.Profile, Verified: resp.Verified}, nil
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
