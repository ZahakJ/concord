# Bots

A bot in Concord is a peer. It has its own identity, it is invited into a guild
the way a person is, and it reads what that membership lets it read: the
guilds it was invited to, nothing more. Its messages are end-to-end encrypted
like anyone's, and the guild can kick it like anyone. Nothing about encryption
changes to make room for it, because nothing has to.

`concord --bot` is the front end for a script. It boots an identity and the
network, then speaks newline-delimited JSON: one event per line on stdout, one
command per line on stdin. Logs go to stderr. Any language that can read and
write lines can drive it.

## Create one

Build the headless binary (`make cli` puts it in `bin/concord`), then give the
bot a data directory and passphrase of its own:

```sh
export CONCORD_HOME=~/bots/dice        # the bot's identity and history live here
export CONCORD_PASSPHRASE='…'          # unlocks it; set it the same way every run
concord --status                       # creates the identity, prints its fingerprint
```

`CONCORD_BOOTSTRAP` and `CONCORD_DISABLE_MDNS` work as they do for
`concord --serve`. One data directory is one bot; never run two processes on
the same one.

## Invite it

Copy an invite code from the guild in the app, exactly as you would for a
person, and hand it to the running bot:

```json
{"cmd":"join","invite":"CI1ARDg…"}
```

A member who can admit it must be online at that moment, as for anyone. The
membership is stored in `CONCORD_HOME`, so later runs need no invite. Both
examples below take the code as an optional first argument and send this for
you. Keep the bot running for a little while after it first joins: like a
person's client, it needs a moment to settle into the guild.

## Events (stdout)

Every line is one JSON object with a `type`.

| `type` | When | Fields |
|---|---|---|
| `ready` | Once, at start | `fingerprint`, `peerId`, `name`, `guilds` (see `list`) |
| `message` | Someone else posted | `guildId`, `guild`, `channelId`, `channel`, `messageId`, `sender` (fingerprint), `senderName`, `content`, `sent` (RFC 3339, UTC), `replyTo`?, `kind`?, `dm`? |
| `edit` | A message the bot saw was edited | `guildId`, `channelId`, `channel`, `messageId`, `sender`, `content` |
| `delete` | A message the bot saw was deleted | `guildId`, `channelId`, `channel`, `messageId` |
| `member` | Someone joined or left a guild the bot is in | `action` (`join`/`leave`), `guildId`, `guild`, `fingerprint`, `name` |
| `guild` | The bot joined or left a guild | `action`, `guildId`, `guild` |
| replies | Answer to a command | see below; carries the command's `id` |
| `error` | A command failed or a line was malformed | `error`, `id` (if the line had one) |

Notes:

- The bot never receives its own messages, from this process or any other
  device of its account.
- `kind` is absent for ordinary chat. `system` is a notice such as "has joined
  the guild"; filter on `kind` if you only want what people typed.
- History is not reported: neither what arrives by sync nor anything sent more
  than a few minutes before the bot started. A bot does not answer week-old
  commands on restart.
- `sender` is the authenticated identity. `senderName` is self-chosen and
  decorative; never authorise anything by it.

## Commands (stdin)

One JSON object per line, with `cmd`. Every command may carry an `id` (any
JSON value); its reply and any error echo it back. Unknown fields are an error,
so typos are not silently ignored. A malformed line produces an `error` and the
bot carries on. Lines over 1 MiB are discarded.

| `cmd` | Fields | Reply |
|---|---|---|
| `send` | `channel`, `text`, `replyTo`? (a `messageId`) | `{"type":"sent","channelId","messageId"}` |
| `react` | `channel`, `message` (a `messageId`), `emoji` | `{"type":"ok","cmd":"react"}`. Toggles: reacting twice removes it. |
| `join` | `invite` | `{"type":"joined","guild":{…}}` |
| `list` | — | `{"type":"guilds","guilds":[{"id","name","dm"?,"channels":[{"id","name","type"?,"ref"}]}]}` |
| `members` | `guild` (id or name) | `{"type":"members","guildId","members":[{"fingerprint","name"}]}` |
| `name` | `text` | `{"type":"ok","cmd":"name"}`. Sets the bot's display name. |
| `quit` | — | `{"type":"ok","cmd":"quit"}`, then the bot exits. |

A `channel` is any of:

- a channel id, such as the `channelId` of a message you are answering;
- `"Guild/#channel"` (guild by name or id; the `#` is optional), as in the
  `ref` that `list` returns;
- `"#channel"`, when exactly one of the bot's guilds has a channel of that name.

Names match exactly, then ignoring case. A name that matches more than one
channel is an error, not a guess.

The bot exits cleanly on `quit`, at the end of stdin, and on SIGINT or SIGTERM.

## Example: ping, in shell

POSIX `sh` and `jq`. Answers `!ping` with `pong`, as a reply.

```sh
#!/bin/sh
# pingbot.sh [invite-code]: answer "!ping" with "pong", as a reply.
set -eu
fifo=$(mktemp -u) && mkfifo "$fifo" && trap 'rm -f "$fifo"' EXIT
concord --bot <"$fifo" | {
  if [ $# -gt 0 ]; then jq -nc --arg c "$1" '{cmd: "join", invite: $c}'; fi
  jq --unbuffered -c '
    if .type == "message" and .content == "!ping" then
      {cmd: "send", channel: .channelId, text: "pong", replyTo: .messageId}
    elif .type == "error" then ("error: " + .error + "\n" | stderr | empty)
    else empty end'
} >"$fifo"
```

## Example: dice, in Python

Answers `!roll 2d6` with the roll, as a reply.

```python
#!/usr/bin/env python3
"""dicebot.py [invite-code]: answer "!roll 2d6" with a roll, as a reply."""
import json, random, re, subprocess, sys

bot = subprocess.Popen(["concord", "--bot"], stdin=subprocess.PIPE,
                       stdout=subprocess.PIPE, text=True, bufsize=1)

def send(**cmd):
    print(json.dumps(cmd), file=bot.stdin, flush=True)

for line in bot.stdout:
    ev = json.loads(line)
    if ev["type"] == "ready":
        send(cmd="name", text="dicebot")
        if len(sys.argv) > 1:
            send(cmd="join", invite=sys.argv[1])
    elif ev["type"] == "error":
        print("error:", ev["error"], file=sys.stderr)
    elif ev["type"] == "message" and not ev.get("kind"):
        m = re.fullmatch(r"!roll ([1-9]\d?)d([2-9]|\d{2,3})", ev["content"].strip())
        if m:
            rolls = [random.randint(1, int(m[2])) for _ in range(int(m[1]))]
            send(cmd="send", channel=ev["channelId"], replyTo=ev["messageId"],
                 text=f"{ev['senderName']} rolled {sum(rolls)} {rolls}")
```

## Posting alerts

A bot that only posts needs no script at all. This turns every line appended
to a log into a message, for as long as it runs:

```sh
tail -F alerts.log |
  jq -Rc --unbuffered '{cmd: "send", channel: "Ops/#alerts", text: .}' |
  concord --bot
```

Keep it running rather than starting one per alert. A bot that starts, posts
and exits has had no time to find the guild's members, and the message waits in
mailboxes instead of arriving.
