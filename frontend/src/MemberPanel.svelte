<script>
  // Right panel: guild members (with the fingerprint-verification card) and
  // the "local peers" network log. Rows are proper buttons; the owner's kick
  // control is its own sibling button (no more button-in-button nesting).
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import StoryTray from "./StoryTray.svelte";
  import {
    S,
    activeGuild,
    refreshRightPanel,
    refreshGuilds,
    flash,
    openProfilePopover,
    openContextMenu,
    roleColorFor,
    inviteToCall,
    isBlocked,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { longpress } from "./lib/touch.js";
  import { splitStatus } from "./lib/presence.js";
  import { gameHue } from "./GameShelf.svelte";
  import { moderationItems, confirmKick } from "./lib/moderation.svelte.js";
  import {
    confirmTransferOwnership,
    confirmNameHeir,
    revokeHeir,
    confirmClaimOwnership,
  } from "./lib/ownership.svelte.js";

  // Touch: long-press opens the member menu (iOS never synthesizes contextmenu
  // for plain elements, and Android's synthesized one would double-fire
  // alongside the longpress action).
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  let showPeers = $state(false);

  const g = $derived(activeGuild());

  // Someone already in the call we're in doesn't need inviting.
  const inThisCall = (fpr) =>
    Object.values(S.voiceRosters[S.voice?.channelId] || {}).some((p) => p.fingerprint === fpr);

  // ONE moderation menu. Right-clicking a member is the natural gesture and it
  // used to be the INCOMPLETE surface: no ban, no mute, while the full set hid
  // behind an unlabelled dots button painted over the profile card's art. The
  // shared list from lib/moderation.svelte.js is now what both render, so the
  // two cannot drift apart again.
  function memberMenu(e, mem) {
    openContextMenu(e, [
      { label: "View profile", icon: "spark", onClick: () => openProfilePopover(mem.fingerprint, e.target) },
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
        label: "Copy user ID",
        icon: "check",
        onClick: () => {
          navigator.clipboard?.writeText(mem.fingerprint);
          flash("Copied user ID", "success");
        },
      },
      { sep: true },
      ...moderationItems(mem),
      { sep: true },
      // Only the sitting owner can hand the crown over, and only to a member
      // who's actually landed (a pending row isn't in the group yet).
      g?.isOwner &&
        !mem.isSelf &&
        !mem.isOwner &&
        !mem.pending && {
          label: "Transfer ownership…",
          icon: "crown",
          danger: true,
          onClick: () => confirmTransferOwnership(mem),
        },
      // Succession: name (or unname) an heir. Same eligibility as a transfer —
      // the crown can only ever land on a member who's actually here.
      g?.isOwner &&
        !mem.isSelf &&
        !mem.isOwner &&
        !mem.pending &&
        (mem.isHeir
          ? { label: "Revoke heir", icon: "crown", onClick: () => revokeHeir(mem) }
          : { label: "Name as heir…", icon: "crown", onClick: () => confirmNameHeir(mem) }),
    ]);
  }

  // "Name an heir" from the nudge: reuse the context-menu idiom as a picker —
  // same surface the member row's own menu uses, anchored at the button.
  function pickHeir(e) {
    const eligible = S.members.filter((m) => !m.isSelf && !m.pending);
    openContextMenu(
      e,
      eligible.map((m) => ({
        label: m.name || m.fingerprint.slice(0, 9),
        icon: "crown",
        onClick: () => confirmNameHeir(m),
      })),
    );
  }

  // ---- sole-admin freeze warning ----
  // The owner is the guild's permission ROOT. If they vanish while nobody
  // else holds manage-members, no one can ever add or remove a member again —
  // the guild freezes. Another admin OR a named heir defuses that, so the
  // nudge only shows while NEITHER exists (and there's actually someone to
  // promote). Dismissal is per guild and purely local.
  let heirNudgeDismissed = $state(
    JSON.parse(localStorage.getItem("heirNudgeDismissed") || "{}"),
  );
  function dismissNudge() {
    heirNudgeDismissed[S.activeGuildId] = true;
    localStorage.setItem("heirNudgeDismissed", JSON.stringify(heirNudgeDismissed));
  }
  const soleAdminRisk = $derived(
    !!g &&
      g.kind !== "dm" &&
      g.isOwner &&
      !g.heir &&
      !heirNudgeDismissed[g.id] &&
      S.members.some((m) => !m.isSelf && !m.pending) &&
      !S.members.some((m) => !m.isSelf && !m.pending && m.canManage),
  );

  // The heir's own affordance: visible only to the named heir, who may not
  // otherwise know (or remember) they hold the break-glass.
  const iAmHeir = $derived(
    !!g && g.kind !== "dm" && !g.isOwner && g.heir === S.identity.fingerprint,
  );

  // "🎮 <name>" is the GameShelf's now-playing convention (its Play button
  // writes it as a plain custom status, so old builds render it fine as-is).
  // Here it's promoted to a "Playing <name>" line in the game's tint.
  function playingGame(status) {
    const { emoji, text } = splitStatus(status);
    return emoji === "🎮" && text ? text : "";
  }

  // The 🎂 is a per-VIEWER render: the member's "MM-DD" (no year — the backend
  // refuses to store one) against THIS client's local clock, decided
  // independently on every screen. Nothing is posted or announced on the
  // member's behalf, so viewers across timezones may briefly disagree.
  function birthdayToday(mem) {
    if (!mem.birthday) return false;
    const now = new Date();
    return mem.birthday === `${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
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

  // Group members under their highest role (role hoisting), roles
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
    // The owner outranks every role, so they cannot be filed UNDER one. Role
    // hoisting put the person who owns the guild in "Members", beneath their
    // own moderator, whenever they had not given themselves a role — which is
    // the normal state, since the owner needs none. They get their own group,
    // with the heir, pinned above everything.
    const top = [];
    for (const grp of out) {
      grp.members = grp.members.filter((m) => {
        if (m.isOwner || m.isHeir) {
          top.push(m);
          return false;
        }
        return true;
      });
    }
    if (top.length) {
      // Owner first, then heir; both are one person's standing, not a role.
      top.sort((a, b) => (b.isOwner ? 1 : 0) - (a.isOwner ? 1 : 0));
      out.unshift({ id: "__top", name: top.length > 1 ? "Owner & heir" : "Owner", color: "", members: top });
    }
    return out.filter((grp) => grp.members.length);
  });

  // ---- member filter ----
  // Only worth its pixels once the roster outgrows a glance — below ~15 rows
  // scanning is faster than typing.
  let memberFilter = $state("");
  const showFilter = $derived(S.members.length > 15);
  const filtering = $derived(!!memberFilter.trim());

  // A query typed against one guild's roster means nothing in the next one's —
  // carrying it over would greet you with a mysteriously empty panel.
  $effect(() => {
    S.activeGuildId;
    memberFilter = "";
  });

  // Matches what the row actually shows: the display name (fingerprint when
  // nameless) and the status one-liner under it. Groups keep their full count
  // so headers can say "n of m"; emptied groups drop out entirely.
  const filteredGroups = $derived.by(() => {
    const q = memberFilter.trim().toLowerCase();
    const groups = memberGroups.map((grp) => ({ ...grp, total: grp.members.length }));
    if (!q) return groups;
    return groups
      .map((grp) => ({
        ...grp,
        members: grp.members.filter(
          (m) =>
            (m.name || m.fingerprint).toLowerCase().includes(q) ||
            m.status?.toLowerCase().includes(q),
        ),
      }))
      .filter((grp) => grp.members.length);
  });

  function filterKeydown(e) {
    if (e.key !== "Escape") return;
    // Escape clears the filter instead of climbing the global Escape ladder
    // (drawer/popover close) — but only while there's something to clear, so a
    // second press still backs you out of the panel.
    if (memberFilter) {
      e.stopPropagation();
      memberFilter = "";
    }
  }

  // Kicking ejects a member — confirm first (a hover-revealed danger button is
  // one stray click otherwise), through the one shared confirm every surface
  // uses. The pending case is genuinely different and stays here: an invitee
  // who has not joined is not in the group, so "removing" them cancels the
  // invite rather than issuing a governance record about a member.
  function kick(mem) {
    if (mem.pending) {
      api
        .cancelPendingMember(S.activeGuildId, mem.fingerprint)
        .then(() => refreshRightPanel())
        .then(() => flash("Invite cancelled"))
        .catch(flash);
      return;
    }
    confirmKick(mem);
  }
</script>

<aside class="panel">
  {#if activeGuild()?.evicted}
    <!-- The roster a removed device holds is the one it had at the moment it was
         removed, frozen: it cannot apply the commit that took it out, so the list
         still shows itself and everyone who was there. Printing that under a live
         "MEMBERS — 3" heading is the same lie the composer was telling. We do not
         know who is in this guild any more, and this says so. -->
    <p class="gone-note">
      You can't see who's in <strong>{activeGuild()?.name}</strong> any more. The
      history below is your own copy and stays on this device.
    </p>
  {:else}
  <!-- Moments tray rides the top of the roster. It hides itself for DMs and
       owns all its data/refresh logic — this panel just gives it the slot. -->
  <StoryTray />
  {#if soleAdminRisk}
    <div class="notice warn" role="note">
      <div class="notice-head">
        <Icon name="crown" size={13} />
        <strong>You're this guild's only admin</strong>
        <button class="notice-x" aria-label="Dismiss warning" onclick={dismissNudge}>
          <Icon name="close" size={11} />
        </button>
      </div>
      <p>
        If you lose this account, nobody will be able to add or remove members again. Give
        someone a role with “manage members”, or name an heir who can take over.
      </p>
      <div class="notice-actions">
        <button class="notice-btn" onclick={() => (S.modal = { kind: "roles" })}>Open roles</button>
        <button class="notice-btn" onclick={pickHeir}>Name an heir…</button>
      </div>
    </div>
  {/if}
  {#if iAmHeir}
    <div class="notice accent" role="note">
      <div class="notice-head">
        <Icon name="crown" size={13} />
        <strong>You're the named heir</strong>
      </div>
      <p>The owner authorized you to take over this guild — for when they're gone, or ask you to.</p>
      <div class="notice-actions">
        <button class="notice-btn" onclick={confirmClaimOwnership}>Take ownership…</button>
      </div>
    </div>
  {/if}
  {#if showFilter}
    <div class="filter-wrap">
      <input
        class="member-filter"
        type="text"
        placeholder="Filter members…"
        aria-label="Filter members by name or status"
        bind:value={memberFilter}
        onkeydown={filterKeydown}
      />
      {#if memberFilter}
        <button class="filter-x" aria-label="Clear filter" onclick={() => (memberFilter = "")}>
          <Icon name="close" size={11} />
        </button>
      {/if}
    </div>
  {/if}
  {#each filteredGroups as grp (grp.id)}
    <div class="section-head">
      <span style={grp.color ? `color:${grp.color}` : ""}
        >{grp.name} — {filtering ? `${grp.members.length} of ${grp.total}` : grp.total}</span>
    </div>
    {#each grp.members as mem (mem.fingerprint)}
      <div class="member-row" data-menu-row>
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
          decoration={mem.style?.dec || ""}
          dc={mem.style?.dc || ""}
        />
        <span class="member-text">
          <span class="member-name" title={mem.fingerprint}>
            <span
              class="mname"
              style={roleColorFor(mem.fingerprint) ? `color:${roleColorFor(mem.fingerprint)}` : ""}
            >{mem.name || mem.fingerprint.slice(0, 9)}</span>{mem.isSelf ? " (you)" : ""}
            {#if birthdayToday(mem)}
              <span class="bday" title="Birthday today">🎂</span>
            {/if}
            {#if mem.isOwner}
              <span class="role-badge owner" title="Guild owner">owner</span>
            {:else if mem.isHeir}
              <span class="role-badge heir" title="Named heir — can take ownership at any time">heir</span>
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
            {#if isBlocked(mem.fingerprint)}
              <span class="blocked-badge" title="Blocked on this device — you don't see what they post">
                <Icon name="eyeOff" size={11} />
              </span>
            {/if}
          </span>
          <!-- Sidebar one-liner: live activity wins; the custom
               status still lives on the expanded profile card.
               A blocked member gets none of it. The status line is a string
               THEY wrote, refreshed whenever they like, and leaving it here
               after hiding everything else they say would make it the one
               sentence a blocked person is guaranteed to get in front of you.
               Saying why the row is quiet is more use than their status. -->
          {#if isBlocked(mem.fingerprint)}
            <span class="muted member-status">Blocked</span>
          {:else if mem.activity}
            <span class="muted member-status listening">
              <span class="eq" aria-label="Listening"><i></i><i></i><i></i></span>
              {mem.activity.artist ? `${mem.activity.artist} — ${mem.activity.title}` : mem.activity.title}
            </span>
          {:else if playingGame(mem.status)}
            {@const game = playingGame(mem.status)}
            <span class="muted member-status playing" style="--game-tint:hsl({gameHue(game)} 45% 55%)">
              <Icon name="play" size={9} /> Playing {game}
            </span>
          {:else if mem.status}
            <span class="muted member-status">{mem.status}</span>
          {/if}
        </span>
      </button>
        {#if g?.canManage && !mem.isSelf && !mem.isOwner}
          <button class="kick" title={mem.pending ? "Cancel invite" : "Kick from guild"} aria-label="{mem.pending ? 'Cancel invite for' : 'Kick'} {mem.name || 'member'}" onclick={() => kick(mem)}>
            <Icon name="close" size={12} />
          </button>
        {/if}
      </div>
    {/each}
  {:else}
    {#if filtering}
      <div class="muted small">No members match “{memberFilter.trim()}”.</div>
    {/if}
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
  {/if}
</aside>

<style>
  .gone-note {
    margin: 0;
    padding: var(--sp-4) var(--sp-3);
    font-size: var(--fs-compact);
    line-height: 1.55;
    color: var(--text-muted);
  }

  .panel {
    background: var(--bg-1);
    border-left: 1px solid var(--border);
    /* No safe-area inset: the right-hand drawer that hosts this on a phone pads
       its own bottom, so repeating it here reserved the home indicator twice. */
    padding: var(--sp-3) var(--sp-2);
    overflow-y: auto;
    /* A fling that runs out of member list stops there instead of dragging the
       message feed visible past the open drawer's edge. */
    overscroll-behavior: contain;
    /* Spinning avatar rings overflow their box by design — don't let that
       summon a horizontal scrollbar (see ChannelList .scroll). */
    overflow-x: clip;
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
    margin: var(--sp-3) var(--sp-2) var(--sp-1);
  }
  /* The filter is background chrome like the rest of this panel — a quiet
     recessed well, no accent until the field itself is focused (the global
     input focus style provides that). */
  .filter-wrap {
    position: relative;
    margin: 8px 4px 0;
  }
  .member-filter {
    background: var(--bg-2);
    border-color: transparent;
    border-radius: var(--radius-md);
    font-size: var(--fs-ui);
    /* Compact: the global 11px/14px input padding is dialog-sized; leave room
       on the right for the clear X so text never runs under it. */
    padding: 6px 26px 6px 10px;
    box-shadow: none;
  }
  @media (pointer: fine) {
    .member-filter:hover:not(:focus) {
      background: var(--bg-3);
      border-color: transparent;
    }
  }
  .member-filter:focus {
    background: var(--bg-3);
  }
  .filter-x {
    position: absolute;
    right: 4px;
    top: 50%;
    transform: translateY(-50%);
    padding: 3px 5px;
    background: transparent;
    color: var(--text-muted);
    display: inline-grid;
    place-items: center;
  }
  .filter-x:hover,
  .filter-x:active {
    background: transparent;
    color: var(--text);
  }
  /* Finger-sized field in the mobile drawer; the X pads its hit box out
     without growing the glyph. */
  @media (pointer: coarse) {
    .member-filter {
      padding: 9px 30px 9px 12px;
    }
    .filter-x::after {
      content: "";
      position: absolute;
      inset: -10px;
    }
  }
  .member-row {
    display: flex;
    align-items: center;
    border-radius: var(--radius-sm);
    transition:
      background var(--dur-standard) ease,
      transform var(--dur-standard) ease;
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
    gap: var(--sp-2);
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
  /* ---- the row a menu is about ----
     The menu opens at the cursor and says nothing about which row it belongs
     to. `data-menu-target` is stamped by openContextMenu (lib/state.svelte.js)
     on whatever `data-menu-row` the gesture came from, for exactly as long as
     the menu is up. The ground is a step past hover and the hairline is
     something hover never draws, so it reads as "this one, held" and not as a
     stronger hover. :global keeps the compiler from pruning a selector that
     never appears in this file's markup. */
  .member-row:global([data-menu-target]) {
    background: color-mix(in srgb, var(--bg-3) 82%, transparent);
    box-shadow: inset 0 0 0 1px var(--border);
    border-radius: var(--radius-sm);
  }
  /* Offline members recede so the online roster reads first;
     hovering or focusing a row brings the person fully back. */
  .member.offline {
    opacity: 0.62;
    transition: opacity var(--dur-standard) ease;
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
    gap: var(--sp-1);
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
  .member-name .v-badge,
  .member-name .bday {
    flex-shrink: 0;
  }
  .member-name .bday {
    font-size: 12px;
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
  /* Playing members: same treatment as listening, but tinted to the game's
     generated-cover hue (GameShelf's title hash) so each title reads as its
     own colour — blended toward --text-muted so any hue stays legible. */
  .member-status.playing {
    color: color-mix(in srgb, var(--game-tint) 55%, var(--text-muted));
  }
  .member-status.playing :global(svg) {
    vertical-align: -1px;
    margin-right: 1px;
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
    animation: v-pop 0.3s var(--ease-spring) both;
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
  .muted-badge,
  .blocked-badge {
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
    border-radius: var(--radius-sm);
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
    border-radius: var(--radius-sm);
    font-weight: 600;
    flex-shrink: 0;
    background: color-mix(in srgb, var(--warn) 22%, transparent);
    color: var(--warn-text);
  }
  /* Heir: same badge shape as owner, warn-tinted — a standing authorization
     worth noticing, not an alarm. Text blends toward --text like the owner
     badge so it stays readable on the tint in every theme pack. */
  .role-badge.heir {
    background: color-mix(in srgb, var(--warn) 20%, transparent);
    color: color-mix(in srgb, var(--warn) 50%, var(--text));
  }
  /* Inline notices (sole-admin freeze warning, heir's claim card). Calm
     tinted cards, not toasts: they describe a standing state of the guild.
     Tokens only, so they hold in dark/light and every pack; full-width and
     wrap-friendly so they sit fine in the 393px right drawer. */
  .notice {
    margin: var(--sp-1) var(--sp-1) var(--sp-2);
    padding: 10px;
    border-radius: var(--radius-md);
    font-size: var(--fs-small);
    line-height: 1.45;
  }
  .notice.warn {
    background: color-mix(in srgb, var(--warn) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--warn) 30%, transparent);
    color: var(--text);
  }
  .notice.warn .notice-head {
    color: var(--warn-text);
  }
  .notice.accent {
    background: var(--accent-soft);
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
    color: var(--text);
  }
  .notice.accent .notice-head {
    color: color-mix(in srgb, var(--accent) 50%, var(--text));
  }
  .notice-head {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-ui);
    margin-bottom: var(--sp-1);
  }
  .notice-head strong {
    flex: 1;
    min-width: 0;
  }
  .notice p {
    margin: 0 0 8px;
    color: var(--text-muted);
  }
  .notice-x {
    background: transparent;
    color: inherit;
    padding: 2px 4px;
    opacity: 0.7;
    flex-shrink: 0;
  }
  .notice-x:hover {
    opacity: 1;
    background: transparent;
  }
  /* Touch: pad the dismiss X's tap area out without growing the glyph. */
  @media (pointer: coarse) {
    .notice-x {
      position: relative;
    }
    .notice-x::after {
      content: "";
      position: absolute;
      inset: -10px;
    }
  }
  .notice-actions {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .notice-btn {
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text);
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .notice-btn:hover,
  .notice-btn:active {
    background: var(--bg-4, var(--bg-3));
  }
  @media (pointer: coarse) {
    .notice-btn {
      min-height: 38px;
    }
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
    padding: var(--sp-1) var(--sp-2);
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
    padding: var(--sp-1) var(--sp-2);
    line-height: 1.45;
    margin: 0;
  }
  .contact {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--sp-2);
    padding: 6px 8px;
    word-break: break-all;
  }
  .peer-fpr {
    font-size: var(--fs-small);
  }
  .badge {
    font-size: var(--fs-small);
    padding: 2px 8px;
    border-radius: var(--radius-md);
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
  /* Touch: invisible overlay pads the small expand/collapse toggle's tap area
     out to the platform floor without growing the glyph. The glyph box is
     23x18, so a single hardcoded inset can only be right on one axis — it was
     -13px, which reached 44 across and 44 down by coincidence of those two
     numbers and went stale the moment --tap-min moved to Android's 48. The
     calc form measures from the element's own size on each axis, so it is
     correct for both and stays correct if the glyph changes. */
  @media (pointer: coarse), (max-width: 768px) {
    .mini {
      position: relative;
    }
    .mini::after {
      content: "";
      position: absolute;
      inset: calc((var(--tap-min) - 100%) / -2);
    }
  }
  .chev {
    display: inline-grid;
    place-items: center;
    transition: transform var(--dur-standard) ease;
  }
  .chev.open {
    transform: rotate(90deg);
  }
</style>
