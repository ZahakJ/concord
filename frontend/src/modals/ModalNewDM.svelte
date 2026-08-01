<script>
  // One entry point for starting a conversation. Pick one contact for a 1:1 DM,
  // or several for a group DM (group DMs require every member to be verified —
  // the backend enforces it). "Invite by link" covers people not yet in your
  // contacts. Reusing an existing conversation is handled server-side.
  import Modal from "./Modal.svelte";
  import Avatar from "../Avatar.svelte";
  import Icon from "../Icon.svelte";
  import { S, startDM, createGroupDM, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { onClose } = $props();

  let contacts = $state([]);
  let query = $state("");
  let picked = $state(new Set());
  let busy = $state(false);
  let loaded = $state(false);

  $effect(() => {
    api
      .contacts()
      .then((cs) => {
        contacts = cs || [];
        loaded = true;
      })
      .catch(() => (loaded = true));
  });

  const shown = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const list = q
      ? contacts.filter((c) => (c.name || c.fingerprint).toLowerCase().includes(q))
      : contacts;
    // Verified first, then by name.
    return [...list].sort(
      (a, b) =>
        (b.verified ? 1 : 0) - (a.verified ? 1 : 0) ||
        (a.name || a.fingerprint).localeCompare(b.name || b.fingerprint),
    );
  });

  const byFpr = $derived(new Map(contacts.map((c) => [c.fingerprint, c])));
  // When 2+ are picked it becomes a group DM, which requires all to be verified.
  const isGroup = $derived(picked.size >= 2);
  const unverifiedPicked = $derived(
    isGroup ? [...picked].filter((f) => !byFpr.get(f)?.verified) : [],
  );
  const canCreate = $derived(picked.size >= 1 && unverifiedPicked.length === 0 && !busy);

  function toggle(fpr) {
    const next = new Set(picked);
    next.has(fpr) ? next.delete(fpr) : next.add(fpr);
    picked = next;
  }

  async function create() {
    if (!canCreate) return;
    busy = true;
    try {
      const fprs = [...picked];
      if (fprs.length === 1) await startDM(fprs[0]);
      else await createGroupDM(fprs);
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }

  async function inviteByLink() {
    try {
      const code = await api.newDMInvite();
      S.modal = { kind: "invite", code }; // swap out for the invite-code modal
    } catch (err) {
      flash(err);
    }
  }
</script>

<Modal title="New message" {onClose}>
  <input class="search" bind:value={query} placeholder="Search contacts…" autofocus={!S.isMobile} />

  {#if isGroup}
    <p class="hint muted">
      {picked.size} selected — this will be a <strong>group DM</strong>.
      {#if unverifiedPicked.length}
        <span class="warn"
          >Verify {unverifiedPicked
            .map((f) => byFpr.get(f)?.name || f.slice(0, 9))
            .join(", ")} to include them.</span
        >
      {/if}
    </p>
  {/if}

  <div class="list">
    {#each shown as c (c.fingerprint)}
      <button class="row" class:sel={picked.has(c.fingerprint)} onclick={() => toggle(c.fingerprint)}>
        <Avatar
          name={c.name || c.fingerprint}
          image={c.avatar || ""}
          emoji={c.emoji || ""}
          color={c.color || ""}
          size={26}
        />
        <span class="nm">{c.name || c.fingerprint.slice(0, 9)}</span>
        {#if c.verified}
          <span class="vbadge" title="Verified"><Icon name="check" size={11} /></span>
        {/if}
        <span class="check" class:on={picked.has(c.fingerprint)}>
          {#if picked.has(c.fingerprint)}<Icon name="check" size={12} />{/if}
        </span>
      </button>
    {:else}
      <div class="muted none">
        {loaded ? "No contacts yet — invite someone by link below." : "Loading…"}
      </div>
    {/each}
  </div>

  <div class="foot">
    <button class="link" onclick={inviteByLink}>Invite by link…</button>
    <button disabled={!canCreate} onclick={create}>
      {picked.size >= 2 ? "Create group" : "Message"}
    </button>
  </div>
</Modal>

<style>
  .search {
    font-size: var(--fs-ui);
  }
  .hint {
    font-size: var(--fs-compact);
    margin: 8px 0 0;
  }
  .warn {
    color: #f0b232;
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
  .row:hover,
  .row:active {
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
  .vbadge {
    color: var(--ok-text);
    display: grid;
    place-items: center;
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
    font-size: var(--fs-ui);
    text-align: center;
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 12px;
  }
  @media (pointer: coarse), (max-width: 768px) {
    /* Two nested scrollers in one sheet means a flick that starts inside the
       list moves the list and the same flick 4px outside moves the sheet. One
       scroller — the sheet's — behaves predictably. */
    .list {
      max-height: none;
      overflow-y: visible;
    }
    /* …which makes the sheet tall, so its commit button has to be pinned or a
       long contact list strands it below the fold. This footer isn't the shared
       `.actions` class Modal pins for free, so it pins itself. */
    .foot {
      position: sticky;
      bottom: calc(-20px - var(--safe-bottom));
      z-index: 2;
      gap: 10px;
      margin: 12px -20px calc(-20px - var(--safe-bottom));
      padding: 10px 20px calc(10px + var(--safe-bottom));
      background: var(--bg-elevated);
      border-top: 1px solid var(--border);
    }
  }
  .link {
    background: transparent;
    color: var(--text-muted);
    padding: 6px 4px;
  }
  .link:hover {
    background: transparent;
    color: var(--text);
    text-decoration: underline;
  }
</style>
