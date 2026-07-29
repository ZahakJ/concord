package app

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/store"
)

// Per-guild GIF packs: a guild curates its own collection, members search it
// locally and post from it. Nothing in THIS file talks to any third party — a
// pack search is a substring match over records already on disk, so no query
// and no IP leaves the machine.
//
// Searching Tenor is a separate, opt-in path that lives in gifsearch.go and
// goes through the user's own rendezvous rather than direct from each client.
// Read that file's header for what it does and does not protect; the two meet
// only at SaveSearchedGif, which drops a searched result into the pack here.
//
// The record is modelled on custom emoji (guild-scoped, gossiped on the
// guild-meta topic, learned by peers, validated on receive exactly as on local
// add) with one deliberate difference: a GIF is megabytes, not the 256 KB an
// emoji is capped at, so the BYTES never ride the meta topic. They go through
// the ordinary encrypted-attachment path (attach.go) and the gossiped record
// carries only a reference — blob id, key, subtype, dimensions — so peers fetch
// and cache the blob out of band, exactly like an image in a message, and any
// member who has viewed it becomes a source.

const (
	// A pack GIF is fetched over the same stream path as an inline image, so it
	// gets the same proven ceiling.
	maxGifPlain = maxAttachmentPlain

	maxGifNameRunes = 64
	maxGifTags      = 12
	maxGifTagRunes  = 32
	// maxGuildGifs bounds one guild's pack. The add path is permission-gated,
	// but a compromised manager key should not be able to grow every member's
	// database without limit.
	maxGuildGifs = 500
)

// gifTagRe pins a tag to one lowercase word. Tags are only ever matched
// against a locally typed query, never interpolated anywhere, but keeping them
// to a known charset means the picker's filter can't be fed something that
// behaves like markup or a regex.
var gifTagRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// A GuildGif is one entry in a guild's pack: searchable text plus a reference
// to the encrypted blob holding the actual image.
type GuildGif struct {
	// ID is the attachment blob id — unique per upload (the ciphertext is
	// sealed under a fresh random key), so it doubles as the record's identity
	// and needs no separate id scheme.
	ID      string   `json:"id"`
	GuildID string   `json:"guildId,omitempty"`
	Name    string   `json:"name"`
	Tags    []string `json:"tags,omitempty"`
	Keys    string   `json:"keys"`    // base64url key||nonce, as in an attachment token
	Subtype string   `json:"subtype"` // png | jpeg | gif | webp
	Width   int      `json:"w,omitempty"`
	Height  int      `json:"h,omitempty"`
}

// validGifText reports whether a display string is safe to store and show.
// Svelte escapes text, so this is not an XSS gate; it stops a peer planting a
// record whose "name" is a screenful of newlines, invisible bidi overrides or
// other formatting codepoints that would wreck the picker's layout or let one
// entry impersonate another.
func validGifText(s string, maxRunes int) bool {
	if s == "" || s != strings.TrimSpace(s) || utf8.RuneCountInString(s) > maxRunes {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cs, r) {
			return false
		}
	}
	return true
}

// validGuildGif validates a record from ANY source. The receive path runs this
// on a peer's record before storing it, and the send path runs it on our own —
// same function, so the two can't drift. Everything here is a field the client
// renders or feeds back into a message token: a bad blob id, key length or
// subtype would produce a token no peer can resolve, and oversized dimensions
// would let one record blow out the picker grid.
func validGuildGif(g GuildGif) error {
	if !blobIDRe.MatchString(g.ID) {
		return fmt.Errorf("app: bad gif id")
	}
	if k, err := b64url.DecodeString(g.Keys); err != nil || len(k) != attachKeysLen {
		return fmt.Errorf("app: bad gif key")
	}
	switch g.Subtype {
	case "png", "jpeg", "gif", "webp":
	default:
		return fmt.Errorf("app: bad gif type")
	}
	if g.Width < 0 || g.Width > 99999 || g.Height < 0 || g.Height > 99999 {
		return fmt.Errorf("app: bad gif dimensions")
	}
	if !validGifText(g.Name, maxGifNameRunes) {
		return fmt.Errorf("app: gif name must be 1–%d printable characters", maxGifNameRunes)
	}
	if len(g.Tags) > maxGifTags {
		return fmt.Errorf("app: at most %d tags", maxGifTags)
	}
	for _, t := range g.Tags {
		if !gifTagRe.MatchString(t) {
			return fmt.Errorf("app: tag %q must be one lowercase word of up to %d characters", t, maxGifTagRunes)
		}
	}
	return nil
}

// cleanGifTags lowercases, trims, drops empties and dedupes — applied to LOCAL
// input only. A peer's tags are validated, not cleaned: silently rewriting what
// arrives would mean two members' packs disagree about the same record.
func cleanGifTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, raw := range tags {
		// One field of "cats, reaction" is the natural way to type these.
		for _, t := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' }) {
			t = strings.ToLower(strings.TrimSpace(t))
			if t == "" || seen[t] || len(out) >= maxGifTags {
				continue
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// AddGuildGif seals an image into an attachment blob, records it in the guild's
// pack and announces the (small) record to the other members.
func (s *Service) AddGuildGif(guildID, name string, tags []string, dataURL string, w, h int) (GuildGif, error) {
	if !s.hasPerm(guildID, PermManageGuild) {
		return GuildGif{}, fmt.Errorf("app: you don't have permission to manage this guild")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return GuildGif{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	if have, err := s.store.GuildGifs(guildID); err == nil && len(have) >= maxGuildGifs {
		return GuildGif{}, fmt.Errorf("app: this guild already has %d GIFs — remove one first", maxGuildGifs)
	}

	m := dataURLRe.FindStringSubmatch(dataURL)
	if m == nil {
		return GuildGif{}, fmt.Errorf("app: a GIF must be a png/jpeg/gif/webp image data URL")
	}
	plain, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		return GuildGif{}, fmt.Errorf("app: bad image encoding: %w", err)
	}
	if len(plain) == 0 || len(plain) > maxGifPlain {
		return GuildGif{}, fmt.Errorf("app: a GIF must be 1 byte – %d MB", maxGifPlain>>20)
	}

	blobID, keys, err := s.sealBlob(plain)
	if err != nil {
		return GuildGif{}, err
	}
	gif := GuildGif{
		ID: blobID, GuildID: guildID,
		Name: strings.TrimSpace(name), Tags: cleanGifTags(tags),
		Keys: keys, Subtype: m[1], Width: w, Height: h,
	}
	if err := validGuildGif(gif); err != nil {
		return GuildGif{}, err
	}
	if err := s.store.SaveGuildGif(gifRow(gif)); err != nil {
		return GuildGif{}, err
	}
	s.emitGuildUpdate()
	announced := gif
	announced.GuildID = "" // the topic already says which guild; don't carry a claim we ignore
	s.publishMeta(groupID, guildMeta{Type: "gif_added", Gif: &announced})
	return gif, nil
}

// RemoveGuildGif drops a GIF from the guild's pack and announces the removal.
// The blob itself is left alone: messages already posted from this GIF still
// reference it, and the attachment store is what keeps them viewable.
func (s *Service) RemoveGuildGif(guildID, id string) error {
	if !s.hasPerm(guildID, PermManageGuild) {
		return fmt.Errorf("app: you don't have permission to manage this guild")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var groupID []byte
	if ok {
		groupID = g.GroupID
	}
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	if err := s.store.DeleteGuildGif(guildID, id); err != nil {
		return err
	}
	s.emitGuildUpdate()
	s.publishMeta(groupID, guildMeta{Type: "gif_removed", Gif: &GuildGif{ID: id}})
	return nil
}

// GuildGifs returns a guild's pack, newest first.
func (s *Service) GuildGifs(guildID string) ([]GuildGif, error) {
	rows, err := s.store.GuildGifs(guildID)
	if err != nil {
		return nil, err
	}
	out := make([]GuildGif, 0, len(rows))
	for _, r := range rows {
		out = append(out, gifFromRow(r))
	}
	return out, nil
}

// SendGuildGif posts a pack GIF into a channel as an ordinary image attachment
// message. It re-uses the blob the pack already holds rather than re-sealing
// the bytes, so posting the same GIF a hundred times costs one copy, and the
// token is the plain v1 form every existing client already renders.
func (s *Service) SendGuildGif(channelID, gifID, replyTo string) (domain.Message, error) {
	s.mu.RLock()
	guildID, tracked := s.channelToGuild[channelID]
	s.mu.RUnlock()
	if !tracked {
		return domain.Message{}, fmt.Errorf("app: unknown channel")
	}
	row, ok, err := s.store.GuildGif(guildID, gifID)
	if err != nil {
		return domain.Message{}, err
	}
	if !ok {
		return domain.Message{}, fmt.Errorf("app: that GIF is no longer in this guild's pack")
	}
	gif := gifFromRow(row)
	if err := validGuildGif(gif); err != nil {
		return domain.Message{}, err
	}
	token := fmt.Sprintf("![image](concord://attach/v1/%s/%s/%s/%dx%d)",
		gif.ID, gif.Keys, gif.Subtype, gif.Width, gif.Height)
	return s.send(channelID, token, "", replyTo)
}

// applyGifMeta applies a gif_added / gif_removed announcement learned from a
// peer. actor is the MLS-authenticated sender: managing the pack is a
// guild-management action, gated on receive the same way it is locally, so a
// member without the permission cannot plant or delete entries.
func (s *Service) applyGifMeta(guildID, actor, typ string, g *GuildGif) {
	if g == nil || !s.memberHasPerm(guildID, actor, PermManageGuild) {
		return
	}
	switch typ {
	case "gif_added":
		if !s.applyGuildGif(guildID, *g) {
			return
		}
	case "gif_removed":
		if !blobIDRe.MatchString(g.ID) {
			return
		}
		if err := s.store.DeleteGuildGif(guildID, g.ID); err != nil {
			return
		}
	default:
		return
	}
	s.emitGuildUpdate()
}

// applyGuildGif stores a record learned from a peer (guild-meta or history
// sync), validated exactly as a local add and bound to the guild whose topic
// carried it. Reports whether it was stored.
func (s *Service) applyGuildGif(guildID string, g GuildGif) bool {
	// The record's own GuildID claim is discarded: the only authority on which
	// guild this belongs to is the (MLS-encrypted, per-guild) topic it arrived
	// on. Otherwise a member of one guild could write into another's pack.
	g.GuildID = guildID
	if err := validGuildGif(g); err != nil {
		return false
	}
	// Cap the pack on the receive path too — an already-known id may always be
	// refreshed, but a new one past the ceiling is dropped.
	if _, known, err := s.store.GuildGif(guildID, g.ID); err == nil && !known {
		if have, err := s.store.GuildGifs(guildID); err == nil && len(have) >= maxGuildGifs {
			return false
		}
	}
	return s.store.SaveGuildGif(gifRow(g)) == nil
}

func gifRow(g GuildGif) store.GuildGifRow {
	return store.GuildGifRow{
		GuildID: g.GuildID, ID: g.ID, Name: g.Name, Tags: strings.Join(g.Tags, " "),
		Keys: g.Keys, Subtype: g.Subtype, Width: g.Width, Height: g.Height,
	}
}

func gifFromRow(r store.GuildGifRow) GuildGif {
	var tags []string
	if r.Tags != "" {
		tags = strings.Fields(r.Tags)
	}
	return GuildGif{
		ID: r.ID, GuildID: r.GuildID, Name: r.Name, Tags: tags,
		Keys: r.Keys, Subtype: r.Subtype, Width: r.Width, Height: r.Height,
	}
}
