package app

import (
	"context"
	"testing"
	"time"
)

// A forged edit (signed by somebody who is not the author) is refused by the
// store and must leave no version behind.
func TestForgedEditLeavesNoVersion(t *testing.T) {
	author := mustID(t)
	svc := storyTestService(t)
	row := signedMessage(author, "ch1", "meet at eight")
	if _, err := svc.store.SaveMessage(row); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}
	svc.applyEdit("act-versions_test.go-18", row.ID, "meet at midnight", mustID(t).PublicKey(), nil, "")
	vs, err := svc.MessageVersions("ch1", row.ID)
	if err != nil || len(vs) != 0 {
		t.Fatalf("forged edit left versions: %v %v", vs, err)
	}
	svc.applyEdit("act-versions_test.go-23", row.ID, "meet at nine", author.PublicKey(), nil, "")
	vs, _ = svc.MessageVersions("ch1", row.ID)
	if len(vs) != 2 || vs[0].Content != "meet at eight" || vs[1].Content != "meet at nine" {
		t.Fatalf("author edit versions wrong: %v", vs)
	}
}

func TestEditHistoryReachesAPeer(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("edit-history")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)
	sent, err := a.SendMessage(channel, "one", "", "")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	waitFor(t, 20*time.Second, func() bool {
		row, ok, err := b.store.MessageByID(sent.ID)
		return err == nil && ok && row.Content == "one"
	}, "B never received the original")
	for _, body := range []string{"two", "three"} {
		if err := a.EditMessage(channel, sent.ID, body, ""); err != nil {
			t.Fatalf("EditMessage: %v", err)
		}
		waitFor(t, 20*time.Second, func() bool {
			row, ok, err := b.store.MessageByID(sent.ID)
			return err == nil && ok && row.Content == body
		}, "B never applied edit "+body)
	}
	vs, err := b.MessageVersions(channel, sent.ID)
	if err != nil || len(vs) != 3 || vs[0].Content != "one" || vs[1].Content != "two" || vs[2].Content != "three" {
		t.Fatalf("B versions: %v %v", vs, err)
	}
	for i := 1; i < len(vs); i++ {
		if vs[i].At.Before(vs[i-1].At) {
			t.Fatalf("versions not in time order: %v", vs)
		}
	}
}
