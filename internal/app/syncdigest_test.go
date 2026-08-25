package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

// These tests drive buildSyncPayload directly — no peers, no MLS, no radio —
// because the interesting decisions ("what does this peer already have" and
// "what fits in 700 KiB") are decisions about data, and answering them without
// a network is what makes them cheap enough to assert on precisely.

// image builds a plausible base64 image data URI of roughly n bytes.
func image(n int) string {
	const head = "data:image/png;base64,"
	return head + strings.Repeat("A", n-len(head))
}

// loadedGuild is a guild carrying every expensive thing a sync payload can
// carry: art on the guild, art on each member, a fat emoji set, a governance
// log, and the small text records.
func loadedGuild(t *testing.T, members int) syncSource {
	t.Helper()
	src := syncSource{
		guild: domain.Guild{
			ID:          "g1",
			Name:        "Hamadan",
			GroupID:     []byte("group"),
			Description: "the long room",
			Icon:        image(400 << 10),
			Banner:      image(300 << 10),
			Channels: []domain.Channel{
				{ID: "c1", GuildID: "g1", Name: "general"},
				{ID: "c2", GuildID: "g1", Name: "offtopic"},
			},
		},
		profiles:   map[string]Profile{},
		categories: []domain.Category{{ID: "cat1", GuildID: "g1", Name: "Rooms"}},
		events:     []domain.Event{{ID: "e1", GuildID: "g1", Title: "Standup", StartUnix: 1000}},
		gifs:       []GuildGif{{ID: "gif1", GuildID: "g1", Name: "shrug", Keys: "k", Subtype: "gif"}},
		stories:    []storyRecord{{StoryID: "s1", GuildID: "g1", Author: "AAAA", Preset: "preset:dusk", PostedAt: 1, ExpiresAt: 99999}},
		selfFpr:    "SELF FPR",
	}
	for i := range members {
		fpr := fmt.Sprintf("MEMBER %04d", i)
		src.profiles[fpr] = Profile{
			Name:   fmt.Sprintf("member-%d", i),
			Avatar: image(60 << 10),
			Banner: image(250 << 10),
		}
	}
	src.profiles[src.selfFpr] = Profile{Name: "me", Avatar: image(60 << 10)}
	for i := range 6 {
		src.emoji = append(src.emoji, domain.CustomEmoji{
			GuildID: "g1", Name: fmt.Sprintf("blob%d", i), Image: image(200 << 10),
		})
	}
	for i := range 40 {
		raw, err := json.Marshal(govOp{Seq: uint64(i), Type: "role_assign", Target: "MEMBER 0001", Add: true, Sig: []byte("sig")})
		if err != nil {
			t.Fatalf("marshal op: %v", err)
		}
		src.govOps = append(src.govOps, raw)
	}
	return src
}

// legacySnapshot is the payload the responder built before digests existed:
// the whole guild, the whole roster, every emoji, the whole op log, appended
// unconditionally, with maxSyncPayload charged against message rows alone. It
// is transcribed from the pre-fix handleSyncRequest so the tests below can put
// a number on what the change is worth.
func legacySnapshot(src syncSource) syncPayload {
	p := syncPayload{
		Guild:      src.guild,
		Profiles:   src.profiles,
		Categories: src.categories,
		Emoji:      src.emoji,
		Gifs:       src.gifs,
		Events:     src.events,
		GovOps:     src.govOps,
		Stories:    src.stories,
		StoryDels:  src.storyDels,
		Messages:   map[string][]domain.Message{},
	}
	budget := maxSyncPayload
	for _, ch := range src.channels {
		for _, m := range ch.rows {
			cost := len(m.Content) + 256
			if budget < cost {
				break
			}
			budget -= cost
			p.Messages[ch.id] = append(p.Messages[ch.id], m)
		}
	}
	return p
}

func marshalledSize(t *testing.T, p syncPayload) int {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return len(raw)
}

// TestIdleSyncCarriesOnlyTheDifference is the headline regression: two peers
// holding identical state must trade almost nothing to establish that. Before
// digests this same guild produced a multi-megabyte answer every sixty seconds,
// in both directions, forever — which is the leak this whole change is about.
func TestIdleSyncCarriesOnlyTheDifference(t *testing.T) {
	src := loadedGuild(t, 12)
	src.reqFpr = "PEER FPR"
	have := sourceDigest(src) // the requester holds exactly what we hold

	payload, truncated := buildSyncPayload(src, have)
	if truncated {
		t.Fatal("an idle sync had to truncate — nothing should have been offered at all")
	}
	got := marshalledSize(t, payload)
	was := marshalledSize(t, legacySnapshot(src))
	t.Logf("idle sync of an unchanged guild: %d bytes, was %d bytes (%.0f×)", got, was, float64(was)/float64(got))

	if got > 4<<10 {
		t.Errorf("an unchanged guild still costs %d bytes to re-establish, want under 4 KiB", got)
	}
	if len(payload.Profiles) != 0 {
		t.Errorf("re-sent %d profiles (avatars and banners) the peer already had", len(payload.Profiles))
	}
	if len(payload.Emoji) != 0 {
		t.Errorf("re-sent %d custom emoji the peer already had", len(payload.Emoji))
	}
	if len(payload.GovOps) != 0 {
		t.Errorf("re-sent %d governance ops the peer already had", len(payload.GovOps))
	}
	if payload.Guild.Icon != "" || payload.Guild.Banner != "" {
		t.Error("re-sent the guild's icon/banner the peer already had")
	}
	if len(payload.Events) != 0 || len(payload.Gifs) != 0 || len(payload.Categories) != 0 || len(payload.Stories) != 0 {
		t.Error("re-sent text records the peer already had")
	}
	// The old shape is what makes the number above meaningful: assert it really
	// was as heavy as claimed, so this test fails if the comparison rots.
	if was < 3<<20 {
		t.Errorf("the pre-digest snapshot for this guild was only %d bytes; the fixture no longer represents the problem", was)
	}
}

// TestSyncOffersOnlyWhatChanged: one member re-skins their profile and one
// emoji is added. Everything else must stay home.
func TestSyncOffersOnlyWhatChanged(t *testing.T) {
	src := loadedGuild(t, 8)
	src.reqFpr = "PEER FPR"
	// Served by the owner: an ordinary member may not relay a profile refresh
	// at all (the requester would refuse it — see the untrusted test below).
	src.selfTrusted = true
	have := sourceDigest(src)

	changed := src.profiles["MEMBER 0003"]
	changed.Avatar = image(50 << 10)
	src.profiles["MEMBER 0003"] = changed
	src.emoji = append(src.emoji, domain.CustomEmoji{GuildID: "g1", Name: "newblob", Image: image(100 << 10)})
	src.guild.Icon = image(200 << 10)

	payload, truncated := buildSyncPayload(src, have)
	if truncated {
		t.Fatal("three changed items did not fit the budget")
	}
	if len(payload.Profiles) != 1 || payload.Profiles["MEMBER 0003"].Avatar != changed.Avatar {
		t.Errorf("expected exactly the changed profile, got %d", len(payload.Profiles))
	}
	if len(payload.Emoji) != 1 || payload.Emoji[0].Name != "newblob" {
		t.Errorf("expected exactly the new emoji, got %d", len(payload.Emoji))
	}
	if payload.Guild.Icon == "" {
		t.Error("the changed guild icon was not offered")
	}
	if payload.Guild.Banner != "" {
		t.Error("the unchanged guild banner tagged along with the changed icon")
	}
}

// TestOldRequesterStillGetsEverything is the compatibility leg: a peer running
// a build that predates digests sends no inventory (Have is nil), and must get
// the same full snapshot it always did.
func TestOldRequesterStillGetsEverything(t *testing.T) {
	src := loadedGuild(t, 2)
	src.reqFpr = "PEER FPR"
	// Small art: the budget applies to legacy requesters too (that is the point
	// of charging the whole payload), and this test is about the filtering.
	src.guild.Icon, src.guild.Banner = image(1<<10), image(1<<10)
	for fpr, p := range src.profiles {
		p.Avatar, p.Banner = image(1<<10), image(1<<10)
		src.profiles[fpr] = p
	}
	for i := range src.emoji {
		src.emoji[i].Image = image(1 << 10)
	}

	payload, _ := buildSyncPayload(src, nil)
	if len(payload.Profiles) != len(src.profiles) {
		t.Errorf("served %d profiles to a legacy requester, want %d", len(payload.Profiles), len(src.profiles))
	}
	if len(payload.Emoji) != len(src.emoji) {
		t.Errorf("served %d emoji to a legacy requester, want %d", len(payload.Emoji), len(src.emoji))
	}
	if len(payload.GovOps) != len(src.govOps) {
		t.Errorf("served %d ops to a legacy requester, want %d", len(payload.GovOps), len(src.govOps))
	}
	if payload.Guild.Icon == "" || payload.Guild.Banner == "" || payload.Guild.Name == "" {
		t.Error("a legacy requester was served a guild stripped of the fields it cannot ask for")
	}
	if len(payload.Categories) == 0 || len(payload.Events) == 0 || len(payload.Gifs) == 0 || len(payload.Stories) == 0 {
		t.Error("a legacy requester was served an incomplete snapshot")
	}
}

// TestPayloadRespectsTheByteBudget: maxSyncPayload used to be charged against
// message rows alone, with images appended before the budget existed. A guild
// like this one produced a "capped" 700 KiB response of about eight megabytes.
func TestPayloadRespectsTheByteBudget(t *testing.T) {
	src := loadedGuild(t, 24)
	src.reqFpr = "PEER FPR"
	src.channels = []syncChannelRows{{id: "c1"}, {id: "c2"}}
	for i := range 20 {
		src.channels[i%2].rows = append(src.channels[i%2].rows, domain.Message{
			ID: fmt.Sprintf("m%d", i), ChannelID: src.channels[i%2].id, Content: image(20 << 10),
		})
	}

	payload, truncated := buildSyncPayload(src, nil)
	size := marshalledSize(t, payload)
	was := marshalledSize(t, legacySnapshot(src))
	t.Logf("stuffed guild: budgeted payload %d bytes, pre-fix payload %d bytes", size, was)

	if size > maxSyncPayload {
		t.Errorf("payload is %d bytes, over the %d-byte budget it claims to respect", size, maxSyncPayload)
	}
	if !truncated {
		t.Error("the payload was cut short but did not say so, so the requester will wait a full beat for the rest")
	}
	if was <= maxSyncPayload {
		t.Errorf("the pre-fix payload for this guild was %d bytes — the fixture no longer overflows", was)
	}
	// Priority: governance and messages are not displaced by cosmetics.
	if len(payload.GovOps) != len(src.govOps) {
		t.Errorf("dropped governance ops (%d of %d) while spending the budget on images", len(payload.GovOps), len(src.govOps))
	}
	if len(payload.Messages["c1"]) == 0 || len(payload.Messages["c2"]) == 0 {
		t.Error("dropped a channel's messages while spending the budget on images")
	}
}

// TestTruncatedSyncConverges: whatever the budget dropped must come back. Each
// round the requester ingests what arrived, its inventory advances, and the
// next answer is the remainder — so a guild too big for one payload still
// converges instead of oscillating between two halves of itself.
func TestTruncatedSyncConverges(t *testing.T) {
	src := loadedGuild(t, 24)
	src.reqFpr = "PEER FPR"

	// The requester starts empty and applies each answer to its own copy.
	mine := syncSource{guild: domain.Guild{ID: "g1"}, profiles: map[string]Profile{}}
	seenOps := map[string]bool{}

	rounds := 0
	for {
		rounds++
		// ~9 MiB of state at 700 KiB a round: the ceiling is generous on
		// purpose, since what is being pinned is that every round makes
		// progress, not how many a particular fixture needs.
		if rounds > 40 {
			t.Fatal("a fully divergent guild never converged")
		}
		have := sourceDigest(mine)
		have.Profiles = digestProfiles(mine.profiles) // never nil: "I hold a row"
		payload, truncated := buildSyncPayload(src, have)
		if size := marshalledSize(t, payload); size > maxSyncPayload {
			t.Fatalf("round %d served %d bytes, over budget", rounds, size)
		}
		// Apply.
		if payload.Guild.Icon != "" {
			mine.guild.Icon = payload.Guild.Icon
		}
		if payload.Guild.Banner != "" {
			mine.guild.Banner = payload.Guild.Banner
		}
		if payload.Guild.Name != "" {
			mine.guild.Name, mine.guild.Description = payload.Guild.Name, payload.Guild.Description
		}
		for fpr, p := range payload.Profiles {
			mine.profiles[fpr] = p
		}
		mine.emoji = append(mine.emoji, payload.Emoji...)
		mine.categories = append(mine.categories, payload.Categories...)
		mine.events = append(mine.events, payload.Events...)
		mine.gifs = append(mine.gifs, payload.Gifs...)
		mine.stories = append(mine.stories, payload.Stories...)
		for _, raw := range payload.GovOps {
			if !seenOps[digestRaw(raw)] {
				seenOps[digestRaw(raw)] = true
				mine.govOps = append(mine.govOps, raw)
			}
		}
		if !truncated {
			break
		}
	}
	if rounds < 2 {
		t.Fatal("the fixture fit in one payload — it no longer tests truncation")
	}
	t.Logf("a %d-member guild with %d emoji converged in %d rounds", len(src.profiles), len(src.emoji), rounds)
	if len(mine.profiles) != len(src.profiles) {
		t.Errorf("converged with %d profiles, want %d", len(mine.profiles), len(src.profiles))
	}
	if len(mine.emoji) != len(src.emoji) {
		t.Errorf("converged with %d emoji, want %d", len(mine.emoji), len(src.emoji))
	}
	if len(mine.govOps) != len(src.govOps) {
		t.Errorf("converged with %d governance ops, want %d", len(mine.govOps), len(src.govOps))
	}
	if mine.guild.Icon != src.guild.Icon || mine.guild.Banner != src.guild.Banner {
		t.Error("the guild's icon and banner never both arrived")
	}
}

// TestUntrustedResponderWithholdsWhatWouldBeRefused: applySyncPayload refuses a
// cached profile overwrite from an ordinary member (it would let a neighbour
// redirect someone's offline mail), so serving one is pure cost. What an
// ordinary member may still serve is a member the requester has never seen, and
// its OWN row — which is how a changed avatar reaches a friend who was offline
// for the announce.
func TestUntrustedResponderWithholdsWhatWouldBeRefused(t *testing.T) {
	src := loadedGuild(t, 4)
	src.reqFpr = "PEER FPR"
	src.selfTrusted = false
	src.profiles["NEWCOMER"] = Profile{Name: "newcomer", Avatar: image(10 << 10)}
	src.profiles[src.reqFpr] = Profile{Name: "the asker", Avatar: image(10 << 10)}

	// The requester holds stale copies of everyone except the newcomer.
	have := sourceDigest(src)
	for fpr := range have.Profiles {
		if fpr != "NEWCOMER" {
			have.Profiles[fpr] = "stale"
		}
	}
	delete(have.Profiles, "NEWCOMER")

	payload, _ := buildSyncPayload(src, have)
	if _, ok := payload.Profiles["NEWCOMER"]; !ok {
		t.Error("withheld a profile the requester has never seen — it would never learn them")
	}
	if _, ok := payload.Profiles[src.selfFpr]; !ok {
		t.Error("withheld our own changed profile, which is the one row an untrusted server may move")
	}
	if _, ok := payload.Profiles[src.reqFpr]; ok {
		t.Error("served the requester its own profile back; only their own devices may move it")
	}
	if _, ok := payload.Profiles["MEMBER 0001"]; ok {
		t.Error("an untrusted server offered a cached-profile overwrite the requester would refuse")
	}

	// A trusted server (owner or SyncHost) may refresh them, and must.
	src.selfTrusted = true
	payload, _ = buildSyncPayload(src, have)
	if _, ok := payload.Profiles["MEMBER 0001"]; !ok {
		t.Error("a trusted server withheld a stale profile refresh")
	}
}

// TestDigestIgnoresNowPlaying: rich presence rides on live announces and is
// never persisted, so it must not make a member's avatar and profile banner
// re-sync every time a track changes.
func TestDigestIgnoresNowPlaying(t *testing.T) {
	p := Profile{Name: "listener", Avatar: image(40 << 10)}
	playing := p
	playing.Activity = &Activity{Title: "Kind of Blue", Artist: "Miles Davis"}
	if digestProfile(p) != digestProfile(playing) {
		t.Error("a now-playing overlay changed a profile's content digest")
	}
	edited := p
	edited.Name = "listener II"
	if digestProfile(p) == digestProfile(edited) {
		t.Error("a real profile edit did not change the digest")
	}
}

// TestSetDigestIgnoresOrder: two peers holding the same records in a different
// order hold the same set, and must not trade them forever to find that out.
func TestSetDigestIgnoresOrder(t *testing.T) {
	a := []domain.Event{{ID: "1", Title: "one"}, {ID: "2", Title: "two"}, {ID: "3", Title: "three"}}
	b := []domain.Event{a[2], a[0], a[1]}
	if digestEvents(a) != digestEvents(b) {
		t.Error("the same events in a different order hashed differently")
	}
	if digestEvents(a) == digestEvents(a[:2]) {
		t.Error("a missing event did not change the set digest")
	}
	if digestEvents(nil) != "" {
		t.Error("an empty set should hash to the empty string, so it costs no wire bytes")
	}
}
