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
    selectChannel,
    registerOverlay,
    flash,
    clearFeed,
    accentForeground,
  } from "./lib/state.svelte.js";
  import { untrack } from "svelte";
  import { guildAccent } from "./lib/guildaccent.js";
  import { api } from "./lib/api.js";
  import { haptic } from "./lib/touch.js";
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

  // Per-guild accent, same stamp and precedence as the desktop grid in
  // App.svelte: explicit user preset > guild banner hue > pack/profile.
  const guildAccentVars = $derived.by(() => {
    if (S.prefs.accent || S.prefs.guildAccents === false) return "";
    const g = activeGuild();
    if (!g || g.kind === "dm") return "";
    const c = guildAccent(g.banner);
    return c ? `--accent:${c};--accent-fg:${accentForeground(c)};` : "";
  });

  const isDM = $derived(activeGuild()?.kind === "dm");
  const hasChannel = $derived(!!S.activeChannelId && !!activeGuild());
  // Forum channels swap the chat feed for the post board.
  const activeChannelObj = $derived(
    activeGuild()?.channels.find((c) => c.id === S.activeChannelId) || null,
  );
  const callHere = $derived(S.voice && S.voice.channelId === S.activeChannelId);
  const canRight = $derived(hasChannel && !isDM);
  // A forum POST is a channel nested under its board. ChatHeader's breadcrumb
  // says so on desktop, but ChatHeader never renders here — and ChannelList
  // filters posts out of the drawer — so without this the top-left button is
  // the only exit and it opens a list the post isn't in.
  const parentChannel = $derived(
    activeChannelObj?.parent
      ? activeGuild()?.channels.find((c) => c.id === activeChannelObj.parent) || null
      : null,
  );

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

  // moving stays true for the whole of a drawer's travel — the tracked drag AND
  // the 0.22s settle that follows it, AND a plain tap-to-open with no drag at
  // all. It is what puts `will-change` on the two panels: the layer has to exist
  // before the movement starts and has to survive the release, or the settle
  // repaints every frame with nothing left to say so.
  let moving = $state(false);
  let moveTimer;
  function holdLayer(ms = 320) {
    moving = true;
    clearTimeout(moveTimer);
    moveTimer = setTimeout(() => (moving = false), ms);
  }

  const leftW = $derived(Math.min(vw * 0.86, 340));
  const rightW = $derived(Math.min(vw * 0.82, 300));
  const scrimO = $derived(Math.max(leftFrac, rightFrac));

  // External opens/closes (hamburger, back button, channel selected) sync the
  // fractions — but never mid-drag, where the finger owns them.
  $effect(() => {
    const open = S.drawerOpen;
    if (!dragging && leftFrac !== (open ? 1 : 0)) {
      leftFrac = open ? 1 : 0;
      untrack(holdLayer);
    }
  });
  $effect(() => {
    const open = S.membersOpen;
    if (!dragging && rightFrac !== (open ? 1 : 0)) {
      rightFrac = open ? 1 : 0;
      untrack(holdLayer);
    }
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
  // The search row is component-local, so the hardware-back ladder in App.svelte
  // cannot see it. Registering it as an overlay is what makes back close the row
  // instead of walking past it and exiting the app.
  $effect(() => {
    if (!searchOpen) return;
    return registerOverlay(() => {
      closeSearch();
      searchOpen = false;
    });
  });

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
    haptic("light"); // the sheet slides up from the far end of the screen; say it opened
    openContextMenu(
      e,
      [
        // The fuzzy palette (every channel, DM and global action) was reachable
        // only by Ctrl+K, i.e. never on a phone. It is the fastest way to move
        // around once there are more than a handful of conversations, so it goes
        // first — and this sheet opens at the bottom, under the thumb.
        { label: "Jump to…", icon: "search", onClick: () => (S.quickSwitcher = true) },
        { label: "Search messages", icon: "search", onClick: () => (searchOpen = true) },
        { label: "Pinned messages", icon: "pin", onClick: () => (S.showPins = !S.showPins) },
        // Every room has a calendar now: guilds share theirs, a DM's belongs
        // to its people, Notes' is private (a group of one).
        { label: g.dmNotes ? "Private events" : "Events", icon: "calendar", onClick: () => (S.modal = { kind: "events" }) },
        // The blended calendar lives on the rail too, but mid-chat the rail is
        // a drawer-swipe away — this sheet is already under the thumb.
        { label: "Your calendar", icon: "calendar", onClick: () => (S.modal = { kind: "myCalendar" }) },
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
        !dm && { label: "Guild settings", icon: "gear", onClick: () => (S.modal = { kind: "guildHub" }) },
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

  // Walk up from the touch target looking for something that scrolls sideways
  // under its own steam — a wide code block, a table. touch-action can't express
  // this (an ancestor's pan-y is intersected with every descendant), and the
  // drawer gesture would otherwise eat the only way to read the rest of the line.
  function inHScroller(el) {
    for (let n = el; n && n !== document.body; n = n.parentElement) {
      if (n.scrollWidth > n.clientWidth + 1) {
        const ov = getComputedStyle(n).overflowX;
        if (ov === "auto" || ov === "scroll") return true;
      }
    }
    return false;
  }

  function onTouchStart(e) {
    if (e.touches.length !== 1) return;
    // Don't hijack swipes that start in text inputs or overlays (sheets,
    // profile card, emoji picker) or on the floating call window, which runs its
    // own pointer drag — touch-action:none stops the BROWSER panning, not an
    // ancestor's touchmove listener, so without .dock here moving the call
    // window sideways dragged the drawer in behind it.
    if (e.target.closest("textarea, input, .bs-sheet, .pop, .picker, .dock")) return;
    if (inHScroller(e.target)) return;
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
      // Message rows own leftward drags (swipe-to-reply); direction isn't
      // known yet, so just remember where the touch landed and decide at
      // claim time. The attribute is set by Message.svelte's action, touch
      // devices only.
      fromMsg: !!e.target.closest?.("[data-swipe-reply]"),
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
      // Swipe-to-reply (Message.svelte) owns leftward drags that start on a
      // message row — same shape as the .dock / inHScroller stand-downs, just
      // decided here because it needs the direction. Only while both drawers
      // are shut: a leftward drag with a drawer open is how it closes, from
      // anywhere. If the row's own threshold never fires it just snaps back;
      // the drag is not handed back to us and that's fine.
      if (drag.fromMsg && dx < 0 && leftFrac === 0 && rightFrac === 0) {
        drag = null;
        return;
      }
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
      moving = true;
      clearTimeout(moveTimer);
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
      holdLayer();
      // Deliberately NO haptic on the drawer snap. Opening and closing the
      // drawer is the single most frequent gesture in the app and it is already
      // fully visible — the drawer is tracking your finger. A buzz on something
      // you do dozens of times an hour reads as a twitchy phone, not as
      // feedback. Haptics are kept for things you cannot see happen or cannot
      // undo: a long-press registering, a destructive confirm, a call ending.
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
      // Only when the feed is NOT already saying it. MessageList shows a full
      // out-of-sync banner for the open guild, and the pill floats over the top
      // of the feed — so both appeared at once, saying the same two words, with
      // the pill landing squarely on the banner's own sentence. The banner is
      // the more useful of the two (it explains what to do), so it wins; the
      // pill still covers the case where the guild you are LOOKING at is fine
      // and some other one is catching up.
      return {
        show: !activeGuild()?.outOfSync,
        cls: "syncing",
        text: "Catching up…",
      };
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

<!-- Toasts pin themselves to --mchrome, so it has to describe the chrome that is
     ACTUALLY on screen: the top bar, plus the search row while it is open, plus
     the floating connection pill. Without the last two, a toast landed on top of
     "Catching up…" and over the search field being typed into — the two states a
     phone user sees most. -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="mshell"
  style="{guildAccentVars}--mchrome: calc(52px + var(--safe-top) + {searchOpen ? 46 : 0}px + {conn.show
    ? 38
    : 0}px)"
  ontouchstart={onTouchStart}
  ontouchmove={onTouchMove}
  ontouchend={onTouchEnd}
  ontouchcancel={onTouchEnd}
  onclickcapture={onClickCapture}
>
  <header class="mtopbar">
    {#if parentChannel}
      <!-- Inside a forum post the left slot is a way OUT of it, not into the
           drawer: the post isn't in the channel list, so the hamburger was a
           dead end. The drawer is still one edge-swipe away. -->
      <button
        class="icon-btn"
        aria-label="Back to {parentChannel.name}"
        onclick={() => selectChannel(parentChannel.id)}
      >
        <span class="chev-back"><Icon name="chevron" size={18} /></span>
      </button>
    {:else}
      <button class="icon-btn" aria-label="Menu" onclick={() => (S.drawerOpen = true)}>
        <Icon name="menu" />
      </button>
    {/if}
    <!-- The title is tappable (Discord/Telegram muscle memory): same sheet as ⋯.
         The chevron is the only thing that says so before you tap it. -->
    <button class="mtitle" onclick={hasChannel ? moreMenu : undefined} disabled={!hasChannel}>
      <span class="mtitle-text">{title}</span>
      {#if hasChannel}<span class="chev-down"><Icon name="chevron" size={12} /></span>{/if}
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
      <!-- Welcome screen: this corner used to be an empty 44px spacer. Nothing
           is open, so navigating is the only thing the user can want. -->
      <button
        class="icon-btn"
        aria-label="Jump to a conversation"
        onclick={() => (S.quickSwitcher = true)}
      >
        <Icon name="search" />
      </button>
    {/if}
  </header>

  {#if searchOpen}
    <form
      class="msearch"
      onsubmit={(e) => runSearch(e)}
    >
      <Icon name="search" size={14} />
      <!-- svelte-ignore a11y_autofocus -->
      <!-- type/enterkeyhint/autocapitalize: without them Android opens a
           capitalised keyboard with predictive text and an unlabelled return
           key, so searching for a handle or a code fragment autocorrects into
           something else and there is no visible way to run it. -->
      <input
        type="search"
        enterkeyhint="search"
        autocapitalize="none"
        autocorrect="off"
        spellcheck="false"
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

  <main class="mchat">
    <!-- Floated over the feed rather than stacked above it: this pill appears
         and disappears on its own (offline → online, "Catching up…" → done), and
         in normal flow every one of those flips shifted the whole conversation
         ~27px under the reader's eyes. -->
    {#if conn.show}
      <button class="conn {conn.cls}" onclick={nudge} aria-label="Reconnect">
        <span class="conn-dot"></span>
        {conn.text}
      </button>
    {/if}
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

  <!-- Scrim: opacity tracks how far a drawer is pulled in.
       `style:` rather than a style="" template, and pointer-events moved to a
       class: a template style attribute is written back WHOLE on every change,
       and replacing an element's entire inline declaration invalidates it and
       everything under it — sixty times a second, for the two biggest subtrees
       on the screen. A style: directive writes the one property through the
       CSSOM instead, and opacity is not inherited, so nothing below it is
       touched. -->
  <button
    class="scrim"
    class:drag={dragging}
    class:moving
    class:lit={scrimO > 0}
    style:opacity={scrimO}
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
    class:moving
    class:hidden={leftFrac === 0 && !dragging}
    style:width="{leftW}px"
    style:transform={leftFrac === 1 && !dragging
      ? "none"
      : `translateX(${(leftFrac - 1) * leftW}px)`}
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
      class:moving
      class:hidden={rightFrac === 0 && !dragging}
      style:width="{rightW}px"
      style:transform={rightFrac === 1 && !dragging
        ? "none"
        : `translateX(${(1 - rightFrac) * rightW}px)`}
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
    /* --sa-* / --kb come from MainActivity's inset bridge; env() is the iOS
       side of the same values. The Android WebView resolves env() to 0 for
       system bars, so on the phone this app actually ships to, env() alone
       meant the composer sat under the gesture pill and the whole feed sat
       under the keyboard. max() takes whichever platform answered. */
    padding-bottom: calc(max(var(--safe-bottom), var(--sa-bottom, 0px)) + var(--kb, 0px));
    padding-left: max(var(--safe-left), var(--sa-left, 0px));
    padding-right: max(var(--safe-right), var(--sa-right, 0px));
  }
  /* Press feedback for lib/touch.js's longpress action: until the sheet opens
     ~400ms later there is otherwise no sign the press registered, and on
     messages/channels/members the long-press is the ONLY route to the menu.
     Global because the nodes wearing it live in other components' scopes;
     mounted only inside the phone shell, which is exactly its audience. */
  :global(.lp-press) {
    transition: transform 0.12s ease, filter 0.12s ease;
    transform: scale(0.985);
    filter: brightness(1.18);
  }
  @media (prefers-reduced-motion: reduce) {
    :global(.lp-press) {
      transform: none;
    }
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
    gap: var(--sp-2);
    height: 52px;
    flex-shrink: 0;
    padding: 0 6px;
    padding-top: max(var(--safe-top), var(--sa-top, 0px));
    box-sizing: content-box;
    background: var(--bg-1);
    border-bottom: 1px solid var(--border);
    /* Faint drop under the bar: reads as elevation over the feed. */
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.14);
    position: relative;
    z-index: 5;
  }
  .mtitle {
    display: flex;
    align-items: center;
    gap: 5px;
    flex: 1;
    min-width: 0;
    font-weight: 600;
    font-size: var(--fs-body);
    /* It's a button now (opens the channel sheet) — keep the plain-text look. */
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 10px 4px;
    min-height: var(--tap-min);
    border-radius: var(--radius-sm);
  }
  .mtitle-text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .chev-down {
    display: flex;
    flex-shrink: 0;
    color: var(--text-faint);
    transform: rotate(90deg);
  }
  .chev-back {
    display: flex;
    transform: rotate(180deg);
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
    width: max(44px, var(--tap-min));
    height: max(44px, var(--tap-min));
    border-radius: var(--radius-md);
    background: transparent;
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .icon-btn:active {
    background: var(--bg-3);
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
    /* The connection pill anchors to this box. */
    position: relative;
    /* Was pan-y. touch-action intersects down the ancestor chain, so pan-y here
       made every descendant unpannable sideways — including Message's
       `pre { overflow-x: auto }`, whose scrollbar was physically impossible to
       move, leaving the rest of every long code line unreadable. Both axes,
       still no pinch-zoom; the drawer gesture does its own axis discrimination
       in JS (onTouchMove) and now stands down inside a horizontal scroller. */
    touch-action: pan-x pan-y;
  }
  /* Connection pill: floats over the top of the feed, Telegram-style, and is
     tappable to force a reconnect. */
  .conn {
    position: absolute;
    top: 8px;
    left: 50%;
    transform: translateX(-50%);
    z-index: 4;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    max-width: calc(100% - 24px);
    min-height: var(--tap-min);
    padding: 0 16px;
    font-size: var(--fs-ui);
    font-weight: 600;
    color: var(--text);
    white-space: nowrap;
    border: 1px solid var(--border);
    border-radius: 999px;
    box-shadow: var(--shadow-pop, 0 4px 16px rgba(0, 0, 0, 0.35));
  }
  .conn-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    flex-shrink: 0;
  }
  .conn.connecting,
  .conn.syncing {
    background: color-mix(in srgb, var(--accent) 22%, var(--bg-1));
  }
  .conn.connecting .conn-dot,
  .conn.syncing .conn-dot {
    background: var(--accent);
    animation: conn-pulse 1.1s ease-in-out infinite;
  }
  .conn.offline {
    background: color-mix(in srgb, var(--danger, #d9534f) 24%, var(--bg-1));
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
    /* Fully transparent and out of the way unless a drawer is showing. This is
       a class rather than an inline pointer-events value because it changes
       exactly twice per gesture, while opacity changes every frame — keeping
       them apart is what lets the per-frame write be opacity alone. */
    pointer-events: none;
  }
  .scrim.lit {
    pointer-events: auto;
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
  /* Promote both to their own compositor layer for the duration of the drag —
     and only for that duration. An untransformed drawer moving under a finger
     is repainted at its new position on every frame (a hundred-odd paints per
     traversal, all of them of a full-height panel); on its own layer the same
     movement is the compositor re-placing a texture. will-change is left OFF at
     rest deliberately: a permanent layer costs its texture in video memory on
     every phone, awake or not, and there are two of these. */
  .drawer.moving {
    will-change: transform;
  }
  .scrim.moving {
    will-change: opacity;
  }
  /* The drawers are position:fixed, so the shell's own insets don't reach them:
     they pad themselves. Without the bottom one the settings gear and profile
     row at the foot of the channel list sit under the gesture nav bar. */
  .drawer {
    padding-top: max(var(--safe-top), var(--sa-top, 0px));
    padding-bottom: calc(max(var(--safe-bottom), var(--sa-bottom, 0px)) + var(--kb, 0px));
  }
  /* A hairline edge highlight so the drawer's rim catches the light. */
  .drawer.left {
    left: 0;
    padding-left: max(var(--safe-left), var(--sa-left, 0px));
    border-right: 1px solid color-mix(in srgb, var(--text) 8%, transparent);
  }
  .drawer.right {
    right: 0;
    padding-right: max(var(--safe-right), var(--sa-right, 0px));
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
  /* Landscape on a handset: ~393px of height, of which the top bar and the
     composer's two-row tray already claim half. Trim the chrome rather than
     leave two messages visible. (The app doesn't lock orientation — people
     rotate to read a wide code block or watch a shared screen.) */
  @media (pointer: coarse) and (max-height: 500px) {
    .mtopbar {
      height: 44px;
    }
    .conn {
      top: 4px;
      min-height: 32px; /* still a real target; the pill is not a primary action */
      font-size: var(--fs-compact);
    }
    .drawer-rail {
      width: 56px;
    }
  }
  /* The narrow floor: at 360px a guild channel shows back/title/members/⋯, and
     at an 8px gap the title ellipsises to about two words. */
  @media (max-width: 400px) {
    .mtopbar {
      gap: var(--sp-1);
      padding: 0 2px;
    }
  }
</style>
