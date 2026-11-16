package store

import (
	"testing"

	"github.com/ZahakJ/concord/internal/domain"
)

// Category ids are one key across every guild a device holds. Saving or
// removing a category under one guild leaves a category of another guild with
// the same id as it was.
func TestCategoryChangesStayInTheirGuild(t *testing.T) {
	s, _ := openTestStore(t)
	ga := domain.NewGuild("A", []byte("group-a"), []byte("owner-a"))
	gb := domain.NewGuild("B", []byte("group-b"), []byte("owner-b"))
	for _, g := range []domain.Guild{ga, gb} {
		if err := s.SaveGuild(g); err != nil {
			t.Fatalf("SaveGuild: %v", err)
		}
	}
	id := domain.NewID()
	if err := s.SaveCategory(domain.Category{ID: id, GuildID: gb.ID, Name: "Projects", Position: 1}); err != nil {
		t.Fatalf("SaveCategory: %v", err)
	}
	inB := func() []domain.Category {
		t.Helper()
		cats, err := s.Categories(gb.ID)
		if err != nil {
			t.Fatalf("Categories: %v", err)
		}
		return cats
	}

	if err := s.SaveCategory(domain.Category{ID: id, GuildID: ga.ID, Name: "Renamed", Position: 9}); err != nil {
		t.Fatalf("SaveCategory: %v", err)
	}
	if got := inB(); len(got) != 1 || got[0].Name != "Projects" || got[0].Position != 1 {
		t.Errorf("a category saved under one guild changed another guild's: %+v", got)
	}

	if err := s.DeleteCategory(ga.ID, id); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	if got := inB(); len(got) != 1 {
		t.Errorf("a category removed under one guild removed another guild's: %+v", got)
	}

	// Within its own guild, both still apply.
	if err := s.SaveCategory(domain.Category{ID: id, GuildID: gb.ID, Name: "Work", Position: 2}); err != nil {
		t.Fatalf("SaveCategory: %v", err)
	}
	if got := inB(); len(got) != 1 || got[0].Name != "Work" || got[0].Position != 2 {
		t.Errorf("a category saved in its own guild did not change: %+v", got)
	}
	if err := s.DeleteCategory(gb.ID, id); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	if got := inB(); len(got) != 0 {
		t.Errorf("a category removed in its own guild is still there: %+v", got)
	}
}
