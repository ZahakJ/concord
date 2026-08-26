package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/ZahakJ/concord/internal/identity"
	cnet "github.com/ZahakJ/concord/internal/net"
)

// Chronicles: owner-signed, content-addressed, lazily-fetched bulk history.
//
// A guild's live history is what its members exchanged while they were members.
// A chronicle is everything that came BEFORE that, or everything that has since
// aged out of it: a decade of conversation carried in from somewhere else, or a
// year that a retention policy swept out of the message table but that nobody
// actually wanted destroyed. Either way it has three properties the live path
// does not, and each one dictates a piece of the design.
//
// IT IS ENORMOUS. A real import is millions of messages. It cannot ride
// gossipsub, it cannot ride a sync payload, and it cannot be held in memory. So
// the bulk is cut into chunks of at most a megabyte of ciphertext, each one
// content-addressed and fetched only when somebody scrolls to it, over a
// dedicated stream protocol with its own frame cap (internal/net/chronicle.go).
// What travels on the ordinary paths is the MANIFEST: the table of contents,
// small enough to gossip and to fold into anti-entropy, from which a client
// knows the archive exists, what is in it, and which chunk holds any given day —
// without a byte of the archive itself.
//
// IT HAS NO AUTHORS PRESENT. Every other record here is signed by somebody who
// is in the guild: a story by its author, a governance op by an admin. The
// people in a chronicle mostly are not members and mostly never will be, and the
// messages predate the group's existence, so there is no key that could attest
// to any of it. The only honest claim anyone can make is "the guild's owner put
// this here", and that is exactly what the manifest's signature says. Every
// authority question therefore roots at one place — the EFFECTIVE owner, the
// head of the transfer chain, never the founding key — and the answer is checked
// on ingest and the signature re-checked on load. A manifest that fails either
// is dropped whole. There is no partial application: half an archive attributed
// to the owner is worse than none.
//
// IT IS OPTIONAL. Nobody is waiting on a page of 2014. That makes it the first
// thing in Concord with a legitimate claim on the metered-connection flag: a
// chunk is a megabyte nobody asked for by name, and refusing to spend a data
// plan on one is a service, where refusing to deliver a message would be a bug.
// See chronicleChunk and the note on Host.Metered.
//
// THE RAW-BYTES RULE. A manifest is stored, served and verified as the exact
// bytes that were signed — json.RawMessage end to end, never a struct that gets
// re-marshalled on the way past. The governance log shows what the alternative
// costs: govOpsFor re-encodes every op from its decoded form before serving it,
// which works only because every field is known to every build, and would break
// the day one is not. A chronicle is the record most likely to grow a field
// (import provenance, per-channel keys, a second signer) and the least likely to
// be re-minted afterwards, because the machine that did the import may be gone.
// So the decoded struct is a VIEW of the manifest, and the bytes are the record.

const (
	// chronicleVersion is the manifest format. A manifest claiming anything else
	// is refused rather than best-guessed: the signature is over the whole
	// record, so a build that cannot read every field cannot honestly say it
	// verified one.
	chronicleVersion = 1

	// maxChronicleManifestBytes caps the table of contents. It has to fit
	// alongside everything else in a 700 KiB sync payload and inside a gossip
	// frame, and it is the one part of an archive that every member stores
	// whether or not they ever read a page. 384 KiB is room for a few thousand
	// chunk entries and a few dozen author portraits; an import too big for one
	// manifest is several chronicles, which the format already allows because
	// nothing anywhere assumes a guild has only one.
	maxChronicleManifestBytes = 384 << 10

	maxChronicleChunks   = 4096
	maxChronicleChannels = 512
	maxChronicleAuthors  = 8192

	// maxChronicleAvatarBytes caps one author portrait carried inline. Inline
	// rather than a blob reference because a portrait is 8 KiB and a fetch is a
	// round trip: an archive whose every name needed a network call to show a
	// face would spend more traffic on faces than on words.
	maxChronicleAvatarBytes = 8 << 10

	maxChronicleSourceLen = 200
	maxChronicleDescLen   = 2000
	maxChronicleNameLen   = 200

	// chronicleChunkMessages and chronicleChunkPlainTarget are the SOFT split
	// points; the ciphertext cap is the hard one, enforced by re-splitting after
	// sealing (see sealChronicleChunks). Two soft limits rather than one because
	// the two failure modes are different: ten thousand one-word messages are
	// cheap to compress and expensive to decode, and one message carrying a
	// paste of a log file is the reverse.
	chronicleChunkMessages    = 1000
	chronicleChunkPlainTarget = 512 << 10

	// chronicleFetchTimeout is the patience for one peer's answer, matching the
	// attachment fetch: a chunk is a comparable size and a member who cannot
	// serve a megabyte in fifteen seconds is a member worth skipping.
	chronicleFetchTimeout = 15 * time.Second

	// maxSyncChroniclesPerGuild caps how many manifests one sync response
	// carries. Newest first — an older archive is not more urgent for being
	// older, and the requester's digest re-asks for whatever was left out.
	maxSyncChroniclesPerGuild = 4
)

// ErrChronicleMetered is returned when a page of the archive is not held
// locally and the connection is billed by the byte. Not a failure — a refusal,
// and the caller can retake the decision by passing the override. The frontend
// renders it as an offer to wait for Wi-Fi.
var ErrChronicleMetered = errors.New("app: archive pages are fetched on Wi-Fi only")

// errChronicleChunkMissing means no reachable member could serve a chunk. Kept
// distinct from a decode failure: one is "ask again later", the other is "this
// archive is damaged".
var errChronicleChunkMissing = errors.New("app: no reachable member holds that page of the archive")

// ---- the manifest ----

// chronicleAuthor is one name that appears in the archive. Messages reference
// authors by INDEX rather than by name, which is most of why a chunk compresses
// as well as it does — a busy channel is one integer per message where it would
// otherwise be a repeated display name.
type chronicleAuthor struct {
	Name string `json:"name"`
	// Avatar is a small raster data URI, or empty. Validated on every path
	// exactly as a profile avatar is: it renders into a CSS context on every
	// member's machine, and an archive is precisely the record whose contents
	// nobody in the guild ever wrote or reviewed.
	Avatar string `json:"avatar,omitempty"`
}

// chronicleChannel is one channel as it existed in the SOURCE. The ids are the
// source's, not this guild's — mapping them onto real channels is the importer's
// job, and it happens at import time on the owner's machine. v1 stores the
// mapping so that a reader who was not there can still be told which room a
// conversation happened in.
type chronicleChannel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type,omitempty"`
	Topic string `json:"topic,omitempty"`
	// Mapped is the channel in THIS guild the importer put the history in front
	// of, or "" when the archive was attached without one. It is what lets a
	// reader who was never in the source community be told which room a
	// conversation belongs above, and it is why the mapping is decided ONCE, on
	// the importing machine, rather than re-guessed from names by every member
	// who opens the archive — names change, and two of them re-guessing
	// differently is two members reading the same history under different
	// headings.
	Mapped string `json:"mapped,omitempty"`
}

// chronicleChunkRef is one entry in the index: where a slice of one channel's
// history lives, and the key that opens it. The key rides HERE, in the manifest,
// and the manifest only ever travels MLS-encrypted — which is what lets the
// chunk itself be served to anybody who asks. It is the attachment posture
// exactly: the capability is the key, not the transport.
type chronicleChunkRef struct {
	ID        string `json:"id"` // hex sha256 of the ciphertext
	Channel   string `json:"ch"` // source channel id
	FirstNano int64  `json:"from"`
	LastNano  int64  `json:"to"`
	Count     int    `json:"n"`
	Size      int    `json:"size"` // ciphertext bytes
	Keys      []byte `json:"k"`    // secretbox key(32) || nonce(24)
}

// chronicleManifest is the signed table of contents. The decoded form; the
// record is the bytes it was decoded from.
type chronicleManifest struct {
	Version int    `json:"v"`
	GuildID string `json:"guildId"`
	// Source and Desc are free text the importer supplies — "the old forum,
	// 2009-2019" — shown wherever the archive is offered. Length-capped because
	// they ride the manifest into every member's database.
	Source  string `json:"source"`
	Desc    string `json:"desc,omitempty"`
	Created int64  `json:"created"` // unix seconds
	// Signer is the account fingerprint of the owner who attached the archive,
	// and SignerKey their account public key. The key travels rather than being
	// looked up in the roster (the story idiom) for one reason: an owner can
	// hand the crown on or leave, and an archive they legitimately attached must
	// not become unreadable when they do. Membership is what the INGEST gate
	// checks, once; the signature is what every later load checks, forever.
	Signer    string              `json:"signer"`
	SignerKey []byte              `json:"signerKey"`
	Channels  []chronicleChannel  `json:"channels"`
	Authors   []chronicleAuthor   `json:"authors"`
	Chunks    []chronicleChunkRef `json:"chunks"`
	Messages  int64               `json:"messages"`
	Bytes     int64               `json:"bytes"`
	// Attachments/AttachBytes are the archive's media totals. The blobs
	// themselves are ordinary content-addressed attachments referenced by
	// ordinary tokens, so they need no machinery here — only a headline, so a
	// member can be told what fetching the whole thing would cost.
	Attachments int    `json:"attachments,omitempty"`
	AttachBytes int64  `json:"attachBytes,omitempty"`
	Sig         []byte `json:"sig"`
}

// signingBytes is the canonical encoding the owner signs: the manifest with Sig
// zeroed and every author portrait replaced by its sha256 — the storyRecord
// idiom, for the storyRecord reason. Hashing the bulky parts keeps the signed
// form small enough to hash cheaply on every load, while still binding the exact
// bytes: a portrait swapped for another changes the hash and breaks the
// signature just as surely as editing it in place would.
func (m chronicleManifest) signingBytes() []byte {
	c := m
	c.Sig = nil
	if len(m.Authors) > 0 {
		c.Authors = make([]chronicleAuthor, len(m.Authors))
		for i, a := range m.Authors {
			sum := sha256.Sum256([]byte(a.Avatar))
			c.Authors[i] = chronicleAuthor{Name: a.Name, Avatar: hex.EncodeToString(sum[:])}
		}
	}
	b, _ := json.Marshal(c)
	return b
}

// verifySig checks the manifest's own signature and that the key it carries is
// the key it claims. A signature by SOME valid key proves nothing; the binding
// to Signer is what makes the fingerprint comparable to the guild's owner.
func (m chronicleManifest) verifySig() bool {
	if len(m.SignerKey) != ed25519.PublicKeySize || len(m.Sig) != ed25519.SignatureSize {
		return false
	}
	if identity.FingerprintOf(m.SignerKey) != m.Signer {
		return false
	}
	return identity.Verify(m.SignerKey, m.signingBytes(), m.Sig)
}

// chronicleIDOf content-addresses a manifest by the EXACT bytes that arrived,
// not by a re-encoding of the decoded struct. Two consequences worth naming:
// the id can be computed without parsing anything, and a build that adds a field
// produces a genuinely different archive rather than silently colliding with the
// old one under the same id.
func chronicleIDOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// validChronicleManifest is the shape gate every manifest passes, whoever built
// it — the local builder runs it before signing, and both receive paths run it
// before storing, one implementation so the two cannot drift.
func validChronicleManifest(m chronicleManifest) bool {
	if m.Version != chronicleVersion || m.GuildID == "" || m.Signer == "" {
		return false
	}
	if m.Source == "" || len(m.Source) > maxChronicleSourceLen || len(m.Desc) > maxChronicleDescLen {
		return false
	}
	if m.Created <= 0 {
		return false
	}
	if len(m.Channels) == 0 || len(m.Channels) > maxChronicleChannels {
		return false
	}
	if len(m.Authors) > maxChronicleAuthors {
		return false
	}
	if len(m.Chunks) == 0 || len(m.Chunks) > maxChronicleChunks {
		return false
	}
	seenChannel := make(map[string]bool, len(m.Channels))
	for _, c := range m.Channels {
		if c.ID == "" || len(c.ID) > 128 || seenChannel[c.ID] {
			return false
		}
		if len(c.Name) > maxChronicleNameLen || len(c.Topic) > maxChronicleDescLen || len(c.Type) > 32 {
			return false
		}
		if len(c.Mapped) > 128 {
			return false
		}
		seenChannel[c.ID] = true
	}
	for _, a := range m.Authors {
		if len(a.Name) > maxChronicleNameLen {
			return false
		}
		// An empty portrait is the ordinary case and must stay free; a present
		// one is held to the same whole-string raster gate a guild banner is.
		if a.Avatar != "" && !validImageDataURI(a.Avatar, maxChronicleAvatarBytes) {
			return false
		}
	}
	seenChunk := make(map[string]bool, len(m.Chunks))
	var totalCount int64
	var totalBytes int64
	for _, c := range m.Chunks {
		if !blobIDRe.MatchString(c.ID) || seenChunk[c.ID] {
			return false
		}
		if !seenChannel[c.Channel] {
			return false // a chunk of a channel the index does not list
		}
		if len(c.Keys) != attachKeysLen {
			return false
		}
		if c.Count <= 0 || c.Size <= 0 || c.Size > cnet.MaxChronicleChunk {
			return false
		}
		if c.FirstNano <= 0 || c.LastNano < c.FirstNano {
			return false
		}
		seenChunk[c.ID] = true
		totalCount += int64(c.Count)
		totalBytes += int64(c.Size)
	}
	// The headline totals are what a member is shown before deciding to fetch
	// anything, so they have to be the index's own arithmetic rather than a
	// separate claim the index does not support.
	if m.Messages != totalCount || m.Bytes != totalBytes {
		return false
	}
	// Author indices are NOT checked here: the manifest does not carry the
	// messages that hold them, so the check belongs where a chunk is opened.
	return true
}

// decodeChronicleManifest turns stored or received bytes back into the view,
// re-checking size, shape and signature. Called on EVERY load, not only on
// ingest: a database is a file on a disk somebody else may have written to, and
// the whole value of a signed archive is that the check is cheap enough to
// repeat.
func decodeChronicleManifest(raw []byte) (chronicleManifest, error) {
	var m chronicleManifest
	if len(raw) == 0 || len(raw) > maxChronicleManifestBytes {
		return m, fmt.Errorf("app: chronicle manifest is %d bytes (max %d)", len(raw), maxChronicleManifestBytes)
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("app: chronicle manifest is not readable: %w", err)
	}
	if !validChronicleManifest(m) {
		return chronicleManifest{}, fmt.Errorf("app: chronicle manifest failed validation")
	}
	if !m.verifySig() {
		return chronicleManifest{}, fmt.Errorf("app: chronicle manifest signature does not verify")
	}
	return m, nil
}

// ---- chunks ----

// chronicleMessage is one archived message. Short JSON names on purpose: this
// shape is repeated a million times inside a chunk, and the difference between
// "content" and "c" is megabytes across a real import even after gzip.
type chronicleMessage struct {
	ID      string `json:"i,omitempty"` // source id, so ReplyTo can resolve
	Author  int    `json:"a"`           // index into the manifest's author table
	Nano    int64  `json:"t"`           // unix nanoseconds
	Content string `json:"c,omitempty"`
	ReplyTo string `json:"r,omitempty"`
	// Reactions is emoji -> count. Counts, not rosters: the people who reacted
	// are mostly not members and their names would double the archive.
	Reactions map[string]int `json:"x,omitempty"`
	// Attach carries concord://attach/... tokens — the EXISTING format, so an
	// archived picture is fetched by the existing attachment protocol, cached in
	// the existing blob table, and rendered by the existing frontend code. The
	// chronicle adds no media machinery at all.
	Attach []string `json:"m,omitempty"`
}

// sealChronicleChunk compresses a run of messages, seals it under a fresh key,
// and returns the index entry plus the ciphertext. gzip from the standard
// library rather than a better modern codec: the project ships as one static
// binary with no C toolchain and no optional dependencies, and the difference
// between gzip and the state of the art on chat text is a fraction of what
// splitting into fetch-on-demand chunks already bought.
func sealChronicleChunk(channelID string, msgs []chronicleMessage) (chronicleChunkRef, []byte, error) {
	var plain bytes.Buffer
	zw := gzip.NewWriter(&plain)
	enc := json.NewEncoder(zw) // one JSON object per line
	for _, m := range msgs {
		if err := enc.Encode(m); err != nil {
			return chronicleChunkRef{}, nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return chronicleChunkRef{}, nil, err
	}

	var key [32]byte
	var nonce [24]byte
	if _, err := rand.Read(key[:]); err != nil {
		return chronicleChunkRef{}, nil, err
	}
	if _, err := rand.Read(nonce[:]); err != nil {
		return chronicleChunkRef{}, nil, err
	}
	ct := secretbox.Seal(nil, plain.Bytes(), &nonce, &key)
	sum := sha256.Sum256(ct)
	ref := chronicleChunkRef{
		ID:        hex.EncodeToString(sum[:]),
		Channel:   channelID,
		FirstNano: msgs[0].Nano,
		LastNano:  msgs[len(msgs)-1].Nano,
		Count:     len(msgs),
		Size:      len(ct),
		Keys:      append(key[:], nonce[:]...),
	}
	return ref, ct, nil
}

// openChronicleChunk reverses it. The ciphertext has already been proved
// authentic by its content address, so a failure here is a manifest whose key
// does not match its own chunk — corrupt, not hostile, and worth saying so
// rather than returning an empty page.
func openChronicleChunk(ct []byte, keys []byte) ([]chronicleMessage, error) {
	if len(keys) != attachKeysLen {
		return nil, fmt.Errorf("app: bad chronicle chunk key")
	}
	var key [32]byte
	var nonce [24]byte
	copy(key[:], keys[:32])
	copy(nonce[:], keys[32:])
	plain, ok := secretbox.Open(nil, ct, &nonce, &key)
	if !ok {
		return nil, fmt.Errorf("app: chronicle chunk key does not open it")
	}
	zr, err := gzip.NewReader(bytes.NewReader(plain))
	if err != nil {
		return nil, fmt.Errorf("app: chronicle chunk is not readable: %w", err)
	}
	defer zr.Close()
	var out []chronicleMessage
	dec := json.NewDecoder(zr)
	for {
		var m chronicleMessage
		if err := dec.Decode(&m); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("app: chronicle chunk is not readable: %w", err)
		}
		out = append(out, m)
	}
	return out, nil
}

// sealChronicleChunks splits one channel's ordered history into sealed chunks,
// each inside the transport's ciphertext cap.
//
// The split is soft-then-hard: cut at a message count or a plaintext size that
// USUALLY lands well inside the cap, seal, and if the result is still too big,
// halve the run and try again. Sealing twice for a pathological batch is cheap
// and happens once, at import; guessing the compression ratio in advance is not
// possible and getting it wrong produces a chunk that no peer will ever serve.
func sealChronicleChunks(channelID string, msgs []chronicleMessage) ([]chronicleChunkRef, map[string][]byte, error) {
	refs := []chronicleChunkRef{}
	out := map[string][]byte{}

	var flush func(run []chronicleMessage) error
	flush = func(run []chronicleMessage) error {
		if len(run) == 0 {
			return nil
		}
		ref, ct, err := sealChronicleChunk(channelID, run)
		if err != nil {
			return err
		}
		if len(ct) > cnet.MaxChronicleChunk {
			if len(run) == 1 {
				return fmt.Errorf("app: a single archived message seals to %d bytes, over the %d-byte page limit",
					len(ct), cnet.MaxChronicleChunk)
			}
			half := len(run) / 2
			if err := flush(run[:half]); err != nil {
				return err
			}
			return flush(run[half:])
		}
		refs = append(refs, ref)
		out[ref.ID] = ct
		return nil
	}

	run := make([]chronicleMessage, 0, chronicleChunkMessages)
	raw := 0
	for _, m := range msgs {
		run = append(run, m)
		raw += len(m.Content) + len(m.ID) + len(m.ReplyTo) + 48
		for _, a := range m.Attach {
			raw += len(a) + 4
		}
		if len(run) >= chronicleChunkMessages || raw >= chronicleChunkPlainTarget {
			if err := flush(run); err != nil {
				return nil, nil, err
			}
			run, raw = make([]chronicleMessage, 0, chronicleChunkMessages), 0
		}
	}
	if err := flush(run); err != nil {
		return nil, nil, err
	}
	return refs, out, nil
}

// buildChronicle assembles and signs a complete archive from already-parsed
// material. Deliberately separate from any file format: the importer's job is to
// turn somebody's export into these structures, and this function's job is to
// turn them into something the guild can carry. Splitting them there means the
// signing, sealing and size rules have exactly one implementation however many
// export formats eventually feed it.
//
// byChannel is per-source-channel history; each slice is sorted here rather than
// trusted, because a chunk's time range is an index the reader relies on.
func (s *Service) buildChronicle(guildID, source, desc string, channels []chronicleChannel,
	authors []chronicleAuthor, byChannel map[string][]chronicleMessage,
	attachments int, attachBytes int64) (raw []byte, chunks map[string][]byte, err error) {

	if strings.TrimSpace(source) == "" {
		return nil, nil, fmt.Errorf("app: an archive needs a source label")
	}
	if len(channels) == 0 {
		return nil, nil, fmt.Errorf("app: an archive needs at least one channel")
	}

	m := chronicleManifest{
		Version:     chronicleVersion,
		GuildID:     guildID,
		Source:      strings.TrimSpace(source),
		Desc:        desc,
		Created:     time.Now().Unix(),
		Signer:      s.id.Fingerprint(),
		SignerKey:   s.id.PublicKey(),
		Channels:    channels,
		Authors:     authors,
		Attachments: attachments,
		AttachBytes: attachBytes,
	}
	chunks = map[string][]byte{}
	// Channels in the order the caller listed them, so the index is stable and
	// two runs over the same input produce the same reading order.
	for _, ch := range channels {
		msgs := append([]chronicleMessage(nil), byChannel[ch.ID]...)
		if len(msgs) == 0 {
			continue
		}
		sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Nano < msgs[j].Nano })
		refs, cts, err := sealChronicleChunks(ch.ID, msgs)
		if err != nil {
			return nil, nil, err
		}
		for id, ct := range cts {
			chunks[id] = ct
		}
		m.Chunks = append(m.Chunks, refs...)
	}
	if len(m.Chunks) == 0 {
		return nil, nil, fmt.Errorf("app: the archive has no messages")
	}
	for _, c := range m.Chunks {
		m.Messages += int64(c.Count)
		m.Bytes += int64(c.Size)
	}

	// Portraits are the only elastic part of the manifest, so they are what
	// gives when it will not fit. Dropping them costs faces; dropping anything
	// else costs history.
	m.Sig = nil
	for {
		if !validChronicleManifest(m) {
			return nil, nil, fmt.Errorf("app: the archive did not pass its own validation")
		}
		m.Sig = s.id.Sign(m.signingBytes())
		raw, err = json.Marshal(m)
		if err != nil {
			return nil, nil, err
		}
		if len(raw) <= maxChronicleManifestBytes {
			return raw, chunks, nil
		}
		dropped := false
		for i := len(m.Authors) - 1; i >= 0; i-- {
			if m.Authors[i].Avatar != "" {
				m.Authors[i].Avatar = ""
				dropped = true
				break
			}
		}
		if !dropped {
			return nil, nil, fmt.Errorf(
				"app: the archive index is %d bytes, over the %d-byte limit; split it into several archives",
				len(raw), maxChronicleManifestBytes)
		}
		m.Sig = nil
	}
}

// ---- ingest ----

// ingestChronicle is the single funnel every manifest from every source passes
// through — our own AttachChronicle, a gossiped announce, a history-sync
// payload. Fail closed at every step, and never partially: a manifest that is
// not the current owner's is not "mostly fine", it is somebody else's claim
// about this guild's past.
//
// Reports whether a NEW manifest was stored, so callers do not re-emit events
// for the same archive arriving from the fourth member in a row.
func (s *Service) ingestChronicle(guildID string, raw []byte) bool {
	m, err := decodeChronicleManifest(raw)
	if err != nil {
		return false
	}
	// The guild binding is under the signature, so a mismatch is dropped rather
	// than rewritten — rewriting would break the signature and store a record no
	// peer could ever re-verify (the storyRecord rule).
	if m.GuildID != guildID {
		return false
	}
	// THE OWNER GATE, and the only moment it is asked. The effective owner, not
	// the founding key: a dethroned founder must not be able to attach a decade
	// of history to a guild they no longer run. A manifest arriving before the
	// transfer op that would authorize it is simply refused; we never claim to
	// hold it, so the requester's digest asks for it again on the next beat, by
	// which time the governance log has caught up.
	if m.Signer == "" || m.Signer != s.effectiveOwner(guildID) {
		return false
	}
	inserted, err := s.store.SaveChronicleManifest(chronicleIDOf(raw), guildID, raw)
	return err == nil && inserted
}

// chroniclesForSync returns the raw manifests a responder serves for one guild,
// newest first and capped.
func (s *Service) chroniclesForSync(guildID string) []json.RawMessage {
	rows, err := s.store.ChronicleManifests(guildID)
	if err != nil || len(rows) == 0 {
		return nil
	}
	if len(rows) > maxSyncChroniclesPerGuild {
		rows = rows[:maxSyncChroniclesPerGuild]
	}
	out := make([]json.RawMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, json.RawMessage(r.Raw))
	}
	return out
}

// applySyncedChronicles folds manifests from a history-sync payload into local
// state. The responder attests NOTHING here — like stories, and unlike message
// rows, every record proves itself or is dropped.
func (s *Service) applySyncedChronicles(guildID string, raws []json.RawMessage) {
	changed := false
	for _, raw := range raws {
		if s.ingestChronicle(guildID, raw) {
			changed = true
		}
	}
	if changed {
		s.emitGuildUpdate()
	}
}

// applyChronicleMeta is the gossip arm: the announce that makes an archive
// visible to members who are online now, instead of on the next anti-entropy
// beat. The MLS-authenticated sender is irrelevant and deliberately unused — the
// manifest carries the owner's own signature, so any member may relay it, which
// is exactly the property that keeps an archive reachable after the machine that
// imported it goes away.
func (s *Service) applyChronicleMeta(guildID string, m guildMeta) {
	if len(m.Chronicle) == 0 {
		return
	}
	if s.ingestChronicle(guildID, m.Chronicle) {
		s.emitGuildUpdate()
	}
}

// ---- fetching a page ----

type chronicleRequest struct {
	ChunkID string `json:"chunkId"`
}

// handleChronicleRequest serves a chunk from the local store; an empty response
// means "don't have it". Ungated, for the reason set out in
// internal/net/chronicle.go: the ciphertext is worthless without the manifest's
// key, and the manifest is members-only.
func (s *Service) handleChronicleRequest(_ context.Context, _ peer.ID, request []byte) ([]byte, error) {
	var req chronicleRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return []byte{}, nil
	}
	if !blobIDRe.MatchString(strings.TrimSpace(req.ChunkID)) {
		return []byte{}, nil
	}
	ct, ok, err := s.store.ChronicleChunk(req.ChunkID)
	if err != nil || !ok || len(ct) > cnet.MaxChronicleChunk {
		return []byte{}, nil
	}
	return ct, nil
}

// chronicleChunk resolves one chunk's ciphertext: local store, then connected
// guild members, one at a time. Whatever it fetches is hash-verified and saved,
// so every member who reads a page becomes a source for it — the property that
// keeps an archive alive after the importing device is gone.
//
// allowMetered is the caller's override of the metered refusal. It is a
// parameter rather than a setting because the decision belongs to the person
// looking at the screen: the default protects a data plan, and someone who has
// deliberately asked for a page from 2011 while on a train is entitled to it.
func (s *Service) chronicleChunk(guildID, chunkID string, allowMetered bool) ([]byte, error) {
	if !blobIDRe.MatchString(chunkID) {
		return nil, fmt.Errorf("app: bad archive page id")
	}
	if ct, ok, err := s.store.ChronicleChunk(chunkID); err == nil && ok {
		return ct, nil
	}
	if !allowMetered && s.host.Metered() {
		return nil, ErrChronicleMetered
	}
	v, err, _ := s.chronicleFlight.Do(chunkID, func() (any, error) {
		return s.fetchChronicleChunk(guildID, chunkID)
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// fetchChronicleChunk walks the guild's connected members until one serves the
// chunk. Sequential, like the attachment fetch and for the same reason: parallel
// megabyte downloads of the same bytes waste bandwidth for no latency win.
func (s *Service) fetchChronicleChunk(guildID, chunkID string) ([]byte, error) {
	// Re-check under the flight: while we waited our turn, the peer we were
	// queued behind may have already saved it.
	if ct, ok, err := s.store.ChronicleChunk(chunkID); err == nil && ok {
		return ct, nil
	}
	req, _ := json.Marshal(chronicleRequest{ChunkID: chunkID})
	for _, p := range s.host.Peers() {
		if !s.guildHasMember(guildID, s.presence(p).Fingerprint) {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, chronicleFetchTimeout)
		ct, err := s.host.RequestChronicleChunk(ctx, p, req)
		cancel()
		if err != nil || len(ct) == 0 {
			continue
		}
		sum := sha256.Sum256(ct)
		if hex.EncodeToString(sum[:]) != chunkID {
			continue // wrong or corrupted bytes; try the next member
		}
		// Not pinned: a page we fetched to READ is a cache entry. Only the
		// device that imported the archive pins, because only it has a copy
		// nobody else can replace.
		_ = s.store.SaveChronicleChunk(chunkID, ct, false)
		return ct, nil
	}
	return nil, errChronicleChunkMissing
}

// ---- the service API ----

// ChronicleChannelView is one channel of an archive as the UI lists it.
type ChronicleChannelView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	// Mapped is the real channel in this guild whose scrollback this archived
	// channel sits above, or "" when the import mapped it nowhere.
	Mapped       string `json:"mapped,omitempty"`
	Topic        string `json:"topic,omitempty"`
	Messages     int64  `json:"messages"`
	FirstNano    int64  `json:"firstNano"`
	LastNano     int64  `json:"lastNano"`
	Chunks       int    `json:"chunks"`
	ChunksCached int    `json:"chunksCached"`
}

// ChronicleView is what a member is told about an archive before deciding to
// read any of it: what it is, how big it is, and how much of it this device
// already holds.
type ChronicleView struct {
	ID              string                 `json:"id"`
	GuildID         string                 `json:"guildId"`
	Source          string                 `json:"source"`
	Description     string                 `json:"description,omitempty"`
	Created         int64                  `json:"created"`
	Signer          string                 `json:"signer"`
	Messages        int64                  `json:"messages"`
	Bytes           int64                  `json:"bytes"`
	Attachments     int                    `json:"attachments,omitempty"`
	AttachmentBytes int64                  `json:"attachmentBytes,omitempty"`
	Channels        []ChronicleChannelView `json:"channels"`
	Chunks          int                    `json:"chunks"`
	ChunksCached    int                    `json:"chunksCached"`
	CachedBytes     int64                  `json:"cachedBytes"`
	Pinned          bool                   `json:"pinned"`
}

// ChronicleMessageView is one archived message, with its author resolved from
// the manifest's table so the caller never sees an index.
type ChronicleMessageView struct {
	ID        string         `json:"id,omitempty"`
	Author    string         `json:"author"`
	Avatar    string         `json:"avatar,omitempty"`
	Nano      int64          `json:"nano"`
	Content   string         `json:"content"`
	ReplyTo   string         `json:"replyTo,omitempty"`
	Reactions map[string]int `json:"reactions,omitempty"`
	Attach    []string       `json:"attach,omitempty"`
}

// AttachChronicle records a signed archive against a guild and makes it
// available to every member. Owner-only, checked twice over: the caller must be
// the effective owner, and the manifest must be signed by them — the second
// check is not redundant, because it is the one every OTHER member will run.
//
// Everything is verified before anything is stored. A chunk whose bytes do not
// match its content address, or an index entry with no chunk behind it, fails
// the whole call: an archive that is 90% present is an archive whose reader
// discovers the missing tenth by scrolling into a hole.
func (s *Service) AttachChronicle(guildID string, manifestRaw []byte, chunks map[string][]byte) error {
	self := s.id.Fingerprint()
	if !s.IsGuildOwner(guildID, self) {
		return fmt.Errorf("app: only the guild's owner can attach an archive")
	}
	m, err := decodeChronicleManifest(manifestRaw)
	if err != nil {
		return err
	}
	if m.GuildID != guildID {
		return fmt.Errorf("app: that archive was signed for a different guild")
	}
	if m.Signer != self {
		return fmt.Errorf("app: that archive was signed by somebody else")
	}
	for _, ref := range m.Chunks {
		ct, ok := chunks[ref.ID]
		if !ok {
			return fmt.Errorf("app: the archive index names a page that is not in the bundle")
		}
		if len(ct) != ref.Size {
			return fmt.Errorf("app: an archive page is %d bytes, the index says %d", len(ct), ref.Size)
		}
		sum := sha256.Sum256(ct)
		if hex.EncodeToString(sum[:]) != ref.ID {
			return fmt.Errorf("app: an archive page does not match its own content address")
		}
	}

	id := chronicleIDOf(manifestRaw)
	// Chunks first: a manifest visible to peers whose pages are not yet on disk
	// would advertise an archive this device cannot serve.
	for _, ref := range m.Chunks {
		// Pinned. This device performed the import, so its copy is the original
		// and there is nobody to fetch it back from.
		if err := s.store.SaveChronicleChunk(ref.ID, chunks[ref.ID], true); err != nil {
			return err
		}
	}
	if _, err := s.store.SaveChronicleManifest(id, guildID, manifestRaw); err != nil {
		return err
	}
	s.emitGuildUpdate()

	// Tell the guild now, rather than leaving it to the sixty-second
	// anti-entropy beat. The manifest is the whole announcement — it is small
	// by construction and self-verifying, so a member who hears it can start
	// reading immediately, and one who is offline gets it from the digest sync
	// like everything else.
	s.mu.RLock()
	g, tracked := s.guilds[guildID]
	var groupID []byte
	if tracked {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if tracked {
		s.publishMeta(groupID, guildMeta{Type: "chronicle", Chronicle: manifestRaw})
	}
	return nil
}

// guildChronicle loads the guild's current archive: the newest manifest that
// still verifies. Newest rather than all of them because v1's reading surface
// is one archive per guild — the format allows several and the store keeps
// several, so the day the UI wants to show a shelf of them, nothing has to be
// migrated.
func (s *Service) guildChronicle(guildID string) (chronicleManifest, string, bool) {
	rows, err := s.store.ChronicleManifests(guildID)
	if err != nil {
		return chronicleManifest{}, "", false
	}
	for _, r := range rows {
		m, err := decodeChronicleManifest(r.Raw)
		if err != nil {
			// Stored bytes that no longer verify: the signature is checked on
			// every load precisely so this is a skipped row rather than a
			// forgery served onward.
			continue
		}
		if m.GuildID != guildID {
			continue
		}
		return m, r.ChronicleID, true
	}
	return chronicleManifest{}, "", false
}

// ChronicleInfo describes a guild's archive, or the zero view when it has none.
func (s *Service) ChronicleInfo(guildID string) (ChronicleView, error) {
	m, id, ok := s.guildChronicle(guildID)
	if !ok {
		return ChronicleView{}, nil
	}
	ids := make([]string, 0, len(m.Chunks))
	for _, c := range m.Chunks {
		ids = append(ids, c.ID)
	}
	held, cached, pinned, err := s.store.ChronicleChunkPresence(ids)
	if err != nil {
		return ChronicleView{}, err
	}

	// Per-channel headline numbers come from the index, so they are available
	// without fetching a single page.
	byChannel := map[string]*ChronicleChannelView{}
	view := ChronicleView{
		ID: id, GuildID: guildID, Source: m.Source, Description: m.Desc,
		Created: m.Created, Signer: m.Signer,
		Messages: m.Messages, Bytes: m.Bytes,
		Attachments: m.Attachments, AttachmentBytes: m.AttachBytes,
		Chunks: len(m.Chunks), ChunksCached: len(held), CachedBytes: cached,
		Pinned: pinned > 0 && pinned == len(held),
	}
	for _, c := range m.Channels {
		cv := &ChronicleChannelView{ID: c.ID, Name: c.Name, Type: c.Type, Mapped: c.Mapped, Topic: c.Topic}
		byChannel[c.ID] = cv
	}
	for _, c := range m.Chunks {
		cv, ok := byChannel[c.Channel]
		if !ok {
			continue
		}
		cv.Messages += int64(c.Count)
		cv.Chunks++
		if held[c.ID] {
			cv.ChunksCached++
		}
		if cv.FirstNano == 0 || c.FirstNano < cv.FirstNano {
			cv.FirstNano = c.FirstNano
		}
		if c.LastNano > cv.LastNano {
			cv.LastNano = c.LastNano
		}
	}
	for _, c := range m.Channels {
		view.Channels = append(view.Channels, *byChannel[c.ID])
	}
	return view, nil
}

// ChronicleMessages reads a page of the archive, newest-backwards: the messages
// in one channel strictly older than beforeNano (0 meaning "the newest"), up to
// limit, returned oldest-first so the caller can render them in reading order.
//
// Pages come from cached chunks where possible and are fetched where not, which
// is where allowMetered comes in — see chronicleChunk. A refusal is
// ErrChronicleMetered and nothing else has happened; the caller can offer the
// choice and call again.
func (s *Service) ChronicleMessages(guildID, channelID string, beforeNano int64, limit int, allowMetered bool) ([]ChronicleMessageView, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	m, _, ok := s.guildChronicle(guildID)
	if !ok {
		return nil, fmt.Errorf("app: this guild has no archive")
	}
	if beforeNano <= 0 {
		beforeNano = int64(^uint64(0) >> 1) // max int64: start at the newest
	}

	// The chunks of this channel that could hold anything older than the
	// cursor, newest first — the index alone answers that, so a page deep in
	// history costs one fetch rather than a walk through everything after it.
	var refs []chronicleChunkRef
	for _, c := range m.Chunks {
		if c.Channel == channelID && c.FirstNano < beforeNano {
			refs = append(refs, c)
		}
	}
	if len(refs) == 0 {
		return []ChronicleMessageView{}, nil
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].LastNano > refs[j].LastNano })

	var picked []chronicleMessage
	opened := 0
	var lastErr error
	for _, ref := range refs {
		ct, err := s.chronicleChunk(guildID, ref.ID, allowMetered)
		if err != nil {
			// A metered refusal ends the call: it is a decision, not a failure,
			// and the caller has to be able to offer it as one.
			if errors.Is(err, ErrChronicleMetered) {
				return nil, err
			}
			// Anything else is one page nobody could serve, which must not hide
			// the pages behind it — but must not be silently reported as an
			// empty stretch of history either. Remembered, and raised only if
			// NOTHING could be read.
			lastErr = err
			continue
		}
		msgs, err := openChronicleChunk(ct, ref.Keys)
		if err != nil {
			lastErr = err
			continue
		}
		opened++
		// Newest-first within the chunk, so the cut at limit takes the messages
		// nearest the cursor.
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Nano >= beforeNano {
				continue
			}
			picked = append(picked, msgs[i])
			if len(picked) >= limit {
				break
			}
		}
		if len(picked) >= limit {
			break
		}
	}
	if opened == 0 && lastErr != nil {
		return nil, lastErr
	}

	out := make([]ChronicleMessageView, 0, len(picked))
	for i := len(picked) - 1; i >= 0; i-- { // back into reading order
		msg := picked[i]
		v := ChronicleMessageView{
			ID: msg.ID, Nano: msg.Nano, Content: msg.Content, ReplyTo: msg.ReplyTo,
			Reactions: msg.Reactions, Attach: msg.Attach,
		}
		if msg.Author >= 0 && msg.Author < len(m.Authors) {
			v.Author = m.Authors[msg.Author].Name
			v.Avatar = m.Authors[msg.Author].Avatar
		}
		out = append(out, v)
	}
	return out, nil
}

// SetChroniclePinned keeps a guild's archive on this device permanently, or
// returns it to the cache. Pinning is what the importing machine does to its own
// copy and what anyone else does when they want the history available offline;
// unpinning does not delete anything, it only makes the pages evictable again.
func (s *Service) SetChroniclePinned(guildID string, pinned bool) error {
	m, _, ok := s.guildChronicle(guildID)
	if !ok {
		return fmt.Errorf("app: this guild has no archive")
	}
	ids := make([]string, 0, len(m.Chunks))
	for _, c := range m.Chunks {
		ids = append(ids, c.ID)
	}
	if err := s.store.PinChronicleChunks(ids, pinned); err != nil {
		return err
	}
	s.emitGuildUpdate()
	return nil
}

// SetChronicleCacheLimit sets how many bytes of UNPINNED archive pages this
// device keeps. Pinned pages are exempt and uncounted.
func (s *Service) SetChronicleCacheLimit(bytes int64) {
	s.store.SetChronicleCap(bytes)
}
