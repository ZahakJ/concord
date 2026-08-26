package chronimport

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func buildFixture(t *testing.T) (string, TestExportFacts) {
	t.Helper()
	dir := t.TempDir()
	facts, err := BuildTestExport(dir)
	if err != nil {
		t.Fatalf("BuildTestExport: %v", err)
	}
	return dir, facts
}

// TestScanCountsTheExport is the correctness floor: every headline number the
// wizard shows has to be the export's own arithmetic, and the fixture reports
// what it wrote so the two can be compared rather than eyeballed.
func TestScanCountsTheExport(t *testing.T) {
	dir, f := buildFixture(t)
	st, err := ScanChatExport(dir)
	if err != nil {
		t.Fatalf("ScanChatExport: %v", err)
	}

	if st.Guild != f.Guild {
		t.Fatalf("guild = %q, want %q", st.Guild, f.Guild)
	}
	// The truncated file contributes one readable message before it dies, so the
	// scan's total is the fixture's plus that one.
	if st.Messages != f.Messages+1 {
		t.Fatalf("scanned %d messages, the fixture wrote %d (+1 from the truncated file)", st.Messages, f.Messages)
	}
	if st.Notices != f.Notices {
		t.Fatalf("scanned %d notices, want %d", st.Notices, f.Notices)
	}
	if st.Replies != f.Replies {
		t.Fatalf("scanned %d replies, want %d", st.Replies, f.Replies)
	}
	if st.Authors != f.Authors {
		t.Fatalf("scanned %d authors, want %d", st.Authors, f.Authors)
	}
	if st.LocalAttachments != f.LocalAttachments || st.LocalAttachmentBytes != f.LocalAttachmentBytes {
		t.Fatalf("local media: %d files / %d bytes, want %d / %d",
			st.LocalAttachments, st.LocalAttachmentBytes, f.LocalAttachments, f.LocalAttachmentBytes)
	}
	// THE POINT OF COUNTING WHAT IS NOT THERE. A remote-only attachment carries
	// its recorded size, and reporting it is the only way somebody can be told
	// what re-exporting with assets would get them.
	if st.RemoteAttachments != f.RemoteAttachments || st.RemoteAttachmentBytes != f.RemoteAttachmentBytes {
		t.Fatalf("remote media: %d files / %d bytes, want %d / %d",
			st.RemoteAttachments, st.RemoteAttachmentBytes, f.RemoteAttachments, f.RemoteAttachmentBytes)
	}
	if st.FirstNano != f.FirstNano || st.LastNano != f.LastNano {
		t.Fatalf("date range %d..%d, want %d..%d", st.FirstNano, st.LastNano, f.FirstNano, f.LastNano)
	}

	if len(st.Channels) != 4 { // three real channels plus the truncated file's
		t.Fatalf("found %d channels, want 4", len(st.Channels))
	}
	gen, ok := st.ChannelByID(f.GeneralID)
	if !ok {
		t.Fatal("the general channel is missing from the scan")
	}
	if gen.Name != "general" || gen.Category != "Text Channels" || gen.Topic != "everything else" {
		t.Fatalf("general's header did not survive the scan: %+v", gen)
	}
	if len(gen.Months) < 12 {
		t.Fatalf("general spans %d month buckets; the date slider has nothing to work with", len(gen.Months))
	}

	// The histogram has to account for every attachment, local and remote alike.
	var histTotal int64
	for _, n := range st.Histogram {
		histTotal += n
	}
	if histTotal != st.Attachments {
		t.Fatalf("histogram counts %d attachments, the scan found %d", histTotal, st.Attachments)
	}
	if st.Histogram[ClassLarge] != 1 {
		t.Fatalf("histogram's >5MiB bucket holds %d, want the one oversized file", st.Histogram[ClassLarge])
	}

	// The custom emoji, with its unusable name folded to a usable one.
	if len(st.Emoji) != 1 {
		t.Fatalf("found %d custom emoji, want 1: %+v", len(st.Emoji), st.Emoji)
	}
	if st.Emoji[0].Sanitized != f.EmojiSanitized || !st.Emoji[0].Local {
		t.Fatalf("emoji = %+v, want %q and a local image", st.Emoji[0], f.EmojiSanitized)
	}

	// Determinism, which is what makes the cache key sound.
	again, err := ScanChatExport(dir)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if again.Key != st.Key || again.Messages != st.Messages || len(again.Channels) != len(st.Channels) {
		t.Fatal("two scans of an unchanged directory disagreed")
	}
	if again.TopAuthors[0].Name != st.TopAuthors[0].Name {
		t.Fatal("the author ranking is not deterministic")
	}
}

// TestMalformedFileIsCountedNotFatal is the tolerance contract. An export is a
// directory of files somebody else's tool wrote, and one of them being broken
// must cost that file and nothing else — the alternative is a decade of history
// that will not import because of a disk that filled up in 2021.
func TestMalformedFileIsCountedNotFatal(t *testing.T) {
	dir, f := buildFixture(t)

	// A file that is not JSON at all, on top of the truncated one.
	if err := os.WriteFile(filepath.Join(dir, "garbage.json"), []byte("not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	// And one whose messages are present but individually unusable: no id in
	// the first, an unparseable stamp in the second.
	bad := `{"guild":{"name":"Test Guild"},"channel":{"id":"c-bad","name":"bad"},"messages":[` +
		`{"type":"Default","timestamp":"2019-01-01T00:00:00Z","content":"no id"},` +
		`{"id":"b2","type":"Default","timestamp":"whenever","content":"no stamp"},` +
		`{"id":"b3","type":"Default","timestamp":"2019-01-01T00:00:00Z","content":"fine"}]}`
	if err := os.WriteFile(filepath.Join(dir, "partial.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := ScanChatExport(dir)
	if err != nil {
		t.Fatalf("a directory with three broken files must still scan: %v", err)
	}
	if st.Messages != f.Messages+2 { // the truncated file's one, plus "fine"
		t.Fatalf("scanned %d messages, want %d", st.Messages, f.Messages+2)
	}
	if st.Malformed != 2 {
		t.Fatalf("counted %d malformed messages, want 2", st.Malformed)
	}
	if len(st.FileErrors) != 2 {
		t.Fatalf("reported %d unreadable files, want 2 (the truncated one and the garbage): %+v",
			len(st.FileErrors), st.FileErrors)
	}
	// The file that failed is named, not just counted.
	var named []string
	for _, fe := range st.FileErrors {
		named = append(named, fe.File)
	}
	if !contains(named, f.BadFile) || !contains(named, "garbage.json") {
		t.Fatalf("unreadable files reported as %v", named)
	}
}

// TestScanStreamsRatherThanLoads is the memory bound, and it is the reason the
// walker is written the way it is: a real export file runs to hundreds of
// megabytes and a phone has to survive one.
//
// Fifty megabytes in one file, and the scan must not hold it. The bound is
// generous — sixteen megabytes of heap growth against a fifty-megabyte file
// would still prove a streaming read, and being generous is what keeps the test
// from failing on an unlucky GC schedule rather than on a real regression.
func TestScanStreamsRatherThanLoads(t *testing.T) {
	dir := t.TempDir()
	writeBigExport(t, filepath.Join(dir, "big.json"), 50<<20)

	fi, err := os.Stat(filepath.Join(dir, "big.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() < 45<<20 {
		t.Fatalf("the generated file is %d bytes; this test needs a big one", fi.Size())
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	st, err := ScanChatExport(dir)
	if err != nil {
		t.Fatalf("ScanChatExport: %v", err)
	}

	// Measured with the result still live, so what the scan legitimately keeps
	// (the report) counts against the budget too.
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(st)

	grew := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	const budget = 16 << 20
	if grew > budget {
		t.Fatalf("scanning a %d MiB file grew the heap by %d MiB; the walk is not streaming",
			fi.Size()>>20, grew>>20)
	}
	if st.Messages == 0 {
		t.Fatal("the big file scanned to nothing")
	}
	t.Logf("scanned %d messages from %d MiB; heap grew %d KiB",
		st.Messages, fi.Size()>>20, grew>>10)
}

// writeBigExport streams a large export file to disk without building it in
// memory — the test's own generator has to obey the same rule it is testing.
func writeBigExport(t *testing.T, path string, target int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := fmt.Fprint(f, `{"guild":{"id":"g","name":"Test Guild"},`+
		`"channel":{"id":"c-big","name":"big","type":"GuildTextChat"},"messages":[`); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 45) // ~2 KiB
	base := time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)
	var written int64
	for i := 0; written < target; i++ {
		sep := ","
		if i == 0 {
			sep = ""
		}
		n, err := fmt.Fprintf(f, `%s{"id":"b%08d","type":"Default","timestamp":%q,`+
			`"content":%q,"author":{"id":"u%d","name":"person%d"},"attachments":[],"reactions":[]}`,
			sep, i, base.Add(time.Duration(i)*time.Minute).Format(time.RFC3339Nano),
			body, i%5, i%5)
		if err != nil {
			t.Fatal(err)
		}
		written += int64(n)
	}
	if _, err := fmt.Fprint(f, `]}`); err != nil {
		t.Fatal(err)
	}
}

// TestPolicyFiltersChangeTheOutput proves each knob does something. A policy
// control that quietly does nothing is worse than one that is missing: the user
// believes they excluded the channel.
func TestPolicyFiltersChangeTheOutput(t *testing.T) {
	dir, f := buildFixture(t)
	st, err := ScanChatExport(dir)
	if err != nil {
		t.Fatal(err)
	}
	all := EstimateChatImport(st, DefaultPolicy())
	if all.Messages == 0 {
		t.Fatal("the generous policy imports nothing")
	}

	// A voice channel carries no history whatever the policy says.
	for _, c := range all.Channels {
		if c.ID == f.VoiceID {
			t.Fatal("the voice channel was projected to carry messages")
		}
	}

	t.Run("channel exclusion", func(t *testing.T) {
		p := DefaultPolicy()
		p.ExcludeChannels = []string{f.PlansID}
		got := EstimateChatImport(st, p)
		if got.Messages >= all.Messages {
			t.Fatalf("excluding a channel did not reduce the import: %d vs %d", got.Messages, all.Messages)
		}
		for _, c := range got.Channels {
			if c.ID == f.PlansID {
				t.Fatal("the excluded channel is still in the projection")
			}
		}
		if got.SkippedByPolicy <= all.SkippedByPolicy {
			t.Fatal("the excluded messages were not counted as skipped")
		}
	})

	t.Run("date range", func(t *testing.T) {
		half := f.FirstNano + (f.LastNano-f.FirstNano)/2
		p := DefaultPolicy()
		p.FromNano = half
		got := EstimateChatImport(st, p)
		if got.Messages >= all.Messages {
			t.Fatalf("a half-open date range did not reduce the import: %d vs %d", got.Messages, all.Messages)
		}
		// Half the span, so roughly half the messages — the fixture spreads them
		// evenly, which is the case the month proration is meant to get right.
		ratio := float64(got.Messages) / float64(all.Messages)
		if ratio < 0.35 || ratio > 0.65 {
			t.Fatalf("half the date range projected %.0f%% of the messages", ratio*100)
		}
		// And a range that excludes everything imports nothing.
		p = DefaultPolicy()
		p.ToNano = f.FirstNano - 1
		if empty := EstimateChatImport(st, p); empty.Messages != 0 {
			t.Fatalf("a range before the first message projected %d messages", empty.Messages)
		}
	})

	t.Run("attachment tier", func(t *testing.T) {
		// The fixture's local media is a pile of 4 KiB pictures plus one 6 MiB
		// recording, which is what makes the ceiling testable at three points.
		//
		// THE SHARP EDGE IS THE POINT. The default 5 MiB ceiling has to exclude
		// the recording exactly — this is the case that caught the histogram
		// being an octave out, where a power-of-two bucket admitted a quarter of
		// a file that was wholly over the limit.
		noCeiling := DefaultPolicy()
		noCeiling.MaxAttachmentBytes = 0
		open := EstimateChatImport(st, noCeiling)
		if open.AttachmentBytes != f.LocalAttachmentBytes {
			t.Fatalf("with no ceiling the projection is %d bytes, the export holds %d",
				open.AttachmentBytes, f.LocalAttachmentBytes)
		}

		if all.AttachmentBytes != f.LocalAttachmentBytes-f.OversizeBytes {
			t.Fatalf("a 5 MiB ceiling projected %d bytes; the export's local media is %d "+
				"of which one file is %d and over the line",
				all.AttachmentBytes, f.LocalAttachmentBytes, f.OversizeBytes)
		}
		if all.Placeholders != open.Placeholders+1 {
			t.Fatalf("the excluded file was not counted as a placeholder: %d vs %d",
				all.Placeholders, open.Placeholders)
		}

		// Below every file in the export: nothing is sealed, everything is named.
		low := DefaultPolicy()
		low.MaxAttachmentBytes = 1024
		got := EstimateChatImport(st, low)
		if got.AttachmentBytes != 0 {
			t.Fatalf("a 1 KiB ceiling still projected %d bytes", got.AttachmentBytes)
		}
		if got.Placeholders != f.LocalAttachments+f.RemoteAttachments {
			t.Fatalf("projected %d placeholders, want every attachment in the export (%d)",
				got.Placeholders, f.LocalAttachments+f.RemoteAttachments)
		}

		// Turning images off leaves only the recording, which is neither an image
		// nor a video.
		noImages := noCeiling
		noImages.IncludeImages = false
		none := EstimateChatImport(st, noImages)
		if none.AttachmentBytes != f.OversizeBytes {
			t.Fatalf("images off projected %d bytes, want the %d-byte recording alone",
				none.AttachmentBytes, f.OversizeBytes)
		}

		// And skipping media altogether seals none of it while keeping every word.
		off := DefaultPolicy()
		off.IncludeImages, off.IncludeVideo, off.IncludeOther = false, false, false
		skipped := EstimateChatImport(st, off)
		if skipped.Attachments != 0 || skipped.AttachmentBytes != 0 {
			t.Fatalf("attachments off still projected %d files / %d bytes",
				skipped.Attachments, skipped.AttachmentBytes)
		}
		if skipped.Messages != all.Messages {
			t.Fatal("turning attachments off lost messages")
		}
	})

	t.Run("reactions and emoji", func(t *testing.T) {
		p := DefaultPolicy()
		p.IncludeReactions, p.IncludeEmoji = false, false
		got := EstimateChatImport(st, p)
		if got.Emoji != 0 {
			t.Fatalf("emoji off still projected %d", got.Emoji)
		}
		if got.ChunkBytes >= all.ChunkBytes {
			t.Fatalf("dropping reactions did not shrink the archive: %d vs %d",
				got.ChunkBytes, all.ChunkBytes)
		}
		if all.Emoji != 1 {
			t.Fatalf("the generous policy projected %d emoji, want 1", all.Emoji)
		}
	})
}

// TestEstimateIsPure guards the property the wizard depends on: calling it does
// not change the stats it was handed, so a hundred re-estimates during a slider
// drag all see the same world.
func TestEstimateIsPure(t *testing.T) {
	dir, _ := buildFixture(t)
	st, err := ScanChatExport(dir)
	if err != nil {
		t.Fatal(err)
	}
	before := *st
	beforeChannels := append([]ChannelStats(nil), st.Channels...)

	for i := 0; i < 50; i++ {
		p := DefaultPolicy()
		p.MaxAttachmentBytes = int64(i) * 1024
		_ = EstimateChatImport(st, p)
	}
	if st.Messages != before.Messages || st.Attachments != before.Attachments ||
		len(st.Channels) != len(beforeChannels) {
		t.Fatal("estimating mutated the scan")
	}
	for i := range st.Channels {
		if st.Channels[i].Messages != beforeChannels[i].Messages ||
			st.Channels[i].LocalAttachmentBytes != beforeChannels[i].LocalAttachmentBytes {
			t.Fatalf("estimating mutated channel %d", i)
		}
	}
	a := EstimateChatImport(st, DefaultPolicy())
	b := EstimateChatImport(st, DefaultPolicy())
	if a.Messages != b.Messages || a.ChunkBytes != b.ChunkBytes || a.TotalBytes != b.TotalBytes {
		t.Fatal("two estimates of the same policy disagreed")
	}
}

// TestSanitizeContentDefusesTheRenderer is the injection gate. Each case here is
// something the renderer would ACT on, and the fail-safe proof for this file is
// to make SanitizeContent the identity function and watch it go red.
func TestSanitizeContentDefusesTheRenderer(t *testing.T) {
	t.Run("self-destruct token", func(t *testing.T) {
		// The worst one: a past-dated eph token is swept by the client within
		// seconds and the message deletes itself out of every member's store.
		in := "innocent text [eph](concord://eph/v1/1000000000) more text"
		got := SanitizeContent(in)
		if strings.Contains(got, "concord://") {
			t.Fatalf("the scheme survived: %q", got)
		}
		if !strings.Contains(got, "innocent text") || !strings.Contains(got, "more text") {
			t.Fatalf("the prose did not survive: %q", got)
		}
	})

	t.Run("every token shape", func(t *testing.T) {
		for _, tok := range []string{
			"[attach](concord://attach/v1/aa/bb/png/1x1)",
			"[file](concord://file/v1/aa/bb/1/x/y)",
			"[poll](concord://poll/v1/xyz)",
			"[embed](concord://embed/v1/xyz)",
			"[announcement](concord://announce/v1/xyz)",
			"[ts](concord://ts/v1/1000000000)",
			"[fx](concord://fx/v1/confetti)",
			"CONCORD://SHOUTED/v1/x",
		} {
			if got := SanitizeContent(tok); strings.Contains(strings.ToLower(got), "concord://") {
				t.Fatalf("%q survived as %q", tok, got)
			}
		}
	})

	t.Run("broadcast mentions", func(t *testing.T) {
		got := SanitizeContent("hey @everyone and @here, party")
		if strings.Contains(got, "@everyone") || strings.Contains(got, "@here") {
			t.Fatalf("a broadcast ping survived: %q", got)
		}
		if !strings.Contains(got, "everyone") || !strings.Contains(got, "here") {
			t.Fatalf("the words themselves were destroyed: %q", got)
		}
		// A name that merely starts with the word is not a broadcast and must be
		// left alone.
		if got := SanitizeContent("@everyoneelse said so"); !strings.Contains(got, "@everyoneelse") {
			t.Fatalf("a non-broadcast mention was mangled: %q", got)
		}
	})

	t.Run("placeholder sentinels", func(t *testing.T) {
		got := SanitizeContent("before \x01" + "7\x01 middle \x00" + "3\x00 after")
		if strings.ContainsAny(got, "\x00\x01") {
			t.Fatalf("a renderer placeholder sentinel survived: %q", got)
		}
	})

	t.Run("invisible and control characters", func(t *testing.T) {
		got := SanitizeContent("a\u202Eb\u200Bc\u0007d")
		if got != "abcd" {
			t.Fatalf("got %q, want %q", got, "abcd")
		}
		// Newlines are conversation, not control.
		if got := SanitizeContent("one\ntwo"); got != "one\ntwo" {
			t.Fatalf("newlines did not survive: %q", got)
		}
		if got := SanitizeContent("one\r\ntwo"); got != "one\ntwo" {
			t.Fatalf("CRLF became %q", got)
		}
	})

	t.Run("export markup becomes plain text", func(t *testing.T) {
		got := SanitizeContent("<:party-parrot:1234> hi <@999> in <#888> at <t:1546336800:R>")
		if strings.Contains(got, "<:") || strings.Contains(got, "<@") || strings.Contains(got, "<#") {
			t.Fatalf("export markup survived: %q", got)
		}
		if !strings.Contains(got, ":party_parrot:") {
			t.Fatalf("the emoji did not become a shortcode: %q", got)
		}
		if !strings.Contains(got, "2019-01-01") {
			t.Fatalf("the timestamp did not become a date: %q", got)
		}
		if !strings.Contains(got, "@999") || !strings.Contains(got, "#888") {
			t.Fatalf("the mention ids were lost: %q", got)
		}
	})

	t.Run("ordinary prose is untouched", func(t *testing.T) {
		// The gate must not be an escaper: escaping here and again at render
		// would leave a decade of history reading "&amp;lt;3".
		for _, s := range []string{
			"hello <3 & goodbye",
			"see https://example.invalid/page for the details",
			"**bold** and `code` and ||spoiler||",
			"a > b, and 5 < 6",
			"مرحبا بالعالم",
		} {
			if got := SanitizeContent(s); got != s {
				t.Fatalf("prose was altered:\n in: %q\nout: %q", s, got)
			}
		}
	})

	t.Run("length is bounded", func(t *testing.T) {
		long := strings.Repeat("x", maxImportedContentRunes*2)
		got := SanitizeContent(long)
		if len([]rune(got)) > maxImportedContentRunes+len([]rune(truncationMark)) {
			t.Fatalf("a %d-rune body came out %d runes", len(long), len([]rune(got)))
		}
		if !strings.HasSuffix(got, truncationMark) {
			t.Fatal("a truncated body did not say so")
		}
	})
}

// TestSanitizeEmojiName covers the fold onto the guild-emoji charset, which is
// the same regex AddCustomEmoji enforces — a name that gets past this and is
// then refused there would be an emoji counted as imported and missing.
func TestSanitizeEmojiName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Party-Parrot!", "party_parrot"},
		{"thumbsup", "thumbsup"},
		{"  Spaced  Out  ", "spaced_out"},
		{"UPPER", "upper"},
		{"a", ""},            // one character is under the floor
		{"!", ""},            // nothing usable at all
		{"__", ""},           // collapses to nothing
		{"9lives", "9lives"}, /* digits are legal */
		{strings.Repeat("a", 40), strings.Repeat("a", 32)},
		{"emoji💥", "emoji"},
	} {
		if got := SanitizeEmojiName(tc.in); got != tc.want {
			t.Errorf("SanitizeEmojiName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Everything it returns must satisfy the guild's own rule.
	for _, in := range []string{"Party-Parrot!", "  Spaced  Out  ", strings.Repeat("a", 40)} {
		got := SanitizeEmojiName(in)
		if got == "" {
			continue
		}
		if len(got) < 2 || len(got) > 32 {
			t.Errorf("SanitizeEmojiName(%q) = %q, outside 2..32", in, got)
		}
		for _, r := range got {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
				t.Errorf("SanitizeEmojiName(%q) = %q, which is not the guild charset", in, got)
			}
		}
	}
}

// TestSanitizeNameBoundsIt keeps a malformed export from producing a sidebar row
// that is a paragraph, or one whose name reorders the text around it.
func TestSanitizeNameBoundsIt(t *testing.T) {
	if got := SanitizeName("  general \n chat \t "); got != "general chat" {
		t.Fatalf("got %q", got)
	}
	if got := SanitizeName("a\u202Eb"); got != "ab" {
		t.Fatalf("a bidi override survived a name: %q", got)
	}
	long := SanitizeName(strings.Repeat("x", 500))
	if len([]rune(long)) != maxImportedNameRunes {
		t.Fatalf("a 500-character name came out %d", len([]rune(long)))
	}
	if SanitizeName("   ") != "" {
		t.Fatal("a whitespace-only name must come out empty so the caller can substitute")
	}
}

// TestLocalAssetPathRefusesToLeaveTheExport is the security boundary. The URL in
// an export file is a string a stranger's tool wrote, and resolving it is the
// moment before the importer reads whatever it names and seals it into a guild's
// permanent, replicated history.
func TestLocalAssetPathRefusesToLeaveTheExport(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "assets", "ok.png")
	if err := writeBlob(inside, 32); err != nil {
		t.Fatal(err)
	}

	if _, ok := localAssetPath(root, "assets/ok.png"); !ok {
		t.Fatal("a file genuinely inside the export was refused")
	}

	rel, err := filepath.Rel(root, secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, hostile := range []string{
		rel,                          // ../<tmp>/secret.txt
		"assets/../" + rel,           // laundered through a subdirectory
		secret,                       // absolute
		"../../../../etc/passwd",     // the classic
		"https://example.invalid/x",  // remote, never opened
		"//example.invalid/x",        // protocol-relative
		"file:///etc/passwd",         // another scheme
		"assets/../../" + "etc/host", // out and back
	} {
		if p, ok := localAssetPath(root, hostile); ok {
			t.Fatalf("%q resolved to %q; the export escaped its own directory", hostile, p)
		}
	}

	// A symlink is the interesting case, because the naive check runs before the
	// resolution and passes.
	link := filepath.Join(root, "assets", "escape.png")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("this filesystem does not do symlinks: %v", err)
	}
	if p, ok := localAssetPath(root, "assets/escape.png"); ok {
		t.Fatalf("a symlink out of the export resolved to %q", p)
	}
}

// TestRemoteAttachmentsAreNeverOpened states the offline rule as a test: a
// remote URL resolves to nothing, whatever it looks like, so no code path
// downstream can be handed a path for one.
func TestRemoteAttachmentsAreNeverOpened(t *testing.T) {
	root := t.TempDir()
	for _, u := range []string{
		"https://cdn.example.invalid/a/b.png",
		"http://example.invalid/x",
		"HTTPS://EXAMPLE.INVALID/X",
		"//example.invalid/x",
		"ftp://example.invalid/x",
	} {
		if !isRemoteURL(u) {
			t.Errorf("%q was not recognised as remote", u)
		}
		if _, ok := LocalAttachmentPath(root, Attachment{URL: u}); ok {
			t.Errorf("%q resolved to a local file", u)
		}
	}
	// And a plain relative path is not remote, Windows drive letters included.
	for _, u := range []string{"assets/x.png", "x.png", `C:\assets\x.png`} {
		if isRemoteURL(u) {
			t.Errorf("%q was wrongly treated as remote", u)
		}
	}
}

// TestChannelTypeMapping covers the vocabulary translation, including the rule
// that an unknown type becomes a text channel rather than being dropped.
func TestChannelTypeMapping(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"GuildTextChat", "text"},
		{"GuildVoiceChat", "voice"},
		{"GuildStageVoice", "voice"},
		{"GuildForum", "forum"},
		{"GuildNews", "announcement"},
		{"GuildAnnouncement", "announcement"},
		{"", "text"},
		{"SomethingNobodyHasSeen", "text"},
	} {
		if got := ChannelTypeOf(tc.in); got != tc.want {
			t.Errorf("ChannelTypeOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTimestampTolerance covers the layouts a decade of export tools have
// written, and the refusal of a value that is not a date at all.
func TestTimestampTolerance(t *testing.T) {
	for _, s := range []string{
		"2019-04-02T18:21:03.117+00:00",
		"2019-04-02T18:21:03Z",
		"2019-04-02T18:21:03.1234567",
		"2019-04-02 18:21:03",
		"2019-04-02",
	} {
		if _, ok := parseStamp(s); !ok {
			t.Errorf("%q did not parse", s)
		}
	}
	for _, s := range []string{"", "null", "whenever", "0001-01-01T00:00:00Z", "9999-01-01T00:00:00Z"} {
		if _, ok := parseStamp(s); ok {
			t.Errorf("%q parsed, and should not have", s)
		}
	}
}

// TestEmptyDirectoryIsAnError: a wizard pointed at the wrong folder should be
// told so, rather than shown an import of nothing.
func TestEmptyDirectoryIsAnError(t *testing.T) {
	if _, err := ScanChatExport(t.TempDir()); err == nil {
		t.Fatal("an empty directory scanned without complaint")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
