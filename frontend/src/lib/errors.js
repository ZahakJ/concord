// errors.js — turning what the core said into what a person reads.
//
// Pure, and its own module for the reason lib/outbox.js is: this is the copy a
// user actually sees in a red row under their own words, and it needs a test
// that does not need a browser.
// Errors arrive here still wearing the Go package that raised them — "app:",
// "net:", "store:", or an "rpc Foo:" from the HTTP transport. That prefix is
// for a log, not for a person, and it reached users on 119 call sites: three
// screens stripped it inline and the fourth forgot, which is how you end up
// with "app: they're already in this guild" in a toast. Strip it once, here.
//
// Only the leading package token: NOT everything up to the last colon, which
// would throw away a helpful multi-clause message and leave just the innermost
// transport error.
import { cannotRepairText } from "./syncbanner.js";

const GO_PREFIX = /^(?:(?:app|net|store|mls|bridge|identity|link|chronimport|rpc\s+\w+):\s*)+/;
// The browser's own words for "nothing answered". They are true and useless.
const RAW_NETWORK = /^(failed to fetch|networkerror when attempting to fetch resource\.?|load failed|the network connection was lost\.?)$/i;
// The transport's words for the same thing, and there are a lot of them: a dial
// failure arrives as the peer id followed by one clause per address it tried,
// with the OS reason for each — six hundred characters of multiaddrs and
// "connection refused". It is the FIRST thing a brand-new user sees when a
// friend's invite code points at a peer who has since closed the app, printed
// in a red box under "That code didn't open the door". Every word of it is true
// and none of it is for a person; the one fact it carries is that nobody
// answered, which fits in a sentence.
const RAW_DIAL = /failed to dial|all dials failed|no good addresses|dial backoff|no route to peer|context deadline exceeded/i;

// What the core says when a channel's slow mode refuses a send. Exported so the
// composer can read the number back out and take ownership of the wait — the
// error is the contract, the countdown is the interface.
export const SLOW_MODE_ERR = /^slow mode: wait (\d+)s$/;
export const slowModeWait = (msg) => {
  const m = String(msg?.message ?? msg ?? "")
    .replace(GO_PREFIX, "")
    .match(SLOW_MODE_ERR);
  return m ? Number(m[1]) : 0;
};

// Stripping the Go package is not the whole job. `api.js` throws
// `rpc SendMessage: HTTP 500`, so the prefix comes off and the TRANSPORT half
// survives verbatim — "HTTP 500", in a red row under the user's own words. The
// rule the worklog already wrote down is that a message must be about the
// reader's message, not about our transport; a bare status code is neither a
// sentence nor something anybody can act on.
const BARE_HTTP = /^HTTP \d{3}$/;

const WRONG_PASSPHRASE =
  "That passphrase doesn't unlock this device. Check caps lock and try again — if you're sure it's right, this device's keystore file may have been damaged or changed.";

// Outcomes the core names with a code (internal/bridge CodedError), and the
// sentence each one is read as. A code is a promise about what happened; the
// sentence that came with it is for a log.
const BY_CODE = {
  // The core cannot tell a wrong passphrase from a keystore file that was
  // damaged or altered (its key settings changed): both fail the same way.
  // The sentence says both, so somebody sure of their passphrase is not
  // left assuming they misremembered it before choosing the recovery path.
  "wrong-passphrase": WRONG_PASSPHRASE,
  // Only the lock screen gets this one, and it already says above the error
  // what the device holds and what each choice does; the core's sentence
  // repeats that, so the error keeps just what happened.
  "data-without-keystore": "Nothing was deleted, and no new account was created beside it.",
};

// Core sentences that reach the login card, in the words they should have had.
// Each is about the field it names (see errorField): the old card painted the
// passphrase boxes red for every one of them, a wrong link code included.
const KNOWN = [
  // A video or HEIC photo the core could not clean (lib/mediastrip.js). The
  // message box offers to send it as it is; a dialog that sends files has
  // no such button, so it says where to go.
  [/details can't be removed from this (video|photo|file)/i, "",
    "Concord couldn't remove the details stored in that file, so it wasn't sent. Attach it in the message box to choose whether it goes as it is, including any location stored in it."],
  [/video has subtitles or other text tracks/i, "",
    "That video has subtitles or other text tracks, which can hold location, so it wasn't sent. Attach it in the message box to choose whether they are removed or it goes as it is."],
  [/^bad code( encoding)?$|^incomplete code$/i, "code",
    "That isn't a link code. Copy it again from Settings → Your devices on your other device."],
  [/^code expired$/i, "code",
    "That code has expired — they last two minutes. Make a new one on your other device."],
  // The device showing the code turned this one away (internal/app/link.go,
  // the linkErr* reasons), or is too old to ask its owner first.
  [/already used by another device/i, "code",
    "That code was already used by another device. If that wasn't one of yours, choose No on your other device, then make a new code there."],
  [/declined this link/i, "",
    "Your other device said no. If that was a mistake, make a new code there and try again."],
  [/nobody confirmed this link/i, "",
    "Nobody confirmed on your other device in time. Make a new code there and try again."],
  [/runs an older Concord, which links without asking/i, "",
    "Your other device runs an older Concord, which would link without asking you first. Update Concord on it, then make a new code there."],
  [/did not come from the device it asks to add/i, "",
    "Your other device refused this request. Make a new code there and try again."],
  [/no active link offer|link rejected/i, "code",
    "That code was already used or closed on your other device. Make a new one there and paste it here."],
  // A dialog's code is fetched the first time it opens. With the core down
  // that fetch fails, and the browser's words for it ("Unable to preload CSS
  // for http://127.0.0.1:…/assets/ModalSettings-….css") reached a toast.
  [/unable to preload css|failed to fetch dynamically imported module|importing a module script failed|error loading dynamically imported module/i, "",
    "That can't open while Concord is reconnecting — try again in a moment."],
  [/recovery phrase isn'?t valid|valid 24-word recovery phrase/i, "phrase",
    "That recovery phrase isn't valid. Check each word and their order — it is 24 words."],
  // The login card, by the core's words when no code came with them.
  [/wrong passphrase or corrupted keystore/i, "pass", WRONG_PASSPHRASE],
  // A node restarted under an open page: the identity is merely locked again.
  // It used to be answered with a sentence about erased data, which was false.
  [/identity is locked/i, "",
    "Concord restarted in the background. Enter your passphrase to carry on — nothing was lost."],
  // Joining.
  [/invite rejected: you are banned/i, "",
    "You can't join this guild. An admin has removed you from it."],
  // The committer turned the join down without saying why. On a current
  // committer this is rare; on an older one it is what a device linked again
  // gets while its earlier copy is still in the guild.
  [/invite rejected: invite failed/i, "",
    "The guild's admin device couldn't add this device. If you linked this device again, that device may still hold its earlier copy and need a newer Concord to replace it."],
  // The answer to an invite named an owner its own group does not support, or
  // came with a welcome for another group. The core joined nothing.
  [/invite's copy of the guild does not match the guild/i, "",
    "This invite doesn't match the guild it names, so Concord didn't join. Nothing was saved. Ask the guild's owner, or another member you trust, for a new invite."],
  [/^bad invite code|invalid invite code/i, "",
    "That isn't a complete invite code. Paste the whole code — codes are long and easy to clip."],
  // Sending.
  [/attachment must be a data URL/i, "",
    "That file is empty, so there's nothing to send."],
  [/database is locked/i, "",
    "Concord is busy saving — try again in a moment."],
  // SQLite's words for a full disk (SQLITE_FULL), and for one failing.
  [/database or disk is full|SQLITE_FULL/i, "",
    "This device's disk is full, so Concord couldn't save. Free some space, then try again."],
  [/disk I\/O error|SQLITE_IOERR/i, "",
    "Concord couldn't write to this device's disk. If it's nearly full, free some space; then try again."],
  // Bookings.
  [/add at least one office-hours window/i, "",
    "Add at least one open hour before turning on your booking page."],
  [/a booking page needs a rendezvous server/i, "",
    "A booking page needs a rendezvous server. Add one in Settings › Connection first."],
  [/a window must start before it ends/i, "",
    "An open hour has to start before it ends, on the same day."],
  [/a window must fit at least one slot/i, "",
    "That open hour is shorter than one slot. Make it longer or the slots shorter."],
  [/two windows on the same day overlap/i, "",
    "Two of your open hours on the same day overlap."],
  // The chat-archive importer, about the folder it was pointed at.
  [/holds no \.json export files/i, "",
    "That folder has no exported channel files in it. Choose the folder that holds the .json files."],
  [/is not a directory this device can read/i, "",
    "Concord can't open that folder on this machine. Check the path."],
  [/already carries an imported archive/i, "",
    "This guild already carries an imported archive, and importing replaces it. Start the import again to confirm."],
];

// errorField says which field an error is about, so only that field turns red
// and is announced as invalid: "pass", "phrase" or "code", or "" when it is
// about none of them.
export function errorField(msg) {
  if (msg?.code === "wrong-passphrase") return "pass";
  const text = String(msg?.message ?? msg ?? "").replace(GO_PREFIX, "").trim();
  for (const [re, field] of KNOWN) if (re.test(text)) return field;
  return "";
}

// What the core says when it refuses a send, a governance act or "Continue
// from here" on a copy that fell behind and that nothing can repair
// (internal/app ErrBehindOnlyCommitter). It is read as the banner's own
// sentence, so the red row under a message and the banner say the same thing.
const CANNOT_REPAIR = /fell behind and cannot be repaired from here/i;

// kind is the kind of the guild the action was in ("dm" for a conversation),
// where the caller knows it; it only changes the noun of that one sentence.
export function humanError(msg, kind = "") {
  if (msg?.code && BY_CODE[msg.code]) return BY_CODE[msg.code];
  const text = String(msg?.message ?? msg ?? "").replace(GO_PREFIX, "");
  if (CANNOT_REPAIR.test(text)) return cannotRepairText(kind);
  for (const [re, , human] of KNOWN) if (re.test(text.trim())) return human;
  // The core refuses a moderation action the caller's role no longer allows,
  // in its own lowercase words. The usual way to get here is a role taken
  // away while this device was offline, with the menu not yet caught up.
  const denied = /^you don'?t have permission to (.+?)\.?$/i.exec(text.trim());
  if (denied) return `Your role doesn't let you ${denied[1]} here any more — it may have changed while you were away.`;
  if (msg?.offline || RAW_NETWORK.test(text.trim()))
    return "Concord isn't responding — trying to reconnect";
  if (RAW_DIAL.test(text))
    return "Couldn't reach them — they're probably offline, or on a network this device can't get to from here.";
  if (BARE_HTTP.test(text.trim())) return "Concord's core rejected it — try again";
  // Slow mode says its own sentence, and says it in the present tense with a
  // number that ticks (PendingMessage). "slow mode: wait 28s" was a Go string
  // captured once: ten seconds later it was stating a falsehood and thirty
  // seconds later it still said wait when Retry would have worked.
  if (slowModeWait(text)) return "Slow mode in this channel";
  // Anything left is the core's own sentence. Most of those were written for
  // a person ("they're already in this guild") and pass; one that still looks
  // like plumbing — a package chain, an address, a path, a Go runtime phrase —
  // never reaches the screen. The raw text goes to the console for whoever is
  // debugging.
  if (looksRaw(text)) {
    console.warn("concord: unmapped error:", text);
    return UNKNOWN;
  }
  return text;
}

// What a person reads for an error nothing above recognised.
export const UNKNOWN =
  "Something went wrong on this device. Try again — if it keeps happening, restart Concord.";

const RAW_MARKS = [
  /^[a-z][a-z0-9_ ]*: [a-z][a-z0-9_ ]*: /, // a chain of lowercase "pkg: op: …" clauses
  /\/(ip4|ip6|dns4?|dns6|tcp|udp|quic(-v1)?|p2p|webtransport)\//i, // a multiaddr
  /(^|\s)(\/[\w.-]+){2,}/, // a filesystem path
  /\b(EOF|nil pointer|panic|goroutine|context canceled|no such host|connection refused|i\/o timeout|unexpected end of JSON|invalid character|sql(?=:)|SQLITE_|0x[0-9a-f]{6,})\b/i,
  /\b12D3Koo\w{20,}/, // a peer id
];
export function looksRaw(text) {
  const t = String(text || "").trim();
  if (!t) return true;
  return RAW_MARKS.some((re) => re.test(t));
}
