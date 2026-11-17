# Sharing files and media

How images, videos, voice messages and other files are sent, encrypted and fetched, along with custom emoji, the guild GIF pack, doodles, memes, sounds, and the preview cards on links.

## Attaching files

Click **Attach a file** beside the composer, paste a file, or drop it onto the conversation. On a phone, use **+**: **Photos and videos** at the top opens your phone's own photo picker, where you can pick up to 10; **Files** opens the file browser; **Camera** takes a picture. Attachments are staged above the text box and nothing is sent until you send the message.

Concord never asks for access to your photo library or your phone's storage. The photo picker is drawn by Android, and Concord receives only the items you pick.

An edit changes only a message's text, so pasting an image while you edit a message does nothing. Send the image as a new message.

| Kind | Limit | How it shows |
|---|---|---|
| Images | 5 MB | Inline, in a grid when there are several. Click to open the viewer: click or scroll to zoom, drag to pan, `Esc` to close. |
| Other files | 25 MB | Videos (`video/*`) play inline and load only when scrolled into view. Audio shows as a voice message player. Anything else is a card with name, size and a download button; it is fetched only when you click. |

**Images** in PNG, JPEG, GIF or WebP format that are already under 5 MB keep their pixels exactly, so an animated GIF stays animated, but their metadata (camera details, location and the like) is removed before they are sent, whichever way they were attached. Other image formats, and images over 5 MB, are re-encoded as JPEG to fit; if one still does not fit, the app says so and nothing is sent. An image the app cannot read is sent as a plain file instead.

**Videos**, and **HEIC or HEIF photos** the app cannot read, are sent as they are, including any location stored in them. The composer says so under the staged item before you send.

Before sending, each staged image has its own controls:

- **Mark as spoiler**: it arrives blurred until clicked.
- **Edit name and description**: a file name and a description for the image.
- **Remove attachment**.

Right-click an image (in the feed or the viewer) to **Copy image** or **Save image**.

### Voice messages

When the text box is empty, the composer shows **Record a voice message**. Record, then listen back, **Record again**, **Discard recording** or **Send voice message**. A voice message is an encrypted audio file like any other.

## Media, files and links in a conversation

Open the inspector and choose **Media** to see every photo, video and file this device holds for the conversation, not only what is loaded in the feed. **Photos and videos** is a grid by month, newest first; **Files** is a list. In a direct message, the **Media and files** button in the header (on a phone, in the top bar) opens it.

- Tap a photo or video to open it. **Go to message** takes you to the message it came in.
- Right-click (long-press on a phone) for **Go to message**, **Save** and **Copy image**.
- Tap a file to go to its message.

Deleted, hidden and expired messages never appear. A tile shows the picture once this device has opened it in this session; otherwise it shows a placeholder the shape of the picture. Pictures this device does not hold are fetched while their tiles are on screen; on a metered connection (mobile data) they wait for a tap. A video shows its length once it has been played here.

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

## Link cards

A link card shows a link's title, the site or channel name and a small picture, with the link's real domain printed under them. The card is made by the **sender's** device, once, when the message is sent, and travels inside the encrypted, signed message. Reading a card contacts nobody: the picture comes from other members like any attachment.

Sending cards is **off until you choose**. The first time you send a link, it goes out as you typed it, and the app asks once: *Concord will look up the title and picture of each link you send, from your device, so others see a card.* **Yes** turns cards on from your next link; **No thanks** leaves them off, and the question is not asked again. **Settings → Privacy & safety → Preview cards for links you send** is the same choice: the line under it says whether you have chosen yet, and setting it there answers the question, which is then never asked. You can change it there at any time. With it on, when you send a message with a link:

- your device loads the page once: for YouTube and Vimeo, their oEmbed service; for other sites, the page and the host of its picture. They see your IP address, as if you had opened the link;
- it reads at most 512 KiB of a page, gives up on the whole card after 6 seconds, sends no cookies or login details, and refuses addresses on your own machine or local network;
- the message waits at most 2 seconds for its card, and goes out without one if the card is slower, if you send anything else into the conversation first, or if you leave the app;
- nothing is fetched while you type, and a link sent again within 10 minutes reuses the card already made.

A link sent without a card is shown as it was typed. A YouTube link with no card shows a placeholder, and YouTube is contacted only when you tap it. Tapping a card's play button loads the video from `youtube-nocookie.com`; tapping anything else on a card opens the link in your browser.

Older versions show a card as its title as a link, the domain, and the picture with the site's name under it.

