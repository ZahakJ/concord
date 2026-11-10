// What the command palette finds, and in what order (`npm test` runs it).
import { parseQuery, buildDests, recentFirst, rank, inMode } from "./switcher.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

const guilds = [
  { id: "g1", name: "Keyboarders", channels: [
    { id: "gen", name: "general", type: "text" },
    { id: "rnd", name: "random", type: "text" },
    { id: "lng", name: "Lounge", type: "voice" },
  ] },
  { id: "d1", kind: "dm", name: "Bashir", dmPeer: "FPR-B", channels: [{ id: "dmb", name: "dm", type: "text" }] },
];
const people = [
  { fingerprint: "FPR-B", name: "Bashir" },
  { fingerprint: "FPR-C", name: "Chen" },
  { fingerprint: "FPR-ME", name: "Alice" },
];
const dests = buildDests(guilds, people, "FPR-ME");

// prefixes
assert(parseQuery("#gen").mode === "channels" && parseQuery("#gen").q === "gen", "# is channels");
assert(parseQuery("@ch").mode === "people", "@ is people");
assert(parseQuery("!lo").mode === "voice", "! is voice");
assert(parseQuery("*key").mode === "guilds", "* is guilds");
assert(parseQuery(">mute").mode === "actions", "> is actions");
assert(parseQuery("rnd").mode === "all", "no prefix: everything");

// people
assert(dests.some((d) => d.kind === "person" && d.label === "Chen"), "a member you have no DM with is findable");
assert(!dests.some((d) => d.kind === "person" && d.label === "Bashir"), "someone you already DM appears once, as the DM");
assert(!dests.some((d) => d.label === "Alice" && d.kind === "person"), "you are not offered to yourself");
const chen = rank(dests.filter((d) => inMode(d, "people")), "chen", 8);
assert(chen[0]?.label === "Chen", "@chen finds Chen");
const gen = rank(dests.filter((d) => inMode(d, "channels")), "gen", 8);
assert(gen.length === 1 && gen[0].label === "general", "#gen finds #general and nothing else");
assert(rank(dests.filter((d) => inMode(d, "channels")), "lounge", 8).length === 0, "# does not list voice channels");
assert(rank(dests.filter((d) => inMode(d, "voice")), "lo", 8)[0]?.label === "Lounge", "!lo finds the voice channel");

// recents
const r = recentFirst(dests, ["dmb", "rnd", "gen"], "gen");
assert(r[0]?.kind === "dm" && r[0].label === "Bashir", "the DM you just visited comes first");
assert(r[1]?.label === "random", "then the channel before it");
assert(!r.some((d) => d.channelId === "gen"), "never the conversation you are in");
const fresh = recentFirst(dests, [], "gen");
assert(fresh.length && fresh.every((d) => d.kind === "channel"), "no trail: the channel list");

if (failures) {
  console.error(`${failures} switcher test(s) failed`);
  process.exit(1);
}
console.log("switcher: all tests passed");
