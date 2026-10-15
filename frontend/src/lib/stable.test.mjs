// keepStable keeps an equal table's identity (`npm test` runs it). Identity is
// what the message rows compare: a fresh-but-equal table re-renders every
// visible body, so an unchanged member list must come back as the same object.
import { keepStable } from "./stable.js";

let failures = 0;
const check = (name, ok) => {
  if (!ok) {
    console.error("  FAIL " + name);
    failures++;
  }
};

const names = (members) => [
  { name: "everyone", self: true },
  { name: "here", self: true },
  ...members.map((m) => ({ name: m.name, self: m.isSelf })),
];
const stable = keepStable();
const a = stable(names([{ name: "ada", isSelf: true }, { name: "bo", isSelf: false }]));
// A presence refresh: a new members array, the same names.
const b = stable(names([{ name: "ada", isSelf: true }, { name: "bo", isSelf: false }]));
check("an equal rebuild returns the previous object", a === b);
const c = stable(names([{ name: "ada", isSelf: true }, { name: "bea", isSelf: false }]));
check("a rename returns a new object", c !== b);
check("the new object carries the change", c[3].name === "bea");
const d = stable(names([{ name: "ada", isSelf: true }, { name: "bea", isSelf: false }]));
check("and is then itself kept", d === c);

const emoji = keepStable();
const e1 = emoji({ party: "data:1" });
check("tables are independent", emoji({ party: "data:1" }) === e1 && stable(c) === c);
check("a changed image is a new emoji map", emoji({ party: "data:2" }) !== e1);

console.log(failures === 0 ? "stable.test.mjs: OK" : `stable.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
