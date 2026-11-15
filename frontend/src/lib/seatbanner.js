// The banner for leaves of a guild that carry somebody's identity under a key
// that is not theirs (app/seatcheck.go). Pure: it turns the guild's seatFlags
// and seatNotices into lines, so the wording can be tested without a DOM.
//
// Every line names who added the device when that is known: it is the most
// useful fact on the screen, because that member's permission is what let it
// happen.

function addedBy(adder, name) {
  return adder ? ` It was added by ${name(adder)}.` : "";
}

// seatBannerLines returns [{ key, text, action? }]. action, when present, is
// { label, title, body, confirmLabel, index, deviceKey } for the removal.
// owner is the guild owner's fingerprint: nobody can take the owner's
// permissions away, so no line asks the reader to.
export function seatBannerLines({ flags = [], notices = [], canManage = false, guildName = "this guild", owner = "", name = (f) => f }) {
  const out = [];
  for (const n of notices) {
    const by = n.adder ? name(n.adder) : "";
    if (n.kind === "removed") {
      out.push({
        key: `n-removed-${n.at}`,
        text:
          `A device that is not yours was added to ${guildName} under your identity` +
          (by ? ` by ${by}` : "") +
          ". Concord removed it. Your own devices are not affected.",
      });
    } else if (n.kind === "repeats") {
      out.push({
        key: `n-repeats-${n.at}`,
        text:
          `${by || "A member"} keeps adding a device under your identity to ${guildName}. Concord removed it again.` +
          (n.adder && n.adder === owner ? "" : ` Take away ${by ? `${by}'s` : "their"} permission to manage members.`),
      });
    } else if (n.kind === "replaced") {
      out.push({
        key: `n-replaced-${n.at}`,
        text:
          `This device was taken out of ${guildName} while a device that is not yours stays there under your identity.` +
          addedBy(n.adder, name),
      });
    }
  }
  for (const f of flags) {
    const who = name(f.account);
    const key = `f-${f.index}-${f.key}`;
    const removal = {
      label: "Remove this device",
      title: "Remove this device?",
      body: `Remove this device from ${guildName}? ${who}'s own devices stay.`,
      confirmLabel: "Remove",
      index: f.index,
      deviceKey: f.key,
    };
    if (f.own && f.provable) {
      out.push({
        key,
        text:
          f.removing
            ? `A device that is not yours is in ${guildName} under your identity.${addedBy(f.adder, name)} Concord is removing it.`
            : canManage
              ? `A device that is not yours is in ${guildName} under your identity.${addedBy(f.adder, name)} Concord will remove it as soon as this device can.`
              : `A device that is not yours is in ${guildName} under your identity.${addedBy(f.adder, name)} Only an admin can remove it.`,
        action: canManage ? { ...removal, body: `Remove this device from ${guildName}? Your own devices stay.` } : undefined,
      });
      continue;
    }
    if (f.provable) {
      out.push({
        key,
        text: canManage
          ? `A device in ${guildName} uses ${who}'s identity but is not one of their devices.${addedBy(f.adder, name)} It can read what is sent here until it is removed.`
          : `A device in ${guildName} uses ${who}'s identity but is not one of their devices.`,
        action: canManage ? removal : undefined,
      });
      continue;
    }
    if (f.kind === "changed") {
      out.push({
        key,
        text: `A device in ${guildName} changed the identity it claims, to ${who}'s.${canManage ? addedBy(f.adder, name) : ""}`,
      });
      continue;
    }
    // This device cannot check the leaf: no removal is offered, or it could
    // take out the real one.
    if (f.kind === "ambiguous") {
      out.push({
        key,
        text: canManage
          ? `Two devices in ${guildName} use ${who}'s identity, and this device cannot tell which one is really ${who}'s.${addedBy(f.adder, name)} ${who}'s own device can tell, and removes the other if ${who} may manage members.`
          : `Two devices in ${guildName} use ${who}'s identity, and this device cannot tell which one is really ${who}'s.`,
      });
      continue;
    }
    out.push({
      key,
      text: `A device in ${guildName} claims to be one of ${who}'s, and this device cannot check it.${canManage ? addedBy(f.adder, name) : ""}`,
    });
  }
  return out;
}
