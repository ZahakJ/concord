import { isEmote, emoteText } from "./emote.js";

let failures = 0;
const ok = (c, m) => { if (!c) { console.error("  FAIL " + m); failures++; } };

const NAME = "Amina Sadiq";
const line = emoteText(NAME, "waves at everyone");
ok(line === "*Amina Sadiq waves at everyone*", `emoteText: ${line}`);
ok(isEmote(line, NAME), "its own output is an emote");
ok(isEmote("  " + line + "  ", NAME), "leading and trailing space is ignored");

// The actor has to match. A relayed guest's line under a host's name must not
// be styled as the host acting.
ok(!isEmote(line, "Bilal Rahman"), "another person's name is not an emote");
ok(!isEmote(line, ""), "no sender name, no emote");
ok(!isEmote("", NAME), "empty body");
ok(!isEmote(null, NAME), "null body");

// Ordinary formatting must not be swallowed.
ok(!isEmote("*Amina Sadiq*", NAME), "the bare name alone is not an action");
ok(!isEmote("**Amina Sadiq waves**", NAME), "bold is not an emote");
ok(!isEmote("*Amina Sadiq waves** ", NAME), "a trailing double marker is not an emote");
ok(!isEmote("Amina Sadiq waves", NAME), "unwrapped text is not an emote");
ok(!isEmote("*waves at everyone*", NAME), "the old form, with no actor, no longer matches");
ok(!isEmote("*Amina Sadiqwaves*", NAME), "the name has to be a whole word");

if (failures) { console.error(`emote.test.mjs: ${failures} failure(s)`); process.exit(1); }
console.log("emote: all tests passed");
