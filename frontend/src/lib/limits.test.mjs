// Every guild-name field uses the one limit (`npm test`).
import { readFileSync } from "node:fs";
import { GUILD_NAME_MAX } from "./limits.js";

let failures = 0;
const here = new URL("..", import.meta.url);
for (const f of ["modals/ModalCreate.svelte", "modals/ModalGuildSettings.svelte"]) {
  const src = readFileSync(new URL(f, here), "utf8");
  if (!src.includes("maxlength={GUILD_NAME_MAX}")) {
    console.error(`FAIL: ${f} does not take its guild-name limit from lib/limits.js`);
    failures++;
  }
}
if (GUILD_NAME_MAX !== 48) {
  console.error("FAIL: the guild-name limit moved; existing 48-character names must stay editable");
  failures++;
}
if (failures) process.exit(1);
console.log("limits.test.mjs: all checks passed");
