# Sharing files and media

How images, videos, voice messages and other files are sent, encrypted and fetched, along with custom emoji, the guild GIF pack, doodles, memes, sounds, and what link previews reach out to.

## Attaching files

Click **Attach a file** beside the composer, paste a file, or drop it onto the conversation. On a phone, use **+** → **Photo or file** or **Camera**. Attachments are staged above the text box and nothing is sent until you send the message.

| Kind | Limit | How it shows |
|---|---|---|
| Images | 5 MB | Inline, in a grid when there are several. Click to open the viewer: click or scroll to zoom, drag to pan, `Esc` to close. |
| Other files | 25 MB | Videos (`video/*`) play inline and load only when scrolled into view. Audio shows as a voice message player. Anything else is a card with name, size and a download button; it is fetched only when you click. |

**Images** in PNG, JPEG, GIF or WebP format that are already under 5 MB are sent byte for byte, so an animated GIF stays animated, and any metadata inside the file (such as camera details or location) goes with it. Other image formats, and images over 5 MB, are re-encoded as JPEG to fit; if one still does not fit, the app says so and nothing is sent. An image the app cannot read is sent as a plain file instead.

Before sending, each staged image has its own controls:

- **Mark as spoiler**: it arrives blurred until clicked.
- **Edit name and description**: a file name and a description for the image.
- **Remove attachment**.

Right-click an image (in the feed or the viewer) to **Copy image** or **Save image**.

### Voice messages

When the text box is empty, the composer shows **Record a voice message**. Record, then listen back, **Record again**, **Discard recording** or **Send voice message**. A voice message is an encrypted audio file like any other.

## How attachments travel

Attachments are never stored on a server and never ride inside the message stream. (When two devices cannot connect directly, the encrypted bytes may pass through a relay, which cannot read them.)

1. When you send, your device encrypts the file with a fresh random key and stores the encrypted copy locally.
2. The message carries only a short reference: the encrypted file's ID and the key. The message itself is end-to-end encrypted, so only members can read the key.
3. A member's device fetches the encrypted file directly from a peer that holds it and decrypts it.

Any member who has already loaded an attachment can serve it to others. Without the key the stored bytes are unreadable, and the ID cannot be guessed. This is why an image stays available after its sender goes offline, as long as someone who has viewed it is online. If nobody who holds it is reachable, the attachment says so and names who sent it; it loads once they, or another holder, come online.

Each device keeps up to **1 GiB** of attachments. Past that, the ones used least recently are dropped from that device; they can be fetched again from any member who still has them.

## Custom emoji

Each guild has its own emoji. Open **Guild settings → Guild emoji** (or **Guild emoji** in the guild's right-click menu) to see them. Anyone in the guild can use one by typing `:name:` in a message or by picking it for a reaction.

Members with **Manage guild** can add and remove them:

- Name: 2 to 32 characters, lowercase letters, digits and `_`.
- Image: PNG, JPEG, GIF or WebP, up to 256 KiB as stored. Animated GIFs keep their animation.

A message made only of emoji shows them large.

## GIFs

The **GIF** button beside the composer, or `/gif`, opens the picker. It has two tabs.

- **This guild**: the guild's own GIF pack. Searching it is a match against names and tags already on your device, so nothing leaves it. Members with **Manage guild** can **Add a GIF** (up to 5 MB, with a name and up to 12 tags) or remove one.
- **Search**: a public GIF service, reached **through your own rendezvous node**. The service sees the node, not you, and your device never connects to it: previews and the GIF you pick come back through the node. The node's operator can see your search terms. The tab names the service the node uses. Search is on by default and can be switched off under **Settings → Privacy & safety**. Members with **Manage guild** can save a result into the guild pack.

A GIF you post, from either tab, is sent as an ordinary encrypted image attachment.

## Doodles, memes and sounds

All three are in the composer's **+** menu or have a slash command.

- **Doodle** (`/doodle`): draw on a pad. A doodle is sent as a list of strokes rather than a picture, so it stays sharp at any size and follows your theme.
- **Meme** (`/meme`, or **Make a meme** on any image's right-click menu): add captions to a bundled template or to an image from the conversation. It is rendered on your device and sent as a normal image. On the device that made it, your own meme has **Edit meme**, which reopens it in the editor and saves back over the message.
- **Sound** (`/sound`): play a sound effect into the channel, or build your own. A sound is a short recipe of a few dozen bytes that each device turns into audio, not an audio file.

## Link previews

Link previews are **off by default**. Turn them on with **Settings → Privacy & safety → Link previews**.

Fetching a preview means your device contacts the linked site, which learns your IP address and when you were online, without you clicking anything. That is why the setting starts off. With it on:

- your device fetches the page's title, description and image when the message scrolls into view;
- it reads at most 512 KiB of the page and gives up after 5 seconds;
- it sends no cookies or login details and refuses to contact addresses on your own machine or local network.

Links to a supported video host get a click-to-play player. With link previews off it shows a plain placeholder instead of the video's thumbnail, and contacts the site only when you click play.

Nothing is fetched for messages in an imported archive. See [Privacy and encryption](privacy.md) for everything that leaves your device.
