<script>
  // MobileShell is the phone/touch layout: one pane at a time instead of the
  // desktop 4-column grid. Discord-style gesture navigation — swipe right
  // anywhere in the chat to pull in the left drawer (guild rail + channel
  // list), swipe left for the member drawer; both track the finger and snap
  // open/closed on release by position + fling velocity. The top-bar buttons
  // and the scrim drive the same state. Every child component (GuildRail,
  // ChannelList, MessageList, Composer, MemberPanel, VoicePanel) is the same
  // one the desktop shell uses; only arrangement and navigation differ.
  import {
    S,
    activeGuild,
    nudge,
    openContextMenu,
    refreshGuilds,
    selectGuild,
    flash,
    clearFeed,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { runSearch, closeSearch } from "./lib/search.js";
  import { PERM, has } from "./lib/perms.js";
  import GuildRail from "./GuildRail.svelte";
  import ChannelList from "./ChannelList.svelte";
  import MessageList from "./MessageList.svelte";
  import ForumView from "./ForumView.svelte";
  import SearchPanel from "./SearchPanel.svelte";
  import Composer from "./Composer.svelte";
  import MemberPanel from "./MemberPanel.svelte";
  import VoicePanel from "./VoicePanel.svelte";
  import Welcome from "./Welcome.svelte";
  import Icon from "./Icon.svelte";

  let {
    composer = $bindable(null),
    onJoinVoice,
    onLeaveVoice,
    onToggleMute,
    onToggleDeafen,
    onToggleShare,
    onToggleCamera,
  } = $props();

  const isDM = $derived(activeGuild()?.kind === "dm");
  const hasChannel = $derived(!!S.activeChannelId && !!activeGuild());
  // Forum channels swap the chat feed for the post board.
  const activeChannelObj = $derived(
    activeGuild()?.channels.find((c) => c.id === S.activeChannelId) || null,
  );
  const callHere = $derived(S.voice && S.voice.channelId === S.activeChannelId);
  const canRight = $derived(hasChannel && !isDM);

  // Title bar: the open channel's name (or guild name on the welcome screen).
  const title = $derived.by(() => {
    const g = activeGuild();
    if (!g) return "Concord";
    if (g.kind === "dm") return g.name;
    const ch = g.channels?.find((c) => c.id === S.activeChannelId);
    return ch ? `#${ch.name}` : g.name;
  });

  // ---- gesture-driven drawers ----
  // Each drawer's position is a fraction: 0 = offscreen, 1 = fully open. The
  // fractions are the single source of truth for rendering; S.drawerOpen /
  // S.membersOpen mirror the settled state so the rest of the app (Android
  // back handling, channel-switch auto-close) keeps working unchanged.
  let leftFrac = $state(S.drawerOpen ? 1 : 0);
  let rightFrac = $state(0);
  let dragging = $state(false); // a claimed horizontal drag is in progress
  let vw = $state(window.innerWidth);

  const leftW = $derived(Math.min(vw * 0.86, 340));
  const rightW = $derived(Math.min(vw * 0.82, 300));
  const scrimO = $derived(Math.max(leftFrac, rightFrac));

  // External opens/closes (hamburger, back button, channel selected) sync the
  // fractions — but never mid-drag, where the finger owns them.
  $effect(() => {
    const open = S.drawerOpen;
    if (!dragging) leftFrac = open ? 1 : 0;
  });
  $effect(() => {
    const open = S.membersOpen;
    if (!dragging) rightFrac = open ? 1 : 0;
  });

  // Selecting a channel from the drawer should reveal the chat — close the
  // drawer whenever the active channel changes.
  let lastChannel = S.activeChannelId;
  $effect(() => {
    if (S.activeChannelId !== lastChannel) {
      lastChannel = S.activeChannelId;
      S.drawerOpen = false;
    }
  });

  // The members drawer unmounts in DMs — also clear its state, or a stale
  // membersOpen=true pops the drawer unbidden on the next guild channel.
  $effect(() => {
    if (!canRight) S.membersOpen = false;
  });

  function closeDrawers() {
    S.drawerOpen = false;
    S.membersOpen = false;
  }

  // ---- "⋯" top-bar menu ----
  // Everything ChatHeader offers on desktop (search, pins, call, invite, guild
  // management) is reachable here — ChatHeader itself never renders on phones.
  // openContextMenu presents these as a bottom action sheet on mobile.
  let searchOpen = $state(false);

  function confirmLeave() {
    const g = activeGuild();
    if (!g) return;
    // A 1:1 DM is only CLOSED (hidden; messaging again reopens it, history
    // intact) — the backend keeps the conversation alive underneath.
    const closeDM = g.kind === "dm" && (g.dmMembers ?? 2) <= 2;
    const verb = closeDM ? "Close" : g.isOwner ? "Delete" : "Leave";
    S.modal = {
      kind: "confirm",
      title: `${verb} "${g.name}"?`,
      body: closeDM
        ? "The conversation is hidden from your list — messaging each other brings it back."
        : "Its messages will be removed from this device.",
      confirmLabel: verb,
      onConfirm: async () => {
        S.modal = null;
        await api.leaveGuild(g.id);
        S.activeGuildId = "";
        S.activeChannelId = "";
        clearFeed();
        await refreshGuilds();
        if (S.guilds.length) selectGuild(S.guilds[0].id);
        flash(closeDM ? "Conversation closed" : g.isOwner ? "Guild deleted" : "Left guild");
      },
    };
  }

  function moreMenu(e) {
    const g = activeGuild();
    if (!g) return;
    const dm = g.kind === "dm";
    const inCall = S.voice && S.voice.channelId === S.activeChannelId;
    openContextMenu(
      e,
      [
        { label: "Search", icon: "search", onClick: () => (searchOpen = true) },
        { label: "Pinned messages", icon: "pin", onClick: () => (S.showPins = !S.showPins) },
        { label: "Disappearing messages", icon: "clock", onClick: () => (S.modal = { kind: "disappear", channelId: S.activeChannelId }) },
        !g.dmNotes &&
          (inCall
            ? { label: dm ? "End call" : "Leave voice", icon: "door", onClick: () => onLeaveVoice() }
            : { label: dm ? "Start call" : "Join voice", icon: "speaker", onClick: () => onJoinVoice() }),
        !dm && g.canManage && {
          label: "Invite people",
          icon: "members",
          onClick: async () => (S.modal = { kind: "invite", code: await api.inviteCode(S.activeGuildId) }),
        },
        !dm && { sep: true },
        !dm && { label: "Guild settings", icon: "gear", onClick: () => (S.modal = { kind: "guildSettings" }) },
        !dm && { label: "Guild emoji", icon: "smile", onClick: () => (S.modal = { kind: "emoji" }) },
        !dm && (has(g.myPerms, PERM.MANAGE_ROLES) || g.isOwner) && {
          label: "Roles",
          icon: "spark",
          onClick: () => (S.modal = { kind: "roles" }),
        },
        !dm && g.canManage && {
          label: "Banned members",
          icon: "door",
          onClick: () => (S.modal = { kind: "bans" }),
        },
        !g.dmNotes && { sep: true },
        !g.dmNotes && {
          label: dm
            ? (g.dmMembers ?? 2) > 2
              ? "Leave group"
              : "Close conversation"
            : g.isOwner
              ? "Delete guild"
              : "Leave guild",
          icon: g.isOwner ? "trash" : "door",
          danger: true,
          onClick: confirmLeave,
        },
      ],
      { title },
    );
  }

  // One drag at a time: claimed once the movement is clearly horizontal, then
  // the touched drawer (or the one a fresh swipe implies) follows the finger.
  let drag = null; // {startX, startY, claimed, target, startFrac, prevX, prevT, vel}

  function onTouchStart(e) {
    if (e.touches.length !== 1) return;
    // Don't hijack swipes that start in text inputs or overlays (sheets,
    // profile card, emoji picker) — those own their gestures.
    if (e.target.closest("textarea, input, .bs-sheet, .pop, .picker")) return;
    const t = e.touches[0];
    drag = {
      startX: t.clientX,
      startY: t.clientY,
      claimed: false,
      target: null,
      startFrac: 0,
      prevX: t.clientX,
      prevT: performance.now(),
      vel: 0,
    };
  }

  function onTouchMove(e) {
    if (!drag) return;
    const t = e.touches[0];
    if (!t) return;
    const dx = t.clientX - drag.startX;
    const dy = t.clientY - drag.startY;
    if (!drag.claimed) {
      // Mostly-vertical movement is a scroll — let it go and stand down.
      if (Math.abs(dy) > 14 && Math.abs(dy) > Math.abs(dx)) {
        drag = null;
        return;
      }
      if (Math.abs(dx) < 12 || Math.abs(dx) < Math.abs(dy) * 1.4) return;
      // Claim: an open drawer always owns the gesture; otherwise the swipe
      // direction picks which drawer is being pulled in.
      drag.target =
        leftFrac > 0 ? "left"
        : rightFrac > 0 ? "right"
        : dx > 0 ? "left"
        : canRight ? "right"
        : null;
      if (!drag.target) {
        drag = null;
        return;
      }
      drag.claimed = true;
      drag.startFrac = drag.target === "left" ? leftFrac : rightFrac;
      dragging = true;
    }
    const now = performance.now();
    if (now > drag.prevT) {
      // Exponentially-smoothed velocity so the release fling test isn't at the
      // mercy of one noisy final sample.
      const inst = (t.clientX - drag.prevX) / (now - drag.prevT);
      drag.vel = drag.vel * 0.7 + inst * 0.3;
    }
    drag.prevX = t.clientX;
    drag.prevT = now;
    const clamp = (v) => Math.max(0, Math.min(1, v));
    if (drag.target === "left") leftFrac = clamp(drag.startFrac + dx / leftW);
    else rightFrac = clamp(drag.startFrac - dx / rightW);
  }

  // After a claimed drag, browsers can still synthesize a click at the touch
  // point — which would "tap" whatever drawer row ended up under the finger.
  // Belt (preventDefault on touchend) and suspenders (a brief capture-phase
  // click eater) kill it.
  let ghostGuardUntil = 0;
  function onClickCapture(e) {
    if (performance.now() < ghostGuardUntil) {
      e.stopPropagation();
      e.preventDefault();
    }
  }

  function onTouchEnd(e) {
    if (!drag) return;
    if (drag.claimed) {
      if (e.cancelable) e.preventDefault();
      ghostGuardUntil = performance.now() + 400;
      const target = drag.target;
      const frac = target === "left" ? leftFrac : rightFrac;
      // Opening-direction velocity for this drawer (left opens rightward,
      // right opens leftward).
      const vel = target === "left" ? drag.vel : -drag.vel;
      const open = Math.abs(vel) > 0.35 ? vel > 0 : frac > 0.5;
      dragging = false;
      if (target === "left") {
        leftFrac = open ? 1 : 0;
        S.drawerOpen = open;
      } else {
        rightFrac = open ? 1 : 0;
        S.membersOpen = open;
      }
    }
    drag = null;
  }

  // Connection pill: a compact status line under the top bar. Hidden once
  // we're solidly online (peers connected, nothing healing) to stay quiet.
  const conn = $derived.by(() => {
    const n = S.netStatus;
    if (!n) return { show: true, cls: "connecting", text: "Connecting…" };
    if (n.outOfSyncGuilds > 0)
      return { show: true, cls: "syncing", text: "Catching up…" };
    if (n.peers > 0)
      return {
        show: false,
        cls: "online",
        text: `Online · ${n.peers} peer${n.peers === 1 ? "" : "s"}`,
      };
    // No peers: offline, unless we have no rendezvous configured at all (LAN-only).
    return {
      show: true,
      cls: "offline",
      text: n.hasBootstrap ? "Offline — reconnecting…" : "No peers yet",
    };
  });
</script>

<svelte:window onresize={() => (vw = window.innerWidth)} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="mshell"
  ontouchstart={onTouchStart}
  ontouchmove={onTouchMove}
  ontouchend={onTouchEnd}
  ontouchcancel={onTouchEnd}
  onclickcapture={onClickCapture}
>
  <header class="mtopbar">
    <button class="icon-btn" aria-label="Menu" onclick={() => (S.drawerOpen = true)}>
      <Icon name="menu" />
    </button>
    <!-- The title is tappable (Discord/Telegram muscle memory): same sheet as ⋯. -->
    <button class="mtitle" onclick={hasChannel ? moreMenu : undefined} disabled={!hasChannel}>
      {title}
    </button>
    {#if canRight}
      <button class="icon-btn" aria-label="Members" onclick={() => (S.membersOpen = true)}>
        <Icon name="members" />
      </button>
    {/if}
    {#if hasChannel}
      <button class="icon-btn" aria-label="More options" onclick={moreMenu}>
        <Icon name="dots" size={18} />
      </button>
    {:else}
      <span class="icon-btn-spacer"></span>
    {/if}
  </header>

  {#if searchOpen}
    <form
      class="msearch"
      onsubmit={(e) => runSearch(e)}
    >
      <Icon name="search" size={14} />
      <!-- svelte-ignore a11y_autofocus -->
      <input
        placeholder="Search all conversations"
        aria-label="Search messages"
        bind:value={S.searchQuery}
        autofocus
      />
      <button
        type="button"
        class="icon-btn"
        aria-label="Close search"
        onclick={() => {
          closeSearch();
          searchOpen = false;
        }}
      >
        <Icon name="close" size={15} />
      </button>
    </form>
  {/if}

  {#if conn.show}
    <button class="conn {conn.cls}" onclick={nudge} aria-label="Reconnect">
      <span class="conn-dot"></span>
      {conn.text}
    </button>
  {/if}

  <main class="mchat">
    {#if hasChannel}
      {#if callHere}
        <VoicePanel
          {onLeaveVoice}
          {onToggleMute}
          {onToggleDeafen}
          {onToggleShare}
          {onToggleCamera}
        />
      {/if}
      <!-- Mounted per-pane, not inside MessageList: a forum channel renders
           ForumView instead, and search results had nowhere to land there. -->
      <SearchPanel />
      {#if activeChannelObj?.type === "forum"}
        <ForumView forum={activeChannelObj} />
      {:else}
        <MessageList onDropFiles={(files) => files.forEach((f) => composer?.attachFile(f))} />
        <Composer bind:this={composer} />
      {/if}
    {:else}
      <Welcome />
    {/if}
  </main>

  <!-- Scrim: opacity tracks how far a drawer is pulled in. -->
  <button
    class="scrim"
    class:drag={dragging}
    style="opacity:{scrimO}; pointer-events:{scrimO > 0 ? 'auto' : 'none'}"
    aria-label="Close menu"
    tabindex={scrimO > 0 ? 0 : -1}
    onclick={closeDrawers}
  ></button>

  <!-- Left drawer: guild rail + channel list. Always mounted; position is
       transform-driven so it can track the finger. -->
  <!-- At rest fully open the transform is dropped entirely: a transformed
       ancestor becomes the containing block for position:fixed descendants,
       which would cage any bottom sheet opened from inside the drawer (e.g.
       the status picker) into the drawer's box instead of the viewport. -->
  <aside
    class="drawer left"
    class:drag={dragging}
    class:hidden={leftFrac === 0 && !dragging}
    style="width:{leftW}px; transform:{leftFrac === 1 && !dragging
      ? 'none'
      : `translateX(${(leftFrac - 1) * leftW}px)`}"
    role="dialog"
    aria-label="Navigation"
    aria-hidden={leftFrac === 0}
  >
    <div class="drawer-rail"><GuildRail /></div>
    <div class="drawer-channels">
      <ChannelList
        {onJoinVoice}
        {onLeaveVoice}
        {onToggleMute}
        {onToggleShare}
        {onToggleCamera}
      />
    </div>
  </aside>

  <!-- Right drawer: member list. -->
  {#if canRight}
    <aside
      class="drawer right"
      class:drag={dragging}
      class:hidden={rightFrac === 0 && !dragging}
      style="width:{rightW}px; transform:{rightFrac === 1 && !dragging
        ? 'none'
        : `translateX(${(1 - rightFrac) * rightW}px)`}"
      role="dialog"
      aria-label="Members"
      aria-hidden={rightFrac === 0}
    >
      <MemberPanel />
    </aside>
  {/if}
</div>

<style>
  .mshell {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
    background: var(--bg-2);
    /* Sit above the animated theme backdrop (App.svelte .theme-bg). */
    position: relative;
    z-index: 1;
  }
  /* Under an animated pack, let the backdrop show through: the outer shell goes
     transparent so only the inner panels (translucent bg-*) tint it — no
     double-dark stacking. */
  :global(:root[data-anim-bg]) .mshell,
  :global(:root[data-textured]) .mshell {
    background: transparent;
  }
  .mtopbar {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 52px;
    flex-shrink: 0;
    padding: 0 6px;
    padding-top: env(safe-area-inset-top);
    box-sizing: content-box;
    background: var(--bg-1);
    border-bottom: 1px solid var(--border);
    /* Faint drop under the bar: reads as elevation over the feed. */
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.14);
    position: relative;
    z-index: 5;
  }
  .mtitle {
    flex: 1;
    min-width: 0;
    font-weight: 600;
    font-size: 16px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    /* It's a button now (opens the channel sheet) — keep the plain-text look. */
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 10px 4px;
    min-height: 44px;
    border-radius: var(--radius-sm);
  }
  .mtitle:active:not(:disabled) {
    background: var(--bg-3);
  }
  .mtitle:disabled {
    color: var(--text);
    opacity: 1;
  }
  .icon-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 44px;
    height: 44px;
    border-radius: var(--radius-md);
    background: transparent;
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .icon-btn:active {
    background: var(--bg-3);
  }
  .icon-btn-spacer {
    width: 44px;
    flex-shrink: 0;
  }
  /* Mobile search row (under the top bar): feeds the shared search pipeline;
     results render in the SearchPanel inside the feed. */
  .msearch {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
    padding: 6px 10px 6px 14px;
    background: var(--bg-1);
    border-bottom: 1px solid var(--border);
    color: var(--text-muted);
  }
  .msearch input {
    flex: 1;
    min-width: 0;
    background: transparent;
    border: none;
    font-size: 16px; /* stops iOS focus zoom */
    padding: 8px 0;
    color: var(--text);
  }
  .msearch input:focus {
    outline: none;
    box-shadow: none;
  }
  .mchat {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
    /* Vertical panning stays native (message scroll); horizontal movement is
       delivered to the drawer gesture handlers instead of being eaten by the
       browser. */
    touch-action: pan-y;
  }
  /* Connection pill: a slim status strip under the top bar; tap to reconnect. */
  .conn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    width: 100%;
    flex-shrink: 0;
    padding: 5px 12px;
    font-size: 12px;
    font-weight: 600;
    color: var(--text);
    border: none;
    border-bottom: 1px solid var(--border);
  }
  .conn-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .conn.connecting,
  .conn.syncing {
    background: color-mix(in srgb, var(--accent) 18%, var(--bg-1));
  }
  .conn.connecting .conn-dot,
  .conn.syncing .conn-dot {
    background: var(--accent);
    animation: conn-pulse 1.1s ease-in-out infinite;
  }
  .conn.offline {
    background: color-mix(in srgb, var(--danger, #d9534f) 20%, var(--bg-1));
  }
  .conn.offline .conn-dot {
    background: var(--danger, #d9534f);
  }
  @keyframes conn-pulse {
    50% {
      opacity: 0.35;
    }
  }
  /* Drawers over a scrim whose opacity tracks the drag. Deliberately a plain
     dim, NOT a full-screen backdrop-filter blur: this element is live while a
     finger drags at 60fps, and blurring the whole viewport every frame is what
     melted the GPU last time. A solid scrim composites for free. */
  .scrim {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.58);
    z-index: 60;
    border: none;
    transition: opacity 0.22s ease;
  }
  .drawer {
    position: fixed;
    top: 0;
    bottom: 0;
    z-index: 61;
    display: flex;
    background: var(--bg-1);
    /* Contact shadow + wide ambient falloff: the drawer floats OVER the chat
       rather than abutting it. */
    box-shadow:
      0 0 4px rgba(0, 0, 0, 0.3),
      0 0 40px rgba(0, 0, 0, 0.55);
    /* Slide via inline transform; hide fully-closed drawers only after the
       slide-out finishes (the delayed-visibility trick), never on the way in. */
    transition:
      transform 0.22s cubic-bezier(0.2, 0.9, 0.3, 1),
      visibility 0s linear 0.25s;
  }
  .drawer:not(.hidden) {
    transition: transform 0.22s cubic-bezier(0.2, 0.9, 0.3, 1);
    visibility: visible;
  }
  .drawer.hidden {
    visibility: hidden;
  }
  .drawer.drag,
  .scrim.drag {
    transition: none;
  }
  /* A hairline edge highlight so the drawer's rim catches the light. */
  .drawer.left {
    left: 0;
    padding-top: env(safe-area-inset-top);
    border-right: 1px solid color-mix(in srgb, var(--text) 8%, transparent);
  }
  .drawer.right {
    right: 0;
    padding-top: env(safe-area-inset-top);
    border-left: 1px solid color-mix(in srgb, var(--text) 8%, transparent);
  }
  .drawer-rail {
    flex-shrink: 0;
    width: 64px;
    border-right: 1px solid var(--border);
    /* GuildRail is only as tall as its guild buttons, so below them the
       drawer's --bg-1 showed through where --bg-0 should be — a 582px seam
       down the strip at 844px tall. Make the rail fill its column. */
    display: flex;
  }
  .drawer-rail > :global(.rail) {
    flex: 1;
  }
  .drawer-channels {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  /* The desktop grid gives these panels their size; inside the drawers they
     must be told to fill, or the channel column collapses to its content
     height and the member panel shrinks to content width (leaving a dead,
     unclickable strip). */
  .drawer-channels > :global(.cols) {
    flex: 1;
    min-height: 0;
  }
  .drawer.right > :global(.panel) {
    flex: 1;
    min-width: 0;
    border-left: none;
  }
</style>
