// Zero-dependency test for openlink.js (`npm test` runs it).
import { conversationFromURL, isConversationLink } from "./openlink.js";

let failures = 0;
const eq = (a, b, msg) => {
  if (a !== b) {
    console.error("FAIL:", msg, JSON.stringify(a), "!==", JSON.stringify(b));
    failures++;
  }
};
const id = "0123456789abcdef0123456789abcdef";

eq(conversationFromURL(`concord://open?c=${id}`), id, "the open link");
eq(conversationFromURL(`concord://channel?id=${id}`), id, "the notification tap");
eq(conversationFromURL("concord://open?c=a_b-C9"), "a_b-C9", "the id alphabet");
for (const bad of [
  "concord://open?c=",
  `concord://open?c=${"a".repeat(65)}`,
  `concord://open?c=${id}&send=hi`,
  `concord://open?send=hi&c=${id}`,
  `concord://open?c=${id}#x`,
  "concord://open?c=a%2Fb",
  "concord://open?c=a/b",
  "concord://open?c=a.b",
  "concord://open?c=a b",
  `concord://open/?c=${id}`,
  `CONCORD://open?c=${id}`,
  `concord://open?C=${id}`,
  `concord://join?c=${id}`,
  `concord://link?c=${id}`,
  `https://open?c=${id}`,
  `concord://open?c=${id}\n`,
  null,
  undefined,
  42,
  `concord://open?c=${id}`.padEnd(201, "x"),
]) {
  eq(conversationFromURL(bad), "", `refused: ${String(bad).slice(0, 60)}`);
}
eq(isConversationLink(`concord://open?c=${id}&send=1`), true, "a malformed open link is still not a device link");
eq(isConversationLink("concord://link?c=CL1abc"), false, "the device link is not a conversation link");

if (failures) process.exit(1);
console.log("openlink: ok");
