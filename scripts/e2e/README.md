# End-to-end suite

Real peers, real UI, real network, on one machine and nowhere else. Each test
gets fresh peer processes with their own data directories, all bootstrapping
from a private rendezvous on `127.0.0.1`, and drives their actual web UI with
headless Chromium. mDNS is off, so the suite never sees, or is seen by,
anything on the LAN, and no peer ever dials public infrastructure.

It is not part of the pull-request gate. Run it when you change behaviour that
only exists in the running app, and add a spec for every such bug you fix.

## Running

```sh
make e2e                                  # build the frontend and binary, run every spec
make e2e E2E_ARGS="guild-join"            # specs whose file name contains "guild-join"
node scripts/e2e/run.mjs                  # same, without rebuilding frontend/dist
node scripts/e2e/run.mjs --grep "edit"    # tests whose "file › name" matches a regex
node scripts/e2e/run.mjs path/to/x.spec.mjs --keep
node scripts/e2e/run.mjs --list           # print the selected tests and exit
```

The runner builds `concord-web` and `rendezvous` from the working tree into
the run directory (seconds, with a warm Go cache). The web binary embeds
`frontend/dist` as it is on disk: `make e2e` rebuilds the frontend first;
`run.mjs` alone does not, so after a frontend change either use `make e2e` or
run `npm --prefix frontend run build` yourself.

| Variable | Default | Meaning |
|---|---|---|
| `E2E_BIN` | built | Use this `concord-web` binary instead of building one |
| `E2E_RENDEZVOUS_BIN` | built | Same for the rendezvous |
| `E2E_BASE_PORT` | `47300` | Rendezvous on this port and the next; peers on base+10 to base+14 |
| `CHROMIUM` | `/usr/bin/chromium` | Browser executable. When it does not exist, the browser playwright-core downloaded is used |
| `PLAYWRIGHT_CORE` | | Where to find `playwright-core` (see below) |
| `E2E_TIMEOUT_SCALE` | `1` | Multiplies every timeout, for slow or loaded machines |
| `E2E_KEEP_RUN` | | Keep the run directory even when everything passed |

`playwright-core` is deliberately not a project dependency. Install it anywhere
(`npm i playwright-core` in a scratch directory) and point `PLAYWRIGHT_CORE` at
the package, or run from a directory whose `node_modules/` has it;
`scripts/playwright.mjs` explains the lookup.

Several copies can run at once as long as each has its own `E2E_BASE_PORT`,
at least 20 apart. The runner refuses to start on a port that is taken rather
than attaching to someone else's peer.

### What a run leaves behind

Everything goes under `.dev/e2e/<run-id>/` (gitignored): the binaries, the
rendezvous log, one directory per test with each peer's data and log. A fully
green run deletes it. A failing test adds
`artifacts/<test>/` with `error.txt`, a screenshot and the browser console of
every page that was open, and a copy of every peer's log; the run directory is
kept. The summary at the end lists every test with PASS or FAIL and its time,
and the exit code is non-zero when anything failed.

`--keep` stops at the first failure (or after the last test) with that test's
peers still running and prints their URLs, passphrases and data directories,
so you can open them yourself. Ctrl-C stops them. Only processes the runner
started are ever signalled.

## Writing a spec

A spec is `scripts/e2e/specs/<area>-<what>.spec.mjs`. Name the area first
(`guild-`, `messages-`, `sync-`, `settings-`, `voice-`, `phone-`, …) so
`make e2e E2E_ARGS=messages` runs a whole area. Register one or more tests:

```js
// What this proves, in one or two sentences — the bug it guards against.
import { test } from "../harness.mjs";

test("a reply quotes the message it answers", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Replies", members: [alice, bashir] });
  const general = g.allChannels[0];

  const a = await alice.open();
  const b = await bashir.open();
  await a.openChannel(general.name, "Replies");
  await b.openChannel(general.name, "Replies");

  step("Alice asks, Bashir replies from the message toolbar");
  await a.send("lunch at noon?");
  await b.waitForMessage("lunch at noon?");
  await b.messageAction("lunch at noon?", "Reply");
  await b.send("yes, see you there");

  const reply = await alice.waitForMessage(general.id, "yes, see you there");
  const asked = await alice.waitForMessage(general.id, "lunch at noon?");
  assert.equal(reply.replyTo, asked.id);
});
```

A file may instead `export default async function (t) { … }`, which becomes one
test named after the file. Tests run one after another, each with its own
peers; nothing carries over between tests.

Keep specs short: if two specs need the same few lines of driving, that belongs
in `harness.mjs`. Assertions use `node:assert/strict`, passed in as `assert`.

### The test context

| | |
|---|---|
| `world` | The test's peers and guilds (below) |
| `assert` | `node:assert/strict` |
| `waitFor(fn, {timeout, interval, message})` | Polls `fn` until it returns something truthy and returns it. Errors thrown by `fn` count as "not yet"; the last one is quoted in the timeout error |
| `seedMessages(peer, channelId, n, {prefix})` | Sends `n` numbered messages over RPC, waits until the sender has stored them, returns their texts |
| `step(text)` | A progress line in the output |
| `log(...)` | Any other output |
| `shot(app, label)` | A screenshot under the test's artifacts directory (kept on green runs only with `--keep` or `E2E_KEEP_RUN=1`) |
| `sleep(ms)` | For pacing an action, never as an assertion |

A test fails on the first thrown error, and after five minutes
(`test(name, fn, { timeout })` changes that).

### `world`

- `world.peers(n, {names, start})` → `n` peers, started and unlocked (their
  identities created, display names set). Default names are Alice, Bashir,
  Chen, Dana, Emre; at most five peers per test. `start: false` returns them
  unstarted.
- `world.peer({name})` → one more peer.
- `world.guild({name, members, channels})` → creates the guild on
  `members[0]`, creates any extra text channels (`"name"` or
  `{name, type}`), joins the rest by invite code over RPC, and waits until
  every member sees every other member and every channel. Returns
  `{id, name, code, channels: {name: id}, allChannels, channel(name)}`.
  Use it whenever joining is not what the test is about.
- `world.settleGuild({id, name}, members, channelNames)` → the same wait, for
  a guild you created or joined some other way (the UI, say).

### A peer

- `name`, `index`, `port`, `url`, `token`, `passphrase`, `home`, `log`,
  `fingerprint`, `peerId`
- `rpc(method, ...args)` → the result, or throws with the core's error. The
  methods are the `Dispatch` switch in `internal/bridge/bridge.go`.
- `messages(channelId)`, `send(channelId, text, replyTo?)` (over RPC; resolves
  with the stored message), `waitForMessage(channelId, textOrPredicate,
  {timeout, deleted})`, `guilds()`
- `stop()` → the process exits, data kept: the person closed the app.
  `start()` brings it back locked; `unlock()` unlocks it. `restart()` is all
  three. Pages open on a stopped peer are closed.
- `events()` → a tap on the peer's server-sent events:
  `{list, waitFor(name, pred?, opts), close()}`. Each entry is
  `{name, data, at}`. Closed at the end of the test.
- `open({viewport, mobile, reducedMotion, colorScheme, unlock})` → an `App`
  (below): a new browser context showing this peer's UI, already past the
  passphrase screen. `viewport` is a name from `VIEWPORTS` (`desktop`,
  `laptop`, `tablet`, `phone`, `phoneSmall`, `phoneLandscape`) or
  `{width, height}`; `phone` and `phoneSmall` turn on touch and the mobile
  user agent. `unlock: false` leaves it at the passphrase screen.

### An open page (`App`)

- `page`, `ctx`: the Playwright objects, for anything not covered here.
- `errors`: console errors and uncaught exceptions so far; `console`: every
  console line.
- `composer()`, `message(text)` (the newest visible message body containing
  `text`), `messageRow(text)` (its `.msg` row), `dialog()` (the topmost
  `[role=dialog]`) — Playwright locators.
- `send(text)` types into the composer and presses Enter, then waits until the
  message is on screen. `waitForMessage(text)`, `waitForNoMessage(text)`.
- `messageAction(text, label)` hovers the message and presses its toolbar
  button by accessible name (`Reply`, `Edit`, `Delete`, `Pin`, `Forward`,
  `More`, `React 👍`, …). `editMessage(text, next)`, `deleteMessage(text)`
  (confirms the dialog).
- `openGuild(name)`, `openChannel(name, guild?)` (waits until the composer
  targets it), `openSettings()` → the settings dialog, `closeModals()`.
- `createGuild(name)` and `joinGuild(code, name?)` go through the
  "Add a guild" menu exactly as a person would.
- `unlock()` / `reload()` reload the page and get past the passphrase screen
  again; `screenshot(file)`; `close()`.

### Finding things on the page

Prefer roles and accessible names (`getByRole("button", { name: "Send" })`),
labels and placeholders: they are what a person, and a screen reader, find the
control by, so a spec written against them also checks that the control is
labelled. The stable handles the helpers rely on are: `nav[aria-label="Guilds"]`
(the guild rail; `Sections` on a phone), `button[aria-label="Add a guild"]`,
`textarea.draft` (the composer, whose `aria-label` is its placeholder, e.g.
`Message #general…`), `.channel` rows, `.msg[data-msg-id]` rows with a `.body`,
the `Message actions` toolbar, `button[aria-label="Settings"]`. If a control
has no accessible name, giving it one is a fix in its own right; do not add
test-only attributes.

## Pitfalls

- **The passphrase screen appears on every page load**, including a reload of
  a peer that is already unlocked. `peer.open()` and `app.reload()` handle it;
  a bare `page.goto` or `page.reload` leaves you at the lock screen.
- **A `.msg` row's bounding box is a degenerate sliver.** Its children are laid
  out outside it, so `isVisible()`, `boundingBox()` and clicks on the row itself
  misbehave. Use `message(text)` (the body) for visibility and the row only for
  its `data-msg-id` and as a scope; for geometry, use vertical positions only.
- **The page never goes network-idle.** It holds a server-sent event stream
  open for its whole life, so `waitForLoadState("networkidle")` never resolves.
  Wait for the thing you care about instead. For the same reason
  `chromium --headless --screenshot` hangs.
- **No fixed sleeps as assertions.** Under load, delivery that takes 300 ms on
  an idle machine can take ten seconds. Wait for a condition with `waitFor`, a
  peer's `waitForMessage`, or a Playwright locator wait; leave the default
  timeouts generous and raise `E2E_TIMEOUT_SCALE` on a slow machine rather
  than shortening a wait.
- **Join right after start can fail** with the owner unreachable, while the
  peers are still finding each other through the rendezvous. `world.guild`
  retries the join for that reason; a UI join (`joinGuild`) does not, because
  what the dialog shows then is the behaviour under test.
- **zsh and `${}`.** Inline `node -e "…${x}…"` gets mangled by zsh before node
  sees it. Write the script to a file.
- **Settings are per browser context**, stored in the page's `localStorage`.
  Two `open()`s of the same peer do not share appearance or notification
  settings; the theme, in particular, is an in-app setting and not
  `prefers-color-scheme`.
