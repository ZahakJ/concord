// outbox.js — the pure half of the outbox: given the rows this device is still
// waiting on and the messages the channel already holds, which rows the feed
// should still draw.
//
// Split out of outbox.svelte.js the way the importer is split from the job that
// runs it: this half has no state, no network and no runes, so it can be tested
// in plain node — and both of the rules in it were bugs before they were rules.

// unsettled: of `entries`, the ones the feed should still draw given the
// messages the channel already holds.
//
// This exists only for a race. A pending row is normally dropped the moment its
// own RPC resolves, but the core stores, emits its event and returns in that
// order over two different connections — so on one of the two arrival orders
// the real row and the pending row are both drawn for a frame. This is what
// suppresses that frame.
//
// It counts rather than tests membership, and each entry carries the count that
// was already there when it was queued. A set membership test — "is this body
// present?" — retires a row the instant it is created if you have ever said
// those words before, and people repeat themselves constantly: the first live
// drive of this showed no pending row at all, because an identical message from
// an earlier run was still in the channel. A timestamp window was tried and is
// worse: it turns "how recently did you last say this?" into a tuning problem,
// and two "ok"s a second apart is a real thing people do.
//
// `consumed` is what makes two identical sends in flight work: one arriving
// message settles exactly one pending row, oldest first.
export function unsettled(entries, messages, me) {
  if (!entries.length) return entries;
  const count = new Map();
  for (const m of messages) {
    if (m.sender !== me || m.deleted || !m.content) continue;
    count.set(m.content, (count.get(m.content) || 0) + 1);
  }
  const consumed = new Map();
  const out = [];
  for (const e of entries) {
    // A failed row is the user's to retry or discard; a coincidence must never
    // silently delete their text. An attachment has no body to match on until
    // the core answers, so it is only ever retired by its own RPC.
    if (e.state === "failed" || !e.match) {
      out.push(e);
      continue;
    }
    const used = consumed.get(e.match) || 0;
    if ((count.get(e.match) || 0) - (e.seen || 0) - used > 0) consumed.set(e.match, used + 1);
    else out.push(e);
  }
  return out;
}

// alreadySaid: how many messages with exactly this body the channel already
// holds from us. Recorded on an entry as it is queued — see above.
export function alreadySaid(messages, me, body) {
  let n = 0;
  for (const m of messages) if (m.sender === me && !m.deleted && m.content === body) n++;
  return n;
}

// toFlush: the failed rows to send again by themselves once the core is back.
// Only rows that failed because the core itself was unreachable (`offline`):
// the banner promised those would go when it returned, and they never did —
// the row sat at "Not sent · trying to reconnect" for good. A row the core
// REFUSED (a slow-mode wait not yet over, a permission, a blocked member)
// stays the user's to retry, because sending it again unasked would only be
// refused again, or worse, land somewhere they no longer meant it to.
export function toFlush(entries, now = Date.now()) {
  return entries.filter(
    (e) => e.state === "failed" && e.offline && e.kind === "text" && e.body && !(e.retryAt > now),
  );
}

// unheard: did this send fail because no unlocked core was there to hear it?
// Either nothing answered at all, or a core that had restarted answered while
// still locked — which is what a core coming back looks like for the moment
// before the user unlocks it, and exactly when the transport-up flush runs.
export function unheard(err) {
  return !!err?.offline || /identity is locked/i.test(String(err?.message ?? err ?? ""));
}

// ---- rows that must outlive the page -----------------------------------------
//
// A message waiting for its link card has never reached the core, so it is
// written down as "Not sent" and goes out by itself after a reload. It stays
// written down until its send has FINISHED (`handed`): on the web a reload or
// a closed tab ends the wait and starts the send in the same moment, and the
// browser may cancel that request as the page goes. The row is removed only
// when the core has answered (link-cards-2 review LC2-03).
//
// Keeping it that long means a reload can find a row whose send did reach the
// core. So a row restored as `handed` is checked against the channel before it
// is sent again (landedSince), and a row another open tab of this browser is
// still sending is left to that tab (`sendId`, claimed while it waits and
// sends: LC2-05).

// How long a claim holds without its tab: a card's wait plus a text send's
// deadline, and some slack. A tab that closes drops its claims at once.
export const CLAIM_TTL_MS = 45000;

// toPersist: the outbox rows to write down, in the form they come back in.
export function toPersist(entries) {
  return entries
    .filter((e) => (e.state === "failed" || e.held || e.handed) && e.kind === "text" && e.body)
    .map((e) => {
      const unsent = !!(e.held || e.handed);
      return {
        id: e.id,
        kind: "text",
        channelId: e.channelId,
        body: e.body,
        replyTo: e.replyTo || "",
        dir: e.dir || "",
        match: e.match,
        seen: e.seen,
        state: "failed",
        // Waiting for its card, or being sent, when the page went: the core
        // may never have had it, so it is "not sent" and flushes by itself
        // (toFlush) once the core is back.
        error: unsent ? "Not sent" : e.error || "",
        // A wall-clock moment survives the reload the same way it survives
        // the next second: a slow-mode wait that expired while the app was
        // closed comes back already expired, which is the truth.
        retryAt: e.retryAt || 0,
        offline: !!e.offline || unsent,
        sendId: e.sendId || "",
        // Its send had started: ask the channel before sending it again.
        handed: !!e.handed,
        at: e.at,
      };
    });
}

// claimedElsewhere: is this row being sent by another open tab right now?
export function claimedElsewhere(claims, sendId, tab, now = Date.now()) {
  const c = sendId && claims ? claims[sendId] : null;
  return !!c && c.tab !== tab && now - (c.at || 0) < CLAIM_TTL_MS;
}

// restorable: of the rows written down, the ones a page loading now takes on.
export function restorable(rows, claims, tab, now = Date.now()) {
  if (!Array.isArray(rows)) return [];
  return rows.filter(
    (e) => e && e.kind === "text" && e.channelId && e.body && !claimedElsewhere(claims, e.sendId, tab, now),
  );
}

// toStore: what this tab writes down. Its own rows, plus any row already
// written down that another open tab is still sending, which this tab never
// took on and must not erase.
export function toStore(mine, stored, claims, tab, now = Date.now()) {
  const own = new Set(mine.map((e) => e.sendId).filter(Boolean));
  const theirs = (Array.isArray(stored) ? stored : []).filter(
    (e) => e && e.sendId && !own.has(e.sendId) && claimedElsewhere(claims, e.sendId, tab, now),
  );
  return [...mine, ...theirs];
}

// landedSince: did the channel get this exact message from us at or after
// `since` (epoch ms)? A small allowance covers the core stamping its own time.
export function landedSince(messages, me, body, since) {
  const from = (since || 0) - 5000;
  return (messages || []).some(
    (m) => m.sender === me && !m.deleted && m.content === body && Date.parse(m.sent) >= from,
  );
}
