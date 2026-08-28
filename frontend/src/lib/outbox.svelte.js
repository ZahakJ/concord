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
import { S, flash, scrollSoon, humanError } from "./state.svelte.js";
import { api } from "./api.js";
import { unsettled, alreadySaid } from "./outbox.js";

let seq = 0;

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
}

function patch(id, fields) {
  S.outbox = S.outbox.map((e) => (e.id === id ? { ...e, ...fields } : e));
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
