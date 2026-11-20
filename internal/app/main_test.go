package app

import (
	"os"
	"testing"
)

// TestMain answers yes on every issuer's link prompt, standing in for the
// person at the issuer in the many tests that link a device to get on with
// something else. Tests about the prompt itself opt their issuer out with
// manualLinkConfirm.
func TestMain(m *testing.M) {
	testLinkAutoConfirm = true
	os.Exit(m.Run())
}
