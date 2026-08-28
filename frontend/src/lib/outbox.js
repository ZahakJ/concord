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
