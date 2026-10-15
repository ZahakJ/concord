# Accounts and identity

What "an account" means in Concord: a key pair you hold rather than a record on a server, the passphrase that protects it on each device, the recovery phrase that can rebuild it, and the profile other people see.

## There is no account database

Concord has no sign-up, no email address, no username registry and no server that knows you exist. Your account is a cryptographic key: an Ed25519 key pair generated on your device the first time you create an identity. Holding the private key is what makes you you. Nobody can suspend the account, rename it or reset it, and nobody can recover it for you if the key is lost.

Everything else follows from that key. Your network address is derived from it, so a connection to you can only be completed by a device holding it. Every message you send is signed with it, so other members can check that it came from you.

In practice, "your account" means three things:

| | What it is | Where it lives |
|---|---|---|
| The account key | The Ed25519 key pair | Encrypted on each of your devices |
| The recovery phrase | The same key written as 24 words | Wherever you wrote it down |
| Your profile | Name, picture, status and the rest | Sent to the people you share guilds with |

Your messages and guild memberships are not part of the account. They are stored on the devices of the people in each conversation, including yours. See [Backups and recovery](backups.md) for what that means when a device is lost.

## Your fingerprint

Other people recognize your key by its **fingerprint**, also called a safety number: 32 letters and digits in groups of four, derived from your public key. It is the same on every device you link, because it comes from the account key.

You can see your own at the bottom of **Settings → Your profile**, under "Your identity fingerprint". To check someone else's, open their profile card, reveal their safety number, and compare it with them over a channel you trust, such as in person or on a call. If they match, select **Verify identity**. Verification is a note you keep for yourself on your own devices; nobody else is told. It matters in two places: you can add a verified contact to a guild directly, without an invite code, and a guild invitation from a verified contact is accepted automatically.

When a contact links a new device, Concord posts a notice in your conversation with them, because their fingerprint does not change when their set of devices does.

## Your passphrase

The passphrase protects your identity on one device. Concord stretches it with Argon2id (64 MiB of memory per guess) into a key that encrypts your identity file, so someone who copies the file still has to guess the passphrase, and each guess is expensive.

Your local message database is encrypted under a key derived from your identity, so it is unreadable without unlocking too. Message text and attachments are sealed. Some details are stored unencrypted: who sent each message and when, display names, guild and channel names, reactions, profile pictures, read positions and settings. Someone with your device but not your passphrase can see the shape of your conversations, but not what was said. [Privacy and encryption](privacy.md) goes into the details.

Some things to know about the passphrase:

- **It is per device.** Each device you link gets its own passphrase, chosen while linking. They do not have to match.
- **It is never stored or sent anywhere,** so there is no reset link. A wrong passphrase looks the same to Concord as a damaged file.
- **There is no minimum.** The meter under the field estimates strength from length and variety. A few unrelated words score well.
- **There is no screen for changing it.** Setting a new one means re-linking the device from another of your devices, or restoring it from the recovery phrase, as described in [Backups and recovery](backups.md#if-you-forget-your-passphrase).
- **Signing out** (**Settings → Account → Sign out (lock this device)**) locks the app until the passphrase is entered again.

On Android you can store the passphrase in the phone's secure hardware and unlock with a fingerprint or your face instead. Concord offers this after your first unlock. With biometric unlock on, **Settings → Privacy & safety → App lock** also asks for your fingerprint every time the app opens.

## Your recovery phrase

The recovery phrase is your account key written as 24 words from the standard BIP-39 word list. It is shown once when the identity is created, and you can reveal it again at any time in **Settings → Account → Recovery phrase**.

Anyone who has these words can become you, on any device, without your passphrase. Keep them offline and never share them.

The phrase brings back the **key**, and only the key. A device restored from it is you, to everyone who has verified you, but it starts with no profile, no guilds and no history. [Backups and recovery](backups.md) explains how to get those back.

## Your profile

Your profile is what other people see. Edit it in **Settings → Your profile**:

| Field | Notes |
|---|---|
| Display name | Up to 32 characters |
| Picture and banner | Set from the top of the page; pictures are cropped in the app |
| Availability | Online, Idle, Do Not Disturb or Invisible |
| Status | A short line, up to 64 characters |
| About me | Up to 600 characters |
| Birthday | Month and day only, no year. Your guilds see a cake on the day. |
| Fallback emoji | Shown when you have no picture |
| Profile theme | A primary and a secondary color for your profile card. The primary is also your accent color unless you choose another in Appearance. |
| Avatar decoration | A frame around your picture |

Your profile travels with your messages to the people you share guilds and conversations with. It is not published anywhere else. When you edit it on one device, your other linked devices pick up the change.

Within a guild, you can also set a **guild nickname** from your own profile card. It replaces your display name in that guild only.

## One account, several devices

You can use the same account on a computer and a phone at once. Each device keeps its own device key alongside the shared account key, and the account key signs a certificate saying the device belongs to you. Other members see one person, not two. [Multiple devices](devices.md) covers linking, what is shared, and removing a device.
