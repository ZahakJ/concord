// "Make admin" reuses the guild's Admin role instead of minting a duplicate
// (`npm test`).
import { adminRoleOf, permLabels, PERM, PERM_ALL } from "./perms.js";

let failures = 0;
const eq = (got, want, what) => {
  if (got !== want) {
    console.error(`FAIL: ${what}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
};

const named = { id: "r1", name: "Admin", perms: 63 };
const full = { id: "r2", name: "Boss", perms: PERM_ALL };
eq(adminRoleOf([named])?.id, "r1", "an existing role named Admin is reused");
eq(adminRoleOf([{ id: "r3", name: " admin ", perms: 1 }])?.id, "r3", "the name match ignores case and spaces");
eq(adminRoleOf([named, full])?.id, "r2", "a role with every permission wins");
eq(adminRoleOf([{ id: "r4", name: "Mods", perms: 1 }]), null, "no admin role yet");
eq(adminRoleOf(undefined), null, "no roles at all");
eq(permLabels(PERM.MANAGE_MEMBERS | PERM.SYNC_HOST).join(", "), "Manage members, Sync host", "labels in order");

if (failures) {
  console.error(`\n${failures} perms test(s) failed`);
  process.exit(1);
}
console.log("perms.test.mjs: all checks passed");
