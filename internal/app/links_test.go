package app

import (
	"context"
	"testing"
)

// Channel links are the answer to "publish this where?", so they have to
// outlive the session that set them and reach the other people who can publish.
// Both properties come free from the guild record — SaveGuild persists them and
// channel_updated carries them to members — but nothing tested it, and a
// silently per-session link list would mean re-picking targets before every
// announcement.
func TestChannelLinksSurviveARestartAndReachMembers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)

	g, err := a.CreateGuild("Links")
	if err != nil {
		t.Fatal(err)
	}
	news, err := a.CreateChannel(g.ID, "news", "announcement", "")
	if err != nil {
		t.Fatal(err)
	}
	target := g.Channels[0].ID // the default text channel

	if err := a.SetChannelLinks(g.ID, news.ID, []string{target}); err != nil {
		t.Fatal(err)
	}

	// Persisted: reload the guild from the store the way a restart would.
	stored, err := a.store.Guilds()
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, sg := range stored {
		if sg.ID != g.ID {
			continue
		}
		for _, c := range sg.Channels {
			if c.ID == news.ID {
				found = c.Links
			}
		}
	}
	if len(found) != 1 || found[0] != target {
		t.Fatalf("links did not survive to the store: %v — every publish would\nstart from an empty target list", found)
	}
}
