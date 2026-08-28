package app

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ZahakJ/concord/internal/store"
)

// inbox.go answers one question that the app has never been able to answer:
// "what happened while I was away that concerns me?"
//
// Everything here is a query over the local store. Nothing is recorded when a
// message arrives, nothing is transmitted, and nothing new is stored except a
// single "you have looked at this" timestamp on this device. That is why the
// feature exists at all: a mention inbox is the highest-frequency unmet need in
// a busy guild, and this architecture can answer it for free, because the
// messages are already here.
//
// THE PRIVACY POINT, and it is the whole reason keyword alerts live in this
// function rather than anywhere else: the alert words are passed IN by the
// caller on every call and are never written down here. A hosted app that offers
// alert words necessarily learns what you are watching for, because the matching
// has to happen where the messages are. Here the messages are already on your
// machine, so the matching happens on your machine, and the words never reach a
// disk this process wrote or a byte this process sent.

// THE READ MODEL, which is the other thing worth writing down, because there
// are two read systems in the app and an entry has to obey both.
//
// The channel read marks (read_state) are the ones that have always existed:
// "I have read this channel through here". The inbox mark is newer and coarser:
// "I have looked at the inbox as a whole, as of here". An entry is unread only
// when the message is newer than BOTH — its channel's mark and the inbox's.
//
// Neither half is optional. Without the channel mark the entire backlog is born
// unread the day the feature ships, because the inbox's own mark starts at zero
// and every mention you read in the channel months ago is newer than that; the
// badge then claims a number nobody can make go down except by pressing "mark
// all read", which is the app asking you to lie to it. And without the inbox
// mark there would be no way to dismiss an entry in a channel you have not
// caught up on.
//
// It also makes the thing people expect happen for free: opening a DM and
// reading it retires its inbox entry, because reading the DM moved that
// channel's mark past the message. Nothing is written to the inbox to make that
// true — Unread is derived on every query, so the two systems cannot drift.
const inboxReadKey = "inbox_read_at"

// Reasons a message is in your inbox, in the order they win when more than one
// applies. A reply that also names you is a mention: it is the more specific
// claim about intent, and the row can only carry one.
const (
	InboxMention = "mention"
	InboxReply   = "reply"
	InboxKeyword = "keyword"
)

// InboxEntry is one thing that concerns you, resolved for display.
type InboxEntry struct {
	MessageID   string `json:"messageId"`
	ChannelID   string `json:"channelId"`
	ChannelName string `json:"channelName,omitempty"`
	GuildID     string `json:"guildId,omitempty"`
	GuildName   string `json:"guildName,omitempty"`
	IsDM        bool   `json:"isDm,omitempty"`

	Sender     string `json:"sender"`
	SenderName string `json:"senderName,omitempty"`

	Reason string `json:"reason"`
	// Term is the alert word that matched, for a keyword entry, or the role name
	// for a mention that arrived through a role. Empty for a plain @you.
	Term    string `json:"term,omitempty"`
	Snippet string `json:"snippet"`
	At      int64  `json:"at"` // unix milliseconds
	Unread  bool   `json:"unread"`
}

// InboxPage is a page of entries plus the two numbers the badge needs.
type InboxPage struct {
	Entries []InboxEntry `json:"entries"`
	// ReadAt is the inbox mark this device last set, in unix milliseconds. It is
	// only half of what makes an entry unread — see the read model above.
	ReadAt int64 `json:"readAt"`
	// Unread counts the entries in THIS page that came back unread. It is a page
	// count on purpose: a badge that says "50+" is as useful as one that says
	// 4,193 and costs a bounded scan instead of the whole history.
	Unread int `json:"unread"`
}

// Inbox returns what concerns you, newest first, across every guild and DM.
//
// words are this device's alert words. They arrive as an argument and leave as
// nothing: they are lowercased, used to build the scan's substring pre-filter,
// and dropped when the call returns.
//
// beforeNano pages backwards (0 = start at the newest). unreadOnly bounds the
// scan at the inbox mark and drops anything a channel mark has already retired,
// which is what makes the badge refresh cheap once you are caught up — there is
// nothing after the mark to decrypt.
func (s *Service) Inbox(words []string, beforeNano int64, limit int, unreadOnly bool) (InboxPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	readAt := s.inboxReadAt()
	// One row per channel you have ever opened, so this is small and it is
	// fetched once for the whole page rather than per entry.
	chanRead, err := s.store.ReadState()
	if err != nil {
		return InboxPage{}, err
	}

	self := s.id.PublicKey()
	selfName := strings.TrimSpace(s.SelfProfile().Name)

	// The scan's needles: everything that could possibly make a message ours.
	// Over-matching here is harmless — every candidate is re-tested precisely
	// below — while under-matching would silently lose a mention.
	roleNames := s.myRoleNamesByGuild()
	terms := []string{"@everyone", "@here"}
	if selfName != "" {
		terms = append(terms, "@"+selfName)
	}
	seenRole := map[string]bool{}
	for _, names := range roleNames {
		for _, n := range names {
			if n != "" && !seenRole[strings.ToLower(n)] {
				seenRole[strings.ToLower(n)] = true
				terms = append(terms, "@"+n)
			}
		}
	}
	clean := cleanAlertWords(words)
	terms = append(terms, clean...)

	// unreadOnly bounds the scan at the INBOX mark only. Being newer than it is a
	// necessary condition for unread but not a sufficient one — the channel mark
	// is per channel and there is no single floor that covers them all — so this
	// is a valid SQL floor and the precise test still runs per entry below.
	var afterNano int64
	if unreadOnly && readAt > 0 {
		afterNano = readAt * int64(time.Millisecond)
	}

	// The scan is generous, so ask for more rows than we intend to keep: a
	// candidate that fails the precise test costs a page slot otherwise.
	hits, err := s.store.InboxCandidates(self, terms, beforeNano, afterNano, limit*3)
	if err != nil {
		return InboxPage{}, err
	}

	out := make([]InboxEntry, 0, limit)
	for _, h := range hits {
		if len(out) >= limit {
			break
		}
		// Blocking is enforced here rather than at the bridge, where the message
		// list enforces it, because an inbox entry is not a MessageView and
		// never becomes one — there is nothing downstream for visibleViews to
		// catch. Same rule, applied at the one boundary this list has.
		if s.SenderBlocked(h.Sender) {
			continue
		}
		guildID, _ := s.channelGuild(h.ChannelID)
		reason, term := s.inboxReason(h, selfName, roleNames[guildID], clean)
		if reason == "" {
			continue
		}
		e := InboxEntry{
			MessageID: h.ID,
			ChannelID: h.ChannelID,
			GuildID:   guildID,
			Sender:    accountFingerprintOf(h.Sender),
			Reason:    reason,
			Term:      term,
			Snippet:   snippet(h.Content),
			At:        h.Sent.UnixMilli(),
		}
		e.Unread = inboxUnread(e.At, readAt, chanRead[h.ChannelID])
		// The SQL floor above could only bound the scan by the inbox mark, so a
		// caller who asked for unread only can still be handed something the
		// channel mark has since retired. Drop it here, or "unread only" would
		// return read entries and the badge would count them.
		if unreadOnly && !e.Unread {
			continue
		}
		e.SenderName = s.govActorName(guildID, e.Sender)
		if e.SenderName == "" {
			e.SenderName = h.Name
		}
		s.fillInboxPlace(&e)
		out = append(out, e)
	}

	page := InboxPage{Entries: out, ReadAt: readAt}
	for _, e := range out {
		if e.Unread {
			page.Unread++
		}
	}
	return page, nil
}

// inboxUnread is the whole read model in one line: an entry is unread when it
// is newer than the inbox's own mark AND newer than the mark on the channel it
// arrived in. A channel you have never opened has no mark, so everything in it
// is unread, which is the right answer.
func inboxUnread(at, inboxReadAt, channelReadAt int64) bool {
	return at > inboxReadAt && at > channelReadAt
}

// MarkInboxRead moves this device's read mark. atMs of 0 means "now".
// Purely local — nothing about what you have read has ever left this machine,
// and this does not change that (see ModalPrivacy on read receipts).
func (s *Service) MarkInboxRead(atMs int64) error {
	if atMs <= 0 {
		atMs = time.Now().UnixMilli()
	}
	if cur := s.inboxReadAt(); atMs < cur {
		return nil // the mark only ever moves forwards
	}
	return s.store.SetSetting(inboxReadKey, strconv.FormatInt(atMs, 10))
}

func (s *Service) inboxReadAt() int64 {
	raw, err := s.store.GetSetting(inboxReadKey)
	if err != nil || raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// inboxReason applies the real rule to a candidate, and is the only place that
// decides what an inbox entry means.
//
// The order matters. A message that both replies to you and names you is a
// mention; a message that replies to you and happens to contain one of your
// alert words is a reply. The most specific true statement wins, because the
// row has one line to say it in.
func (s *Service) inboxReason(h store.InboxHit, selfName string, myRoles []string, words []string) (string, string) {
	body := h.Content

	// @everyone / @here address every member, so they count without a name to
	// match. They only count from a real member: a guest's message is relayed
	// under the host's key, and letting a guest reach the whole guild by typing
	// a word would be a broadcast nobody authorised. Guests never reach here
	// anyway (the scan is kind = ''), but the rule is written down because the
	// frontend's isMentionOfSelf states it and the two must agree.
	if mentionsWord(body, "everyone") || mentionsWord(body, "here") {
		return InboxMention, ""
	}
	if selfName != "" && mentionsWord(body, selfName) {
		return InboxMention, ""
	}
	// A role you hold, in the guild this message landed in. A role name is not
	// portable: holding "Moderator" in one guild must not light up a message
	// addressed to a different guild's moderators.
	for _, r := range myRoles {
		if r != "" && mentionsWord(body, r) {
			return InboxMention, r
		}
	}
	if h.RepliesToYou {
		// Set by the store, and only when SQL proved the parent is ours.
		return InboxReply, ""
	}
	for _, w := range words {
		if containsWholeWord(body, w) {
			return InboxKeyword, w
		}
	}
	return "", ""
}

// mentionsWord matches "@name" where the match does not run into a longer word.
// It mirrors containsMention in frontend/src/lib/markdown.js — the same trailing
// word-boundary rule, and deliberately no LEADING one, so "hi@ada" still counts
// the way the renderer counts it.
func mentionsWord(body, name string) bool {
	return findBounded(body, "@"+name, false)
}

// containsWholeWord is the alert-word rule: the term has to stand on its own.
// Both edges, unlike a mention, because "cat" must not fire on "concatenate" —
// that is the failure that makes people turn alert words off everywhere else.
func containsWholeWord(body, word string) bool {
	return findBounded(body, word, true)
}

// findBounded scans for a case-insensitive occurrence of needle whose edges are
// not letters or digits. Single pass with no backtracking; it walks forwards
// from each candidate index and never re-examines what it has passed.
func findBounded(body, needle string, leading bool) bool {
	if needle == "" {
		return false
	}
	hay := strings.ToLower(body)
	n := strings.ToLower(needle)
	from := 0
	for {
		i := strings.Index(hay[from:], n)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(n)
		okBefore := !leading || i == 0 || !isWordRune(lastRune(hay[:i]))
		okAfter := end >= len(hay) || !isWordRune(firstRune(hay[end:]))
		if okBefore && okAfter {
			return true
		}
		from = i + 1
	}
}

// A word character for boundary purposes: letters and digits in any script, plus
// the underscore. Unicode-aware on purpose — an Arabic or Cyrillic alert word
// has to have edges too, and a byte-wise test would find one in the middle of a
// word and call it a match.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func lastRune(s string) rune {
	var last rune
	for _, r := range s {
		last = r
	}
	return last
}

// cleanAlertWords normalises the list the caller handed over: trimmed,
// lowercased, deduplicated, length-bounded, and capped in count. The bounds are
// not about trust — the list came from the person running the app — but about
// the scan: every word is a substring pass over every candidate body, so an
// unbounded list is a slow inbox, and a one-character word matches everything.
func cleanAlertWords(words []string) []string {
	const maxWords = 50
	const maxLen = 64
	out := make([]string, 0, len(words))
	seen := map[string]bool{}
	for _, w := range words {
		w = strings.ToLower(strings.TrimSpace(w))
		if len(w) < 2 || len(w) > maxLen || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
		if len(out) >= maxWords {
			break
		}
	}
	return out
}

// myRoleNamesByGuild lists, per guild, the names of the roles this account
// holds there. Roles are per-guild, so the map is too.
func (s *Service) myRoleNamesByGuild() map[string][]string {
	self := s.id.Fingerprint()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]string, len(s.guilds))
	for gid := range s.guilds {
		st, ok := s.govState[gid]
		if !ok {
			continue
		}
		var names []string
		for _, rid := range st.MemberRoles[self] {
			if r, ok := st.Roles[rid]; ok && r.Name != "" {
				names = append(names, r.Name)
			}
		}
		if len(names) > 0 {
			out[gid] = names
		}
	}
	return out
}

// fillInboxPlace names the conversation an entry landed in. A DM has no guild
// name worth printing — "Direct message" is what the reader is looking for.
func (s *Service) fillInboxPlace(e *InboxEntry) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[e.GuildID]
	if !ok {
		return
	}
	e.GuildName = g.Name
	e.IsDM = g.Kind == "dm" || g.Kind == "group"
	for _, c := range g.Channels {
		if c.ID == e.ChannelID {
			e.ChannelName = c.Name
			break
		}
	}
}
