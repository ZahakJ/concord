// Custom emoji lookups answer only for the guild's own emoji. (`npm test`.)
import { customEmojiTable, reactionImage } from "./customemoji.js";

let failures = 0;
const ok = (c, m) => {
  if (!c) {
    console.error("  FAIL " + m);
    failures++;
  }
};
const t = customEmojiTable([{ name: "party", image: "data:image/png;base64,AAAA" }]);
ok(reactionImage(t, ":party:") === "data:image/png;base64,AAAA", "a real custom emoji reaction");
ok(reactionImage(t, "👍") === null && reactionImage(t, ":nope:") === null, "others are plain");
for (const n of ["constructor", "__proto__", "valueof", "tostring", "hasownproperty", "__definegetter__"]) {
  ok(t[n] === undefined, `the table answers for "${n}"`);
  ok(reactionImage(t, `:${n}:`) === null, `reaction ":${n}:" must not find an image`);
  ok(reactionImage({}, `:${n}:`) === null, `reaction ":${n}:" on a plain table must not find an image`);
}
if (failures) {
  console.error(`customemoji: ${failures} failure(s)`);
  process.exit(1);
}
console.log("customemoji: ok");
