// calltimer.svelte.js — how long you have been in this call.
//
// Every calling product on earth shows `12:34` and Concord showed it nowhere:
// not on the stage, not in the sidebar bar, not in the floating dock, not in
// the channel header. It is the cheapest liveness signal there is — a clock
// that has stopped is a call that has died — and it is the answer to the only
// question a caller asks the screen once the talking has started.
//
// ONE ticker for every surface that shows it. Four intervals would be four
// clocks free to disagree by up to a second, which is exactly the kind of
// detail that makes a UI feel assembled rather than built.

// BOTH are $state, and the start time especially. `callClock()` returns early
// while there is no call, without ever reading the tick — so a reader that
// first ran before the call was up would have captured NO dependency at all
// and never re-run. The panel that mounts on the click and the sidebar bar that
// mounts a second later would then disagree about whether there is a clock.
let startedAt = $state(0);
let now = $state(0);
let timer = null;
// When the call stopped carrying, if it has. See holdCallClock.
let heldAt = $state(0);

// startCallClock is called when the call is really up (S.voice is set), not
// when the channel is clicked: the seconds should count the call, not the
// microphone permission dialog in front of it.
export function startCallClock(at = Date.now()) {
  startedAt = at;
  now = Date.now();
  if (timer) return;
  // 1000ms exactly is fine here — the displayed value only has second
  // resolution, so a drifting tick shows the same string either way.
  timer = setInterval(() => (now = Date.now()), 1000);
}

export function stopCallClock() {
  startedAt = 0;
  now = 0;
  heldAt = 0;
  clearInterval(timer);
  timer = null;
}

// holdCallClock(true) freezes the display; holdCallClock(false) resumes it from
// the number it froze at.
//
// The comment at the top of this file sells the clock as the cheapest liveness
// signal there is — "a clock that has stopped is a call that has died" — and it
// was a wall clock started at join and never consulted about the call again, so
// it could not stop. Killing the node under a peer left it counting past two
// minutes beside a banner saying that peer was offline.
//
// Resuming shifts the start rather than jumping forward, so the number counts
// the time the call was actually carrying. Anything else would make the claim
// false in the other direction: a call that spent a minute reconnecting has not
// been running for that minute.
export function holdCallClock(on) {
  if (!startedAt) return;
  if (on) {
    if (!heldAt) heldAt = Date.now();
  } else if (heldAt) {
    startedAt += Date.now() - heldAt;
    heldAt = 0;
  }
}
export function callHeld() {
  return !!heldAt;
}

export function callSeconds() {
  if (!startedAt) return 0;
  return Math.max(0, Math.floor(((heldAt || now) - startedAt) / 1000));
}

// mm:ss, and h:mm:ss once a call has run past the hour — a bare "78:04" reads
// as a bug the first time you see it.
export function fmtElapsed(secs) {
  const s = Math.max(0, Math.floor(secs));
  const mm = Math.floor(s / 60) % 60;
  const ss = s % 60;
  const hh = Math.floor(s / 3600);
  const pad = (n) => (n < 10 ? `0${n}` : `${n}`);
  return hh ? `${hh}:${pad(mm)}:${pad(ss)}` : `${mm}:${pad(ss)}`;
}

// The string the surfaces render. Empty before the call is up, so a caller
// never sees "0:00" for the second between the click and the connection.
export function callClock() {
  return startedAt ? fmtElapsed(callSeconds()) : "";
}
