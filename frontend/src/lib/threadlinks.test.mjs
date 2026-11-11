// A thread opener's link back to its source message is read client-side, so
// the parent message can say it has a thread without anything new on the wire.
import { openerSource, threadsBySource } from "./threadlinks.js";

const fail = (msg) => {
  console.error("  FAIL", msg);
  process.exitCode = 1;
};

const opener = "**Mira** wrote:\n> what glue?\n\nconcord://msg/ch1/m42";
if (openerSource(opener, "ch1") !== "m42") fail("reads the source message id");
if (openerSource(opener, "ch2") !== "") fail("a link into another channel is not this channel's thread");
if (openerSource("just talk", "ch1") !== "") fail("no link, no source");

const msgs = {
  t1: [{ content: opener }],
  t2: [{ content: "a thread nobody started from a message" }],
  t3: [{ content: "", kind: "system" }, { content: "concord://msg/ch1/m42" }],
  t4: [{ content: "concord://msg/ch1/m7" }],
};
const threads = [
  { id: "t1", name: "Glue" },
  { id: "t2", name: "Loose" },
  { id: "t3", name: "More glue" },
  { id: "t4", name: "Seven" },
];
const map = await threadsBySource(threads, "ch1", async (id) => msgs[id]);
if ((map.get("m42") || []).map((t) => t.id).join() !== "t1,t3") fail(`two threads from one message: ${JSON.stringify(map.get("m42"))}`);
if ((map.get("m7") || []).length !== 1) fail("a third thread from another message");
if (map.size !== 2) fail("a thread with no link is not attached anywhere");

// Capped: only the first `cap` threads are read.
let reads = 0;
await threadsBySource(threads, "ch1", async (id) => (reads++, msgs[id]), 2);
if (reads !== 2) fail(`read ${reads} threads with a cap of 2`);

// A thread that cannot be read is skipped, not fatal.
const m2 = await threadsBySource([{ id: "x", name: "X" }, ...threads], "ch1", async (id) => {
  if (id === "x") throw new Error("locked");
  return msgs[id];
});
if (!m2.get("m42")) fail("one unreadable thread does not hide the rest");

if (!process.exitCode) console.log("threadlinks.test.mjs: ok");
