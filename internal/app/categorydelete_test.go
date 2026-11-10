package app

import (
	"context"
	"testing"
)

// TestDeletingACategoryAppendsItsChannels pins where a deleted category's
// channels land: after the uncategorized ones already there, in their old
// order, rather than interleaved by their in-category position.
func TestDeletingACategoryAppendsItsChannels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	g, err := s.CreateGuild("Shelves")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	cat, err := s.CreateCategory(g.ID, "Staff Room")
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	second, _ := s.CreateChannel(g.ID, "general-two", "", "")
	news, _ := s.CreateChannel(g.ID, "news", "", "")
	ideas, _ := s.CreateChannel(g.ID, "ideas", "", "")
	general := g.Channels[0].ID
	_ = s.SetChannelMeta(g.ID, general, "", "", 0, "")
	_ = s.SetChannelMeta(g.ID, second.ID, "", "", 1, "")
	_ = s.SetChannelMeta(g.ID, news.ID, "", cat.ID, 0, "")
	_ = s.SetChannelMeta(g.ID, ideas.ID, "", cat.ID, 1, "")

	if err := s.DeleteCategory(g.ID, cat.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	pos := map[string]int{}
	for _, c := range s.Guilds() {
		if c.ID != g.ID {
			continue
		}
		for _, ch := range c.Channels {
			if ch.Category != "" {
				t.Fatalf("%s is still in a category", ch.Name)
			}
			pos[ch.ID] = ch.Position
		}
	}
	if !(pos[general] < pos[second.ID] && pos[second.ID] < pos[news.ID] && pos[news.ID] < pos[ideas.ID]) {
		t.Fatalf("order after delete: general=%d general-two=%d news=%d ideas=%d", pos[general], pos[second.ID], pos[news.ID], pos[ideas.ID])
	}
}
