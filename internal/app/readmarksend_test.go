package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Sending in a chat moves our own read mark to the message, here and on our
// other device, so what we wrote never sits past our own mark. A read marker
// from our own account counts only over the Notes group: the same marker in an
// ordinary guild's meta topic is dropped.
func TestSendingMovesTheReadMarkOnEveryOwnDevice(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	desk, phone, ch, _ := linkedPair(t, ctx, t.TempDir(), t.TempDir(), boot)

	msg, err := desk.SendMessage(ch, "written, so read", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	want := msg.Sent.UnixMilli()
	if at, _ := desk.store.ReadMark(ch); at < want {
		t.Fatalf("the sending device's mark %d is before its own message %d", at, want)
	}
	waitUntil(t, 30*time.Second, func() bool {
		at, _ := phone.store.ReadMark(ch)
		return at >= want
	}, "the other device's mark never reached the sent message")

	// Our own marker, but in the shared guild's meta topic rather than Notes.
	g, ok := desk.channelGuild(ch)
	if !ok {
		t.Fatal("no guild for channel")
	}
	desk.mu.RLock()
	groupID := desk.guilds[g].GroupID
	desk.mu.RUnlock()
	payload, _ := json.Marshal(guildMeta{Type: "read_marker", Markers: map[string]int64{"elsewhere": time.Now().UnixMilli()}})
	ct, err := desk.mls.Encrypt(ctx, groupID, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	phone.receiveGuildMeta(g, groupID, ct)
	if at, _ := phone.store.ReadMark("elsewhere"); at != 0 {
		t.Fatal("a read marker outside the Notes group moved a mark")
	}
	// The same marker over Notes is taken, so the drop above is the guard.
	notes, ok := desk.findNotesDM()
	if !ok {
		t.Fatal("no Notes group")
	}
	ct, err = desk.mls.Encrypt(ctx, notes.GroupID, payload)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	phone.receiveGuildMeta(notes.ID, notes.GroupID, ct)
	if at, _ := phone.store.ReadMark("elsewhere"); at == 0 {
		t.Fatal("our own read marker over Notes was not taken")
	}
}

func TestReadMarkerBatchesAreBounded(t *testing.T) {
	big := map[string]int64{}
	for i := 0; i < 3*helloReadMarks+7; i++ {
		big[fmt.Sprintf("ch%04d", i)] = int64(i + 1)
	}
	big[string(make([]byte, maxMarkIDLen+1))] = 5
	big[""] = 5
	big["zero"] = 0

	n := 0
	takeMarks(big, func(ch string, at int64) {
		if ch == "" || len(ch) > maxMarkIDLen || at <= 0 {
			t.Fatalf("took a mark no channel could have: %q=%d", ch, at)
		}
		n++
	})
	if n != helloReadMarks {
		t.Fatalf("one message applied %d marks, want at most %d", n, helloReadMarks)
	}

	seen := map[string]bool{}
	for _, b := range splitMarks(big, helloReadMarks) {
		if len(b) > helloReadMarks {
			t.Fatalf("a batch of %d marks", len(b))
		}
		for ch := range b {
			if seen[ch] {
				t.Fatalf("%q sent twice", ch)
			}
			seen[ch] = true
		}
	}
	if len(seen) != len(big) {
		t.Fatalf("split kept %d of %d marks", len(seen), len(big))
	}
}
