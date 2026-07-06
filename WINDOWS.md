# Running Concord on Windows

Concord's `.exe` is **safe** — it's an open-source Go program with no installer
and no admin rights. But because it isn't code-signed (signing costs money),
Windows SmartScreen and sometimes Windows Defender flag *any* unknown unsigned
program by default. This is a false positive. Here's how to get past it.

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
