<script>
  // "Delete everything on this device."
  //
  // The erase itself is old code — it is what "Forgot passphrase? → start over"
  // has always called. What is new is that a logged-in person can find it, and
  // that they are told the truth before they do it. Three things this dialog
  // has to say and does:
  //
  //   * What actually goes: this device's copy. Not the messages other people
  //     are holding, because nothing running here can reach those.
  //   * What the way back is, if there is one. For an account with another
  //     linked device, re-linking brings the guilds and the history back. For
  //     an account with no other device there is no way back, and the 24-word
  //     recovery phrase is NOT one — it restores the account key alone, and
  //     somebody who reads "recovery phrase" as "backup" finds that out at the
  //     worst possible moment.
  //   * That it wants the name typed. Two clicks is how people delete things by
  //     accident; the backend spends a one-shot ticket on the attempt either
  //     way (internal/bridge/wipe.go), so this is a gate, not a speed bump.
  import Modal from "./Modal.svelte";
  import { api } from "../lib/api.js";
  import { flash } from "../lib/state.svelte.js";
  import { haptic } from "../lib/touch.js";

  let { onClose } = $props();

  let ticket = $state(null); // WipeView from the backend, or null while loading
  let typed = $state("");
  let busy = $state(false);

  (async () => {
    try {
      ticket = await api.beginWipe();
    } catch (err) {
      flash(err);
      onClose?.();
    }
  })();

  const matches = $derived(
    !!ticket && typed.trim().toLowerCase() === (ticket.phrase || "").toLowerCase(),
  );

  async function erase() {
    if (!matches || busy) return;
    busy = true;
    haptic("heavy");
    try {
      await api.confirmWipe(ticket.ticket, typed.trim());
      // The session this page was talking to no longer exists, and neither does
      // the data behind it. Reloading is what puts the app back on the first
      // screen it ever shows, which is now the honest one: no identity here.
      location.reload();
    } catch (err) {
      busy = false;
      // The ticket is spent whether or not it matched, so there is nothing left
      // to retry against — close and let them start the flow again.
      flash(err);
      onClose?.();
    }
  }
</script>

<Modal title="Delete everything on this device" {onClose}>
  {#if ticket}
    <p>
      This erases your identity, your profile, the encrypted database holding every message stored
      here, and the group keys for
      {ticket.guilds === 1 ? "the guild" : `all ${ticket.guilds} guilds`} this device is in. It
      happens immediately and it cannot be undone from here.
    </p>
    <p>
      <b>It deletes nothing from anyone else.</b> Every message you have sent is on the devices of the
      people you sent it to, sealed to them, and nothing running on this machine can reach it. Guilds
      you are in carry on without you.
    </p>
    {#if ticket.devices === 1}
      <p>
        Your other linked device is untouched and will keep working — it is not told, this one simply
        stops answering. That is also the way back: link this device to it again and your guilds and
        history come with it.
      </p>
    {:else if ticket.devices > 1}
      <p>
        Your {ticket.devices} other linked devices are untouched and will keep working — they are not
        told, this one simply stops answering. That is also the way back: link this device to one of
        them again and your guilds and history come with it.
      </p>
    {:else}
      <p>
        No other device is linked to this account, so this is the only copy. Your 24-word recovery
        phrase is <b>not</b> a backup of it — it restores your account key, and you would come back to
        the same name with no guilds and no history.
      </p>
    {/if}
    <p class="type-line">
      Type <b>{ticket.phrase}</b> to confirm.
    </p>
    <!-- svelte-ignore a11y_autofocus -->
    <input
      bind:value={typed}
      autofocus
      autocomplete="off"
      autocapitalize="off"
      spellcheck="false"
      aria-label="Type {ticket.phrase} to confirm"
      placeholder={ticket.phrase}
      disabled={busy}
      onkeydown={(e) => e.key === "Enter" && erase()}
    />
    <div class="actions">
      <button class="ghost" onclick={onClose} disabled={busy}>Cancel</button>
      <button class="danger" disabled={!matches || busy} onclick={erase}>
        {busy ? "Erasing…" : "Delete everything"}
      </button>
    </div>
  {:else}
    <p>Checking what is on this device…</p>
  {/if}
</Modal>

<style>
  p {
    font-size: var(--fs-ui);
    color: var(--text-muted);
    line-height: 1.55;
    margin: 0 0 10px;
  }
  p b {
    color: var(--text);
  }
  .type-line {
    margin-top: 14px;
    margin-bottom: 6px;
  }
  input {
    width: 100%;
  }
  .actions {
    margin-top: 14px;
  }
  button.danger {
    background: var(--danger);
    box-shadow: 0 0 12px color-mix(in srgb, var(--danger) 35%, transparent);
    transition: background 0.15s ease;
  }
  button.danger:hover:not(:disabled) {
    background: color-mix(in srgb, var(--danger) 85%, white);
  }
  /* Unarmed, it must not look like a button that is waiting to be pressed —
     the whole point of the typed confirmation is that this one is out of
     reach until the name is right. */
  button.danger:disabled {
    background: color-mix(in srgb, var(--danger) 22%, var(--bg-3));
    color: var(--text-muted);
    box-shadow: none;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .actions {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 10px;
    }
    .actions button {
      min-height: 48px;
    }
  }
</style>
