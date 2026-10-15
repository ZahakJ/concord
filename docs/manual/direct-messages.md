# Direct messages

Private conversations outside any guild: one-to-one messages, group DMs, your own Notes, message requests from people you do not know, and how messages reach someone who is offline.

## Where DMs live

Click the home button (**Home / Direct messages**) at the top of the guild rail. The channel list becomes **Direct messages**, with **Notes** (a conversation with yourself, synced to your own linked devices), your conversations, and **Message requests** when any are waiting.

Every DM is its own end-to-end-encrypted group, just like a guild, with no roles or permissions inside it. Both sides can pin, and a delete erases the text for real on every device rather than leaving it for a moderator to reveal.

## Starting a conversation

- **From a profile.** Click someone's name or avatar anywhere and type into the **Message @name** box on their card. This opens your one conversation with them, creating it if needed; there is only ever one 1:1 conversation per person.
- **From the list.** Click **New** beside **Direct messages** to open **New message**, search your contacts and pick one.
- **With someone not in your contacts.** In **New message**, choose **Invite by link…**. This makes a fresh DM and shows its invite code; send it to them by any route. The first person to redeem it becomes the other side of the conversation, and anyone else who redeems it turns it into a group DM, so give it to one person.

If the other person is offline when you start a conversation, the invite is kept and delivered when they next connect. You can write straight away.

## Group DMs

Pick two or more contacts in **New message** to make a **group DM**. Every person in it must be a contact you have **verified**; see [Accounts and identity](identity.md). A group DM holds two to ten people besides you. Choosing the same set of people again reopens the existing conversation instead of creating a second one.

In a group DM, the chat header menu has **Rename group**.

## Closing a conversation

Right-click a 1:1 conversation in the list and choose **Close DM**. It is hidden from your list, not deleted: messaging each other brings it back with its history.

For a group DM the item is **Leave group**, and it works like leaving a guild: your copy is removed from this device and you need a new invite to come back.

## Disappearing messages

**Disappearing messages…** in the conversation's menu sets a timer (**5 minutes** to **1 week**) for the messages *you* send there from then on. Each one erases itself on every device when its time runs out. See [Guilds and channels](guilds.md#disappearing-messages).

## Message requests

A DM from someone you have no relationship with does not land in your conversation list. It waits under **Message requests**.

Someone counts as known, and their DMs go straight through, if any of these is true:

- you have verified them,
- you share a guild or another conversation with them,
- you started a conversation with them yourself,
- it is one of your own linked devices.

Everyone else is a stranger. Until you accept, a stranger gets nothing from you: not your profile, your presence or your typing and read signals. Accepting is what actually joins you to their conversation; until then you hold only their invite. A waiting request shows as a dot on the home button, never as an unread count.

For each request you can:

- **Accept**: join the conversation. What they wrote while waiting arrives through the normal history sync.
- **Delete**: drop it without telling them.
- **Block**: drop it and stop them contacting you again. See [Moderation and safety](moderation.md).

Requests nobody answers are dropped after 30 days, and at most 200 are kept (oldest go first).

## When messages are delivered

There is no server holding conversations, so delivery happens between devices:

1. **Live.** If the other person's device is connected to yours, directly or through a relay, the message goes straight there.
2. **The offline mailbox.** If you use a rendezvous node, every message you send (in a DM or a guild) is also sealed for each member device you are not connected to and left in that device's mailbox on the rendezvous. The node cannot read it and cannot tell who it is from; it sees only an opaque address tag. The recipient's device collects its mail when it connects to the rendezvous, and checks again about once a minute while it runs.
3. **History sync.** When a device reconnects, members who are online send it what it missed. This covers anything the mailbox did not hold.

The mailbox is a short-term safety net, not storage:

| Limit | Value |
|---|---|
| How long a deposit waits | Up to 14 days |
| Deposits held per device | 200 |
| Size of one deposit | 64 KiB (attachments travel separately; see [Sharing files and media](files-and-media.md)) |
| Where it is kept | The rendezvous node's memory; a restart clears it |

So a message reaches someone who was offline when they next come online, either from the mailbox or from any member who was online when it was sent. Two people who are never online at the same time, with no rendezvous and nobody else in the conversation, have no path between them until they are.

On Android, a message can also wake the app; see [Notifications and push](notifications.md). How rendezvous nodes work is covered in [Running a rendezvous](../RENDEZVOUS.md).
