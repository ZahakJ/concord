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
// The confirm step's refusals, in the issuer's words (internal/app/link.go).
eq(humanError("app: code expired").startsWith("That code has expired"), true, "expired by the issuer's clock");
eq(humanError("app: that code was already used by another device").startsWith("That code was already used by another device."), true, "lost the race");
eq(errorField("app: that code was already used by another device"), "code", "lost the race is about the code");
eq(humanError("app: the other device declined this link").startsWith("Your other device said no."), true, "declined");
eq(humanError("app: nobody confirmed this link on the other device in time").startsWith("Nobody confirmed"), true, "unanswered");
eq(
  humanError("net: the other device runs an older Concord, which links without asking you to confirm; update Concord on it, then make a new code").startsWith("Your other device runs an older Concord"),
  true,
  "old issuer",
);
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
// src/. It reads each flash, flashSlot, toastError and toastOk call to its
// closing paren, however many lines that takes, and fails when the call puts
// text in front of a raw error: a template whose `${}` starts with one, or a
// `+` next to one. It also follows a name that was given such a template and
// is then flashed. A raw error is a `.message` read, `String(…)` of a caught
// error, or a caught error by name (e, err, error, ex, reason, cause, or any
// name ending in Err or Error), with or without a `||` or `??` fallback. The
// whole message on its own, flash(err) or flash(e.message), is fine: flash()
// maps it.
const srcRoot = new URL("../", import.meta.url);
const errName = String.raw`(?:e|err|error|ex|reason|cause|\w*(?:Err|Error))`;
// One operand that is a raw error. A caught error by name must not go on to
// read a property (e.targetName is not an error) or be called.
const rawOperand = String.raw`(?:String\(\s*(?:\w+\??\.message|${errName})\s*\)|\w+\??\.message\b|\b${errName}\b(?!\s*(?:\??\.|\[|\()))`;
const rawInterp = new RegExp(String.raw`^\s*${rawOperand}\s*(?:(?:\|\||\?\?)[\s\S]*)?$`);
const rawConcat = new RegExp(String.raw`\+\s*${rawOperand}|${rawOperand}\s*\+`);

// Skips a quoted string or a template starting at i; returns the index after
// it. A template's `${}` parts are collected into exprs, its text into texts.
function skipString(src, i, exprs, texts) {
  const q = src[i++];
  let text = "";
  while (i < src.length && src[i] !== q) {
    if (src[i] === "\\") {
      i += 2;
      continue;
    }
    if (q === "`" && src[i] === "$" && src[i + 1] === "{") {
      const end = skipCode(src, i + 2, "}");
      exprs?.push(src.slice(i + 2, end));
      texts?.push(text);
      text = "";
      i = end + 1;
      continue;
    }
    text += src[i++];
  }
  texts?.push(text);
  return i + 1;
}
// Walks code from i to the closer at depth 0 (")" for call arguments, "}" for
// a `${}`), or, with closer null, to the end of an assignment's right side.
// Returns the index of the closer.
function skipCode(src, i, closer) {
  let depth = 0;
  let seen = false;
  for (; i < src.length; i++) {
    const c = src[i];
    if (c === '"' || c === "'" || c === "`") {
      i = skipString(src, i) - 1;
      seen = true;
      continue;
    }
    if (c === "/" && src[i + 1] === "/") {
      while (i < src.length && src[i] !== "\n") i++;
      continue;
    }
    if ("([{".includes(c)) depth++;
    else if (")]}".includes(c)) {
      if (depth === 0) return i;
      depth--;
    } else if (closer === null && depth === 0 && (c === ";" || c === "," || (c === "\n" && seen))) return i;
    if (!/\s/.test(c)) seen = true;
  }
  return i;
}
// Code outside every string and template, the `${}` parts of each template,
// and whether text comes before a `${}` in the same template.
function parts(expr) {
  let code = "";
  const interps = [];
  for (let i = 0; i < expr.length; ) {
    const c = expr[i];
    if (c === '"' || c === "'" || c === "`") {
      const exprs = [];
      const texts = [];
      i = skipString(expr, i, exprs, texts);
      code += " _ ";
      interps.push(...exprs);
      continue;
    }
    code += c;
    i++;
  }
  return { code, interps };
}
// The reasons expr puts a raw error on screen after other text.
function rawIn(expr) {
  const { code, interps } = parts(expr);
  const found = interps.filter((x) => rawInterp.test(x)).map((x) => "${" + x.trim() + "}");
  if (rawConcat.test(code)) found.push(code.match(rawConcat)[0].trim());
  return found;
}
const callRe = /\b(flash|flashSlot|toastError|toastOk)\s*\(/g;
function flashCalls(src) {
  const calls = [];
  for (const m of src.matchAll(callRe)) {
    const start = m.index + m[0].length;
    calls.push({ fn: m[1], args: src.slice(start, skipCode(src, start, ")")) });
  }
  return calls;
}
// Every call in src that shows a raw error, as readable strings.
function rawToasts(src) {
  // Names given a raw template or concatenation anywhere in the file.
  const tainted = new Set();
  for (const m of src.matchAll(/\b(\w+)\s*=(?![=>])/g)) {
    const start = m.index + m[0].length;
    if (rawIn(src.slice(start, skipCode(src, start, null))).length) tainted.add(m[1]);
  }
  const hits = [];
  for (const { fn, args } of flashCalls(src)) {
    const why = rawIn(args);
    for (const a of args.split(",").map((s) => s.trim())) if (tainted.has(a)) why.push(`${a}, built with a raw error`);
    if (why.length) hits.push(`${fn}(${args.replace(/\s+/g, " ").trim()}): ${why.join(", ")}`);
  }
  return hits;
}
const scanned = [];
const sources = new Map();
for (const rel of readdirSync(srcRoot, { recursive: true })) {
  if (!/\.(svelte|js)$/.test(rel) || /\.test\./.test(rel)) continue;
  const src = readFileSync(new URL(rel, srcRoot), "utf8");
  scanned.push(rel);
  sources.set(rel, src);
  const raw = rawToasts(src);
  eq(raw.length, 0, `${rel}: a toast prints the raw error (${raw.join("; ")})`);
}
// The scan must reach the component tree, not just lib/, and must read calls.
for (const f of ["GalleryViewer.svelte", "MediaGallery.svelte", "Composer.svelte", "modals/ModalNewPost.svelte"])
  eq(scanned.includes(f), true, `the raw-toast scan covers ${f}`);
eq(
  [...sources.values()].reduce((n, s) => n + flashCalls(s).length, 0) > 300,
  true,
  "the raw-toast scan reads the app's toast calls",
);
// The scan itself: each raw shape is caught, each mapped one is not.
for (const bad of [
  "flash(`Couldn't copy image: ${e?.message || e}`)",
  "flash(`Couldn't save: ${e.message}`)",
  "flash(`Couldn't save: ${e.message}`, \"error\")",
  "flash(`Couldn't open your photos: ${err}`, \"error\")",
  "flashSlot(\"x\", `Failed: ${error?.message}`)",
  "flash(\n  `Couldn't add the emoji: ${e.message}`,\n  \"error\",\n)",
  "flash(`Couldn't send: ${String(e)}`)",
  "flash(`Couldn't send: ${String(e.message)}`)",
  "flash(`Couldn't send: ${err || \"unknown\"}`)",
  "flash(`Couldn't send: ${err ?? \"unknown\"}`)",
  "flash(\"Couldn't send: \" + e.message)",
  "flash(\"Couldn't send: \" + err, \"error\")",
  "toastError(e.message + \" (try again)\")",
  "flash(`Couldn't join: ${reason}`)",
  "flash(`Couldn't upload: ${uploadErr}`)",
  "const msg = `Couldn't send: ${e.message}`;\nflash(msg, \"error\");",
  "let msg\nmsg = \"Couldn't send: \" + err\nflash(msg)",
])
  eq(rawToasts(bad).length, 1, `raw-toast scan catches ${JSON.stringify(bad)}`);
for (const ok of [
  "flash(`Couldn't copy image: ${humanError(e)}`)",
  "flash(\n  `Couldn't copy image: ${humanError(e)}`,\n  \"error\",\n)",
  "flash(`${e.targetName} can rejoin`)",
  "flash(\"Couldn't send: \" + humanError(e))",
  "flash(err)",
  "flash(e.message, \"error\")",
  "flash(`${count} new`)",
  "flash(`Saved ${name || \"it\"}`)",
  "const msg = `Couldn't send: ${humanError(e)}`;\nflash(msg);",
  "const msg = `Couldn't send: ${e.message}`;\nconsole.warn(msg);\nflash(\"Couldn't send\");",
])
  eq(rawToasts(ok).length, 0, `raw-toast scan passes ${JSON.stringify(ok)}`);

// A failed copy or save is a failure: kind "error" plays the nope sound and
// styles the toast red. Left at the default "info" it was silent and looked
// like a notice. The call is read to its closing paren, so a call wrapped onto
// several lines is judged on its last argument, not its first line. Each
// prefix maps to the least number of sites it must find.
const failToasts = { "Couldn't copy image:": 4, "Couldn't copy the link": 1, "Couldn't save:": 1 };
const lastArgIsError = (args) => /,\s*"error"\s*,?\s*$/.test(args);
const failFound = Object.fromEntries(Object.keys(failToasts).map((p) => [p, 0]));
for (const [rel, src] of sources) {
  for (const { fn, args } of flashCalls(src)) {
    const p = Object.keys(failToasts).find((p) => args.trimStart().slice(1).startsWith(p));
    if (fn !== "flash" || !p) continue;
    failFound[p]++;
    eq(lastArgIsError(args), true, `${rel}: a failure toast is an error toast (flash(${args.replace(/\s+/g, " ").trim()}))`);
  }
}
for (const [p, want] of Object.entries(failToasts))
  eq(failFound[p] >= want, true, `the failure-toast scan found ${want} "${p}" site(s) (found ${failFound[p]})`);
eq(lastArgIsError("`Couldn't copy image: ${humanError(e)}`,\n  \"error\",\n"), true, "a wrapped error toast passes");
eq(lastArgIsError("`Couldn't copy image: ${humanError(e)}`"), false, "a toast left at info fails");

if (failures) {
  console.error(`\n${failures} errors test(s) failed`);
  process.exit(1);
}
console.log("errors.test.mjs: all checks passed");
