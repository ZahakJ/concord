<script>
  // Add people you've VERIFIED straight into this server — no invite code, no
  // copy-paste, exactly like starting a DM with them.
  //
  // Only verified contacts appear, and that isn't a UI nicety: their client
  // auto-accepts a server invite only from someone THEY verified. Verification
  // is the trust that makes "just add them" safe on both ends.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, flash, refreshGuilds } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { onClose } = $props();

  let busy = $state("");
  let added = $state(new Set());

  const memberFprs = $derived(new Set(S.members.map((m) => m.fingerprint)));
  const candidates = $derived(
    S.contacts.filter((c) => c.verified && !memberFprs.has(c.fingerprint)),
  );
  const unverified = $derived(S.contacts.filter((c) => !c.verified).length);

  async function add(c) {
    busy = c.fingerprint;
    try {
      await api.addMember(S.activeGuildId, c.fingerprint);
      added = new Set([...added, c.fingerprint]);
      flash(`Added ${c.name || "them"} — they'll appear once they accept`, "success");
      setTimeout(refreshGuilds, 2500);
    } catch (err) {
      flash(err);
    } finally {
      busy = "";
    }
  }
</script>

<Modal title="Add people" {onClose}>
  {#if candidates.length}
    <p class="tiny muted intro">
      Verified contacts drop straight in — no invite code needed.
    </p>
    <div class="list">
      {#each candidates as c (c.fingerprint)}
        <div class="row">
          <Avatar
            name={c.name || c.fingerprint}
            image={c.avatar || ""}
            emoji={c.emoji || ""}
            color={c.color || ""}
            size={32}
          />
          <span class="who">
            <strong>{c.name || c.fingerprint.slice(0, 9)}</strong>
            <span class="tiny muted mono">{c.fingerprint.slice(0, 9)}</span>
          </span>
          {#if added.has(c.fingerprint)}
            <span class="done tiny"><Icon name="check" size={12} /> Added</span>
          {:else}
            <button disabled={busy === c.fingerprint} onclick={() => add(c)}>
              {busy === c.fingerprint ? "Adding…" : "Add"}
            </button>
          {/if}
        </div>
      {/each}
    </div>
  {:else}
    <div class="empty">
      <Icon name="members" size={22} />
      <p>No verified contacts to add.</p>
      <p class="tiny muted">
        {#if unverified}
          You have {unverified} contact{unverified === 1 ? "" : "s"} you haven't verified. Open their
          profile and compare safety numbers — then they can be added with one click.
        {:else}
          Verify someone (open their profile → compare safety numbers) and they'll show up here.
        {/if}
      </p>
    </div>
  {/if}

  <p class="tiny muted foot">
    Anyone else can still join with an invite code.
  </p>
</Modal>

<style>
  .intro {
    margin: 0 0 10px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 46vh;
    max-height: 46dvh; /* the vh line above is the fallback; dvh shrinks with the keyboard */
    overflow-y: auto;
  }
  /* One scroller per sheet — see Modal.svelte. */
  @media (pointer: coarse), (max-width: 768px) {
    .list {
      max-height: none;
      overflow-y: visible;
    }
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 8px;
    border-radius: var(--radius-md);
  }
  .row:hover {
    background: var(--bg-3);
  }
  .row button {
    flex: none;
  }
  .who {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }
  .who strong {
    font-size: var(--fs-ui);
  }
  .mono {
    font-family: var(--mono, monospace);
  }
  .done {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--ok-text);
  }
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 22px 12px;
    color: var(--text-muted);
    text-align: center;
  }
  .empty p {
    margin: 0;
    line-height: 1.5;
  }
  .foot {
    margin: 12px 0 0;
    text-align: center;
  }
</style>
