// Zero-dependency test for the Android notifier's policy sync (`npm test` runs it).
import { createPolicySync } from "./nativepolicy.js";

let failures = 0;
const assert = (cond, msg) => {
  if (!cond) {
    console.error("FAIL:", msg);
    failures++;
  }
};

const muted = { channels: { d1: "none" }, guilds: {} };
const dmGuild = { id: "g1", kind: "dm", channels: [{ id: "d1" }] };
const me = (presence = "") => ({ fingerprint: "AAAA", presence });

// G1: nothing is sent before the page knows the answer.
{
  const sent = [];
  const st = { guilds: [], notifs: muted, identity: null };
  const sync = createPolicySync({ read: () => st, send: (p) => sent.push(p) });
  sync.push(); // the page hidden a second into a cold start
  assert(sent.length === 0, "a push before anything has loaded must not overwrite the stored policy");
  st.identity = me();
  sync.push();
  assert(sent.length === 0, "nor one with the identity but no guild list yet");
  st.guilds = [dmGuild];
  sync.guildsLoaded();
  assert(sent.length === 1 && sent[0].quiet.includes("d1"), "once both have loaded, the real decision goes out");
}
{
  const sent = [];
  const st = { guilds: [dmGuild], notifs: muted, identity: null };
  const sync = createPolicySync({ read: () => st, send: (p) => sent.push(p) });
  sync.guildsLoaded();
  assert(sent.length === 0, "guilds without an identity (still locked) send nothing");
}

// G2: a change of status is sent when it happens, once.
{
  const sent = [];
  const st = { guilds: [dmGuild], notifs: muted, identity: me() };
  const sync = createPolicySync({ read: () => st, send: (p) => sent.push(p) });
  sync.guildsLoaded();
  sync.push();
  assert(sent.length === 1, "an unchanged decision is sent once");
  st.identity = me("dnd"); // set on another device while this one is in the background
  sync.identityChanged();
  assert(sent.length === 2 && sent[1].dnd === true, "Do Not Disturb reaches the native side when it changes");
}

if (failures) {
  console.error(`\n${failures} nativepolicy test(s) failed`);
  process.exit(1);
}
console.log("nativepolicy.js: all tests passed");
