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
		if len(msgs) == 0 {
			b.WriteString("_No messages._\n\n")
			continue
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
	return b.String(), nil
}

func editedMark(m domain.Message) string {
	if m.Edited {
		return " _(edited)_"
	}
	return ""
}
