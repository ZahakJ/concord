// nativepolicy.js — keeping the Android notifier's copy of the page's decision.
//
// While the page is in the background or gone, the native side posts direct
// messages on its own and follows the decision it was last sent (lib/notifs.js
// nativePolicy): Do Not Disturb, and the DMs to leave alone. Two rules keep
// that copy right:
//
//   • nothing is sent before the page knows the answer. Until the guild list
//     has loaded once and the identity is known, the policy would read "no DND,
//     nothing quiet", and sending that overwrote a correct stored copy: a cold
//     start left within a second un-muted every muted DM until the next hide;
//   • a change is sent when it happens, including a change of status that
//     arrives while the page is already in the background (another device of
//     the account setting Do Not Disturb), not only at the next hide.
//
// Identical decisions are sent once.

import { nativePolicy } from "./notifs.js";

// read() → { guilds, notifs, identity }; send(policy) delivers it.
export function createPolicySync({ read, send }) {
  let guildsLoaded = false;
  let last = "";
  function push() {
    const { guilds = [], notifs, identity } = read();
    if (!guildsLoaded || !identity?.fingerprint) return false;
    const dms = guilds
      .filter((g) => g.kind === "dm")
      .flatMap((g) => (g.channels || []).map((c) => ({ channelId: c.id, guildId: g.id })));
    const policy = nativePolicy({ notifs, dms, dnd: identity?.presence === "dnd" });
    const key = JSON.stringify(policy);
    if (key === last) return false;
    last = key;
    send(policy);
    return true;
  }
  return {
    // A level changed, or the page is going to the background.
    push,
    // The guild list has loaded (successfully) at least once.
    guildsLoaded() {
      guildsLoaded = true;
      return push();
    },
    // The identity changed: a new status, possibly from another device.
    identityChanged: push,
  };
}
