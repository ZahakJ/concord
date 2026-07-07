<script>
  // Right panel: guild members (with the fingerprint-verification card) and
  // the "known peers" network log. Rows are proper buttons; the owner's kick
  // control is its own sibling button (no more button-in-button nesting).
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import {
    S,
    activeGuild,
    refreshRightPanel,
    flash,
    openProfilePopover,
    roleColorFor,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";

  let showPeers = $state(false);

  const g = $derived(activeGuild());

  // A member's highest-ranked role (roles are highest-first), for a badge.
  function topRole(mem) {
    if (!mem.roleIds?.length) return null;
    return S.roles.find((r) => mem.roleIds.includes(r.id)) || null;
  }

  async function kick(fingerprint) {
    try {
      await api.removeMember(S.activeGuildId, fingerprint);
      await refreshRightPanel();
      flash("Member removed");
    } catch (err) {
      flash(err);
    }
  }
</script>

<aside class="panel">
  <div class="section-head"><span>Members — {g?.name ?? ""}</span></div>
  {#each S.members as mem (mem.fingerprint)}
    <div class="member-row">
      <button class="member" onclick={(e) => openProfilePopover(mem.fingerprint, e.currentTarget)}>
        <Avatar
          name={mem.name || mem.fingerprint}
          emoji={mem.emoji}
          color={mem.color}
          image={mem.avatar}
          size={30}
          online={mem.online}
          presence={mem.presence}
        />
        <span class="member-text">
          <span class="member-name" title={mem.fingerprint}>
            <span
              class="mname"
              style={roleColorFor(mem.fingerprint) ? `color:${roleColorFor(mem.fingerprint)}` : ""}
            >{mem.name || mem.fingerprint.slice(0, 9)}</span>{mem.isSelf ? " (you)" : ""}
            {#if mem.isOwner}
              <span class="role-badge owner" title="Guild owner">owner</span>
            {:else if topRole(mem)}
              {@const r = topRole(mem)}
              <span
                class="role-badge"
                style="background:color-mix(in srgb, {r.color || 'var(--text-faint)'} 22%, transparent); color:{r.color || 'var(--text-muted)'}"
                title={r.name}>{r.name}</span>
            {/if}
            {#if mem.verified && !mem.isSelf}
              <span class="v-badge" title="Identity verified"><Icon name="check" size={11} /></span>
            {/if}
          </span>
          {#if mem.status}<span class="muted member-status">{mem.status}</span>{/if}
        </span>
      </button>
      {#if g?.canManage && !mem.isSelf && !mem.isOwner}
        <button class="kick" title="Remove from guild" aria-label="Remove {mem.name || 'member'} from guild" onclick={() => kick(mem.fingerprint)}>
          <Icon name="close" size={12} />
        </button>
      {/if}
    </div>
  {/each}

  <div class="section-head">
    <span>Known peers</span>
    <button class="mini" aria-label={showPeers ? "Collapse" : "Expand"} onclick={() => (showPeers = !showPeers)}>
      <span class="chev" class:open={showPeers}><Icon name="chevron" size={11} /></span>
    </button>
  </div>
  {#if showPeers}
    <p class="muted small peers-info">
      Every Concord node your device has ever connected to — including strangers on the same
      Wi-Fi discovered automatically. They can't read anything (messages are end-to-end
      encrypted); this is just a network log. To verify a <em>friend</em>, click them in the
      Members list above.
    </p>
    {#each S.contacts as c (c.peerId)}
      <div class="contact">
        <span class="mono peer-fpr" title={c.peerId}>{c.fingerprint.slice(0, 19)}…</span>
        {#if c.verified}<span class="badge verified">✓</span>{/if}
      </div>
    {:else}
      <div class="muted small">No peers seen yet.</div>
    {/each}
  {/if}
</aside>

<style>
  .panel {
    background: var(--bg-1);
    border-left: 1px solid var(--border);
    padding: 12px 8px;
    overflow-y: auto;
  }
  .section-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    text-transform: uppercase;
    font-size: 11px;
    letter-spacing: 0.05em;
    color: var(--text-muted);
    margin: 10px 8px 4px;
  }
  .member-row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
  }
  .member-row:hover {
    background: var(--bg-3);
  }
  .member {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 6px 8px;
    min-width: 0;
  }
  .member:hover {
    background: transparent;
  }
  .member-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: 13px;
  }
  .member-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .member-status {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .v-badge {
    color: var(--ok);
    display: inline-grid;
    place-items: center;
  }
  .role-badge {
    font-size: 9px;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    padding: 1px 5px;
    border-radius: 7px;
    font-weight: 600;
    flex-shrink: 0;
  }
  .role-badge.owner {
    background: color-mix(in srgb, var(--accent) 22%, transparent);
    color: var(--accent);
  }
  .role-badge.mod {
    background: color-mix(in srgb, var(--ok) 20%, transparent);
    color: var(--ok);
  }
  .kick {
    background: transparent;
    color: var(--danger);
    padding: 4px 8px;
    opacity: 0;
  }
  .member-row:hover .kick,
  .kick:focus-visible {
    opacity: 1;
  }
  .kick:hover {
    background: var(--danger-soft);
  }
  .peers-info {
    padding: 4px 8px;
    line-height: 1.45;
    margin: 0;
  }
  .contact {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    word-break: break-all;
  }
  .peer-fpr {
    font-size: 11px;
  }
  .badge {
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 10px;
  }
  .badge.verified {
    background: var(--ok-soft);
    color: var(--ok);
  }
  .small {
    font-size: 12px;
    padding: 6px 8px;
  }
  .mini {
    padding: 2px 6px;
    background: transparent;
    color: var(--text-muted);
  }
  .mini:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .chev {
    display: inline-grid;
    place-items: center;
    transition: transform 0.15s ease;
  }
  .chev.open {
    transform: rotate(90deg);
  }
</style>
