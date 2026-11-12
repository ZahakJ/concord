<script>
  // Chat column header: channel name, search, pins, voice, guild actions.
  import Icon from "./Icon.svelte";
  import OwnerMark from "./OwnerMark.svelte";
  import { ownsPlace, ownerLabel } from "./lib/ownermark.js";
  import Menu from "./Menu.svelte";
  import {
    S,
    activeGuild,
    selectChannel,
    closePost,
    activeChannel,
    channelName,
    flash,
    voiceMembersFor,
    channelTypeIcon,
    toggleMemberPanel,
    openProfilePopover,
    confirmLeaveGuild,
    leaveGuildLabel,
    openGuildHub,
    openCallStage,
    channelShort,
    markRead,
    setPref,
    setChannelNotifs,
    guildNotifLevel,
    callHealth,
  } from "./lib/state.svelte.js";
  import { callClock } from "./lib/calltimer.svelte.js";
  import { LEVELS, levelLabel } from "./lib/notifs.js";
  import { PERM, has } from "./lib/perms.js";
  import { api } from "./lib/api.js";
  import { saveText } from "./lib/savefile.js";
  // Operator parsing (from:/in:/has:/before:/after:) + the backend call live
  // in lib/search.js, shared with the results panel's chip refinement.
  import { runSearch, closeSearch, queueSearch, registerSearchInput } from "./lib/search.js";
  import { channelTTL, ttlLabel } from "./lib/ephemeral.svelte.js";
  // Icon buttons carry use:tooltip (below-center, default delay) instead of
  // native title= — instant theme-matched labels; aria-label stays and is the
  // tip's text unless the tip needs richer wording than the label.
  import { tooltip } from "./lib/tooltip.js";
  import { fitText } from "./lib/fittext.js";
  import { insp } from "./lib/inspector.svelte.js";

  let { onJoinVoice, onLeaveVoice } = $props();

  const callState = $derived(callHealth());
  const clock = $derived(callClock());

  // Handed to lib/search.js so a filter chip clicked in the results panel can
  // type its prefix here and put the caret back after it.
  let searchEl = $state(null);
  $effect(() => registerSearchInput(searchEl));

  const g = $derived(activeGuild());
  const ch = $derived(activeChannel());

  // Slow mode, said out loud. It is a governed rule the room lives under and
  // it was legible only from inside the dialog that sets it.
  const slowSecs = $derived(Number(ch?.slowMode) || 0);
  const slowLabel = $derived(
    slowSecs >= 3600
      ? `${Math.round(slowSecs / 3600)}h`
      : slowSecs >= 60
        ? `${Math.round(slowSecs / 60)}m`
        : `${slowSecs}s`,
  );
  const ephTTL = $derived(ch ? channelTTL(S.activeChannelId) : 0);
  // In a DM (or meeting), is the other side already in the call while we're not?
  // Drives a "🔴 Live · Join" affordance so a call in progress is obvious.
  const peerInCall = $derived(
    !!ch &&
      (g?.kind === "dm" || g?.kind === "meeting") &&
      S.voice?.channelId !== ch.id &&
      (voiceMembersFor(ch.id) || []).some((m) => !m.self),
  );
  const peerSharing = $derived(peerInCall && (voiceMembersFor(ch.id) || []).some((m) => !m.self && m.sharing));
  // The inspector toggle wears the member count (tabular figures): the people
  // glyph alone said nothing about how many were in here.
  const memberCount = $derived(S.members.length);
  const canManageChannels = $derived(!!g && (g.isOwner || has(g.myPerms || 0, PERM.MANAGE_CHANNELS)));
  async function copyChannelLink() {
    try {
      await navigator.clipboard.writeText(`concord://channel?id=${encodeURIComponent(S.activeChannelId)}`);
      flash("Copied channel link", "success");
    } catch {
      flash("Couldn't copy the link");
    }
  }
  // ---- how much room this header actually has -----------------------------
  //
  // Every tier here used to be a viewport media query, and a viewport is the
  // wrong thing to measure twice over.
  //
  // The zoom. Ctrl+= is the app's own zoom, implemented as CSS `zoom` on <html>
  // (applyAppearance). A media query and `window.innerWidth` both evaluate
  // against the UNZOOMED initial containing block, so at 150% in a 1440px
  // window they both still say 1440 while the header has 416 layout px to lay
  // itself out in. Nothing fired. The topic stayed in the layout, the labels
  // stayed on the buttons, the three occasional actions stayed out of the menu,
  // and the title — which absorbs all the shrink by design — was crushed to
  // literally 0px and clipped. At 150% the header read "Search · Start a call ·
  // 📌 1 · [half a clock]" and nothing on it said which channel you were in.
  //
  // The panes. The channel list is draggable and the member panel toggles, so
  // the header's width changes by hundreds of pixels with the viewport
  // completely still. A viewport query cannot see either.
  //
  // So the header measures ITSELF. One ResizeObserver, the numbers below are
  // the header's own content box, and every give-way rule keys off it — which
  // is correct under zoom, under a dragged sidebar and under a toggled member
  // panel, all three, without knowing about any of them.
  let headEl = $state(null);
  // Seeded wide: the first frame before the observer reports must not flash the
  // squeezed layout on a full-width window.
  let headW = $state(1000);
  $effect(() => {
    const el = headEl;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver((entries) => {
      const e = entries[0];
      const box = e.contentBoxSize?.[0];
      headW = box ? box.inlineSize : e.contentRect.width;
    });
    ro.observe(el);
    return () => ro.disconnect();
  });

  // The give-way ladder, in the header's own pixels. Each tier is the width at
  // which the tier above it has run out of slack, measured on the real thing.
  //
  //   narrow  the topic leaves, and the buttons drop their word labels
  //   stub    the search field becomes a 32px magnifier
  //   packed  pins and the member-list toggle fold in too
  //
  // The channel NAME is not on this ladder. It is the last thing standing, at
  // every tier, by construction — see .title strong.
  // The numbers are the header's content box, measured on the real thing at
  // 1440x900 across the app's whole zoom range (100/110/120/125/130/150%),
  // which puts this header at 864, 733, 624, 576, 532 and 384px in turn. 700 is
  // the last width at which the full row still fits — at 624 it overflowed by
  // 86px and put Invite and the ⋯ menu itself off the right edge.
  const narrow = $derived(headW <= 640);
  const stub = $derived(headW <= 460);
  const packed = $derived(headW <= 380);

  // Pins, one click from the header again, while the window is wide enough
  // for it to be the fifth control at most; below that it is a row in ⋯.
  let winW = $state(window.innerWidth);
  const showPins = $derived(winW >= 1024 && !packed);
  function openPins() {
    insp.tab = "pins";
    if (!S.prefs.memberPanel) setPref("memberPanel", true);
  }
  const callLabel = $derived(S.voice ? channelShort(S.voice.channelId) : "");

  async function showInvite() {
    S.modal = { kind: "invite", code: await api.inviteCode(S.activeGuildId) };
  }

  // The transcript comes from the backend, which reads the store. Building it
  // here from S.messages meant exporting only the page the reader had loaded —
  // the last 200 plus whatever they scrolled through — which looked like a full
  // history right up until someone needed the rest of it.
  async function exportChannel() {
    if (!ch) return;
    try {
      const how = await saveText(
        `${ch.name}-history.md`,
        await api.exportMarkdown(S.activeGuildId, S.activeChannelId),
        "text/markdown",
      );
      if (how === "file") flash("History exported", "success");
      else if (how === "clipboard") flash("History copied to the clipboard", "success");
    } catch (err) {
      flash(err);
    }
  }

</script>

<svelte:window bind:innerWidth={winW} />

<header
  class="chat-head"
  class:narrow
  class:stub
  class:packed
  bind:this={headEl}
>
  <div class="row title">
    {#if g?.kind === "dm"}
      <!-- A speech bubble, not a pencil. The pencil reads as "edit this", which
           is what it means everywhere else in this app. -->
      <Icon name="bubble" size={15} />
      <strong>{g.name}</strong>
      {#if ownsPlace(g)}<OwnerMark size={12} label={ownerLabel(g)} />{/if}
    {:else if ch?.parent}
      <!-- A forum post: breadcrumb back to its board. -->
      <button
        class="thread-back"
        title="Close this post"
        aria-label="Close this post"
        onclick={closePost}
      >
        <Icon name="forum" size={14} />
        {activeGuild()?.channels.find((c) => c.id === ch.parent)?.name || "forum"}
        <span class="tb-sep">›</span>
      </button>
      <strong class="post-name" title={ch.name}>{ch.name}</strong>
    {:else if ch}
      <Icon name={channelTypeIcon(ch.type)} size={15} />
      <strong>{ch.name}</strong>
      {#if slowSecs > 0}
        <!-- Slow mode was invisible until you opened the panel that set it, so
             a room that had gone quiet looked broken rather than paced. The
             pill is a fixed-width chip and never competes with the name for
             room: it sits after the name and before the topic, which is the
             half that gives way. -->
        <span
          class="slow-pill"
          use:tooltip={`Slow mode: one message per member every ${slowLabel}`}
          aria-label={`Slow mode: one message per member every ${slowLabel}`}
        >
          <Icon name="clock" size={11} />
          {slowLabel}
        </span>
      {/if}
      {#if ch.topic}
        <span class="topic-sep"></span>
        <!-- Cut by fitText rather than by text-overflow alone, which keeps a
             space in front of its "…" whenever the cut falls after a word.
             The drawn text is shortened, so the whole topic stays in the tree
             for a screen reader and in the title for the pointer. -->
        <span class="chan-topic" title={ch.topic} aria-hidden="true" use:fitText={ch.topic}></span>
        <span class="sr-only">{ch.topic}</span>
      {/if}
    {:else}
      <span class="muted">No channel</span>
    {/if}
  </div>
  <div class="row">
    <form
      class="search-wrap"
      class:open={!!S.searchQuery || S.searchResults !== null}
      onsubmit={runSearch}
    >
      <!-- The operator syntax used to be documented in a title= on this field.
           It now lives as clickable chips in the results panel directly below,
           where it can be read without hovering and used without typing. -->
      <input
        class="search-box"
        class:busy={S.searchLoading || S.searchQuery || S.searchResults !== null}
        placeholder="Search…"
        aria-label="Search messages across all conversations"
        bind:this={searchEl}
        bind:value={S.searchQuery}
        oninput={() => queueSearch()}
        onkeydown={(e) => {
          // ↓ walks into the results (SearchPanel answers the arrows there).
          if (e.key === "ArrowDown" && !e.altKey) {
            const hit = document.querySelector(".search-panel .sp-hit");
            if (hit) {
              e.preventDefault();
              hit.focus();
            }
            return;
          }
          if (e.key === "Escape") {
            // blur FIRST: closeSearch homes focus to the composer, and a blur
            // after it would take that away again and land on <body>.
            e.currentTarget.blur();
            closeSearch();
          }
        }}
      />
      {#if S.searchLoading}
        <span class="search-spin" aria-hidden="true"></span>
      {:else if S.searchQuery || S.searchResults !== null}
        <button
          type="button"
          class="search-clear"
          aria-label="Clear search"
          title="Clear search"
          onclick={closeSearch}
        >
          <Icon name="close" size={11} />
        </button>
      {/if}
      <!-- Only ever seen in the squeezed band, where the field itself is a
           32px stub: without it the stub is a small empty box that says
           nothing about what it does. -->
      <span class="search-glyph" aria-hidden="true"><Icon name="search" size={14} /></span>
    </form>

    <!-- The call cluster leaves at the last tier. When you are IN this
         channel's call the panel below carries every one of these controls
         already, so the pill is a duplicate; when you are not, "Start a call"
         moves into the ⋯ menu with everything else. Below ~380px of header
         there is not room for a five-button pill AND a channel name, and the
         name wins. -->
    {#if packed}
      <!-- nothing: see the ⋯ menu -->
    {:else if S.voice && S.voice.channelId === S.activeChannelId}
      <!-- Nothing: you are in this conversation's call, and the call has one
           surface (§8, one of each). The sidebar's dock carries the room, the
           clock, the five controls and the hang-up; with the sidebar hidden
           the small floating dock does. This header used to carry a third
           copy — a pill with its own timer and hang-up, or "Open the call"
           beside "End call" — so a call showed two timers and two hang-ups. -->
    {:else if ch && peerInCall}
      <!-- The other side is already on a call — make it obvious and one-click. -->
      <button class="ghost iconbtn live-join" use:tooltip={"Join the call"} onclick={() => onJoinVoice()}>
        <span class="live-dot"></span>
        <span class="n">Live{peerSharing ? " · sharing" : ""} · Join</span>
      </button>
      <!-- Notes is a conversation with yourself, so it gets no call button: the
           phone shell already withheld it (`!g.dmNotes`) and the desktop header
           did not, which left a Call button whose only possible outcome was
           ringing an empty room. -->
    {:else if S.voice && S.prefs.sidebarHidden}
      <!-- In a call somewhere else with the sidebar put away: the bar that
           names the call and takes you back is gone with it, so the way back
           is said here too, beside the floating window. The dot and the clock
           answer to the same state as the window's. -->
      <button
        class="ghost iconbtn return-call"
        class:trouble={!callState.live}
        use:tooltip={{ text: `Back to ${callLabel || "your call"}` }}
        onclick={() => openCallStage()}
      >
        <span class="live-dot"></span>
        <span class="n">{callState.live ? `Return to call${clock ? ` · ${clock}` : ""}` : callState.label}</span>
      </button>
    {:else if S.voice}
      <!-- In a call somewhere else: the sidebar's call line and the floating
           window say which and take you back. A "Start a call" here would
           mean leaving that one, so it waits. -->
    {:else if ch && !g?.dmNotes && !g?.evicted && (g?.kind === "dm" || g?.kind === "meeting" || ch.type === "voice")}
      <!-- A guild text channel shows no call button: a speaker glyph there
           read as "join voice" and started a call in the text channel
           instead. Starting one is still in ⋯.

           No colour. Green means one thing in this app — the call is live — and
           this is the button that has not started one yet, in either place. It
           used to be success-green in a DM and neutral grey in a channel: the
           same button, the same label, the same icon, the same action, painted
           in the palette's "connected" colour purely because of what kind of
           conversation it was in. Nothing else on a DM screen is green, so it
           read as a status. The two branches above are where the colour lives:
           somebody else is live in here (danger), or you are in a call
           elsewhere (ok). -->
      <button
        class="ghost iconbtn"
        aria-label={g?.kind === "dm" || g?.kind === "meeting" ? "Start a call" : "Start a call in this channel"}
        use:tooltip={{ text: g?.kind === "dm" || g?.kind === "meeting" ? "Start a call" : "Start a call in this channel" }}
        onclick={() => onJoinVoice()}
      >
        <Icon name="speaker" />{#if g?.kind === "dm" || g?.kind === "meeting"}
          <span class="n">Start a call</span>{/if}
      </button>
    {/if}

    {#if ch && g?.kind !== "dm" && showPins}
      <button
        class="ghost iconbtn"
        class:pin-active={S.prefs.memberPanel && insp.tab === "pins"}
        use:tooltip={{ text: "Pinned messages" }}
        aria-label="Pinned messages"
        onclick={openPins}
      >
        <Icon name="pin" />
      </button>
    {/if}
    {#if ch && g?.kind !== "dm" && !packed}
      <!-- The inspector: members, pins, files and threads beside the
           conversation. People glyph + the member count; Ctrl+U as before. -->
      <button
        class="ghost iconbtn insp-toggle"
        class:pin-active={S.prefs.memberPanel}
        use:tooltip={{ text: "Members, pins and files · Ctrl+U" }}
        aria-label="Members, pins and files"
        aria-pressed={S.prefs.memberPanel}
        onclick={toggleMemberPanel}
      >
        <Icon name="members" />{#if memberCount}<span class="n count">{memberCount}</span>{/if}
      </button>
    {/if}

    {#if g}
      <Menu label="More" icon="dots">
        <!-- The overflow well. Everything that leaves the bar arrives here, in
             the order it left, so a squeezed header hides nothing — it moves
             things. Before this the last tier simply clipped: at 150% zoom
             Invite, the member toggle and the events button were off the right
             edge with no menu entry, no horizontal scroll and no sign they
             existed. -->
        {#if packed && S.voice && S.voice.channelId === S.activeChannelId}
          <button class="menu-item" onclick={() => openCallStage()}>
            <Icon name="screen" size={14} /> Open the call
          </button>
          <button class="menu-item" onclick={onLeaveVoice}>
            <Icon name="door" size={14} /> Leave the call
          </button>
        {:else if packed && ch && peerInCall}
          <button class="menu-item" onclick={() => onJoinVoice()}>
            <Icon name="speaker" size={14} /> Join the call — it's live
          </button>
        {:else if packed && S.voice}
          <button class="menu-item" onclick={() => openCallStage()}>
            <Icon name="speaker" size={14} /> Open {callLabel || "the call"}
          </button>
        {:else if packed && ch && !g?.dmNotes && !g?.evicted}
          <button class="menu-item" onclick={() => onJoinVoice()}>
            <Icon name="speaker" size={14} /> Start a call
          </button>
        {/if}
        {#if packed && ch && g.kind !== "dm"}
          <button class="menu-item" onclick={toggleMemberPanel}>
            <Icon name="members" size={14} />
            {S.prefs.memberPanel ? "Hide members, pins and files" : "Members, pins and files"}
          </button>
        {/if}
        <div class="menu-sep"></div>
        {#if ch && !g.dmNotes}
          <!-- First in ⋯, as the channel's own menu has it: the level in force
               ticked, and "Use guild default" as its own answer. Flat, because
               menus here have no fly-outs. -->
          <div class="menu-head">Notifications</div>
          {#if g.kind !== "dm"}
            <button class="menu-item" class:active={!S.notifs.channels[ch.id]} onclick={() => setChannelNotifs(ch.id, null)}>
              <Icon name="bell" size={14} /> Use guild default ({levelLabel(guildNotifLevel(g.id))})
              {#if !S.notifs.channels[ch.id]}<span class="tick"><Icon name="check" size={13} /></span>{/if}
            </button>
          {/if}
          {#each LEVELS as l (l.id)}
            <button
              class="menu-item"
              class:active={S.notifs.channels[ch.id] === l.id}
              onclick={() => setChannelNotifs(ch.id, g.kind === "dm" && l.id === "all" ? null : l.id)}
            >
              <Icon name={l.id === "none" ? "bellOff" : "bell"} size={14} />
              {l.label}
              {#if S.notifs.channels[ch.id] === l.id || (g.kind === "dm" && l.id === "all" && !S.notifs.channels[ch.id])}<span class="tick"><Icon name="check" size={13} /></span>{/if}
            </button>
          {/each}
          <div class="menu-sep"></div>
        {/if}
        {#if !packed && ch && !g.dmNotes && !g.evicted && g.kind !== "dm" && g.kind !== "meeting" && ch.type !== "voice" && !S.voice && !peerInCall}
          <button class="menu-item" onclick={() => onJoinVoice()}>
            <Icon name="speaker" size={14} /> Start a call in this channel
          </button>
        {/if}
        {#if ch && g.kind !== "dm" && !showPins}
          <button class="menu-item" onclick={openPins}>
            <Icon name="pin" size={14} /> Pinned messages
          </button>
        {/if}
        {#if ch}
          <button class="menu-item" onclick={() => markRead(S.activeChannelId)}>
            <Icon name="check" size={14} /> Mark as read
          </button>
          <button class="menu-item" onclick={() => (S.modal = { kind: "scheduled" })}>
            <Icon name="clock" size={14} /> Scheduled messages
          </button>
        {/if}
        <button class="menu-item" onclick={() => (S.modal = { kind: "events" })}>
          <Icon name="calendar" size={14} /> {g.dmNotes ? "Private events" : "Events"}
        </button>
        {#if g.canManage && g.kind !== "dm"}
          <button class="menu-item" onclick={showInvite}>
            <Icon name="personPlus" size={14} /> Invite people
          </button>
        {/if}
        {#if ch}
          <button
            class="menu-item"
            onclick={() => (S.modal = { kind: "disappear", channelId: S.activeChannelId })}
          >
            <Icon name="eyeOff" size={14} />
            {ephTTL > 0 ? `Disappearing after ${ttlLabel(ephTTL)}` : "Disappearing messages"}
          </button>
        {/if}
        {#if ch && g.kind !== "dm" && canManageChannels && ch.type !== "voice" && !ch.parent}
          <button class="menu-item" onclick={() => (S.modal = { kind: "channelTopic", channel: ch })}>
            <Icon name="gear" size={14} /> Channel settings
          </button>
        {/if}
        {#if ch && g.kind !== "dm"}
          <button class="menu-item" onclick={copyChannelLink}>
            <Icon name="link" size={14} /> Copy channel link
          </button>
        {/if}
        {#if !S.isMobile}
          <button class="menu-item" onclick={() => setPref("sidebarHidden", true)}>
            <Icon name="menu" size={14} /> Hide sidebar
          </button>
        {/if}
        <div class="menu-sep"></div>
        {#if ch}
          <button class="menu-item" onclick={exportChannel}>
            <Icon name="download" size={14} /> Export history
          </button>
        {/if}
        {#if g.kind === "dm"}
          {#if g.dmPeer}
            <button class="menu-item" onclick={(e) => openProfilePopover(g.dmPeer, e.currentTarget)}>
              <Icon name="spark" size={14} /> View profile
            </button>
          {/if}
          {#if (g.dmMembers ?? 2) > 2}
            <button
              class="menu-item"
              onclick={() =>
                (S.modal = { kind: "renameGroup", guildId: g.id, current: g.dmNamed ? g.name : "" })}
            >
              <Icon name="edit" size={14} /> Rename group
            </button>
          {/if}
        {:else}
          <!-- One door instead of a pile: emoji, roles, bans and rename all
               live inside the guild hub now (renaming is the hub's Overview
               panel). The menu keeps only the exit — leaving isn't managing. -->
          <button class="menu-item" onclick={openGuildHub}>
            <Icon name="gear" size={14} /> Guild settings
          </button>
          <!-- NOT inside a post. The most destructive action in the product was
               the last item of a forum thread's overflow menu, under two items
               that had nothing to do with the post either; the post's own
               actions live in its header now, and deleting the guild lives in
               the danger zone of guild settings where it has proper framing. -->
          {#if !ch?.parent}
            <div class="menu-sep"></div>
            <button class="menu-item danger" onclick={() => confirmLeaveGuild(g)}>
              <Icon name={g.isOwner ? "trash" : "door"} size={14} />
              {leaveGuildLabel(g)}
            </button>
          {/if}
        {/if}
      </Menu>
    {/if}
  </div>
</header>

<style>
  .chat-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    min-height: var(--header-h);
    box-sizing: border-box;
    padding: 0 12px var(--header-pad-b) var(--sp-edge);
    border-bottom: 1px solid var(--border);
    gap: 10px;
    position: relative;
    /* Above the message rows (which raise their hover bar / attachments to
       z-30): the header's dropdown menus overhang the feed, and a menu that
       paints UNDER a message also hands it the pointer — that's what made
       hovering the guild menu pop message hover-bars and images. */
    z-index: 40;
  }
  /* An accent thread under the channel name, fading out rightward — the
     header quietly points at where you are. */
  /* Where you ARE outranks everything you can do here, and this is the third
     time that has had to be said in CSS. First the title was a plain flex item
     beside an action row of nowrap buttons, and flex handed the width to the
     side that refused to shrink: the name measured 0px between 769 and 1200px.
     Then it got `min-width: 8ch`, which stopped the name vanishing but left the
     shrink to be SPLIT between the name and the topic inside it — so at 1440,
     an entirely ordinary laptop, `#general` rendered as `# ge…` while its topic
     kept thirty characters of a sentence nobody needs.
     The rule now is an order, not a ratio, and it is enforced INSIDE the title
     rather than by a floor on the title itself. The search box gives first (it
     has `min-width: 0` and shrinks to a stub); then the topic, all of it, down
     to nothing and then out of the layout entirely below the narrow contract;
     the name is `flex: 0 0 auto` and simply never participates. A floor here
     instead — `min-width: min-content` — looks like the same idea and is not:
     Chromium counts the topic's full width in the title's min-content, so the
     floor grew to include the sentence and pushed the action row 249px off the
     end of the header at 1280. The clip is the backstop for a name longer than
     the whole bar; it is not the mechanism. */
  .title {
    gap: 6px;
    color: var(--text-muted);
    flex: 1 1 auto;
    /* The floor. `min-width: 0` alone is what let the action row take the whole
       bar and clip the name to nothing — the fourth and, with the give-way
       ladder above it, final act of this argument. It is safe to state as a
       floor now in a way it was not before: the name no longer participates in
       the shrink (flex: 0 0 auto), so this cannot be split with the topic the
       way `min-width: 8ch` was, and it is a fixed character count rather than
       min-content, so Chromium cannot fold the topic's whole sentence into it.
       Twelve characters is "# announcem…" — enough to know where you are, and
       six at the last tier, where six is all there is. */
    min-width: 12ch;
    overflow: hidden;
  }
  /* The action row gives up its own slack — the search box has min-width:0 and
     collapses first — but never goes below what its buttons actually measure.
     With a plain `min-width: 0` flex was happy to hand it a box narrower than
     its contents, and since none of the buttons wrap, the last two simply
     spilled past the column's edge and were clipped: at 800px the Invite
     button and the overflow menu were on screen but unreachable. */
  .chat-head > .row:last-child {
    flex: 0 1 auto;
    min-width: min-content;
  }
  /* The search field is the row's slack, and it is spent before the topic is.
     It used to be a fixed 190px input, which made 190px part of the action
     row's min-content: the row could not shrink at all, so every pixel the bar
     lacked came out of the topic — at 1280 a thirty-character topic showed as
     "t…" beside a search field with nothing typed in it. Now the field's width
     is a flex basis with a floor, and the row shrinks forty times faster than
     the title, so the field narrows to its floor first and only then does the
     topic start to give. Focus still widens it: you get the room when you are
     actually typing. */
  /* Search is a 32px control until it is used; then it becomes a 280px
     field in place, and the topic is what gives way to it. */
  .search-wrap {
    flex: 0 0 32px;
    min-width: 0;
  }
  .search-wrap:focus-within,
  .search-wrap.open {
    flex-basis: min(280px, 40 * var(--vw));
  }
  .chat-head:not(.stub) > .row:last-child {
    flex-shrink: 40;
  }
  @media (prefers-reduced-motion: reduce) {
    .search-wrap {
      transition: none;
    }
  }
  /* The channel-type glyph carries the accent — a small "you are here" tint. */
  .title :global(svg) {
    color: var(--text-faint);
    flex-shrink: 0;
  }
  /* The cap is the one concession: a channel someone named with a sentence
     cannot be allowed to push the whole action row off the header. Everything
     shorter than the cap is immune to the squeeze. */
  .title strong {
    color: var(--text);
    font-family: var(--display-font);
    font-size: var(--fs-title);
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 0 0 auto;
    max-width: min(32ch, 38 * var(--vw));
  }
  /* At the last tier the name finally does shrink — but it ellipsises inside
     its own box rather than being clipped by the container, so what is left of
     it is a readable prefix with a "…" saying there is more, instead of a bare
     hash glyph and nothing. */
  /* At the last tier the header's own frame gives way too: 16px of padding on
     each side and a 10px gutter is a sixth of the bar when the bar is 190px. */
  .chat-head.packed {
    padding-left: 10px;
    padding-right: 10px;
    gap: 6px;
  }
  .chat-head.packed .row {
    gap: var(--sp-1);
  }
  .chat-head.packed .title {
    min-width: 6ch;
  }
  .chat-head.packed .title strong {
    flex: 0 1 auto;
    min-width: 0;
  }
  /* A forum post inverts the order, because a post TITLE is a sentence and the
     board it belongs to is the one word that gets you back. The breadcrumb
     keeps its width; the sentence ellipsises. */
  .title strong.post-name {
    flex: 0 1 auto;
    min-width: 0;
    max-width: none;
  }
  .thread-back {
    flex: 0 0 auto;
    max-width: 20ch;
  }
  /* Fixed content, so it never takes width from the channel name — the topic
     is the part that absorbs the shrink (see .chan-topic). */
  .slow-pill {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 1px 7px;
    border-radius: 999px;
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--warn-text);
    background: color-mix(in srgb, var(--warn) 16%, transparent);
  }
  .topic-sep {
    width: 1px;
    align-self: stretch;
    background: var(--border);
    margin: 3px 2px;
    flex-shrink: 0;
  }
  .chan-topic {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 0 1 auto;
    min-width: 0;
  }
  /* Below the narrow contract the topic stops being a fragment and starts being
     a smear — two words and an ellipsis, in the space the name wants. It is the
     one thing on this bar the reader can go and read somewhere else (the
     channel's own menu), so it leaves rather than starving the name. */
  .chat-head.narrow .topic-sep,
  .chat-head.narrow .chan-topic {
    display: none;
  }
  .search-wrap {
    position: relative;
    display: inline-flex;
    align-items: center;
  }
  .search-box {
    /* Fills .search-wrap, which owns the width (see above). */
    width: 100%;
    min-width: 0;
    height: 32px;
    padding: 0 10px;
    font-size: var(--fs-ui);
    transition: width var(--dur-calm) var(--ease-out), border-color var(--dur-standard) ease, box-shadow var(--dur-standard) ease, background var(--dur-standard) ease;
  }
  @media (prefers-reduced-motion: reduce) {
    .search-box {
      transition: border-color var(--dur-standard) ease, box-shadow var(--dur-standard) ease;
    }
  }
  /* Leave room for the clear button / spinner once there's something to show. */
  .search-box.busy {
    padding-right: 26px;
  }
  /* Collapsed: the box is a bare magnifier button. */
  .search-wrap:not(:focus-within):not(.open) .search-box {
    background: transparent;
    box-shadow: none;
    border-color: transparent;
    color: transparent;
    cursor: pointer;
    padding: 0;
  }
  .search-wrap:not(:focus-within):not(.open) .search-box::placeholder {
    color: transparent;
  }
  .search-wrap:not(:focus-within):not(.open):hover .search-box {
    background: var(--bg-3);
  }
  .search-glyph {
    display: grid;
    place-items: center;
    position: absolute;
    inset: 0;
    color: var(--text-muted);
    pointer-events: none;
  }
  .search-wrap:focus-within .search-glyph,
  .search-wrap.open .search-glyph {
    display: none;
  }
  /* At the `stub` tier there is no width left to share: giving the channel name
     its floor squeezed this field to literally zero, which is worse than not
     drawing it. So it becomes a 32px stub with a magnifier in it and floats
     back out over the button row the moment it has focus or a query — the
     buttons it covers are all still one Escape away. */
  .chat-head.stub .search-wrap {
    flex: 0 0 32px;
    width: 32px;
    min-width: 0;
  }
  .chat-head.stub .search-box,
  .chat-head.stub .search-box:focus {
    width: 100%;
    min-width: 0;
    padding: 5px 6px;
  }
  .chat-head.stub .search-box.busy {
    padding-right: 26px;
  }
  .chat-head.stub .search-wrap:focus-within,
  .chat-head.stub .search-wrap.open {
    position: absolute;
    right: 16px;
    top: 50%;
    transform: translateY(-50%);
    width: min(320px, calc(100% - 48px));
    z-index: 3;
  }
  .chat-head.stub .search-glyph {
    display: grid;
    place-items: center;
    position: absolute;
    inset: 0;
    color: var(--text-muted);
    pointer-events: none;
  }
  .chat-head.stub .search-wrap:focus-within .search-glyph,
  .chat-head.stub .search-wrap.open .search-glyph {
    display: none;
  }
  .search-clear {
    position: absolute;
    right: 5px;
    padding: 2px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: transparent;
    color: var(--text-muted);
  }
  .search-clear:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .search-spin {
    position: absolute;
    right: 8px;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 2px solid var(--accent-soft);
    border-top-color: var(--accent);
    animation: search-spin 0.7s linear infinite;
    pointer-events: none;
  }
  @keyframes search-spin {
    to {
      transform: rotate(360deg);
    }
  }
  .iconbtn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 5px;
    height: 32px;
    min-width: 32px;
    padding: 0 7px;
    box-shadow: none;
    border-color: transparent;
    color: var(--text-muted);
  }
  .iconbtn:hover {
    color: var(--text);
    background: var(--bg-3);
  }
  /* The ⋯ trigger is the fourth control and wears the same quiet glyph look. */
  .chat-head :global(.trigger) {
    width: 32px;
    height: 32px;
    border: none;
    background: transparent;
    color: var(--text-muted);
  }
  .chat-head :global(.trigger:hover) {
    background: var(--bg-3);
    color: var(--text);
  }
  .n {
    font-size: var(--fs-compact);
    font-variant-numeric: tabular-nums;
    /* These labels are two and three words now ("Start a call", "Return to
       call · 12:34"), and a header button that wraps its own label is a header
       button that grows a second line and shoves the row taller. */
    white-space: nowrap;
  }
  /* Squeezed column: the words come off the call buttons and the glyph plus its
     tooltip carries them instead. The pin COUNT stays — that is data, not a
     label, and there is no other place it appears. */
  .chat-head.narrow .n:not(.count) {
    display: none;
  }
  /* Pressed: the selected-control look, ink on the hover ground. */
  .pin-active {
    color: var(--text);
    background: var(--bg-3);
  }
  .insp-toggle .count {
    font-weight: 600;
  }
  @media (prefers-reduced-motion: reduce) {
    .iconbtn:hover {
      transform: none;
    }
  }
  /* Same type and box as "Start a call" beside it: it used to inherit the
     body's 16px and stand a size larger and a few pixels taller than every
     other button on the bar. */
  .invite {
    padding: 6px 12px;
  }
  .invite-label {
    font-size: var(--fs-compact);
    white-space: nowrap;
  }
  /* Peer is already on the call — a live, inviting affordance. */
  .iconbtn.live-join {
    color: var(--danger-text);
    border-color: color-mix(in srgb, var(--danger) 50%, transparent);
    background: color-mix(in srgb, var(--danger) 12%, transparent);
    font-weight: 600;
  }
  .iconbtn.live-join:hover {
    background: color-mix(in srgb, var(--danger) 20%, transparent);
  }
  /* Your OWN call, running elsewhere: green, the room you are already in,
     not the red of somebody else's live room you could join. */
  .iconbtn.return-call {
    color: var(--ok-text);
    border-color: color-mix(in srgb, var(--ok) 45%, transparent);
    background: var(--ok-soft);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .iconbtn.return-call:hover {
    background: color-mix(in srgb, var(--ok) 22%, transparent);
  }
  .iconbtn.return-call .live-dot {
    background: var(--ok);
    animation: none;
  }
  .iconbtn.return-call.trouble {
    color: var(--warn-text);
    border-color: color-mix(in srgb, var(--warn) 45%, transparent);
  }
  .iconbtn.return-call.trouble .live-dot {
    background: var(--warn);
  }
  .live-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: #f04747;
    animation: ch-live-pulse 1.4s ease-in-out infinite;
  }
  @keyframes ch-live-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .live-dot {
      animation: none;
    }
  }
  /* The board's name is a breadcrumb, and a breadcrumb that line-breaks mid-word
     ("help-" / "desk") reads as a rendering fault. It wraps as a unit or it
     ellipsises; it never splits. */
  .thread-back {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    padding: 3px 8px;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: var(--fs-ui);
    cursor: pointer;
  }
  .thread-back:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .tb-sep {
    color: var(--text-faint);
  }
  .menu-head {
    padding: 6px 10px 2px;
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .tick {
    margin-left: auto;
    display: inline-flex;
    color: var(--accent-hover);
  }
</style>
