import { typistsAfter } from "./typing.js";
let failures = 0;
const check = (name, ok) => { if (!ok) { failures++; console.log("  FAIL", name); } };
const list = [{ from: "lin", label: "Lin" }, { from: "ada", label: "Ada" }];
const msg = (o) => ({ channelId: "c1", sender: "lin", ...o });

let r = typistsAfter(list, msg({}), "c1", true);
check("the sender's line ends when their message lands", r.keep.length === 1 && r.keep[0].from === "ada" && r.gone.length === 1);
r = typistsAfter(list, msg({}), "c1", false);
check("an edit or reaction re-emit leaves the line alone", r.keep === list);
r = typistsAfter(list, msg({ backfill: true }), "c1", true);
check("history arriving by sync leaves the line alone", r.keep === list);
r = typistsAfter(list, msg({ channelId: "c2" }), "c1", true);
check("a message in another channel leaves the line alone", r.keep === list);
r = typistsAfter(list, msg({ sender: "noor" }), "c1", true);
check("someone else's message leaves the line alone", r.keep === list);

console.log(failures ? `typing.test.mjs: ${failures} failure(s)` : "typing.test.mjs: OK");
process.exit(failures ? 1 : 0);
