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
const GO_PREFIX = /^(?:(?:app|net|store|mls|bridge|identity|rpc\s+\w+):\s*)+/;
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
const RAW_DIAL = /failed to dial|all dials failed|no good addresses|dial backoff/i;

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

export function humanError(msg) {
  const text = String(msg?.message ?? msg ?? "").replace(GO_PREFIX, "");
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
  return text;
}
