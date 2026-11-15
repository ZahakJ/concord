package app

import (
	"testing"
	"time"
)

// The background retry of a self-erase is off in this package's tests, so a
// retry left running by one test cannot act on another's files or hooks.
// A test that is about the retry turns it on with fastSelfEraseRetry.
func init() {
	selfEraseRetryAttempts, selfEraseSlowAttempts = 0, 0
}

// fastSelfEraseRetry retries a self-erase every 100 ms, for at most about
// thirty seconds, and waits for every retry to end when the test does.
func fastSelfEraseRetry(t *testing.T) {
	t.Helper()
	pause, attempts := selfEraseRetryPause, selfEraseRetryAttempts
	selfEraseRetryPause, selfEraseRetryAttempts = 100*time.Millisecond, 300
	t.Cleanup(func() {
		selfErasers.Wait()
		selfEraseRetryPause, selfEraseRetryAttempts = pause, attempts
	})
}
