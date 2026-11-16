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
//
// The backfill keeps its rowid cursor after it finishes, and Close moves it
// to the newest row, since every row this build writes is indexed as it is
// written. At the next start the backfill indexes whatever lies past the
// cursor before any blob may go again: rows an older build wrote to the same
// store in between, or rows of a run that ended without Close. After a clean
// close nothing lies past it, and the check is one query.

const (
	settingBlobRefsCursor = "blob_refs_backfill_rowid"
	// blobRefsBatch bounds one backfill transaction, as pruneBatch does a
	// prune: the store runs on one connection.
	blobRefsBatch = 2000
	// blobRefsBatchRefs ends a backfill transaction early once it has written
	// about this many references, so a batch of long bodies holds the
	// connection no longer than one of ordinary ones.
	blobRefsBatchRefs = 20000
	// blobRefsBatchBytes ends a backfill batch early once it has read this
	// many bytes of sealed bodies, which bounds the batch's memory and the
	// time spent opening bodies.
	blobRefsBatchBytes = 4 << 20
	// blobRefsPerBody is the most blob ids one body is indexed under: far
	// above the pictures, files and link previews of any message a person
	// writes. Reading stops there, so a body's cost to store and to release
	// is bounded by this, not by its length.
	blobRefsPerBody = 64
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
// its image and file tokens and of its link previews' pictures, each once, at
// most blobRefsPerBody of them. Reading stops at the cap, and at most
// blobRefsPerBody link-preview tokens are decoded.
func BlobIDsIn(body string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) bool {
		if hexBlobIDRe.MatchString(id) && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
		return len(out) < blobRefsPerBody
	}
	for rest := body; ; {
		loc := blobTokenIDRe.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		if !add(rest[loc[2]:loc[3]]) {
			return out
		}
		rest = rest[loc[1]:]
	}
	decoded := 0
	for rest := body; decoded < blobRefsPerBody; decoded++ {
		loc := lpTokenRe.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		payload := rest[loc[2]:loc[3]]
		rest = rest[loc[1]:]
		if len(payload) > lpTokenMax {
			continue
		}
		raw, err := base64.RawURLEncoding.DecodeString(payload)
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
			if e.Image != nil && !add(e.Image.Blob) {
				return out
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
	return insertBlobRefs(q, messageID, BlobIDsIn(body))
}

func insertBlobRefs(q querier, messageID string, ids []string) error {
	for _, id := range ids {
		if _, err := q.Exec(`INSERT INTO blob_refs (blob_id, message_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, messageID); err != nil {
			return fmt.Errorf("store: note blob refs: %w", err)
		}
	}
	return nil
}

// BlobRefsComplete reports whether every stored row has been indexed, as
// far as this process has checked (BackfillBlobRefs).
func (s *Store) BlobRefsComplete() bool {
	return s.blobRefsLive.Load()
}

// BackfillBlobRefs indexes up to one batch of the rows past the backfill's
// cursor, in rowid order from where the last batch stopped, and reports
// whether every row is now indexed. The caller loops, giving other readers
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
		rowid      int64
		id         string
		enc, nonce []byte
	}
	var batch []row
	size, cut := 0, false
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.rowid, &r.id, &r.enc, &r.nonce); err != nil {
			rows.Close()
			return false, err
		}
		batch = append(batch, r)
		if size += len(r.enc); size >= blobRefsBatchBytes {
			cut = true
			break
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	done := !cut && len(batch) < blobRefsBatch
	last, refs := after, 0
	for _, r := range batch {
		last = r.rowid
		if body, err := s.open(r.enc, r.nonce); err == nil {
			ids := BlobIDsIn(body)
			if err := insertBlobRefs(tx, r.id, ids); err != nil {
				return false, err
			}
			refs += len(ids)
		}
		if refs >= blobRefsBatchRefs {
			// The rest of the rows are read again by the next batch.
			done = false
			break
		}
	}
	if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		settingBlobRefsCursor, strconv.FormatInt(last, 10)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if done {
		s.blobRefsLive.Store(true)
	}
	return done, nil
}

// noteBlobRefsThrough moves the backfill's cursor to the newest row, at
// Close: once the backfill has finished, every row up to it is indexed.
func (s *Store) noteBlobRefsThrough() {
	if !s.BlobRefsComplete() {
		return
	}
	_, _ = s.db.Exec(`INSERT INTO settings (key, value)
		SELECT ?, CAST(coalesce(max(rowid), 0) AS TEXT) FROM messages WHERE true
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingBlobRefsCursor)
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
