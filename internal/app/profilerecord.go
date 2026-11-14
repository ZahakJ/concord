package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/ZahakJ/concord/internal/identity"
)

// A profile record is how an account gets its own profile back after a
// recovery phrase restore, which carries the account key and nothing else.
//
// The profile itself lives in the device's database, and the copies other
// people hold are not evidence of anything: a gossiped announce is
// authenticated by the MLS sender at the moment it arrives, and nothing about
// it can be shown to a third party afterwards. So a restored device asking "what
// did I look like?" had only two possible answers — its own other devices, or
// anyone's unverifiable say-so — and it took neither.
//
// A record is the profile as the account announced it to its guilds, plus a
// signature by the ACCOUNT key over exactly those bytes. Two properties decide
// the shape:
//
//   - Only the account can check it. The signature travels sealed under a key
//     derived from the account seed, so the members who hold a record cannot
//     open it, and cannot show anyone that the account said this. The profile is
//     already seen by every member it was announced to; what the seal withholds
//     is a transferable proof of who wrote it. A restored device derives the same
//     key from the phrase, opens the seal and verifies the signature itself.
//   - Nobody can be handed somebody else's. A member answers a request with the
//     record it holds for the account the connection is authenticated to, and
//     nothing else, so the only profile anyone can ask for is their own.
//
// The edit stamp is inside the signed bytes, so a member replaying an old record
// can only ever offer an older profile, never a forged or a future one; the
// receiver keeps the newest it can verify. A device restored from nothing has no
// stamp of its own to compare against and takes the newest it is offered, so
// one malicious member alone could show it a profile the account has since
// replaced, until a newer copy turns up from anywhere — another member, another
// of its own devices, or its own next edit.

// profileRecordVersion separates this encoding from any later one.
const profileRecordVersion = 1

// profileRecordDomain is the signing context.
const profileRecordDomain = "concord/profile-record/v1\x00"

// profileRecordBody is exactly what is signed: the fields of a profile announce
// that are the account's own presentation. Not the mailbox key (derived from the
// seed, so a restored device has it anyway), not the now-playing activity
// (ephemeral), not the birthday (never announced to guilds).
//
// The signer and every holder build these bytes from the same announce, so the
// encoding has to be the same on both: a fixed struct through encoding/json,
// which writes fields in declaration order and escapes identically everywhere.
// A holder on a build whose Style has fewer dials than the signer's drops one of
// them and stores bytes the account never signed — the restored device then
// rejects that record, which is the right failure: it only ever costs a source.
type profileRecordBody struct {
	V         int    `json:"v"`
	Name      string `json:"name,omitempty"`
	Status    string `json:"status,omitempty"`
	Emoji     string `json:"emoji,omitempty"`
	Color     string `json:"color,omitempty"`
	Avatar    string `json:"avatar,omitempty"`
	Banner    string `json:"banner,omitempty"`
	Presence  string `json:"presence,omitempty"`
	Bio       string `json:"bio,omitempty"`
	Games     []Game `json:"games,omitempty"`
	Color2    string `json:"color2,omitempty"`
	Frame     string `json:"frame,omitempty"`
	Effect    string `json:"effect,omitempty"`
	Style     *Style `json:"style,omitempty"`
	UpdatedAt int64  `json:"at"`
}

// profileRecord is what a holder stores and serves: the body as text (no base64
// for a body that is mostly base64 pictures already) and the sealed signature.
type profileRecord struct {
	Body string `json:"b"`
	Seal []byte `json:"s"`
}

func encodeProfileBody(p Profile) []byte {
	b, _ := json.Marshal(profileRecordBody{
		V: profileRecordVersion, Name: p.Name, Status: p.Status, Emoji: p.Emoji, Color: p.Color,
		Avatar: p.Avatar, Banner: p.Banner, Presence: p.Presence, Bio: p.Bio, Games: p.Games,
		Color2: p.Color2, Frame: p.Frame, Effect: p.Effect, Style: p.Style, UpdatedAt: p.UpdatedAt,
	})
	return b
}

// profileSealKey is the symmetric key the signature is sealed under. Derived
// from the account seed, so every device of the account and a device restored
// from the phrase all hold it, and nobody else does.
func profileSealKey(id *identity.Identity) [32]byte {
	var k [32]byte
	r := hkdf.New(sha256.New, id.Seed(), nil, []byte("concord-profile-seal-v1"))
	_, _ = io.ReadFull(r, k[:])
	return k
}

func profileSigned(pub ed25519.PublicKey, body []byte) []byte {
	m := make([]byte, 0, len(profileRecordDomain)+len(pub)+len(body))
	m = append(m, profileRecordDomain...)
	m = append(m, pub...)
	return append(m, body...)
}

// sealProfileBody signs body with the account key and seals the signature.
func sealProfileBody(id *identity.Identity, body []byte) []byte {
	sig := id.Sign(profileSigned(id.PublicKey(), body))
	var nonce [24]byte
	_, _ = rand.Read(nonce[:])
	key := profileSealKey(id)
	return secretbox.Seal(nonce[:], sig, &nonce, &key)
}

var errProfileRecord = errors.New("app: profile record does not verify for this account")

// openProfileRecord verifies a record against OUR account and returns the
// profile it carries. Anything else — no seal, a seal under another account's
// key, a signature by another key, a body edited after signing, a body this
// build cannot read — is refused.
func openProfileRecord(id *identity.Identity, rec profileRecord) (Profile, error) {
	if len(rec.Seal) < 24+secretbox.Overhead || rec.Body == "" {
		return Profile{}, errProfileRecord
	}
	var nonce [24]byte
	copy(nonce[:], rec.Seal[:24])
	key := profileSealKey(id)
	sig, ok := secretbox.Open(nil, rec.Seal[24:], &nonce, &key)
	if !ok || len(sig) != ed25519.SignatureSize {
		return Profile{}, errProfileRecord
	}
	if !identity.Verify(id.PublicKey(), profileSigned(id.PublicKey(), []byte(rec.Body)), sig) {
		return Profile{}, errProfileRecord
	}
	var b profileRecordBody
	if json.Unmarshal([]byte(rec.Body), &b) != nil || b.V != profileRecordVersion || b.UpdatedAt <= 0 {
		return Profile{}, errProfileRecord
	}
	return Profile{
		Name: b.Name, Status: b.Status, Emoji: b.Emoji, Color: b.Color, Avatar: b.Avatar,
		Banner: b.Banner, Presence: b.Presence, Bio: b.Bio, Games: b.Games,
		Color2: b.Color2, Frame: b.Frame, Effect: b.Effect, Style: b.Style,
		UpdatedAt: b.UpdatedAt,
		// The record is the account's whole presentation: an empty list in it
		// means no games, not "not sent".
		GamesSet: true,
	}, nil
}

// profileSealCache keeps the last seal, so re-announcing an unchanged profile
// costs a hash rather than a signature, and — more to the point — produces the
// same bytes, which is what lets publishProfile's "already said this" check
// keep working with a randomly-nonced seal in the frame.
type profileSealCache struct {
	mu   sync.Mutex
	sum  [32]byte
	seal []byte
}

// profileSealFor returns the seal for a profile we are about to announce, or
// nil for a profile with no edit stamp (nothing a restored device could order).
func (s *Service) profileSealFor(p Profile) []byte {
	if p.UpdatedAt <= 0 {
		return nil
	}
	body := encodeProfileBody(p)
	sum := sha256.Sum256(body)
	s.profileSeal.mu.Lock()
	defer s.profileSeal.mu.Unlock()
	if s.profileSeal.seal != nil && s.profileSeal.sum == sum {
		return s.profileSeal.seal
	}
	s.profileSeal.sum, s.profileSeal.seal = sum, sealProfileBody(s.id, body)
	return s.profileSeal.seal
}

// keepProfileRecord stores the record a member's announce carried. The caller
// has bound the announce to its MLS-authenticated author, which is the only
// thing a holder can check: it cannot open the seal, and does not need to — a
// holder that keeps a bad record only ever hands the account something its own
// device will refuse.
func (s *Service) keepProfileRecord(fingerprint string, p Profile, seal []byte) {
	if fingerprint == "" || fingerprint == s.id.Fingerprint() || len(seal) == 0 || p.UpdatedAt <= 0 {
		return
	}
	if len(seal) > 24+secretbox.Overhead+ed25519.SignatureSize {
		return
	}
	if p.UpdatedAt <= s.store.ProfileRecordAt(fingerprint) {
		return // the common case: an announce we already hold, re-sent on a reconnect
	}
	raw, err := json.Marshal(profileRecord{Body: string(encodeProfileBody(p)), Seal: seal})
	if err != nil {
		return
	}
	_, _ = s.store.SaveProfileRecord(fingerprint, p.UpdatedAt, raw)
}

// profileRequest is what a device asks with: the stamp it already has, so a
// holder with nothing newer answers with nothing instead of a quarter megabyte.
type profileRequest struct {
	Have int64 `json:"have"`
}

// handleProfileRequest answers "what do you hold for me?". Whose record is
// served is decided by the connection, not the request: presence() maps the
// authenticated key to its account (directly for an account key, through a
// verified certificate for a linked device), so a peer can only ever be handed
// its own account's record.
func (s *Service) handleProfileRequest(_ context.Context, from peer.ID, raw []byte) ([]byte, error) {
	var req profileRequest
	_ = json.Unmarshal(raw, &req)
	fpr := s.presence(from).Fingerprint
	if fpr == "" {
		return []byte("{}"), nil
	}
	if fpr == s.id.Fingerprint() {
		// One of our own devices — or our account restored somewhere new. We
		// hold the key, so we sign the stored profile fresh.
		p := s.selfStoredProfile()
		if p.UpdatedAt <= req.Have {
			return []byte("{}"), nil
		}
		body := encodeProfileBody(p)
		return json.Marshal(profileRecord{Body: string(body), Seal: sealProfileBody(s.id, body)})
	}
	rec, at, err := s.store.ProfileRecord(fpr)
	if err != nil || rec == nil || at <= req.Have {
		return []byte("{}"), nil
	}
	return rec, nil
}

// profileHintFor is the edit stamp of the record we could hand peer p, for the
// hello: 8 bytes that let p decide whether asking is worth a stream. Zero for a
// peer we can tell nothing.
func (s *Service) profileHintFor(p peer.ID) int64 {
	fpr := s.presence(p).Fingerprint
	if fpr == "" {
		return 0
	}
	if fpr == s.id.Fingerprint() {
		if !s.ownDevice(p) {
			return 0
		}
		return s.profileStamp()
	}
	return s.store.ProfileRecordAt(fpr)
}

// fetchOwnProfile asks p for our record when its hint says it holds a newer one
// than ours, verifies it, and adopts it. One fetch per peer at a time, each
// hint fetched once, and at most one fetch per peer every profileAskGap: the
// hint comes from the peer, so a peer that lies about it costs us a stream now
// and then and nothing more. A failed fetch is retried here, and if it still
// fails the hint stays "heard" for the own-device beat to try again.
func (s *Service) fetchOwnProfile(p peer.ID, hint int64) {
	if hint <= s.profileStamp() || p == s.host.PeerID() {
		return
	}
	o := &s.ownProfile
	o.mu.Lock()
	o.init()
	if hint > o.heard[p] {
		o.heard[p] = hint
	}
	if hint <= o.asked[p] {
		o.mu.Unlock()
		return
	}
	if o.fetching[p] || time.Since(o.lastAsk[p]) < profileAskGap {
		// Busy or too soon: look again once the gap has passed, at whatever
		// is newest by then. One such wait per peer.
		if !o.pending[p] {
			o.pending[p] = true
			go func() {
				select {
				case <-s.ctx.Done():
					return
				case <-time.After(profileAskGap):
				}
				o.mu.Lock()
				o.pending[p] = false
				next := o.heard[p]
				o.mu.Unlock()
				s.fetchOwnProfile(p, next)
			}()
		}
		o.mu.Unlock()
		return
	}
	o.fetching[p], o.lastAsk[p] = true, time.Now()
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		o.fetching[p] = false
		o.mu.Unlock()
	}()
	for attempt, wait := 0, time.Second; attempt < profileTellTries; attempt, wait = attempt+1, wait*2 {
		if attempt > 0 {
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		if hint <= s.profileStamp() {
			return // arrived some other way meanwhile
		}
		req, _ := json.Marshal(profileRequest{Have: s.profileStamp()})
		ctx, cancel := context.WithTimeout(s.ctx, helloTimeout*4)
		resp, err := s.host.AskProfile(ctx, p, req)
		cancel()
		if err != nil {
			continue
		}
		// Answered: adopted, or refused as not ours. Either way this hint is
		// spent — asking again would get the same answer.
		s.adoptProfileRecord(resp)
		o.mu.Lock()
		if hint > o.asked[p] {
			o.asked[p] = hint
		}
		o.mu.Unlock()
		return
	}
}

// profileAskGap spaces fetches from one peer.
const profileAskGap = 2 * time.Second

// adoptProfileRecord applies a record served to us, if it verifies against our
// own account key and is newer than what this device has.
func (s *Service) adoptProfileRecord(raw []byte) bool {
	var rec profileRecord
	if json.Unmarshal(raw, &rec) != nil || rec.Body == "" {
		return false
	}
	p, err := openProfileRecord(s.id, rec)
	if err != nil {
		return false
	}
	// The record never carries a birthday (it is not announced to guilds), so
	// whatever this device has stays; on a restored device that is nothing.
	p.Birthday, _ = s.store.GetSetting("birthday")
	return s.AdoptLinkedProfile(p)
}
