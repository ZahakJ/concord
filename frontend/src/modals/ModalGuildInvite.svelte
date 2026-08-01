<script>
  // Someone you've VERIFIED wants to add you to their server. This is an offer,
  // not a fait accompli: nothing joins until you say yes (accepting is what
  // redeems the invite code — see internal/app/dm.go, which deliberately does
  // NOT auto-redeem server invites the way DM invites are auto-redeemed).
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, flash, refreshGuilds, selectGuild } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { invite, onClose } = $props();
  let busy = $state(false);

  // Server invites only come from verified contacts, so their learned profile
  // face is available — show it rather than an initials disc.
  const fromContact = $derived(S.contacts.find((c) => c.fingerprint === invite.from));

  async function accept() {
    busy = true;
    try {
      const g = await api.joinViaInvite(invite.code);
      await refreshGuilds();
      if (g?.id) selectGuild(g.id);
      flash(`Joined ${g?.name || invite.guild}`, "success");
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title="Server invite" onClose={busy ? () => {} : onClose}>
  <div class="who">
    <Avatar
      name={invite.fromName || invite.from}
      image={fromContact?.avatar || ""}
      emoji={fromContact?.emoji || ""}
      color={fromContact?.color || ""}
      size={44}
    />
    <p>
      <strong>{invite.fromName || invite.from.slice(0, 9)}</strong> invited you to
      <strong>{invite.guild || "their server"}</strong>.
    </p>
    <p class="tiny muted">
      You've verified them, which is why this reached you at all — invites from
      anyone else are ignored.
    </p>
  </div>
  <div class="actions">
    <button class="ghost" disabled={busy} onclick={onClose}>Ignore</button>
    <button disabled={busy} onclick={accept}>
      <Icon name="check" size={14} />
      {busy ? "Joining…" : "Join server"}
    </button>
  </div>
</Modal>

<style>
  .who {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    text-align: center;
    padding: 6px 0 4px;
  }
  .who p {
    margin: 0;
    line-height: 1.5;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
</style>
