package app

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zahak/concord/internal/domain"
	"github.com/zahak/concord/internal/store"
)

// Custom server emoji: a guild-scoped image referenced as :name: in messages
// and reactions. Stored as a small data URI, distributed to members over the
// guild-meta topic (and history sync), same trust model as channels/categories.

const maxEmojiBytes = 256 << 10 // 256 KiB per emoji image

var emojiNameRe = regexp.MustCompile(`^[a-z0-9_]{2,32}$`)

// emojiImageRe pins the image to a strict base64 data-URI whitelist. This is a
// security control, not just format hygiene: the image string is rendered into
// an <img src="…"> in the client's markdown renderer, so a value containing a
// double-quote (e.g. `data:image/png;base64,x" onerror=…`) would break out of
// the attribute and inject script. The base64 charset cannot contain a quote,
// and excluding svg avoids the scriptable-image class entirely.
var emojiImageRe = regexp.MustCompile(`^data:image/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+$`)

// validEmojiImage reports whether an image data URI is safe to store and render.
func validEmojiImage(dataURI string) bool {
	return len(dataURI) <= maxEmojiBytes && emojiImageRe.MatchString(dataURI)
}

// AddCustomEmoji adds (or replaces) a guild emoji and announces it.
func (s *Service) AddCustomEmoji(guildID, name, dataURI string) error {
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
	name = strings.ToLower(strings.TrimSpace(name))
	if !emojiNameRe.MatchString(name) {
		return fmt.Errorf("app: emoji name must be 2–32 chars, lowercase letters/numbers/underscore")
	}
	if !validEmojiImage(dataURI) {
		return fmt.Errorf("app: emoji must be a base64 PNG/JPEG/GIF/WebP image under %d KB", maxEmojiBytes/1024)
	}
	e := domain.CustomEmoji{GuildID: guildID, Name: name, Image: dataURI}
	if err := s.store.SaveCustomEmoji(store.CustomEmojiRow{GuildID: guildID, Name: name, Image: dataURI}); err != nil {
		return err
	}
	s.emitGuildUpdate()
	s.publishMeta(groupID, guildMeta{Type: "emoji_added", CustomEmoji: e})
	return nil
}

// RemoveCustomEmoji deletes a guild emoji and announces it.
func (s *Service) RemoveCustomEmoji(guildID, name string) error {
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
	name = strings.ToLower(strings.TrimSpace(name))
	if err := s.store.DeleteCustomEmoji(guildID, name); err != nil {
		return err
	}
	s.emitGuildUpdate()
	s.publishMeta(groupID, guildMeta{Type: "emoji_removed", CustomEmoji: domain.CustomEmoji{GuildID: guildID, Name: name}})
	return nil
}

// CustomEmoji returns a guild's custom emoji.
func (s *Service) CustomEmoji(guildID string) ([]domain.CustomEmoji, error) {
	rows, err := s.store.CustomEmoji(guildID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.CustomEmoji, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.CustomEmoji{GuildID: r.GuildID, Name: r.Name, Image: r.Image})
	}
	return out, nil
}

// applyCustomEmoji stores an emoji learned from a peer (guild-meta or sync),
// validating it the same way as a local add.
func (s *Service) applyCustomEmoji(guildID string, e domain.CustomEmoji) {
	// Same strict validation as a local add — a malicious peer must not be able
	// to plant an emoji whose image string breaks out of the client's <img src>
	// (stored XSS). See emojiImageRe.
	if !emojiNameRe.MatchString(e.Name) || !validEmojiImage(e.Image) {
		return
	}
	_ = s.store.SaveCustomEmoji(store.CustomEmojiRow{GuildID: guildID, Name: e.Name, Image: e.Image})
}
