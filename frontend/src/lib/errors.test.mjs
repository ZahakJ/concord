// The copy a user sees in a red row under their own words.
//
// `humanError` exists because errors arrive wearing the Go package that raised
// them, and the strip alone is not enough: api.js throws
// `rpc SendMessage: HTTP 500`, so the prefix came off and the TRANSPORT half
// survived verbatim — "HTTP 500", on screen, under somebody's message. The
// worklog's own comment uses that exact string as the example of what must
// never be shown.
import { readFileSync, readdirSync } from "node:fs";
import { errorField, humanError, slowModeWait, looksRaw, UNKNOWN } from "./errors.js";

let failures = 0;
const eq = (got, want, what) => {
  if (got !== want) {
    console.error(`  FAIL ${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
};

// The prefix, which already worked.
eq(humanError("app: they're already in this guild"), "they're already in this guild", "strips app:");
eq(humanError("store: net: nope"), "nope", "strips a stack of prefixes");

// A copy that fell behind and that nothing can repair refuses to send. The red
// row says the banner's sentence, not the core's lowercase one and not the
// generic failure.
const behind = "app: this copy fell behind and cannot be repaired from here; start a new guild with the same people";
eq(
  humanError(behind),
  "This copy fell behind and cannot be repaired from here. Start a new guild with the same people.",
  "a refused send on a dead copy reads as the banner",
);
eq(
  humanError(new Error(behind), "dm"),
  "This copy fell behind and cannot be repaired from here. Start a new conversation with the same people.",
  "in a conversation it says conversation",
);
eq(humanError(behind) === UNKNOWN, false, "never the generic failure");

// What was left over after the strip.
eq(humanError("rpc SendMessage: HTTP 500"), "Concord's core rejected it — try again", "HTTP 500");
eq(
  humanError("send: no route to peer: context deadline exceeded"),
  "Couldn't reach them — they're probably offline, or on a network this device can't get to from here.",
  "a send that found no route is said as a sentence",
);
eq(humanError("HTTP 404"), "Concord's core rejected it — try again", "a bare status code");
eq(
  humanError("app: you don't have permission to remove members"),
  "Your role doesn't let you remove members here any more — it may have changed while you were away.",
  "a refused moderation action explains itself instead of printing the core's words",
);
eq(humanError("rpc Foo: HTTP 503 "), "Concord's core rejected it — try again", "trailing space");
// …and what must NOT be swallowed by that rule: a status code inside a real
// sentence is somebody explaining something.
eq(
  humanError("app: the invite server answered HTTP 500 twice"),
  "the invite server answered HTTP 500 twice",
  "a status code inside a sentence survives",
);

// Slow mode gets a present-tense sentence; the NUMBER is the row's job, because
// a number captured once is a falsehood ten seconds later.
eq(humanError("app: slow mode: wait 28s"), "Slow mode in this channel", "slow mode sentence");
eq(slowModeWait("app: slow mode: wait 28s"), 28, "reads the wait back out");
eq(slowModeWait("slow mode: wait 3s"), 3, "unprefixed too");
eq(slowModeWait("app: slow mode is on"), 0, "no number, no wait");
eq(slowModeWait("HTTP 500"), 0, "not a slow-mode error");
eq(slowModeWait(new Error("app: slow mode: wait 7s")), 7, "an Error object");

// The network cases, unchanged.
eq(humanError("Failed to fetch"), "Concord isn't responding — trying to reconnect", "offline");
eq(humanError({ offline: true, message: "anything" }), "Concord isn't responding — trying to reconnect", "offline flag");

// The login card: what the core says, in a person's words, about the right
// field. The raw chain used to be printed as is ("open identity: identity:
// wrong passphrase or corrupted keystore") over two red passphrase boxes,
// whatever the mistake was.
const wrong = Object.assign(new Error("app: open identity: identity: wrong passphrase or corrupted keystore"), {
  code: "wrong-passphrase",
});
eq(humanError(wrong), "That passphrase doesn't unlock this device. Check caps lock and try again — if you're sure it's right, this device's keystore file may have been damaged or changed.", "wrong passphrase by code, which cannot be told from a damaged keystore");
eq(errorField(wrong), "pass", "wrong passphrase is about the passphrase");
const noKeystore = Object.assign(new Error("This device holds an account's data, but not the keystore that opens it. Nothing was deleted…"), {
  code: "data-without-keystore",
});
eq(humanError(noKeystore), "Nothing was deleted, and no new account was created beside it.", "data without keystore: what happened, once");
eq(humanError("link: bad code").startsWith("That isn't a link code."), true, "bad link code");
eq(errorField("link: bad code"), "code", "bad link code is about the code");
eq(errorField(new Error("link: bad code encoding")), "code", "bad code encoding");
eq(humanError("link: code expired").startsWith("That code has expired"), true, "expired code");
eq(errorField("app: link rejected: app: no active link offer"), "code", "spent code");
eq(
  errorField("that recovery phrase isn't valid — check each word and their order"),
  "phrase",
  "bad phrase is about the phrase",
);
eq(
  humanError("identity: that recovery phrase isn't valid — check the words and order").startsWith("That recovery phrase"),
  true,
  "bad phrase copy",
);
eq(errorField("they're already in this guild"), "", "unrelated error names no field");
eq(
  humanError("Unable to preload CSS for http://127.0.0.1:18200/assets/ModalSettings-abc.css"),
  "That can't open while Concord is reconnecting — try again in a moment.",
  "a lazy dialog that could not load",
);
eq(
  humanError("app: chronimport: /tmp/x holds no .json export files"),
  "That folder has no exported channel files in it. Choose the folder that holds the .json files.",
  "the importer's empty folder, without its package names",
);
eq(humanError("chronimport: not a channel export file"), "not a channel export file", "strips chronimport:");

// ---- the copy map (interface spec §9): every raw string testers saw on
// screen, and what is shown instead.
const silence = console.warn;
console.warn = () => {}; // the unmapped ones log the raw text; not this test's business
eq(
  humanError("app: open identity: identity: wrong passphrase or corrupted keystore"),
  "That passphrase doesn't unlock this device. Check caps lock and try again — if you're sure it's right, this device's keystore file may have been damaged or changed.",
  "wrong passphrase without a code",
);
eq(errorField("open identity: identity: wrong passphrase or corrupted keystore"), "pass", "…and it is about the passphrase");
eq(
  humanError("rpc Send: identity is locked"),
  "Concord restarted in the background. Enter your passphrase to carry on — nothing was lost.",
  "a locked identity after a restart never claims data was erased",
);
eq(
  humanError("app: invite rejected: you are banned from this guild"),
  "You can't join this guild. An admin has removed you from it.",
  "banned at join",
);
eq(humanError("app: bad invite code").startsWith("That isn't a complete invite code."), true, "bad invite code");
eq(humanError("app: attachment must be a data URL"), "That file is empty, so there's nothing to send.", "empty attachment");
eq(humanError("store: history: database is locked"), "Concord is busy saving — try again in a moment.", "locked database");
eq(
  humanError("app: add at least one office-hours window before publishing"),
  "Add at least one open hour before turning on your booking page.",
  "booking page with no hours",
);
eq(
  humanError("app: a booking page needs a rendezvous server (Settings → Connection)"),
  "A booking page needs a rendezvous server. Add one in Settings › Connection first.",
  "booking page without a rendezvous",
);
eq(humanError("app: two windows on the same day overlap"), "Two of your open hours on the same day overlap.", "overlapping hours");
eq(
  humanError("app: you don't have permission to ban members"),
  "Your role doesn't let you ban members here any more — it may have changed while you were away.",
  "moderation refused after a demotion",
);
// Unmapped plumbing never reaches the screen…
eq(humanError("dial tcp 10.0.0.2:4001: connect: connection refused"), UNKNOWN, "a socket error");
eq(humanError("open /home/x/.local/share/concord/db: permission denied"), UNKNOWN, "a path");
eq(humanError("unexpected EOF"), UNKNOWN, "EOF");
eq(humanError("store: sql: no rows in result set"), UNKNOWN, "a package chain");
eq(humanError("failed on /ip4/127.0.0.1/tcp/4001/p2p/12D3KooWAbc"), UNKNOWN, "a multiaddr");
eq(humanError(""), UNKNOWN, "an empty error");
// …and the core's own sentences, written for a person, still do.
eq(looksRaw("they're already in this guild"), false, "a human sentence is not raw");
eq(humanError("app: that name is taken"), "that name is taken", "a human sentence passes");
console.warn = silence;

// A toast that names what failed and then pastes the error after a colon put
// the core's words on screen untouched: flash() maps only the start of the
// string, and "Couldn't change the timer: app: …" does not start with the
// prefix. Every such site maps the error itself, so the scan covers all of
// src/: a toast template that splices in `x.message`, `x?.message`, or a bare
// caught error (`${e}`, `${err}`) fails here.
const srcRoot = new URL("../", import.meta.url);
// One line, from the flash call to its template: `${` followed straight away
// by a .message read or a bare e/err/error/ex.
const rawToast =
  /\b(?:flash|flashSlot|toastError|toastOk)\([^;\n`]*`[^`\n]*\$\{\s*(?:\w+\??\.message\b|(?:e|err|error|ex)\s*\})[^`]*`/g;
const scanned = [];
for (const rel of readdirSync(srcRoot, { recursive: true })) {
  if (!/\.(svelte|js)$/.test(rel) || /\.test\./.test(rel)) continue;
  const src = readFileSync(new URL(rel, srcRoot), "utf8");
  scanned.push(rel);
  const raw = src.match(rawToast) || [];
  eq(raw.length, 0, `${rel}: a toast prints the raw error (${raw.join("; ")})`);
}
// The scan must reach the component tree, not just lib/.
for (const f of ["GalleryViewer.svelte", "MediaGallery.svelte", "Composer.svelte", "modals/ModalNewPost.svelte"])
  eq(scanned.includes(f), true, `the raw-toast scan covers ${f}`);
// The pattern itself: each raw shape is caught, a mapped one is not.
for (const bad of [
  "flash(`Couldn't copy image: ${e?.message || e}`)",
  "flash(`Couldn't save: ${e.message}`)",
  "flash(`Couldn't open your photos: ${err}`, \"error\")",
  "flashSlot(\"x\", `Failed: ${error?.message}`)",
])
  eq((bad.match(rawToast) || []).length, 1, `raw-toast scan catches ${bad}`);
for (const ok of ["flash(`Couldn't copy image: ${humanError(e)}`)", "flash(`${e.targetName} can rejoin`)"])
  eq((ok.match(rawToast) || []).length, 0, `raw-toast scan passes ${ok}`);

if (failures) {
  console.error(`\n${failures} errors test(s) failed`);
  process.exit(1);
}
console.log("errors.test.mjs: all checks passed");
