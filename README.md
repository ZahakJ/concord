<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/media/banner.png">
    <source media="(prefers-color-scheme: light)" srcset="docs/media/banner-light.png">
    <img src="docs/media/banner.png" alt="Concord" width="820">
  </picture>
</p>

<p align="center">
  <a href="https://github.com/ZahakJ/concord/actions/workflows/ci.yml"><img src="https://github.com/ZahakJ/concord/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/ZahakJ/concord/releases/latest"><img src="https://img.shields.io/github/v/release/ZahakJ/concord?color=14a394&label=release" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/licence-AGPL--3.0--or--later-14a394" alt="AGPL-3.0-or-later"></a>
</p>

Concord is peer-to-peer, end-to-end encrypted group chat: guilds, channels,
direct messages, voice, video and screen sharing. There is no account server
and no message store. Each guild is an MLS group (RFC 9420), and history lives
on the members' devices.

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/media/screens/hero-dark.webp">
    <source media="(prefers-color-scheme: light)" srcset="docs/media/screens/hero-light.webp">
    <img src="docs/media/screens/hero-dark.webp" alt="Concord on the desktop: the Lantern Makers guild open on #workshop, with a photo, a C code block, reactions and a mention, the guild shortcuts above the channel list, and the member list in the side panel" width="100%">
  </picture>
</p>

```sh
curl -fsSL https://zahakj.github.io/concord/install.sh | sh
```

That installs the single-file build for Linux or macOS into `~/.local/bin`
after checking it against the release's `SHA256SUMS`. Run `concord` and your
browser opens on the app. Windows, the native desktop apps and Android are on
the [releases page](https://github.com/ZahakJ/concord/releases/latest).

## What it looks like

<table>
<tr>
<td width="50%"><picture><source media="(prefers-color-scheme: light)" srcset="docs/media/screens/shelf-light.webp"><img src="docs/media/screens/shelf-dark.webp" alt="The guild switcher open over the sidebar, listing eleven guilds with a search field"></picture><br>Guilds sit in a row above the channel list. Click the guild's name to find any of them.</td>
<td width="50%"><picture><source media="(prefers-color-scheme: light)" srcset="docs/media/screens/appearance-light.webp"><img src="docs/media/screens/appearance-dark.webp" alt="Settings, Appearance: the Layout cards Sidebar, Rail and Compact, the theme choice and the theme packs"></picture><br>Settings › Appearance › Layout: Sidebar, Rail or Compact.</td>
</tr>
<tr>
<td width="50%"><picture><source media="(prefers-color-scheme: light)" srcset="docs/media/screens/rail-light.webp"><img src="docs/media/screens/rail-dark.webp" alt="The Rail layout: a column of guild icons beside the channel list"></picture><br>The Rail layout keeps the guilds in a column.</td>
<td width="50%"><picture><source media="(prefers-color-scheme: light)" srcset="docs/media/screens/callfloat-light.webp"><img src="docs/media/screens/callfloat-dark.webp" alt="A small floating call window, showing a shared screen, over the #workshop conversation"></picture><br>Leave the call view and the call stays in a small window over any channel.</td>
</tr>
<tr>
<td width="50%"><picture><source media="(prefers-color-scheme: light)" srcset="docs/media/screens/callstage-light.webp"><img src="docs/media/screens/callstage-dark.webp" alt="A voice room with two people, one sharing a screen with a lantern pattern on it"></picture><br>A voice room with a shared screen, straight between devices.</td>
<td width="50%"><picture><source media="(prefers-color-scheme: light)" srcset="docs/media/screens/meeting-light.webp"><img src="docs/media/screens/meeting-dark.webp" alt="The Your meeting is live dialog with a guest link, its lifetime choices and an invite for the full app"></picture><br>Instant meeting: a guest link for people without the app.</td>
</tr>
<tr>
<td width="50%"><picture><source media="(prefers-color-scheme: light)" srcset="docs/media/screens/forum-light.webp"><img src="docs/media/screens/forum-dark.webp" alt="A forum channel in the Gallery layout, with tag filters and four posts as cards"></picture><br>Forum channels, here in the Gallery layout.</td>
<td width="50%"><picture><source media="(prefers-color-scheme: light)" srcset="docs/media/screens/thread-light.webp"><img src="docs/media/screens/thread-dark.webp" alt="A forum post open beside its board, with the discussion under the opening message"></picture><br>A post opens beside its board.</td>
</tr>
<tr>
<td width="50%"><picture><source media="(prefers-color-scheme: light)" srcset="docs/media/screens/palette-light.webp"><img src="docs/media/screens/palette-dark.webp" alt="The command palette with the query st, matching a post, an action, a guild and a channel"></picture><br>Ctrl+K finds channels, posts, guilds, people and actions.</td>
<td width="50%"><img src="docs/media/screens/themes.webp" alt="The same conversation split diagonally: the dark theme on the left, the light theme on the right"><br>Light and dark, and theme packs on top.</td>
</tr>
</table>

On a phone, the same app with a tab bar:

<p align="center">
  <img src="docs/media/screens/phone-guilds-dark.webp" alt="Phone: the Guilds tab with the Lantern Makers channels" width="24%">
  <img src="docs/media/screens/phone-chat-dark.webp" alt="Phone: the #workshop conversation with a code block, reactions and a mention" width="24%">
  <img src="docs/media/screens/phone-dm-dark.webp" alt="Phone: a direct message with Tobin" width="24%">
  <img src="docs/media/screens/phone-voice-dark.webp" alt="Phone: a voice room with three people" width="24%">
</p>

<p align="center"><img src="docs/demo.gif" alt="Two Concord instances on phones, each with its own identity, exchanging direct messages with a live typing indicator" width="560"></p>

## Using it

Pick a passphrase; the first unlock creates your identity, an Ed25519 keypair
stored on the device. Create a guild and send its invite code to a friend.

Two peers must be able to reach each other:

| Where you are | What it takes |
|---|---|
| Same Wi-Fi, or the same Tailscale/WireGuard mesh | Nothing. |
| One side can forward a port | One router rule. The invite code carries that address. |
| Neither can | Someone in the group runs a [rendezvous](docs/RENDEZVOUS.md): one small always-on node that introduces peers and relays ciphertext. |

**Can people reach me?** in the command palette (Ctrl+K) reports which row
you are in.

## Features

- Guilds with channels, categories, forums, roles and permissions
- Replies, edits, reactions, pins, mentions, polls, events, Markdown
- Direct messages, delivered through an encrypted offline mailbox when the
  recipient is away; message requests from strangers
- Voice, video and screen sharing over a WebRTC mesh; push-to-talk
- Meeting rooms that guests can join from a browser without installing
- Encrypted attachments, custom emoji, local full-text search, Markdown export
- Several devices per identity, linked by QR code
- Import of an exported community's history into a guild archive
- [Bots](docs/BOTS.md): a script with its own identity, speaking JSON lines
- Desktop apps for Linux and Windows, a browser-served build for Linux, macOS
  and Windows, and Android

Emoji, fonts, icons and sounds are compiled in. Nothing is fetched from a
third party unless you switch on a feature that says it will;
[PRIVACY.md](PRIVACY.md) lists every outbound request the app can make.

## Building

Go (version in `go.mod`) and Node 22+.

```sh
git clone https://github.com/ZahakJ/concord && cd concord
make web          # -> bin/concord-web, serves the UI on 127.0.0.1:8787
make gui          # native window, needs webkit2gtk-4.1
make peers        # two isolated local peers for development
make help         # everything else
```

## Documentation

The manual is at **[zahakj.github.io/concord/manual](https://zahakj.github.io/concord/manual/)**:
installing on each platform, guilds, calls, devices, backups, privacy,
moderation and troubleshooting, with developer pages on the architecture,
protocol, building and testing. Its sources are the Markdown in
[docs/manual](docs/manual) and [docs/dev](docs/dev); the documents below are
pages of it too.

| | |
|---|---|
| [DESIGN.md](docs/DESIGN.md) | How it works: identity, discovery, NAT traversal, MLS, sync, threat model |
| [RENDEZVOUS.md](docs/RENDEZVOUS.md) | Running the optional node |
| [BOTS.md](docs/BOTS.md) | Writing a bot |
| [PUSH.md](docs/PUSH.md) | Optional push wakes |
| [FAQ.md](docs/FAQ.md) | Common questions |
| [PRIVACY.md](PRIVACY.md) | What data exists and who can read it |
| [SECURITY.md](SECURITY.md) | Reporting a vulnerability; known gaps |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Development loop, tests, code style |
| [RELEASING.md](docs/RELEASING.md) | Maintainer runbook |
| [WINDOWS.md](WINDOWS.md) | SmartScreen and Defender |
| [THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) | Bundled assets and licences |

The optional meme-template pack is not in the repository; see
[frontend/public/memes/README.md](frontend/public/memes/README.md).

## Licence

GNU Affero General Public License, version 3 or later. See [LICENSE](LICENSE).
