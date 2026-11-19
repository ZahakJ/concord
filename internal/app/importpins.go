package app

import "github.com/ZahakJ/concord/internal/store"

// importPinsScopedKey records that scopeImportPinsOnce has run to the end on
// this device.
const importPinsScopedKey = "app.import_pins_scoped"

// scopeImportPinsOnce gives each picture an earlier import pinned on this
// device the guild it was imported into, one time per device.
//
// An import now records the guild with each pin (store.PinImportedBlob), and
// a pinned picture is passed on only to members of a guild recorded for it.
// A pin made before that has no guild and is passed on to nobody, so its
// archive's pictures would reach no member while the importer alone holds
// them. This run reads the archives of each guild held here, and records the
// guild for each such pin the archive's pages name. A pin no held page names
// keeps no guild.
//
// The run is recorded only when it finishes. One stopped by shutdown, or by
// an error reading the store, starts again at the next launch.
func (s *Service) scopeImportPinsOnce() {
	if v, err := s.store.GetSetting(importPinsScopedKey); err != nil || v != "" {
		return
	}
	unscoped, err := s.store.UnscopedPins()
	if err != nil {
		return
	}
	if len(unscoped) > 0 {
		s.mu.RLock()
		guilds := make([]string, 0, len(s.guilds))
		for id := range s.guilds {
			guilds = append(guilds, id)
		}
		s.mu.RUnlock()
		for _, g := range guilds {
			if s.ctx.Err() != nil {
				return
			}
			ids, err := s.archivePictures(g, unscoped)
			if err != nil {
				return
			}
			if err := s.store.ScopeImportPins(g, ids); err != nil {
				return
			}
		}
	}
	_ = s.store.SetSetting(importPinsScopedKey, "1")
}

// archivePictures returns the pictures among want that the pages of
// guildID's archives held on this device name. A page not held here, or one
// that does not open, is skipped; an error reading the store is returned.
func (s *Service) archivePictures(guildID string, want map[string]bool) ([]string, error) {
	rows, err := s.store.ChronicleManifests(guildID)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, r := range rows {
		m, err := decodeChronicleManifest(r.Raw)
		if err != nil || m.GuildID != guildID {
			continue
		}
		for _, ref := range m.Chunks {
			ct, ok, err := s.store.ChronicleChunkStale(ref.ID)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			msgs, err := openChronicleChunk(ct, ref.Keys)
			if err != nil {
				continue
			}
			for _, msg := range msgs {
				for _, tok := range msg.Attach {
					for _, id := range store.BlobIDsIn(tok) {
						if want[id] && !seen[id] {
							seen[id] = true
							out = append(out, id)
						}
					}
				}
			}
		}
	}
	return out, nil
}
