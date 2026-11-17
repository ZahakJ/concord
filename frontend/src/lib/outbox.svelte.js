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
// to retry is how you send the same message twice. A row that waited for its
// link card is the exception: the core has never seen it while it waits
// (`held`), and once the wait ends it stays written down until its send has
// finished (`handed`), checked against the channel before it is sent again
// (outbox.js, "rows that must outlive the page").
//
// LINK CARDS. A message with a link waits up to CARD_WAIT_MS for the card this
// device fetches for it, and goes out the moment the card comes. The wait is
// never a queue (lib/cardgate.js): any other send into the channel, and the
// app going to the background, end it, and the message goes out first,
// without its card.
import { S, flash, scrollSoon, humanError, slowModeWait } from "./state.svelte.js";
import { api, onTransportHealth, onBeforeSend, clientID } from "./api.js";
import {
  unsettled,
  alreadySaid,
  toFlush,
  unheard,
  toPersist,
  restorable,
  toStore,
  landedSince,
} from "./outbox.js";
import { makeCardGate } from "./cardgate.js";
import { CARD_WAIT_MS, cardLink, wantsThumb, withCard, sendingCards, cardChoice } from "./linkcard.js";
import { heldRow, fileSendOp } from "./mediastrip.js";

let seq = 0;

const gate = makeCardGate({ waitMs: CARD_WAIT_MS });
onBeforeSend((channelId) => gate.release(channelId));
if (typeof document !== "undefined") {
  // Hidden is the last moment a phone app reliably runs: swiped away, it is
  // killed without another event. The waiting message is handed over now.
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "hidden") gate.releaseAll();
  });
  // On the web this is also a reload or a closed tab: the waiting message's
  // send starts here and may be cut off, so its row stays written down until
  // the send finishes (outbox.js toPersist), and the next page sends it if it
  // never arrived. The claims go, so that page does not take this tab for
  // one still sending it.
  window.addEventListener("pagehide", () => {
    gate.releaseAll();
    dropClaims();
  });
}

const STORE_KEY = "concord.outbox";
const CLAIMS_KEY = "concord.outbox.claims";

// Claims: which open tab is sending which waiting row, so a second tab of the
// same browser, which reads the same storage, does not send it too (LC2-05).
function readClaims() {
  try {
    const c = JSON.parse(localStorage.getItem(CLAIMS_KEY) || "{}");
    return c && typeof c === "object" ? c : {};
  } catch {
    return {};
  }
}
function writeClaims(c) {
  try {
    if (Object.keys(c).length) localStorage.setItem(CLAIMS_KEY, JSON.stringify(c));
    else localStorage.removeItem(CLAIMS_KEY);
  } catch {
    /* private mode / quota */
  }
}
function claim(sendId) {
  if (sendId) writeClaims({ ...readClaims(), [sendId]: { tab: clientID, at: Date.now() } });
}
function unclaim(sendId) {
  const c = readClaims();
  if (!sendId || !c[sendId]) return;
  delete c[sendId];
  writeClaims(c);
}
function dropClaims() {
  const c = readClaims();
  for (const [k, v] of Object.entries(c)) if (v?.tab === clientID) delete c[k];
  writeClaims(c);
}
function newSendId() {
  return typeof crypto !== "undefined" && crypto.randomUUID
    ? crypto.randomUUID()
    : `s${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`;
}

function readStored() {
  try {
    return JSON.parse(localStorage.getItem(STORE_KEY) || "[]");
  } catch {
    return [];
  }
}

function persist() {
  try {
    const keep = toStore(toPersist(S.outbox), readStored(), readClaims(), clientID);
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
  // A row another open tab is still sending is that tab's (LC2-05).
  S.outbox = restorable(rows, readClaims(), clientID);
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
  // Whatever waits for a card here goes first (also done by api.js for every
  // send; done here as well so the order does not hang on that hook).
  gate.release(channelId);
  const cardUrl = sendingCards(cardChoice(S.prefs)) ? cardLink(body) : "";
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
    // The link whose card this row is waiting for (PendingMessage draws the
    // card's place), and whether it is still waiting.
    cardUrl,
    held: !!cardUrl,
    sendId: cardUrl ? newSendId() : "",
    at: Date.now(),
  });
  if (!cardUrl) return run(entry);
  claim(entry.sendId);
  persist();
  return new Promise((resolve) => {
    gate.hold(
      channelId,
      () => api.linkCard(cardUrl, wantsThumb(body)).then((c) => c?.block || ""),
      (block) => {
        const final = block ? withCard(body, cardUrl, block) : body;
        // The words that come back from the core are these, so this is what
        // the echo check has to look for.
        patch(entry.id, {
          body: final,
          match: final,
          seen: alreadySaid(S.messages, S.identity.fingerprint, final),
          held: false,
          // Still written down, as not sent, until the core answers: this
          // send may be cut off by the page going away (LC2-03).
          handed: true,
        });
        const now = S.outbox.find((e) => e.id === entry.id);
        resolve(now ? run(now) : false);
      },
    );
  });
}

// sendAttachment: the staged chip, with its thumbnail carried into the pending
// row so the picture is on screen while it is being sealed and sent, which on a
// 5 MB image is the whole of the wait.
// text is what rides in the same message (attachText: caption plus stamps);
// caption is the bare words, for the pending row and the clipboard.
export function sendAttachment(channelId, att, replyTo, text = "", dir = "", caption = "") {
  gate.release(channelId);
  const entry = add({
    id: `o${++seq}`,
    kind: "att",
    channelId,
    att,
    text,
    dir,
    caption,
    replyTo: replyTo || "",
    match: "", // an attach token's blob id is not known until the core answers
    state: "sending",
    at: Date.now(),
  });
  return run(entry);
}

// How long a send may go unanswered before its row stops saying "Sending…".
// A wedged core (or a request lost on the way) used to leave the row clocked
// forever, with no Retry and no Discard — the draft gone from the box and no
// way to get it back short of a reload.
export const SEND_TIMEOUT_MS = { text: 30000, att: 120000 };
const TIMED_OUT = "No answer from Concord's core yet — it may still go out. Retry, or discard it.";

function deliver(entry) {
  if (entry.kind === "att") {
    const a = entry.att;
    if (a.isImage)
      return api.sendAttachment(
        entry.channelId,
        a.dataUrl,
        a.w,
        a.h,
        entry.replyTo,
        !!a.spoiler,
        a.name || "",
        a.desc || "",
        entry.text || "",
        entry.dir || "",
      );
    // The person's choice on a held row (sendAsIs, sendRemovingText), if any.
    return api[fileSendOp(a)](entry.channelId, a.dataUrl, a.name, entry.replyTo, entry.text || "", entry.dir || "");
  }
  return api.sendMessage(entry.channelId, entry.body, entry.replyTo, entry.dir);
}

async function run(entry) {
  const attempt = `${entry.id}:${Date.now()}`;
  patch(entry.id, { attempt });
  const call = deliver(entry);
  // An answer that comes after the row gave up still counts: the message did
  // go out, so the row goes and the real one takes its place. (A Retry pressed
  // in between sends it twice; telling the two apart needs an idempotency key
  // the core does not have.)
  call.then(
    () => {
      const now = S.outbox.find((e) => e.id === entry.id);
      if (now?.attempt === attempt && now.timedOut) drop(entry.id);
    },
    () => {},
  );
  let timer;
  const deadline = new Promise((_, reject) => {
    timer = setTimeout(() => reject(TIMED_OUT), SEND_TIMEOUT_MS[entry.kind] || SEND_TIMEOUT_MS.text);
  });
  try {
    await Promise.race([call, deadline]);
    drop(entry.id);
    return true;
  } catch (err) {
    if (err === TIMED_OUT) {
      patch(entry.id, { state: "failed", error: TIMED_OUT, retryAt: 0, timedOut: true, handed: false });
      return false;
    }
    // The row stays where it is and says so. It is NOT re-thrown: the caller
    // used to be the only thing that knew a send had failed, which is why the
    // recovery was "your draft is back, work out what happened".
    // humanError, the same pass the toasts get: a row that says "rpc
    // SendMessage: HTTP 500" is telling the reader about our transport, not
    // about their message.
    // A slow-mode refusal is a WAIT, not a failure, and the core told us how
    // long. Keeping the moment rather than the number is what lets the row tick
    // instead of freezing at "wait 28s" for the next half minute.
    // A file the core held back is waiting, not failed: the row says why
    // (it would go as it is, location and all; or it has text tracks) and
    // offers the choices. Nothing has left the device.
    const held = heldRow(err);
    if (held) {
      patch(entry.id, held);
      return false;
    }
    const wait = slowModeWait(err);
    // The guild's kind only picks a noun ("guild" or "conversation") for the
    // one refusal that names it: a copy that cannot be repaired from here.
    const kind = S.guilds.find((g) => g.channels?.some((c) => c.id === entry.channelId))?.kind;
    patch(entry.id, {
      state: "failed",
      error: humanError(err, kind) || "Couldn't send",
      retryAt: wait ? Date.now() + wait * 1000 : 0,
      // The core never heard it — see toFlush.
      offline: unheard(err),
      timedOut: false,
      // A request that died on the way (the page going away cuts it off) may
      // still have reached the core: a row that was being sent after its
      // card's wait keeps being checked against the channel before it goes
      // again.
      handed: unheard(err) && !!S.outbox.find((e) => e.id === entry.id)?.handed,
    });
    return false;
  } finally {
    clearTimeout(timer);
    unclaim(entry.sendId);
  }
}

// flushOutbox sends again, in the order they were written, every row that failed
// only because the core was gone. Called when the transport comes back and when
// a session unlocks (a restarted core comes back locked, and a send into a
// locked core fails the same way). Sequential, so a conversation's messages
// keep their order; the first one that fails again stops the run — the core is
// still not taking them, and the rest would only fail behind it.
let flushing = false;
export async function flushOutbox() {
  if (flushing) return;
  flushing = true;
  try {
    for (const e of toFlush(S.outbox)) {
      if (!S.outbox.some((x) => x.id === e.id && x.state === "failed")) continue;
      // Its send had started when the last page went: it may have arrived.
      if (e.handed) {
        let msgs;
        try {
          msgs = (await api.messages(e.channelId)) || [];
        } catch {
          break;
        }
        if (landedSince(msgs, S.identity.fingerprint, e.body, e.at)) {
          drop(e.id);
          continue;
        }
      }
      claim(e.sendId);
      patch(e.id, { state: "sending", error: "" });
      if (!(await run({ ...e, state: "sending" }))) break;
    }
  } finally {
    flushing = false;
  }
}

export function retry(id) {
  const entry = S.outbox.find((e) => e.id === id);
  if (!entry || entry.state !== "failed") return;
  patch(id, { state: "sending", error: "", timedOut: false });
  run({ ...entry, state: "sending" });
}

// sendAsIs: the person read that this file's details could not be removed and
// chose to send it as it is. Only a row the core held for that reason.
export function sendAsIs(id) {
  const entry = S.outbox.find((e) => e.id === id);
  if (!entry || entry.state !== "failed" || !entry.asIs || entry.kind !== "att") return;
  const att = { ...entry.att, asIsChosen: true };
  patch(id, { state: "sending", error: "", asIs: "", textTracks: false, att });
  run({ ...entry, att, asIs: "", textTracks: false, state: "sending" });
}

// sendRemovingText: the person chose to have a video's subtitle, text and
// timecode tracks removed with its other details. Only a row held for that.
export function sendRemovingText(id) {
  const entry = S.outbox.find((e) => e.id === id);
  if (!entry || entry.state !== "failed" || !entry.textTracks || entry.kind !== "att") return;
  const att = { ...entry.att, dropTextChosen: true };
  patch(id, { state: "sending", error: "", asIs: "", textTracks: false, att });
  run({ ...entry, att, asIs: "", textTracks: false, state: "sending" });
}

// discard: give up on a failed send. The body goes back to the clipboard rather
// than into the void, because it is the only copy left — the draft was cleared
// when the row was created.
export function discard(id) {
  const entry = S.outbox.find((e) => e.id === id);
  const words = entry?.kind === "text" ? entry.body : entry?.caption;
  if (words) {
    navigator.clipboard?.writeText(words);
    flash("Copied to the clipboard", "success");
  }
  drop(id);
}

// The core answering again is the moment the banner's promise comes due. A
// core that restarted comes back locked, so this finds nothing to say yet and
// the unlock (App.svelte, on S.ready) flushes instead.
//
// Asked first, not assumed: a send into a locked core would fail, and the
// page reloads to the lock screen on that answer — possibly while the row is
// marked "sending", which is the one state never written down (see persist),
// so the message would be lost in the reload. The identity call gets the same
// answer without risking a row.
onTransportHealth((up) => {
  if (!up || !S.ready) return;
  api
    .identity()
    .then(() => flushOutbox())
    .catch(() => {});
});
