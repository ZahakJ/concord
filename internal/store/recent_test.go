package store

import (
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// recentIDs reads RecentMedia in pages of `page` and returns "id/ord@channel"
// for every item, newest first.
func recentIDs(t *testing.T, st *Store, page int) string {
	t.Helper()
	var out []string
	cur := MediaCursor{}
	for i := 0; ; i++ {
		items, next, err := st.RecentMedia(cur, page)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			out = append(out, it.MessageID+"@"+it.ChannelID)
		}
		if next.MessageID == "" {
			break
		}
		if i > 1000 {
			t.Fatal("paging does not end")
		}
		cur = next
	}
	return strings.Join(out, ",")
}

// The attach menu's Recent strip spans every channel, newest first, and
// leaves out everything the gallery leaves out (deleted, hidden, expired,
// system rows) and, stricter than the gallery, any row its sender did not
// sign (an unverified relayed copy, a guest's message) and any disappearing
// message, even one whose deadline is an hour away.
func TestRecentMediaFilters(t *testing.T) {
	st := sidebarStore(t)
	now := time.Now()
	mtSave(t, st, "old", "c1", now.Add(-9*time.Second), mtImage(1))
	mtSave(t, st, "file", "c2", now.Add(-8*time.Second), mtFile(2))
	mtSave(t, st, "deleted", "c1", now.Add(-7*time.Second), mtImage(3))
	mtSave(t, st, "hidden", "c2", now.Add(-6*time.Second), mtImage(4))
	mtSave(t, st, "expired", "c1", now.Add(-5*time.Second), mtImage(5)+mtEph(now.Add(-time.Second)))
	mtSave(t, st, "system", "c2", now.Add(-4*time.Second), mtImage(6))
	mtSave(t, st, "plain", "c1", now.Add(-3*time.Second), "no attachment here")
	// Still live, but sent to disappear: never offered for a permanent copy.
	mtSave(t, st, "disappearing", "c2", now.Add(-2500*time.Millisecond), mtImage(11)+mtEph(now.Add(time.Hour)))
	mtSave(t, st, "row-deadline", "c1", now.Add(-2200*time.Millisecond), mtImage(12))
	if _, err := st.db.Exec(`UPDATE messages SET expires = ? WHERE id = 'row-deadline'`, now.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	for i, m := range []domain.Message{
		{ID: "copy", ChannelID: "c1", Sender: mtSender, Sent: now.Add(-2 * time.Second), Content: mtImage(7), Unverified: true},
		{ID: "guest", ChannelID: "c2", Sender: mtSender, Kind: "guest", Sent: now.Add(-time.Second), Content: mtImage(8)},
	} {
		if _, err := st.SaveMessage(m); err != nil {
			t.Fatal(i, err)
		}
	}
	mtSave(t, st, "new", "c3", now, mtVideo(9)+mtImage(10))
	if _, err := st.db.Exec(`UPDATE messages SET kind = 'system' WHERE id = 'system'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MarkDeleted("deleted", nil, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetHidden(HideRec{Channel: "c2", ID: "hidden", Hidden: true, At: 10}); err != nil {
		t.Fatal(err)
	}
	want := "new@c3,new@c3,file@c2,old@c1"
	for _, page := range []int{1, 2, 50} {
		if got := recentIDs(t, st, page); got != want {
			t.Fatalf("page %d: recent shows %s, want %s", page, got, want)
		}
	}
	items, _, err := st.RecentMedia(MediaCursor{}, 1)
	if err != nil || len(items) != 1 || items[0].Item.Ord != 1 || items[0].Item.BlobID != mtBlob(10) {
		t.Fatalf("newest item %+v %v, want the second attachment of the newest message", items, err)
	}
}

// Opening the attach menu must not sort the whole index: the query walks
// idx_media_recent in order.
func TestRecentMediaUsesItsIndex(t *testing.T) {
	st := sidebarStore(t)
	rows, err := st.db.Query(`EXPLAIN QUERY PLAN `+mediaRecentQuery, int64(1), "x", 0, 48)
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
	if !strings.Contains(got, "idx_media_recent") || strings.Contains(got, "TEMP B-TREE") {
		t.Fatalf("the recent query does not walk idx_media_recent: %s", got)
	}
}
