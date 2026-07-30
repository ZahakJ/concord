package app

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zahak/concord/internal/domain"
)

// forum.go turns a forum channel from a list of titles into a board you can
// navigate: a per-forum tag palette, per-post tags, pinning, and an answered
// marker — plus the derived per-post metadata (author, reply count, excerpt)
// that makes a card worth looking at.
//
// The organising decision here is DERIVE, DON'T CARRY. A post is a channel, and
// a channel record is announced to every member and stored by every member
// forever. Reply count and opening author are both recoverable from the post's
// own messages, so they never touch the wire: counting is a grouped query
// (store.PostStatsFor), and the first message's sender is MLS-authenticated,
// which makes it strictly MORE trustworthy than an "author" string a peer wrote
// into a channel announcement. Only what cannot be derived — a curator's
// choices: which tags exist, which a post carries, whether it is pinned or
// answered — is state, and each of those is one optional field that only
// appears on a forum or a post.
//
// Compatibility: every field is optional and ignorable, and the two new
// guild-meta types ("forum_tags", "post_meta") fall through the switch on a peer
// that predates them. Such a peer sees a tagged, pinned, solved post as exactly
// what it is underneath — an ordinary thread channel nested under its forum.

const (
	// maxForumTags bounds a forum's palette. A tag set you have to scroll is not
	// a taxonomy, it's a second search box; twenty is more than any real board
	// uses and small enough that the chips fit one filter row.
	maxForumTags = 20
	// maxPostTags bounds one post. Five keeps a card's chip row readable and
	// keeps a post from carrying the whole palette, which would make tags
	// meaningless as a filter.
	maxPostTags = 5
	// maxTagNameRunes / maxTagEmojiRunes bound a tag label and its optional
	// glyph. Runes, not bytes: a byte cap truncates mid-character, and these are
	// short deliberate inputs from a settings form, so an over-long one is
	// refused rather than mangled. Eight runes of emoji is room for a ZWJ family
	// sequence, not a sentence.
	maxTagNameRunes  = 24
	maxTagEmojiRunes = 8
	// maxPostExcerpt is how much of the opening message a board card shows.
	maxPostExcerpt = 240
)

// validTagID bounds a forum-tag id to a charset that cannot escape the CSS or
// attribute context the client renders a chip in — deliberately the same bound
// as a banner preset id, for the same reason (see validPresetID). Locally minted
// ids are domain.NewID(), which is 32 lowercase hex characters and passes.
func validTagID(id string) bool { return validPresetID(id) }

// validHexColor accepts exactly "#rrggbb". Strict on purpose: this string is
// interpolated into a CSS colour by the client, so "#abc", "rgb(0 0 0)", "red"
// and "#fff;background:url(x)" are all refused rather than parsed. A validator
// that tries to understand CSS is a validator with a hole in it.
func validHexColor(c string) bool {
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for i := 1; i < 7; i++ {
		switch ch := c[i]; {
		case ch >= '0' && ch <= '9', ch >= 'a' && ch <= 'f', ch >= 'A' && ch <= 'F':
		default:
			return false
		}
	}
	return true
}

// validTagText reports whether a tag label/glyph is safe to hand a renderer:
// valid UTF-8, within its rune budget, and made only of characters that draw
// themselves. An interior space is fine — "In progress" is a tag name — but a
// newline breaks the chip row it sits in, and a bidi override re-orders the text
// AROUND the chip rather than inside it, which is how one member's tag garbles
// everyone's filter bar.
func validTagText(s string, maxRunes int, allowEmpty bool) bool {
	if s == "" {
		return allowEmpty
	}
	if !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxRunes {
		return false
	}
	for _, r := range s {
		switch {
		case unicode.IsControl(r):
			return false
		case r == 0x2028, r == 0x2029: // line/paragraph separator: a newline by another name
			return false
		case r >= 0x202A && r <= 0x202E, // LRE, RLE, PDF, LRO, RLO
			r >= 0x2066 && r <= 0x2069, // LRI, RLI, FSI, PDI
			r == 0x200E, r == 0x200F:   // LRM, RLM
			return false
		}
	}
	return true
}

// sanitizeForumTags reduces a palette to the entries that are actually usable:
// well-formed id, non-empty label, strict colour, deduplicated, capped. It never
// errors — this is the shape used on the RECEIVE path, where the only options
// are "take what is valid" and "drop the frame", and dropping a whole palette
// because one entry is malformed hands any member a way to keep a forum
// untagged. The local path checks first and reports (see SetForumTags), so a
// human gets told why rather than watching a tag vanish.
func sanitizeForumTags(tags []domain.ForumTag) []domain.ForumTag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]domain.ForumTag, 0, min(len(tags), maxForumTags))
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		if len(out) >= maxForumTags {
			break
		}
		t.Name = strings.TrimSpace(t.Name)
		t.Emoji = strings.TrimSpace(t.Emoji)
		t.Color = strings.ToLower(strings.TrimSpace(t.Color))
		if !validTagID(t.ID) || seen[t.ID] {
			continue
		}
		if !validTagText(t.Name, maxTagNameRunes, false) ||
			!validTagText(t.Emoji, maxTagEmojiRunes, true) ||
			!validHexColor(t.Color) {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	return out
}

// sanitizePostTags reduces a post's tag list to well-formed, deduplicated,
// capped ids. It does NOT check them against the forum's palette: on the receive
// path the palette may not have arrived yet, and dropping a valid tag because of
// frame ordering is a bug you can never see. An id with no palette entry renders
// as nothing, which is the same thing the client does for a tag that was later
// deleted — so the unknown case is already designed for. The local path filters
// against the palette, where we are certain we have it.
func sanitizePostTags(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(ids), maxPostTags))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if len(out) >= maxPostTags {
			break
		}
		if !validTagID(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// sanitizeForumMeta scrubs the forum fields of a channel record arriving from
// ANYWHERE — a peer's channel_added, a history-sync snapshot, or our own
// creation path. It lives at the single funnel every one of those goes through
// (addChannel) rather than at each call site, because the call site that gets
// forgotten is the one that matters: history sync adopts a peer-supplied
// domain.Channel wholesale, so a validator bolted only onto the gossip path
// would leave a peer free to hand us a ten-thousand-entry palette with a colour
// of `#fff;background:url(…)` and have it stored and rendered.
//
// It also clears fields that cannot mean anything for the channel's type, so a
// text channel never carries a tag palette and a forum is never itself "solved".
func sanitizeForumMeta(c *domain.Channel) {
	switch c.ChannelType() {
	case "forum":
		c.ForumTags = sanitizeForumTags(c.ForumTags)
		c.Tags, c.Pinned, c.Solved = nil, false, false
		c.Banner = sanitizeForumBanner(c.Banner)
	case "thread":
		c.Tags = sanitizePostTags(c.Tags)
		c.ForumTags, c.Banner = nil, ""
	default:
		c.ForumTags, c.Tags, c.Pinned, c.Solved, c.Banner = nil, nil, false, false, ""
	}
}

// sanitizeForumBanner keeps a banner only if it is one of the two shapes the
// client can safely paint, and returns "" for anything else rather than an
// error — this runs on records arriving from peers, where the right answer to a
// junk field is to drop the field, not the record.
//
// The two shapes are exactly the guild banner's, and deliberately share its
// validators: a complete base64 raster data URI, or "preset:<id>" over a narrow
// charset. A PREFIX check is not enough. The guild header once built an unquoted
// CSS url() from this kind of value, so a string beginning "data:image/" and
// continuing ");background:url(http://…" escaped the declaration and made every
// member who opened it fetch a remote asset — an IP disclosure handed to
// whoever set the banner.
func sanitizeForumBanner(b string) string {
	if b == "" {
		return ""
	}
	if strings.HasPrefix(b, presetPrefix) {
		if validPresetID(strings.TrimPrefix(b, presetPrefix)) {
			return b
		}
		return ""
	}
	if validImageDataURI(b, maxForumBannerBytes) {
		return b
	}
	return ""
}

// maxForumBannerBytes bounds an uploaded forum banner. Smaller than a guild's:
// a guild has one, a guild may hold many forums, and every one of them rides in
// the channel list each member stores and syncs.
const maxForumBannerBytes = 384 << 10

// SetForumBanner sets (or clears, with "") a forum's own artwork. Managing a
// channel's appearance is the same permission as managing the channel.
func (s *Service) SetForumBanner(guildID, forumID, banner string) error {
	if !s.hasPerm(guildID, PermManageChannels) {
		return fmt.Errorf("app: you don't have permission to manage channels")
	}
	if banner != "" && sanitizeForumBanner(banner) == "" {
		return fmt.Errorf("app: a forum banner must be a png/jpeg/gif/webp data URI under %d KB, or a preset", maxForumBannerBytes/1024)
	}
	if !s.isForum(guildID, forumID) {
		return fmt.Errorf("app: banners can only be set on a forum channel")
	}
	_, groupID, ok := s.mutateChannel(guildID, forumID, func(c *domain.Channel) bool {
		c.Banner = banner
		return true
	})
	if !ok {
		return fmt.Errorf("app: unknown forum %s", forumID)
	}
	// Its own meta type for the same reason the palette has one: channel_updated
	// publishes a bare four-field Channel, so folding the banner into it would
	// erase it every time somebody moved the channel.
	s.publishMeta(groupID, guildMeta{Type: "forum_banner", ChannelID: forumID, ForumBanner: banner})
	return nil
}

// applyForumBanner is the receive half, authorized and validated exactly as the
// local path is — a peer's banner string reaches the same CSS context ours does.
func (s *Service) applyForumBanner(guildID, actor, forumID, banner string) {
	if forumID == "" || !s.memberHasPerm(guildID, actor, PermManageChannels) {
		return
	}
	if !s.isForum(guildID, forumID) {
		return
	}
	clean := sanitizeForumBanner(banner)
	if banner != "" && clean == "" {
		return // junk: leave whatever the forum already had rather than clearing it
	}
	s.mutateChannel(guildID, forumID, func(c *domain.Channel) bool {
		c.Banner = clean
		return true
	})
}

// isForum reports whether the id names a forum channel in this guild.
func (s *Service) isForum(guildID, channelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return false
	}
	for _, c := range g.Channels {
		if c.ID == channelID && c.ChannelType() == "forum" {
			return true
		}
	}
	return false
}

// mayAddChannel decides whether actor is allowed to introduce this channel to
// this guild — the authorization behind an inbound channel_added.
//
// Creating a channel needs ManageChannels. Opening a forum POST does not:
// CreateThread deliberately skips that check because a post is member content,
// exactly like a message. Demanding the permission on the RECEIVE side anyway
// meant an ordinary member's post was dropped by every other peer — it existed
// only on the author's screen, and the author had no way to tell. (The symptom
// was intermittent rather than total, because history sync adopts a peer's
// channel list wholesale and would eventually repair it. A post that shows up on
// a delay nobody can explain is arguably the worse failure.)
//
// A post is recognised structurally — a thread whose parent is a forum we
// already hold in this guild — so this grants nothing beyond posting: a member
// cannot smuggle in a text channel, a voice room, or a thread under a channel
// that is not a forum.
func (s *Service) mayAddChannel(guildID, actor string, ch domain.Channel) bool {
	if s.isForumPost(guildID, ch) {
		return true
	}
	return s.memberHasPerm(guildID, actor, PermManageChannels)
}

// isForumPost reports whether a channel record describes a post in a forum this
// guild actually has. Used to recognise member content on the receive path.
func (s *Service) isForumPost(guildID string, c domain.Channel) bool {
	if c.Type != "thread" || c.Parent == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return false
	}
	for _, existing := range g.Channels {
		if existing.ID == c.Parent && existing.Type == "forum" {
			return true
		}
	}
	return false
}

// postAndForum resolves a post channel and its forum inside one guild. It is the
// gate that stops a post id from another guild (or a plain text channel) being
// curated as if it were a post here.
func (s *Service) postAndForum(guildID, postID string) (post, forum domain.Channel, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, gok := s.guilds[guildID]
	if !gok {
		return post, forum, false
	}
	for _, c := range g.Channels {
		if c.ID == postID {
			post = c
		}
	}
	if post.ID == "" || post.ChannelType() != "thread" || post.Parent == "" {
		return post, forum, false
	}
	for _, c := range g.Channels {
		if c.ID == post.Parent && c.ChannelType() == "forum" {
			forum = c
		}
	}
	return post, forum, forum.ID != ""
}

// mutateChannel applies mut to one channel under the lock, persists the guild,
// and refreshes the UI — the update path for state that addChannel deliberately
// will not touch (it early-returns on a channel it already knows, which is what
// keeps a re-announced post from resetting its own board state). Returns the
// channel as it now stands, and false if nothing changed.
func (s *Service) mutateChannel(guildID, channelID string, mut func(*domain.Channel) bool) (domain.Channel, []byte, bool) {
	s.mu.Lock()
	g, ok := s.guilds[guildID]
	if !ok {
		s.mu.Unlock()
		return domain.Channel{}, nil, false
	}
	changed := false
	var updated domain.Channel
	for i := range g.Channels {
		if g.Channels[i].ID == channelID {
			changed = mut(&g.Channels[i])
			updated = g.Channels[i]
		}
	}
	groupID := g.GroupID
	s.mu.Unlock()
	if !changed {
		return updated, groupID, false
	}
	// One row, not the whole guild: see store.UpdateChannelForumMeta.
	_ = s.store.UpdateChannelForumMeta(updated)
	s.emitGuildUpdate()
	return updated, groupID, true
}

// SetForumTags replaces a forum's tag palette (Manage Channels — defining the
// board's taxonomy is curation, not posting). Entries without an id get one
// minted, so a client can send {name,color} for a new tag; entries WITH an id
// keep it, which is what makes renaming or recolouring a tag leave every post
// that carries it tagged. Returns the stored palette so the caller learns the
// minted ids.
//
// Removing a tag does not rewrite the posts that carry it: that would mean a
// channel announcement per post, and the client already renders an unknown id as
// nothing. Re-adding the tag with the same id restores it everywhere.
func (s *Service) SetForumTags(guildID, forumID string, tags []domain.ForumTag) ([]domain.ForumTag, error) {
	if !s.hasPerm(guildID, PermManageChannels) {
		return nil, fmt.Errorf("app: you don't have permission to manage channels")
	}
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	isForum := false
	if ok {
		for _, c := range g.Channels {
			if c.ID == forumID && c.ChannelType() == "forum" {
				isForum = true
			}
		}
	}
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("app: unknown guild %s", guildID)
	}
	if !isForum {
		return nil, fmt.Errorf("app: tags can only be set on a forum channel")
	}
	if len(tags) > maxForumTags {
		return nil, fmt.Errorf("app: a forum can define at most %d tags", maxForumTags)
	}
	// Report the first bad entry instead of silently dropping it — a tag the user
	// typed that then does not appear is the worst possible feedback.
	clean := make([]domain.ForumTag, 0, len(tags))
	for _, t := range tags {
		t.Name = strings.TrimSpace(t.Name)
		t.Emoji = strings.TrimSpace(t.Emoji)
		t.Color = strings.ToLower(strings.TrimSpace(t.Color))
		if t.ID == "" {
			t.ID = domain.NewID()
		}
		if !validTagID(t.ID) {
			return nil, fmt.Errorf("app: bad tag id %q", t.ID)
		}
		if !validTagText(t.Name, maxTagNameRunes, false) {
			return nil, fmt.Errorf("app: a tag name must be 1–%d characters of plain text", maxTagNameRunes)
		}
		if !validTagText(t.Emoji, maxTagEmojiRunes, true) {
			return nil, fmt.Errorf("app: a tag emoji must be at most %d characters", maxTagEmojiRunes)
		}
		if !validHexColor(t.Color) {
			return nil, fmt.Errorf("app: tag colour must be a #rrggbb hex value, got %q", t.Color)
		}
		clean = append(clean, t)
	}
	clean = sanitizeForumTags(clean) // dedupes ids; everything else already passed
	_, groupID, _ := s.mutateChannel(guildID, forumID, func(c *domain.Channel) bool {
		c.ForumTags = clean
		return true
	})
	// Its own meta type, NOT channel_updated: channel_updated carries a bare
	// Channel built from four fields, so folding the palette into it would make
	// every ordinary "move this channel" announcement erase the tags.
	s.publishMeta(groupID, guildMeta{Type: "forum_tags", ChannelID: forumID, ForumTags: clean})
	return clean, nil
}

// mayCuratePost reports whether actor may change a post's tags or answered mark:
// its author may (closing your own question, tagging your own post), and so may
// a moderator holding Manage Messages. Pinning is deliberately NOT here — see
// SetPostPinned.
//
// authorKnown is false when the post's opening message has not synced yet, in
// which case there is no author to compare against and only the moderator arm
// can pass. That is the honest answer: authorship is derived from an
// MLS-authenticated message, and we do not have it.
func (s *Service) mayCuratePost(guildID, postID, actor string) bool {
	if actor == "" {
		return false
	}
	if s.memberHasPerm(guildID, actor, PermManageMessages) {
		return true
	}
	author, ok := s.postAuthor(postID)
	return ok && author == actor
}

// postAuthor is the account fingerprint of whoever wrote a post's opening
// message, derived from the message itself rather than from anything a peer
// asserted about the channel.
func (s *Service) postAuthor(postID string) (string, bool) {
	stats, err := s.store.PostStatsFor([]string{postID})
	if err != nil {
		return "", false
	}
	st, ok := stats[postID]
	if !ok || len(st.AuthorKey) == 0 {
		return "", false
	}
	return accountFingerprintOf(st.AuthorKey), true
}

// SetPostTags sets which of its forum's tags a post carries. Author or
// Manage Messages. Unknown ids are rejected here (unlike on the receive path)
// because locally we are certain we hold the palette, so an unknown id is a
// client bug worth surfacing rather than a sync race.
func (s *Service) SetPostTags(guildID, postID string, tagIDs []string) error {
	_, forum, ok := s.postAndForum(guildID, postID)
	if !ok {
		return fmt.Errorf("app: unknown forum post %s", postID)
	}
	if !s.mayCuratePost(guildID, postID, s.id.Fingerprint()) {
		return fmt.Errorf("app: only the author or a moderator can tag this post")
	}
	if len(tagIDs) > maxPostTags {
		return fmt.Errorf("app: a post can carry at most %d tags", maxPostTags)
	}
	known := make(map[string]bool, len(forum.ForumTags))
	for _, t := range forum.ForumTags {
		known[t.ID] = true
	}
	for _, id := range tagIDs {
		if !known[id] {
			return fmt.Errorf("app: this forum has no tag %q", id)
		}
	}
	clean := sanitizePostTags(tagIDs)
	_, groupID, _ := s.mutateChannel(guildID, postID, func(c *domain.Channel) bool {
		c.Tags = clean
		return true
	})
	s.publishMeta(groupID, guildMeta{Type: "post_meta", ChannelID: postID, PostTags: &clean})
	return nil
}

// SetPostPinned floats a post to the top of its board, or unpins it. Manage
// Messages only — and NOT the author, however much they would like to be first.
// It is the same permission bit that already governs pinning a message, so a
// moderator's existing role needs no change to moderate a forum.
func (s *Service) SetPostPinned(guildID, postID string, pinned bool) error {
	if _, _, ok := s.postAndForum(guildID, postID); !ok {
		return fmt.Errorf("app: unknown forum post %s", postID)
	}
	if !s.hasPerm(guildID, PermManageMessages) {
		return fmt.Errorf("app: you need the Manage messages permission to pin posts")
	}
	_, groupID, changed := s.mutateChannel(guildID, postID, func(c *domain.Channel) bool {
		if c.Pinned == pinned {
			return false
		}
		c.Pinned = pinned
		return true
	})
	if !changed {
		return nil // already in that state; publishing would be noise
	}
	s.publishMeta(groupID, guildMeta{Type: "post_meta", ChannelID: postID, PostPinned: &pinned})
	return nil
}

// SetPostSolved marks a post answered (or reopens it). Author or Manage
// Messages: a question's asker is exactly who knows when it has been answered,
// and making them beg a moderator for it is how a Q&A board fills with posts
// nobody can tell are finished.
func (s *Service) SetPostSolved(guildID, postID string, solved bool) error {
	if _, _, ok := s.postAndForum(guildID, postID); !ok {
		return fmt.Errorf("app: unknown forum post %s", postID)
	}
	if !s.mayCuratePost(guildID, postID, s.id.Fingerprint()) {
		return fmt.Errorf("app: only the author or a moderator can mark this post answered")
	}
	_, groupID, changed := s.mutateChannel(guildID, postID, func(c *domain.Channel) bool {
		if c.Solved == solved {
			return false
		}
		c.Solved = solved
		return true
	})
	if !changed {
		return nil
	}
	s.publishMeta(groupID, guildMeta{Type: "post_meta", ChannelID: postID, PostSolved: &solved})
	return nil
}

// SetPostLocked closes a post to new messages, or reopens it.
//
// Moderation rather than curation, so it takes Manage Messages and the author
// alone cannot do it: marking your own question answered leaves everyone free to
// keep talking, whereas locking silences other people, and those should not be
// the same button.
func (s *Service) SetPostLocked(guildID, postID string, locked bool) error {
	if _, _, ok := s.postAndForum(guildID, postID); !ok {
		return fmt.Errorf("app: unknown forum post %s", postID)
	}
	if !s.hasPerm(guildID, PermManageMessages) {
		return fmt.Errorf("app: you don't have permission to close posts")
	}
	_, groupID, changed := s.mutateChannel(guildID, postID, func(c *domain.Channel) bool {
		if c.Locked == locked {
			return false
		}
		c.Locked = locked
		return true
	})
	if !changed {
		return nil
	}
	s.publishMeta(groupID, guildMeta{Type: "post_meta", ChannelID: postID, PostLocked: &locked})
	return nil
}

// postIsLocked reports whether this channel is a forum post that has been
// closed. Used on BOTH the send and the receive side: refusing only to send
// would make the lock a suggestion to whoever is running an unmodified client,
// which is precisely the person who did not need convincing.
func (s *Service) postIsLocked(channelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.guilds {
		for _, c := range g.Channels {
			if c.ID == channelID {
				return c.Locked
			}
		}
	}
	return false
}

// applyForumTags adopts a peer's palette replacement. Same permission and the
// same validation as the local path — a remote string is no more trustworthy
// than one we typed, and this one ends up in a CSS colour.
func (s *Service) applyForumTags(guildID, actor, forumID string, tags []domain.ForumTag) {
	if forumID == "" || !s.memberHasPerm(guildID, actor, PermManageChannels) {
		return
	}
	s.mu.RLock()
	isForum := false
	if g, ok := s.guilds[guildID]; ok {
		for _, c := range g.Channels {
			if c.ID == forumID && c.ChannelType() == "forum" {
				isForum = true
			}
		}
	}
	s.mu.RUnlock()
	if !isForum {
		return
	}
	clean := sanitizeForumTags(tags)
	s.mutateChannel(guildID, forumID, func(c *domain.Channel) bool {
		c.ForumTags = clean
		return true
	})
}

// applyPostMeta adopts a peer's change to a post's board state. Each field is
// authorized on its own — a member who may tag their post may not pin it — and
// only the fields the sender actually set are touched, which is why they are
// pointers: a nil PostSolved means "unchanged", not "reopen".
func (s *Service) applyPostMeta(guildID, actor string, m guildMeta) {
	if m.ChannelID == "" {
		return
	}
	if _, _, ok := s.postAndForum(guildID, m.ChannelID); !ok {
		return // not a post of a forum in THIS guild
	}
	curator := s.mayCuratePost(guildID, m.ChannelID, actor)
	mod := s.memberHasPerm(guildID, actor, PermManageMessages)
	var tags []string
	if m.PostTags != nil {
		tags = sanitizePostTags(*m.PostTags)
	}
	s.mutateChannel(guildID, m.ChannelID, func(c *domain.Channel) bool {
		changed := false
		if m.PostTags != nil && curator && !slices.Equal(c.Tags, tags) {
			c.Tags = tags
			changed = true
		}
		if m.PostPinned != nil && mod && c.Pinned != *m.PostPinned {
			c.Pinned = *m.PostPinned
			changed = true
		}
		// Locking is moderation, not curation: an author closing their own thread
		// to everyone else is a different act from marking their question
		// answered, and only the former silences other people.
		if m.PostLocked != nil && mod && c.Locked != *m.PostLocked {
			c.Locked = *m.PostLocked
			changed = true
		}
		if m.PostSolved != nil && curator && c.Solved != *m.PostSolved {
			c.Solved = *m.PostSolved
			changed = true
		}
		return changed
	})
}

// ForumPost is one card on a forum board: the post's own curated state plus the
// metadata derived from its messages. Times are UnixNano, matching
// ChannelView.LastActivity — the forum board already sorts on that field, and
// two time units in one API is a bug waiting for a frontend to find it.
type ForumPost struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Tags   []string `json:"tags"`   // forum-tag IDs; resolve against the forum's palette
	Pinned bool     `json:"pinned"` // sorted first by default
	Solved bool     `json:"solved"`
	// AuthorFingerprint/AuthorName describe the opening message's author. BOTH
	// are empty until that message has synced — render a pending card, not a post
	// by nobody. Resolve the fingerprint against the member roster for the live
	// name and avatar; AuthorName is only the decorative name captured at post
	// time, for when the roster has no entry.
	AuthorFingerprint string `json:"authorFingerprint"`
	AuthorName        string `json:"authorName"`
	// Excerpt is the opening message as one line of plain text, whitespace
	// collapsed and cut at 240 characters. Not markdown: it is a card preview.
	Excerpt string `json:"excerpt"`
	// Replies counts real messages after the opening one — tombstones and system
	// notices excluded, so the number matches what a reader will actually find.
	Replies int `json:"replies"`
	// Created is the opening message's time, 0 if unsynced. LastActivity is when
	// the post last moved at all (including edits), so a card can say "3h ago".
	Created      int64 `json:"created"`
	LastActivity int64 `json:"lastActivity"`
	// Unanswered is the board's headline filter: a question with no reply yet and
	// no answered mark. Computed here so every client agrees what it means.
	Unanswered bool `json:"unanswered"`
}

// ForumBoard is everything a forum board needs, in one round trip: the palette
// to resolve chips against, and the posts. Two calls would let the client render
// a chip whose tag it cannot name yet.
type ForumBoard struct {
	ForumID string            `json:"forumId"`
	Name    string            `json:"name"`
	Topic   string            `json:"topic"`
	Tags    []domain.ForumTag `json:"tags"`
	Posts   []ForumPost       `json:"posts"`
}

// ForumBoard assembles a forum's board. Posts come back pinned-first then
// newest-activity-first — the default order, not the only one: sorting and
// filtering are client-side over this list, since it is already in hand and a
// re-sort should not cost a round trip.
func (s *Service) ForumBoard(guildID, forumID string) (ForumBoard, error) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var forum domain.Channel
	var posts []domain.Channel
	if ok {
		for _, c := range g.Channels {
			switch {
			case c.ID == forumID && c.ChannelType() == "forum":
				forum = c
			case c.Parent == forumID && c.ChannelType() == "thread":
				posts = append(posts, c)
			}
		}
	}
	s.mu.RUnlock()
	if !ok {
		return ForumBoard{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	if forum.ID == "" {
		return ForumBoard{}, fmt.Errorf("app: %s is not a forum channel", forumID)
	}

	ids := make([]string, 0, len(posts))
	for _, p := range posts {
		ids = append(ids, p.ID)
	}
	// One grouped query set for the whole board. Per-post stat calls are what
	// make a busy forum stutter, and this runs on every guild update.
	stats, err := s.store.PostStatsFor(ids)
	if err != nil {
		return ForumBoard{}, err
	}

	out := make([]ForumPost, 0, len(posts))
	for _, p := range posts {
		st := stats[p.ID]
		fp := ForumPost{
			ID: p.ID, Title: p.Name, Tags: p.Tags, Pinned: p.Pinned, Solved: p.Solved,
			AuthorName: st.AuthorName, Excerpt: postExcerpt(st.Opening),
			Replies: st.Replies, Created: st.Created, LastActivity: st.LastAt,
		}
		if len(st.AuthorKey) > 0 {
			fp.AuthorFingerprint = accountFingerprintOf(st.AuthorKey)
			// A name we know from the roster beats the one captured at post time.
			if hinted := s.ProfileName(fp.AuthorFingerprint); hinted != "" {
				fp.AuthorName = hinted
			}
		}
		if fp.Tags == nil {
			fp.Tags = []string{} // a JSON null here is a needless null check per card
		}
		fp.Unanswered = !fp.Solved && fp.Replies == 0
		out = append(out, fp)
	}
	// Pinned first, then most recent activity. A post with no messages yet sorts
	// by 0 and lands last, which is right: there is nothing to read in it.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].LastActivity > out[j].LastActivity
	})
	tags := forum.ForumTags
	if tags == nil {
		tags = []domain.ForumTag{}
	}
	return ForumBoard{ForumID: forum.ID, Name: forum.Name, Topic: forum.Topic, Tags: tags, Posts: out}, nil
}

// postExcerpt renders an opening message as a one-line card preview: whitespace
// collapsed (a card is not a document) and cut on a rune boundary.
//
// It walks at most maxPostExcerpt+1 runes, so a 40 KB opening message costs the
// same as a one-word one. The obvious version —
// strings.Join(strings.Fields(body), " ") then truncate — copies and splits the
// entire body first, once per post, on every board refresh.
func postExcerpt(body string) string {
	out := make([]rune, 0, maxPostExcerpt+1)
	pendingSpace := false
	for _, r := range body {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' {
			pendingSpace = len(out) > 0 // never a leading space
			continue
		}
		if pendingSpace {
			out = append(out, ' ')
			pendingSpace = false
		}
		out = append(out, r)
		if len(out) > maxPostExcerpt {
			return string(out[:maxPostExcerpt]) + "…"
		}
	}
	return string(out)
}
