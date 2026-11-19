//go:build !wails && unix

package main

import (
	"fmt"
	"os"
	"syscall"
)

// checkRuntimeDir refuses a runtime directory that is not a directory, that
// another user owns, or that group or others may write to: anyone who can
// write there can swap what the launch socket's directory resolves to
// between our checks of it and our use of it.
func checkRuntimeDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("XDG_RUNTIME_DIR %s is not a directory", dir)
	}
	if !ownedBy(fi, runtimeOwnerUID()) {
		return fmt.Errorf("XDG_RUNTIME_DIR %s belongs to another user; not serving the launch socket there", dir)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("XDG_RUNTIME_DIR %s is writable by group or others (mode %o); not serving the launch socket there", dir, fi.Mode().Perm())
	}
	return nil
}

// ownedBy reports whether fi (from Lstat) belongs to uid.
func ownedBy(fi os.FileInfo, uid int) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}
