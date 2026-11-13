package store

import (
	"database/sql"
	"fmt"
)

// Profile records are what a member holds so that an account can be handed its
// own profile back: the record body an account published with its profile
// announce, sealed so that only that account can check it (see
// app/profilerecord.go). One row per account, newest edit stamp only.

func (s *Store) migrateProfileRecords() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS profile_records (
	   fingerprint TEXT PRIMARY KEY,
	   at          INTEGER NOT NULL,
	   record      BLOB NOT NULL
	 )`); err != nil {
		return fmt.Errorf("store: migrate profile records: %w", err)
	}
	return nil
}

// SaveProfileRecord keeps record for fingerprint if at is newer than the one
// held. Reports whether it was written.
func (s *Store) SaveProfileRecord(fingerprint string, at int64, record []byte) (bool, error) {
	res, err := s.db.Exec(
		`INSERT INTO profile_records (fingerprint, at, record) VALUES (?, ?, ?)
		 ON CONFLICT(fingerprint) DO UPDATE SET at=excluded.at, record=excluded.record
		 WHERE excluded.at > profile_records.at`,
		fingerprint, at, record)
	if err != nil {
		return false, fmt.Errorf("store: save profile record: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ProfileRecordAt returns the edit stamp of the record held for fingerprint, 0
// for none.
func (s *Store) ProfileRecordAt(fingerprint string) int64 {
	var at int64
	if err := s.rd().QueryRow(`SELECT at FROM profile_records WHERE fingerprint = ?`, fingerprint).Scan(&at); err != nil {
		return 0
	}
	return at
}

// ProfileRecord returns the record held for fingerprint, nil for none.
func (s *Store) ProfileRecord(fingerprint string) ([]byte, int64, error) {
	var rec []byte
	var at int64
	err := s.rd().QueryRow(`SELECT record, at FROM profile_records WHERE fingerprint = ?`, fingerprint).Scan(&rec, &at)
	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("store: profile record: %w", err)
	}
	return rec, at, nil
}
