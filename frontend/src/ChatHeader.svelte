<script>
  // Chat column header: channel name, search, pins, voice, guild actions.
  import Icon from "./Icon.svelte";
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
    jumpToChannel,
    channelShort,
    callRoster,
    callHealth,
  } from "./lib/state.svelte.js";
  import { callClock } from "./lib/calltimer.svelte.js";
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

  let { onJoinVoice, onLeaveVoice, onToggleMute, onToggleDeafen, onToggleShare, onToggleCamera } =
    $props();

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
  const pinnedCount = $derived(S.messages.filter((m) => m.pinned && !m.deleted).length);
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
  //   tight   disappearing / events / invite fold into the ⋯ menu
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
  const narrow = $derived(headW <= 700);
  const tight = $derived(!S.isMobile && narrow);
  const stub = $derived(headW <= 460);
  const packed = $derived(headW <= 380);
  const clock = $derived(callClock());
  const callLabel = $derived(S.voice ? channelShort(S.voice.channelId) : "");
  // One state machine for the call — see callHealth in state.svelte.js.
  const callState = $derived(callHealth());

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
        <span class="chan-topic" title={ch.topic}>{ch.topic}</span>
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
        placeholder="Search messages…"
        aria-label="Search messages across all conversations"
        bind:this={searchEl}
        bind:value={S.searchQuery}
        oninput={() => queueSearch()}
        onkeydown={(e) => {
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
    {:else if S.voice && S.voice.channelId === S.activeChannelId && (g?.kind === "dm" || g?.kind === "meeting")}
      <!-- In a DM call, the call box carries the controls; the header is just a
           one-click hang-up so clicking "call" again intuitively leaves. -->
      <button class="ghost iconbtn endcall" use:tooltip aria-label="Leave call" onclick={onLeaveVoice}>
        <Icon name="door" /> <span class="n">End call</span>
      </button>
    {:else if S.voice && S.voice.channelId === S.activeChannelId}
      <!-- Active voice collapses to one pill with mute + leave inside it. -->
      <span class="voice-pill" class:trouble={!callState.live}>
        <!-- callRoster, not a second count: this is the same list the stage
             draws its tiles from, so the number and the picture cannot drift
             apart while somebody is arriving or leaving. -->
        <span class="pill-label" title="{callRoster().length} in this call">
          <Icon name="speaker" size={12} />
          {callRoster().length}
        </span>
        <span class="pill-sep"></span>
        <!-- The same controls, the same two colours, on every surface that
             carries them: mic and deafen light DANGER when they are stopping
             something, camera and screen light OK when they are sending
             something. This pill used to have no lit state for the mic at all —
             the glyph swapped and nothing else did — while the sidebar bar lit
             the SAME state green, which is this row's colour for "on the air". -->
        <button class="callbtn xs cut" class:on={S.muted} title={S.muted ? "Unmute mic" : "Mute mic"} aria-label={S.muted ? "Unmute mic" : "Mute mic"} aria-pressed={S.muted} onclick={onToggleMute}>
          <Icon name={S.muted ? "micOff" : "mic"} size={15} />
        </button>
        <button class="callbtn xs cut" class:on={S.deafened} title={S.deafened ? "Undeafen" : "Deafen"} aria-label={S.deafened ? "Undeafen" : "Deafen"} aria-pressed={S.deafened} onclick={onToggleDeafen}>
          <Icon name={S.deafened ? "deafened" : "speaker"} size={15} />
        </button>
        <button class="callbtn xs" class:on={S.cameraOn} title={S.cameraOn ? "Turn off camera" : "Turn on camera"} aria-label={S.cameraOn ? "Turn off camera" : "Turn on camera"} aria-pressed={S.cameraOn} onclick={onToggleCamera}>
          <Icon name={S.cameraOn ? "cameraOff" : "camera"} size={15} />
        </button>
        <button class="callbtn xs" class:on={S.sharing} title={S.sharing ? "Stop sharing" : "Share screen"} aria-label={S.sharing ? "Stop sharing" : "Share screen"} aria-pressed={S.sharing} onclick={onToggleShare}>
          <Icon name={S.sharing ? "screenOff" : "screen"} size={15} />
        </button>
        <button class="callbtn xs hang" title="Leave voice" aria-label="Leave voice" onclick={onLeaveVoice}>
          <Icon name="door" size={15} />
        </button>
      </span>
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
    {:else if S.voice}
      <!-- You are already in a call, somewhere else. This button used to read
           "Voice" here — the same word as the button that STARTS one, two
           channels away — and clicking it took you back to the call you were
           in with no explanation. Same label, two meanings. The clock says
           which of the two this is without needing the sentence. -->
      <button
        class="ghost iconbtn return-call"
        class:trouble={!callState.live}
        use:tooltip={{ text: `Back to ${callLabel || "your call"}` }}
        onclick={() => jumpToChannel(S.voice.channelId)}
      >
        <span class="live-dot"></span>
        <!-- The dot and the clock are the same claim the sidebar bar and the
             dock make, so they answer to the same state — a green dot beside a
             running number over a call that is not carrying is the whole of the
             bug this button used to share with them. -->
        <span class="n">
          {callState.live ? `Return to call${clock ? ` · ${clock}` : ""}` : callState.label}
        </span>
      </button>
    {:else if ch && !g?.dmNotes}
      <button
        class="ghost iconbtn"
        class:call={g?.kind === "dm" || g?.kind === "meeting"}
        use:tooltip={{ text: g?.kind === "dm" || g?.kind === "meeting" ? "Start a call" : "Start a call in this channel" }}
        onclick={() => onJoinVoice()}
      >
        <Icon name="speaker" /> <span class="n">Start a call</span>
      </button>
    {/if}

    {#if ch && !packed}
      <button
        class="ghost iconbtn"
        class:pin-active={S.showPins}
        use:tooltip
        aria-label="Pinned messages"
        onclick={() => (S.showPins = !S.showPins)}
      >
        <Icon name="pin" />{#if pinnedCount}<span class="n count">{pinnedCount}</span>{/if}
      </button>
    {/if}

    {#if ch && !tight}
      <button
        class="ghost iconbtn"
        class:pin-active={ephTTL > 0}
        use:tooltip={{ text: ephTTL > 0 ? `Disappearing after ${ttlLabel(ephTTL)}` : "Disappearing messages" }}
        aria-label="Disappearing messages"
        onclick={() => (S.modal = { kind: "disappear", channelId: S.activeChannelId })}
      >
        <Icon name="clock" />
      </button>
    {/if}

    {#if g && !tight}
      <!-- The calendar — the thing that replaces "so when are we on?"
           scroll-back. Every room gets one: a guild's is the crew's shared
           board, a DM's is "when are we hopping on?" between its people, and
           Notes' is your private list (a single-member group, so private by
           construction). Mounted like Pins: a button here, a sheet entry on
           phones (MobileShell's ⋯, since this header never renders there). -->
      <button
        class="ghost iconbtn"
        use:tooltip={{ text: g.dmNotes ? "Private events" : "Events" }}
        aria-label={g.dmNotes ? "Private events" : g.kind === "dm" ? "Events in this conversation" : "Guild events"}
        onclick={() => (S.modal = { kind: "events" })}
      >
        <Icon name="calendar" />
      </button>
    {/if}

    {#if ch && g?.kind !== "dm" && !packed}
      <button
        class="ghost iconbtn"
        class:pin-active={S.prefs.memberPanel}
        use:tooltip={{ text: "Toggle member list (Ctrl+U)" }}
        aria-label="Toggle member list"
        aria-pressed={S.prefs.memberPanel}
        onclick={toggleMemberPanel}
      >
        <Icon name="members" />
      </button>
    {/if}

    {#if g?.canManage && g?.kind !== "dm" && !tight}
      <button class="ghost invite" onclick={showInvite}>Invite</button>
    {/if}

    {#if g}
      <Menu label="More" icon="chevron">
        <!-- The overflow well. Everything that leaves the bar arrives here, in
             the order it left, so a squeezed header hides nothing — it moves
             things. Before this the last tier simply clipped: at 150% zoom
             Invite, the member toggle and the events button were off the right
             edge with no menu entry, no horizontal scroll and no sign they
             existed. -->
        {#if packed && S.voice && S.voice.channelId === S.activeChannelId}
          <button class="menu-item" onclick={onLeaveVoice}>
            <Icon name="door" size={14} /> Leave the call
          </button>
        {:else if packed && ch && peerInCall}
          <button class="menu-item" onclick={() => onJoinVoice()}>
            <Icon name="speaker" size={14} /> Join the call — it's live
          </button>
        {:else if packed && S.voice}
          <button class="menu-item" onclick={() => jumpToChannel(S.voice.channelId)}>
            <Icon name="speaker" size={14} /> Back to {callLabel || "your call"}
          </button>
        {:else if packed && ch && !g?.dmNotes}
          <button class="menu-item" onclick={() => onJoinVoice()}>
            <Icon name="speaker" size={14} /> Start a call
          </button>
        {/if}
        {#if packed && ch}
          <button class="menu-item" onclick={() => (S.showPins = !S.showPins)}>
            <Icon name="pin" size={14} />
            {pinnedCount ? `Pinned messages (${pinnedCount})` : "Pinned messages"}
          </button>
        {/if}
        {#if packed && ch && g.kind !== "dm"}
          <button class="menu-item" onclick={toggleMemberPanel}>
            <Icon name="members" size={14} />
            {S.prefs.memberPanel ? "Hide the member list" : "Show the member list"}
          </button>
        {/if}
        {#if tight && ch}
          <button
            class="menu-item"
            onclick={() => (S.modal = { kind: "disappear", channelId: S.activeChannelId })}
          >
            <Icon name="clock" size={14} />
            {ephTTL > 0 ? `Disappearing after ${ttlLabel(ephTTL)}` : "Disappearing messages"}
          </button>
        {/if}
        {#if tight}
          <button class="menu-item" onclick={() => (S.modal = { kind: "events" })}>
            <Icon name="calendar" size={14} /> {g.dmNotes ? "Private events" : "Events"}
          </button>
        {/if}
        {#if tight && g.canManage && g.kind !== "dm"}
          <button class="menu-item" onclick={showInvite}>
            <Icon name="plus" size={14} /> Invite people
          </button>
        {/if}
        {#if tight || packed}
          <div class="menu-sep"></div>
        {/if}
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
          <button class="menu-item" onclick={() => (S.modal = { kind: "guildHub" })}>
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
              {g.isOwner ? "Delete guild" : "Leave guild"}
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
    padding: 11px 16px;
    border-bottom: 1px solid var(--border);
    gap: 10px;
    /* Faint drop under the header: elevation over the feed it caps. */
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.1);
    position: relative;
    /* Above the message rows (which raise their hover bar / attachments to
       z-30): the header's dropdown menus overhang the feed, and a menu that
       paints UNDER a message also hands it the pointer — that's what made
       hovering the guild menu pop message hover-bars and images. */
    z-index: 40;
  }
  /* An accent thread under the channel name, fading out rightward — the
     header quietly points at where you are. */
  .chat-head::after {
    content: "";
    position: absolute;
    left: 16px;
    bottom: -1px;
    /* Clamped to the bar. A flat 180px starting 16px in is 196px of box, which
       on a 189px header is 7px of horizontal overflow — invisible, because it
       is a one-pixel hairline, and enough to make the header report itself as
       scrollable at the tightest zoom. */
    width: min(180px, calc(100% - 32px));
    height: 1px;
    background: linear-gradient(90deg, color-mix(in srgb, var(--accent) 55%, transparent), transparent);
    pointer-events: none;
  }
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
  .search-wrap {
    flex: 0 1 auto;
  }
  /* The channel-type glyph carries the accent — a small "you are here" tint. */
  .title :global(svg) {
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  /* The cap is the one concession: a channel someone named with a sentence
     cannot be allowed to push the whole action row off the header. Everything
     shorter than the cap is immune to the squeeze. */
  .title strong {
    color: var(--text);
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
    min-width: 0;
  }
  .search-box {
    /* Fluid: shrinks with the window so it never overlaps the channel name,
       but stays usable (min 84px) and caps at its comfortable width. Focus
       stretches it — room appears exactly when you start typing. */
    width: clamp(84px, 20 * var(--vw), 190px);
    min-width: 0;
    padding: 5px 10px;
    font-size: var(--fs-ui);
    transition: width 0.25s var(--ease-out), border-color var(--dur-standard) ease, box-shadow var(--dur-standard) ease, background var(--dur-standard) ease;
  }
  .search-box:focus {
    width: clamp(84px, 28 * var(--vw), 260px);
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
  .search-glyph {
    display: none;
  }
  /* At the `stub` tier there is no width left to share: giving the channel name
     its floor squeezed this field to literally zero, which is worse than not
     drawing it. So it becomes a 32px stub with a magnifier in it and floats
     back out over the button row the moment it has focus or a query — the
     buttons it covers are all still one Escape away. */
  .chat-head.stub .search-wrap {
    flex: 0 0 auto;
    width: 32px;
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
    gap: 5px;
    padding: 6px 9px;
    transition:
      background var(--dur-standard) ease,
      color var(--dur-standard) ease,
      border-color var(--dur-standard) ease,
      transform var(--dur-quick) ease;
  }
  .iconbtn:hover {
    transform: translateY(-1px);
  }
  .iconbtn:active {
    transform: none;
  }
  .n {
    font-size: var(--fs-compact);
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
  .pin-active {
    color: var(--accent-hover);
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .voice-pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--ok-text);
    padding: 3px 4px 3px 9px;
    background: var(--ok-soft);
    border-radius: var(--radius-lg);
    white-space: nowrap;
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ok) 25%, transparent);
    /* One quiet breath while the call is live (the pill only exists then). */
    animation: pill-breathe 3.6s ease-in-out infinite;
  }
  @keyframes pill-breathe {
    0%,
    100% {
      box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--ok) 25%, transparent);
    }
    50% {
      box-shadow:
        inset 0 0 0 1px color-mix(in srgb, var(--ok) 35%, transparent),
        0 0 10px color-mix(in srgb, var(--ok) 30%, transparent);
    }
  }
  /* Green is the claim that the call is carrying. It stops making it. */
  .voice-pill.trouble {
    color: var(--warn-text);
    background: var(--warn-soft);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--warn) 35%, transparent);
    animation: none;
  }
  .voice-pill.trouble .pill-sep {
    background: color-mix(in srgb, var(--warn) 35%, transparent);
  }
  @media (prefers-reduced-motion: reduce) {
    .voice-pill {
      animation: none;
    }
    .iconbtn:hover {
      transform: none;
    }
  }
  .pill-label {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    line-height: 1;
  }
  .pill-sep {
    width: 1px;
    align-self: stretch;
    margin: 2px 2px;
    background: color-mix(in srgb, var(--ok) 35%, transparent);
  }
  /* The buttons are .callbtn (app.css) at their smallest size — the same
     controls, in the same order, with the same on/cut/hang colours as the
     stage bar, the sidebar bar and the dock. */
  .invite {
    padding: 6px 12px;
  }
  .iconbtn.call {
    color: var(--ok-text);
    border-color: color-mix(in srgb, var(--ok) 45%, transparent);
  }
  .iconbtn.call:hover {
    background: var(--ok-soft);
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
  /* Your OWN call, running elsewhere. Green rather than the red of "somebody
     else is live in here": one is a room you could join, the other is the room
     you are already in. */
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
  }
  .iconbtn.return-call.trouble {
    color: var(--warn-text);
    border-color: color-mix(in srgb, var(--warn) 45%, transparent);
  }
  .iconbtn.return-call.trouble .live-dot {
    background: var(--warn);
    animation: none;
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
  .iconbtn.endcall {
    color: var(--danger-text);
    border-color: color-mix(in srgb, var(--danger) 45%, transparent);
  }
  .iconbtn.endcall:hover {
    background: var(--danger-soft);
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
</style>
