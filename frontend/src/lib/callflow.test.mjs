import {
  step,
  idle,
  isRinging,
  RING_MS,
  DECLINED,
  recordText,
  callLength,
  declineBlob,
  parseDecline,
  sharersOf,
  ghostsToReap,
  GHOST_MS,
  callPreview,
} from "./callflow.js";

let failures = 0;
const check = (name, ok) => {
  if (!ok) {
    failures++;
    console.log("  FAIL", name);
  }
};
const has = (fx, pred) => fx.some(pred);
const run = (st, ...evs) => {
  let fx = [];
  for (const ev of evs) ({ st, fx } = step(st, ev));
  return { st, fx };
};

// ---- caller ----
let r = step(idle(), { type: "dial", now: 0, reachable: true });
check("dial while they are online rings", r.st.phase === "ringing" && isRinging(r.st) && r.fx.length === 0);
r = step(idle(), { type: "dial", now: 0, reachable: false });
check("dial while nobody of theirs is online is unreachable, not ringing", r.st.phase === "unreachable" && isRinging(r.st));
r = run(idle(), { type: "dial", now: 0, reachable: false }, { type: "reach", reachable: true });
check("they come online mid-ring: unreachable becomes ringing", r.st.phase === "ringing");
r = run(idle(), { type: "dial", now: 0 }, { type: "reach", reachable: false });
check("they drop mid-ring: ringing becomes unreachable", r.st.phase === "unreachable");

r = run(idle(), { type: "dial", now: 0 }, { type: "arrive", now: 5000 });
check("they answer: active, and the clock starts then", r.st.phase === "active" && r.st.answeredAt === 5000 && has(r.fx, (f) => f.clock === "start"));
r = run(idle(), { type: "dial", now: 0, reachable: false }, { type: "arrive", now: 5000 });
check("an unreachable callee who answers is answered", r.st.phase === "active");

r = run(idle(), { type: "dial", now: 0 }, { type: "declined", by: "ben", now: 4000 });
check(
  "1:1 decline ends the caller's call, posts Declined and says so",
  r.st.phase === "ended" && r.st.outcome === "declined" && has(r.fx, (f) => f.leave) && has(r.fx, (f) => f.wire === DECLINED) && has(r.fx, (f) => f.say === "declined"),
);
r = run(idle(), { type: "dial", now: 0, group: true }, { type: "declined", by: "ben", now: 4000 });
check("group: one decline keeps ringing the others", r.st.phase === "ringing" && r.st.declinedBy.includes("ben") && !has(r.fx, (f) => f.leave));
r = run(idle(), { type: "dial", now: 0, group: true }, { type: "declined", by: "ben", now: 4000, everyone: true });
check("group: when everyone has declined the call ends", r.st.phase === "ended" && has(r.fx, (f) => f.leave));
r = run(idle(), { type: "dial", now: 0 }, { type: "arrive", now: 1 }, { type: "declined", by: "ben", now: 2 });
check("a stale decline after the answer changes nothing", r.st.phase === "active" && r.fx.length === 0);

r = run(idle(), { type: "dial", now: 0 }, { type: "tick", now: RING_MS - 1 });
check("still ringing just before the timeout", r.st.phase === "ringing" && r.fx.length === 0);
r = run(idle(), { type: "dial", now: 0 }, { type: "tick", now: RING_MS });
check(
  "ring timeout: caller hangs up and posts the missed-call line",
  r.st.phase === "ended" && r.st.outcome === "missed" && has(r.fx, (f) => f.leave) && has(r.fx, (f) => f.wire === "Missed call") && has(r.fx, (f) => f.say === "missed"),
);
r = run(idle(), { type: "dial", now: 0, reachable: false }, { type: "tick", now: RING_MS });
check("unreachable also times out, and says why", r.st.outcome === "missed" && has(r.fx, (f) => f.say === "unreachable"));
r = run(idle(), { type: "dial", now: 0 }, { type: "arrive", now: 10 }, { type: "tick", now: RING_MS * 10 });
check("an answered call never times out", r.st.phase === "active");

r = run(idle(), { type: "dial", now: 0 }, { type: "hangup", now: 3000 });
check(
  "caller hangs up while ringing: cancelled, missed-call line, no second leave",
  r.st.outcome === "cancelled" && has(r.fx, (f) => f.wire === "Missed call") && !has(r.fx, (f) => f.leave),
);
r = run(idle(), { type: "dial", now: 0 }, { type: "arrive", now: 1000 }, { type: "hangup", now: 121000 });
check(
  "hanging up an answered call writes a local Call ended record with its length",
  r.st.outcome === "ended" && has(r.fx, (f) => f.local === "ended:120") && !has(r.fx, (f) => f.wire),
);

// ---- the other side leaving ----
const active = run(idle(), { type: "dial", now: 0 }, { type: "arrive", now: 1000 }).st;
r = step(active, { type: "depart", goodbye: true, remaining: 0, now: 61000 });
check(
  "1:1: they hang up, so the call ends for us too",
  r.st.phase === "ended" && has(r.fx, (f) => f.leave) && has(r.fx, (f) => f.local === "ended:60") && has(r.fx, (f) => f.say === "ended"),
);
r = step(active, { type: "depart", goodbye: false, remaining: 0, now: 61000 });
check("1:1: a heartbeat that went quiet is not a hang-up", r.st.phase === "active" && r.fx.length === 0);
r = step(active, { type: "depart", goodbye: true, remaining: 1, now: 61000 });
check("1:1: one of their two devices leaving is not a hang-up", r.st.phase === "active");
const gActive = run(idle(), { type: "dial", now: 0, group: true }, { type: "arrive", now: 1000 }).st;
r = step(gActive, { type: "depart", goodbye: true, remaining: 0, now: 5000 });
check("group: the others leaving does not end our side, and never rings again", r.st.phase === "active" && !isRinging(r.st));
r = run(active, { type: "depart", goodbye: true, remaining: 0, now: 61000 }, { type: "hangup", now: 62000 });
check("the hang-up that follows an ended call writes nothing twice", r.fx.length === 0);

r = step(idle(), { type: "join-existing", now: 7, group: false });
check("joining a room someone is in is a call, clock started", r.st.phase === "active" && r.st.answeredAt === 7 && has(r.fx, (f) => f.clock === "start"));
r = step(active, { type: "dial", now: 5 });
check("dial does not restart a call in progress", r.st.phase === "active");

// ---- callee ----
r = step(idle(), { type: "ring", now: 100, caller: "ada" });
check("a ring starts the ringtone", r.st.phase === "incoming" && has(r.fx, (f) => f.ring === true));
const inc = r.st;
r = step(inc, { type: "ring", now: 200, caller: "ada" });
check("a second heartbeat does not restart the ring clock", r.st.since === 100 && r.fx.length === 0);
r = step(inc, { type: "tick", now: 100 + RING_MS - 1 });
check("callee still ringing just before the timeout", r.st.phase === "incoming");
r = step(inc, { type: "tick", now: 100 + RING_MS });
check("callee ring times out on the same clock", r.st.phase === "ended" && r.st.outcome === "missed" && has(r.fx, (f) => f.ring === false) && !has(r.fx, (f) => f.local));
r = run(inc, { type: "tick", now: 100 + RING_MS }, { type: "ring", now: 100 + RING_MS + 3000, caller: "ada" });
check("an old caller still sitting in the room does not re-ring after the timeout", r.st.phase === "ended" && r.fx.length === 0);
r = step(inc, { type: "decline" });
check("decline stops the ring and tells the caller", r.st.phase === "ended" && r.st.outcome === "declined" && has(r.fx, (f) => f.ring === false) && has(r.fx, (f) => f.sendDecline));
r = step(inc, { type: "accept" });
check("accept stops the ring", r.st.phase === "idle" && has(r.fx, (f) => f.ring === false));
r = step(inc, { type: "gone" });
check("caller gave up: ring stops, no local record in a 1:1", r.st.phase === "idle" && has(r.fx, (f) => f.ring === false) && !has(r.fx, (f) => f.local));
r = run(inc, { type: "decline" }, { type: "gone" });
check("the room emptying after a decline resets for the next call", r.st.phase === "idle" && r.fx.length === 0);

const gInc = step(idle(), { type: "ring", now: 0, caller: "ada", group: true }).st;
r = run(gInc, { type: "seen-answer" }, { type: "gone" });
check("group: somebody else answered, we never did → our own Missed call line", has(r.fx, (f) => f.local === "missed:ada"));
r = step(gInc, { type: "gone" });
check("group: nobody answered → the caller's shared line covers it", !has(r.fx, (f) => f.local));
r = run(gInc, { type: "seen-answer" }, { type: "tick", now: RING_MS });
check("group: ring timed out while the others talk → Missed call line", has(r.fx, (f) => f.local === "missed:ada"));
r = step(inc, { type: "reset" });
check("reset of a ringing call silences it", r.st.phase === "idle" && has(r.fx, (f) => f.ring === false));

// ---- records ----
check("caller's missed", recordText({ kind: "call-missed", content: "Missed call", mine: true, other: "Ben" }) === "You called — no answer");
check("callee's missed", recordText({ kind: "call-missed", content: "Missed call", mine: false, other: "Ada" }) === "Missed call from Ada");
check("caller's declined", recordText({ kind: "call-missed", content: DECLINED, mine: true, other: "Ben" }) === "Ben declined");
check("callee's declined", recordText({ kind: "call-missed", content: DECLINED, mine: false, other: "Ada" }) === "You declined Ada's call");
check("ended", recordText({ kind: "call", content: "ended:125", mine: true }) === "Call ended · 2m");
check("group missed", recordText({ kind: "call", content: "missed:f1", mine: true, nameOf: () => "Ada" }) === "Missed call from Ada");
check("lengths", callLength(45) === "45s" && callLength(60) === "1m" && callLength(3900) === "1h 5m" && callLength(7200) === "2h");

// ---- DM list preview (mirrors internal/app/snippet.go) ----
check("preview: caller's missed", callPreview("call-missed", "Missed call", true) === "You called — no answer");
check("preview: callee's missed", callPreview("call-missed", "Missed call", false) === "Missed call");
check("preview: declined", callPreview("call-missed", DECLINED, true) === "Call declined" && callPreview("call-missed", DECLINED, false) === "You declined a call");
check("preview: ended", callPreview("call", "ended:125", true) === "Call ended · 2m");
check("preview: not a call", callPreview("", "hi", true) === "");

// ---- decline wire ----
check("decline round-trips", parseDecline(declineBlob("ch1")) === "ch1");
check("decline carries no channelId (older meshes would open a connection for it)", !("channelId" in JSON.parse(declineBlob("ch1"))));
check("an ordinary signal is not a decline", parseDecline(JSON.stringify({ channelId: "ch1", description: {} })) === "");
check("garbage is not a decline", parseDecline("{") === "");

// ---- live badge ----
const sh = sharersOf([
  { peerId: "p1", kind: "camera" },
  { peerId: "p2", kind: "screen" },
  { peerId: "me", kind: "screen", self: true },
  { peerId: "p3" },
]);
check("only a remote screen share is LIVE", sh.p2 === true && !sh.p1 && !sh.me && !sh.p3);

// ---- ghost tiles ----
let g = ghostsToReap(["a", "b"], { a: {} }, {}, 1000);
check("an unlisted peer gets a grace first", g.reap.length === 0 && g.firstSeen.b === 1000);
g = ghostsToReap(["a", "b"], { a: {} }, g.firstSeen, 1000 + GHOST_MS);
check("…and is reaped after it", g.reap.length === 1 && g.reap[0] === "b");
g = ghostsToReap(["b"], { b: {} }, { b: 0 }, 99999);
check("a peer that turns up in presence is forgotten, not reaped", g.reap.length === 0 && !("b" in g.firstSeen));

console.log(failures ? `callflow.test.mjs: ${failures} failure(s)` : "callflow.test.mjs: OK");
process.exit(failures ? 1 : 0);
