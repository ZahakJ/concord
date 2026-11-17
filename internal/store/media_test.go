package store

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/mediatok"
)

// Token builders for the gallery tests: valid by the shared grammar
// (internal/mediatok), with made-up blob ids and keys.
var mtKeys = strings.Repeat("K", 75)

func mtBlob(n int) string { return fmt.Sprintf("%064x", n) }

func mtImage(n int) string {
	return fmt.Sprintf("![image](concord://attach/v1/%s/%s/png/%dx%d)", mtBlob(n), mtKeys, 100+n%7, 50+n%5)
}

func mtVideo(n int) string {
	return fmt.Sprintf("[file](concord://file/v1/%s/%s/%d/dmlkZW8vbXA0/Y2xpcC5tcDQ)", mtBlob(n), mtKeys, 1000+n)
}

func mtFile(n int) string {
	return fmt.Sprintf("[file](concord://file/v1/%s/%s/%d/YXBwbGljYXRpb24vcGRm/ZG9jLnBkZg)", mtBlob(n), mtKeys, 10+n)
}

func mtEph(at time.Time) string { return fmt.Sprintf("[eph](concord://eph/v1/%d)", at.Unix()) }

var mtSender = []byte("sender-a")

func mtSave(t testing.TB, st *Store, id, ch string, sent time.Time, body string) {
	t.Helper()
	if _, err := st.SaveMessage(domain.Message{ID: id, ChannelID: ch, Sender: mtSender, Sent: sent, Content: body}); err != nil {
		t.Fatal(err)
	}
}

type mtKey struct {
	id   string
	ord  int
	kind mediatok.Kind
}

// mtAll reads every item of both tabs of a channel through MediaPage, in
// pages of `page`, so the cursor is exercised as well as the filter.
func mtAll(t testing.TB, st *Store, ch string, page int) []mtKey {
	t.Helper()
	var out []mtKey
	for _, tab := range []mediatok.Tab{mediatok.TabVisual, mediatok.TabFiles} {
		cur := MediaCursor{}
		for i := 0; ; i++ {
			items, next, err := st.MediaPage(ch, tab, cur, page)
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range items {
				if mediatok.TabOf(it.Item.Kind) != tab {
					t.Fatalf("%s/%d (kind %d) listed under tab %d", it.MessageID, it.Item.Ord, it.Item.Kind, tab)
				}
				out = append(out, mtKey{it.MessageID, it.Item.Ord, it.Item.Kind})
			}
			if next.MessageID == "" {
				break
			}
			if i > 10000 {
				t.Fatal("paging does not end")
			}
			cur = next
		}
	}
	return out
}

// mtOracle recomputes what the gallery must show from the rows themselves:
// the messages the feed lists (hidden ones excluded by Messages), live, a
// person's or a guest's, not past a deadline (the body's or the row's),
// every attachment in each body.
func mtOracle(t testing.TB, st *Store, ch string) []mtKey {
	t.Helper()
	msgs, err := st.Messages(ch, 0)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	type row struct {
		k    mtKey
		sent int64
	}
	var rows []row
	for _, m := range msgs {
		if m.Deleted || (m.Kind != "" && m.Kind != "guest") {
			continue
		}
		if exp := EphemeralExpiry(m.Content); exp > 0 && exp <= now {
			continue
		}
		// The row's own deadline counts too: an edit does not take a timer
		// off a message, so a body edited after its row was stamped is still
		// due when the row says.
		var rowExp int64
		if err := st.db.QueryRow(`SELECT expires FROM messages WHERE id = ?`, m.ID).Scan(&rowExp); err != nil {
			t.Fatal(err)
		}
		if rowExp > 0 && rowExp <= now {
			continue
		}
		for _, it := range mediatok.Parse(m.Content) {
			rows = append(rows, row{mtKey{m.ID, it.Ord, it.Kind}, m.Sent.UnixNano()})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		ta, tb := mediatok.TabOf(a.k.kind), mediatok.TabOf(b.k.kind)
		if ta != tb {
			return ta < tb
		}
		if a.sent != b.sent {
			return a.sent > b.sent
		}
		if a.k.id != b.k.id {
			return a.k.id > b.k.id
		}
		return a.k.ord > b.k.ord
	})
	out := make([]mtKey, len(rows))
	for i, r := range rows {
		out[i] = r.k
	}
	return out
}

func mtEqual(a, b []mtKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Oracle property test: after every step of a random history (sends,
// catch-up inserts, edits, deletes, expiries, erasures, hides and unhides,
// retention prunes, channel deletes, relayed rows proven by their author), the
// gallery is exactly what the rows say it should be, page by page.
func TestMediaIndexMatchesOracle(t *testing.T) {
	for seed := int64(1); seed <= 4; seed++ {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			st := sidebarStore(t)
			r := rand.New(rand.NewSource(seed))
			chans := []string{"c1", "c2"}
			base := time.Now().Add(-time.Hour)
			var ids []string
			blob := 0
			body := func() string {
				blob++
				switch r.Intn(9) {
				case 0:
					return "just words"
				case 1:
					return mtImage(blob)
				case 2:
					return mtImage(blob) + "\ncaption " + mtFile(blob+1000)
				case 3:
					return mtVideo(blob)
				case 4:
					return mtImage(blob) + mtImage(blob+2000) + mtVideo(blob+3000)
				case 5:
					return mtImage(blob) + " " + mtEph(time.Now().Add(-time.Minute))
				case 6:
					return mtImage(blob) + " " + mtEph(time.Now().Add(time.Hour))
				case 7:
					return mtFile(blob)
				default:
					return "[game](concord://game/v1/abc)"
				}
			}
			hideAt := int64(1)
			for step := 0; step < 160; step++ {
				ch := chans[r.Intn(len(chans))]
				pick := func() string {
					if len(ids) == 0 {
						return "none"
					}
					return ids[r.Intn(len(ids))]
				}
				op := r.Intn(12)
				switch op {
				case 0, 1, 2:
					id := fmt.Sprintf("m%03d", step)
					kind := ""
					if r.Intn(8) == 0 {
						kind = "system"
					}
					if _, err := st.SaveMessage(domain.Message{ID: id, ChannelID: ch, Sender: mtSender, Kind: kind,
						Sent: base.Add(time.Duration(step) * time.Second), Content: body()}); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, id)
				case 3:
					id := fmt.Sprintf("s%03d", step)
					if _, err := st.UpsertSyncedMessage(domain.Message{ID: id, ChannelID: ch, Sender: mtSender,
						Sent: base.Add(time.Duration(r.Intn(160)) * time.Second), Content: body()}, "self", true); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, id)
				case 4:
					if _, err := st.UpdateContent(pick(), mtSender, body(), nil, "", false); err != nil {
						t.Fatal(err)
					}
				case 5:
					if _, _, err := st.MarkDeleted(pick(), nil, true); err != nil {
						t.Fatal(err)
					}
				case 6:
					if _, _, err := st.MarkDisappeared(pick()); err != nil {
						t.Fatal(err)
					}
				case 7:
					if err := st.EraseContent(pick()); err != nil {
						t.Fatal(err)
					}
				case 8:
					id := pick()
					if m, ok, _ := st.MessageByID(id); ok {
						hideAt++
						if _, err := st.SetHidden(HideRec{Channel: m.ChannelID, ID: id, Hidden: r.Intn(2) == 0, At: hideAt}); err != nil {
							t.Fatal(err)
						}
					}
				case 9:
					if r.Intn(4) == 0 {
						cut := base.Add(time.Duration(r.Intn(40)) * time.Second).UnixNano()
						if _, err := st.PruneMessagesBefore([]string{ch}, cut); err != nil {
							t.Fatal(err)
						}
					}
				case 10:
					if r.Intn(10) == 0 {
						if err := st.DeleteChannel(ch); err != nil {
							t.Fatal(err)
						}
					}
				case 11:
					// A relayed copy, then the author's own copy with other words.
					id := fmt.Sprintf("r%03d", step)
					sent := base.Add(time.Duration(step) * time.Second)
					if _, err := st.SaveMessage(domain.Message{ID: id, ChannelID: ch, Sender: mtSender, Sent: sent,
						Content: body(), Unverified: true}); err != nil {
						t.Fatal(err)
					}
					if _, err := st.ProveRelayedMessage(domain.Message{ID: id, ChannelID: ch, Sender: mtSender, Sent: sent,
						Content: body()}); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, id)
				}
				// Not after every step for every page size: a full read drains the
				// queue, and the point is also that work queued across several
				// steps comes out right.
				if r.Intn(3) == 0 || step == 159 {
					for _, c := range chans {
						got, want := mtAll(t, st, c, 1+r.Intn(4)), mtOracle(t, st, c)
						if !mtEqual(got, want) {
							t.Fatalf("step %d (op %d), channel %s:\n gallery %v\n oracle  %v\n index %s", step, op, c, got, want, mtDump(st, c))
						}
					}
				}
			}
		})
	}
}

// What the gallery must never show, each on its own.
func TestMediaNeverShows(t *testing.T) {
	st := sidebarStore(t)
	now := time.Now()
	mtSave(t, st, "live", "c", now.Add(-5*time.Second), mtImage(1))
	mtSave(t, st, "other-channel", "c2", now.Add(-4*time.Second), mtImage(2))
	mtSave(t, st, "deleted", "c", now.Add(-3*time.Second), mtImage(3))
	mtSave(t, st, "hidden", "c", now.Add(-2*time.Second), mtImage(4))
	mtSave(t, st, "expired", "c", now.Add(-1*time.Second), mtImage(5)+mtEph(now.Add(2*time.Second)))
	mtSave(t, st, "system", "c", now, "")
	if _, err := st.db.Exec(`UPDATE messages SET kind = 'system' WHERE id = 'system'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MarkDeleted("deleted", nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetHidden(HideRec{Channel: "c", ID: "hidden", Hidden: true, At: 10}); err != nil {
		t.Fatal(err)
	}
	ids := func() []string {
		var out []string
		for _, k := range mtAll(t, st, "c", 50) {
			out = append(out, k.id)
		}
		return out
	}
	if got := strings.Join(ids(), ","); got != "expired,live" {
		t.Fatalf("gallery shows %s, want expired,live", got)
	}

	// A hidden row comes back on unhide.
	if _, err := st.SetHidden(HideRec{Channel: "c", ID: "hidden", Hidden: false, At: 11}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(ids(), ","); got != "expired,hidden,live" {
		t.Fatalf("after unhide the gallery shows %s", got)
	}

	// Past its deadline and not yet swept: gone from the gallery at once, with
	// the row still live on disk.
	time.Sleep(time.Until(now.Add(2*time.Second)) + 1100*time.Millisecond)
	if m, ok, _ := st.MessageByID("expired"); !ok || m.Deleted {
		t.Fatal("the expired row was swept; this test needs it unswept")
	}
	if got := strings.Join(ids(), ","); got != "hidden,live" {
		t.Fatalf("an expired, unswept row is shown: %s", got)
	}
}

// A disappearing message written before rows carried their deadline has
// expires = 0 on the row; the deadline read out of the body still hides it.
func TestMediaHidesExpiredBodyWithoutRowDeadline(t *testing.T) {
	st := sidebarStore(t)
	mtSave(t, st, "old", "c", time.Now(), mtImage(1)+mtEph(time.Now().Add(1100*time.Millisecond)))
	if _, err := st.db.Exec(`UPDATE messages SET expires = 0 WHERE id = 'old'`); err != nil {
		t.Fatal(err)
	}
	if n := len(mtAll(t, st, "c", 10)); n != 1 {
		t.Fatalf("before its deadline: %d items", n)
	}
	time.Sleep(2200 * time.Millisecond)
	if n := len(mtAll(t, st, "c", 10)); n != 0 {
		t.Fatalf("after its deadline the gallery still shows it (%d items)", n)
	}
}

// The page repeats the feed's filters instead of trusting the index: a row
// the index still lists (here put back by hand, as a maintenance slip would
// leave it) is not shown once its message is a tombstone or a system notice.
func TestMediaPageFiltersDespiteStaleIndex(t *testing.T) {
	st := sidebarStore(t)
	now := time.Now()
	mtSave(t, st, "t", "c", now, mtImage(1))
	mtSave(t, st, "s", "c", now.Add(time.Second), mtImage(2))
	if n := len(mtAll(t, st, "c", 10)); n != 2 {
		t.Fatalf("live: %d items", n)
	}
	for _, q := range []string{
		`UPDATE messages SET deleted = 1 WHERE id = 't'`,
		`UPDATE messages SET kind = 'system' WHERE id = 's'`,
	} {
		if _, err := st.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	// The drain would now index nothing for either; put the stale rows back.
	if _, err := st.drainMedia("c", 0); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"t", "s"} {
		if _, err := st.db.Exec(`INSERT INTO media_index (message_id, ord, channel_id, tab, kind, sent, expires) VALUES (?, 0, 'c', 1, 1, ?, 0)`,
			id, now.UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	if got := mtAll(t, st, "c", 10); len(got) != 0 {
		t.Fatalf("stale index rows are shown: %v", got)
	}
}

// An unverified row replaced by the author's signed copy is indexed from the
// new body, not the relayed one.
func TestMediaReindexesProvenRow(t *testing.T) {
	st := sidebarStore(t)
	sent := time.Now()
	if _, err := st.SaveMessage(domain.Message{ID: "x", ChannelID: "c", Sender: mtSender, Sent: sent, Content: mtImage(1), Unverified: true}); err != nil {
		t.Fatal(err)
	}
	if got := mtAll(t, st, "c", 10); len(got) != 1 || got[0].kind != mediatok.Photo {
		t.Fatalf("relayed row: %v", got)
	}
	if ok, err := st.ProveRelayedMessage(domain.Message{ID: "x", ChannelID: "c", Sender: mtSender, Sent: sent, Content: mtFile(9)}); err != nil || !ok {
		t.Fatalf("prove: %v %v", ok, err)
	}
	got := mtAll(t, st, "c", 10)
	if len(got) != 1 || got[0].kind != mediatok.File {
		t.Fatalf("after the author's copy the gallery shows %v, want the file", got)
	}
}

// A page carries what the tile needs from the body, and nothing about a
// row the page did not include is opened.
func TestMediaPageItemFields(t *testing.T) {
	st := sidebarStore(t)
	now := time.Now()
	mtSave(t, st, "a", "c", now.Add(-time.Second), mtImage(7)+" hi "+mtVideo(8))
	mtSave(t, st, "b", "c", now, mtFile(9))
	items, next, err := st.MediaPage("c", mediatok.TabVisual, MediaCursor{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if next.MessageID != "" || len(items) != 2 {
		t.Fatalf("visual tab: %d items, next %+v", len(items), next)
	}
	v, p := items[0].Item, items[1].Item
	if v.Kind != mediatok.Video || v.Ord != 1 || v.Mime != "video/mp4" || v.Size != 1008 || v.Name != "clip.mp4" {
		t.Fatalf("video item %+v", v)
	}
	if p.Kind != mediatok.Photo || p.BlobID != mtBlob(7) || p.Keys != mtKeys || p.W != 100 || p.H != 52 {
		t.Fatalf("photo item %+v", p)
	}
	if !bytes.Equal(items[0].Sender, mtSender) || items[0].Sent != now.Add(-time.Second).UnixNano() {
		t.Fatalf("message fields %+v", items[0])
	}
	files, _, err := st.MediaPage("c", mediatok.TabFiles, MediaCursor{}, 10)
	if err != nil || len(files) != 1 || files[0].Item.Name != "doc.pdf" || files[0].Item.Mime != "application/pdf" {
		t.Fatalf("files tab: %+v %v", files, err)
	}
}

func TestMediaCursorRoundTrip(t *testing.T) {
	c := MediaCursor{Sent: 1234567890123456789, MessageID: "id:with:colons", Ord: 3}
	if got := ParseMediaCursor(c.String()); got != c {
		t.Fatalf("round trip %+v -> %q -> %+v", c, c.String(), got)
	}
	for _, bad := range []string{"", "x", "1:2", "a:b:c", "1:x:id"} {
		if got := ParseMediaCursor(bad); got != (MediaCursor{}) {
			t.Fatalf("%q parsed as %+v", bad, got)
		}
	}
}

// The history already on disk when the index arrives is indexed: an existing
// database (no index tables, no triggers) reopened by this build shows its
// old pictures, and a backfill gives the same index as live indexing.
func TestMediaBackfillOfExistingHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.db")
	key := bytes.Repeat([]byte{0x42}, 32)
	st, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	// Make it the database an older build left: no index, no triggers, no mark.
	for _, q := range []string{
		`DROP TRIGGER media_msg_update`, `DROP TRIGGER media_msg_delete`,
		`DROP TABLE media_index`, `DROP TABLE media_pending`, `DELETE FROM settings WHERE key = '` + mediaIndexKey + `'`,
	} {
		if _, err := st.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	for i := 0; i < 700; i++ {
		b := "words"
		if i%3 == 0 {
			b = mtImage(i)
		}
		if i%7 == 0 {
			b += mtFile(i)
		}
		mtSave(t, st, fmt.Sprintf("old%04d", i), "c", now.Add(time.Duration(i)*time.Millisecond), b)
	}
	if _, _, err := st.MarkDeleted("old0003", nil, true); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if !st.MediaPending("c") {
		t.Fatal("reopening queued nothing for the old history")
	}
	got, want := mtAll(t, st, "c", 37), mtOracle(t, st, "c")
	if len(want) < 200 || !mtEqual(got, want) {
		t.Fatalf("backfilled gallery has %d items, oracle %d", len(got), len(want))
	}
	if st.MediaPending("c") {
		t.Fatal("a page left its own channel's queue undrained")
	}
}

// The background drain empties the queue without anybody opening a page.
func TestMediaBackgroundDrain(t *testing.T) {
	old, oldDelay := mediaDrainEvery, expiresBackfillDelay
	mediaDrainEvery, expiresBackfillDelay = 50*time.Millisecond, 10*time.Millisecond
	defer func() { mediaDrainEvery, expiresBackfillDelay = old, oldDelay }()
	st := sidebarStore(t)
	for i := 0; i < 600; i++ {
		mtSave(t, st, fmt.Sprintf("q%04d", i), fmt.Sprintf("c%d", i%3), time.Now(), mtImage(i))
	}
	deadline := time.Now().Add(expiresBackfillDelay + mediaDrainAfterBackfill + 10*time.Second)
	for st.MediaPending("") {
		if time.Now().After(deadline) {
			t.Fatal("the background drain never emptied the queue")
		}
		time.Sleep(100 * time.Millisecond)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM media_index`).Scan(&n); err != nil || n != 600 {
		t.Fatalf("index has %d rows (%v), want 600", n, err)
	}
}

// The page is a walk of idx_media_page in its own order: no scan of the
// index table, no sort of the channel's items.
func TestMediaPageUsesItsIndex(t *testing.T) {
	st := sidebarStore(t)
	rows, err := st.db.Query(`EXPLAIN QUERY PLAN `+mediaPageQuery, "c", 1, 0, 0, int64(1), "x", 0, 120)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	got := strings.Join(plan, " | ")
	if !strings.Contains(got, "idx_media_page") || strings.Contains(got, "TEMP B-TREE") || strings.Contains(got, "SCAN mi") || strings.Contains(got, "SCAN m ") {
		t.Fatalf("the page query does not walk idx_media_page: %s", got)
	}
}

// ---- performance ----

// mtSeed builds the design's fixture: one channel of total messages of which
// media carry attachments (70% photos, 6% videos, 14% files, the rest a
// photo and a file together), a fifth of the media messages hidden, deleted
// or expired. Rows go in through SaveMessage, so the triggers run.
func mtSeed(tb testing.TB, st *Store, ch string, total, media int) {
	tb.Helper()
	base := time.Now().Add(-time.Duration(total) * time.Minute)
	every := total / media
	m := 0
	for i := 0; i < total; i++ {
		body := fmt.Sprintf("message %d with some ordinary words in it", i)
		if i%every == 0 && m < media {
			switch m % 50 {
			case 0, 1, 2:
				body = mtVideo(i)
			case 3, 4, 5, 6, 7, 8, 9:
				body = mtFile(i) + " the report"
			default:
				body = mtImage(i)
			}
			m++
		}
		mtSave(tb, st, fmt.Sprintf("%08d", i), ch, base.Add(time.Duration(i)*time.Minute), body)
		if i%every == 0 && m%5 == 0 {
			switch (m / 5) % 3 {
			case 0:
				_, _ = st.SetHidden(HideRec{Channel: ch, ID: fmt.Sprintf("%08d", i), Hidden: true, At: int64(i + 1)})
			case 1:
				_, _, _ = st.MarkDeleted(fmt.Sprintf("%08d", i), nil, true)
			}
		}
	}
}

// BenchmarkMediaIndexBuild is the first open of a 30,000-message channel
// with 5,000 media messages after an upgrade: the whole channel queued,
// drained by the page that asks for it.
func BenchmarkMediaIndexBuild(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		st := sidebarStore(b)
		mtSeed(b, st, "c", 30000, 5000)
		// Everything queued again, as the migration queues an old history.
		if _, err := st.db.Exec(`DELETE FROM media_index`); err != nil {
			b.Fatal(err)
		}
		if _, err := st.db.Exec(`INSERT OR IGNORE INTO media_pending (message_id, channel_id) SELECT id, channel_id FROM messages WHERE deleted = 0`); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, _, err := st.MediaPage("c", mediatok.TabVisual, MediaCursor{}, 120); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMediaPage is one page of 120 from the same channel, indexed.
func BenchmarkMediaPage(b *testing.B) {
	st := sidebarStore(b)
	mtSeed(b, st, "c", 30000, 5000)
	if _, _, err := st.MediaPage("c", mediatok.TabVisual, MediaCursor{}, 1); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.MediaPage("c", mediatok.TabVisual, MediaCursor{}, 120); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMediaWalk pages through the whole visual tab, 120 at a time: a
// fling from the top of the grid to the bottom.
func BenchmarkMediaWalk(b *testing.B) {
	st := sidebarStore(b)
	mtSeed(b, st, "c", 30000, 5000)
	if _, _, err := st.MediaPage("c", mediatok.TabVisual, MediaCursor{}, 1); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cur, n := MediaCursor{}, 0
		for {
			items, next, err := st.MediaPage("c", mediatok.TabVisual, cur, 120)
			if err != nil {
				b.Fatal(err)
			}
			n += len(items)
			if next.MessageID == "" {
				break
			}
			cur = next
		}
		if n < 3000 {
			b.Fatalf("walk saw %d items", n)
		}
	}
}

// A row inserted into messages reaches the gallery only because its inserting
// function queues it (queueMedia): there is no insert trigger, to keep plain
// messages at one b-tree write. So every INSERT INTO messages in the package
// must sit beside a queueMedia call; a new insert path fails here until it
// does.
func TestEveryMessageInsertQueuesMedia(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	inserts, queues := 0, 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		inserts += strings.Count(src, "INSERT INTO messages") + strings.Count(src, "INSERT OR IGNORE INTO messages") + strings.Count(src, "INSERT OR REPLACE INTO messages")
		queues += strings.Count(src, "queueMedia(") - strings.Count(src, "func queueMedia(")
	}
	if inserts == 0 || inserts != queues {
		t.Fatalf("%d statements insert into messages, %d queue media: every insert path must call queueMedia", inserts, queues)
	}
}

func mtDump(st *Store, ch string) string {
	var b strings.Builder
	rows, err := st.db.Query(`SELECT message_id, ord, kind, tab, sent FROM media_index WHERE channel_id = ? ORDER BY sent DESC`, ch)
	if err != nil {
		return err.Error()
	}
	for rows.Next() {
		var id string
		var ord, kind, tab int
		var sent int64
		_ = rows.Scan(&id, &ord, &kind, &tab, &sent)
		fmt.Fprintf(&b, "%s/%d k%d t%d %d; ", id, ord, kind, tab, sent)
	}
	rows.Close()
	prow, err := st.db.Query(`SELECT channel_id, message_id FROM media_pending`)
	if err == nil {
		for prow.Next() {
			var c, id string
			_ = prow.Scan(&c, &id)
			fmt.Fprintf(&b, "\n pending %s/%s", c, id)
		}
		prow.Close()
	}
	return b.String()
}
