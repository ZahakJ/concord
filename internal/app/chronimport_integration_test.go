package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/chronimport"
)

// TestChatImportIntoLiveGuild is the acceptance test for the whole pipeline, and
// it is deliberately shaped as the thing somebody actually does: they already
// have a guild with a #general in it, they point the importer at a directory of
// exported JSON, and afterwards the OTHER member — who was never in the
// community the history came from, and who never sees the export directory — can
// scroll it.
//
// Everything the offline tests cannot reach is here: the channels and categories
// really get made and really gossip, the manifest really gets signed and
// announced, and the second peer really fetches a page over the chronicle stream
// and reads it back with its authors, stamps and replies intact.
func TestChatImportIntoLiveGuild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping networked integration test in -short mode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)

	g, err := a.CreateGuild("carried over")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	// CHANNELS THAT ALREADY EXIST. Somebody importing their old community into a
	// guild they have already set up has a #general in it — every new guild does
	// — and an importer that made a second one would be telling them their setup
	// was wrong. #Plans is here for the same reason with the case changed on
	// purpose: "Plans" and "plans" are the same room to everybody except a string
	// comparison.
	existing := ""
	for _, ch := range guildChannels(t, a, g.ID) {
		if strings.EqualFold(ch.Name, "general") {
			existing = ch.ID
		}
	}
	if existing == "" {
		t.Fatal("a new guild is supposed to come with a general channel")
	}
	existingPlans, err := a.CreateChannel(g.ID, "Plans", "text", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	code, _ := a.InviteCode(g.ID)
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 20*time.Second, 2, a, b)

	dir := t.TempDir()
	facts, err := chronimport.BuildTestExport(dir)
	if err != nil {
		t.Fatalf("BuildTestExport: %v", err)
	}

	// A member who is not the owner cannot import, however much of the export
	// they hold. The check is the early one; AttachChronicle would refuse too,
	// but not before an hour of reading.
	if _, err := b.ImportChatExport(g.ID, dir, chronimport.DefaultPolicy()); err == nil {
		t.Fatal("a non-owner started an import")
	}
	if info, _ := b.ChronicleInfo(g.ID); info.ID != "" {
		t.Fatal("a refused import left an archive behind at B")
	}

	// Progress has to actually arrive: a twenty-minute operation with no signal
	// is one somebody force-quits.
	var beatMu sync.Mutex
	phases := map[string]bool{}
	var maxDone int64
	a.OnChatImport(func(p ChatImportProgress) {
		beatMu.Lock()
		phases[p.Phase] = true
		if p.Done > maxDone {
			maxDone = p.Done
		}
		beatMu.Unlock()
	})

	jobID, err := a.ImportChatExport(g.ID, dir, chronimport.DefaultPolicy())
	if err != nil {
		t.Fatalf("A ImportChatExport: %v", err)
	}
	if jobID == "" {
		t.Fatal("the import returned no job id")
	}
	// It returns immediately — the whole reason it is a job — so a second call
	// while it runs has to be refused rather than queued.
	if _, err := a.ImportChatExport(g.ID, dir, chronimport.DefaultPolicy()); err == nil {
		t.Fatal("a second import started while the first was running")
	}

	var res *ChatImportResult
	waitUntil(t, 90*time.Second, func() bool {
		st, err := a.ChronicleImportStatus(jobID)
		if err != nil || st.Running {
			return false
		}
		if st.Error != "" {
			t.Fatalf("the import failed: %s", st.Error)
		}
		res = st.Result
		return res != nil
	}, "the import never finished")

	beatMu.Lock()
	for _, want := range []string{PhaseStructure, PhaseReading, PhaseSealing, PhaseAttaching, PhaseDone} {
		if !phases[want] {
			t.Errorf("no progress beat reported phase %q (saw %v)", want, phases)
		}
	}
	if maxDone < chatImportProgressEvery {
		t.Errorf("the furthest progress beat reported %d messages; the reading phase reported nothing",
			maxDone)
	}
	beatMu.Unlock()

	// ---- what the import did ----

	if res.Imported != facts.Messages+1 { // +1: the truncated file's one good line
		t.Fatalf("imported %d messages, the export holds %d", res.Imported, facts.Messages)
	}
	if res.SkippedNotices != facts.Notices {
		t.Fatalf("skipped %d notices, want %d", res.SkippedNotices, facts.Notices)
	}
	if res.ChannelsReused != 2 {
		t.Fatalf("reused %d channels; #general and #Plans were both already there", res.ChannelsReused)
	}
	// The voice room, and the channel out of the truncated file — whose one
	// readable message is kept, because losing a channel over a download that
	// died partway is exactly the trade the tolerant parser exists to avoid.
	if res.ChannelsCreated != 2 {
		t.Fatalf("created %d channels; the voice room and the cut-off one were both new",
			res.ChannelsCreated)
	}
	if res.CategoriesCreated < 1 {
		t.Fatal("the export's categories were not created")
	}
	if res.EmojiImported != 1 {
		t.Fatalf("imported %d custom emoji, want the one the export brought a file for", res.EmojiImported)
	}
	if res.AttachmentsSealed == 0 || res.Placeholders == 0 {
		t.Fatalf("sealed %d files and named %d missing ones; the fixture has both",
			res.AttachmentsSealed, res.Placeholders)
	}

	// ---- the guild's structure, on the importing machine ----

	byName := map[string]string{}
	for _, ch := range guildChannels(t, a, g.ID) {
		byName[strings.ToLower(ch.Name)] = ch.ID
	}
	if byName["general"] != existing {
		t.Fatalf("#general is now channel %q; the existing one (%q) was not reused",
			byName["general"], existing)
	}
	if byName["plans"] != existingPlans.ID {
		t.Fatalf("#plans is now channel %q; #Plans (%q) was not matched past its capital",
			byName["plans"], existingPlans.ID)
	}
	for _, want := range []string{"general", "plans", "lounge"} {
		if byName[want] == "" {
			t.Fatalf("channel %q was not created; the guild has %v", want, byName)
		}
	}
	// One of each, not two.
	counts := map[string]int{}
	for _, ch := range guildChannels(t, a, g.ID) {
		counts[strings.ToLower(ch.Name)]++
	}
	for _, name := range []string{"general", "plans", "lounge"} {
		if counts[name] != 1 {
			t.Fatalf("the guild now has %d channels called %s: %v", counts[name], name, counts)
		}
	}
	cats, _ := a.Categories(g.ID)
	var haveText bool
	for _, c := range cats {
		if c.Name == "Text Channels" {
			haveText = true
		}
	}
	if !haveText {
		t.Fatalf("the export's category was not created; the guild has %+v", cats)
	}
	emoji, _ := a.CustomEmoji(g.ID)
	if len(emoji) != 1 || emoji[0].Name != facts.EmojiSanitized {
		t.Fatalf("the guild's emoji are %+v, want one called %q", emoji, facts.EmojiSanitized)
	}

	// ---- what the other member sees ----

	// The structure travels the ordinary channel-added lane.
	waitUntil(t, 30*time.Second, func() bool {
		names := map[string]bool{}
		for _, ch := range guildChannels(t, b, g.ID) {
			names[strings.ToLower(ch.Name)] = true
		}
		return names["general"] && names["plans"] && names["lounge"]
	}, "the imported channels never reached B")

	// The archive travels as a manifest of a few kilobytes — no pages.
	own, err := a.ChronicleInfo(g.ID)
	if err != nil {
		t.Fatalf("A ChronicleInfo: %v", err)
	}
	if own.ID != res.ChronicleID {
		t.Fatalf("the result names archive %q, the guild holds %q", res.ChronicleID, own.ID)
	}
	if !own.Pinned {
		t.Fatal("the importing device did not pin its own archive; it holds the only copy")
	}
	waitUntil(t, 30*time.Second, func() bool {
		info, _ := b.ChronicleInfo(g.ID)
		return info.ID == own.ID
	}, "B never learned the imported archive")

	atB, _ := b.ChronicleInfo(g.ID)
	if atB.Messages != res.Imported {
		t.Fatalf("B's view says %d messages, the import made %d", atB.Messages, res.Imported)
	}
	if atB.ChunksCached != 0 {
		t.Fatalf("B already holds %d pages; the index has to travel without the archive", atB.ChunksCached)
	}
	// The channel table tells a reader who was never there which room a
	// conversation belongs above.
	var mapped string
	for _, ch := range atB.Channels {
		if ch.ID == facts.GeneralID {
			mapped = ch.Mapped
		}
	}
	if mapped != existing {
		t.Fatalf("the archive maps its general onto %q; the real channel is %q", mapped, existing)
	}

	// ---- B reads a page, fetched from A over the chronicle stream ----

	page, err := b.ChronicleMessages(g.ID, facts.GeneralID, 0, 30, false)
	if err != nil {
		t.Fatalf("B ChronicleMessages: %v", err)
	}
	if len(page) != 30 {
		t.Fatalf("B read %d messages, want 30", len(page))
	}
	if info, _ := b.ChronicleInfo(g.ID); info.ChunksCached == 0 {
		t.Fatal("B read a page without caching it; availability would never spread")
	}

	mine, err := a.ChronicleMessages(g.ID, facts.GeneralID, 0, 30, false)
	if err != nil {
		t.Fatalf("A ChronicleMessages: %v", err)
	}
	for i := range page {
		if page[i].ID != mine[i].ID || page[i].Content != mine[i].Content ||
			page[i].Author != mine[i].Author || page[i].Nano != mine[i].Nano ||
			page[i].ReplyTo != mine[i].ReplyTo {
			t.Fatalf("B's copy of message %d differs from A's:\n%+v\n%+v", i, page[i], mine[i])
		}
	}
	// Real content, not a page of blanks: names resolved from the manifest's
	// table, stamps in reading order, and replies that point somewhere.
	var replies int
	var last int64
	for _, m := range page {
		if m.Author == "" {
			t.Fatalf("an archived message has no author: %+v", m)
		}
		if m.Nano <= 0 || m.Nano < last {
			t.Fatalf("archived messages are not in reading order: %+v", m)
		}
		last = m.Nano
		if m.ReplyTo != "" {
			replies++
		}
	}
	if replies == 0 {
		t.Fatal("no reply survived into the page B read")
	}

	// The oldest end of the archive, which is a different page and therefore a
	// second fetch, and the one that proves the whole thing is reachable rather
	// than just its newest chunk.
	oldest, err := b.ChronicleMessages(g.ID, facts.GeneralID, page[0].Nano, 10, false)
	if err != nil || len(oldest) == 0 {
		t.Fatalf("B read %d older messages (err %v)", len(oldest), err)
	}
	if oldest[len(oldest)-1].Nano >= page[0].Nano {
		t.Fatal("the older page overlaps the newer one")
	}

	// The other channel too, so that a second source channel is proved to be
	// separately addressable rather than everything landing in one bucket.
	plans, err := b.ChronicleMessages(g.ID, facts.PlansID, 0, 5, false)
	if err != nil || len(plans) != 5 {
		t.Fatalf("B read %d messages of the second channel (err %v)", len(plans), err)
	}

	// And an archived picture is an ordinary attachment token, so it goes through
	// the machinery that already exists rather than any of its own.
	var withMedia int
	deep, _ := b.ChronicleMessages(g.ID, facts.GeneralID, 0, 200, false)
	for _, m := range deep {
		for _, tok := range m.Attach {
			if !strings.HasPrefix(tok, "concord://attach/v1/") && !strings.HasPrefix(tok, "concord://file/v1/") {
				t.Fatalf("archived media is not an attachment token: %q", tok)
			}
			withMedia++
		}
	}
	if withMedia == 0 {
		t.Fatal("no archived message carried media")
	}

	// A job whose result is still readable afterwards, which is what a client
	// that was closed when the import finished comes back to.
	if st, err := a.ChronicleImportStatus(""); err != nil || st.Running || st.Result == nil {
		t.Fatalf("the finished job is no longer answerable: %+v (err %v)", st, err)
	}
}

// guildChannels reads a guild's channels off a service.
func guildChannels(t *testing.T, s *Service, guildID string) []struct {
	ID, Name string
} {
	t.Helper()
	var out []struct{ ID, Name string }
	for _, g := range s.Guilds() {
		if g.ID != guildID {
			continue
		}
		for _, ch := range g.Channels {
			if ch.Parent != "" {
				continue
			}
			out = append(out, struct{ ID, Name string }{ch.ID, ch.Name})
		}
	}
	return out
}
