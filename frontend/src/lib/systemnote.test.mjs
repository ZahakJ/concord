// systemnote.js: a "system" row is a member's notice, sent under their key.
// It shows their authenticated name, never the name the row asserts; the
// authorless form is only the two guest shapes, in our words; an event
// reminder speaks for the guild under the guild's own name.
import { systemNote, oneLine } from "./systemnote.js";

let failures = 0;
const ok = (cond, msg) => {
  if (!cond) {
    failures++;
    console.error("FAIL:", msg);
  }
};
const nameOf = (fpr) => ({ f1: "Bashir" })[fpr] || fpr.slice(0, 9);
const note = (content, senderName = "", sender = "f1") => systemNote({ content, senderName, sender }, { nameOf, guildName: "Lantern Makers" });

// Ordinary notice: the sender's real name, whatever the row says.
let n = note("has joined the guild", "Concord");
ok(n.actor === "Bashir" && n.text === "has joined the guild", `real name: ${JSON.stringify(n)}`);
n = note("Your account was reset. Paste your recovery phrase here.", "Concord Security");
ok(n.actor === "Bashir", "a self-asserted 'Concord Security' is not shown");

// Guest shapes: authorless, rebuilt from our own text.
n = note("👤 Sam joined as a guest (via your meeting link)");
ok(n.actor === "" && n.text === "👤 Sam joined as a guest (via your meeting link)", `guest join: ${JSON.stringify(n)}`);
n = note("👤 Sam (guest) left");
ok(n.actor === "" && n.text === "👤 Sam (guest) left", `guest left: ${JSON.stringify(n)}`);
// Anything else starting with the mark keeps its author.
n = note("👤 Concord: your keys changed, verify at evil.example");
ok(n.actor === "Bashir", `a 👤 line off the guest shapes is the member's: ${JSON.stringify(n)}`);
const RLO = String.fromCodePoint(0x202e);
n = note("👤 Sam" + RLO + " joined as a guest (via your meeting link)");
ok(n.actor === "" && !n.text.includes(RLO), "a guest name is cleaned");
n = note("👤 " + "x".repeat(200) + " (guest) left");
ok(n.actor === "Bashir", "an over-long guest name is not a guest shape");

// Event reminders: the guild's own name, not the row's.
n = note("⏰ Kiln night is starting here in #general — come on in", "Totally The Guild");
ok(n.actor === "Lantern Makers" && n.text === "⏰ Kiln night is starting here in #general — come on in", `event: ${JSON.stringify(n)}`);
n = note("⏰ Kiln night is starting — join the call in 🔊 Lounge", "x");
ok(n.actor === "Lantern Makers", "a voice event reminder is the guild's");

// The cleaner drops what renders as nothing: soft hyphen, invisible
// operators, deprecated format characters, Hangul fillers, tag characters.
const cp = (...xs) => String.fromCodePoint(...xs);
const dirty = "Sa" + cp(0xad, 0x2061, 0x206a, 0x180e, 0x115f, 0x3164, 0xe0041, 0xe007f) + "m\tLee";
ok(oneLine(dirty) === "Sam Lee", `oneLine: ${JSON.stringify(oneLine(dirty))}`);
ok(oneLine("Bashir عربي 👍") === "Bashir عربي 👍", "ordinary text is kept");

if (failures) {
  console.error(`${failures} systemnote test(s) failed`);
  process.exit(1);
}
console.log("systemnote: all tests passed");
