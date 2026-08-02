// Run with: node lib/eventtime.test.mjs
import { happeningNow, isPast, eventPhase, fmtCountdown } from "./eventtime.js";

let fails = 0;
const ok = (cond, msg) => { if (!cond) { console.error("FAIL:", msg); fails++; } };

const NOW = 1_800_000_000; // fixed epoch seconds

// --- happeningNow / isPast (the existing contract, now living here) ---
ok(happeningNow({ startUnix: NOW - 60, endUnix: NOW + 60 }, NOW), "mid-event is now");
ok(!happeningNow({ startUnix: NOW + 60, endUnix: NOW + 120 }, NOW), "future is not now");
ok(happeningNow({ startUnix: NOW - 100 }, NOW), "open-ended counts as now for an hour");
ok(!happeningNow({ startUnix: NOW - 3601 }, NOW), "open-ended expires after an hour");
ok(isPast({ startUnix: NOW - 200, endUnix: NOW - 100 }, NOW), "ended is past");
ok(!isPast({ startUnix: NOW - 60, endUnix: NOW + 60 }, NOW), "live is not past");
ok(isPast({ startUnix: NOW - 3600 }, NOW), "open-ended is past exactly at +1h");

// --- eventPhase: the four temperatures ---
ok(eventPhase({ startUnix: NOW + 7200 }, NOW) === "upcoming", ">60m out is upcoming");
ok(eventPhase({ startUnix: NOW + 3601 }, NOW) === "upcoming", "3601s out is still upcoming");
ok(eventPhase({ startUnix: NOW + 3600 }, NOW) === "soon", "exactly T-60m is soon");
ok(eventPhase({ startUnix: NOW + 60 }, NOW) === "soon", "one minute out is soon");
ok(eventPhase({ startUnix: NOW, endUnix: NOW + 600 }, NOW) === "live", "start instant is live");
ok(eventPhase({ startUnix: NOW - 60, endUnix: NOW + 60 }, NOW) === "live", "mid-event is live");
ok(eventPhase({ startUnix: NOW - 200, endUnix: NOW - 100 }, NOW) === "ended", "over is ended");
ok(eventPhase({ startUnix: NOW - 3600 }, NOW) === "ended", "open-ended ends after an hour");

// --- fmtCountdown: minute-grained, never negative, never seconds ---
ok(fmtCountdown(NOW + 720, NOW) === "Starts in 12 min", "12 minutes out");
ok(fmtCountdown(NOW + 61, NOW) === "Starts in 2 min", "partial minutes round up (61s → 2 min)");
ok(fmtCountdown(NOW + 60, NOW) === "Starts in 1 min", "exactly one minute");
ok(fmtCountdown(NOW + 30, NOW) === "Starts in 1 min", "sub-minute still says 1 min");
ok(fmtCountdown(NOW, NOW) === "Starting now", "the start instant");
ok(fmtCountdown(NOW - 5, NOW) === "Starting now", "a hair late never goes negative");
ok(fmtCountdown(NOW + 3600, NOW) === "Starts in 60 min", "top of the soon window");

if (fails) { console.error(fails + " failure(s)"); process.exit(1); }
console.log("eventtime.test.mjs: all good");
