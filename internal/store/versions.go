package store

import (
	"crypto/rand"
	"time"

	"golang.org/x/crypto/nacl/secretbox"
)

// Edit history. Every body a message has held is kept locally, sealed like the
// message itself, so the "(edited)" mark can open a list of what it used to say.
//
// Privacy: a version is only ever recorded from text THIS device already
// received (the original, then each verified edit), so history reveals nothing
// to anyone who did not already see it.
//
// There is deliberately no wire protocol for fetching old versions from peers.
// A member who joins later, or catches up through sync, holds whatever sync
// hands them (the latest body) and so starts their history there.
//
// Cap: a message keeps at most maxVersions versions and maxVersionBytes of
// stored body. Past that the oldest MIDDLE versions go first: the original is
// the reference point for "what did this first say" and the latest is the
// current text, so those two are never dropped; the intermediate steps are the
// most expendable and the ones a runaway edit loop would flood.
const (
	maxVersions     = 20
	maxVersionBytes = 64 << 10
)

// Version is one past or present body of a message, with when it took effect.
type Version struct {
	Content string
	At      time.Time
}

// recordVersion appends newContent as the message's newest version, first
// seeding the history with oldContent (at its send time) if this is the
// message's first edit. An unchanged body is not a new version.
func (s *Store) recordVersion(q querier, id, oldContent string, sentNano int64, newContent string, atNano int64) error {
	if oldContent == newContent {
		return nil
	}
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM message_versions WHERE message_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if err := s.insertVersion(q, id, oldContent, sentNano); err != nil {
			return err
		}
	}
	if err := s.insertVersion(q, id, newContent, atNano); err != nil {
		return err
	}
	return trimVersions(q, id)
}

func (s *Store) insertVersion(q querier, id, content string, atNano int64) error {
	var nonce [nonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	sealed := secretbox.Seal(nil, []byte(content), &nonce, &s.key)
	_, err := q.Exec(`INSERT INTO message_versions (message_id, content_enc, nonce, at) VALUES (?, ?, ?, ?)`,
		id, sealed, nonce[:], atNano)
	return err
}

// trimVersions enforces the cap by deleting the second-oldest version until
// the history fits, so the first and last always survive.
func trimVersions(q querier, id string) error {
	for {
		rows, err := q.Query(`SELECT seq, LENGTH(content_enc) FROM message_versions WHERE message_id = ? ORDER BY seq`, id)
		if err != nil {
			return err
		}
		var seqs []int64
		total := 0
		for rows.Next() {
			var seq int64
			var l int
			if err := rows.Scan(&seq, &l); err != nil {
				rows.Close()
				return err
			}
			seqs = append(seqs, seq)
			total += l
		}
		rows.Close()
		if len(seqs) <= 2 || (len(seqs) <= maxVersions && total <= maxVersionBytes) {
			return nil
		}
		if _, err := q.Exec(`DELETE FROM message_versions WHERE seq = ?`, seqs[1]); err != nil {
			return err
		}
	}
}

func dropVersions(q querier, id string) error {
	_, err := q.Exec(`DELETE FROM message_versions WHERE message_id = ?`, id)
	return err
}

// MessageVersions returns a message's kept versions, oldest first (version 0 is
// the original). An unedited message has none.
func (s *Store) MessageVersions(id string) ([]Version, error) {
	rows, err := s.rd().Query(`SELECT content_enc, nonce, at FROM message_versions WHERE message_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		var enc, nonceB []byte
		var at int64
		if err := rows.Scan(&enc, &nonceB, &at); err != nil {
			return nil, err
		}
		c, err := s.open(enc, nonceB)
		if err != nil {
			continue
		}
		out = append(out, Version{Content: c, At: time.Unix(0, at).UTC()})
	}
	return out, rows.Err()
}
