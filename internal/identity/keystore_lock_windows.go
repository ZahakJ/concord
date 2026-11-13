//go:build windows

package identity

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

const lockOpenFlags = 0

// tryLockFile takes an exclusive lock on the first byte of f without
// blocking. It reports false when another holder has it, and an error only
// when the filesystem cannot lock at all.
func tryLockFile(f *os.File) (bool, error) {
	err := windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION), errors.Is(err, windows.ERROR_IO_PENDING):
		return false, nil
	}
	return false, err
}

func unlockFile(f *os.File) {
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
}

// renameDurable replaces to with from and returns only once the change is on
// disk. os.Rename asks for no write-through, and NTFS may then lose a lazily
// flushed rename to a power cut: a passphrase change reported done would come
// back as the old keystore.
func renameDurable(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(f, t, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
