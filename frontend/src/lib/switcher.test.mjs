// What the command palette finds, and in what order (`npm test` runs it).
import { parseQuery, buildDests, recentFirst, rank, inMode, fuzzy } from "./switcher.js";

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

// Highlights mark the characters that matched as the reader typed them.
assert(JSON.stringify(fuzzy("workshop", "op").idx) === "[6,7]", "a run beats scattered letters");
assert(JSON.stringify(fuzzy("Lantern Makers", "ma").idx) === "[8,9]", "a word start beats a mid-word run");
assert(JSON.stringify(fuzzy("brainstorm", "bstm").idx) === "[0,5,6,9]", "scattered letters still match");
assert(fuzzy("general", "general").score < fuzzy("general-chat", "general").score, "the whole name ranks first");
assert(fuzzy("help-desk", "desk").score < fuzzy("deskside", "desk").score + 1000, "word start found after a hyphen");
// Scattered letters stay in a word or jump to word starts — never mid-word elsewhere.
assert(fuzzy("Join with an invite code", "wo") === null, "wo must not match w(ith) + (c)o(de)");
assert(fuzzy("Can people reach me?", "app") === null, "app must not match across mid-word letters");
assert(JSON.stringify(fuzzy("Your calendar", "yc").idx) === "[0,5]", "initials across word starts match");
// A run beats scatter: with a run on offer, scattered matches are not listed.
{
  const r = rank([{ label: "workshop" }, { label: "wobbly-pots" }, { label: "wide orbit" }], "wo", 8).map((x) => x.label);
  assert(r.includes("workshop") && r.includes("wobbly-pots") && !r.includes("wide orbit"), `runs only when there are runs: ${r}`);
}
assert(fuzzy("abc", "x") === null, "no match is null");
console.log("switcher highlight ok");

// A forum post is listed as a post, with the forum glyph, not as a "#" channel.
{
  const ds = buildDests([{ id: "g1", name: "G", channels: [{ id: "f", name: "ideas", type: "forum" }, { id: "p", name: "Seven-point star", type: "text", parent: "f" }] }]);
  const post = ds.find((d) => d.channelId === "p");
  if (post.icon !== "forum" || !/^post in ideas/.test(post.sub)) {
    console.error("FAIL: a post is typed as a post", post.icon, post.sub);
    process.exitCode = 1;
  }
}
