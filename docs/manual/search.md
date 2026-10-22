# Search

How to search your messages: what the search covers, the filters you can type, and the limits of a search that runs entirely on your own device.

## Opening search

- **Desktop:** the **Search…** box in the chat header, or `Ctrl+F` (`Cmd+F` on macOS).
- **Phone:** open the conversation's menu from its header and tap **Search**.

Results appear as you type, once there are at least two characters; `Enter` searches immediately. Each result shows where it was posted (`#channel · guild`, or the other person's name for a DM) with your words highlighted. Click one to jump to the message. `Esc` closes the search.

## What is searched

Search runs on your device, over the history stored on it. Your query never leaves the device.

| Included | Not included |
|---|---|
| Messages in every guild and direct message on this device, including Notes | Messages from before you joined that have not synced to this device |
| Your own messages and everyone else's | Deleted messages |
| The text of ordinary messages | Attachments: their contents, file names and descriptions; poll questions and options; doodles |
| The imported archive of the guild you are looking at, as far as its pages are on this device (shown in a separate section) | Anything on another member's device |

Matching is a case-insensitive substring match: `deploy` finds "Deploy", "deployed" and "redeploy". It does not match across spelling variants or stems.

## Filters

Type these anywhere in the query, alone or with words. The results panel lists them as chips; clicking one types it for you.

| Filter | Matches |
|---|---|
| `from:name` | Messages whose sender's display name contains *name* |
| `in:#channel` | Messages in channels with exactly that name (ignoring case), in any guild |
| `has:link` | Messages containing an `http://` or `https://` link |
| `has:image` | Messages with an image |
| `has:file` | Messages with a file attachment |
| `before:YYYY-MM-DD` | Messages sent before that date |
| `after:YYYY-MM-DD` | Messages sent after that date |

Use quotes for a value with spaces: `from:"Ann Lee"`. An unknown filter is treated as ordinary search text. A query made only of filters is allowed, for example `from:bilal has:image`.

## Limits

Message bodies are encrypted on disk, and there is deliberately no search index, because an index would store readable words next to the encrypted messages. Each search decrypts and checks messages instead, newest first, which bounds it:

- A search returns the **50 newest matches** (up to 300 for a filter-only query).
- One search reads at most the **20,000 newest messages** that pass the `from:` and `in:` filters.

`from:` and `in:` narrow the messages read before anything is decrypted, so they let a search reach further back. `has:`, `before:` and `after:` are applied to the results afterwards: they do not reach further back on their own.

## Searching an archive

If the guild you are looking at carries an imported archive (see [Importing a chat archive](guilds.md#importing-a-chat-archive)), a second section shows matches from it. It searches only the archive pages already on this device and does not fetch others while you type; it says how much it covered. To make the whole archive searchable, turn on **Keep the full archive on this device** under **Guild settings → Archive**.

## Other ways to find things

- **Forum boards** have their own **Search posts…** box; see [Forum boards](forums.md#finding-posts).
- **Pinned messages** in the chat header lists a channel's pins.
- `Ctrl+K` opens the command palette, which jumps to guilds, channels and conversations by name. See [Keyboard shortcuts](shortcuts.md).
