package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// UnreadRows is exactly the rows UnreadCounts counts, past the cursor, oldest
// first: so the client's exact tally can never disagree with the cheap one
// about which rows are in play.
func TestUnreadRowsMatchUnreadCounts(t *testing.T) {
	st, _ := openTestStore(t)
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	save := func(id, kind, content string, at int) {
		if _, err := st.SaveMessage(domain.Message{ID: id, ChannelID: "c", Sender: []byte("s"), Kind: kind,
			Content: content, Sent: base.Add(time.Duration(at) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	save("old", "", "before the cursor", 1)
	save("a", "", "hello", 3)
	save("sys", "system", "joined", 4)
	save("game", "", "[game](concord://game/v1/eyJrIjoibXYiLCJpIjoiYWIxMjM0In0)", 5)
	save("b", "guest", "a guest line", 6)
	save("gone", "", "deleted later", 7)
	if _, err := st.db.Exec(`UPDATE messages SET deleted = 1 WHERE id = 'gone'`); err != nil {
		t.Fatal(err)
	}
	cursor := base.Add(2 * time.Minute).UnixNano()
	rows, err := st.UnreadRows("c", cursor, 200)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range rows {
		ids = append(ids, m.ID)
	}
	if fmt.Sprint(ids) != "[a b]" {
		t.Fatalf("UnreadRows = %v, want [a b]", ids)
	}
	if rows[0].Content != "hello" {
		t.Fatalf("body not opened: %q", rows[0].Content)
	}
	counts, err := st.UnreadCounts(map[string]int64{"c": cursor})
	if err != nil {
		t.Fatal(err)
	}
	if counts["c"] != len(rows) {
		t.Fatalf("UnreadCounts %d, UnreadRows %d", counts["c"], len(rows))
	}
	// The newest `limit`, still oldest first.
	two, _ := st.UnreadRows("c", 0, 2)
	if len(two) != 2 || two[0].ID != "a" || two[1].ID != "b" {
		t.Fatalf("limited UnreadRows = %+v", two)
	}
}
