// The ownership flows — transfer, name an heir, revoke, claim — lifted out of
// MemberPanel so the settings panel that ADVERTISES them can run the same
// code. The confirmation copy is the good part and must not be reworded per
// surface: an heir designation is a permanent break-glass, and the sentence
// that says so is the whole safety of the feature.
import { S, activeGuild, flash, refreshGuilds, refreshRightPanel } from "./state.svelte.js";
import { api } from "./api.js";

const label = (mem) => mem?.name || mem?.fingerprint?.slice(0, 9) || "this member";

// Handing the guild over is the most destructive thing an owner can do to
// themselves — a destructive-tier confirm spelling out exactly what changes
// hands, the same two-step shape kick and ban use.
export function confirmTransferOwnership(mem) {
  const name = label(mem);
  const gname = activeGuild()?.name || "this guild";
  S.modal = {
    kind: "confirm",
    title: `Transfer ownership to ${name}?`,
    body: `This makes ${name} the owner of ${gname}. You'll become a regular member, and only ${name} can hand ownership back.`,
    confirmLabel: "Transfer ownership",
    onConfirm: async () => {
      S.modal = null;
      try {
        await api.transferOwnership(S.activeGuildId, mem.fingerprint);
        // Both sides of the handover show immediately: the crown badge moves
        // (members) and our own owner-only affordances drop (guild flags).
        await refreshGuilds();
        await refreshRightPanel();
        flash(`${name} now owns this guild`, "success");
      } catch (err) {
        flash(err);
      }
    },
  };
}

// Naming an heir hands out a PERMANENT break-glass: the claim is valid
// whenever the heir uses it, not just "if the owner goes quiet" — a liveness
// gate can't exist in a partitioned P2P network without risking two crowned
// owners. The confirm copy must say that plainly, never soften it into a
// dead-man switch the system doesn't actually have.
export function confirmNameHeir(mem) {
  const name = label(mem);
  const gname = activeGuild()?.name || "this guild";
  S.modal = {
    kind: "confirm",
    title: `Name ${name} as heir?`,
    body: `${name} will be able to take ownership of ${gname} at any time — including while you're still around. Only name someone you'd trust to run this place. You can revoke this whenever you like, until it's used.`,
    confirmLabel: "Name as heir",
    onConfirm: async () => {
      S.modal = null;
      try {
        await api.setHeir(S.activeGuildId, mem.fingerprint);
        await refreshGuilds();
        await refreshRightPanel();
        flash(`${name} is now this guild's heir`, "success");
      } catch (err) {
        flash(err);
      }
    },
  };
}

// Revoking is the safe direction — no confirm needed, just do it.
export async function revokeHeir(mem) {
  try {
    await api.clearHeir(S.activeGuildId);
    await refreshGuilds();
    await refreshRightPanel();
    flash(`${mem?.name || "They are"} no longer the heir`);
  } catch (err) {
    flash(err);
  }
}

// The heir cashing the designation — worded for the real situation (the owner
// is gone, or asked them to), two-step confirmed like a transfer.
export function confirmClaimOwnership() {
  const gname = activeGuild()?.name || "this guild";
  S.modal = {
    kind: "confirm",
    title: "Take ownership of this guild?",
    body: `The owner named you their heir, so you can take over at any time. You'll become the owner of ${gname} and the current owner becomes a regular member. Do this if the owner is gone — or asked you to.`,
    confirmLabel: "Take ownership",
    onConfirm: async () => {
      S.modal = null;
      try {
        await api.claimOwnership(S.activeGuildId);
        await refreshGuilds();
        await refreshRightPanel();
        flash("You now own this guild", "success");
      } catch (err) {
        flash(err);
      }
    },
  };
}
