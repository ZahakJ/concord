//go:build !windows

package identity

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// lockOpenFlags: a symlink planted at the lock file's name is refused rather
// than followed.
const lockOpenFlags = unix.O_NOFOLLOW

// tryLockFile takes an exclusive advisory lock on f without blocking. It
// reports false when another holder has it, and an error only when the
// filesystem cannot lock at all.
func tryLockFile(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EINTR):
		return false, nil
	}
	return false, err
}

func unlockFile(f *os.File) { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN) }
