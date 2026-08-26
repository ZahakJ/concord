package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/chronimport"
	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/store"
)

// These tests drive the import pipeline WITHOUT a network, which is possible
// because only two steps of it need one: creating channels (which gossips) and
// AttachChronicle (which announces). Everything in between — reading the export,
// sanitizing bodies, sealing blobs, deduplicating authors, building and signing
// the manifest — runs against a bare Service with a real store and a real
// identity, exactly as the chronicle tests next door do. The networked half is
// covered by the integration test in chronimport_integration_test.go.

// importTestService is a Service with a store and an identity and nothing else,
// owning one guild.
func importTestService(t *testing.T) *Service {
	t.Helper()
	id := mustID(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "concord.db"), bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Service{
		store:    st,
		id:       id,
		guilds:   map[string]*domain.Guild{"g1": {ID: "g1", OwnerID: id.PublicKey()}},
		govOps:   map[string][]govOp{},
		govState: map[string]GuildState{},
	}
}

func importFixture(t *testing.T) (string, chronimport.TestExportFacts) {
	t.Helper()
	dir := t.TempDir()
	facts, err := chronimport.BuildTestExport(dir)
	if err != nil {
		t.Fatalf("BuildTestExport: %v", err)
	}
	return dir, facts
}

// testPlan builds the channel plan the structure phase would have built, without
// creating anything — the one part of the pipeline that needs a live guild. Real
// channel ids are stand-ins; nothing downstream of the plan cares what they are
// beyond carrying them into the manifest.
func testPlan(stats *chronimport.Stats, policy chronimport.Policy) *importPlan {
	plan := &importPlan{bySource: map[string]*plannedChannel{}}
	for _, c := range chronimport.IncludedChannels(stats, policy) {
		ctype := chronimport.ChannelTypeOf(c.Type)
		plan.channels = append(plan.channels, plannedChannel{
			source: c, ctype: ctype, mapped: "real-" + c.ID, history: ctype != "voice",
		})
	}
	for i := range plan.channels {
		plan.bySource[plan.channels[i].source.ID] = &plan.channels[i]
	}
	return plan
}

// runImportOffline runs everything but the structure and attach phases and
// returns the accumulator, so a test can look at what was actually archived.
func runImportOffline(t *testing.T, s *Service, dir string, policy chronimport.Policy) (*chronimport.Stats, *importAccumulator) {
	t.Helper()
	stats, err := chronimport.ScanChatExport(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	policy = chronimport.NormalizePolicy(policy)
	acc := &importAccumulator{
		dir: dir, policy: policy, plan: testPlan(stats, policy),
		seen: map[string]int{}, imported: map[string]bool{},
		byChannel: map[string][]chronicleMessage{},
	}
	job := &chatImportJob{id: "job", guildID: "g1", dir: dir}
	if err := s.readChatExport(job, stats, acc, 0); err != nil {
		t.Fatalf("readChatExport: %v", err)
	}
	return stats, acc
}

// TestGzipRatioHoldsUp keeps the estimator's one magic number honest.
//
// GzipRatio is what the whole size projection scales by, and it is the sort of
// constant that gets set once from a measurement and then quietly rots as the
// message shape changes. So it is re-measured here, against real chunks sealed
// by the real builder from a realistic corpus, and the constant has to be within
// a sixth of the truth or this fails and says what the truth now is.
func TestGzipRatioHoldsUp(t *testing.T) {
	s := importTestService(t)
	dir, _ := importFixture(t)
	_, acc := runImportOffline(t, s, dir, chronimport.DefaultPolicy())

	var plain, cipher int64
	var chunks int
	for _, msgs := range acc.byChannel {
		if len(msgs) == 0 {
			continue
		}
		// The plaintext the builder compresses is JSON Lines of exactly these
		// structs, so measuring it means encoding them the same way.
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		for _, m := range msgs {
			if err := enc.Encode(m); err != nil {
				t.Fatal(err)
			}
		}
		plain += int64(buf.Len())

		refs, cts, err := sealChronicleChunks("ch", msgs)
		if err != nil {
			t.Fatalf("sealChronicleChunks: %v", err)
		}
		chunks += len(refs)
		for _, ct := range cts {
			// secretbox adds a fixed 16-byte tag per chunk; the ratio being
			// measured is gzip's, not the sealing's.
			cipher += int64(len(ct)) - 16
		}
	}
	if plain == 0 || cipher == 0 {
		t.Fatal("nothing was sealed; the measurement is meaningless")
	}
	measured := float64(plain) / float64(cipher)
	t.Logf("measured gzip ratio %.2fx over %d chunks (%d plaintext bytes -> %d compressed)",
		measured, chunks, plain, cipher)

	const tolerance = 1.0 / 6.0
	if drift := abs(chronimport.GzipRatio-measured) / measured; drift > tolerance {
		t.Fatalf("GzipRatio is %.2f but chat text now compresses %.2fx (%.0f%% off); "+
			"update the constant in internal/chronimport/estimate.go",
			chronimport.GzipRatio, measured, drift*100)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// TestEstimateTracksTheRealImport is the accuracy contract the wizard's sizing
// panel rests on. The estimate is arithmetic over a scan; this runs the actual
// import over the same fixture and insists the projection was within a fifth.
func TestEstimateTracksTheRealImport(t *testing.T) {
	s := importTestService(t)
	dir, _ := importFixture(t)
	policy := chronimport.DefaultPolicy()

	stats, acc := runImportOffline(t, s, dir, policy)
	est := chronimport.EstimateChatImport(stats, policy)

	// Messages, which the estimate should get almost exactly right with no date
	// filter in play.
	within(t, "messages", float64(est.Messages), float64(acc.count), 0.20)

	// Sealed media.
	within(t, "attachment bytes", float64(est.AttachmentBytes), float64(acc.sealedBytes), 0.20)
	if est.Attachments != acc.sealed64() {
		t.Errorf("projected %d sealed files, sealed %d", est.Attachments, acc.sealed64())
	}
	if est.Placeholders != acc.placeholders {
		t.Errorf("projected %d placeholders, wrote %d", est.Placeholders, acc.placeholders)
	}

	// And the size of the archive itself — the number somebody actually decides
	// on, and the one that goes through the compression estimate.
	raw, chunks, err := s.buildChronicle("g1", "Test Guild, imported today", "",
		acc.manifestChannels(), acc.authors, acc.byChannel, acc.sealed, acc.sealedBytes)
	if err != nil {
		t.Fatalf("buildChronicle: %v", err)
	}
	var chunkBytes int64
	for _, ct := range chunks {
		chunkBytes += int64(len(ct))
	}
	within(t, "chunk bytes", float64(est.ChunkBytes), float64(chunkBytes), 0.20)
	within(t, "manifest bytes", float64(est.ManifestBytes), float64(len(raw)), 0.35)
	within(t, "chunk count", float64(est.Chunks), float64(len(chunks)), 0.20)

	t.Logf("estimate: %d messages / %d chunk bytes / %d chunks / %d manifest bytes",
		est.Messages, est.ChunkBytes, est.Chunks, est.ManifestBytes)
	t.Logf("actual:   %d messages / %d chunk bytes / %d chunks / %d manifest bytes",
		acc.count, chunkBytes, len(chunks), len(raw))

	// The same accuracy has to survive a policy that cuts, not only the generous
	// one — a projection that is only right when nothing is filtered is not much
	// use to a wizard whose whole purpose is filtering.
	half := stats.FirstNano + (stats.LastNano-stats.FirstNano)/2
	cut := chronimport.DefaultPolicy()
	cut.FromNano = half
	cut.MaxAttachmentBytes = 64 << 10

	s2 := importTestService(t)
	_, acc2 := runImportOffline(t, s2, dir, cut)
	est2 := chronimport.EstimateChatImport(stats, cut)
	within(t, "messages (filtered)", float64(est2.Messages), float64(acc2.count), 0.20)
	within(t, "attachment bytes (filtered)", float64(est2.AttachmentBytes), float64(acc2.sealedBytes), 0.20)
	if acc2.count >= acc.count {
		t.Fatal("the filtered import brought as much as the unfiltered one")
	}
}

func within(t *testing.T, what string, projected, actual, tolerance float64) {
	t.Helper()
	if actual == 0 {
		if projected != 0 {
			t.Errorf("%s: projected %.0f, actual 0", what, projected)
		}
		return
	}
	if drift := abs(projected-actual) / actual; drift > tolerance {
		t.Errorf("%s: projected %.0f, actual %.0f — %.0f%% out (tolerance %.0f%%)",
			what, projected, actual, drift*100, tolerance*100)
	}
}

// TestImportedContentIsDefused is the fail-safe test for the sanitizer. The
// fixture plants one message carrying everything the renderer would act on; if
// SanitizeContent stops running, or stops covering one of them, this goes red.
//
// The proof was recorded by making SanitizeContent return its input unchanged:
// this test then failed on the very first assertion (the self-destruct token
// survived), and the change was reverted.
func TestImportedContentIsDefused(t *testing.T) {
	s := importTestService(t)
	dir, f := importFixture(t)
	_, acc := runImportOffline(t, s, dir, chronimport.DefaultPolicy())

	var hostile *chronicleMessage
	for _, msgs := range acc.byChannel {
		for i := range msgs {
			if msgs[i].ID == f.HostileID {
				hostile = &msgs[i]
			}
		}
	}
	if hostile == nil {
		t.Fatalf("the fixture's hostile message %q was not imported", f.HostileID)
	}
	body := hostile.Content

	// THE SELF-DESTRUCT. A past-dated eph token is swept by the client and the
	// message deletes itself out of every member's store within seconds.
	if strings.Contains(strings.ToLower(body), "concord://") {
		t.Fatalf("a concord:// token survived the import: %q", body)
	}
	// A broadcast ping that would light up a guild that did not exist yet.
	if strings.Contains(body, "@everyone") {
		t.Fatalf("@everyone survived the import: %q", body)
	}
	// The renderer's own span placeholders.
	if strings.ContainsAny(body, "\x00\x01") {
		t.Fatalf("a placeholder sentinel survived: %q", body)
	}
	// A bidi override, which can reorder the visible text around it.
	if strings.ContainsAny(body, "‮‭​") {
		t.Fatalf("an invisible formatting character survived: %q", body)
	}
	// The export's markup translated rather than left as noise.
	if strings.Contains(body, "<:") || strings.Contains(body, "<@") || strings.Contains(body, "<t:") {
		t.Fatalf("export markup survived: %q", body)
	}
	if !strings.Contains(body, ":party_parrot:") {
		t.Fatalf("the custom emoji did not become a shortcode: %q", body)
	}
	// And the words are still there — a sanitizer that eats the message is not a
	// sanitizer, it is a deletion.
	if !strings.Contains(body, "look at this") {
		t.Fatalf("the prose was destroyed: %q", body)
	}
	t.Logf("archived body: %q", body)
}

// TestImportSealsWhatItHasAndNamesWhatItDoesNot is the offline stance, checked
// on both sides: a local file within the tier becomes a real attachment token
// backed by a pinned blob, and everything else becomes a line of text saying so.
func TestImportSealsWhatItHasAndNamesWhatItDoesNot(t *testing.T) {
	s := importTestService(t)
	dir, f := importFixture(t)
	_, acc := runImportOffline(t, s, dir, chronimport.DefaultPolicy())

	if acc.sealed == 0 {
		t.Fatal("nothing was sealed; the local media did not import")
	}
	// The remote-only files and the one over the 5 MiB tier.
	wantPlaceholders := f.RemoteAttachments + 1
	if acc.placeholders != wantPlaceholders {
		t.Fatalf("wrote %d placeholders, want %d (the remote-only files plus the oversized one)",
			acc.placeholders, wantPlaceholders)
	}

	var tokens, notes int
	var blobID string
	for _, msgs := range acc.byChannel {
		for _, m := range msgs {
			for _, tok := range m.Attach {
				tokens++
				if !strings.HasPrefix(tok, "concord://attach/v1/") &&
					!strings.HasPrefix(tok, "concord://file/v1/") {
					t.Fatalf("archived a token that is not an attachment reference: %q", tok)
				}
				if blobID == "" {
					blobID = strings.Split(strings.TrimPrefix(tok, "concord://attach/v1/"), "/")[0]
				}
			}
			if strings.Contains(m.Content, "attachment not exported:") {
				notes++
			}
		}
	}
	if int64(tokens) != acc.sealed64() {
		t.Fatalf("%d tokens for %d sealed files", tokens, acc.sealed)
	}
	if notes == 0 {
		t.Fatal("no message said an attachment was missing")
	}
	// The placeholder names the file and its size, because "re-export with
	// assets" is only an informed choice if the cost is stated.
	var sample string
	for _, msgs := range acc.byChannel {
		for _, m := range msgs {
			if i := strings.Index(m.Content, "[attachment not exported:"); i >= 0 {
				sample = m.Content[i:]
			}
		}
	}
	if !strings.Contains(sample, "holiday.jpg") && !strings.Contains(sample, "recording.bin") {
		t.Fatalf("the placeholder does not name the file: %q", sample)
	}
	t.Logf("placeholder reads: %s", sample)

	// PINNED. This device performed the import, so its copy of an archived
	// picture is the original — letting the attachment LRU evict it would
	// destroy the only copy in the world.
	if blobID == "" {
		t.Fatal("no image token was archived, so pinning cannot be checked")
	}
	if !blobIsPinned(t, s, blobID) {
		t.Fatal("an imported blob was left evictable; the archive's own pictures could be swept")
	}
}

// blobIsPinned asks the store directly. Both halves matter: a blob that is not
// there at all would pass a naive "is it evictable" check.
func blobIsPinned(t *testing.T, s *Service, blobID string) bool {
	t.Helper()
	pinned, ok, err := s.store.AttachmentPinned(blobID)
	if err != nil {
		t.Fatalf("AttachmentPinned: %v", err)
	}
	if !ok {
		t.Fatalf("the sealed blob %s is not in the store at all", blobID[:8])
	}
	return pinned
}

// TestImportRepliesReactionsAndAuthors covers the three things that make an
// archive read like a conversation rather than a list.
func TestImportRepliesReactionsAndAuthors(t *testing.T) {
	s := importTestService(t)
	dir, f := importFixture(t)
	_, acc := runImportOffline(t, s, dir, chronimport.DefaultPolicy())

	if len(acc.authors) != f.Authors {
		t.Fatalf("the archive carries %d authors, the export had %d", len(acc.authors), f.Authors)
	}
	// One portrait fits a manifest and one does not; the big one must be dropped
	// rather than carried or mangled, and the name kept either way.
	var withAvatar int
	names := map[string]bool{}
	for _, a := range acc.authors {
		names[a.Name] = true
		if a.Avatar != "" {
			withAvatar++
			if !validImageDataURI(a.Avatar, maxChronicleAvatarBytes) {
				t.Fatalf("author %q carries a portrait the manifest gate refuses", a.Name)
			}
		}
	}
	if withAvatar != 1 {
		t.Fatalf("%d authors carry portraits, want 1 (the other is over the cap or remote)", withAvatar)
	}
	// Nicknames win over account names, which is what a reader of that community
	// would recognise.
	if !names["Ada"] || !names["linus"] {
		t.Fatalf("author names came out as %v", names)
	}

	var replies, reacted int
	for _, msgs := range acc.byChannel {
		byID := map[string]bool{}
		for _, m := range msgs {
			byID[m.ID] = true
		}
		for _, m := range msgs {
			if m.ReplyTo != "" {
				replies++
				if !byID[m.ReplyTo] {
					t.Fatalf("message %q replies to %q, which is not in the archive", m.ID, m.ReplyTo)
				}
			}
			if len(m.Reactions) > 0 {
				reacted++
			}
		}
	}
	if replies == 0 {
		t.Fatal("no reply survived the import")
	}
	if reacted == 0 {
		t.Fatal("no reaction survived the import")
	}

	// A DATE CUT MUST NOT LEAVE DANGLING REPLIES. Every reply whose target was
	// cut has to drop its reference, or a reader scrolls into a quote of
	// nothing.
	half := f.FirstNano + (f.LastNano-f.FirstNano)/2
	p := chronimport.DefaultPolicy()
	p.FromNano = half
	s2 := importTestService(t)
	_, acc2 := runImportOffline(t, s2, dir, p)
	for ch, msgs := range acc2.byChannel {
		byID := map[string]bool{}
		for _, m := range msgs {
			byID[m.ID] = true
		}
		for _, m := range msgs {
			if m.ReplyTo != "" && !byID[m.ReplyTo] {
				t.Fatalf("in %s, message %q kept a reference to %q, which the date range cut",
					ch, m.ID, m.ReplyTo)
			}
		}
	}
}

// TestImportSkipsNoticesAndVoice: an export is a third join-and-leave notices,
// and a voice channel is nothing else. Importing them would mean a reader
// scrolling a decade of history spends most of it on "X joined".
func TestImportSkipsNoticesAndVoice(t *testing.T) {
	s := importTestService(t)
	dir, f := importFixture(t)
	_, acc := runImportOffline(t, s, dir, chronimport.DefaultPolicy())

	if _, ok := acc.byChannel[f.VoiceID]; ok {
		t.Fatal("the voice channel carried history into the archive")
	}
	for _, msgs := range acc.byChannel {
		for _, m := range msgs {
			if strings.Contains(m.Content, "pinned a message to this channel") {
				t.Fatalf("a system notice was imported as conversation: %q", m.Content)
			}
		}
	}
	// And the messages that did come are the conversational ones, plus the one
	// readable line out of the truncated file.
	if acc.count != f.Messages+1 {
		t.Fatalf("imported %d messages, the export had %d conversational ones", acc.count, f.Messages)
	}
}

// TestImportedArchiveRoundTrips proves the output is a real chronicle: it
// verifies, it opens, and the messages come back with their authors, stamps and
// replies intact — which is what every member will do to it.
func TestImportedArchiveRoundTrips(t *testing.T) {
	s := importTestService(t)
	dir, _ := importFixture(t)
	_, acc := runImportOffline(t, s, dir, chronimport.DefaultPolicy())

	raw, chunks, err := s.buildChronicle("g1", "Test Guild, imported today", "a decade of it",
		acc.manifestChannels(), acc.authors, acc.byChannel, acc.sealed, acc.sealedBytes)
	if err != nil {
		t.Fatalf("buildChronicle: %v", err)
	}
	m, err := decodeChronicleManifest(raw)
	if err != nil {
		t.Fatalf("the archive this importer built does not verify: %v", err)
	}
	if m.Messages != acc.count {
		t.Fatalf("the manifest claims %d messages, the import made %d", m.Messages, acc.count)
	}
	// The channel table records where each source channel landed, so a reader
	// who was never in that community is told which room they are looking at.
	for _, c := range m.Channels {
		if c.Mapped == "" {
			t.Fatalf("channel %q was archived without a mapping", c.Name)
		}
	}

	// Open every page and check the author indices resolve — the one thing the
	// manifest's own validation deliberately does not check, because it does not
	// carry the messages that hold them.
	var seen int64
	for _, ref := range m.Chunks {
		msgs, err := openChronicleChunk(chunks[ref.ID], ref.Keys)
		if err != nil {
			t.Fatalf("page %s does not open: %v", ref.ID[:8], err)
		}
		var last int64
		for _, msg := range msgs {
			seen++
			if msg.Author < 0 || msg.Author >= len(m.Authors) {
				t.Fatalf("message %q names author %d of %d", msg.ID, msg.Author, len(m.Authors))
			}
			if msg.Nano < last {
				t.Fatalf("page %s is out of time order", ref.ID[:8])
			}
			last = msg.Nano
		}
		if len(msgs) != ref.Count {
			t.Fatalf("page %s holds %d messages, the index says %d", ref.ID[:8], len(msgs), ref.Count)
		}
	}
	if seen != acc.count {
		t.Fatalf("the pages hold %d messages, the import made %d", seen, acc.count)
	}
}

// TestImportRefusedForNonOwner is the fail-safe test for the owner gate. The
// proof was recorded by deleting the IsGuildOwner check at the top of
// ImportChatExport: this test then failed ("a non-owner started an import"), and
// the check was put back.
func TestImportRefusedForNonOwner(t *testing.T) {
	s := importTestService(t)
	dir, _ := importFixture(t)

	// Hand the crown to somebody else. Nothing about the caller changes: they
	// still hold the files and still hold the key they signed with, which is
	// exactly the situation the gate exists for.
	other := mustID(t)
	s.guilds["g1"] = &domain.Guild{ID: "g1", OwnerID: other.PublicKey()}

	if _, err := s.ImportChatExport("g1", dir, chronimport.DefaultPolicy()); err == nil {
		t.Fatal("a non-owner started an import")
	}
	if _, err := s.ImportChatExport("nope", dir, chronimport.DefaultPolicy()); err == nil {
		t.Fatal("an import into an unknown guild was accepted")
	}
	if st, _ := s.ChronicleImportStatus(""); st.JobID != "" {
		t.Fatalf("a refused import left a job behind: %+v", st)
	}
}

// TestOnlyOneImportAtATime: two imports racing would create the same channel
// twice and leave the guild with two of everything.
func TestOnlyOneImportAtATime(t *testing.T) {
	s := importTestService(t)
	dir, _ := importFixture(t)

	// Stand a job up directly rather than starting a real one, so the test is
	// about the gate and not about a race with a goroutine finishing.
	s.importMu.Lock()
	s.importJob = &chatImportJob{id: "already", guildID: "g1", running: true, started: time.Now()}
	s.importMu.Unlock()

	_, err := s.ImportChatExport("g1", dir, chronimport.DefaultPolicy())
	if err == nil {
		t.Fatal("a second import started while one was running")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Fatalf("the refusal does not say why: %v", err)
	}

	st, err := s.ChronicleImportStatus("")
	if err != nil || st.JobID != "already" || !st.Running {
		t.Fatalf("status of the running job: %+v (err %v)", st, err)
	}
	if _, err := s.ChronicleImportStatus("some-other-id"); err == nil {
		t.Fatal("status answered for a job id that does not exist")
	}
}

// TestScanIsCachedButNotStale: the wizard re-estimates on every slider drag, so
// the scan has to be cached — and a directory that changed underneath has to be
// re-read, or the sizing panel describes an export that is no longer there.
func TestScanIsCachedButNotStale(t *testing.T) {
	s := importTestService(t)
	dir, _ := importFixture(t)

	first, err := s.ScanChatExport(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ScanChatExport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("the second scan of an unchanged directory re-read it")
	}

	// Change the directory and the cache has to let go.
	extra := `{"guild":{"name":"Test Guild"},"channel":{"id":"c-new","name":"new"},"messages":[` +
		`{"id":"n1","type":"Default","timestamp":"2020-01-01T00:00:00Z","content":"hello",` +
		`"author":{"id":"u9","name":"newcomer"}}]}`
	if err := os.WriteFile(filepath.Join(dir, "new.json"), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := s.ScanChatExport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if third == second {
		t.Fatal("a changed directory served a stale scan")
	}
	if third.Messages != second.Messages+1 {
		t.Fatalf("the re-scan found %d messages, want %d", third.Messages, second.Messages+1)
	}

	if _, err := s.ScanChatExport(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Fatal("scanning a missing directory succeeded")
	}
}

// TestImportRefusesWhatTheFormatCannotHold: an index over its own ceiling has to
// be refused BEFORE the reading starts. Discovering it at the end means an hour
// of work thrown away and no useful advice.
func TestImportRefusesWhatTheFormatCannotHold(t *testing.T) {
	s := importTestService(t)
	dir := t.TempDir()

	// An export with far more channels than a manifest can index. Each is tiny;
	// it is the channel and chunk COUNT that blows the ceiling.
	for i := 0; i < 700; i++ {
		body := fmt.Sprintf(`{"guild":{"name":"Test Guild"},`+
			`"channel":{"id":"c-%04d","name":"channel-%04d","type":"GuildTextChat"},"messages":[`+
			`{"id":"m-%04d","type":"Default","timestamp":"2020-01-01T00:00:00Z",`+
			`"content":"hello there","author":{"id":"u1","name":"ada"}}]}`, i, i, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("c%04d.json", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := chronimport.ScanChatExport(dir)
	if err != nil {
		t.Fatal(err)
	}
	est := chronimport.EstimateChatImport(stats, chronimport.DefaultPolicy())
	if !est.ChannelsOverCap {
		t.Fatalf("700 channels did not trip the %d-channel ceiling", maxChronicleChannels)
	}

	job := &chatImportJob{id: "j", guildID: "g1", dir: dir}
	_, err = s.importChatExport(job, chronimport.DefaultPolicy())
	if err == nil {
		t.Fatal("an import too big for its own index was accepted")
	}
	if !strings.Contains(err.Error(), "channels") {
		t.Fatalf("the refusal does not explain itself: %v", err)
	}
	// The refusal has to come BEFORE anything is read: this is the whole point,
	// since discovering it at the end costs an hour and teaches nothing.
	if job.phase != PhaseScanning && job.phase != PhaseStructure {
		t.Fatalf("the import got as far as %q before refusing", job.phase)
	}
}

// TestEstimatorAgreesWithTheAppOnTheManifestCap: the pure package duplicates the
// manifest ceiling because it must not depend on the service. Duplicated
// constants drift, so this is the thing that stops it.
func TestEstimatorAgreesWithTheAppOnTheManifestCap(t *testing.T) {
	if got := chronimport.MaxManifestBytes; got != maxChronicleManifestBytes {
		t.Fatalf("chronimport says a manifest caps at %d, app says %d", got, maxChronicleManifestBytes)
	}
}

// TestImportedNamesAreBounded: channel and author names come out of a file
// somebody else wrote, and the live create path has no cap of its own.
func TestImportedNamesAreBounded(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("wide", 200)
	body := fmt.Sprintf(`{"guild":{"name":%q},`+
		`"channel":{"id":"c-1","name":%q,"type":"GuildTextChat","category":%q},"messages":[`+
		`{"id":"m1","type":"Default","timestamp":"2020-01-01T00:00:00Z","content":"hi",`+
		`"author":{"id":"u1","name":%q}}]}`, long, long, long, long)
	if err := os.WriteFile(filepath.Join(dir, "wide.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	s := importTestService(t)
	_, acc := runImportOffline(t, s, dir, chronimport.DefaultPolicy())
	if len(acc.authors) != 1 {
		t.Fatalf("got %d authors", len(acc.authors))
	}
	if n := len([]rune(acc.authors[0].Name)); n > maxChronicleNameLen {
		t.Fatalf("an author name came through at %d runes", n)
	}
	chans := acc.manifestChannels()
	if len(chans) != 1 {
		t.Fatalf("got %d channels", len(chans))
	}
	if len(chans[0].Name) > maxChronicleNameLen {
		t.Fatalf("a channel name came through at %d bytes", len(chans[0].Name))
	}
	// And the whole thing still passes the manifest's own gate, which is the
	// real test: an unbounded name would fail validation at signing time.
	if _, _, err := s.buildChronicle("g1", "wide", "", chans, acc.authors, acc.byChannel, 0, 0); err != nil {
		t.Fatalf("an archive of wide names did not build: %v", err)
	}
}
