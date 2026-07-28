<script>
  // The block list: people you've blocked can't add you to DMs or servers.
  // Unblock from here.
  import Modal from "./Modal.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, unblockUser, nameFor } from "../lib/state.svelte.js";

  let { onClose } = $props();
</script>

<Modal title="Blocked users" {onClose}>
  {#if S.blocked.length === 0}
    <p class="muted empty">
      You haven't blocked anyone. Blocking someone stops them from adding you to
      DMs or servers — open their profile and choose “Block”.
    </p>
  {:else}
    <p class="muted tiny intro">Blocked people can't add you to DMs or servers.</p>
    <div class="list">
      {#each S.blocked as fpr (fpr)}
        <div class="row">
          <Avatar name={nameFor(fpr)} size={30} />
          <span class="who">
            <strong>{nameFor(fpr)}</strong>
            <span class="tiny muted mono">{fpr.slice(0, 12)}…</span>
          </span>
          <button class="unblock" onclick={() => unblockUser(fpr, nameFor(fpr))}>Unblock</button>
        </div>
      {/each}
    </div>
  {/if}
</Modal>

<style>
  .empty {
    font-size: 13px;
    line-height: 1.5;
    margin: 0;
  }
  .intro {
    font-size: 11px;
    margin: 0 0 10px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .who strong {
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mono {
    font-family: var(--mono, monospace);
  }
  .tiny {
    font-size: 11px;
  }
  .unblock {
    flex-shrink: 0;
    padding: 6px 12px;
    background: var(--bg-3);
    color: var(--text);
    border-radius: var(--radius-sm);
    font-size: 13px;
  }
  .unblock:hover {
    background: var(--accent);
    color: var(--accent-fg);
  }
</style>
