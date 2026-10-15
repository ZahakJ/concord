# Notifications and push

How Concord tells you something happened: notification levels for each guild and channel, mentions, alert words, sounds, desktop and Android notifications, the Android **Stay connected** service, and optional push wakes.

## Where the settings are

Most of it is in **Settings → Notifications & sounds**. Levels for a single guild or channel are on that guild's or channel's own menu: right-click it on the desktop, or long-press it on a phone.

## Notification levels

Every guild and every channel has one of three levels:

| Level | What it does |
|---|---|
| **All messages** | Every message pings. |
| **Only @mentions** | Unread messages still show, but only mentions, alert words and direct messages ping. |
| **Nothing** | Silent. The channel is greyed out and shows no badge, and it is left out of the guild's unread count. |

A channel uses its own level if you set one, otherwise its guild's, otherwise **All messages**.

- **Guild:** open the guild's menu in the guild rail and pick a level under **Notifications**.
- **Channel:** open the channel's menu and pick a level under **Notifications**, or **Use guild default** to go back to following the guild.
- **Mute a channel quickly:** on the desktop, the bell on a channel row toggles **Mute channel** / **Unmute channel**. On a phone, a muted channel shows the bell, and tapping it unmutes. The quick switcher (`Ctrl+K`) also offers **Mute #channel** and **Unmute #channel**. Muting sets the channel to **Nothing**. Unmuting returns it to the guild default.

Mutes last until you undo them; there is no timed mute. Levels are stored on this device only and are not synced to your other devices.

## Mentions

A message counts as mentioning you when it contains:

- `@` followed by your display name;
- `@` followed by the name of a role you hold;
- `@everyone` or `@here`.

Mentions are matched case-sensitively. `@everyone`, `@here` and role mentions count only when a guild member sends them, not a meeting guest. In a direct message, every message counts as a mention.

## Alert words

Alert words ping you the way your own name does. Add them in **Settings → Notifications & sounds → Alert words**: type a word or short phrase and choose **Add**.

- Each word is 2 to 64 characters, and the list holds at most 50.
- Matching ignores case and only matches whole words, so `cat` does not match `concatenate`.
- Your own messages never trigger them.
- A matching message gets the same badge, sound and notification as a mention, and the word is highlighted in the message.

Alert words are stored on this device only. They are never sent anywhere, never shared with your other devices, and never part of any guild.

## Do Not Disturb

**Settings → Notifications & sounds → Do Not Disturb** stops every ping, mentions included. Badges still count, and you stay online. It is the same as your Do Not Disturb status, so switching it off sets you back to Online.

## Sounds

| Setting | Default | What it does |
|---|---|---|
| **Volume** | 100% | Master volume for everything Concord plays. At 0 it shows **Off** and all sounds stop. |
| **Sound effects** | On | Soundboard presses in a voice room, and sound chips people send you. |
| **Blip on every message** | Off | A soft note when a message arrives in the channel you are already reading. |
| **Call ringtone** | Classic | The ringtone for incoming calls: Classic, Digital, Chime, Marimba or Pulse. Picking one plays it. |
| **Test the speakers** | — | Plays a chime and checks that it reached your speakers. |

Direct messages play their own chime, and mentions play a ping.

## Desktop notifications

On Linux, macOS, Windows and the web build, Concord asks for permission to show notifications when you sign in. After that, a notification appears when:

- a new message arrives in a channel set to **All messages**, while the window is hidden;
- you are mentioned, an alert word matches, or a direct message arrives, while the window is hidden or just not focused.

Only new messages from other people notify. Catching up on history after being offline does not. The notification shows the sender's name (with `@` in front for a mention) and the first 120 characters of the message; there is no setting to hide the text. Each channel has at most one notification at a time. Click it to jump to the channel.

Incoming calls ring inside the app; they do not post a system notification. Neither do message requests.

The window title shows how many mentions are waiting, for example `(3) #general — Concord`.

## Badges, the Inbox, and marking read

- The guild rail shows an unread count on each guild you are not looking at. It turns red when any of it mentions you or matches an alert word. Folders add up the guilds inside them.
- The **Inbox** (the bell on the guild rail) collects every mention, reply and alert word aimed at you, with **Mentions** and **Replies** filters and **Mark all read**.
- **Mark as read** is on every guild, channel and direct message menu, and on the new-messages divider. `Esc` marks the current channel read, and `Shift+Esc` marks everything read.

Read state syncs to your other linked devices and to nobody else.

## Android

### Permission

Android 13 and later need permission to post notifications. Concord does not ask at startup. The first time you would have been notified, a bar appears: **You just missed a message.** Choose **Enable** to grant permission. You can also grant it under **Settings → Notifications & sounds → Show notifications**, which opens the system settings if you blocked it earlier.

### Message notifications

Messages stack by conversation, up to six lines each. Tap one to open the channel. Two actions are offered:

- **Reply** answers from the notification. It needs the phone unlocked on Android 12 and later, and is not offered when **App lock** is on.
- **Mark read** marks the conversation read on all your devices and clears the notification.

The levels above decide what notifies for as long as the interface is running. Once Android has closed the interface but the connection is still running, only **direct messages** notify, because the guild and channel levels live in the interface's own storage.

The Android notification channels are **Messages** (new messages and mentions), **Connection** (the Stay connected service) and **Calls** (shown during a call, with a **Hang up** action). You can tune each one in Android's own app settings.

### Stay connected

There is no server holding your messages, so Concord has to be running to receive them. **Settings → Notifications & sounds → Stay connected** keeps it running after you close the app. It is **on** by default.

- It runs as a foreground service and shows a permanent, minimum-priority notification: **Connected — you'll receive messages**.
- If Android stops the process, the service restarts it.
- On Android 15, the system limits this kind of service to about six hours a day. When that runs out, the service stops and starts again the next time you open Concord.

With **Stay connected** off, or when the service is not running, messages sent to you wait in your rendezvous's offline mailbox and arrive when you next open the app.

## Push wakes

Push closes the gap when Concord is not running: a rendezvous that holds push credentials sends your phone a **contentless wake** when something lands in your mailbox. The wake carries no sender and no text. Concord wakes, collects the mailbox and decrypts on the phone, exactly as if you had opened it.

Push is optional and **not enabled in the published builds**. It needs a build made with a Firebase configuration file and a rendezvous given matching credentials; without both, nothing registers and nothing is sent. A node with push learns nothing new about you: device tokens are filed under your opaque mailbox tag, not your identity, wakes are limited to one per mailbox every 30 seconds, and tokens not refreshed in 60 days are forgotten.

Running a node with push does mean trusting it with platform credentials. Self-hosting without push works fully. The setup steps, for whoever publishes a build or runs a node, are in [Push wakes](../PUSH.md).
