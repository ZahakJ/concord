# concord-bridge — the local app-bus

A headless Concord client that lets your **other local apps** (trove,
sentinel, anything you run) send and read messages in your Concord guilds —
without embedding a Concord node, and without weakening anything.

```
 your app ──loopback HTTP──▶ concord-bridge ══E2EE P2P══▶ the guild
 (trove, sentinel, …)        (its own identity)
```

The bridge is a **first-class Concord peer**: its own keypair, invited into a
guild like any member, every message end-to-end encrypted by the normal MLS
path. The HTTP side never leaves the machine: it binds loopback only and
requires a bearer token read from a mode-0600 file.

## Install & run

```sh
go build -o ~/bin/concord-bridge ./cmd/concord-bridge   # from the concord repo
concord-bridge serve                                    # first run creates the identity
```

State lives in `~/.config/concord-bridge/` (override: `$CONCORD_BRIDGE_HOME`):
the standard Concord keystore + encrypted DB, plus

* `pass`  — auto-generated keystore passphrase (0600)
* `token` — the API bearer token (0600); local clients read it from here

Listen address: `$CONCORD_BRIDGE_ADDR` (default `127.0.0.1:8790`;
non-loopback refuses to start). Internet rendezvous: set `$CONCORD_BOOTSTRAP`
to the same multiaddrs your main Concord uses.

## Inviting the bridge into a guild

On the machine's main Concord (as the guild owner or a member with invite
permission): create an invite code, then:

```sh
concord-bridge name "zman's bridge"     # a human-readable roster name
concord-bridge join <invite-code>
concord-bridge id                       # fingerprint, for out-of-band verification
```

**On ZahakJ's machine** (both ends need a bridge for app↔app exchange, e.g.
the sentinel leaderboard): build `concord-bridge` from this repo, run
`concord-bridge serve` once to mint the identity, send it an invite to the
shared guild from his Concord (`join <code>`), and point his sentinel at
`http://127.0.0.1:8790` with the token from
`~/.config/concord-bridge/token`. That's the whole install.

## The API contract (stable — v1)

All requests need `Authorization: Bearer <token>`.

### `GET /api/health`

```json
{ "ok": true,
  "identity": { "fingerprint": "ABCD EFGH …", "name": "zman's bridge" },
  "guilds": [ { "id": "…", "name": "Grey mane",
                "channels": [ { "id": "…", "name": "general" } ] } ] }
```

### `POST /api/send`

```json
{ "channel": "general", "text": "hello from sentinel" }
```

`channel` is a channel **id**, a bare **name** (`"general"`), or
`"guild/name"` (`"Grey mane/general"`) if a bare name is ambiguous (an
ambiguous name is a 400 listing the candidates). Response:

```json
{ "ok": true, "message_id": "…" }
```

Sends are rate-limited to **1 msg/s, burst 5, per channel** (HTTP 429 above
that) — these are human-visible channels; the bridge never floods one.

### `GET /api/messages?channel=<spec>&since=<cursor>&limit=<n≤500>`

Decrypted messages the bridge identity can see, **chronological**, strictly
newer than `since` (`""`/absent = from the start):

```json
{ "messages": [ { "id": "…", "sender": "PY7Y 3GYS …",
                  "sender_display": "Brahma",
                  "ts": "2026-07-17T22:41:03.128702Z",
                  "text": "APPBUS:sentinel:1\n{\"leaderboard\": …}" } ],
  "next_cursor": "1784328063128702000" }
```

Poll with `since=<next_cursor>` from the previous response and you will never
miss or re-read a message. Cursors are opaque, strictly-ordered strings.

## App-to-app payload convention

Machine payloads keep the same shape:

```
APPBUS:<app>:<schema-version>
<JSON body>
```

e.g. first line `APPBUS:sentinel:1`, JSON after the newline. Apps ignore
`APPBUS:` messages that aren't addressed to them.

### Two planes

Payloads used to ride as ordinary chat messages, on the theory that the humans
in the channel might like to watch their machines talk. In practice sentinel
filled `#general` with lines nobody wanted to read. Machine data is not
conversation, and rendering it as conversation degrades the conversation.

So app traffic now rides a separate **data plane**, distinguished by a message
`kind`:

| kind | meaning | rendered in the channel? |
|---|---|---|
| `chat` (default) | a message a person typed | yes |
| `app` | a machine payload | **no** — apps/integrations view only |

Everything else about the two planes is identical: same bridge identity, same
MLS group, same end-to-end encryption, same store, same cursor feed. Only the
rendering contract differs. App-kind messages never mark a channel unread and
never notify anyone.

**Sending.** `POST /api/send` takes an optional `"kind"`:

```json
{"channel": "grey mane/general", "text": "APPBUS:sentinel:1\n{\"cpu\":91}", "kind": "app"}
```

The response echoes the plane it used: `{"ok": true, "message_id": "...", "kind": "app"}`.

**Reading.** `GET /api/messages` reports `"kind"` on every message and accepts
an optional `?kind=app` or `?kind=chat` filter. With no filter it returns both
planes — byte-for-byte what it returned before the split, so existing pollers
need no change. The cursor advances past filtered-out messages, so a
`?kind=app` poller always makes forward progress.

### Detecting support before you send

Unknown fields degrade *silently* on this API. An older bridge handed
`?kind=app` on a read, or `"kind":"app"` in a send body, ignores it and returns
`200`. On a read that is harmless. **On a send it is not** — the payload lands
in the human channel as ordinary chat, and the response looks exactly like
success.

So don't infer support from a send; detect it first. `GET /api/health`
advertises it:

```json
{"ok": true, "identity": {...}, "guilds": [...],
 "capabilities": ["data_plane"], "data_plane": true}
```

A producer should check for `data_plane` and **refuse to push machine payloads
into a human channel** through a bridge that lacks it. An older bridge has
neither field.

### Back-compatibility

You do not have to upgrade. A message whose text begins `APPBUS:` is treated as
app-kind **even with no `kind` field at all** — on send (the bridge moves it to
the data plane for you) and on read (it reports as `"kind": "app"`). App-bus
producers live in other repos, on other machines, on their own release
cadences; requiring a lockstep upgrade would mean either breaking them or
leaving their traffic in the human channel until the last one shipped.

This also applies retroactively: payloads already sitting in a channel from
before the split stop rendering as chat, without any migration.

Setting `"kind": "app"` explicitly is still preferred — it works for payloads
that don't carry the prefix, and it states the intent rather than inferring it.

### Rate limits

The planes have independent per-channel budgets, so a chatty machine can never
consume the budget a human message needs:

| plane | rate | burst |
|---|---|---|
| `chat` | 1/s | 5 |
| `app` | 20/s | 60 |

The chat limit is about human attention — a message that renders in a
conversation should never arrive faster than a person would send one. The app
plane costs nobody's attention, so it gets a looser budget, still bounded so a
wedged producer cannot fill the store or saturate the gossip topic.

## Security posture

* The bridge identity is an ordinary Concord member: it sees exactly the
  guilds/channels it was invited to, nothing else, and everything it sends or
  receives is E2EE'd peer-to-peer.
* Locally: loopback bind (enforced), constant-time bearer-token check, token
  and passphrase in 0600 files.
* Verify the bridge like any peer: compare `concord-bridge id` with the
  fingerprint shown in the guild roster.
