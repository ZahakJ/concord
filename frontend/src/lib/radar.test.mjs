// Run with: node lib/radar.test.mjs
import {
  eventSig,
  goneLive,
  liveDestination,
  unseenCount,
  snapshot,
  pruneFired,
} from "./radar.js";

let fails = 0;
const ok = (cond, msg) => { if (!cond) { console.error("FAIL:", msg); fails++; } };

const NOW = 1_800_000_000; // fixed epoch seconds

// --- eventSig: nudge-worthy changes only ---
const base = { id: "e1", startUnix: NOW, endUnix: NOW + 3600, location: "lounge", locationChannelId: "c1" };
ok(eventSig(base) === eventSig({ ...base, rsvps: { a: "going" }, updatedAt: 99 }), "RSVPs don't change the sig");
ok(eventSig(base) !== eventSig({ ...base, startUnix: NOW + 60 }), "moved start changes the sig");
ok(eventSig(base) !== eventSig({ ...base, endUnix: NOW + 7200 }), "moved end changes the sig");
ok(eventSig(base) !== eventSig({ ...base, locationChannelId: "c2" }), "moved channel changes the sig");
ok(eventSig(base) !== eventSig({ ...base, location: "roof" }), "moved free-text place changes the sig");
ok(eventSig(base) === eventSig({ ...base, title: "renamed", details: "x" }), "title/details edits stay quiet");

// --- goneLive: the grace window around start ---
ok(goneLive({ startUnix: NOW - 10, endUnix: NOW + 600 }, NOW), "10s after start is a go-live");
ok(goneLive({ startUnix: NOW, endUnix: NOW + 600 }, NOW), "the start instant is a go-live");
ok(!goneLive({ startUnix: NOW + 5, endUnix: NOW + 600 }, NOW), "not before start");
ok(!goneLive({ startUnix: NOW - 600, endUnix: NOW + 600 }, NOW), "grace expired at exactly +10m");
ok(goneLive({ startUnix: NOW - 599, endUnix: NOW + 600 }, NOW), "just inside the grace");
ok(!goneLive({ startUnix: NOW - 30, endUnix: NOW - 1 }, NOW), "already over never fires");
ok(goneLive({ startUnix: NOW - 30 }, NOW), "open-ended events count as live");
ok(goneLive({ startUnix: NOW - 1200, endUnix: NOW + 600 }, NOW, 3600), "custom grace widens the window");

// --- liveDestination: only a real channel of the SAME guild is a door ---
const g = { id: "g1", channels: [{ id: "c1", name: "lounge", type: "voice" }] };
ok(liveDestination({ locationChannelId: "c1" }, g)?.name === "lounge", "resolves its own channel");
ok(liveDestination({ locationChannelId: "cX" }, g) === null, "vanished/foreign channel is not a door");
ok(liveDestination({ location: "Blue room" }, g) === null, "free-text events never trigger");
ok(liveDestination({ locationChannelId: "c1" }, null) === null, "no guild, no door");

// --- unseenCount: the watermark model ---
const me = "MYFPR";
const mk = (id, over = {}) => ({
  id, startUnix: NOW + 3600, endUnix: NOW + 7200, createdBy: "PEER", createdAt: NOW - 100, ...over,
});
const entry = snapshot([mk("a"), mk("b")], NOW - 50);
ok(unseenCount([mk("a"), mk("b")], entry, me, NOW) === 0, "nothing new after a fresh snapshot");
ok(unseenCount([mk("a"), mk("b"), mk("c", { createdAt: NOW - 10 })], entry, me, NOW) === 1, "a new event by a peer counts");
ok(unseenCount([mk("a"), mk("b"), mk("c", { createdAt: NOW - 10, createdBy: me })], entry, me, NOW) === 0, "your own event never counts");
ok(unseenCount([mk("a", { startUnix: NOW + 9000 }), mk("b")], entry, me, NOW) === 1, "a rescheduled event counts as unseen");
ok(unseenCount([mk("a", { rsvps: { x: "going" } }), mk("b")], entry, me, NOW) === 0, "an RSVP is not a change");
ok(unseenCount([mk("c", { createdAt: NOW - 10, startUnix: NOW - 8000, endUnix: NOW - 7000 })], entry, me, NOW) === 0, "an already-ended event is not news");
ok(unseenCount([mk("c", { createdAt: NOW - 999 })], entry, me, NOW) === 0, "absent from sigs but older than the watermark stays quiet");
ok(unseenCount([mk("c")], null, me, NOW) === 0, "no watermark yet counts nothing");

// --- pruneFired: markers age out ---
const nowMs = NOW * 1000;
const pruned = pruneFired({ fresh: nowMs - 1000, stale: nowMs - 8 * 86400000, junk: "x" }, nowMs);
ok("fresh" in pruned, "recent markers survive");
ok(!("stale" in pruned), "week-old markers are dropped");
ok(!("junk" in pruned), "non-numeric garbage is dropped");

if (fails) {
  console.error(`${fails} failure(s)`);
  process.exit(1);
}
console.log("radar.test.mjs: all tests passed");
