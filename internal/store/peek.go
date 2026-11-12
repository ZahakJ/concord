package store

import (
	"database/sql"
	"net/url"
	"os"

	"golang.org/x/crypto/nacl/secretbox"
)

// The functions here read a database that nobody has unlocked: the lock screen
// has to say whose account is on the device before anything is opened, and the
// flows that replace that account (recovery phrase, re-link) have to know
// whether they are about to erase it. Read-only, and every failure is an
// "I can't tell" rather than an error, because each caller has an honest
// answer for that case.

func openReadOnly(path string) (*sql.DB, bool) {
	if _, err := os.Stat(path); err != nil {
		return nil, false
	}
	db, err := sql.Open("sqlite", "file:"+(&url.URL{Path: path}).EscapedPath()+"?mode=ro")
	if err != nil {
		return nil, false
	}
	db.SetMaxOpenConns(1)
	return db, true
}

// PeekSetting returns a stored setting from the database at path without a
// key ("" when the database or the setting is absent). Settings are not
// sealed; message bodies are.
func PeekSetting(path, key string) string {
	db, ok := openReadOnly(path)
	if !ok {
		return ""
	}
	defer db.Close()
	var v string
	if db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v) != nil {
		return ""
	}
	return v
}

// KeyOpensMessages reports whether dataKey is the key the database at path
// sealed its messages with. known is false when there is no sealed message to
// test against (or no database), and then opens means nothing.
func KeyOpensMessages(path string, dataKey []byte) (opens, known bool) {
	if len(dataKey) != 32 {
		return false, false
	}
	db, ok := openReadOnly(path)
	if !ok {
		return false, false
	}
	defer db.Close()
	var enc, nonceB []byte
	if db.QueryRow(`SELECT content_enc, nonce FROM messages WHERE length(nonce) = ? LIMIT 1`, nonceSize).Scan(&enc, &nonceB) != nil {
		return false, false
	}
	var key [32]byte
	copy(key[:], dataKey)
	var nonce [nonceSize]byte
	copy(nonce[:], nonceB)
	_, opens = secretbox.Open(nil, enc, &nonce, &key)
	return opens, true
}
