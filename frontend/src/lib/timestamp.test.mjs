// Run with: node lib/timestamp.test.mjs
import { TS_RE, stampTimestamp, sealedAt, stripTimestamp, sealAgo, sealShort } from "./timestamp.js";

let fails = 0;
const ok = (cond, msg) => { if (!cond) { console.error("FAIL:", msg); fails++; } };

const NOW = 1_800_000_000_000; // fixed epoch ms

// --- sealing ---
const c = stampTimestamp("hello", NOW);
ok(c.startsWith("[ts](concord://ts/v1/1800000000)"), "seal is prepended in the token shape");
ok(stripTimestamp(c) === "hello", "stripping leaves the original content");
ok(sealedAt(c) === NOW, "the sealed instant round-trips");

// Never stack: sealing an already-sealed message is a no-op.
ok(stampTimestamp(c, NOW + 60000) === c, "a second seal does not stack");

// Empty content stays empty (the composer must not send a bare token).
ok(stampTimestamp("", NOW) === "", "empty content is not sealed");

// --- reading ---
ok(sealedAt("no token here") === 0, "unsealed content reads as 0");
ok(sealedAt("[ts](concord://ts/v1/1)") === 0, "an implausibly old seal is rejected");
ok(sealedAt("[ts](concord://ts/v1/999999999999)") === 0, "a far-future seal is rejected");
ok(stripTimestamp("plain") === "plain", "stripping is safe on unsealed content");

// The seal must survive content that merely looks similar.
ok(sealedAt("see concord://ts/v1/1800000000 inline") === 0, "a bare URL is not a seal");

// --- relative wording ---
ok(sealAgo(NOW, NOW) === "just now", "a fresh seal reads as just now");
ok(sealAgo(NOW - 45_000, NOW) === "45s ago", "seconds");
ok(sealAgo(NOW - 300_000, NOW) === "5m ago", "minutes");
ok(sealAgo(NOW - 7_200_000, NOW) === "2h ago", "hours");
ok(sealAgo(NOW - 86_400_000, NOW) === "yesterday", "a day reads as yesterday");
ok(sealAgo(NOW + 5_000, NOW) === "just now", "a clock skewed into the future does not go negative");

// --- formatting doesn't throw on junk ---
ok(sealShort(NOW).length > 0, "short label renders");
ok(sealShort(NaN) === "" || typeof sealShort(NaN) === "string", "a bad instant does not throw");

console.log(fails ? `timestamp.js: ${fails} FAILED` : "timestamp.js: all tests passed");
process.exit(fails ? 1 : 0);
