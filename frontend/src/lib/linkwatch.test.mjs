import { newlyLinked } from "./linkwatch.js";

let failures = 0;
const check = (what, ok) => {
  if (!ok) {
    console.error(`  FAIL ${what}`);
    failures++;
  }
};
const me = { key: "a", thisOne: true };
const laptop = { key: "b", name: "Laptop" };
check("nothing new", newlyLinked([me, laptop], [me, laptop]) === null);
check("a new device", newlyLinked([me], [me, laptop])?.name === "Laptop");
check("this device never counts", newlyLinked([], [me]) === null);
check("an unlinked one is not news", newlyLinked([me], [me, { key: "c", revokedAt: 5 }]) === null);
console.log(failures === 0 ? "linkwatch.test.mjs: OK" : `linkwatch.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
