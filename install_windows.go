//go:build wails && windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"

	"github.com/zahak/concord/internal/version"
)

// ensureInstalled makes the desktop app behave like a properly installed
// program NO MATTER how it was launched — double-clicked in Downloads, run
// off a USB stick, or via the one-click Setup (which drops it in the right
// place already). If we aren't running from %LOCALAPPDATA%\Concord\
// Concord.exe, we copy ourselves there, create Start Menu + Desktop
// shortcuts and an Add/Remove Programs entry, relaunch the installed copy,
// and exit — Discord's exact self-install behavior. Running from the install
// home is a no-op (plus cleanup of a self-update's parked .old binary).
//
// Everything here is silent: registry work goes through syscalls, and the
// few helper processes (shortcut creation, uninstall sweep) run with hidden
// windows — no console flashes on launch or install.
//
// Returns true when the caller must exit (we relaunched or uninstalled).
func ensureInstalled() bool {
	// "Concord.exe --uninstall" is the Add/Remove entry's uninstall action.
	if len(os.Args) > 1 && os.Args[1] == "--uninstall" {
		uninstall()
		return true
	}

	exe, err := os.Executable()
	if err != nil {
		return false
	}
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		return false
	}
	dir := filepath.Join(local, "Concord")
	target := filepath.Join(dir, "Concord.exe")

	if strings.EqualFold(exe, target) {
		os.Remove(target + ".old") // finished self-update: drop the parked copy
		// Re-register only when the recorded version is stale (first run after
		// a self-update) — not on every launch.
		if installedVersion() != version.Version {
			registerApp(target)
		}
		return false
	}

	// Running from OUTSIDE the install home: install ourselves there — unless
	// a NEWER version is already installed (re-running a stale exe from
	// Downloads must never downgrade a self-updated install; just hand over).
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false // read-only environment: stay portable rather than break
	}
	if semverLess(version.Version, installedVersion()) {
		if cmd := exec.Command(target, os.Args[1:]...); cmd.Start() == nil {
			return true
		}
		return false
	}
	if err := copySelf(exe, target); err != nil {
		// Couldn't place the binary (an instance may hold an odd lock). If an
		// installed copy exists, hand over to it; otherwise keep running here.
		if _, statErr := os.Stat(target); statErr != nil {
			return false
		}
	}
	registerApp(target)
	// Hand over to the installed copy and bow out.
	cmd := exec.Command(target, os.Args[1:]...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return false // couldn't launch it; keep running from here instead
	}
	return true
}

const uninstKeyPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Concord`

// installedVersion reads the version registerApp recorded ("" when absent).
func installedVersion() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, uninstKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("DisplayVersion")
	if err != nil {
		return ""
	}
	return v
}

// semverLess mirrors the updater's comparison (vX.Y.Z; anything unparseable
// compares as "not less", so dev builds always reinstall).
func semverLess(a, b string) bool {
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

// copySelf places our own bytes at target, swapping atomically so it works
// even when target is a RUNNING instance (rename-the-running-image is legal
// on Windows, same trick the self-updater uses).
func copySelf(src, target string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := target + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	old := target + ".old"
	os.Remove(old)
	_ = os.Rename(target, old) // fails harmlessly when target doesn't exist
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Rename(old, target) // restore
		os.Remove(tmp)
		return err
	}
	return nil
}

// registerApp creates the shortcuts and the per-user Add/Remove Programs
// entry. Registry writes are direct syscalls (no processes); the shortcut
// helper runs with a hidden window. Best-effort and idempotent.
func registerApp(target string) {
	dir := filepath.Dir(target)
	makeShortcut(filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Concord.lnk"), target)
	if home, err := os.UserHomeDir(); err == nil {
		makeShortcut(filepath.Join(home, "Desktop", "Concord.lnk"), target)
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstKeyPath, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	_ = k.SetStringValue("DisplayName", "Concord")
	_ = k.SetStringValue("DisplayVersion", version.Version)
	_ = k.SetStringValue("DisplayIcon", target)
	_ = k.SetStringValue("InstallLocation", dir)
	_ = k.SetStringValue("UninstallString", fmt.Sprintf(`"%s" --uninstall`, target))
	_ = k.SetStringValue("Publisher", "Concord contributors")
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
}

// uninstall undoes registerApp and removes the install dir (the encrypted
// chat database under %AppData% is deliberately kept — uninstalling the app
// must never destroy someone's identity and history).
func uninstall() {
	os.Remove(filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Concord.lnk"))
	if home, err := os.UserHomeDir(); err == nil {
		os.Remove(filepath.Join(home, "Desktop", "Concord.lnk"))
	}
	_ = registry.DeleteKey(registry.CURRENT_USER, uninstKeyPath)
	// The running exe can't delete itself; a detached (hidden) shell sweeps
	// the install dir right after we exit.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if strings.EqualFold(filepath.Base(dir), "Concord") {
			_ = hiddenCmd("cmd", "/C",
				"ping -n 6 127.0.0.1 > nul & rmdir /S /Q \""+dir+"\"").Start()
		}
	}
}

func makeShortcut(lnk, target string) {
	_ = os.MkdirAll(filepath.Dir(lnk), 0o755)
	ps := fmt.Sprintf(
		`$s=(New-Object -ComObject WScript.Shell).CreateShortcut(%q);$s.TargetPath=%q;$s.WorkingDirectory=%q;$s.Save()`,
		lnk, target, filepath.Dir(target))
	_ = hiddenCmd("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run()
}

// hiddenCmd builds an exec.Cmd whose process never shows a console window —
// GUI apps spawning console helpers otherwise flash black boxes at the user.
func hiddenCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}
