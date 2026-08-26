// Run with: node lib/report.test.mjs
import { buildReport, reportFilename, REPORT_VERSION } from "./report.js";

let fails = 0;
const ok = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    fails++;
  }
};

const msg = {
  id: "m-7",
  channelId: "ch-2",
  sender: "AB12 CD34 EF56 GH78",
  senderName: "Mallory",
  content: "the thing they said",
  sent: "2026-08-25T11:02:03.456Z",
  edited: true,
};
const now = new Date("2026-08-25T12:00:00.000Z");

// --- the file is JSON, and survives the round trip ---
// It exists to be handed to someone else, quite possibly opened by a tool
// nobody here wrote, so "it looked right in the console" is not the bar.
{
  const text = JSON.stringify(buildReport({ message: msg, reporter: "ME", now }), null, 2);
  let parsed;
  try {
    parsed = JSON.parse(text);
  } catch (e) {
    ok(false, `the exported evidence is not valid JSON: ${e.message}`);
  }
  ok(parsed?.concordReport === REPORT_VERSION, "the file names its own format version");
  ok(parsed?.message?.id === "m-7", "the message ID survives the round trip");
  ok(parsed?.message?.content === "the thing they said", "the content survives the round trip");
  ok(parsed?.exportedAt === "2026-08-25T12:00:00.000Z", "the export time is the time given, not 'now'");
  ok(parsed?.message?.sentAt === "2026-08-25T11:02:03.456Z", "the message's own timestamp is kept separate");
  ok(parsed?.message?.edited === true, "an edited message is recorded as edited");
}

// --- the fingerprint is the identity; the display name is only a claim ---
// Anyone can call themselves anything, so a record that carried the name and
// dropped the key would identify nobody.
{
  const r = buildReport({ message: msg, reporter: "ME", now });
  ok(r.message.senderFingerprint === "AB12 CD34 EF56 GH78", "the sender's safety number is recorded");
  ok(r.message.senderDisplayName === "Mallory", "the self-asserted name is recorded alongside it");
  ok(r.reportedBy === "ME", "the file says who wrote it");

  const anon = buildReport({ message: { ...msg, senderName: "" }, now });
  ok(anon.message.senderFingerprint === "AB12 CD34 EF56 GH78", "a nameless sender is still identified by key");
  ok(anon.message.senderDisplayName === "", "a missing name is empty, not undefined — JSON keeps the key");
  ok("senderDisplayName" in anon.message, "the field is present even when empty");
}

// --- context comes along, because a message alone proves nothing ---
{
  const r = buildReport({ message: msg, guildId: "g-1", guildName: "Some Guild", now });
  ok(r.guildId === "g-1" && r.guildName === "Some Guild", "the guild is recorded");
  ok(r.channelId === "ch-2", "the channel is recorded");

  // A DM has no guild. That must produce empty strings rather than the word
  // "undefined" appearing in a document somebody may rely on.
  const dm = buildReport({ message: msg, now });
  ok(dm.guildId === "" && dm.guildName === "", "a DM report carries empty guild fields, not undefined");
  ok(!JSON.stringify(dm).includes("undefined"), "no 'undefined' anywhere in the exported text");
}

// --- an empty message body is a real case, not a failure ---
// An attachment-only message has no text; exporting it must still work.
{
  const r = buildReport({ message: { id: "m-8", sender: "K", content: "" }, now });
  ok(r.message.content === "", "an attachment-only message exports with empty content");
  ok(r.message.sentAt === "", "a message with no timestamp exports an empty one rather than crashing");
}

// --- refusing to write a meaningless file ---
{
  let threw = false;
  try {
    buildReport({ message: null, now });
  } catch {
    threw = true;
  }
  ok(threw, "building a report with no message is an error, not an empty file");
}

// --- the filename is portable ---
// Colons are legal on Linux and rejected by Windows and Android; a file the
// user cannot save on the machine they need it on is not evidence.
{
  const name = reportFilename(now);
  ok(name.endsWith(".json"), "the export is named as JSON");
  ok(!/[:*?"<>|]/.test(name), `the filename avoids characters Windows rejects: ${name}`);
  ok(name.includes("2026-08-25"), "the filename carries the date, so a folder of them sorts");
  ok(reportFilename(new Date("2027-01-01T00:00:00Z")) !== name, "two exports do not collide on one name");
}

console.log(fails ? `report.js: ${fails} failure(s)` : "report.js: all tests passed");
process.exit(fails ? 1 : 0);
