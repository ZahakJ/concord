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
  assert.equal(l.text, "Two devices in Den use Olu's identity, and this device cannot tell which one is really Olu's. Olu's own device can tell, and removes the other if Olu may manage members.");
  const [m] = seatBannerLines({ guildName: g, name, flags: [{ index: 4, key: "cd", kind: "ambiguous", account: "O", provable: false }] });
  assert.equal(m.text, "Two devices in Den use Olu's identity, and this device cannot tell which one is really Olu's.");
}

// Every other leaf this device cannot check (a key unlike its record, a seat
// taken twice, a certificate leaf flagged at its commit): one plain sentence,
// never "first device", never an action.
for (const kind of ["key_differs", "seated", "repeated", "replaced", "not_theirs"]) {
  const [l] = seatBannerLines({ guildName: g, name, canManage: true, flags: [{ index: 5, key: "gh", kind, account: "O", adder: "M", provable: false }] });
  assert.equal(l.text, "A device in Den claims to be one of Olu's, and this device cannot check it. It was added by Mo.", kind);
  assert.equal(l.action, undefined);
  assert.doesNotMatch(l.text, /first device/);
}

// This account's own identity, on a device that cannot commit.
{
  const [l] = seatBannerLines({ guildName: g, name, flags: [{ index: 2, key: "ef", kind: "not_theirs", account: "A", adder: "M", own: true, provable: true }] });
  assert.equal(l.text, "A device that is not yours is in Den under your identity. It was added by Mo. Only an admin can remove it.");
  assert.equal(l.action, undefined);
}

// "Concord is removing it" only when this device will; otherwise it says when.
{
  const flag = { index: 2, key: "ef", kind: "not_theirs", account: "A", adder: "M", own: true, provable: true };
  const [now] = seatBannerLines({ guildName: g, name, canManage: true, flags: [{ ...flag, removing: true }] });
  assert.equal(now.text, "A device that is not yours is in Den under your identity. It was added by Mo. Concord is removing it.");
  const [later] = seatBannerLines({ guildName: g, name, canManage: true, flags: [flag] });
  assert.equal(later.text, "A device that is not yours is in Den under your identity. It was added by Mo. Concord will remove it as soon as this device can.");
}

// Repeats: the reader is told to take the adder's permission away, unless the
// adder is the owner, whose permissions nobody can take.
{
  const [l] = seatBannerLines({ guildName: g, name, owner: "O", notices: [{ kind: "repeats", adder: "M", at: 2 }] });
  assert.equal(l.text, "Mo keeps adding a device under your identity to Den. Concord removed it again. Take away Mo's permission to manage members.");
  const [o] = seatBannerLines({ guildName: g, name, owner: "O", notices: [{ kind: "repeats", adder: "O", at: 3 }] });
  assert.equal(o.text, "Olu keeps adding a device under your identity to Den. Concord removed it again.");
}

// The owner's device removed by another member: nothing stands in for it, so
// the line says who removed it and what happens next, and never that a device
// not theirs stays under their identity.
{
  const [l] = seatBannerLines({ guildName: g, name, owner: "A", notices: [{ kind: "owner_removed", adder: "M", at: 4 }] });
  assert.equal(l.text, "Mo removed this device from Den. You still own Den, so this device asks to be let back in and rejoins once a member who can add it is online.");
  assert.doesNotMatch(l.text, /not yours|under your identity|replaced/);
  assert.equal(l.action, undefined);
  const [u] = seatBannerLines({ guildName: g, name, notices: [{ kind: "owner_removed", at: 5 }] });
  assert.match(u.text, /^A member removed this device from Den\./);
}

console.log("seatbanner: ok");
