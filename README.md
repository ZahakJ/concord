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

```sh
curl -fsSL https://zahakj.github.io/concord/install.sh | sh
```

That installs the single-file build for Linux or macOS into `~/.local/bin`
after checking it against the release's `SHA256SUMS`. Run `concord` and your
browser opens on the app. Windows, the native desktop apps and Android are on
the [releases page](https://github.com/ZahakJ/concord/releases/latest).

![Two Concord instances, each with its own identity, exchanging messages with a live typing indicator](docs/demo.gif)

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
- Desktop (Linux, Windows, macOS), browser-served build, Android

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
