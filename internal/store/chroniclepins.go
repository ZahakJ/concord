package store

import (
	"database/sql"
	"fmt"
	"time"
)

// Chronicle pins: pictures of a guild's archive that this device fetched
// while reading it, kept so it can pass them on.
//
// The device that imported an archive pins its pictures (the attachments
// table's pinned column). A member who reads the archive fetches the pictures
// it scrolls past; without a pin those are cache entries no held message
// names, which the serving rule does not serve, so while the importer is
// offline nobody could pass them on. PinChronicleBlob pins such a picture,
// within a bound per guild and one for the device, and records the pin here
// so it can be taken back: when the archive is replaced by a newer one, when
// the guild is left, or when the archive is removed (ReleaseChronicleBlobs).
//
// A pin made by the import has no row here and is never taken back by this
// file: only pins this file made are removed.

const (
	// ChroniclePinsPerGuild bounds how many archive pictures one guild's
	// archive may pin on a reading member's device. A page of the archive
	// shows tens of pictures; a thousand is many evenings of reading.
	ChroniclePinsPerGuild = 1000
	// ChroniclePinBytesPerGuild bounds their total size, whichever bound is
	// reached first. 128 MiB is the thousand pictures at about 130 KiB each,
	// some 650 at the 200 KiB of a picture a messenger has recompressed, or
	// five files at the 25 MiB file limit.
	ChroniclePinBytesPerGuild = 128 << 20
	// ChroniclePinBytesTotal bounds the pins of all guilds together. A pinned
	// blob is exempt from the attachment cache's eviction but counts against
	// its 1 GiB, so this keeps at least half of that cache for ordinary
	// pictures however many archives a member reads.
	ChroniclePinBytesTotal = 512 << 20
)

func (s *Store) migrateChroniclePins() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS chronicle_pins (
  guild_id     TEXT NOT NULL,
  blob_id      TEXT NOT NULL,
  chronicle_id TEXT NOT NULL,
  size         INTEGER NOT NULL,
  at           INTEGER NOT NULL,
  PRIMARY KEY (guild_id, blob_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_chronicle_pins_blob ON chronicle_pins(blob_id)`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// PinChronicleBlob pins blobID, a picture that guildID's archive chronicleID
// names, if this device holds it and the bounds allow. It reports whether the
// blob is now pinned by any means. Past a bound it pins nothing and reports
// false: the picture stays an ordinary cache entry, shown here but not passed
// on. A blob already pinned by an import is left as it is and not recorded.
func (s *Store) PinChronicleBlob(guildID, chronicleID, blobID string) (bool, error) {
	if guildID == "" || chronicleID == "" || !hexBlobIDRe.MatchString(blobID) {
		return false, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	var size int64
	var pinned int
	err = tx.QueryRow(`SELECT size, pinned FROM attachments WHERE blob_id = ?`, blobID).Scan(&size, &pinned)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var mine int
	if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM chronicle_pins WHERE guild_id = ? AND blob_id = ?)`,
		guildID, blobID).Scan(&mine); err != nil {
		return false, err
	}
	if mine != 0 {
		// Already counted for this guild; follow the archive that names it now.
		if _, err := tx.Exec(`UPDATE chronicle_pins SET chronicle_id = ? WHERE guild_id = ? AND blob_id = ?`,
			chronicleID, guildID, blobID); err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if pinned != 0 {
		var ours int
		if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM chronicle_pins WHERE blob_id = ?)`, blobID).Scan(&ours); err != nil {
			return false, err
		}
		if ours == 0 {
			return true, nil // pinned by an import: not ours to count or take back
		}
	}
	var n, guildBytes, totalBytes int64
	if err := tx.QueryRow(`SELECT count(*), coalesce(sum(size), 0) FROM chronicle_pins WHERE guild_id = ?`,
		guildID).Scan(&n, &guildBytes); err != nil {
		return false, err
	}
	if err := tx.QueryRow(`SELECT coalesce(sum(size), 0) FROM chronicle_pins`).Scan(&totalBytes); err != nil {
		return false, err
	}
	if n+1 > ChroniclePinsPerGuild || guildBytes+size > ChroniclePinBytesPerGuild || totalBytes+size > ChroniclePinBytesTotal {
		return false, nil
	}
	if _, err := tx.Exec(`INSERT INTO chronicle_pins (guild_id, blob_id, chronicle_id, size, at) VALUES (?, ?, ?, ?, ?)`,
		guildID, blobID, chronicleID, size, time.Now().UnixNano()); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE attachments SET pinned = 1 WHERE blob_id = ?`, blobID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ChroniclePinned reports how many pins guildID's archives hold on this
// device and their total size.
func (s *Store) ChroniclePinned(guildID string) (count int, bytes int64, err error) {
	err = s.rd().QueryRow(`SELECT count(*), coalesce(sum(size), 0) FROM chronicle_pins WHERE guild_id = ?`,
		guildID).Scan(&count, &bytes)
	return count, bytes, err
}

// UnpinOtherChronicles takes back guildID's pins made for any archive other
// than keepChronicleID: the guild's archive is now another one. The pictures
// return to the cache, so they are no longer passed on from here unless
// something else keeps them; nothing is banned, since the new archive may
// name the same picture and pin it again when it is read.
func (s *Store) UnpinOtherChronicles(guildID, keepChronicleID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	if _, err := unpinChronicleTx(tx, `guild_id = ? AND chronicle_id <> ?`, guildID, keepChronicleID); err != nil {
		return err
	}
	return tx.Commit()
}

// ReleaseChronicleBlobs is for an archive that is removed: it takes back
// guildID's pins made for chronicleID and removes and bans each of those
// pictures that nothing else keeps (the rule of ReleaseBlobs: no held message
// names it, no scheduled send names it, it is in no guild GIF pack, and it is
// not pinned otherwise). A banned picture is neither served nor stored again.
// It returns the banned ids.
func (s *Store) ReleaseChronicleBlobs(guildID, chronicleID string) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	freed, err := unpinChronicleTx(tx, `guild_id = ? AND chronicle_id = ?`, guildID, chronicleID)
	if err != nil {
		return nil, err
	}
	var banned []string
	var queued map[string]bool
	for _, id := range freed {
		var kept int
		if err := tx.QueryRow(`SELECT
			EXISTS (SELECT 1 FROM blob_refs WHERE blob_id = ?1)
			OR EXISTS (SELECT 1 FROM guild_gifs WHERE id = ?1)
			OR EXISTS (SELECT 1 FROM attachments WHERE blob_id = ?1 AND pinned = 1)`, id).Scan(&kept); err != nil {
			return nil, err
		}
		if kept != 0 {
			continue
		}
		if queued == nil {
			if queued, err = s.queuedBlobIDs(tx); err != nil {
				return nil, err
			}
		}
		if queued[id] {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM attachments WHERE blob_id = ?`, id); err != nil {
			return nil, err
		}
		// No account: a ban for a removed archive is not lifted by a message.
		if _, err := tx.Exec(`INSERT INTO banned_blobs (blob_id, at) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			id, time.Now().UnixNano()); err != nil {
			return nil, err
		}
		banned = append(banned, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return banned, nil
}

// unpinChronicleTx deletes the chronicle pins matching where and clears the
// attachment pin of each blob left with no chronicle pin at all. It returns
// those blobs.
func unpinChronicleTx(q querier, where string, args ...any) ([]string, error) {
	rows, err := q.Query(`SELECT DISTINCT blob_id FROM chronicle_pins WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if _, err := q.Exec(`DELETE FROM chronicle_pins WHERE `+where, args...); err != nil {
		return nil, err
	}
	var freed []string
	for _, id := range ids {
		var left int
		if err := q.QueryRow(`SELECT EXISTS (SELECT 1 FROM chronicle_pins WHERE blob_id = ?)`, id).Scan(&left); err != nil {
			return nil, err
		}
		if left != 0 {
			continue // another guild's archive still pins it
		}
		if _, err := q.Exec(`UPDATE attachments SET pinned = 0 WHERE blob_id = ?`, id); err != nil {
			return nil, err
		}
		freed = append(freed, id)
	}
	return freed, nil
}
