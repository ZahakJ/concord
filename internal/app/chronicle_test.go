package app

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
	cnet "github.com/ZahakJ/concord/internal/net"
	"github.com/ZahakJ/concord/internal/store"
)

// chronicleTestService is a Service with a real store and a real identity
// behind it and nothing else — building, signing, verifying and ingesting an
// archive touch neither the network nor MLS, so they can be driven directly the
// way the story and govstate tests drive theirs. owner is whose key the guild's
// crown sits on.
func chronicleTestService(t *testing.T, id *identity.Identity, owner *identity.Identity) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "concord.db"), bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Service{
		store:    st,
		id:       id,
		guilds:   map[string]*domain.Guild{"g1": {ID: "g1", OwnerID: owner.PublicKey()}},
		govOps:   map[string][]govOp{},
		govState: map[string]GuildState{},
	}
}

// sampleChronicle builds a small but real archive: two channels, 500 messages
// between them, bodies fat enough that the builder has to split more than once.
func sampleChronicle(t *testing.T, s *Service, guildID string) ([]byte, map[string][]byte) {
	t.Helper()
	channels := []chronicleChannel{
		{ID: "src-general", Name: "general", Type: "text", Topic: "everything else"},
		{ID: "src-photos", Name: "photos", Type: "text"},
	}
	authors := []chronicleAuthor{
		{Name: "ada"},
		{Name: "grace", Avatar: "data:image/png;base64," + strings.Repeat("A", 200)},
		{Name: "linus"},
	}
	body := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 70) // ~3 KiB
	byChannel := map[string][]chronicleMessage{}
	for i := 0; i < 500; i++ {
		ch := channels[i%2].ID
		m := chronicleMessage{
			ID:      fmt.Sprintf("m%04d", i),
			Author:  i % len(authors),
			Nano:    int64(1_300_000_000_000_000_000 + i*1_000_000_000),
			Content: fmt.Sprintf("%04d %s", i, body),
		}
		if i%50 == 0 {
			m.Reactions = map[string]int{"👍": i}
			m.Attach = []string{"concord://attach/v1/" + strings.Repeat("ab", 32) + "/k/png/1x1"}
		}
		if i > 0 {
			m.ReplyTo = fmt.Sprintf("m%04d", i-1)
		}
		byChannel[ch] = append(byChannel[ch], m)
	}
	raw, chunks, err := s.buildChronicle(guildID, "the old forum", "2009 to 2019", channels, authors, byChannel, 10, 4096)
	if err != nil {
		t.Fatalf("buildChronicle: %v", err)
	}
	return raw, chunks
}

// TestChronicleSignVerifyRoundTrip is the trust core: a manifest the owner
// signed verifies, and every route to changing a byte of it afterwards fails.
func TestChronicleSignVerifyRoundTrip(t *testing.T) {
	owner := mustID(t)
	s := chronicleTestService(t, owner, owner)
	raw, chunks := sampleChronicle(t, s, "g1")

	m, err := decodeChronicleManifest(raw)
	if err != nil {
		t.Fatalf("a manifest signed by its owner must verify: %v", err)
	}
	if m.Signer != identity.FingerprintOf(owner.PublicKey()) {
		t.Fatalf("signer = %q, want the owner's fingerprint", m.Signer)
	}
	if m.Messages != 500 || int(m.Messages) != countChunkMessages(m) {
		t.Fatalf("headline total %d does not match the index", m.Messages)
	}
	if len(chunks) != len(m.Chunks) {
		t.Fatalf("built %d pages for an index of %d", len(chunks), len(m.Chunks))
	}

	// TAMPERED RAW BYTES. The description is free text nobody validates, which
	// makes it the most tempting thing to rewrite in transit — and it is under
	// the signature, so rewriting it must kill the manifest whole.
	edited := bytes.Replace(raw, []byte(`"2009 to 2019"`), []byte(`"2009 to 2020"`), 1)
	if bytes.Equal(edited, raw) {
		t.Fatal("test bug: the description was not found in the raw manifest")
	}
	if _, err := decodeChronicleManifest(edited); err == nil {
		t.Fatal("an edited manifest verified; the signature does not cover the description")
	}

	// A REPLACED PORTRAIT. Author avatars are represented in the signed form by
	// their hash rather than inline, so this is the case that proves hashing
	// still binds the bytes.
	var view chronicleManifest
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	view.Authors[1].Avatar = "data:image/png;base64," + strings.Repeat("B", 200)
	swapped, _ := json.Marshal(view)
	if _, err := decodeChronicleManifest(swapped); err == nil {
		t.Fatal("a swapped author portrait verified; hashing it did not bind it")
	}

	// A WRONG SIGNER KEY. Somebody else's key with the owner's fingerprint
	// still claimed: the binding check must catch it before the signature is
	// even considered.
	mallory := mustID(t)
	view = chronicleManifest{}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	view.SignerKey = mallory.PublicKey()
	mislabelled, _ := json.Marshal(view)
	if _, err := decodeChronicleManifest(mislabelled); err == nil {
		t.Fatal("a manifest whose key does not match its claimed signer verified")
	}

	// A COMPLETE FORGERY: Mallory signs her own manifest but writes the owner's
	// fingerprint into it — the shape a hostile member would actually send.
	view = chronicleManifest{}
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	view.SignerKey = mallory.PublicKey()
	view.Sig = nil
	view.Sig = mallory.Sign(view.signingBytes())
	forged, _ := json.Marshal(view)
	if _, err := decodeChronicleManifest(forged); err == nil {
		t.Fatal("a manifest signed by the wrong key but claiming the owner verified")
	}
}

func countChunkMessages(m chronicleManifest) int {
	n := 0
	for _, c := range m.Chunks {
		n += c.Count
	}
	return n
}

// TestChronicleIngestRequiresTheOwner is the ingest gate. The signature only
// says WHO signed; this is what says whether that person may speak for the
// guild's past.
func TestChronicleIngestRequiresTheOwner(t *testing.T) {
	owner := mustID(t)
	member := mustID(t)

	// Built and signed by an ordinary member, on their own machine.
	theirs := chronicleTestService(t, member, owner)
	forged, _ := sampleChronicle(t, theirs, "g1")

	// It arrives at a peer whose guild is owned by somebody else.
	mine := chronicleTestService(t, owner, owner)
	if mine.ingestChronicle("g1", forged) {
		t.Fatal("an archive signed by a non-owner was accepted")
	}
	if rows, _ := mine.store.ChronicleManifests("g1"); len(rows) != 0 {
		t.Fatalf("a refused archive left %d rows behind; ingest must be all or nothing", len(rows))
	}

	// The owner's own archive is accepted, once.
	ours := chronicleTestService(t, owner, owner)
	raw, _ := sampleChronicle(t, ours, "g1")
	if !mine.ingestChronicle("g1", raw) {
		t.Fatal("the owner's own archive was refused")
	}
	if mine.ingestChronicle("g1", raw) {
		t.Fatal("the same archive was reported new twice")
	}

	// The same manifest replayed into a DIFFERENT guild: the guild id is under
	// the signature, so it cannot be rewritten, and must not be accepted as-is.
	mine.mu.Lock()
	mine.guilds["g2"] = &domain.Guild{ID: "g2", OwnerID: owner.PublicKey()}
	mine.mu.Unlock()
	if mine.ingestChronicle("g2", raw) {
		t.Fatal("an archive signed for one guild was accepted into another")
	}

	// And a guild we do not track at all has no owner, so it has no archive.
	if mine.ingestChronicle("unknown-guild", raw) {
		t.Fatal("an archive was accepted for a guild we do not track")
	}
}

// TestChroniclePagesRespectTheTransportCap: a page bigger than the transfer
// protocol's frame is a page no peer will ever serve, so the builder — not the
// reader — is where the limit has to bite.
func TestChroniclePagesRespectTheTransportCap(t *testing.T) {
	owner := mustID(t)
	s := chronicleTestService(t, owner, owner)
	raw, chunks := sampleChronicle(t, s, "g1")
	m, err := decodeChronicleManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Chunks) < 3 {
		t.Fatalf("500 fat messages produced %d pages; the split is not happening", len(m.Chunks))
	}
	seen := map[string]bool{}
	for _, ref := range m.Chunks {
		ct, ok := chunks[ref.ID]
		if !ok {
			t.Fatalf("the index names page %s but the bundle does not carry it", ref.ID)
		}
		if len(ct) > cnet.MaxChronicleChunk {
			t.Fatalf("page %s is %d bytes, over the %d-byte cap", ref.ID, len(ct), cnet.MaxChronicleChunk)
		}
		if len(ct) != ref.Size {
			t.Fatalf("page %s is %d bytes, the index says %d", ref.ID, len(ct), ref.Size)
		}
		if seen[ref.ID] {
			t.Fatalf("page %s appears twice in the index", ref.ID)
		}
		seen[ref.ID] = true
		if ref.LastNano < ref.FirstNano {
			t.Fatalf("page %s has a backwards time range", ref.ID)
		}
	}

	// The split recurses until a run fits — and stops at one message, because
	// there is nothing left to halve. A single incompressible body over the cap
	// must therefore be a refusal, not a silently unservable page.
	noise := make([]byte, cnet.MaxChronicleChunk+(64<<10))
	if _, err := rand.Read(noise); err != nil {
		t.Fatal(err)
	}
	huge := []chronicleMessage{{ID: "x", Nano: 1, Content: base64.StdEncoding.EncodeToString(noise)}}
	if _, _, err := sealChronicleChunks("c", huge); err == nil {
		t.Fatal("a single message too big to seal into one page was accepted")
	}
	// Two of them split into two pages rather than failing.
	pair := []chronicleMessage{
		{ID: "x", Nano: 1, Content: base64.StdEncoding.EncodeToString(noise[:len(noise)/3])},
		{ID: "y", Nano: 2, Content: base64.StdEncoding.EncodeToString(noise[len(noise)/3:])},
	}
	refs, cts, err := sealChronicleChunks("c", pair)
	if err != nil {
		t.Fatalf("a pair that fits in two pages was refused: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("expected the pair to split into 2 pages, got %d", len(refs))
	}
	for _, r := range refs {
		if len(cts[r.ID]) > cnet.MaxChronicleChunk {
			t.Fatalf("a split page is still %d bytes", len(cts[r.ID]))
		}
	}
}

// TestChroniclePageSealRoundTrip: seal, open, and get exactly what went in —
// including the fields that only some messages carry.
func TestChroniclePageSealRoundTrip(t *testing.T) {
	in := []chronicleMessage{
		{ID: "a", Author: 0, Nano: 1000, Content: "first"},
		{ID: "b", Author: 2, Nano: 2000, Content: "second", ReplyTo: "a",
			Reactions: map[string]int{"🎉": 3}, Attach: []string{"concord://attach/v1/x/y/png/1x1"}},
		{ID: "c", Author: 1, Nano: 3000, Content: ""},
	}
	ref, ct, err := sealChronicleChunk("chan", in)
	if err != nil {
		t.Fatalf("sealChronicleChunk: %v", err)
	}
	if ref.Count != 3 || ref.FirstNano != 1000 || ref.LastNano != 3000 || ref.Channel != "chan" {
		t.Fatalf("index entry does not describe the page: %+v", ref)
	}
	out, err := openChronicleChunk(ct, ref.Keys)
	if err != nil {
		t.Fatalf("openChronicleChunk: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("opened %d messages, sealed %d", len(out), len(in))
	}
	for i := range in {
		if fmt.Sprint(out[i]) != fmt.Sprint(in[i]) {
			t.Fatalf("message %d came back as %+v, sealed %+v", i, out[i], in[i])
		}
	}
	// The wrong key must not open it, and must say so rather than returning an
	// empty page that reads as "this part of history was blank".
	wrong := bytes.Repeat([]byte{0}, attachKeysLen)
	if _, err := openChronicleChunk(ct, wrong); err == nil {
		t.Fatal("the wrong key opened a page")
	}
	if _, err := openChronicleChunk(ct, []byte{1, 2, 3}); err == nil {
		t.Fatal("a malformed key was accepted")
	}
}

// TestChronicleReadPagesBackwards drives the reading API against a real archive
// on a Service with no network: everything it needs is already cached, so no
// fetch is attempted and the metered rule never comes into it.
func TestChronicleReadPagesBackwards(t *testing.T) {
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
	for id, ct := range chunks {
		if err := s.store.SaveChronicleChunk(id, ct, true); err != nil {
			t.Fatal(err)
		}
	}

	info, err := s.ChronicleInfo("g1")
	if err != nil {
		t.Fatalf("ChronicleInfo: %v", err)
	}
	if info.Messages != 500 || len(info.Channels) != 2 {
		t.Fatalf("info reports %d messages across %d channels", info.Messages, len(info.Channels))
	}
	if info.ChunksCached != len(m.Chunks) || !info.Pinned {
		t.Fatalf("a fully-present pinned archive reads as %d/%d cached, pinned=%v",
			info.ChunksCached, info.Chunks, info.Pinned)
	}
	var general ChronicleChannelView
	for _, c := range info.Channels {
		if c.ID == "src-general" {
			general = c
		}
	}
	if general.Messages != 250 {
		t.Fatalf("general holds %d messages, want 250", general.Messages)
	}

	page, err := s.ChronicleMessages("g1", "src-general", 0, 20, false)
	if err != nil {
		t.Fatalf("ChronicleMessages: %v", err)
	}
	if len(page) != 20 {
		t.Fatalf("asked for 20 messages, got %d", len(page))
	}
	for i := 1; i < len(page); i++ {
		if page[i].Nano <= page[i-1].Nano {
			t.Fatal("a page must come back in reading order, oldest first")
		}
	}
	// Authors are resolved from the manifest's table, never handed back as an
	// index the caller would have to look up itself.
	for _, v := range page {
		if v.Author == "" {
			t.Fatalf("message %s came back with no author name", v.ID)
		}
	}
	// The last message of src-general is m0498 (even indices), by author 498%3.
	last := page[len(page)-1]
	if last.ID != "m0498" {
		t.Fatalf("the newest message of general is %q, want m0498", last.ID)
	}

	// Paging backwards from the oldest of that page must not repeat it.
	older, err := s.ChronicleMessages("g1", "src-general", page[0].Nano, 20, false)
	if err != nil {
		t.Fatalf("ChronicleMessages (older): %v", err)
	}
	if len(older) != 20 {
		t.Fatalf("second page is %d messages", len(older))
	}
	if older[len(older)-1].Nano >= page[0].Nano {
		t.Fatal("the second page overlaps the first")
	}

	// A channel that is not in the archive is empty, not an error.
	if got, err := s.ChronicleMessages("g1", "src-nope", 0, 20, false); err != nil || len(got) != 0 {
		t.Fatalf("an unknown channel returned %d messages (err %v)", len(got), err)
	}
	// Unpinning leaves the archive readable — it only makes the pages evictable.
	if err := s.SetChroniclePinned("g1", false); err != nil {
		t.Fatalf("SetChroniclePinned: %v", err)
	}
	if info, err := s.ChronicleInfo("g1"); err != nil || info.Pinned {
		t.Fatalf("still pinned after unpinning (err %v)", err)
	}
}

// TestChronicleDigestCostsTensOfBytes measures the standing price of the
// feature: what a guild carrying one archive adds to every anti-entropy request
// forever, whether or not anybody ever reads a page.
func TestChronicleDigestCostsTensOfBytes(t *testing.T) {
	owner := mustID(t)
	s := chronicleTestService(t, owner, owner)
	raw, _ := sampleChronicle(t, s, "g1")
	if _, err := s.store.SaveChronicleManifest(chronicleIDOf(raw), "g1", raw); err != nil {
		t.Fatal(err)
	}

	bare, _ := json.Marshal(&syncDigest{})
	withOne, _ := json.Marshal(&syncDigest{Chronicles: digestChronicles(s.chroniclesForSync("g1"))})
	cost := len(withOne) - len(bare)
	if cost > 64 {
		t.Fatalf("one archive adds %d bytes to every sync request; it must stay in the tens", cost)
	}
	// And a peer that holds the same archive must produce the same inventory
	// entry, or the responder re-ships the manifest on every beat.
	other := chronicleTestService(t, mustID(t), owner)
	if !other.ingestChronicle("g1", raw) {
		t.Fatal("the owner's archive was refused by a member")
	}
	mine := digestChronicles(s.chroniclesForSync("g1"))
	theirs := digestChronicles(other.chroniclesForSync("g1"))
	if len(mine) != 1 || len(theirs) != 1 || mine[0] != theirs[0] {
		t.Fatalf("two peers holding one archive disagree: %v vs %v", mine, theirs)
	}
	// Which is the point: the responder now has nothing to send.
	out, _ := buildSyncPayload(syncSource{
		guild:      domain.Guild{ID: "g1"},
		chronicles: s.chroniclesForSync("g1"),
	}, &syncDigest{Chronicles: theirs})
	if len(out.Chronicles) != 0 {
		t.Fatal("a manifest the requester already holds was served again")
	}
	// A peer that holds nothing gets it.
	out, _ = buildSyncPayload(syncSource{
		guild:      domain.Guild{ID: "g1"},
		chronicles: s.chroniclesForSync("g1"),
	}, &syncDigest{})
	if len(out.Chronicles) != 1 || !bytes.Equal(out.Chronicles[0], raw) {
		t.Fatal("a manifest the requester lacks was not served, or was re-encoded on the way out")
	}
}
