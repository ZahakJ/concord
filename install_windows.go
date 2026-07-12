//go:build wails && windows

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
		registerApp(target)        // keep shortcuts/registry fresh (cheap, idempotent)
		return false
	}

	// Running from OUTSIDE the install home: install ourselves there.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false // read-only environment: stay portable rather than break
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
// entry. Every step is best-effort and idempotent.
func registerApp(target string) {
	dir := filepath.Dir(target)
	makeShortcut(filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Concord.lnk"), target)
	if home, err := os.UserHomeDir(); err == nil {
		makeShortcut(filepath.Join(home, "Desktop", "Concord.lnk"), target)
	}
	const key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\Concord`
	regAdd(key, "DisplayName", "REG_SZ", "Concord")
	regAdd(key, "DisplayVersion", "REG_SZ", version.Version)
	regAdd(key, "DisplayIcon", "REG_SZ", target)
	regAdd(key, "InstallLocation", "REG_SZ", dir)
	regAdd(key, "UninstallString", "REG_SZ", fmt.Sprintf(`"%s" --uninstall`, target))
	regAdd(key, "Publisher", "REG_SZ", "Concord contributors")
	regAdd(key, "NoModify", "REG_DWORD", "1")
	regAdd(key, "NoRepair", "REG_DWORD", "1")
}

// uninstall undoes registerApp and removes the install dir (the encrypted
// chat database under %AppData% is deliberately kept — uninstalling the app
// must never destroy someone's identity and history).
func uninstall() {
	os.Remove(filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Concord.lnk"))
	if home, err := os.UserHomeDir(); err == nil {
		os.Remove(filepath.Join(home, "Desktop", "Concord.lnk"))
	}
	_ = exec.Command("reg", "delete",
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\Concord`, "/f").Run()
	// The running exe can't delete itself; a detached shell sweeps the install
	// dir right after we exit.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if strings.EqualFold(filepath.Base(dir), "Concord") {
			_ = exec.Command("cmd", "/C",
				"ping -n 6 127.0.0.1 > nul & rmdir /S /Q \""+dir+"\"").Start()
		}
	}
}

func makeShortcut(lnk, target string) {
	_ = os.MkdirAll(filepath.Dir(lnk), 0o755)
	ps := fmt.Sprintf(
		`$s=(New-Object -ComObject WScript.Shell).CreateShortcut(%q);$s.TargetPath=%q;$s.WorkingDirectory=%q;$s.Save()`,
		lnk, target, filepath.Dir(target))
	_ = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps).Run()
}

func regAdd(key, name, typ, value string) {
	_ = exec.Command("reg", "add", key, "/v", name, "/t", typ, "/d", value, "/f").Run()
}
