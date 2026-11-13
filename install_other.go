//go:build wails && !windows && !linux

package main

// ensureInstalled is self-installation for Windows (install_windows.go) and
// Linux (install_linux.go); macOS would get it with a native .app build.
func ensureInstalled(bool) bool { return false }

// uninstallHelp is empty: with nothing self-installed there is no
// --uninstall, and the flag is not offered.
const uninstallHelp = ""
