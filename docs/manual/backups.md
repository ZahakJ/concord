# Backups and recovery

What can be recovered if you lose a device or forget your passphrase, and how: the recovery phrase for your identity, linked devices and backup files for your history, and Markdown export for a readable copy.

## What there is to lose

Concord keeps no copy of anything on a server. Two separate things live on your devices, and each has its own way back:

| What | Recovered by |
|---|---|
| Your identity (the account key) | The 24-word recovery phrase, or another linked device |
| Your guilds, conversations and history | Another linked device, a backup file, or rejoining and syncing from other members |

The two are recovered separately on purpose. The recovery phrase holds no messages, and a backup file holds no identity. **Settings → Account** puts both under "Your safety net": **Backup & restore** and **Recovery phrase**.

The most complete safety net is a second linked device (see [Multiple devices](devices.md)). Linking from it brings back everything: profile, guilds and history.

## The recovery phrase

Your recovery phrase is your account key written as 24 words. It is shown when the identity is created and can be revealed again in **Settings → Account → Recovery phrase**. Write it down on paper or in a password manager.

To use it on a new or reset device, choose **Restore from a recovery phrase** on the first-run screen, enter the 24 words, choose a new passphrase for the device, and select **Restore account**.

What comes back is the key: you are the same person, with the same fingerprint, to everyone who has verified you. What does not come back is everything else. The restored device starts with no name, no guilds and no history. To fill it in:

- If another of your devices is still linked, link from that device instead; everything comes with it.
- Otherwise, ask someone in each guild for a fresh invite. Once you are back in, recent history syncs from the members who are online.
- Then restore a backup file, if you have one, to fill in the rest.

## Backup files

A backup file is a sealed copy of the history on one device. Make one in **Settings → Account → Backup & restore**.

### Make a backup

1. Under **Make a backup**, choose a passphrase for the file (at least 8 characters) and repeat it. It can be different from your app passphrase.
2. Optionally tick **Include images and files**.
3. Select **Download backup**. The file is saved as `concord-backup-<date>.archive`.

The backup contains every guild and direct-message conversation on the device, your Notes, and your saved messages. It is compressed, then encrypted under the file passphrase with the same construction that protects your identity on disk (Argon2id and XSalsa20-Poly1305). There is no way to recover a lost file passphrase.

**Include images and files** adds the attachments still cached on this device. The cache is limited in size and older attachments are removed as space is needed, so this makes the file much larger without guaranteeing every file is in it.

A backup does **not** contain:

- **Your identity.** That is what the recovery phrase is for, and keeping it out means a leaked backup file cannot be used to impersonate you.
- **Guild membership.** Being in a guild is a cryptographic state shared with the other members; it cannot be restored from a file.

### Restore a backup

1. Make sure the device is signed in and has rejoined the guilds you want history for.
2. In **Settings → Account → Backup & restore**, under **Restore a backup**, enter the archive's passphrase.
3. Select **Choose a backup file…** and pick the `.archive` file.

Restoring only adds. Nothing already on the device is overwritten or removed, so restoring an old backup cannot lose newer messages, and restoring the same file twice does nothing the second time. History is restored only into guilds and conversations this device is a member of; the rest is counted as skipped. Your Notes and saved messages come back regardless, since there is nobody to rejoin for those.

A backup written by a newer version of Concord cannot be read by an older one; update first.

## Markdown export

Export writes a readable transcript, not a restorable backup. It is plain text, unencrypted, and one-way: use it to keep, print or share a conversation.

- **One channel:** open the **More** menu in the channel header and choose **Export history**. It saves `<channel>-history.md`.
- **A whole guild:** **Guild settings → Data → Export history** shows the file name, size and message count, then saves every channel as one `<guild>-history.md` file, including any history the guild imported from elsewhere.

Exports are read from the full local history, not just what is on screen. Pictures, files and voice messages are named in the transcript but not copied. Anyone who can read the exported file can read the conversation, and a guild's retention setting does not reach a file once it is written.

Where the platform cannot save a file, the transcript is copied to the clipboard instead.

The events calendar can also be exported as an `.ics` file from its own panel.

## If you forget your passphrase

Your passphrase is never stored, so it cannot be looked up or reset. On the unlock screen, select **Forgot passphrase?**. There are three ways forward:

| Option | What happens |
|---|---|
| **Re-link from my other device** | Link this device again from another of your devices and choose a new passphrase. Everything comes back. |
| **Use my recovery phrase** | Erases this device's data, then restores your identity under a new passphrase. You get the account key back but not your guilds or history; see [The recovery phrase](#the-recovery-phrase). |
| **I have neither — start over (deletes everything)** | Deletes the identity and all data on this device so you can create a new identity. Guilds you own are lost. |

## If you lose a device

If the lost device was linked to another of yours, unlink it from the remaining one (see [Remove a device](devices.md#remove-a-device)). That removes it from your guilds straight away. It erases its own copy only if it comes back online running Concord, so treat whatever was on it as exposed.

If it was your only device, restore your identity from the recovery phrase on a new one, rejoin your guilds, and restore your latest backup file.

Without the recovery phrase and without another linked device, the identity cannot be recovered by anyone.
