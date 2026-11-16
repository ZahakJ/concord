package store

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Blob references: which held message bodies name which attachment blob.
//
// A blob's id and key travel only inside message bodies (attach.go), so the
// store learns a message's blobs when it seals the body: from its image and
// file tokens and from the picture of each link preview it carries. A blob
// whose last referencing message is deleted for everyone or expires on this
// device is removed and banned here, unless something else keeps it: another
// message this device holds, the guild GIF pack, or a pin (a chronicle
// import pins its pictures). A banned blob is neither served to peers nor
// stored again from a fetch or an archive.
//
// The index is filled at every insert and body change. Rows stored before it
// existed are indexed by a bounded backfill at startup (BackfillBlobRefs);
// until that has finished, ReleaseBlobs removes and bans nothing, since a
// blob shared with a row not yet indexed would look unused.

const (
	settingBlobRefsComplete = "blob_refs_complete"
	settingBlobRefsCursor   = "blob_refs_backfill_rowid"
	// blobRefsBatch bounds one backfill transaction, as pruneBatch does a
	// prune: the store runs on one connection.
	blobRefsBatch = 2000
	// lpTokenMax bounds the link-preview payload read for picture ids, as
	// the app does on receipt (linkEmbedTokenMax).
	lpTokenMax = 8192
)

var (
	// The blob id inside an image or file token. Mirrors the token grammar in
	// internal/app/attach.go: the id is the first path segment after the
	// version in both.
	blobTokenIDRe = regexp.MustCompile(`concord://(?:attach/v[12]|file/v1)/([0-9a-f]{64})/`)
	lpTokenRe     = regexp.MustCompile(`\[lp\]\(concord://lp/v1/([A-Za-z0-9_-]+)\)`)
	hexBlobIDRe   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func (s *Store) migrateBlobRefs() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS blob_refs (
  blob_id    TEXT NOT NULL,
  message_id TEXT NOT NULL,
  PRIMARY KEY (blob_id, message_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS idx_blob_refs_message ON blob_refs(message_id);
CREATE TABLE IF NOT EXISTS banned_blobs (
  blob_id TEXT PRIMARY KEY,
  at      INTEGER NOT NULL
)`); err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	return nil
}

// BlobIDsIn returns the attachment blob ids a message body names: those of
// its image and file tokens and of its link previews' pictures, each once.
func BlobIDsIn(body string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if hexBlobIDRe.MatchString(id) && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, m := range blobTokenIDRe.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	for _, m := range lpTokenRe.FindAllStringSubmatch(body, -1) {
		if len(m[1]) > lpTokenMax {
			continue
		}
		raw, err := base64.RawURLEncoding.DecodeString(m[1])
		if err != nil {
			continue
		}
		var embeds []struct {
			Image *struct {
				Blob string `json:"b"`
			} `json:"i"`
		}
		if json.Unmarshal(raw, &embeds) != nil {
			continue
		}
		for _, e := range embeds {
			if e.Image != nil {
				add(e.Image.Blob)
			}
		}
	}
	return out
}

// noteBlobRefs records the blobs body names as referenced by messageID.
// References are only added here: a body that changes (an edit) keeps the
// blobs of its earlier versions, which the message still holds until it is
// deleted.
func noteBlobRefs(q querier, messageID, body string) error {
	for _, id := range BlobIDsIn(body) {
		if _, err := q.Exec(`INSERT INTO blob_refs (blob_id, message_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, messageID); err != nil {
			return fmt.Errorf("store: note blob refs: %w", err)
		}
	}
	return nil
}

// BlobRefsComplete reports whether every stored row has been indexed.
func (s *Store) BlobRefsComplete() bool {
	v, err := s.GetSetting(settingBlobRefsComplete)
	return err == nil && v == "1"
}

// BackfillBlobRefs indexes up to one batch of rows stored before the index
// existed, in rowid order from where the last batch stopped, and reports
// whether the backfill is complete. The caller loops, giving other readers
// the connection back between batches.
func (s *Store) BackfillBlobRefs() (bool, error) {
	if s.BlobRefsComplete() {
		return true, nil
	}
	cur, _ := s.GetSetting(settingBlobRefsCursor)
	after, _ := strconv.ParseInt(cur, 10, 64)
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	rows, err := tx.Query(`SELECT rowid, id, content_enc, nonce FROM messages WHERE rowid > ? ORDER BY rowid LIMIT ?`, after, blobRefsBatch)
	if err != nil {
		return false, err
	}
	type row struct {
		id         string
		enc, nonce []byte
	}
	var batch []row
	last := after
	for rows.Next() {
		var r row
		if err := rows.Scan(&last, &r.id, &r.enc, &r.nonce); err != nil {
			rows.Close()
			return false, err
		}
		batch = append(batch, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, r := range batch {
		body, err := s.open(r.enc, r.nonce)
		if err != nil {
			continue
		}
		if err := noteBlobRefs(tx, r.id, body); err != nil {
			return false, err
		}
	}
	done := len(batch) < blobRefsBatch
	key, val := settingBlobRefsCursor, strconv.FormatInt(last, 10)
	if done {
		key, val = settingBlobRefsComplete, "1"
	}
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, val); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return done, nil
}

// ReleaseBlobs drops messageID's blob references, and removes and bans each
// of its blobs that nothing else keeps: no other held message names it, it
// is in no guild GIF pack, and it is not pinned. It returns the banned ids.
// Until the backfill is complete only the references are dropped.
func (s *Store) ReleaseBlobs(messageID string) ([]string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit
	banned, err := releaseBlobs(tx, messageID, s.BlobRefsComplete())
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return banned, nil
}

func releaseBlobs(q querier, messageID string, complete bool) ([]string, error) {
	rows, err := q.Query(`SELECT blob_id FROM blob_refs WHERE message_id = ?`, messageID)
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
	if _, err := q.Exec(`DELETE FROM blob_refs WHERE message_id = ?`, messageID); err != nil {
		return nil, err
	}
	if !complete {
		return nil, nil
	}
	var banned []string
	for _, id := range ids {
		var kept int
		err := q.QueryRow(`SELECT
			EXISTS (SELECT 1 FROM blob_refs WHERE blob_id = ?1)
			OR EXISTS (SELECT 1 FROM guild_gifs WHERE id = ?1)
			OR EXISTS (SELECT 1 FROM attachments WHERE blob_id = ?1 AND pinned = 1)`, id).Scan(&kept)
		if err != nil {
			return nil, err
		}
		if kept != 0 {
			continue
		}
		if _, err := q.Exec(`DELETE FROM attachments WHERE blob_id = ?`, id); err != nil {
			return nil, err
		}
		if _, err := q.Exec(`INSERT INTO banned_blobs (blob_id, at) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, time.Now().UnixNano()); err != nil {
			return nil, err
		}
		banned = append(banned, id)
	}
	return banned, nil
}

// BlobBanned reports whether blobID was removed and banned on this device.
func (s *Store) BlobBanned(blobID string) bool {
	var one int
	err := s.rd().QueryRow(`SELECT 1 FROM banned_blobs WHERE blob_id = ?`, blobID).Scan(&one)
	return err == nil
}

// DeletedWithBlobs lists the deleted messages, in the given channels or on
// the whole device when channelIDs is empty, that still hold blob
// references: what Empty trash releases.
func (s *Store) DeletedWithBlobs(channelIDs []string) ([]string, error) {
	q := `SELECT DISTINCT r.message_id FROM blob_refs r JOIN messages m ON m.id = r.message_id WHERE m.deleted = 1`
	var args []any
	if len(channelIDs) > 0 {
		q += ` AND m.channel_id IN (` + placeholders(len(channelIDs)) + `)`
		for _, c := range channelIDs {
			args = append(args, c)
		}
	}
	rows, err := s.rd().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// placeholders is n comma-separated SQL parameters.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, 2*n)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}
