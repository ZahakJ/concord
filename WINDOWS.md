# Running Concord on Windows

**The easy way: download `Concord-Setup-<version>.exe` and run it.** It's a
one-click installer, Discord-style — no admin prompt, no wizard: it installs
Concord for your user account, puts it in the Start Menu and on the Desktop,
and launches it. Updates after that happen from inside the app (Settings →
Software update). Uninstall from Windows' "Add or remove programs" as usual
(your chat history and identity are kept).

Everything below is for the standalone `.exe` downloads and for getting past
SmartScreen, which can flag the installer too.

Concord's `.exe` is **safe** — it's an open-source Go program that needs no
admin rights. But because it isn't code-signed (signing costs money), Windows
SmartScreen and sometimes Windows Defender flag *any* unknown unsigned
program by default. This is a false positive. Here's how to get past it.

> **Note:** SmartScreen trust is tied to a file's exact contents, so **every new
> release starts fresh** and may warn again even if the last one stopped — it's
> not a new problem, just how unsigned apps work.

## Quickest path if the desktop app keeps getting blocked

Grab **`concord-windows.exe`** (the *web* build) instead of
`concord-desktop-windows.exe`. It's the same app, but it opens in your browser
rather than its own window — and it's the build that has always run cleanly. No
window, same features.

## If you see "Windows protected your PC" (blue popup)

1. Click **More info**
2. Click **Run anyway**

That's it — Concord opens in your browser.

## If Defender deletes/quarantines the file, or "Run anyway" doesn't appear

Defender occasionally quarantines unsigned network apps. Do this once:

1. Open **Windows Security** (search it in the Start menu)
2. **Virus & threat protection** → **Protection history**
3. Find the Concord item → **Actions** → **Allow** (and **Restore** if it was removed)
4. Re-run the `.exe`

### Bulletproof method (recommended if it keeps getting blocked)

Tell Defender to trust a folder, then keep Concord there:

1. **Windows Security** → **Virus & threat protection**
2. Under *Virus & threat protection settings*, click **Manage settings**
3. Scroll to **Exclusions** → **Add or remove exclusions** → **Add an exclusion** → **Folder**
4. Pick (or make) a folder, e.g. `Documents\Concord`
5. Put `concord-windows.exe` in that folder and run it from there

## Is this actually safe?

Yes. Concord:

- runs as your normal user (no admin, per its manifest)
- has no installer and writes only its own data folder (`%AppData%\concord`)
- is fully open source — you can read every line and build it yourself
- talks only peer-to-peer, end-to-end encrypted

The flag is purely "this program isn't signed by a company Microsoft
recognizes," not a detection of anything harmful.

## Verifying your download (optional)

Every release ships a `SHA256SUMS` file listing the expected hash of each binary.
To confirm your download wasn't tampered with, in PowerShell:

```powershell
Get-FileHash .\concord-desktop-windows.exe -Algorithm SHA256
```

Compare the printed hash against the matching line in `SHA256SUMS`.
