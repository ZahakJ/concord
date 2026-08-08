//go:build wails && linux

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZahakJ/concord/internal/version"
)

// ensureInstalled — Linux flavor of the Windows self-install: launched from a
// Downloads folder (or anywhere else ad hoc), the desktop app installs itself
// the XDG way — binary under ~/.local/share/concord, icon + .desktop launcher
// entry so it shows up in app menus/search — then hands over to the installed
// copy. Launching from the install home (or from a system location like
// /usr/bin, where a package manager owns the file) is a no-op.
//
// Returns true when the caller must exit (we relaunched or uninstalled).
func ensureInstalled() bool {
	if len(os.Args) > 1 && os.Args[1] == "--uninstall" {
		uninstallLinux()
		return true
	}

	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// A package manager (or admin) owns system paths — never relocate those.
	for _, sys := range []string{"/usr/", "/opt/", "/nix/", "/snap/", "/app/"} {
		if strings.HasPrefix(exe, sys) {
			return false
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(dataHome, "concord")
	target := filepath.Join(dir, "concord")

	if exe == target {
		os.Remove(target + ".old") // finished self-update: drop the parked copy
		if installedVersionLinux(dir) != version.Version {
			registerLinux(dir, target, dataHome)
		}
		return false
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	// Downgrade guard, same rule as Windows: a stale exe hands over.
	if semverLessLinux(version.Version, installedVersionLinux(dir)) {
		if cmd := exec.Command(target, os.Args[1:]...); cmd.Start() == nil {
			return true
		}
		return false
	}
	if err := copyFileLinux(exe, target); err != nil {
		if _, statErr := os.Stat(target); statErr != nil {
			return false
		}
	}
	registerLinux(dir, target, dataHome)
	cmd := exec.Command(target, os.Args[1:]...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return false
	}
	return true
}

func installedVersionLinux(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".version"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func registerLinux(dir, target, dataHome string) {
	// Icon: the same embedded appicon the window uses.
	iconPath := filepath.Join(dir, "concord.png")
	_ = os.WriteFile(iconPath, appIcon, 0o644)
	// Launcher entry (menus, app search, docks).
	appsDir := filepath.Join(dataHome, "applications")
	_ = os.MkdirAll(appsDir, 0o755)
	desktop := "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Concord\n" +
		"Comment=Serverless end-to-end-encrypted chat\n" +
		"Exec=" + target + "\n" +
		"Icon=" + iconPath + "\n" +
		"Terminal=false\n" +
		"Categories=Network;InstantMessaging;\n" +
		"StartupWMClass=concord\n"
	_ = os.WriteFile(filepath.Join(appsDir, "concord.desktop"), []byte(desktop), 0o644)
	_ = os.WriteFile(filepath.Join(dir, ".version"), []byte(version.Version), 0o644)
	// Refresh menu caches where the tool exists (best-effort, silent).
	_ = exec.Command("update-desktop-database", appsDir).Run()
}

func uninstallLinux() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	os.Remove(filepath.Join(dataHome, "applications", "concord.desktop"))
	dir := filepath.Join(dataHome, "concord")
	// The chat database lives under the user config dir, not here — removing
	// the install dir never touches identity or history.
	os.Remove(filepath.Join(dir, "concord"))
	os.Remove(filepath.Join(dir, "concord.old"))
	os.Remove(filepath.Join(dir, "concord.png"))
	os.Remove(filepath.Join(dir, ".version"))
	os.Remove(dir)
}

func copyFileLinux(src, target string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := target + ".new"
	if err := os.WriteFile(tmp, in, 0o755); err != nil {
		return err
	}
	old := target + ".old"
	os.Remove(old)
	_ = os.Rename(target, old)
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Rename(old, target)
		os.Remove(tmp)
		return err
	}
	return nil
}

// semverLessLinux mirrors the updater's comparison (vX.Y.Z; unparseable
// never compares as less).
func semverLessLinux(a, b string) bool {
	parse := func(v string) ([3]int, bool) {
		var out [3]int
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		parts := strings.Split(v, ".")
		if len(parts) != 3 {
			return out, false
		}
		for i, p := range parts {
			n := 0
			for _, c := range p {
				if c < '0' || c > '9' {
					return out, false
				}
				n = n*10 + int(c-'0')
			}
			out[i] = n
		}
		return out, true
	}
	pa, ok1 := parse(a)
	pb, ok2 := parse(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return false
}
