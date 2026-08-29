// The copy a user sees in a red row under their own words.
//
// `humanError` exists because errors arrive wearing the Go package that raised
// them, and the strip alone is not enough: api.js throws
// `rpc SendMessage: HTTP 500`, so the prefix came off and the TRANSPORT half
// survived verbatim — "HTTP 500", on screen, under somebody's message. The
// worklog's own comment uses that exact string as the example of what must
// never be shown.
import { humanError, slowModeWait } from "./errors.js";

let failures = 0;
const eq = (got, want, what) => {
  if (got !== want) {
    console.error(`  FAIL ${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
};

// The prefix, which already worked.
eq(humanError("app: they're already in this guild"), "they're already in this guild", "strips app:");
eq(humanError("store: net: nope"), "nope", "strips a stack of prefixes");

// What was left over after the strip.
eq(humanError("rpc SendMessage: HTTP 500"), "Concord's core rejected it — try again", "HTTP 500");
eq(humanError("HTTP 404"), "Concord's core rejected it — try again", "a bare status code");
eq(humanError("rpc Foo: HTTP 503 "), "Concord's core rejected it — try again", "trailing space");
// …and what must NOT be swallowed by that rule: a status code inside a real
// sentence is somebody explaining something.
eq(
  humanError("app: the invite server answered HTTP 500 twice"),
  "the invite server answered HTTP 500 twice",
  "a status code inside a sentence survives",
);

// Slow mode gets a present-tense sentence; the NUMBER is the row's job, because
// a number captured once is a falsehood ten seconds later.
eq(humanError("app: slow mode: wait 28s"), "Slow mode in this channel", "slow mode sentence");
eq(slowModeWait("app: slow mode: wait 28s"), 28, "reads the wait back out");
eq(slowModeWait("slow mode: wait 3s"), 3, "unprefixed too");
eq(slowModeWait("app: slow mode is on"), 0, "no number, no wait");
eq(slowModeWait("HTTP 500"), 0, "not a slow-mode error");
eq(slowModeWait(new Error("app: slow mode: wait 7s")), 7, "an Error object");

// The network cases, unchanged.
eq(humanError("Failed to fetch"), "Concord isn't responding — trying to reconnect", "offline");
eq(humanError({ offline: true, message: "anything" }), "Concord isn't responding — trying to reconnect", "offline flag");

if (failures) {
  console.error(`\n${failures} errors test(s) failed`);
  process.exit(1);
}
console.log("errors.test.mjs: all checks passed");
