// outbox.svelte.js — what a message looks like between pressing Enter and the
// core saying it exists.
//
// send() used to clear the draft and the attachment tray FIRST and then await
// the round trip. On a fast loopback that reads as instant; on a real P2P link
// with a 5 MB image it is several seconds in which your words have visibly
// vanished from the box and nothing has appeared in the feed. The only signal
// was the failure case, which handed the draft back with a toast — good
// recovery, and no during-state at all.
//
// So a send now becomes a row immediately: dimmed, with a clock, at the bottom
// of the feed where the real one will be. It is promoted the moment the message
// comes back from the core, and on failure it stays put and grows a Retry.
//
// WHY NOT IN S.messages. That array is the core's answer to "what is in this
// channel", and half the app is entitled to treat it that way — dedupe by id,
// the backfill prepend, the MAX_LOADED_ROWS trim, reply and pin lookups, the
// unread arithmetic. A row with no id and no `sent` from the store would have
// to be excluded from each of those by hand, one forgotten exclusion at a time.
// The outbox is its own list and the feed merges it at the very end, where the
// merge is one concat of rows that are always newest.
//
// SURVIVING A RELOAD. A failed row is the only copy of what you wrote — the
// draft is deliberately cleared when the pending row is created, and Discard is
// the only path that puts the text on the clipboard. It used to die on F5 in
// silence, with no beforeunload prompt and nothing on disk. So failed rows are
// written beside the drafts, in the same storage, keyed the same way, and read
// back on load; and while anything is still unsettled the page asks before it
// goes away.
//
// Text only, and the asymmetry is deliberate. An attachment's data URL is
// megabytes and localStorage is a handful, so persisting one would break the
// storage the drafts live in — and the picture still exists on the user's disk,
// which is exactly what the typed words do not. The unload guard covers that
// case; persistence covers the one where there is nothing to go back to.
//
// Only FAILED rows are persisted. A row still in flight at unload may well have
// landed — the core stores before it answers — so writing it down as something
// to retry is how you send the same message twice.
import { S, flash, scrollSoon, humanError } from "./state.svelte.js";
import { api } from "./api.js";
import { unsettled, alreadySaid } from "./outbox.js";

let seq = 0;

const STORE_KEY = "concord.outbox";

function persist() {
  try {
    const keep = S.outbox
      .filter((e) => e.state === "failed" && e.kind === "text" && e.body)
      .map((e) => ({
        id: e.id,
        kind: "text",
        channelId: e.channelId,
        body: e.body,
        replyTo: e.replyTo || "",
        dir: e.dir || "",
        match: e.match,
        seen: e.seen,
        state: "failed",
        error: e.error || "",
        at: e.at,
      }));
    if (keep.length) localStorage.setItem(STORE_KEY, JSON.stringify(keep));
    else localStorage.removeItem(STORE_KEY);
  } catch {
    /* private mode / quota: the row is still on screen, it just won't survive */
  }
}

// rehydrate runs once, at import, before the first render — so a reload that
// followed a failed send shows the row and its Retry rather than a feed with a
// hole where the message was.
function rehydrate() {
  let rows = [];
  try {
    rows = JSON.parse(localStorage.getItem(STORE_KEY) || "[]");
  } catch {
    return;
  }
  if (!Array.isArray(rows) || !rows.length) return;
  S.outbox = rows.filter((e) => e && e.kind === "text" && e.channelId && e.body);
  // New ids must not collide with restored ones, or retry/discard would act on
  // the wrong row.
  for (const e of S.outbox) {
    const n = Number(String(e.id).slice(1));
    if (Number.isFinite(n) && n > seq) seq = n;
  }
}
rehydrate();

// The unload guard. Not "are there failed rows" but "is anything unsettled":
// a send still in flight is also something a reload would throw away, and it is
// the case where the user cannot even see that they are about to lose it.
export function outboxUnsettled() {
  return S.outbox.some((e) => e.state === "failed" || e.state === "sending");
}

if (typeof window !== "undefined") {
  window.addEventListener("beforeunload", (e) => {
    if (!outboxUnsettled()) return;
    e.preventDefault();
    // Chrome ignores the string and shows its own wording; Firefox and WebKit
    // want returnValue set at all. Both need the assignment to raise anything.
    e.returnValue = "";
  });
}

// failedIn / failedCount — what the sidebar and the rail read. A channel with an
// unsent DRAFT was marked and a channel with a FAILED SEND was not, which is
// exactly backwards: one is a thought you chose not to finish, the other is a
// message you believe you sent.
export function failedIn(channelId) {
  if (!channelId || !S.outbox.length) return 0;
  return S.outbox.filter((e) => e.channelId === channelId && e.state === "failed").length;
}
// The text of the oldest failed row in a conversation — what the DM list shows
// in place of the last message, the way it already does for a draft.
export function failedBodyIn(channelId) {
  if (!channelId || !S.outbox.length) return "";
  return S.outbox.find((e) => e.channelId === channelId && e.state === "failed")?.body || "";
}
export function failedInGuild(guild) {
  if (!guild?.channels?.length || !S.outbox.length) return 0;
  const ids = new Set(guild.channels.map((c) => c.id));
  return S.outbox.filter((e) => e.state === "failed" && ids.has(e.channelId)).length;
}

// entriesFor: what the feed should draw under `channelId`, in send order. One
// scan of an array that is empty every moment nobody is sending.
export function entriesFor(channelId) {
  if (!S.outbox.length) return [];
  const mine = S.outbox.filter((e) => e.channelId === channelId);
  if (!mine.length) return [];
  return unsettled(mine, S.messages, S.identity.fingerprint);
}

function add(entry) {
  S.outbox = [...S.outbox, entry];
  scrollSoon();
  return entry;
}

function drop(id) {
  S.outbox = S.outbox.filter((e) => e.id !== id);
  persist();
}

function patch(id, fields) {
  S.outbox = S.outbox.map((e) => (e.id === id ? { ...e, ...fields } : e));
  persist();
}

// sendText: the ordinary case. `body` is the FINAL string — slash commands,
// shortcodes, the ephemeral and seal stamps have all been applied by the
// caller, because that is the string the core will store and therefore the
// string the echo check has to compare against.
export function sendText(channelId, body, replyTo, dir) {
  const entry = add({
    id: `o${++seq}`,
    kind: "text",
    channelId,
    body,
    replyTo: replyTo || "",
    dir: dir || "",
    match: body,
    // How many times this exact body is already in the channel from us, so the
    // echo check can tell a NEW arrival from an old one you happened to repeat.
    seen: alreadySaid(S.messages, S.identity.fingerprint, body),
    state: "sending",
    at: Date.now(),
  });
  return run(entry);
}

// sendAttachment: the staged chip, with its thumbnail carried into the pending
// row so the picture is on screen while it is being sealed and sent, which on a
// 5 MB image is the whole of the wait.
export function sendAttachment(channelId, att, replyTo) {
  const entry = add({
    id: `o${++seq}`,
    kind: "att",
    channelId,
    att,
    replyTo: replyTo || "",
    match: "", // an attach token's blob id is not known until the core answers
    state: "sending",
    at: Date.now(),
  });
  return run(entry);
}

async function run(entry) {
  try {
    if (entry.kind === "att") {
      const a = entry.att;
      if (a.isImage)
        await api.sendAttachment(
          entry.channelId,
          a.dataUrl,
          a.w,
          a.h,
          entry.replyTo,
          !!a.spoiler,
          a.name || "",
          a.desc || "",
        );
      else await api.sendFile(entry.channelId, a.dataUrl, a.name, entry.replyTo);
    } else {
      await api.sendMessage(entry.channelId, entry.body, entry.replyTo, entry.dir);
    }
    drop(entry.id);
    return true;
  } catch (err) {
    // The row stays where it is and says so. It is NOT re-thrown: the caller
    // used to be the only thing that knew a send had failed, which is why the
    // recovery was "your draft is back, work out what happened".
    // humanError, the same pass the toasts get: a row that says "rpc
    // SendMessage: HTTP 500" is telling the reader about our transport, not
    // about their message.
    patch(entry.id, { state: "failed", error: humanError(err) || "Couldn't send" });
    return false;
  }
}

export function retry(id) {
  const entry = S.outbox.find((e) => e.id === id);
  if (!entry || entry.state !== "failed") return;
  patch(id, { state: "sending", error: "" });
  run({ ...entry, state: "sending" });
}

// discard: give up on a failed send. The body goes back to the clipboard rather
// than into the void, because it is the only copy left — the draft was cleared
// when the row was created.
export function discard(id) {
  const entry = S.outbox.find((e) => e.id === id);
  if (entry?.kind === "text" && entry.body) {
    navigator.clipboard?.writeText(entry.body);
    flash("Copied to the clipboard", "success");
  }
  drop(id);
}
