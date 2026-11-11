package store

import (
	"testing"
	"time"
)

// TestCloseWaitsForAWriteInFlight: Close used to return while a statement
// already running on the store's connection was still going, so that write
// landed after the service had closed. In a shut-down app that is a write
// racing the process exit; in the tests it raced the removal of the data
// directory ("directory not empty"). Close now returns only once the
// connection has been given back and shut.
func TestCloseWaitsForAWriteInFlight(t *testing.T) {
	s, _ := openTestStore(t)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	finished := make(chan time.Time, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = tx.Exec(`CREATE TABLE IF NOT EXISTS close_wait_probe (x INTEGER)`)
		_ = tx.Commit()
		finished <- time.Now()
	}()
	time.Sleep(20 * time.Millisecond) // the write is under way before Close starts
	_ = s.Close()
	closed := time.Now()
	if done := <-finished; done.After(closed) {
		t.Fatalf("Close returned %v before the write in flight finished", done.Sub(closed))
	}
}
