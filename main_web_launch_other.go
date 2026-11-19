//go:build !wails && !unix

package main

import (
	"fmt"
	"os"
)

// Off unix (Windows) a directory has no owner uid or mode bits to check, and
// XDG_RUNTIME_DIR is rarely set; the launch socket is served as before.

func checkRuntimeDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("XDG_RUNTIME_DIR %s is not a directory", dir)
	}
	return nil
}

func ownedBy(os.FileInfo, int) bool { return true }
