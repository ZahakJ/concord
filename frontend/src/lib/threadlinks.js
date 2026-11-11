// threadlinks.js — which threads were started from which message.
//
// A thread started from a message opens with a quote of it and a
// concord://msg/<channel>/<message> link back (Message.svelte
// createThreadFromMessage). Nothing on the wire records the pairing the
// other way, so the parent message had no sign it had grown a thread. The
// client reads it back from the openers it already holds: no new field, no
// new message, and an older peer's threads are found the same way.

const LINK = /concord:\/\/msg\/([^/\s)]+)\/([^\s)]+)/;

// openerSource(content, parentId) -> the id of the message in `parentId`
// this opener links back to, or "".
export function openerSource(content, parentId) {
  const m = LINK.exec(String(content || ""));
  if (!m || m[1] !== parentId) return "";
  return m[2];
}

// The newest CAP threads of a channel are read, each from its own first
// messages; an older thread simply shows no chip. A thread's history is local,
// so this costs reads, not network — but it is still one read per thread.
export const THREAD_CAP = 20;

// threadsBySource(threads, parentId, readFirst) -> Map messageId → [{id, name}]
//   threads    the parent's child channels, newest first
//   readFirst  async (channelId) => the thread's messages, oldest first
export async function threadsBySource(threads, parentId, readFirst, cap = THREAD_CAP) {
  const out = new Map();
  const picked = (threads || []).slice(0, cap);
  const found = await Promise.all(
    picked.map(async (t) => {
      try {
        const msgs = (await readFirst(t.id)) || [];
        // The opener is the first message; a few in case a system row leads.
        for (const m of msgs.slice(0, 3)) {
          const src = openerSource(m.content, parentId);
          if (src) return [src, t];
        }
      } catch {
        /* a thread we cannot read just shows no chip */
      }
      return null;
    }),
  );
  for (const hit of found) {
    if (!hit) continue;
    const [src, t] = hit;
    if (!out.has(src)) out.set(src, []);
    out.get(src).push({ id: t.id, name: t.name });
  }
  return out;
}
