<script>
  // "Ownership & heir" used to be a row in the danger zone that told you to go
  // somewhere else: not a link, not a button, a paragraph of documentation
  // sitting in a list of actions. The flow it pointed at is genuinely good —
  // this is that flow, reachable from the place that advertises it.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, activeGuild, openContextMenu } from "../lib/state.svelte.js";
  import {
    confirmTransferOwnership,
    confirmNameHeir,
    revokeHeir,
    confirmClaimOwnership,
  } from "../lib/ownership.svelte.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const owner = $derived(S.members.find((m) => m.isOwner));
  const heir = $derived(S.members.find((m) => m.isHeir));
  const iAmHeir = $derived(!!heir?.isSelf);
  // A pending invitee is not yet in the group, and both ops are re-checked
  // receive-side against live membership — offering them would mint an op
  // every peer drops.
  const eligible = $derived(S.members.filter((m) => !m.isSelf && !m.pending));

  function pick(e, onPicked) {
    if (!eligible.length) return;
    openContextMenu(
      e,
      eligible.map((m) => ({
        label: m.name || m.fingerprint.slice(0, 9),
        icon: "crown",
        onClick: () => onPicked(m),
      })),
      { title: "Choose a member" },
    );
  }
</script>

<Modal title="Ownership &amp; heir" {onClose}>
  <section class="grp">
    <div class="sec-label">Owner</div>
    <div class="card">
      <div class="row">
        <Avatar
          name={owner?.name || "Owner"}
          image={owner?.avatar}
          emoji={owner?.emoji}
          color={owner?.color}
          size={30}
        />
        <span class="row-text">
          <span class="row-title">{owner?.name || "Unknown"}{owner?.isSelf ? " (you)" : ""}</span>
          <span class="row-sub">
            Holds every permission in {g?.name || "this guild"} and outranks every role.
          </span>
        </span>
      </div>
    </div>
  </section>

  <section class="grp">
    <div class="sec-label">Heir</div>
    <div class="card">
      {#if heir}
        <div class="row">
          <Avatar
            name={heir.name || "Heir"}
            image={heir.avatar}
            emoji={heir.emoji}
            color={heir.color}
            size={30}
          />
          <span class="row-text">
            <span class="row-title">{heir.name || "Unknown"}{heir.isSelf ? " (you)" : ""}</span>
            <span class="row-sub">Can take ownership at any time, until the designation is revoked.</span>
          </span>
          {#if g?.isOwner}
            <button class="ghost small" onclick={() => revokeHeir(heir)}>Revoke</button>
          {/if}
        </div>
      {:else}
        <div class="row note">
          <span class="chip"><Icon name="crown" size={16} /></span>
          <span class="row-text">
            <span class="row-title">No heir named</span>
            <span class="row-sub">
              If the owner loses this account, nobody can add or remove members again.
            </span>
          </span>
        </div>
      {/if}
    </div>
  </section>

  {#if g?.isOwner}
    <section class="grp">
      <div class="sec-label">Actions</div>
      <div class="card">
        <button class="row act" onclick={(e) => pick(e, confirmNameHeir)} disabled={!eligible.length}>
          <span class="chip"><Icon name="crown" size={16} /></span>
          <span class="row-text">
            <span class="row-title">{heir ? "Name a different heir…" : "Name an heir…"}</span>
            <span class="row-sub">A standing, revocable authorization to take over.</span>
          </span>
          <span class="chev">›</span>
        </button>
        <button
          class="row act danger"
          onclick={(e) => pick(e, confirmTransferOwnership)}
          disabled={!eligible.length}
        >
          <span class="chip danger-chip"><Icon name="door" size={16} /></span>
          <span class="row-text">
            <span class="row-title">Transfer ownership…</span>
            <span class="row-sub">Immediate and one-way — only the new owner can hand it back.</span>
          </span>
          <span class="chev">›</span>
        </button>
      </div>
      {#if !eligible.length}
        <p class="muted tiny">
          There is nobody else in this guild yet. Invite someone first — ownership and the heir
          designation can only go to a current member.
        </p>
      {/if}
    </section>
  {:else if iAmHeir}
    <section class="grp">
      <div class="sec-label">Actions</div>
      <div class="card">
        <button class="row act" onclick={confirmClaimOwnership}>
          <span class="chip"><Icon name="crown" size={16} /></span>
          <span class="row-text">
            <span class="row-title">Take ownership</span>
            <span class="row-sub">You were named heir, so this is yours to use whenever you need it.</span>
          </span>
          <span class="chev">›</span>
        </button>
      </div>
    </section>
  {/if}

  <p class="muted tiny">
    Both are signed governance records, not MLS changes: nobody joins, leaves or re-keys, so
    messages and calls carry straight through a handover.
  </p>
</Modal>

<style>
  .grp {
    display: flex;
    flex-direction: column;
    gap: 7px;
    text-align: left;
  }
  .sec-label {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 0 4px;
  }
  .card {
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  .card > .row + .row {
    border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    width: 100%;
    min-height: 52px;
    padding: 10px 14px;
    background: transparent;
    color: var(--text);
    text-align: left;
    border-radius: 0;
  }
  button.row:hover:not(:disabled) {
    background: var(--bg-3);
  }
  button.row:disabled {
    opacity: 0.55;
  }
  .row-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .row-title {
    font-size: var(--fs-ui);
    font-weight: 600;
    line-height: 1.3;
  }
  .row-sub {
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .chip {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    flex-shrink: 0;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent-hover);
  }
  .danger-chip {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger-text);
  }
  .row.danger .row-title {
    color: var(--danger-text);
  }
  .chev {
    flex-shrink: 0;
    font-size: 20px;
    line-height: 1;
    color: var(--text-faint);
  }
  .small {
    padding: 5px 11px;
    font-size: var(--fs-small);
    flex: none;
  }
  .tiny {
    margin: 0;
    font-size: var(--fs-small);
    line-height: 1.5;
  }
</style>
