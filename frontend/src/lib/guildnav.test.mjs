// The guild hub table, held in place (`npm test` runs it).
//
// Same three silent failures as settingsnav.test.mjs: a kind App.svelte cannot
// load, an icon Icon.svelte has not got, two entries claiming one kind. Plus
// the trail rule that is this table's whole reason for existing — Events from
// the header, Roles from the member panel, Stats from a shortcut must stay
// ordinary dialogs; only a panel reached through the hub wears the rail.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  GUILD_GROUPS,
  GUILD_ITEMS,
  guildItem,
  inGuildHub,
  guildRailFor,
  onGuildTrail,
  guildField,
  guildGroupsFor,
} from "./guildnav.js";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
let failures = 0;
const fail = (msg) => {
  console.error("  FAIL " + msg);
  failures++;
};
const check = (name, ok) => {
  if (!ok) fail(name);
};

check("there are groups", GUILD_GROUPS.length > 0);
for (const g of GUILD_GROUPS) {
  if (!g.label) fail("a group with no label");
  if (!g.items?.length) fail(`group "${g.label}" has no items`);
  for (const it of g.items || []) {
    if (!it.kind) fail(`an item in "${g.label}" has no kind`);
    if (!it.title) fail(`${it.kind} has no title`);
    if (!it.icon) fail(`${it.kind} has no icon`);
    if (!it.sub) fail(`${it.kind} has no sub`);
  }
}

{
  const seen = new Set();
  for (const it of GUILD_ITEMS) {
    if (seen.has(it.kind)) fail(`${it.kind} appears twice — two rail rows would light up`);
    seen.add(it.kind);
  }
}

{
  const app = fs.readFileSync(path.join(SRC, "App.svelte"), "utf8");
  const block = /const MODAL_LOADERS\s*=\s*\{([\s\S]*?)\n\s*\};/.exec(app);
  if (!block) fail("cannot find MODAL_LOADERS in App.svelte");
  else {
    const kinds = new Set([...block[1].matchAll(/^\s*([A-Za-z][A-Za-z0-9]*):/gm)].map((m) => m[1]));
    for (const it of GUILD_ITEMS) {
      if (!kinds.has(it.kind)) fail(`${it.kind} is not a modal App.svelte can load`);
    }
  }
}

{
  const icon = fs.readFileSync(path.join(SRC, "Icon.svelte"), "utf8");
  const names = new Set([...icon.matchAll(/^\s{4}([A-Za-z][A-Za-z0-9]*):/gm)].map((m) => m[1]));
  const owner = { isOwner: true, canManage: true, myPerms: 0xffffffff };
  for (const it of GUILD_ITEMS) {
    const name = guildField(it.icon, owner);
    if (!names.has(name)) fail(`${it.kind} asks for the icon "${name}", which Icon.svelte has not got`);
  }
}

check("guildItem finds Overview", guildItem("guildSettings")?.title === "Overview");
check("guildItem returns null for a stranger", guildItem("appearance") === null);
check("inGuildHub knows Members", inGuildHub("members"));
check("inGuildHub says no to a settings page", !inGuildHub("appearance"));

check(
  "a hub-stamped panel lights itself",
  guildRailFor({ kind: "members", hub: "g1" }, []) === "members",
);
check(
  "Events from the header, with no hub stamp, gets no rail",
  guildRailFor({ kind: "events" }, []) === "",
);
check(
  "…and a settings trail does not count either",
  guildRailFor({ kind: "stats" }, [{ kind: "settings" }]) === "",
);
check(
  "a drilled Import lights Archive, the page it came from",
  guildRailFor({ kind: "chronicleImport", from: "chronicle" }, [{ kind: "chronicle", hub: "g1" }]) ===
    "chronicle",
);
check("nothing open at all is not a rail", guildRailFor(null, []) === "");
check("onGuildTrail sees the stamp", onGuildTrail({ kind: "roles", hub: "g1" }, []) === true);
check("onGuildTrail ignores a plain Events dialog", onGuildTrail({ kind: "events" }, []) === false);

{
  const owner = { isOwner: true, canManage: true, myPerms: 0xffffffff, heir: "" };
  const member = { isOwner: false, canManage: false, myPerms: 0, heir: "" };
  const ownerKinds = guildGroupsFor(owner).flatMap((g) => g.items.map((i) => i.kind));
  const memberKinds = guildGroupsFor(member).flatMap((g) => g.items.map((i) => i.kind));
  check("the owner is offered Import", ownerKinds.includes("chronicleImport"));
  check("a plain member is not offered Import", !memberKinds.includes("chronicleImport"));
  check("a plain member is not offered Roles", !memberKinds.includes("roles"));
  check("a plain member still sees the moderation log", memberKinds.includes("modLog"));
  const memberSub = guildField(guildItem("members").sub, member);
  check(
    "a plain member is not promised the authority to moderate",
    memberSub === "See who is here",
  );
}

console.log(failures === 0 ? "guildnav.test.mjs: OK" : `guildnav.test.mjs: ${failures} failure(s)`);
process.exit(failures ? 1 : 0);
