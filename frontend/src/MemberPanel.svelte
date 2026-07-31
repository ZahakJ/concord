<script>
  // Right panel: guild members (with the fingerprint-verification card) and
  // the "local peers" network log. Rows are proper buttons; the owner's kick
  // control is its own sibling button (no more button-in-button nesting).
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import {
    S,
    activeGuild,
    refreshRightPanel,
    flash,
    openProfilePopover,
    openContextMenu,
    roleColorFor,
    inviteToCall,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { longpress } from "./lib/touch.js";

  // Touch: long-press opens the member menu (iOS never synthesizes contextmenu
  // for plain elements, and Android's synthesized one would double-fire
  // alongside the longpress action).
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  let showPeers = $state(false);

  const g = $derived(activeGuild());

  // Someone already in the call we're in doesn't need inviting.
  const inThisCall = (fpr) =>
    Object.values(S.voiceRosters[S.voice?.channelId] || {}).some((p) => p.fingerprint === fpr);

  function memberMenu(e, mem) {
    openContextMenu(e, [
      { label: "View Profile", icon: "spark", onClick: () => openProfilePopover(mem.fingerprint, e.target) },
      // Only while you're actually in a call, and only for someone who isn't
      // already in it — otherwise it's an option that can't mean anything.
      !!S.voice &&
        !mem.isSelf &&
        !inThisCall(mem.fingerprint) && {
          label: "Invite to call",
          icon: "phone",
          onClick: () => inviteToCall(mem.fingerprint),
        },
      {
        label: "Copy User ID",
        icon: "check",
        onClick: () => {
          navigator.clipboard?.writeText(mem.fingerprint);
          flash("Copied user ID", "success");
        },
      },
      g?.canManage && !mem.isSelf && !mem.isOwner && { sep: true },
      g?.canManage &&
        !mem.isSelf &&
        !mem.isOwner && {
          label: "Remove from Guild",
          icon: "close",
          danger: true,
          onClick: () => kick(mem),
        },
    ]);
  }

  // A member's highest-ranked role (roles are highest-first), for a badge.
  function topRole(mem) {
    if (!mem.roleIds?.length) return null;
    return S.roles.find((r) => mem.roleIds.includes(r.id)) || null;
  }

  const sortMembers = (ms) =>
    [...ms].sort(
      (a, b) =>
        (b.online ? 1 : 0) - (a.online ? 1 : 0) ||
        (a.name || a.fingerprint).localeCompare(b.name || b.fingerprint),
    );

  // Group members under their highest role (Discord-style hoisting), roles
  // highest-position first, with a "Members" catch-all for the roleless.
  const memberGroups = $derived.by(() => {
    const roles = [...S.roles].sort((a, b) => b.position - a.position);
    const bucket = new Map();
    const noRole = [];
    for (const m of S.members) {
      const r = topRole(m);
      if (r) (bucket.get(r.id) || bucket.set(r.id, []).get(r.id)).push(m);
      else noRole.push(m);
    }
    const out = [];
    for (const r of roles) {
      const ms = bucket.get(r.id);
      if (ms?.length) out.push({ id: r.id, name: r.name, color: r.color, members: sortMembers(ms) });
    }
    if (noRole.length) out.push({ id: "", name: "Members", color: "", members: sortMembers(noRole) });
    return out;
  });

  // Kicking ejects a member — confirm first (a hover-revealed danger button is
  // one stray click otherwise), matching the profile popover's flow.
  function kick(mem) {
    const name = mem.name || mem.fingerprint.slice(0, 9);
    // A pending member isn't in the group yet — "removing" them just cancels the
    // invite you sent, no confirm needed.
    if (mem.pending) {
      api
        .cancelPendingMember(S.activeGuildId, mem.fingerprint)
        .then(() => refreshRightPanel())
        .then(() => flash("Invite canceled"))
        .catch(flash);
      return;
    }
    S.modal = {
      kind: "confirm",
      title: `Remove ${name}?`,
      body: `${name} will be removed from this guild. They can rejoin with a new invite unless you ban them.`,
      confirmLabel: "Remove",
      onConfirm: async () => {
        S.modal = null;
        try {
          await api.removeMember(S.activeGuildId, mem.fingerprint);
          await refreshRightPanel();
          flash("Member removed");
        } catch (err) {
          flash(err);
        }
      },
    };
  }
</script>

<aside class="panel">
  {#each memberGroups as grp (grp.id)}
    <div class="section-head">
      <span style={grp.color ? `color:${grp.color}` : ""}>{grp.name} — {grp.members.length}</span>
    </div>
    {#each grp.members as mem (mem.fingerprint)}
      <div class="member-row">
      <button
        class="member"
        class:offline={!mem.online}
        class:pending={mem.pending}
        onclick={(e) => openProfilePopover(mem.fingerprint, e.currentTarget)}
        oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => memberMenu(e, mem)}
        use:longpress={{ handler: (e) => memberMenu(e, mem) }}
      >
        <Avatar
          name={mem.name || mem.fingerprint}
          emoji={mem.emoji}
          color={mem.color}
          image={mem.avatar}
          size={30}
          online={mem.online}
          presence={mem.presence}
          frame={mem.frame}
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
              <!-- A role colour is picked to be seen, not to be read: painting
                   it on a 22% tint of ITSELF leaves a pale role invisible. Same
                   trick as --danger-text — blend the label toward --text, so
                   whatever colour the guild chose still resolves as writing. -->
              {@const rc = r.color || "var(--text-muted)"}
              <span
                class="role-badge"
                style="background:color-mix(in srgb, {rc} 22%, transparent); color:color-mix(in srgb, {rc} 66%, var(--text))"
                title={r.name}>{r.name}</span>
            {/if}
            {#if mem.verified && !mem.isSelf && !mem.pending}
              <span class="v-badge" title="Identity verified"><Icon name="check" size={11} /></span>
            {/if}
            {#if mem.pending}
              <span class="pending-badge" title="Added — they'll appear once they accept &amp; sync">pending</span>
            {/if}
            {#if mem.mutedUntil > Date.now() / 1000}
              <span class="muted-badge" title="Muted"><Icon name="micOff" size={11} /></span>
            {/if}
          </span>
          <!-- Sidebar one-liner: live activity wins (Discord-style); the custom
               status still lives on the expanded profile card. -->
          {#if mem.activity}
            <span class="muted member-status listening">
              <span class="eq" aria-label="Listening"><i></i><i></i><i></i></span>
              {mem.activity.artist ? `${mem.activity.artist} — ${mem.activity.title}` : mem.activity.title}
            </span>
          {:else if mem.status}
            <span class="muted member-status">{mem.status}</span>
          {/if}
        </span>
      </button>
        {#if g?.canManage && !mem.isSelf && !mem.isOwner}
          <button class="kick" title={mem.pending ? "Cancel invite" : "Remove from guild"} aria-label="{mem.pending ? 'Cancel invite for' : 'Remove'} {mem.name || 'member'}" onclick={() => kick(mem)}>
            <Icon name="close" size={12} />
          </button>
        {/if}
      </div>
    {/each}
  {/each}

  <div class="section-head">
    <span>Local peers</span>
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
    <!-- Your own devices are not "nodes your device has connected to" in the
         sense this log means, and they now get a proper section of their own in
         Stats & diagnostics. Left in, your phone appeared here as your own
         fingerprint, which reads as a stranger who has somehow got your key. -->
    {#each S.contacts.filter((c) => c.fingerprint !== S.identity.fingerprint) as c (c.peerId)}
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
    /* No safe-area inset: the right-hand drawer that hosts this on a phone pads
       its own bottom, so repeating it here reserved the home indicator twice. */
    padding: 12px 8px;
    overflow-y: auto;
    /* A fling that runs out of member list stops there instead of dragging the
       message feed visible past the open drawer's edge. */
    overscroll-behavior: contain;
    /* Spinning avatar rings overflow their box by design — don't let that
       summon a horizontal scrollbar (see ChannelList .scroll). */
    overflow-x: clip;
  }
  .add-people {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    width: 100%;
    padding: 8px;
    margin-bottom: 8px;
    background: var(--accent-soft);
    color: var(--accent-hover);
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
    border-radius: var(--radius-md);
    font-size: var(--fs-ui);
    font-weight: 600;
    transition:
      background 0.13s ease,
      transform 0.1s ease;
  }
  @media (pointer: fine) {
    .add-people:hover {
      background: color-mix(in srgb, var(--accent) 22%, transparent);
    }
  }
  .add-people:active {
    transform: scale(0.98);
  }
  .section-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    text-transform: uppercase;
    font-size: var(--fs-tiny);
    letter-spacing: 0.07em;
    font-weight: 700;
    color: var(--text-muted);
    margin: 12px 8px 4px;
  }
  .member-row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
    transition:
      background 0.15s ease,
      transform 0.15s ease;
  }
  /* Hover is a mouse state; on touch a tap leaves it stuck on the row behind
     you, which reads as a selection you cannot clear. */
  @media (pointer: fine) {
    .member-row:hover {
      background: var(--bg-3);
      transform: translateX(2px);
    }
  }
  .member-row:active {
    background: var(--bg-3);
  }
  @media (prefers-reduced-motion: reduce) {
    .member-row:hover {
      transform: none;
    }
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
  .member:hover,
  .member:active {
    background: transparent;
  }
  /* Offline members recede (Discord-style) so the online roster reads first;
     hovering or focusing a row brings the person fully back. */
  .member.offline {
    opacity: 0.62;
    transition: opacity 0.15s ease;
  }
  .member-row:hover .member.offline,
  .member.offline:focus-visible {
    opacity: 1;
  }
  @media (prefers-reduced-motion: reduce) {
    .member.offline {
      transition: none;
    }
  }
  /* Finger-sized rows in the mobile members drawer. */
  @media (pointer: coarse) {
    .member {
      min-height: 46px;
      padding: 8px 10px;
      font-size: var(--fs-body);
    }
    /* Touch never hovers/focuses, so the recede-then-restore can't kick in —
       keep offline members readable (a gentler dim than the desktop 0.62). */
    .member.offline {
      opacity: 0.8;
    }
  }
  .member-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    font-size: var(--fs-ui);
  }
  .member-name {
    overflow: hidden;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
  }
  /* The name itself ellipsizes (text-overflow doesn't work on the flex parent);
     the role/verified badges keep their size instead of being shoved out. */
  .member-name .mname {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .member-name .role-badge,
  .member-name .v-badge {
    flex-shrink: 0;
  }
  .member-status {
    font-size: var(--fs-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Listening members get a tiny live equalizer instead of a static emoji.
     Only playing members animate — not a list-wide loop. */
  .member-status.listening {
    color: color-mix(in srgb, var(--accent) 70%, var(--text-muted));
  }
  .eq {
    display: inline-flex;
    align-items: flex-end;
    gap: 1.5px;
    height: 9px;
    margin-right: 3px;
    color: var(--accent-hover);
  }
  .eq i {
    width: 2px;
    border-radius: 1px;
    background: currentColor;
    height: 30%;
    animation: eq-bounce 1s ease-in-out infinite;
  }
  .eq i:nth-child(2) {
    animation-delay: 0.25s;
  }
  .eq i:nth-child(3) {
    animation-delay: 0.5s;
  }
  @keyframes eq-bounce {
    0%,
    100% {
      height: 30%;
    }
    50% {
      height: 100%;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .eq i {
      animation: none;
      height: 60%;
    }
  }
  .v-badge {
    color: var(--ok-text);
    display: inline-grid;
    place-items: center;
    animation: v-pop 0.3s cubic-bezier(0.34, 1.56, 0.64, 1) both;
  }
  @keyframes v-pop {
    from {
      transform: scale(0.3) rotate(-30deg);
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .v-badge {
      animation: none;
    }
  }
  .muted-badge {
    color: var(--danger-text);
    display: inline-grid;
    place-items: center;
  }
  /* 9px uppercase with positive tracking has a cap-height of about 6.5px: on
     --text-muted over --bg-1 it reads as a coloured smudge rather than a word,
     and this panel IS the phone's right drawer. The token holds the desktop
     size and grows it on touch; the tracking comes off there entirely. */
  .role-badge {
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    padding: 1px 5px;
    border-radius: 7px;
    font-weight: 600;
    flex-shrink: 0;
  }
  /* Pending: added, not yet joined — dim the row and tag it, so it reads as
     "on the way" rather than a normal offline member. */
  .member.pending {
    opacity: 0.6;
  }
  .pending-badge {
    font-size: var(--fs-micro);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    padding: 1px 5px;
    border-radius: 7px;
    font-weight: 600;
    flex-shrink: 0;
    background: color-mix(in srgb, var(--warn) 22%, transparent);
    color: var(--warn-text);
  }
  .role-badge.owner {
    background: color-mix(in srgb, var(--accent) 22%, transparent);
    /* Not --accent-hover. That is the accent's ink on a PLAIN surface; here the
       surface is a 22% wash of the accent itself, so the two sit far too close —
       3.3:1 in light mode and under 4.5 in eight of the packs. Blending toward
       --text pushes it to whichever end of the scale the current theme's page
       isn't, which is the same trick --danger-text uses and the only one that
       holds across dark, light and every pack at once. */
    color: color-mix(in srgb, var(--accent) 50%, var(--text));
  }
  .kick {
    background: transparent;
    color: var(--danger-text);
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
  /* Hover doesn't exist on touch — keep the control visible but quiet (same
     treatment as ChannelList's always-visible affordances). */
  @media (pointer: coarse), (max-width: 768px) {
    .kick {
      opacity: 0.55;
      padding: 8px 10px;
      position: relative;
    }
    /* Removing someone is destructive and the glyph is 12px — the hit box has
       to be worth aiming at, and it grows rightward into the panel padding so
       it never eats into the row it sits beside. */
    .kick::after {
      content: "";
      position: absolute;
      inset: -6px -6px -6px 0;
    }
    .role-badge,
    .pending-badge {
      letter-spacing: 0;
      padding: 2px 6px;
    }
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
    font-size: var(--fs-small);
  }
  .badge {
    font-size: var(--fs-small);
    padding: 2px 8px;
    border-radius: 10px;
  }
  .badge.verified {
    background: var(--ok-soft);
    color: var(--ok-text);
  }
  .small {
    font-size: var(--fs-compact);
    padding: 6px 8px;
  }
  .mini {
    padding: 2px 6px;
    background: transparent;
    color: var(--text-muted);
  }
  .mini:hover,
  .mini:active {
    background: var(--bg-3);
    color: var(--text);
  }
  /* Touch: invisible overlay pads the small expand/collapse toggle's tap
     area out to ~44px without growing the glyph. */
  @media (pointer: coarse), (max-width: 768px) {
    .mini {
      position: relative;
    }
    .mini::after {
      content: "";
      position: absolute;
      inset: -13px;
    }
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
