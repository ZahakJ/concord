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

**Images** in PNG, JPEG, GIF or WebP format that are already under 5 MB keep their picture data unchanged, so an animated GIF stays animated, but the details stored beside the picture are removed before it is sent: location, camera make and model, dates, embedded thumbnails, XMP and IPTC data, comments and maker notes. What is kept is what the picture needs to look right: its colour profile and, when the picture is not upright, a tag saying which way up it goes. Images in other formats, HEIC and AVIF among them, and images over 5 MB, are re-encoded as JPEG to fit when the app can display them, which leaves their details behind; if one still does not fit, the app says so and nothing is sent. In a message, a picture the app cannot display is sent as a file and cleaned as described below. A PNG, JPEG, GIF or WebP too damaged to clean is refused with a message saying so.

The same cleaning applies to every picture you send or set: from the composer, by paste or drop, from the phone's share sheet, as a GIF from search, and as an avatar, banner, guild icon, forum banner, story, custom emoji or guild GIF. Ultra HDR photos from recent phones arrive as ordinary pictures, because their extra brightness information is stored with the details that are removed. Pictures sent before Concord 0.61 are not changed.

**Videos in MP4 or MOV**, **HEIC, HEIF and AVIF pictures** sent as files, and **M4A audio** lose their location, camera make and model, dates and other stored details before they are sent. A video also loses its tracks that are neither picture nor sound, such as the per-frame location some phones and action cameras record, and its timecode. The picture and sound themselves are not changed, so a location a camera writes into the video stream itself stays.

Some files are held before they are sent, on their own row in the conversation, and nothing goes out until you choose:

- A video with subtitles or other text tracks, which can hold location: **Remove and send** removes them along with the other details, and **Send as it is** sends the file unchanged.
- A file Concord cannot clean safely: **Send as it is**. This covers an MP4, MOV, HEIC or AVIF file it cannot rewrite with certainty, a video in another container (MKV, WebM, AVI, MPEG transport or program stream, WMV, FLV), and TIFF, camera raw, JPEG XL and JPEG 2000 pictures. Video clips from Sony, Canon and Panasonic cameras that have PCM sound and a timecode track are held this way for now.

The advanced composer and a new forum post do not send a held file; they say to attach it in the message box, where the choice is offered.

Other files are sent exactly as they are, with whatever they hold: documents, audio files other than M4A and WebM (MP3, Ogg, WAV, FLAC and the like), and any file Concord does not recognise. The composer says so under the staged file before you send. An attached WebM or other Matroska file is held like a video in a container Concord does not read, even when it holds only sound.

A voice message you record in Concord is sent as it is when the recording is Ogg or WebM audio, with no line under it; any other recording is cleaned or held as an attached file would be.

A picture sent with text is one message: the text is its caption, and the two arrive, show and disappear together.

Concord keeps a song's title, artist and album. A voice recording's title is removed, since phones often name recordings after the place.

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

- your device loads the page once: for YouTube and Vimeo, their oEmbed service and the server of the video's picture; for other sites, the page and the host of its picture. They see your IP address, as if you had opened the link;
- it reads at most 512 KiB of a page, gives up on the whole card after 6 seconds, sends no cookies or login details, and refuses addresses on your own machine or local network;
- the message waits at most 2 seconds for its card, and goes out without one if the card is slower, if you send anything else into the conversation first, or if you leave the app;
- nothing is fetched while you type, and a link sent again within 10 minutes reuses the card already made.

A link sent without a card is shown as it was typed. A YouTube link with no card shows a placeholder, and YouTube is contacted only when you tap it. Tapping a card's play button loads the video from `youtube-nocookie.com`; tapping anything else on a card opens the link in your browser.

Older versions show a card as its title as a link, the domain, and the picture with the site's name under it.

