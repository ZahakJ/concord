package app

import (
	"strings"
	"testing"

	"github.com/ZahakJ/concord/internal/chronimport"
	"github.com/ZahakJ/concord/internal/domain"
)

// "Export history" left the imported archive out entirely: a guild carrying
// thousands of imported messages exported its own few lines and nothing else.
func TestExportIncludesTheImportedArchive(t *testing.T) {
	s := importTestService(t)
	dir, _ := importFixture(t)
	_, acc := runImportOffline(t, s, dir, chronimport.DefaultPolicy())
	channels := acc.manifestChannels()
	raw, chunks, err := s.buildChronicle("g1", "Test Guild, imported today", "",
		channels, acc.authors, acc.byChannel, acc.sealed, acc.sealedBytes)
	if err != nil {
		t.Fatalf("buildChronicle: %v", err)
	}
	if _, err := s.store.SaveChronicleManifest(chronicleIDOf(raw), "g1", raw); err != nil {
		t.Fatalf("SaveChronicleManifest: %v", err)
	}
	for id, ct := range chunks {
		if err := s.store.SaveChronicleChunk(id, ct, true); err != nil {
			t.Fatalf("SaveChronicleChunk: %v", err)
		}
	}
	// The guild has the channels the import mapped onto.
	for _, c := range channels {
		s.guilds["g1"].Channels = append(s.guilds["g1"].Channels,
			domain.Channel{ID: c.Mapped, GuildID: "g1", Name: c.Name, Type: "text"})
	}

	md, err := s.ExportMarkdown("g1", "")
	if err != nil {
		t.Fatalf("ExportMarkdown: %v", err)
	}
	var want []string
	for _, msgs := range acc.byChannel {
		for _, m := range msgs {
			if m.Content != "" && len(want) < 20 {
				want = append(want, m.Content)
			}
		}
	}
	if len(want) == 0 {
		t.Fatal("the fixture archived no text to look for")
	}
	for _, w := range want {
		if !strings.Contains(md, w) {
			t.Fatalf("the export leaves out the archived message %q", w)
		}
	}
	if !strings.Contains(md, "Imported from Test Guild, imported today") {
		t.Fatal("the export does not say where the archived history came from")
	}
}
