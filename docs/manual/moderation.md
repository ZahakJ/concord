# Moderation and safety

The tools for keeping yourself and a guild safe: blocking, message requests, reporting, muting and removing members, deleting messages, invites, and how a guild governs itself without a server.

## How moderation works without a server

There is no company and no central moderation team in Concord. A guild is moderated by its own owner and the members they give permissions to. Every moderation decision (a kick, a ban, a role change) is a signed entry in the guild's **governance log**. Every member's app replays that log and checks each signature and permission itself, so a decision that the person making it was not allowed to make is ignored everywhere.

Some rules depend on each member's app honoring them: mutes, slow mode, announcement-only channels and call moves. An honest app enforces them on both sides: it refuses to send, and other members' apps drop what should not have been sent. A deliberately modified app could ignore them on its own screen. Removing someone from a guild is different: it changes the group's encryption keys, so a removed member cannot read anything new whatever app they run.

## Blocking

Blocking is personal. It changes what **your device** shows you and does not remove anyone from anything.

To block someone, choose **Block** from:

- their profile card's menu, or their entry in the member list (right-click);
- the menu on a 1:1 direct message in your DM list;
- the **Report message** dialog;
- a message request.

Once blocked, the person's messages, forum posts, reactions, typing indicator, moments and presence in calls are hidden from you, they cannot start a direct message with you or add you to a guild, and your 1:1 conversation with them is closed. They stay in any guilds you share, and other members still see them. They are not told.

The list is in **Settings → Privacy & safety → Blocked users**, with **Unblock** beside each name. Unblocking brings everything back, because nothing was deleted. The list is kept on this device only: it does not sync to your linked devices and is not in your backup, so each device needs its own.

## Message requests

A direct message from a stranger waits as a **message request** instead of opening. A stranger here means someone you have not verified, share no guild or direct message with, and have never contacted first.

Requests appear in a **Message requests** row at the top of your direct messages, with a dot on the guild rail, and in **Settings → Privacy & safety → Message requests**. For each one you can:

- **Accept**: join the conversation. The sender needs to be online for this to work; if they are not, the request stays.
- **Delete**: drop it without telling them.
- **Block**: drop it and block them.

Until you accept, the sender learns nothing: not your profile, not whether you are online, not even that you exist. Requests you do not answer are dropped after 30 days.

## Reporting a message

Choose **Report message** from the menu on someone else's message. Nothing you do in this dialog leaves your device, and no one receives a report. There is no moderation team and no server copy of the message to act on. The dialog offers what can be done:

- **Hide this message**: stops it showing on this device. There is no way to show it again from the app.
- **Block** *name*: as described above.
- **Export evidence**: saves a file with the message, its timestamps and the sender's safety number, for you to hand to someone who can act. On platforms where Concord cannot save a file, it is copied to the clipboard instead.

To have a message removed or a member removed for everyone, ask the guild's admins. A problem with Concord itself, rather than with a person, goes to the maintainers through the private reporting route in [SECURITY.md](../../SECURITY.md).

## Moderation permissions

Moderation tools need permissions that come from roles. See [Roles and permissions](roles.md).

| Permission | Allows |
|---|---|
| **Manage members** | Creating invites, kicking, banning and lifting bans |
| **Mute members** | Muting members for a set time |
| **Manage messages** | Deleting anyone's messages, reading deleted messages, closing forum posts, posting in announcement channels |
| **Manage channels** | Channel settings including slow mode, locking calls |
| **Manage guild** | Guild-wide settings including message history |

The owner can always do everything and cannot be kicked, banned or muted. Anyone with **Manage members** can kick or ban any member except the owner, whatever their roles.

## Kicking and banning

Right-click a member in the member list, or open the menu on their profile card, and choose **Kick** or **Ban**. Both ask for an optional reason, which goes in the moderation log.

- **Kick** removes them now. They cannot rejoin with an invite code until someone with **Manage members** chooses **Allow back** on the kick entry in the **Moderation log**, or adds them back directly as a verified contact. (The kick dialog says they can rejoin with a new invite; the app refuses that until they are allowed back.)
- **Ban** removes them and takes away their roles. They cannot rejoin, from any device or with any invite, until the ban is lifted.

Either way, every device on their account is removed from the guild's encryption group and the group gets new keys, so they cannot read anything sent afterwards. Messages they posted before stay.

Bans are listed under **Banned members**, in the guild's settings under **People** and in the guild menu; it needs **Manage members**. Choose **Unban** to lift one; they can then join again with an invite.

A removed member can still connect through members' devices as a relay until those apps restart. They cannot read anything, but they are not cut off from the network at once. See [DESIGN.md §13](../DESIGN.md#13-threat-model-what-concord-defends-against).

## Muting a member

Members with **Mute members** can choose **Mute…** on a member and pick how long: 60 seconds, 5 minutes, 10 minutes, 1 hour, 1 day, 1 week, or a custom time up to one week. A muted member cannot send messages in the guild until the mute ends or someone chooses **Unmute**. A mute does not affect calls.

## In calls

- **Mute** *name* **for me**, on a person's tile, silences them on your device only. Anyone can do it.
- Moderators (owner, **Manage members** or **Mute members**) can right-click someone in a voice channel and choose **Move to…** another voice channel, or **Disconnect** them from the call. Their app checks the moderator's permission and obeys.
- **Lock call** makes people knock before joining. See [Voice and video](voice-and-video.md#locked-calls).

There is no way for a moderator to mute someone else's microphone for everyone.

## Deleting messages

Choose **Delete** from a message's menu. You can always delete your own; deleting other people's needs **Manage messages**. Moderators can also type `/clear` followed by a number to delete the last messages in a channel, up to 100.

What happens depends on where the message was:

- **In a guild**, the message disappears for members, but every member's app keeps the original, still encrypted, so that moderators can choose **Show original** on it later.
- **In a direct message**, the text is erased for both of you.

Two settings in **Settings → Privacy & safety** control what you see of deleted messages:

- **Show deleted messages** (off by default) leaves a faint marker where a deleted message was.
- **Empty trash** erases every deleted message's retained text on this device for good. Other members' devices keep their copies until they do the same.

Edited messages show **(edited)**. Only the latest version is kept.

## Other guild tools

- **Slow mode**: in a channel's **Channel settings…**, allow one message per member every 5 seconds to 15 minutes. Moderators are exempt.
- **Announcement channels**: create a channel of type **Announce**, and only members with **Manage messages** or **Manage channels** can post in it.
- **Closing forum posts**: **Close post** stops replies. It needs **Manage messages**. See [Forum boards](forums.md).
- **Message history**: a guild can agree to keep messages for 24 hours up to a year, guild-wide or per channel. Each member's app deletes its own copy; pinned and saved messages are kept. A modified app could keep everything, so treat it as an agreement, not a guarantee.

## Invites

Creating an invite needs **Manage members**. **Invite a friend** shows a code, a QR code, and **Copy code** or **Share code…**. Anyone with the code can join, so share it only with people you trust. See [Guilds and channels](guilds.md).

Invite codes do not expire, have no use limit and cannot be revoked. A code only identifies the guild and where to reach it, and joining is approved by a member with **Manage members** whose device is in sync, checking the ban list. If a code leaks:

- ban or kick anyone who should not have joined;
- remove **Manage members** from people who do not need it, since only their devices can admit newcomers.

A direct invitation to a verified contact can be withdrawn with **Cancel invite** in the member list before it is accepted.

## Governance and ownership

The **Moderation log**, in the guild's settings under **People**, shows every signed decision, and every member can read it. Each entry is checked on your own device and marked **Signature verified**, **Signed, but not permitted**, or **Signature does not verify**.

- **Roles have a ranking.** Nobody can create or assign a role ranked at or above their own, or give a role a permission they do not have.
- **Ownership** passes with **Transfer ownership…**, which is immediate and one-way.
- **An heir** can be named with **Name an heir…**. The heir can choose **Take ownership** at any time, so that a guild survives its owner disappearing. The owner can revoke the heir until then.

These are under **Danger zone → Ownership & heir** in the guild's settings, visible to the owner and the heir. When an owner leaves (**Leave and delete my copy**), nobody can add or remove members afterwards, so hand ownership on first.

The design behind this, and its limits, is in [DESIGN.md §7.4](../DESIGN.md#74-authorising-membership-changes-with-no-server-to-arbitrate).
