package app

import (
	"context"
	"testing"
)

// A moderator's reveal answers for a deleted message in the channel it names,
// and for nothing else: not the same message named through another channel,
// and not a message that is still live.
func TestRevealAnswersOnlyForADeletedMessageInItsChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	g, err := owner.CreateGuild("reveal")
	if err != nil {
		t.Fatal(err)
	}
	a := g.Channels[0].ID
	other, err := owner.CreateChannel(g.ID, "other", "text", "")
	if err != nil {
		t.Fatal(err)
	}
	b := other.ID
	live, err := owner.SendMessage(a, "still live", "", "")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := owner.SendMessage(b, "deleted in b", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.DeleteMessage(b, gone.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := owner.RevealDeleted(b, gone.ID); err != nil || got != "deleted in b" {
		t.Errorf("reveal in its own channel: %q %v", got, err)
	}
	if got, err := owner.RevealDeleted(a, gone.ID); err == nil {
		t.Errorf("a reveal named through another channel answered %q", got)
	}
	if got, err := owner.RevealDeleted(a, live.ID); err == nil {
		t.Errorf("a reveal of a live message answered %q", got)
	}
}
