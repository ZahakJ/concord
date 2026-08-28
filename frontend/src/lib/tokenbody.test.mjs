// Which bodies the inline editor refuses, and — just as important — which it
// must not. A rule that over-reaches here silently takes editing away from
// ordinary messages, which is a worse bug than the one it fixes.
import { bodyToken, uneditableReason, canEditBody } from "./tokenbody.js";

let failures = 0;
const fail = (msg) => {
  console.error("  FAIL: " + msg);
  failures++;
};
const refuses = (body, what) => {
  if (canEditBody(body)) fail(`${what}: should be refused, was editable`);
  else if (!uneditableReason(body)) fail(`${what}: refused with no reason to show`);
};
const allows = (body, what) => {
  if (!canEditBody(body)) fail(`${what}: should be editable, was refused ("${uneditableReason(body)}")`);
};

const POLL = "[poll](concord://poll/v1/eyJxIjoiV2hpY2g_In0)";
const GAME = "[game](concord://game/v1/eyJnIjoiYzQifQ)";
const DOODLE = "[doodle](concord://doodle/v1/AQIDBA)";
const SOUND = "[sound](concord://sfx/v1/AQIDBA)";
const ANNOUNCE = "[announcement](concord://announce/v1/eyJ0IjoiaGkifQ)";

// ---- the encoded bodies ----
for (const [body, name] of [
  [POLL, "poll"],
  [GAME, "game"],
  [DOODLE, "doodle"],
  [SOUND, "sound"],
  [ANNOUNCE, "announcement"],
]) {
  refuses(body, name);
}
if (!/delete and repost/i.test(uneditableReason(POLL))) fail("the poll hint must say what to do instead");

// ---- modifiers ride in front of ORDINARY text and must not change the answer ----
allows("[eph](concord://eph/v1/1756000000)just words", "a disappearing message");
allows("[ts](concord://ts/v1/1756000000)just words", "a sealed message");
allows("[fx](concord://fx/v1/confetti)just words", "a message with an effect");
allows("[eph](concord://eph/v1/1756000000)[ts](concord://ts/v1/1756000000)words", "both modifiers");
// …and must not smuggle a poll past the check either.
refuses("[eph](concord://eph/v1/1756000000)" + POLL, "a disappearing poll");
refuses("[ts](concord://ts/v1/1756000000)" + POLL, "a sealed poll");

// ---- mixed bodies are refused too; see the module header for why ----
refuses(POLL + " what do you think?", "a poll with prose after it");
refuses("what do you think? " + POLL, "a poll with prose before it");

// ---- what must stay editable ----
allows("just an ordinary message", "plain text");
allows("", "an empty body");
allows("look: https://concord.example/poll/v1/whatever", "a URL that merely mentions poll/v1");
allows("we should run a poll about this", "the word poll");
allows("```\nconcord://poll/v1/<payload>\n```", "a code block quoting the bare scheme");
// A code fence containing the WHOLE bracketed token is still refused, and that
// is right rather than an over-reach: the renderers match the token wherever it
// appears in the body, fence or no fence, so this message really does draw a
// poll card and really is one keystroke from corruption.
refuses("```\n" + POLL + "\n```", "a code block containing a whole poll token");
// Attachments are deliberately NOT refused — the token is a blob reference, not
// an encoded payload, there is no "repost it" story for bytes already sealed
// and shared, and the name/description it carries has its own editor.
allows("![image](concord://attach/v2/abc/def/png/200x120)", "an image");
allows("[file](concord://file/v1/abc/def/1024/x/y)", "a file");
allows("![image](concord://attach/v2/abc/def/png/200x120)\nwith a caption", "an image with a caption");

// bodyToken hands back the entry so a caller can tell them apart.
if (bodyToken(POLL)?.scheme !== "poll") fail("bodyToken should name the scheme it found");
if (bodyToken("hello") !== null) fail("bodyToken should be null for ordinary text");

if (failures) {
  console.error(`tokenbody: ${failures} failure(s)`);
  process.exit(1);
}
console.log("tokenbody: all tests passed");
