package app

import (
	"strings"
	"testing"
)

// The search panel says ALL CONVERSATIONS and scanned the messages table. An
// archive lives in sealed chunks, so imported history — visible on the screen
// behind the panel, scrolling perfectly — answered "0 results" to phrases read
// straight off it.
func TestSearchChronicleFindsImportedText(t *testing.T) {
	owner := mustID(t)
	s := chronicleTestService(t, owner, owner)
	raw, chunks := sampleChronicle(t, s, "g1")
	if _, err := s.store.SaveChronicleManifest(chronicleIDOf(raw), "g1", raw); err != nil {
		t.Fatal(err)
	}
	for id, ct := range chunks {
		if err := s.store.SaveChronicleChunk(id, ct, true); err != nil {
			t.Fatal(err)
		}
	}

	// Every message opens with its own zero-padded index, so "0498 " is one
	// message and nothing else — the exact shape of "I can see this line on the
	// screen; find it".
	res, err := s.SearchChronicle("g1", "0498 the quick brown", 50)
	if err != nil {
		t.Fatalf("SearchChronicle: %v", err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("got %d hits for a phrase that occurs once", len(res.Hits))
	}
	h := res.Hits[0]
	if !strings.HasPrefix(h.Content, "0498 ") {
		t.Fatalf("hit content is %q", h.Content[:min(40, len(h.Content))])
	}
	// The author comes back as a NAME. The chunk stores an index into the
	// manifest's table and a result panel cannot render an integer.
	if h.Author != "ada" { // 498 % 3 == 0
		t.Fatalf("author is %q, want ada", h.Author)
	}
	if h.ChannelName != "general" || h.ChannelID != "src-general" {
		t.Fatalf("hit says it came from %q/%q", h.ChannelID, h.ChannelName)
	}
	if h.Nano == 0 {
		t.Fatal("hit carries no timestamp")
	}

	// Case-insensitive, like the live scan.
	if up, err := s.SearchChronicle("g1", "0498 THE QUICK BROWN", 50); err != nil || len(up.Hits) != 1 {
		t.Fatalf("case-insensitive search returned %d hits (err %v)", len(up.Hits), err)
	}

	// Newest first: the answer somebody wants first is the most recent one.
	many, err := s.SearchChronicle("g1", "lazy dog", 50)
	if err != nil {
		t.Fatalf("SearchChronicle: %v", err)
	}
	if len(many.Hits) != 50 {
		t.Fatalf("a phrase in every message returned %d hits, want the 50 cap", len(many.Hits))
	}
	for i := 1; i < len(many.Hits); i++ {
		if many.Hits[i].Nano > many.Hits[i-1].Nano {
			t.Fatal("hits must come back newest first")
		}
	}

	// Nothing matches nothing, and an empty query is not a match-everything.
	if none, err := s.SearchChronicle("g1", "xyzzy plugh", 50); err != nil || len(none.Hits) != 0 {
		t.Fatalf("a phrase in nothing returned %d hits (err %v)", len(none.Hits), err)
	}
	if empty, err := s.SearchChronicle("g1", "   ", 50); err != nil || len(empty.Hits) != 0 {
		t.Fatalf("an empty query returned %d hits (err %v)", len(empty.Hits), err)
	}
	// A guild with no archive is not an error either — every search runs this.
	if v, err := s.SearchChronicle("g2", "anything", 50); err != nil || len(v.Hits) != 0 || v.Total != 0 {
		t.Fatalf("a guild with no archive returned %+v (err %v)", v, err)
	}
}

// The honesty half. Coverage is the reason this feature is not simply "search
// the archive": chunks arrive as they are scrolled to and are evicted under a
// cache cap, so on every device except the importer's the archive is a thin and
// shifting slice — and a panel that searched a tenth of it and said nothing
// would be the old lie in a new place.
func TestSearchChronicleReportsWhatItCouldNotSee(t *testing.T) {
	owner := mustID(t)
	s := chronicleTestService(t, owner, owner)
	raw, chunks := sampleChronicle(t, s, "g1")
	m, err := decodeChronicleManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.SaveChronicleManifest(chronicleIDOf(raw), "g1", raw); err != nil {
		t.Fatal(err)
	}
	if len(m.Chunks) < 3 {
		t.Fatalf("this test needs a multi-chunk archive; got %d", len(m.Chunks))
	}

	// Nothing on the device at all: a member who has never scrolled back.
	res, err := s.SearchChronicle("g1", "lazy dog", 50)
	if err != nil {
		t.Fatalf("SearchChronicle: %v", err)
	}
	if len(res.Hits) != 0 || res.Searched != 0 {
		t.Fatalf("searched %d messages of an archive it holds none of", res.Searched)
	}
	if res.Total != m.Messages || res.Total == 0 {
		t.Fatalf("total is %d, want the manifest's %d", res.Total, m.Messages)
	}

	// One chunk present: the count must go up by exactly that chunk, and must
	// not creep towards the total.
	var first chronicleChunkRef
	for _, c := range m.Chunks {
		if first.ID == "" || c.LastNano > first.LastNano {
			first = c
		}
	}
	if err := s.store.SaveChronicleChunk(first.ID, chunks[first.ID], false); err != nil {
		t.Fatal(err)
	}
	part, err := s.SearchChronicle("g1", "lazy dog", 50)
	if err != nil {
		t.Fatalf("SearchChronicle: %v", err)
	}
	if part.Searched != int64(first.Count) {
		t.Fatalf("searched %d, want the one chunk's %d", part.Searched, first.Count)
	}
	if part.Searched >= part.Total {
		t.Fatal("one chunk of many must not report full coverage")
	}
	if len(part.Hits) == 0 {
		t.Fatal("the chunk that IS present produced no hits")
	}

	// (A search must not warm the eviction order either — that half is
	// store.TestChronicleChunkStaleDoesNotWarmTheCache, where last_used is
	// visible.)

	// Everything present: full coverage, nothing to apologise for.
	for id, ct := range chunks {
		if err := s.store.SaveChronicleChunk(id, ct, true); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.SearchChronicle("g1", "lazy dog", 50)
	if err != nil {
		t.Fatalf("SearchChronicle: %v", err)
	}
	if all.Searched != all.Total {
		t.Fatalf("with every chunk present it searched %d of %d", all.Searched, all.Total)
	}
	if all.Truncated {
		t.Fatal("a 500-message archive should not exhaust the scan budget")
	}
}
