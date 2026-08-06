package app

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/identity"
	"github.com/zahak/concord/internal/store"
)

// Moments v1: guild-scoped stories. A story is TEXT ON A BANNER PRESET —
// a preset id, a short caption, and the author's accent color pair. No photos,
// no video, no view receipts; the record is a few hundred bytes, so it rides
// the same MLS-encrypted guild-meta topic a profile announce does and the same
// history-sync payload, with no blob machinery at all.
//
// Every record is signed by its AUTHOR's account key. That is not decoration:
// the gossip path authenticates the sender via MLS, but history-sync content
// is attested by the RESPONDER, not the original author (README §12's forgery
// hole — the same gap that leaves synced messages forgeable today). A forged
// "story from Alice" must die at verification on BOTH arrival paths, so the
// signature is checked on gossip receive AND on sync apply, against the key
// the guild roster holds for the claimed author.

const (
	// maxStoryPresetBytes length-caps the preset reference. The frontend owns
	// the preset library; the server's contract is only "a short 'preset:<id>'
	// string" — but the id renders into a CSS context on every viewer's
	// machine, so it is additionally held to the same charset every other
	// banner preset is (validPresetID).
	maxStoryPresetBytes = 64
	// maxStoryCaptionBytes bounds the caption — a story is a headline, not a
	// blog post, and the record rides gossip frames and sync payloads.
	maxStoryCaptionBytes = 300
	// storyLifetime is how long a story lives. Fixed, not author-chosen: the
	// receive side rejects anything longer, so a peer cannot make a story
	// immortal on everyone's disk (same posture as meeting_life's cap).
	storyLifetime = 24 * time.Hour
	// maxSyncStoriesPerGuild caps one guild's stories in a sync response —
	// newest first, so what gets cut is what was about to expire anyway.
	maxSyncStoriesPerGuild = 20
	// storyGCTick paces the expiry sweep. Coarse on purpose: reads already
	// filter by expiry (store.GuildStories takes now), so the sweep only
	// reclaims disk — an hour of dead rows is invisible.
	storyGCTick = time.Hour
)

// storyRecord is the wire and app form of one story. It travels MLS-encrypted
// (guild-meta gossip and sync payloads), so members-only confidentiality is
// the transport's; the record's own Sig supplies what MLS cannot — authorship
// that survives being relayed by a third party.
type storyRecord struct {
	StoryID string `json:"id"`
	// GuildID is COVERED BY THE SIGNATURE, unlike a GIF record's advisory
	// claim: the receive side requires it to equal the guild whose topic (or
	// sync lane) carried it, so a story signed for one guild cannot be
	// replayed into another — not even by its own author.
	GuildID string `json:"guildId"`
	Author  string `json:"author"` // author's account fingerprint
	Preset  string `json:"preset"` // "preset:<id>" banner reference
	Caption string `json:"caption,omitempty"`
	// Color1/Color2 are the author's accent pair, carried clear in the store —
	// the same posture as profile colors, and like them they render into
	// inline CSS, so both are held to the #hex gate on every path.
	Color1    string `json:"color1,omitempty"`
	Color2    string `json:"color2,omitempty"`
	PostedAt  int64  `json:"postedAt"`  // unix seconds
	ExpiresAt int64  `json:"expiresAt"` // unix seconds
	Sig       []byte `json:"sig"`       // author's signature (see signingBytes)
}

// signingBytes is the canonical byte encoding the author signs: the record
// with Sig zeroed and the caption replaced by its sha256 — same JSON-marshal
// idiom as govOp.signingBytes, so the two can't drift in style. Hashing the
// caption (rather than embedding it raw) keeps the signed form canonical even
// if some future transport re-encodes the caption text.
func (r storyRecord) signingBytes() []byte {
	sum := sha256.Sum256([]byte(r.Caption))
	c := r
	c.Sig = nil
	c.Caption = hex.EncodeToString(sum[:])
	b, _ := json.Marshal(c)
	return b
}

// verifySig checks the record's signature against the given account public
// key AND that the key actually belongs to the claimed author — a signature
// by *some* valid key proves nothing if the key isn't the Author's.
func (r storyRecord) verifySig(authorKey []byte) bool {
	if len(authorKey) != ed25519.PublicKeySize || len(r.Sig) != ed25519.SignatureSize {
		return false
	}
	if identity.FingerprintOf(authorKey) != r.Author {
		return false
	}
	return identity.Verify(authorKey, r.signingBytes(), r.Sig)
}

// validStoryPreset admits the frontend contract: a "preset:<id>" string,
// length-capped and charset-pinned because the id ends up in CSS.
func validStoryPreset(preset string) bool {
	if len(preset) > maxStoryPresetBytes || !strings.HasPrefix(preset, presetPrefix) {
		return false
	}
	return validPresetID(strings.TrimPrefix(preset, presetPrefix))
}

// validStoryRecord validates a record from ANY source — our own PostStory runs
// it before signing and both receive paths run it before storing, one
// implementation so they can't drift (the validGuildGif lesson).
func validStoryRecord(r storyRecord) bool {
	if r.StoryID == "" || len(r.StoryID) > 64 || r.GuildID == "" || r.Author == "" {
		return false
	}
	if !validStoryPreset(r.Preset) {
		return false
	}
	if len(r.Caption) > maxStoryCaptionBytes {
		return false
	}
	// Colors render into inline CSS on every viewer's machine; a peer's string
	// is no more trustworthy than our own (see sanitizeProfileExtras).
	if !validColor(r.Color1) || !validColor(r.Color2) {
		return false
	}
	if r.PostedAt <= 0 || r.ExpiresAt <= r.PostedAt {
		return false
	}
	// A story lives storyLifetime, full stop. Rejecting a longer claim here is
	// what keeps a hostile peer from parking a record on our disk forever.
	if r.ExpiresAt-r.PostedAt > int64(storyLifetime/time.Second) {
		return false
	}
	return true
}

func storyRow(r storyRecord) store.StoryRow {
	return store.StoryRow{
		StoryID: r.StoryID, GuildID: r.GuildID, Author: r.Author,
		Preset: r.Preset, Caption: r.Caption, Color1: r.Color1, Color2: r.Color2,
		PostedAt: r.PostedAt, ExpiresAt: r.ExpiresAt, Sig: r.Sig,
	}
}

func storyFromRow(w store.StoryRow) storyRecord {
	return storyRecord{
		StoryID: w.StoryID, GuildID: w.GuildID, Author: w.Author,
		Preset: w.Preset, Caption: w.Caption, Color1: w.Color1, Color2: w.Color2,
		PostedAt: w.PostedAt, ExpiresAt: w.ExpiresAt, Sig: w.Sig,
	}
}

// memberAccountKey resolves a member fingerprint to the account public key the
// guild's MLS roster holds for it — the same authority govOp verification
// leans on (the op carries its Signer key, and replay checks that key's
// fingerprint against the roster-derived state). A story carries only the
// fingerprint, so the key is looked up rather than trusted from the wire.
func (s *Service) memberAccountKey(guildID, fpr string) []byte {
	if fpr == "" {
		return nil
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	creds, err := s.mls.Members(s.ctx, groupID)
	if err != nil {
		return nil
	}
	for _, c := range creds {
		if accountFingerprintOf(c) == fpr {
			return accountKeyOf(c)
		}
	}
	return nil
}

// ingestStory is the single funnel every story from every source passes
// through: signature, shape, guild binding, expiry, then an idempotent
// insert. Reports whether a NEW row was stored (a replay of a known id is a
// clean false, so callers don't re-emit events for it).
func (s *Service) ingestStory(guildID string, r storyRecord, authorKey []byte, nowUnix int64) bool {
	// The only authority on which guild a story belongs to is the MLS-encrypted
	// lane it arrived on — and here, unlike a GIF record, the claim is signed,
	// so a mismatch is dropped rather than rewritten (rewriting would break
	// the signature and store a record no peer could ever re-verify).
	if r.GuildID != guildID {
		return false
	}
	if !validStoryRecord(r) {
		return false
	}
	// Drop if expired: sync and gossip both replay old records, and a dead
	// story must not be resurrected onto anyone's screen.
	if r.ExpiresAt <= nowUnix {
		return false
	}
	if !r.verifySig(authorKey) {
		return false
	}
	inserted, err := s.store.SaveStory(storyRow(r))
	return err == nil && inserted
}

// PostStory publishes one story to each of the given guilds. Per guild the
// record is built fresh (its own id, the guild's id under the signature),
// signed, stored locally, and announced on the guild META topic exactly like a
// profile announce — same MLS-encrypted guildMeta frame, Type "story".
func (s *Service) PostStory(guildIDs []string, preset, caption string) error {
	caption = strings.TrimSpace(caption)
	if !validStoryPreset(preset) {
		return fmt.Errorf("app: a story needs a %q banner preset", presetPrefix+"<id>")
	}
	if len(caption) > maxStoryCaptionBytes {
		return fmt.Errorf("app: a story caption is at most %d bytes", maxStoryCaptionBytes)
	}
	if len(guildIDs) == 0 {
		return fmt.Errorf("app: pick at least one guild for the story")
	}
	// The author's accent pair comes from their own profile — a story is
	// rendered in the author's colors, like their profile card.
	p := s.SelfProfile()
	now := time.Now().Unix()
	self := s.id.Fingerprint()

	var posted int
	for _, guildID := range guildIDs {
		s.mu.RLock()
		g, ok := s.guilds[guildID]
		var groupID []byte
		if ok {
			groupID = g.GroupID
		}
		s.mu.RUnlock()
		if !ok {
			continue
		}
		rec := storyRecord{
			StoryID: domain.NewID(), GuildID: guildID, Author: self,
			Preset: preset, Caption: caption, Color1: p.Color, Color2: p.Color2,
			PostedAt: now, ExpiresAt: now + int64(storyLifetime/time.Second),
		}
		rec.Sig = s.id.Sign(rec.signingBytes())
		// Store through the same funnel a peer's record takes — if our own
		// record wouldn't survive the receive gate, better to fail here than
		// to publish something every honest peer silently drops.
		if !s.ingestStory(guildID, rec, s.id.PublicKey(), now) {
			continue
		}
		posted++
		s.emitStory(guildID)
		raw := rec
		s.publishMeta(groupID, guildMeta{Type: "story", Story: &raw})
	}
	if posted == 0 {
		return fmt.Errorf("app: story posted to none of the %d guilds", len(guildIDs))
	}
	return nil
}

// applyStoryMeta is the receive half of a gossiped story announce.
func (s *Service) applyStoryMeta(guildID, actor string, m guildMeta) {
	if m.Story == nil {
		return
	}
	rec := *m.Story
	// THE ACTOR-BINDING GATE (same as applyProfileMeta): a story only speaks
	// for its own author. The record's self-reported Author fingerprint MUST
	// equal the MLS-authenticated sender's account fingerprint, or any
	// co-member could post stories as you. Drop silently on mismatch.
	if rec.Author != actor {
		return
	}
	// Verify the author's signature against the account key the guild roster
	// holds for them. Redundant with the actor gate on THIS path (MLS already
	// authenticated the sender) — kept anyway, because it is the only gate the
	// sync path has, and a record that fails it must never enter the store
	// where sync would re-serve it as ours-attested.
	key := s.memberAccountKey(guildID, rec.Author)
	if !s.ingestStory(guildID, rec, key, time.Now().Unix()) {
		return
	}
	s.emitStory(guildID)
}

// applySyncedStory folds one story from a history-sync payload into local
// state. The responder attests NOTHING here: sync payloads are the responding
// member's local copies, so authorship must come from the record's own
// signature, and membership of the claimed author is re-checked against OUR
// roster — otherwise any member could serve a doctored snapshot carrying
// stories "from" people who were never even in the guild (README §12).
// Reports whether a new story was stored.
func (s *Service) applySyncedStory(guildID string, rec storyRecord) bool {
	if !s.guildHasMember(guildID, rec.Author) {
		return false
	}
	key := s.memberAccountKey(guildID, rec.Author)
	return s.ingestStory(guildID, rec, key, time.Now().Unix())
}

// storiesForSync returns the unexpired stories a sync RESPONDER serves for one
// guild — filtered by expiry before answering (a dead record wastes payload
// budget and would be dropped by the applier anyway) and capped to the newest
// maxSyncStoriesPerGuild.
func (s *Service) storiesForSync(guildID string, nowUnix int64) []storyRecord {
	rows, err := s.store.GuildStories(guildID, nowUnix)
	if err != nil {
		return nil
	}
	if len(rows) > maxSyncStoriesPerGuild {
		rows = rows[:maxSyncStoriesPerGuild] // newest first already
	}
	out := make([]storyRecord, 0, len(rows))
	for _, w := range rows {
		out = append(out, storyFromRow(w))
	}
	return out
}

// GuildStories returns a guild's unexpired stories, newest first.
func (s *Service) GuildStories(guildID string) ([]storyRecord, error) {
	rows, err := s.store.GuildStories(guildID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	out := make([]storyRecord, 0, len(rows))
	for _, w := range rows {
		out = append(out, storyFromRow(w))
	}
	return out, nil
}

// MarkStorySeen records locally that the user opened a story. Local-only by
// design — v1 has no view receipts, so nothing is announced.
func (s *Service) MarkStorySeen(storyID string) error {
	return s.store.MarkStorySeen(storyID)
}

// StoryIsSeen reports whether the user opened a story on this device.
func (s *Service) StoryIsSeen(storyID string) bool {
	seen, err := s.store.StoryIsSeen(storyID)
	return err == nil && seen
}

// OnStory registers a callback fired when a guild's stories change. An empty
// guildID means "several guilds may have changed" (the expiry sweep).
func (s *Service) OnStory(fn func(guildID string)) {
	s.mu.Lock()
	s.onStory = append(s.onStory, fn)
	s.mu.Unlock()
}

func (s *Service) emitStory(guildID string) {
	s.mu.RLock()
	cbs := append([]func(string){}, s.onStory...)
	s.mu.RUnlock()
	for _, cb := range cbs {
		cb(guildID)
	}
}

// runStoryGCLoop sweeps expired stories: once at open (a client that was
// closed for a day starts clean, not with yesterday's stories briefly
// flickering into view) and hourly after that, stretched to the background
// beat on a backgrounded phone like the scheduled-send sweep it mirrors.
// Started once at service start; lives until shutdown.
func (s *Service) runStoryGCLoop() {
	s.gcStories(time.Now().Unix())
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.bgWakeCh():
			// Foregrounded: reclaim whatever expired during the slow beat.
		case <-time.After(s.bgPace(storyGCTick)):
		}
		s.gcStories(time.Now().Unix())
	}
}

// gcStories deletes expired stories and pokes the UI if anything went.
// Split from the loop so tests can drive the time boundary directly.
func (s *Service) gcStories(nowUnix int64) {
	if n, err := s.store.DeleteExpiredStories(nowUnix); err == nil && n > 0 {
		s.emitStory("")
	}
}
