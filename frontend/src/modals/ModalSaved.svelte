<script>
  // Saved messages: the reader's private shelf. Rows jump back to the message
  // in place (same two-frame jump the search panel uses) — the whole point
  // over forward-to-Notes was keeping the way back.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EmptyState from "../EmptyState.svelte";
  import { S, jumpToChannel, scrollToMessage, channelShort, isDMChannel, flash } from "../lib/state.svelte.js";
  import { saved, refreshSaved } from "../lib/saved.svelte.js";
  import { api } from "../lib/api.js";
  import { plainSnippet } from "../lib/snippet.js";
  import { tick } from "svelte";

  let { onClose } = $props();

  let rows = $state(null); // null = loading
  $effect(() => {
    api
      .savedMessages()
      .then((r) => (rows = r || []))
      .catch(() => (rows = []));
  });

  async function open(m) {
    onClose();
    await jumpToChannel(m.channelId);
    await tick();
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        if (!scrollToMessage(m.id)) flash("That message isn't loaded yet");
      }),
    );
  }

  async function unsave(m) {
    try {
      await api.unbookmarkMessage(m.id);
      saved.ids.delete(m.id);
      saved.ids = new Set(saved.ids);
      rows = rows.filter((r) => r.id !== m.id);
    } catch (err) {
      flash(err);
    }
  }

  const where = (m) => (isDMChannel(m.channelId) ? channelShort(m.channelId) : `#${channelShort(m.channelId)}`);
  const when = (t) => new Date(t).toLocaleDateString(undefined, { month: "short", day: "numeric" });

  // Ensure the menu label set stays honest if this panel was the first load.
  refreshSaved();
</script>

<Modal title="Saved messages" {onClose}>
  {#if rows === null}
    <p class="tiny muted center">Loading…</p>
  {:else if !rows.length}
    <div class="center">
      <EmptyState
        icon="pin"
        headline="Nothing saved yet"
        sub="Right-click any message (or long-press on a phone) and pick Save Message — it lands here, with the way back."
      />
    </div>
  {:else}
    <div class="list">
      {#each rows as m (m.id)}
        <div class="row">
          <button class="body" onclick={() => open(m)}>
            <span class="meta">
              <strong>{m.senderName || m.sender.slice(0, 9)}</strong>
              <span class="tiny muted">{where(m)} · {when(m.sent)}</span>
            </span>
            <span class="snippet">{plainSnippet(m.content)}</span>
          </button>
          <button class="x" title="Remove from saved" aria-label="Remove from saved" onclick={() => unsave(m)}>
            <Icon name="close" size={12} />
          </button>
        </div>
      {/each}
    </div>
  {/if}
</Modal>

<style>
  .center {
    display: flex;
    justify-content: center;
    padding: 18px 0;
  }
  .center.tiny,
  p.center {
    text-align: center;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    max-height: 56vh;
    max-height: 56dvh;
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
    align-items: stretch;
    gap: 2px;
  }
  .body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
    text-align: left;
    padding: 8px 10px;
    border-radius: var(--radius-md);
    background: none;
  }
  .body:hover {
    background: var(--bg-3);
  }
  .meta {
    display: flex;
    align-items: baseline;
    gap: var(--sp-2);
    min-width: 0;
  }
  .meta strong {
    font-size: var(--fs-ui);
  }
  .snippet {
    font-size: var(--fs-ui);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }
  .x {
    flex: none;
    align-self: center;
    padding: var(--sp-2);
    border-radius: var(--radius-md);
    color: var(--text-muted);
    background: none;
  }
  .x:hover {
    color: var(--danger-text);
    background: var(--bg-3);
  }
</style>
