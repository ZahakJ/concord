//go:build wails && !windows

package main

// ensureInstalled is Windows-only self-installation (see install_windows.go);
// everywhere else the desktop app runs from wherever it lives.
func ensureInstalled() bool { return false }
