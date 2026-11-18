package store

import "fmt"

// Import pins: the guild each picture an import sealed on this device belongs
// to.
//
// The device that imports an archive pins its pictures (the attachments
// table's pinned column), and records here the guild the archive was
// imported into. Such a picture is passed on only to members of that guild
// (BlobServeScope). A pin made before the guild was recorded has no row here
// until ScopeImportPins gives it one, and is passed on to nobody meanwhile.

func (s *Store) migrateImportPins() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS import_pins (
  guild_id TEXT NOT NULL,
  blob_id  TEXT NOT NULL,
  PRIMARY KEY (guild_id, blob_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_import_pins_blob ON import_pins(blob_id)`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// PinImportedBlob pins blobID, a picture an import into guildID sealed on
// this device, and records the guild. A blob this device does not hold is
// left alone. An empty guildID pins without recording a guild.
func (s *Store) PinImportedBlob(guildID, blobID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	res, err := tx.Exec(`UPDATE attachments SET pinned = 1 WHERE blob_id = ?`, blobID)
	if err != nil {
		return fmt.Errorf("store: pin imported blob: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 || guildID == "" {
		return tx.Commit()
	}
	if _, err := tx.Exec(`INSERT INTO import_pins (guild_id, blob_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
		guildID, blobID); err != nil {
		return fmt.Errorf("store: pin imported blob: %w", err)
	}
	return tx.Commit()
}

// UnscopedPins returns the pinned blobs that have neither an import's guild
// nor an archive reader's pin recorded: the pins an import made before the
// guild was recorded.
func (s *Store) UnscopedPins() (map[string]bool, error) {
	rows, err := s.rd().Query(`SELECT blob_id FROM attachments a WHERE a.pinned = 1
		AND NOT EXISTS (SELECT 1 FROM import_pins p WHERE p.blob_id = a.blob_id)
		AND NOT EXISTS (SELECT 1 FROM chronicle_pins c WHERE c.blob_id = a.blob_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ScopeImportPins records guildID for each of ids that is pinned and that no
// archive reader pinned: the caller passes the pictures of guildID's archives
// among the pins UnscopedPins returned, which an import made before the guild
// was recorded.
func (s *Store) ScopeImportPins(guildID string, ids []string) error {
	if guildID == "" || len(ids) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	for _, id := range ids {
		if _, err := tx.Exec(`INSERT INTO import_pins (guild_id, blob_id)
			SELECT ?1, ?2 WHERE EXISTS (SELECT 1 FROM attachments WHERE blob_id = ?2 AND pinned = 1)
			AND NOT EXISTS (SELECT 1 FROM chronicle_pins WHERE blob_id = ?2)
			ON CONFLICT DO NOTHING`, guildID, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
