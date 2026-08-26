package app

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync/atomic"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// Author signatures on records that outlive the connection they arrived on.
//
// THE PROBLEM, stated exactly. Two lanes carry a guild's content, and they
// authenticate different people:
//
//	live gossip  → MLS authenticates the SENDER, who is the author
//	history sync → MLS authenticates the RESPONDER, who is whoever answered
//
// The second is the hole. A catch-up response is one member's copy of their own
// disk, encrypted to the group at their epoch, and nothing in it is re-checked
// against the person it claims to come from. That is what DESIGN.md §13 meant by
// "an ordinary member could forge a new message attributed to someone else", and
// by GIF-pack and custom-emoji records arriving with no authority behind them:
// the gossip path checks that the announcer holds Manage Guild, and the sync
// path had nobody to check.
//
// The fix is the one the governance log, the story lane and the chronicle
// manifest already use: the record carries its author's own Ed25519 signature,
// so the relay attests nothing and does not have to. Every check here is
// arithmetic on bytes we already hold — no roster, no network, and no
// requirement that the author still be online, or even still a member.
//
// THE SIGNED FORM IS A CLOSED PROJECTION, not the record itself. A message
// cannot travel as json.RawMessage the way a chronicle manifest does: it is
// decrypted, decomposed into columns, sealed at rest and rebuilt from SQL before
// the next peer ever sees it, so the bytes that arrived are gone by the time
// anyone re-verifies. The signature therefore covers a fixed struct listing
// exactly the fields that survive that round trip losslessly, hashing the body
// rather than embedding it. Two consequences worth stating, because they are the
// whole reason the shape was chosen:
//
//   - Adding a field to domain.Message can never invalidate an old signature.
//     The projection is closed; growing the record grows only what is NOT signed.
//     (The marshal-the-whole-struct idiom govOp uses has the opposite property,
//     which is fine for a log entry that travels raw and fatal for one that does
//     not.)
//   - Mutable state is deliberately outside it. deleted / edited / pinned /
//     updated / reactions are not authorship, they are what happened to the
//     message afterwards, and they are governed by the destructive-reconcile
//     rule (trusted sources only) rather than by the author's pen. An edit is
//     the exception, and it re-signs — see messageRowSigningBytes.

// recordSigVersion is the first byte of meaning in every signed projection.
// It is under the signature, so a future version cannot be passed off as this
// one by anyone who does not hold the author's key.
const recordSigVersion = 1

// messageSigV1 is the canonical projection a message's author signs. Field names
// are short because this struct is marshalled once per message on both sides;
// they are also FROZEN — renaming one silently invalidates every signature ever
// made, which is exactly the kind of break that shows up as "history stopped
// syncing" three releases later.
type messageSigV1 struct {
	V         int    `json:"v"`
	Rec       string `json:"rec"` // "msg" — domain separation from every other signed record
	ID        string `json:"id"`
	ChannelID string `json:"ch"` // the context binding: a message cannot be replayed into another channel
	Sender    []byte `json:"snd"`
	Name      string `json:"nm"`
	Kind      string `json:"k"`
	ReplyTo   string `json:"re"`
	Dir       string `json:"dir"`
	Sent      int64  `json:"sent"` // unix nanoseconds, as stored
	Body      string `json:"body"` // sha256 of Content, hex
}

// messageRowSigningBytes is the canonical byte encoding for one message row,
// with the body given separately.
//
// The body is a parameter rather than being read off m because an EDIT re-signs
// the row it targets: the author holds the row, changes the text, and signs the
// same identity (id, channel, sender, original timestamp) around the new body.
// Without that a peer that receives the edited row through catch-up would hold a
// signature over text that no longer exists and would have to either reject the
// message or stop believing in the mechanism — and the first of those means
// every edited message vanishes from anyone's catch-up.
func messageRowSigningBytes(m domain.Message, body string) []byte {
	sum := sha256.Sum256([]byte(body))
	b, _ := json.Marshal(messageSigV1{
		V:         recordSigVersion,
		Rec:       "msg",
		ID:        m.ID,
		ChannelID: m.ChannelID,
		Sender:    m.Sender,
		Name:      m.Name,
		Kind:      m.Kind,
		ReplyTo:   m.ReplyTo,
		Dir:       domain.ValidDir(m.Dir),
		Sent:      m.Sent.UnixNano(),
		Body:      hex.EncodeToString(sum[:]),
	})
	return b
}

// messageSigningBytes is messageRowSigningBytes over the message's own content.
func messageSigningBytes(m domain.Message) []byte {
	return messageRowSigningBytes(m, m.Content)
}

// verifyMessageSig checks a message's signature against the key the message
// itself names as its sender.
//
// That is the whole check, and the self-reference is the point: the signature
// binds the body to WHOEVER holds that key. A stranger can mint a keypair and
// sign anything they like, but the result is attributed to a key nobody in the
// guild recognises — it renders as an unknown member, not as Alice. Forging a
// message attributed to Alice needs Alice's private key, which is the property
// §13 was missing.
//
// Deliberately NOT a roster check. History outlives membership: a message from
// somebody who has since left the guild must still verify years later, on a peer
// that has never met them and cannot ask anyone. Stories check the roster
// because a story carries only a fingerprint and has to look the key up;
// a message carries the key.
func verifyMessageSig(m domain.Message) bool {
	if len(m.Sender) != ed25519.PublicKeySize || len(m.Sig) != ed25519.SignatureSize {
		return false
	}
	return identity.Verify(m.Sender, messageSigningBytes(m), m.Sig)
}

// signMessage produces the author's signature over a message as it stands.
func (s *Service) signMessage(m domain.Message) []byte {
	return s.id.Sign(messageSigningBytes(m))
}

// messageAttestation decides what a message row arriving through HISTORY SYNC
// is worth, and is the single place that decision is made.
//
// The rules, and why each is the honest one:
//
//   - A signature that does not verify is a rejection, full stop. It is not a
//     downgrade and not a warning: bytes that claim a signature and fail it have
//     been altered in transit or fabricated, and there is no reading of that
//     under which storing the row is the careful choice.
//
//   - No signature at all is stored, and MARKED. It cannot be a rejection,
//     because "no signature" is the entire history of every guild that existed
//     before this field did, plus everything an older peer will ever serve.
//     Refusing it would not close a hole, it would delete history and break
//     catch-up against every peer that has not upgraded. So the row is kept and
//     the claim is qualified: the UI says nobody proved this, which is a true
//     statement about a relayed message and a far better one than attributing it
//     silently.
//
//   - A tombstone is neither. Its body is gone, so there is nothing left to
//     attribute; demanding a signature over an empty string would only mean
//     deletes stopped propagating.
//
// Reports whether to keep the row; the message is updated in place.
func messageAttestation(m *domain.Message) bool {
	switch {
	case m.Deleted || m.Expired:
		// The body was erased; whatever signature once covered it is meaningless
		// and must not be carried on as if it still said something.
		m.Sig = nil
		m.Unverified = false
		return true
	case len(m.Sig) > 0:
		if !verifyMessageSig(*m) {
			return false
		}
		m.Unverified = false
		return true
	default:
		m.Unverified = true
		return true
	}
}

// ---- packs: GIF and custom-emoji records ----
//
// These are a different problem from a message, and get a stricter answer.
//
// A message's signature is a claim of AUTHORSHIP: who wrote this. A pack record's
// is a claim of AUTHORITY: somebody holding Manage Guild put this in the guild's
// shared assets, where it renders in every member's picker. The gossip path has
// always checked that authority against the announcing member; the sync path
// could not, because catch-up is served by whoever answered rather than by the
// admin who created the record, and requiring the responder to be an admin would
// stop an ordinary member relaying a pack that is legitimately theirs to pass on.
//
// So the record carries the admin's own signature and the sync path checks it,
// which is the check that was missing rather than a softer stand-in for it.

// packSigV1 is the projection an admin signs for one pack record. GuildID is in
// it and does NOT travel: the verifier fills in the guild whose (MLS-encrypted,
// per-guild) lane the record arrived on, so a pack signed for one guild produces
// different bytes when replayed into another and fails. Cross-guild replay is
// closed by not asking the record where it belongs.
type packSigV1 struct {
	V       int    `json:"v"`
	Rec     string `json:"rec"` // "gif" | "emoji"
	GuildID string `json:"g"`
	ID      string `json:"id"`            // gif: blob id; emoji: name
	Name    string `json:"nm,omitempty"`  // gif display name
	Tags    string `json:"tg,omitempty"`  // gif tags, in the order signed
	Keys    string `json:"ky,omitempty"`  // gif attachment key material
	Subtype string `json:"st,omitempty"`  // gif subtype
	Width   int    `json:"w,omitempty"`   // gif width
	Height  int    `json:"h,omitempty"`   // gif height
	Image   string `json:"img,omitempty"` // emoji image, sha256 hex
	Author  []byte `json:"au"`            // account public key
}

// gifSigningBytes is the canonical encoding for one GIF-pack record in a guild.
func gifSigningBytes(guildID string, g GuildGif) []byte {
	b, _ := json.Marshal(packSigV1{
		V: recordSigVersion, Rec: "gif", GuildID: guildID,
		ID: g.ID, Name: g.Name, Tags: joinTags(g.Tags), Keys: g.Keys,
		Subtype: g.Subtype, Width: g.Width, Height: g.Height, Author: g.Author,
	})
	return b
}

// emojiSigningBytes is the canonical encoding for one custom-emoji record. The
// image is hashed rather than embedded: it is up to 256 KiB of base64 and the
// signature only needs to pin it, not carry it a second time.
func emojiSigningBytes(guildID string, e domain.CustomEmoji) []byte {
	sum := sha256.Sum256([]byte(e.Image))
	b, _ := json.Marshal(packSigV1{
		V: recordSigVersion, Rec: "emoji", GuildID: guildID,
		ID: e.Name, Image: hex.EncodeToString(sum[:]), Author: e.Author,
	})
	return b
}

// joinTags renders a GIF's tags into the one string the signature covers. Tags
// are stored space-joined already (store.GuildGifRow.Tags), so this is the same
// value on both sides of a round trip through the database.
func joinTags(tags []string) string {
	out := ""
	for i, t := range tags {
		if i > 0 {
			out += " "
		}
		out += t
	}
	return out
}

// verifyPackSig checks one pack record's signature against the key it carries.
func verifyPackSig(author, sig, signed []byte) bool {
	if len(author) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false
	}
	return identity.Verify(author, signed, sig)
}

// authorizedPackRecord is the sync-path gate for a pack record: a signature that
// verifies, over these exact bytes for THIS guild, by a member who holds Manage
// Guild in our own governance state.
//
// The permission is judged against OUR state, not the responder's word, which is
// the same posture commitAuthorized takes on a backfilled MLS commit. The two
// lanes now ask the identical question of the identical person, which is the
// point: a record that gossip would have refused can no longer be smuggled in
// through catch-up.
func (s *Service) authorizedPackRecord(guildID string, author, sig, signed []byte, perm map[string]bool) bool {
	if !verifyPackSig(author, sig, signed) {
		return false
	}
	fpr := identity.FingerprintOf(author)
	ok, cached := perm[fpr]
	if !cached {
		ok = s.memberHasPerm(guildID, fpr, PermManageGuild)
		perm[fpr] = ok
	}
	return ok
}

// ---- refusal logging ----
//
// A rejected record is a security event and has to leave a trace, but it is also
// something a hostile peer chooses how often to cause, so the log line is what is
// rate-limited rather than the check. One line per minute per category carries
// the fact that it is happening; a counter carries how much.

type refusalLog struct {
	n    atomic.Int64
	last atomic.Int64 // unix nanos of the last line written
}

var (
	refusedMessages refusalLog
	refusedPacks    refusalLog
)

const refusalLogInterval = time.Minute

// note records n refusals and writes at most one line per refusalLogInterval.
func (r *refusalLog) note(n int, what, from string) {
	if n <= 0 {
		return
	}
	total := r.n.Add(int64(n))
	now := time.Now().UnixNano()
	prev := r.last.Load()
	if prev != 0 && now-prev < int64(refusalLogInterval) {
		return
	}
	if !r.last.CompareAndSwap(prev, now) {
		return
	}
	log.Printf("concord/app: refused %d %s from %s whose author signature did not verify (%d total this session)",
		n, what, from, total)
}

// count exposes the running total, for tests that need to prove a refusal
// happened rather than infer it from an absent row.
func (r *refusalLog) count() int64 { return r.n.Load() }

// adoptUnsignedPackRecords re-signs this device's legacy pack records, once per
// launch, for every guild where this account actually holds Manage Guild.
//
// Without it the compatibility story has a sharp edge. A record created before
// signatures existed carries none, the catch-up lane refuses unsigned pack
// records on purpose, and so a guild that had built up fifty emoji would watch
// them stop reaching new members with nothing on screen saying why. The fix is
// not to weaken the lane, it is to notice who is running this code: an admin's
// signature over a pack record means "somebody entitled to put this in the guild
// put it there", and an admin re-signing a record they already hold is making
// exactly that statement, truthfully, about a record they could add from scratch
// in the same breath. It is not a claim about who added it first, and it does not
// pretend to be — the key on the record is this admin's own.
//
// Non-admins leave their copies alone, because they would be signing a claim
// they are not entitled to make; their unsigned copies stay usable locally and
// simply do not spread, which is the correct answer for them.
func (s *Service) adoptUnsignedPackRecords() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	self := s.id.PublicKey()
	for _, guildID := range ids {
		if !s.hasPerm(guildID, PermManageGuild) {
			continue
		}
		adopted := 0
		if rows, err := s.store.CustomEmoji(guildID); err == nil {
			for _, r := range rows {
				if len(r.Sig) > 0 {
					continue
				}
				e := domain.CustomEmoji{Name: r.Name, Image: r.Image, Author: self}
				e.Sig = s.id.Sign(emojiSigningBytes(guildID, e))
				if s.store.SaveCustomEmoji(emojiRow(guildID, e)) == nil {
					adopted++
				}
			}
		}
		if rows, err := s.store.GuildGifs(guildID); err == nil {
			for _, r := range rows {
				if len(r.Sig) > 0 {
					continue
				}
				g := gifFromRow(r)
				g.Author = self
				g.Sig = s.id.Sign(gifSigningBytes(guildID, g))
				if s.store.SaveGuildGif(gifRow(g)) == nil {
					adopted++
				}
			}
		}
		if adopted > 0 {
			log.Printf("concord/app: guild %s: signed %d pack records added before signatures existed, so they can reach new members again", guildID, adopted)
		}
	}
}
