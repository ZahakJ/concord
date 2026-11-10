<script>
  // Ctrl+K command palette: fuzzy jump to any channel / DM / guild, plus app
  // actions. A leading ">" or "/" filters to actions only; otherwise actions
  // rank alongside destinations under their own section header.
  import Icon from "./Icon.svelte";
  import {
    S,
    jumpToChannel,
    selectGuild,
    openCallStage,
    joinVoiceChannel,
    markAllRead,
    toggleMute,
    isMuted,
    channelShort,
    isDMChannel,
    guildUnread,
    startDM,
    flash,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { parseQuery, buildDests, recentFirst, rank, inMode } from "./lib/switcher.js";
  import { pushLayer } from "./lib/navstack.svelte.js";

  // Escape used to be answered here as well as in the desktop keymap's ladder;
  // now the one stack owns it.
  $effect(() => pushLayer("panel", () => (S.quickSwitcher = false)));

  let query = $state("");
  let sel = $state(0);
  let inputEl = $state(null);
  let listEl = $state(null);

  $effect(() => inputEl?.focus());

  // People to offer: everyone you know (contacts), plus the guild you are in.
  let contacts = $state([]);
  api
    .contacts()
    .then((cs) => (contacts = cs || []))
    .catch(() => {});
  const people = $derived([
    ...contacts,
    ...S.members.map((m) => ({ fingerprint: m.fingerprint, name: m.name })),
  ]);

  // Stable ids for aria-activedescendant.
  const uid = Math.random().toString(36).slice(2, 8);
  const listId = `qs-list-${uid}`;
  const optId = (i) => `qs-opt-${uid}-${i}`;

  // Split a label into plain/highlighted runs from fuzzy-match indices.
  function toParts(label, idx) {
    if (!idx?.length) return [{ t: label, hit: false }];
    const parts = [];
    let last = 0;
    for (const i of idx) {
      if (i > last) parts.push({ t: label.slice(last, i), hit: false });
      const prev = parts[parts.length - 1];
      if (prev?.hit) prev.t += label[i];
      else parts.push({ t: label[i], hit: true });
      last = i + 1;
    }
    if (last < label.length) parts.push({ t: label.slice(last), hit: false });
    return parts;
  }

  // App actions. Each runs after the palette closes.
  const actions = $derived.by(() => {
    const list = [
      { id: "newdm", label: "New message", icon: "edit", run: () => (S.modal = { kind: "newDM" }) },
      { id: "create", label: "Create guild", icon: "plus", run: () => (S.modal = { kind: "create" }) },
      { id: "join", label: "Join with an invite code", icon: "download", run: () => (S.modal = { kind: "join" }) },
      { id: "mycal", label: "Your calendar", icon: "calendar", run: () => (S.modal = { kind: "myCalendar" }) },
      { id: "saved", label: "Saved messages", icon: "pin", run: () => (S.modal = { kind: "saved" }) },
      { id: "reach", label: "Can people reach me?", icon: "bolt", sub: "connection check", run: () => (S.modal = { kind: "reach" }) },
      { id: "settings", label: "Open settings", icon: "gear", run: () => (S.modal = { kind: "settings" }) },
      { id: "appearance", label: "Open appearance", icon: "spark", run: () => (S.modal = { kind: "appearance" }) },
      { id: "status", label: "Set status", icon: "smile", run: () => (S.statusPopRequest = true) },
      { id: "read", label: "Mark all as read", icon: "check", sub: "Shift+Esc", run: () => markAllRead() },
    ];
    if (S.activeChannelId) {
      const id = S.activeChannelId;
      const muted = !!isMuted(id);
      const short = channelShort(id);
      const name = !short ? "this channel" : isDMChannel(id) ? short : `#${short}`;
      list.push({
        id: "mute",
        label: `${muted ? "Unmute" : "Mute"} ${name}`,
        icon: muted ? "bell" : "bellOff",
        run: () => toggleMute(id),
      });
    }
    // A list of key bindings for a device with no keys. Off the menu on touch.
    if (!S.isMobile)
      list.push({
        id: "shortcuts",
        label: "Keyboard shortcuts",
        icon: "chevron",
        sub: "Ctrl+/",
        run: () => (S.modal = { kind: "shortcuts" }),
      });
    return list.map((a) => ({ ...a, kind: "action", key: `act:${a.id}` }));
  });

  const grouped = $derived.by(() => {
    const { mode, q } = parseQuery(query);
    const actionOnly = mode === "actions";
    const dests = actionOnly ? [] : buildDests(S.guilds, people, S.identity?.fingerprint);
    if (!q) {
      // Empty query = recency: the conversations you visited (channels and
      // DMs), newest first, padded with the channel list for fresh installs.
      return {
        jump: recentFirst(dests, S.recentChannels, S.activeChannelId, mode),
        acts: mode === "all" ? actions.slice(0, 5) : actionOnly ? actions : [],
      };
    }
    const pool = dests.filter((d) => inMode(d, mode));
    const ranked = (list, cap) => rank(list, q, cap).map((x) => ({ ...x, parts: toParts(x.label, x.idx) }));
    return {
      jump: ranked(pool, 8),
      acts: mode === "all" || actionOnly ? ranked(actions, actionOnly ? 12 : 5) : [],
    };
  });

  const flat = $derived([...grouped.jump, ...grouped.acts]);

  $effect(() => {
    query;
    sel = 0;
  });

  // Keep the selected row visible when arrowing through a scrolled list.
  $effect(() => {
    sel;
    listEl?.querySelector(".hit.sel")?.scrollIntoView({ block: "nearest" });
  });

  // Unread state for a result row, so the list shows which hit needs you.
  // Channels read their own counter; guild/DM rows aggregate like the rail.
  function unreadFor(item) {
    if (item.kind === "channel") return S.unread[item.c.id] || null;
    if (item.kind === "guild" || item.kind === "dm") {
      const u = guildUnread(item.g);
      return u.count ? u : null;
    }
    return null;
  }

  async function go(item) {
    S.quickSwitcher = false;
    if (item.kind === "action") {
      item.run();
    } else if (item.kind === "channel") {
      if (item.c.type === "voice") {
        if (S.activeGuildId !== item.g.id) await selectGuild(item.g.id);
        if ((S.voice && S.voice.channelId === item.c.id) || S.joiningVoice === item.c.id) {
          openCallStage();
        } else {
          await joinVoiceChannel(item.c.id);
        }
      } else await jumpToChannel(item.c.id);
    } else if (item.kind === "person") {
      try {
        await startDM(item.fpr);
      } catch (err) {
        flash(err);
      }
    } else if (item.kind === "dm" && item.channelId) {
      await jumpToChannel(item.channelId);
    } else {
      await selectGuild(item.g.id);
    }
  }

  function onKeydown(e) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      sel = (sel + 1) % Math.max(flat.length, 1);
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      sel = (sel - 1 + Math.max(flat.length, 1)) % Math.max(flat.length, 1);
    } else if (e.key === "Enter" && flat[sel]) {
      e.preventDefault();
      go(flat[sel]);
    }
  }
</script>

{#snippet row(item, i)}
  <button
    class="hit"
    class:sel={i === sel}
    id={optId(i)}
    role="option"
    aria-selected={i === sel}
    tabindex="-1"
    onclick={() => go(item)}
  >
    <span class="hit-icon">
      <Icon name={item.icon} size={13} />
    </span>
    <span class="hit-label">
      {#if item.parts}
        {#each item.parts as p, j (j)}{#if p.hit}<span class="hl">{p.t}</span>{:else}{p.t}{/if}{/each}
      {:else}
        {item.label}
      {/if}
    </span>
    {#if item.sub}
      <!-- Action subs are keyboard shortcuts — render them as kbd chips. -->
      {#if item.kind === "action"}
        <kbd class="hit-kbd">{item.sub}</kbd>
      {:else}
        <span class="muted hit-sub">{item.sub}</span>
      {/if}
    {/if}
    {#if item.kind !== "action"}
      {@const u = unreadFor(item)}
      {#if u?.count}
        <span class="hit-unread" class:mention={u.mentions > 0}>{u.mentions > 0 ? u.mentions : u.count}</span>
      {/if}
    {/if}
    <span class="hit-enter" aria-hidden="true">↵</span>
  </button>
{/snippet}

<div class="overlay" role="presentation" onclick={() => (S.quickSwitcher = false)}>
  <div class="switcher" role="dialog" aria-modal="true" aria-label="Command palette" onclick={(e) => e.stopPropagation()}>
    <!-- A combobox over a listbox: the highlighted result is announced as you
         arrow (aria-activedescendant) while the caret stays in the field. It
         used to be a CSS class on a plain button, invisible to a screen
         reader. -->
    <input
      role="combobox"
      aria-expanded="true"
      aria-controls={listId}
      aria-autocomplete="list"
      aria-activedescendant={flat[sel] ? optId(sel) : undefined}
      bind:this={inputEl}
      bind:value={query}
      placeholder="Jump anywhere, or type “&gt;” for commands…"
      aria-label="Jump to a conversation, or run a command"
      onkeydown={onKeydown}
    />
    <div class="results scroll-fade" bind:this={listEl} id={listId} role="listbox" aria-label="Results">
      {#if grouped.jump.length}
        <div class="sec muted" role="presentation">Jump to</div>
        {#each grouped.jump as item, i (item.key)}
          {@render row(item, i)}
        {/each}
      {/if}
      {#if grouped.acts.length}
        <div class="sec muted" role="presentation">Actions</div>
        {#each grouped.acts as item, i (item.key)}
          {@render row(item, grouped.jump.length + i)}
        {/each}
      {/if}
      {#if !flat.length}
        <div class="muted none">
          Nothing matches “{query}”.
          <span class="none-tip">Start with # for channels, @ for people, ! for voice, * for guilds, &gt; for commands.</span>
        </div>
      {/if}
    </div>
    <span class="sr-only" role="status">{flat.length ? `${flat.length} results` : "No results"}</span>
    <div class="hint muted">↑↓ navigate · ↵ select · esc close · # @ ! * &gt; to narrow</div>
  </div>
</div>

<style>
  /* Just a dim — the frosted look comes from the palette's OWN backdrop-filter
     below (a small, bounded region). A full-screen blur here would be the exact
     GPU hazard we avoid, for no visible gain over the glass panel. */
  .overlay {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    display: flex;
    justify-content: center;
    padding-top: calc(12 * var(--vh));
    /* Above a dialog's overlay, not level with it. Ctrl+K is refused while a
       modal is open (lib/shortcuts.js), but a tie here meant that whenever the
       two did coexist the switcher lost on DOM order and took the keyboard from
       behind the scrim. */
    z-index: 110;
    animation: qs-fade var(--dur-quick) ease;
  }
  .switcher {
    width: 460px;
    max-width: calc(92 * var(--vw));
    height: fit-content;
    /* Opaque, not glass. This used to be a 12% hole over a blur(14px), which
       is the most expensive filter on the screen for the software renderer the
       desktop ships with (WEBKIT_DISABLE_DMABUF_RENDERER) — and where the blur
       does not land, the hole is just channel names printed over messages. The
       scrim behind the palette is what separates it from the app. */
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    padding: var(--sp-3);
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    box-shadow: var(--shadow-pop);
    transform-origin: top center;
    animation: qs-pop var(--dur-standard) var(--ease-out);
  }
  @keyframes qs-fade {
    from {
      opacity: 0;
    }
  }
  @keyframes qs-pop {
    from {
      opacity: 0;
      transform: translateY(-6px) scale(0.97);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .switcher {
      animation: none;
    }
  }
  .results {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: min(52 * var(--vh), 430px);
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .sec {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 4px 10px 2px;
  }
  .sec + .sec,
  .hit + .sec {
    margin-top: 6px;
  }
  .hit {
    position: relative;
    display: flex;
    align-items: center;
    gap: 9px;
    flex-shrink: 0;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 8px 10px;
    border-radius: var(--radius-sm);
    font-size: var(--fs-ui);
    transition:
      background var(--dur-quick) ease,
      transform var(--dur-quick) ease;
  }
  .hit.sel {
    background: var(--bg-3);
    transform: translateX(2px);
  }
  @media (pointer: fine) {
    .hit:hover {
      background: var(--bg-3);
      transform: translateX(2px);
    }
  }
  .hit:active {
    background: var(--bg-3);
  }
  /* The selected row carries an accent edge + tinted icon, so keyboard focus
     reads instantly even while the mouse hovers elsewhere. */
  .hit.sel {
    background: color-mix(in srgb, var(--accent) 12%, var(--bg-3));
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .hit.sel .hit-icon {
    color: var(--accent-hover);
  }
  .hit-icon {
    color: var(--text-muted);
    display: grid;
    place-items: center;
    transition: color var(--dur-quick) ease;
  }
  /* ↵ affordance surfaces only on the selected row. */
  .hit-enter {
    color: var(--accent-hover);
    font-size: 12px;
    opacity: 0;
    transform: translateX(-3px);
    transition:
      opacity var(--dur-quick) ease,
      transform var(--dur-quick) ease;
  }
  .hit.sel .hit-enter {
    opacity: 0.9;
    transform: none;
  }
  .hit-kbd {
    font-family: inherit;
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-bottom-width: 2px;
    border-radius: var(--radius-sm);
    padding: 1px 6px;
    white-space: nowrap;
  }
  /* Same pill the channel list wears, so "this one needs you" reads the same
     everywhere. Mentions swap the count for the mention tally in accent. */
  .hit-unread {
    min-width: 18px;
    padding: 0 5px;
    height: 16px;
    border-radius: var(--radius-md);
    background: var(--text-muted);
    color: var(--bg-1);
    font-size: var(--fs-tiny);
    font-weight: 700;
    display: grid;
    place-items: center;
    font-variant-numeric: tabular-nums;
  }
  .hit-unread.mention {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .hit-label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hl {
    color: var(--accent-hover);
    font-weight: 600;
  }
  .hit-sub {
    font-size: var(--fs-compact);
  }
  .none {
    padding: 10px;
    font-size: var(--fs-ui);
  }
  .none-tip {
    display: block;
    margin-top: var(--sp-1);
    font-size: var(--fs-small);
  }
  .hint {
    font-size: var(--fs-small);
    text-align: center;
    padding-top: 2px;
    border-top: 1px solid var(--border);
  }
  /* Mobile: the palette drops in full-width from the top edge; keyboard
     shortcut hints are meaningless on touch. */
  @media (pointer: coarse), (max-width: 768px) {
    .overlay {
      padding-top: 0;
      align-items: flex-start;
    }
    .switcher {
      width: 100%;
      max-width: none;
      border: none;
      border-radius: 0 0 16px 16px;
      padding-top: calc(12px + var(--safe-top));
    }
    .switcher input {
      font-size: 16px; /* stops iOS auto-zoom on focus */
    }
    .hit {
      min-height: var(--tap-min);
      font-size: var(--fs-body);
    }
    /* Both name keys nobody can press, and both steal width from .hit-label,
       which ellipsizes — so a shortcut chip was truncating channel names to
       make room for itself. */
    .hit-kbd,
    .hit-enter {
      display: none;
    }
    .results {
      /* dvh: the keyboard is up the whole time this palette is open, and vh
         does not shrink for it in an Android WebView. */
      max-height: calc(60 * var(--dvh));
    }
    .hint {
      display: none;
    }
  }
</style>
