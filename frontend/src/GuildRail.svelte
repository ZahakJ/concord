<script>
  // The leftmost rail: a Concord "home" bubble (opens DMs), one bubble per DM
  // and per guild with unread badges, plus create/join.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import GroupAvatar from "./GroupAvatar.svelte";
  import {
    S,
    selectGuild,
    openDMs,
    guildUnread,
    openContextMenu,
    startMeeting,
    guildMenuItems,
  } from "./lib/state.svelte.js";

  // Right-click a server bubble for everything you can do to it — the same list
  // the header's "More" menu shows, permission-gated identically. On touch this
  // becomes the bottom action sheet (long-press), so it isn't desktop-only.
  function guildMenu(e, sv) {
    openContextMenu(e, guildMenuItems(sv), { title: sv.name });
  }

  // Mobile: one + bubble → an action sheet (create / join). Two near-identical
  // mystery bubbles are a desktop-ism; the sheet explains itself.
  function addMenu(e) {
    openContextMenu(
      e,
      [
        { label: "Create a server", icon: "spark", onClick: () => (S.modal = { kind: "create" }) },
        { label: "Join with an invite code", icon: "download", onClick: () => (S.modal = { kind: "join", code: "" }) },
        { label: "Instant meeting (24h room)", icon: "bolt", onClick: startMeeting },
      ],
      { title: "Add a server" },
    );
  }

  // Easter egg: clicking the home logo flies the Concorde through a barrel roll
  // (à la "do a barrel roll") — pure flair on top of opening DMs. Re-armable on
  // every click; the class clears itself on animationend.
  let rolling = $state(false);
  function homeClick() {
    openDMs();
    rolling = false;
    // Reflow so re-clicking mid-idle restarts the animation cleanly.
    requestAnimationFrame(() => (rolling = true));
  }

  const g = $derived(S.guilds.find((x) => x.id === S.activeGuildId) || null);
  const inDMs = $derived(g?.kind === "dm");
  const servers = $derived(S.guilds.filter((x) => x.kind !== "dm"));
  // DM bubbles surface only when they need attention: a DM with unread
  // messages. Everything else (incl. Notes) lives behind the home button.
  const dms = $derived(
    S.guilds.filter(
      (x) =>
        x.kind === "dm" &&
        !x.dmNotes &&
        // Unread DMs surface here; the currently-open DM stays pinned so clicking
        // it (which marks it read) doesn't make the bubble you're in vanish.
        (guildUnread(x).count > 0 || x.id === S.activeGuildId),
    ),
  );

  const initials = (name) =>
    name
      .split(/\s+/)
      .map((w) => w[0])
      .join("")
      .slice(0, 2);

  // Icon-less guilds get a stable per-guild tint (hash of the id) so several
  // of them don't collapse into identical grey circles.
  function guildTint(id) {
    let h = 0;
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
    const hue = h % 360;
    return `background:linear-gradient(135deg, hsl(${hue} 42% 34%), hsl(${(hue + 45) % 360} 48% 25%));color:#fff`;
  }
</script>

<nav class="rail" aria-label="Servers">
  <button
    class="pill home"
    class:active={inDMs}
    title="Direct messages"
    aria-label="Home / Direct messages"
    onclick={homeClick}
  >
    <span class="jet" class:roll={rolling} onanimationend={() => (rolling = false)}>
      <Icon name="concorde" size={24} />
    </span>
  </button>
  <div class="divider"></div>

  {#each dms as dm, i (dm.id)}
    {@const u = guildUnread(dm)}
    <div class="bubble-wrap" style="--i:{i}">
      <button
        class="pill dm"
        class:active={dm.id === S.activeGuildId}
        title={dm.name}
        aria-label={dm.name}
        onclick={() => selectGuild(dm.id)}
      >
        {#if (dm.dmMembers ?? 2) > 2}
          <GroupAvatar faces={dm.dmFaces || []} size={42} />
        {:else}
          <Avatar
            name={dm.name}
            image={dm.dmPeerAvatar || dm.dmFaces?.[0]?.avatar || dm.icon}
            size={42}
            online={dm.dmPeer ? !!dm.dmPeerOnline : null}
            presence={dm.dmPeerPresence || ""}
          />
        {/if}
      </button>
      {#if dm.id !== S.activeGuildId && u.count > 0}
        <span class="badge">{u.count > 99 ? "99+" : u.count}</span>
      {/if}
    </div>
  {/each}

  {#if dms.length}<div class="divider"></div>{/if}

  {#each servers as sv, i (sv.id)}
    {@const u = guildUnread(sv)}
    <div class="bubble-wrap" style="--i:{i}">
      <button
        class="pill"
        class:active={sv.id === S.activeGuildId}
        class:hasicon={sv.icon}
        title={sv.name}
        aria-label={sv.name}
        onclick={() => selectGuild(sv.id)}
        oncontextmenu={(e) => guildMenu(e, sv)}
      >
        {#if sv.icon}
          <img class="icon" src={sv.icon} alt="" />
        {:else}
          <span class="face" style={guildTint(sv.id)}>{initials(sv.name)}</span>
        {/if}
      </button>
      {#if sv.id !== S.activeGuildId && u.count > 0}
        <span class="badge" class:mention={u.mentions > 0}>{u.count > 99 ? "99+" : u.count}</span>
      {/if}
    </div>
  {/each}

  {#if S.isMobile}
    <button class="pill add" title="Add a server" aria-label="Add a server" onclick={addMenu}>
      <Icon name="plus" />
    </button>
    <button
      class="pill add meet"
      title="Instant meeting — a disposable room + invite to send anyone"
      aria-label="Start an instant meeting"
      onclick={startMeeting}
    >
      <Icon name="bolt" />
    </button>
  {:else}
    <button class="pill add" title="Create a guild" aria-label="Create a guild" onclick={() => (S.modal = { kind: "create" })}>
      <Icon name="plus" />
    </button>
    <button class="pill add meet" title="Instant meeting — a disposable room + invite to send anyone" aria-label="Start an instant meeting" onclick={startMeeting}>
      <Icon name="bolt" />
    </button>
    <button class="pill add" title="Join with invite" aria-label="Join with invite" onclick={() => (S.modal = { kind: "join", code: "" })}>
      <Icon name="download" />
    </button>
  {/if}
</nav>

<style>
  .rail {
    background: var(--bg-0);
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    padding: 10px 0;
    overflow-y: auto;
    scrollbar-width: none;
  }
  .rail::-webkit-scrollbar {
    display: none;
  }
  .pill {
    position: relative;
    width: 42px;
    height: 42px;
    border-radius: 50%;
    background: var(--bg-2);
    color: var(--text);
    font-weight: 600;
    font-size: 14px;
    text-transform: uppercase;
    display: grid;
    place-items: center;
    padding: 0;
    transition:
      border-radius 0.18s cubic-bezier(0.2, 0.9, 0.3, 1),
      background 0.18s ease,
      transform 0.12s ease,
      box-shadow 0.18s ease;
    flex-shrink: 0;
  }
  .pill:hover {
    border-radius: 14px;
    background: var(--bg-3);
    transform: translateY(-1px) scale(1.04);
  }
  .pill:active {
    transform: scale(0.92);
  }
  .pill.active {
    border-radius: 14px;
    background: var(--accent);
    color: white;
    box-shadow: var(--accent-glow);
  }
  /* DM/group bubbles stay circular (their avatar is a circle, so the squircle
     hover/active would bleed colored corners around it). Active = an accent
     ring instead. */
  .pill.dm:hover,
  .pill.dm.active {
    border-radius: 50%;
  }
  .pill.dm.active {
    background: var(--bg-2);
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  /* On a DM bubble the unread badge moves to the top corner so it doesn't sit
     on the presence dot (which lives bottom-right). */
  .pill.dm .badge {
    top: -3px;
    bottom: auto;
  }
  /* The home bubble is deliberately not a server: a fixed rounded-square with a
     neutral surface, so it reads as "brand / home" rather than a guild. */
  .pill.home {
    border-radius: 15px;
    background: var(--bg-3);
    color: var(--accent);
  }
  .pill.home:hover {
    border-radius: 15px;
    color: white;
    background: var(--accent);
  }
  /* The jet noses up on hover — a tiny on-brand takeoff. */
  .pill.home :global(svg) {
    transition: transform 0.18s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .pill.home:hover :global(svg) {
    transform: rotate(-10deg) translateY(-1px);
  }
  /* Easter egg: barrel roll on click (see homeClick). The wrapper spins so the
     svg's own hover-nose transform stays independent. */
  .jet {
    display: inline-flex;
    transform-origin: 50% 55%;
  }
  .jet.roll {
    animation: barrel-roll 0.72s cubic-bezier(0.5, 0.05, 0.2, 1);
  }
  @keyframes barrel-roll {
    0% {
      transform: rotate(0) scale(1);
    }
    55% {
      transform: rotate(235deg) scale(1.28);
    }
    100% {
      transform: rotate(360deg) scale(1);
    }
  }
  .pill.home.active {
    border-radius: 15px;
    background: var(--accent);
    color: white;
  }
  .pill .icon {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: inherit;
  }
  /* A pill showing a real icon keeps it edge-to-edge; drop the tinted bg. */
  .pill.hasicon {
    background: var(--bg-2);
    overflow: hidden;
  }
  /* The meeting bolt: a bit of electric identity so it reads as "instant". */
  .pill.meet {
    color: var(--warn);
  }
  .pill.meet:hover {
    background: color-mix(in srgb, var(--warn) 24%, var(--bg-3));
    color: #fff;
  }
  .divider {
    width: 28px;
    height: 2px;
    border-radius: 2px;
    background: var(--border);
    margin: 2px 0;
    flex-shrink: 0;
  }
  .pill.add {
    background: transparent;
    border: 1px dashed var(--border);
    color: var(--text-muted);
  }
  .pill.add :global(svg) {
    transition: transform 0.22s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .pill.add:hover {
    color: var(--accent-hover);
    border-color: var(--accent-hover);
    background: var(--bg-2);
  }
  /* The + winds up a quarter turn on hover — "something new is coming". */
  .pill.add:hover :global(svg) {
    transform: rotate(90deg);
  }
  /* A wrapper so the unread badge can sit at the bubble's corner WITHOUT being
     clipped by a pill's overflow:hidden (which was cutting the number off). */
  .bubble-wrap {
    position: relative;
    display: flex;
    flex-shrink: 0;
    /* Cascade entrance: each bubble drops in a beat after the previous one.
       Keyed each-blocks mean this replays only for genuinely new bubbles. */
    animation: rail-in 0.3s cubic-bezier(0.2, 0.9, 0.3, 1) both;
    animation-delay: calc(40ms + var(--i, 0) * 25ms);
  }
  .pill.home,
  .pill.add {
    animation: rail-in 0.3s cubic-bezier(0.2, 0.9, 0.3, 1) both;
  }
  .pill.add {
    animation-delay: 0.18s;
  }
  @keyframes rail-in {
    from {
      opacity: 0;
      transform: translateX(-14px) scale(0.8);
    }
  }
  /* Discord-style edge indicator on the rail's left: a small nub on hover
     that grows into a tall pill for the active guild. Sized off the 11px gap
     between the 42px bubble and the 64px rail edge (8px on touch, below). */
  .bubble-wrap::before {
    content: "";
    position: absolute;
    left: -11px;
    top: 50%;
    translate: 0 -50%;
    width: 4px;
    height: 0;
    border-radius: 0 4px 4px 0;
    background: var(--accent);
    opacity: 0;
    pointer-events: none;
    transition:
      height 0.22s cubic-bezier(0.34, 1.56, 0.64, 1),
      opacity 0.18s ease;
  }
  .bubble-wrap:hover::before {
    height: 16px;
    opacity: 0.7;
  }
  .bubble-wrap:has(.pill.active)::before {
    height: 30px;
    opacity: 1;
  }
  /* Older WebKit (WebKitGTK < 2.46) has no :has() — skip the animated edge
     pill and mark the active guild with a static accent ring instead. */
  @supports not selector(:has(*)) {
    .pill.active {
      box-shadow: 0 0 0 2px var(--accent);
    }
  }
  .badge {
    position: absolute;
    top: -3px;
    right: -3px;
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border-radius: 9px;
    background: var(--danger); /* unread = a clean red corner bubble */
    color: #fff;
    font-size: 11px;
    font-weight: 700;
    line-height: 1;
    display: grid;
    place-items: center;
    border: 2px solid var(--bg-0);
    pointer-events: none;
    animation: badge-pop 0.25s cubic-bezier(0.34, 1.56, 0.64, 1) both;
  }
  @keyframes badge-pop {
    from {
      transform: scale(0.4);
      opacity: 0;
    }
  }
  /* Mentions demand attention: after the pop, a slow heartbeat. */
  .badge.mention {
    animation:
      badge-pop 0.25s cubic-bezier(0.34, 1.56, 0.64, 1) both,
      badge-beat 2.6s ease-in-out 0.4s infinite;
  }
  @keyframes badge-beat {
    0%,
    72%,
    100% {
      transform: scale(1);
    }
    80% {
      transform: scale(1.18);
    }
    88% {
      transform: scale(1);
    }
    94% {
      transform: scale(1.12);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .badge.mention {
      animation: badge-pop 0.25s cubic-bezier(0.34, 1.56, 0.64, 1) both;
    }
    .bubble-wrap,
    .pill.home,
    .pill.add,
    .jet.roll {
      animation: none;
    }
    .pill:hover {
      transform: none;
    }
    .pill.add:hover :global(svg),
    .pill.home:hover :global(svg) {
      transform: none;
    }
  }
  /* Touch: bubbles grow to a comfortable 48px tap target. */
  @media (pointer: coarse), (max-width: 700px) {
    .rail {
      gap: 10px;
      padding: 12px 0;
    }
    .pill {
      width: 48px;
      height: 48px;
    }
    /* 48px bubbles in the 64px rail → the edge gap shrinks to 8px. */
    .bubble-wrap::before {
      left: -8px;
    }
  }
  .face {
    width: 100%;
    height: 100%;
    display: grid;
    place-items: center;
    border-radius: inherit;
    font-weight: 700;
  }
</style>
