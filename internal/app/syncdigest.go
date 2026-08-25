package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/ZahakJ/concord/internal/domain"
)

// syncdigest.go makes anti-entropy conditional.
//
// The catch-up request used to say only "I am at epoch N and my newest message
// per channel is T". Everything else in the answer — the guild icon and banner,
// EVERY member's avatar and profile banner, every custom emoji, the whole
// governance log, the events, the GIF records — was rebuilt and re-encrypted
// from scratch on every request, whether or not a single byte of it had
// changed. Two idle peers reconciling each other on the sixty-second beat
// therefore traded megabytes an hour to establish that nothing had happened.
//
// So the requester now states what it already HOLDS, as content hashes, and the
// responder leaves out everything that matches. The hashes are over content,
// never timestamps: a peer that re-saved the same avatar has not changed it.
//
// Compatibility runs both ways, which is why the digests ride in one optional
// field rather than a protocol version:
//
//   - an OLD responder does not know the field, ignores it, and answers exactly
//     as it always did; every filter here is an omission, so a full answer is
//     still correct — the applier is idempotent;
//   - a NEW responder receiving an OLD request sees no digests at all (Have is
//     nil) and serves the full snapshot, byte for byte as before.

// digestBytes is how much of each SHA-256 travels. Eight bytes is 2^64 — a
// collision would mean silently believing a peer holds state it does not, and
// at the few-thousand-item scale of a guild the odds of that are nil, while the
// full 32 would put a member roster's worth of hex in every request.
const digestBytes = 8

// digestRaw hashes bytes that are already canonical (a marshalled op).
func digestRaw(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:digestBytes])
}

// digestOf hashes any value through its JSON encoding. Both sides reach it
// through the SAME accessor (s.CustomEmoji, s.govOpsFor, profileRoster…), so
// the encoding is canonical by construction rather than by convention.
func digestOf(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return digestRaw(raw)
}

// setDigest hashes a collection order-independently: two peers holding the same
// rows in a different order hold the same set. An empty set hashes to "", which
// is also what an absent field decodes to, so "I have none" needs no wire bytes.
func setDigest(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:digestBytes])
}

// syncDigest is the requester's inventory. Every field is a hash of what the
// requester already holds, so the responder can answer with the difference.
//
// The three guild fields are separate hashes rather than one, so an icon that
// fits the byte budget and a banner that does not are decided independently —
// with a single combined hash the banner that never fits would keep the icon
// looking stale forever and both would be re-sent on every beat.
type syncDigest struct {
	GuildText   string `json:"guild,omitempty"`  // name + description
	GuildIcon   string `json:"icon,omitempty"`   // ≤512 KiB image
	GuildBanner string `json:"banner,omitempty"` // ≤512 KiB image
	// Profiles is per-member (fingerprint -> hash): one member changing their
	// avatar must not re-ship the other forty. It carries every fingerprint the
	// requester holds a profile for, INCLUDING ones it only knows as a blank
	// name, because its presence is also how the responder learns that an
	// overwrite would be refused (see buildSyncPayload).
	Profiles map[string]string `json:"profiles,omitempty"`
	// Emoji is per-name for the same reason: one added emoji must not re-ship
	// every other 256 KiB image in the set.
	Emoji map[string]string `json:"emoji,omitempty"`
	// The rest are cheap text records, so one hash per collection is enough:
	// when it differs the whole (small) collection travels.
	Categories string `json:"cats,omitempty"`
	Gifs       string `json:"gifs,omitempty"`
	Events     string `json:"events,omitempty"`
	Stories    string `json:"stories,omitempty"`
	// GovOps is per-op: the log is append-only and must converge exactly, so the
	// responder sends the ops we lack rather than the whole log. A hash per op
	// costs ~6% of an op's own size, which is what buys the incremental replay.
	GovOps []string `json:"govOps,omitempty"`
}

// maxDigestOps bounds the op inventory. Past it the oldest ops are left out and
// re-sent (ingest is idempotent and hash-deduped, so that costs bytes, never
// correctness) — a guild with four thousand governance ops is already far
// outside anything this protocol was shaped for.
const maxDigestOps = 4096

// digestProfile hashes a profile's CONTENT. Activity — the now-playing overlay —
// is stripped first: it is ephemeral, never persisted, and travels on live
// announces, so letting it into the hash would make every member's avatar and
// profile banner re-sync each time somebody's music changed track.
func digestProfile(p Profile) string {
	p.Activity = nil
	return digestOf(p)
}

func digestProfiles(m map[string]Profile) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for fpr, p := range m {
		out[fpr] = digestProfile(p)
	}
	return out
}

func digestEmoji(em []domain.CustomEmoji) map[string]string {
	if len(em) == 0 {
		return nil
	}
	out := make(map[string]string, len(em))
	for _, e := range em {
		out[e.Name] = digestOf(e)
	}
	return out
}

func digestCategories(cats []domain.Category) string {
	parts := make([]string, 0, len(cats))
	for _, c := range cats {
		parts = append(parts, digestOf(c))
	}
	return setDigest(parts)
}

func digestGifs(gifs []GuildGif) string {
	parts := make([]string, 0, len(gifs))
	for _, g := range gifs {
		parts = append(parts, digestOf(g))
	}
	return setDigest(parts)
}

func digestEvents(evs []domain.Event) string {
	parts := make([]string, 0, len(evs))
	for _, e := range evs {
		parts = append(parts, digestOf(e))
	}
	return setDigest(parts)
}

// digestStories covers the records and their retractions together: a payload
// carrying both nets out to deleted, so they converge or diverge as one.
func digestStories(recs []storyRecord, dels []storyDelete) string {
	parts := make([]string, 0, len(recs)+len(dels))
	for _, r := range recs {
		parts = append(parts, digestOf(r))
	}
	for _, d := range dels {
		parts = append(parts, digestOf(d))
	}
	return setDigest(parts)
}

func digestOps(ops []json.RawMessage) []string {
	if len(ops) == 0 {
		return nil
	}
	if len(ops) > maxDigestOps {
		ops = ops[len(ops)-maxDigestOps:]
	}
	out := make([]string, 0, len(ops))
	for _, raw := range ops {
		out = append(out, digestRaw(raw))
	}
	return out
}

// guildText is the guild's cheap, non-image profile — the part the applier
// adopts alongside the images.
func guildText(g domain.Guild) string { return g.Name + "\x00" + g.Description }

// sourceDigest hashes one peer's copy of a guild. Requester and responder both
// reach it with a syncSource gathered from the same accessors, so "we hold the
// same thing" is decided by one function rather than by two that have to be
// kept agreeing.
func sourceDigest(src syncSource) *syncDigest {
	return &syncDigest{
		GuildText:   digestOf(guildText(src.guild)),
		GuildIcon:   digestOf(src.guild.Icon),
		GuildBanner: digestOf(src.guild.Banner),
		Profiles:    digestProfiles(src.profiles),
		Emoji:       digestEmoji(src.emoji),
		Categories:  digestCategories(src.categories),
		Gifs:        digestGifs(src.gifs),
		Events:      digestEvents(src.events),
		Stories:     digestStories(src.stories, src.storyDels),
		GovOps:      digestOps(src.govOps),
	}
}

// syncSourceFor gathers this peer's copy of a guild. Messages are left to the
// caller: the responder reads them per channel against the requester's cursor,
// and the requester doesn't hash them at all (the cursor already covers them).
// Stories come back unexpired only — filtered here rather than at the applier,
// so a dead record never spends payload budget — newest first, capped per guild.
func (s *Service) syncSourceFor(guildID string, guild domain.Guild, nowUnix int64) syncSource {
	src := syncSource{
		guild:     guild,
		profiles:  s.profileRoster(),
		govOps:    s.govOpsFor(guildID),
		stories:   s.storiesForSync(guildID, nowUnix),
		storyDels: s.storyDelsForSync(guildID, nowUnix),
		selfFpr:   s.id.Fingerprint(),
	}
	if cats, err := s.store.Categories(guildID); err == nil {
		src.categories = cats
	}
	if emoji, err := s.CustomEmoji(guildID); err == nil {
		src.emoji = emoji
	}
	if gifs, err := s.GuildGifs(guildID); err == nil {
		src.gifs = gifs
	}
	if evs, err := s.store.Events(guildID); err == nil {
		src.events = evs
	}
	return src
}

// syncDigestFor assembles the inventory the requester sends.
func (s *Service) syncDigestFor(guildID string, guild domain.Guild, nowUnix int64) *syncDigest {
	src := s.syncSourceFor(guildID, guild, nowUnix)
	// profileRoster drops members we only know as a blank name; the inventory
	// must not, because an entry here also says "a row exists for this person,
	// and an untrusted overwrite of it would be refused".
	src.profiles = nil
	d := sourceDigest(src)
	d.Profiles = s.profileDigests()
	return d
}

// profileDigests hashes every profile we hold, our own included. Unlike
// profileRoster it does NOT drop members we only know as a blank name: the
// point of an entry here is partly "I already have a row for this person", and
// an untrusted responder may not overwrite such a row whatever is in it.
func (s *Service) profileDigests() map[string]string {
	s.mu.RLock()
	out := make(map[string]string, len(s.profiles)+1)
	for fpr, p := range s.profiles {
		out[fpr] = digestProfile(p)
	}
	s.mu.RUnlock()
	out[s.id.Fingerprint()] = digestProfile(s.selfStoredProfile())
	return out
}

// ---- the byte budget ----

// syncBudget charges the WHOLE payload against maxSyncPayload. It used to be
// charged against message rows alone, with the guild snapshot, the roster, the
// emoji set and the governance log all appended before the budget existed —
// which is how a "capped" 700 KiB response reached several megabytes.
type syncBudget struct {
	left      int
	truncated bool
}

// take charges cost, reporting whether it fit. A refusal marks the payload
// truncated, which becomes syncResponse.More and earns the requester another
// round immediately instead of a sixty-second wait.
func (b *syncBudget) take(cost int) bool {
	if cost > b.left {
		b.truncated = true
		return false
	}
	b.left -= cost
	return true
}

// spend charges something that is not up for negotiation (the guild skeleton).
func (b *syncBudget) spend(cost int) {
	if b.left -= cost; b.left < 0 {
		b.left = 0
	}
}

// jsonCost is what one item will actually cost in the marshalled payload, plus
// a little for the comma and key that carry it. Measuring beats estimating here
// because the expensive items are base64 images whose size is the whole point.
func jsonCost(v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	return len(raw) + 8
}

// syncChannelRows is one channel's changed rows, kept as a slice so the
// per-channel order of the guild (not a map's random walk) decides which
// channel is served first when the budget runs out.
type syncChannelRows struct {
	id   string
	rows []domain.Message
}

// syncSource is everything the responder gathered locally for one guild, before
// the requester's digests and the byte budget decide what actually travels.
// Plain data on purpose: the filtering below is where the interesting decisions
// are, and a plain value lets a test exercise them without a network.
type syncSource struct {
	guild      domain.Guild
	profiles   map[string]Profile
	categories []domain.Category
	emoji      []domain.CustomEmoji
	gifs       []GuildGif
	events     []domain.Event
	govOps     []json.RawMessage
	stories    []storyRecord
	storyDels  []storyDelete
	channels   []syncChannelRows
	// selfFpr is the responder's own account fingerprint, reqFpr the
	// certificate-authenticated fingerprint of whoever asked.
	selfFpr string
	reqFpr  string
	// selfTrusted is whether WE are one of this guild's trusted history sources
	// (owner or a SyncHost holder). It decides whether serving a profile the
	// requester already holds would achieve anything — see below.
	selfTrusted bool
}

// buildSyncPayload assembles one response body: everything the requester does
// not already hold, in priority order, up to maxSyncPayload. It reports whether
// anything was left out.
//
// The order is the answer to "what may a cosmetic image displace?" — nothing.
// Governance goes first because it decides who may do what; then the cheap
// structural records; then messages, which are the point of the application;
// and only then avatars, custom emoji and guild art. Nothing dropped is lost:
// every omission is re-offered on the next request, because the requester's own
// digests and message cursor are what asked for it, and they only advance over
// what it actually ingested.
func buildSyncPayload(src syncSource, have *syncDigest) (syncPayload, bool) {
	b := syncBudget{left: maxSyncPayload}
	out := syncPayload{Guild: src.guild, Messages: map[string][]domain.Message{}}
	// The images come back at the end, if they are wanted and if they fit.
	out.Guild.Icon, out.Guild.Banner = "", ""
	if have != nil && have.GuildText == digestOf(guildText(src.guild)) {
		// The applier only ever adopts non-empty values, so blanking a field
		// the requester already agrees with is exactly "no news" — including to
		// an old applier, which is why this needed no version negotiation.
		out.Guild.Name, out.Guild.Description = "", ""
	}
	// The skeleton — ids, group id, the channel list — is what makes the rest
	// applicable at all, and it is a few hundred bytes. Charged, never dropped.
	b.spend(jsonCost(out.Guild))

	// 1. Governance. The signed log decides roles and bans; it converges
	//    exactly or the guild's authority is a matter of opinion.
	if len(src.govOps) > 0 {
		var haveOps map[string]bool
		if have != nil {
			haveOps = make(map[string]bool, len(have.GovOps))
			for _, h := range have.GovOps {
				haveOps[h] = true
			}
		}
		for _, raw := range src.govOps {
			if haveOps[digestRaw(raw)] {
				continue
			}
			if !b.take(jsonCost(raw)) {
				break
			}
			out.GovOps = append(out.GovOps, raw)
		}
	}

	// 2. The cheap structural records. Each is all-or-nothing against one set
	//    hash: they are text, so a few kilobytes settles the whole collection.
	if have == nil || have.Categories != digestCategories(src.categories) {
		for _, c := range src.categories {
			if !b.take(jsonCost(c)) {
				break
			}
			out.Categories = append(out.Categories, c)
		}
	}
	if have == nil || have.Events != digestEvents(src.events) {
		for _, e := range src.events {
			if !b.take(jsonCost(e)) {
				break
			}
			out.Events = append(out.Events, e)
		}
	}
	if have == nil || have.Gifs != digestGifs(src.gifs) {
		for _, g := range src.gifs {
			if !b.take(jsonCost(g)) {
				break
			}
			out.Gifs = append(out.Gifs, g)
		}
	}
	if have == nil || have.Stories != digestStories(src.stories, src.storyDels) {
		for _, r := range src.stories {
			if !b.take(jsonCost(r)) {
				break
			}
			out.Stories = append(out.Stories, r)
		}
		// Retractions are tiny and must not be starved by the records they
		// retract, or a deleted story comes back on every sync.
		for _, d := range src.storyDels {
			if !b.take(jsonCost(d)) {
				break
			}
			out.StoryDels = append(out.StoryDels, d)
		}
	}

	// 3. Messages, per channel in the guild's own order. A row that doesn't fit
	//    stops THIS channel only — a smaller row in the next one may still fit,
	//    and the cursor means neither is lost.
	for _, ch := range src.channels {
		for _, m := range ch.rows {
			if !b.take(len(m.Content) + 256) {
				break
			}
			out.Messages[ch.id] = append(out.Messages[ch.id], m)
		}
	}

	// 4. Profiles: avatars up to 64 KiB and profile banners up to 256 KiB, so
	//    the heaviest thing in the payload and the reason it needed a budget.
	//    Cheapest first, so a name-only row is never displaced by somebody's
	//    banner, and whatever didn't fit leads the next round.
	type sized struct {
		key  string
		cost int
	}
	var wanted []sized
	for fpr, p := range src.profiles {
		if fpr == src.reqFpr && src.reqFpr != src.selfFpr {
			// Their own profile, served back to them. They will refuse it
			// unless we are another device of their account — only an account's
			// own devices may move its identity — so sending it is pure cost.
			continue
		}
		if have != nil {
			if have.Profiles[fpr] == digestProfile(p) {
				continue // they hold this exact profile already
			}
			if _, known := have.Profiles[fpr]; known && !src.selfTrusted && fpr != src.selfFpr {
				// They already hold a copy, and an untrusted backfill may not
				// overwrite a cached identity (applySyncPayload drops it, in
				// particular to protect MailboxPub). Our own row is the
				// exception the applier makes, and it is the one that matters:
				// it is how a member whose avatar changed while a friend was
				// offline still reaches that friend.
				continue
			}
		}
		wanted = append(wanted, sized{key: fpr, cost: jsonCost(p)})
	}
	sort.Slice(wanted, func(i, j int) bool {
		if wanted[i].cost != wanted[j].cost {
			return wanted[i].cost < wanted[j].cost
		}
		return wanted[i].key < wanted[j].key
	})
	for _, it := range wanted {
		if !b.take(it.cost) {
			break
		}
		if out.Profiles == nil {
			out.Profiles = make(map[string]Profile, len(wanted))
		}
		out.Profiles[it.key] = src.profiles[it.key]
	}

	// 5. Custom emoji — the other unbounded pile of images, same rule.
	wanted = wanted[:0]
	byName := make(map[string]domain.CustomEmoji, len(src.emoji))
	for _, e := range src.emoji {
		byName[e.Name] = e
		if have != nil && have.Emoji[e.Name] == digestOf(e) {
			continue
		}
		wanted = append(wanted, sized{key: e.Name, cost: jsonCost(e)})
	}
	sort.Slice(wanted, func(i, j int) bool {
		if wanted[i].cost != wanted[j].cost {
			return wanted[i].cost < wanted[j].cost
		}
		return wanted[i].key < wanted[j].key
	})
	for _, it := range wanted {
		if !b.take(it.cost) {
			break
		}
		out.Emoji = append(out.Emoji, byName[it.key])
	}

	// 6. The guild's own art, last because it is the most decorative thing here
	//    and the easiest to be a beat late with.
	if src.guild.Icon != "" && (have == nil || have.GuildIcon != digestOf(src.guild.Icon)) {
		if b.take(len(src.guild.Icon) + 32) {
			out.Guild.Icon = src.guild.Icon
		}
	}
	if src.guild.Banner != "" && (have == nil || have.GuildBanner != digestOf(src.guild.Banner)) {
		if b.take(len(src.guild.Banner) + 32) {
			out.Guild.Banner = src.guild.Banner
		}
	}
	return out, b.truncated
}
