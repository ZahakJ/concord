package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
)

// ExportMarkdown renders conversation history as a readable transcript.
//
// Deliberately different from an archive: this is lossy, human-readable and
// one-way — something to keep, print or hand to someone, not something to
// restore from. ExportArchive is the restorable one.
//
// It reads the STORE rather than whatever the UI happens to have loaded. The
// version this replaces walked the in-memory message list, which is the most
// recent page plus whatever the reader had scrolled through, so "export
// history" quietly meant "export the part you already looked at" — the failure
// nobody notices until the day they need the rest.
//
// channelID exports one channel; empty exports every channel in the guild.
func (s *Service) ExportMarkdown(guildID, channelID string) (string, error) {
	s.mu.RLock()
	g, ok := s.guilds[guildID]
	var channels []domain.Channel
	if ok {
		channels = append(channels, g.Channels...)
	}
	guildName := ""
	if ok {
		guildName = g.Name
	}
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("app: no such guild")
	}

	if channelID != "" {
		var only []domain.Channel
		for _, c := range channels {
			if c.ID == channelID {
				only = append(only, c)
			}
		}
		if len(only) == 0 {
			return "", fmt.Errorf("app: no such channel in this guild")
		}
		channels = only
	}

	// Stable order so two exports of an unchanged guild are byte-identical:
	// diffable, and no spurious churn if someone keeps them in version control.
	sort.SliceStable(channels, func(i, j int) bool {
		if channels[i].Position != channels[j].Position {
			return channels[i].Position < channels[j].Position
		}
		return channels[i].Name < channels[j].Name
	})

	arch := s.exportArchive(guildID, channelID)

	var b strings.Builder
	if channelID == "" {
		fmt.Fprintf(&b, "# %s\n\n", guildName)
		fmt.Fprintf(&b, "_Exported %s. Voice channels carry no transcript._\n\n",
			time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	}

	for _, c := range channels {
		if c.Type == "voice" {
			continue
		}
		msgs, err := s.store.Messages(c.ID, 0) // 0 = the whole channel, not a page
		if err != nil {
			return "", fmt.Errorf("app: read %s: %w", c.Name, err)
		}
		// Messages come back newest-first; a transcript reads the other way.
		sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Sent.Before(msgs[j].Sent) })

		if channelID == "" {
			fmt.Fprintf(&b, "## #%s\n\n", c.Name)
			if c.Topic != "" {
				fmt.Fprintf(&b, "_%s_\n\n", c.Topic)
			}
		} else {
			fmt.Fprintf(&b, "# #%s\n\n", c.Name)
		}
		// The imported history that sits above this channel comes first,
		// because it is older than anything said here.
		archived := arch.writeMapped(&b, c.ID)
		if len(msgs) == 0 {
			if !archived {
				b.WriteString("_No messages._\n\n")
			}
			continue
		}
		if archived {
			b.WriteString("### Since then, in this guild\n\n")
		}
		for _, m := range msgs {
			if m.Deleted {
				continue // a transcript of what was said, not of what was retracted
			}
			who := m.Name
			if who == "" {
				who = accountFingerprintOf(m.Sender)
			}
			if m.Kind == "system" {
				fmt.Fprintf(&b, "> %s %s\n\n", who, m.Content)
				continue
			}
			fmt.Fprintf(&b, "**%s** (%s):%s\n%s\n\n",
				who, m.Sent.UTC().Format(time.RFC3339), editedMark(m), m.Content)
		}
	}
	if channelID == "" {
		arch.writeUnmapped(&b)
	}
	arch.writeMissing(&b)
	return b.String(), nil
}

// exportedArchive is a guild's imported archive as an export reads it: every
// page this device holds or can fetch without a metered connection, opened,
// grouped by the guild channel it was put above.
//
// "Export history" used to leave the archive out entirely: a guild carrying
// forty thousand imported messages exported a page of system lines, and the
// history people had imported precisely so it would not be lost was the one
// part missing from the copy they took away.
type exportedArchive struct {
	m        chronicleManifest
	ok       bool
	byChan   map[string][]chronicleMessage // source channel id -> messages, oldest first
	missing  int                           // messages in pages that could not be read
	unmapped []chronicleChannel
	mapped   map[string][]chronicleChannel // guild channel id -> source channels
}

// channelID narrows it to the history put above that one channel ("" for all).
func (s *Service) exportArchive(guildID, channelID string) *exportedArchive {
	a := &exportedArchive{byChan: map[string][]chronicleMessage{}, mapped: map[string][]chronicleChannel{}}
	m, _, ok := s.guildChronicle(guildID)
	if !ok {
		return a
	}
	a.m, a.ok = m, true
	for _, c := range m.Channels {
		if c.Mapped == "" {
			a.unmapped = append(a.unmapped, c)
		} else {
			a.mapped[c.Mapped] = append(a.mapped[c.Mapped], c)
		}
	}
	wanted := map[string]bool{}
	for _, c := range m.Channels {
		wanted[c.ID] = channelID == "" || c.Mapped == channelID
	}
	for _, ref := range m.Chunks {
		if !wanted[ref.Channel] {
			continue
		}
		ct, err := s.chronicleChunk(guildID, ref.ID, false)
		if err != nil {
			a.missing += ref.Count
			continue
		}
		msgs, err := openChronicleChunk(ct, ref.Keys)
		if err != nil {
			a.missing += ref.Count
			continue
		}
		a.byChan[ref.Channel] = append(a.byChan[ref.Channel], msgs...)
	}
	for id := range a.byChan {
		msgs := a.byChan[id]
		sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Nano < msgs[j].Nano })
	}
	return a
}

// writeMapped writes the archived history put above one guild channel, and
// reports whether there was any.
func (a *exportedArchive) writeMapped(b *strings.Builder, channelID string) bool {
	wrote := false
	for _, src := range a.mapped[channelID] {
		msgs := a.byChan[src.ID]
		if len(msgs) == 0 {
			continue
		}
		fmt.Fprintf(b, "### Imported from %s (#%s)\n\n", a.m.Source, src.Name)
		a.writeMessages(b, msgs)
		wrote = true
	}
	return wrote
}

// writeUnmapped writes the archived channels that were put above no channel
// here, at the end of a whole-guild export.
func (a *exportedArchive) writeUnmapped(b *strings.Builder) {
	for _, src := range a.unmapped {
		msgs := a.byChan[src.ID]
		if len(msgs) == 0 {
			continue
		}
		fmt.Fprintf(b, "## #%s (imported from %s)\n\n", src.Name, a.m.Source)
		a.writeMessages(b, msgs)
	}
}

// writeMissing says how much of the archive this export could not include,
// rather than letting a short file pass for a complete one.
func (a *exportedArchive) writeMissing(b *strings.Builder) {
	if a.missing > 0 {
		fmt.Fprintf(b, "_%d imported messages are not in this export: their pages are not on this device and could not be fetched (a metered connection, or nobody online holding them)._\n", a.missing)
	}
}

func (a *exportedArchive) writeMessages(b *strings.Builder, msgs []chronicleMessage) {
	for _, m := range msgs {
		who := "someone"
		if m.Author >= 0 && m.Author < len(a.m.Authors) && a.m.Authors[m.Author].Name != "" {
			who = a.m.Authors[m.Author].Name
		}
		body := m.Content
		if n := len(m.Attach); n > 0 {
			if body != "" {
				body += "\n"
			}
			body += fmt.Sprintf("_(%d attachment%s)_", n, map[bool]string{true: "", false: "s"}[n == 1])
		}
		fmt.Fprintf(b, "**%s** (%s):\n%s\n\n", who, time.Unix(0, m.Nano).UTC().Format(time.RFC3339), body)
	}
}

func editedMark(m domain.Message) string {
	if m.Edited {
		return " _(edited)_"
	}
	return ""
}
