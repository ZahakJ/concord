<script>
  // Lists a guild's banned members and lets a moderator lift a ban.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, refreshRightPanel, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { onClose } = $props();

  let bans = $state([]);
  let loading = $state(true);
  let busy = $state("");

  async function load() {
    loading = true;
    try {
      bans = (await api.bans(S.activeGuildId)) || [];
    } catch (err) {
      flash(err);
    } finally {
      loading = false;
    }
  }

  async function unban(fpr) {
    busy = fpr;
    try {
      await api.unbanMember(S.activeGuildId, fpr);
      await refreshRightPanel();
      bans = bans.filter((b) => b.fingerprint !== fpr);
      flash("Ban lifted", "success");
    } catch (err) {
      flash(err);
    } finally {
      busy = "";
    }
  }

  load();
</script>

<Modal title="Banned members" {onClose}>
  {#if loading}
    <p class="muted empty">Loading…</p>
  {:else if bans.length === 0}
    <p class="muted empty">No one is banned from this guild.</p>
  {:else}
    <div class="list">
      {#each bans as b (b.fingerprint)}
        <div class="ban-row">
          <Avatar name={b.name || b.fingerprint} size={26} />
          <span class="ban-name">{b.name || b.fingerprint.slice(0, 12)}</span>
          <button class="unban" disabled={busy === b.fingerprint} onclick={() => unban(b.fingerprint)}>
            Unban
          </button>
        </div>
      {/each}
    </div>
  {/if}
</Modal>

<style>
  .empty {
    font-size: var(--fs-ui);
    padding: 8px 2px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 320px;
    overflow-y: auto;
  }
  /* One scroller per sheet: a list that scrolls inside a sheet that scrolls
     makes the sheet feel arbitrarily sticky under a thumb. */
  @media (pointer: coarse), (max-width: 768px) {
    .list {
      max-height: none;
      overflow-y: visible;
    }
  }
  .ban-row {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 7px 6px;
    border-radius: var(--radius-sm);
  }
  .ban-row:hover {
    background: var(--bg-3);
  }
  .ban-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-ui);
  }
  .unban {
    padding: 5px 12px;
    font-size: var(--fs-compact);
    background: var(--bg-3);
    border-radius: var(--radius-sm);
    flex: none;
  }
  .unban:hover,
  .unban:active {
    background: var(--accent);
    color: var(--accent-fg);
  }
</style>
