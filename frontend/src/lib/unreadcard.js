// unreadcard.js — the counts Concord offers Jayb's Messages tile.
//
// Jayb (the owner's launcher) shows how many messages are unread and in how
// many conversations, read through a provider only an app signed by the
// owner's Jayb key can open (jaybos CONTRACTS.md §3). The page is where the
// counts live (S.unread), so it hands the native side one number per
// conversation: no words, no names, nothing else. The rules:
//
//   • nothing is sent before the page has counted once. Until then S.unread
//     holds whatever survived the last refresh, and sending it would replace
//     the native side's own, newer tally;
//   • a burst of changes is one call, a second after the last of them;
//   • identical summaries are sent once.

// At most this many conversations go native; the rest add to none.
export const MAX_CONVERSATIONS = 2000;
export const MAX_COUNT = 999_999;
export const DEBOUNCE_MS = 1000;
const ID = /^[0-9A-Za-z_.:-]{1,128}$/;

// summary(unread) → { counts: { id: n } } with only conversations that have
// something unread, ids of the form the core makes, and capped counts.
export function summary(unread) {
  const counts = {};
  let n = 0;
  for (const [id, u] of Object.entries(unread || {})) {
    if (n >= MAX_CONVERSATIONS) break;
    const c = Math.floor(Number(u?.count) || 0);
    if (!ID.test(id) || c <= 0) continue;
    counts[id] = Math.min(c, MAX_COUNT);
    n++;
  }
  return { counts };
}

// read() → S.unread; send(summary) delivers it; timers are injectable for tests.
export function createUnreadSync({ read, send, setTimer = setTimeout, clearTimer = clearTimeout }) {
  let counted = false;
  let last = "";
  let timer = null;
  function flush() {
    timer = null;
    const s = summary(read());
    const key = JSON.stringify(s);
    if (key === last) return false;
    last = key;
    send(s);
    return true;
  }
  function changed() {
    if (!counted) return;
    if (timer !== null) clearTimer(timer);
    timer = setTimer(flush, DEBOUNCE_MS);
  }
  return {
    // S.unread moved (a bump, a read, a recount).
    changed,
    // The page finished counting once: from now on its counts are the truth.
    counted() {
      counted = true;
      changed();
    },
    // Going to the background: send now, not a second from now.
    flushNow() {
      if (!counted) return false;
      if (timer !== null) clearTimer(timer);
      return flush();
    },
  };
}
