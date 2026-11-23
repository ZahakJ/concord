// node src/lib/biometric.test.mjs — the page's side of fingerprint unlock.
import assert from "node:assert/strict";

const store = new Map([["concord.bioEnrolled", "1"]]);
globalThis.localStorage = {
  getItem: (k) => (store.has(k) ? store.get(k) : null),
  setItem: (k, v) => store.set(k, String(v)),
  removeItem: (k) => store.delete(k),
};
const calls = [];
let reply = { code: "ok" };
globalThis.window = {
  Capacitor: {
    Plugins: {
      ConcordCore: {
        bioState: async () => ({ available: true, enrolled: false }),
        bioEnrol: async (a) => (calls.push(["bioEnrol", a]), reply),
        bioUnlock: async (a) => (calls.push(["bioUnlock", a]), reply),
        bioDisable: async () => calls.push(["bioDisable"]),
      },
      // 0.63's plugin, if a stale shell still had it: never touched.
      NativeBiometric: {
        getCredentials: async () => {
          throw new Error("the old plugin was used");
        },
      },
    },
  },
};

const bio = await import("./biometric.js");

// The 0.63 flag is gone on load.
assert.equal(store.has("concord.bioEnrolled"), false);

assert.deepEqual(await bio.bioState(), { available: true, enrolled: false });

// Right after a passphrase unlock the page sends no passphrase at all.
assert.equal(await bio.enableBiometric(), "ok");
assert.deepEqual(calls.at(-1), ["bioEnrol", {}]);
assert.equal(await bio.enableBiometric("typed"), "ok");
assert.deepEqual(calls.at(-1), ["bioEnrol", { passphrase: "typed" }]);

// Unlocking hands back a code, never a secret.
reply = { code: "ok", password: "should never be read" };
assert.equal(await bio.unlockWithBiometric(), "ok");
reply = { code: "lockout" };
assert.equal(await bio.unlockWithBiometric(), "lockout");
reply = undefined;
assert.equal(await bio.unlockWithBiometric(), "failed");

for (const c of ["invalidated", "lockout", "no_slot", "unavailable", "failed"]) {
  assert.ok(bio.bioMessage(c).length > 0, c);
}
assert.equal(bio.bioMessage("cancelled"), "");
assert.equal(bio.bioMessage("ok"), "");

// Off the phone: nothing available, nothing attempted.
delete window.Capacitor.Plugins.ConcordCore;
assert.deepEqual(await bio.bioState(), { available: false, enrolled: false });
assert.equal(await bio.unlockWithBiometric(), "unavailable");
console.log("biometric.test: ok");
