<script>
  // Create a group DM from your VERIFIED contacts. Verification is the trust
  // gate (backend enforces it too); only verified people can be added, matching
  // "pull the people I've approved into a private group."
  import Modal from "./Modal.svelte";
  import Avatar from "../Avatar.svelte";
  import Icon from "../Icon.svelte";
  import { createGroupDM, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { onClose } = $props();

  let contacts = $state([]);
  let query = $state("");
  let picked = $state(new Set());
  let busy = $state(false);
  let loaded = $state(false);

  // Only verified contacts are eligible (the backend rejects the rest anyway).
  $effect(() => {
    api
      .contacts()
      .then((cs) => {
        contacts = (cs || []).filter((c) => c.verified);
        loaded = true;
      })
      .catch(() => (loaded = true));
  });

  const shown = $derived.by(() => {
    const q = query.trim().toLowerCase();
    return q
      ? contacts.filter((c) => (c.name || c.fingerprint).toLowerCase().includes(q))
      : contacts;
  });

  function toggle(fpr) {
    const next = new Set(picked);
    next.has(fpr) ? next.delete(fpr) : next.add(fpr);
    picked = next;
  }

  const canCreate = $derived(picked.size >= 2 && !busy);

  async function create() {
    if (!canCreate) return;
    busy = true;
    try {
      await createGroupDM([...picked]);
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title="New group DM" {onClose}>
  <p class="hint muted">
    Group DMs are built from people you've <strong>verified</strong>. Pick at least two.
  </p>
  <input class="search" bind:value={query} placeholder="Search verified contacts…" />
  <div class="list">
    {#each shown as c (c.fingerprint)}
      <button class="row" class:sel={picked.has(c.fingerprint)} onclick={() => toggle(c.fingerprint)}>
        <Avatar name={c.name || c.fingerprint} size={26} />
        <span class="nm">{c.name || c.fingerprint.slice(0, 9)}</span>
        <span class="check" class:on={picked.has(c.fingerprint)}>
          {#if picked.has(c.fingerprint)}<Icon name="check" size={12} />{/if}
        </span>
      </button>
    {:else}
      <div class="muted none">
        {loaded ? "No verified contacts yet — verify someone from their profile first." : "Loading…"}
      </div>
    {/each}
  </div>
  <div class="foot">
    <span class="muted count">{picked.size} selected</span>
    <button disabled={!canCreate} onclick={create}>Create group</button>
  </div>
</Modal>

<style>
  .hint {
    font-size: 13px;
    margin: 0 0 10px;
  }
  .search {
    font-size: 13px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 300px;
    overflow-y: auto;
    margin-top: 8px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 9px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 7px 8px;
    border-radius: var(--radius-sm);
  }
  .row:hover {
    background: var(--bg-3);
  }
  .row.sel {
    background: var(--accent-soft);
  }
  .nm {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .check {
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 1px solid var(--border);
    display: grid;
    place-items: center;
    color: #fff;
    flex-shrink: 0;
  }
  .check.on {
    background: var(--accent);
    border-color: transparent;
  }
  .none {
    padding: 14px;
    font-size: 13px;
    text-align: center;
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 12px;
  }
  .count {
    font-size: 12px;
  }
</style>
