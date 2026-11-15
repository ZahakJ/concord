import assert from "node:assert/strict";
import { seatBannerLines } from "./seatbanner.js";

const name = (f) => ({ A: "Alice", M: "Mo", O: "Olu" })[f] || f;
const g = "Den";

// Nothing flagged, nothing said.
assert.deepEqual(seatBannerLines({ guildName: g, name }), []);

// A removal done for this account names who added it.
{
  const [l] = seatBannerLines({ guildName: g, name, notices: [{ kind: "removed", adder: "M", at: 1 }] });
  assert.equal(l.text, "A device that is not yours was added to Den under your identity by Mo. Concord removed it. Your own devices are not affected.");
  assert.equal(l.action, undefined);
}

// Provable, seen by an admin: the removal is offered, with its confirmation.
{
  const [l] = seatBannerLines({ guildName: g, name, canManage: true, flags: [{ index: 3, key: "ab", kind: "not_theirs", account: "O", adder: "M", provable: true }] });
  assert.equal(l.text, "A device in Den uses Olu's identity but is not one of their devices. It was added by Mo. It can read what is sent here until it is removed.");
  assert.equal(l.action.label, "Remove this device");
  assert.equal(l.action.body, "Remove this device from Den? Olu's own devices stay.");
  assert.equal(l.action.index, 3);
  assert.equal(l.action.deviceKey, "ab");
}

// The same leaf seen by a member who cannot remove it: first sentence, no action.
{
  const [l] = seatBannerLines({ guildName: g, name, flags: [{ index: 3, key: "ab", kind: "not_theirs", account: "O", adder: "M", provable: true }] });
  assert.equal(l.text, "A device in Den uses Olu's identity but is not one of their devices.");
  assert.equal(l.action, undefined);
}

// Two leaves of one identity that this device cannot tell apart: never an action.
{
  const [l] = seatBannerLines({ guildName: g, name, canManage: true, flags: [{ index: 4, key: "cd", kind: "ambiguous", account: "O", provable: false }] });
  assert.equal(l.action, undefined);
  assert.match(l.text, /^Two devices in Den both claim to be Olu's first device, and only one can be\./);
}

// This account's own identity, on a device that cannot commit.
{
  const [l] = seatBannerLines({ guildName: g, name, flags: [{ index: 2, key: "ef", kind: "not_theirs", account: "A", adder: "M", own: true, provable: true }] });
  assert.equal(l.text, "A device that is not yours is in Den under your identity. It was added by Mo. Only an admin can remove it.");
  assert.equal(l.action, undefined);
}

console.log("seatbanner: ok");
