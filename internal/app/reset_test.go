package app

import (
	"errors"

	"github.com/ZahakJ/concord/internal/identity"
)

// resetIdentity erases the device whatever it holds: the tests' stand-in for
// an erase someone confirmed, when what was confirmed is not what the test
// is about. Every erase in the app itself is checked (ResetIdentityIf).
func resetIdentity(dataDir string) error {
	err := removeKeystore(keystorePathIn(dataDir), func() bool { return true }, nil)
	if err != nil && !errors.Is(err, identity.ErrEraseDeferred) {
		return err
	}
	// A deferred keystore erase and a data erase that failed are both said
	// (see TestDeferredEraseWithDataLeftSaysBoth).
	if derr := resetDataIn(dataDir); derr != nil {
		return errors.Join(err, derr)
	}
	return err
}
