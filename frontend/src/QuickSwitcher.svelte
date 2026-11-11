<script>
  // Ctrl+K command palette: fuzzy jump to any channel / DM / guild, plus app
  // actions. A leading ">" or "/" filters to actions only; otherwise actions
  // rank alongside destinations under their own section header.
  import Icon from "./Icon.svelte";
  import Kbd from "./Kbd.svelte";
  import Badge from "./Badge.svelte";
  import Avatar from "./Avatar.svelte";
  import GroupAvatar from "./GroupAvatar.svelte";
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
  // A typed scope prefix ("#", "@", "!", "*", ">") becomes a chip in front of
  // the field instead of staying a literal character in it; Backspace in an
  // empty field removes the chip. The search reads chip + text as before.
  let scopeChar = $state("");
  const full = $derived(scopeChar + query);
  function onInput() {
    if (!scopeChar && SCOPE_CHARS.has(query[0])) {
      scopeChar = query[0];
      query = query.slice(1).trimStart();
    }
  }
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
    ...S.members.map((m) => ({ fingerprint: m.fingerprint, name: m.name, avatar: m.avatar, color: m.color, emoji: m.emoji })),
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

  // Empty query: Unread (mentions first), Recent, and four actions. With a
  // query: ONE ranked list, no headers — the leading glyph says what each row
  // is, and unread is a boost in the ranking rather than a section of its own.
  const sections = $derived.by(() => {
    const { mode, q } = parseQuery(full);
    const actionOnly = mode === "actions";
    const dests = actionOnly ? [] : buildDests(S.guilds, people, S.identity?.fingerprint);
    if (!q) {
      if (actionOnly) return [{ title: "Actions", items: actions }];
      const recent = recentFirst(dests, S.recentChannels, S.activeChannelId, mode, 8);
      if (mode !== "all") return [{ title: "Recent", items: recent }];
      const unread = dests
        .filter((d) => d.kind === "channel" && d.channelId !== S.activeChannelId && unreadFor(d)?.count)
        .sort((x, y) => (unreadFor(y).mentions > 0) - (unreadFor(x).mentions > 0))
        .slice(0, 5);
      const seen = new Set(unread.map((d) => d.key));
      return [
        { title: "Unread", items: unread },
        { title: "Recent", items: recent.filter((d) => !seen.has(d.key)) },
        { title: "Actions", items: actions.filter((x) => ["read", "newdm", "create", "settings"].includes(x.id)).slice(0, 4) },
      ].filter((sec) => sec.items.length);
    }
    const pool = dests.filter((d) => inMode(d, mode));
    const boost = (x) => {
      const u = x.kind === "action" ? null : unreadFor(x);
      return u?.mentions > 0 ? 150 : u?.count ? 60 : 0;
    };
    const list = [...rank(pool, q, 12), ...(mode === "all" || actionOnly ? rank(actions, q, actionOnly ? 12 : 5) : [])]
      .map((x) => ({ ...x, s: x.score - boost(x) }))
      .sort((x, y) => x.s - y.s)
      .slice(0, 10)
      .map((x) => ({ ...x, parts: toParts(x.label, x.idx) }));
    return [{ title: "", items: list }];
  });
  const flat = $derived(sections.flatMap((sec) => sec.items));
  const offsets = $derived(sections.map((_, i) => sections.slice(0, i).reduce((n, sec) => n + sec.items.length, 0)));
  const scope = $derived(parseQuery(full).mode);
  const empty = $derived(!parseQuery(full).q);
  const SCOPES = [
    ["#", "Channels"],
    ["@", "People"],
    ["!", "Voice"],
    ["*", "Guilds"],
    [">", "Actions"],
    ["?", "Help"],
  ];
  const SCOPE_CHARS = new Set(["#", "@", "!", "*", ">"]);
  const SCOPE_NAME = Object.fromEntries(SCOPES);

  $effect(() => {
    full;
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
    if (!item) return null;
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
    if (e.key === "Backspace" && !query && scopeChar) {
      e.preventDefault();
      scopeChar = "";
      return;
    }
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
    <!-- The leading slot says what the row IS: a face for a person or a
         conversation with one, a group's faces for a group, a glyph for
         places and actions. A pencil on a person read as "edit". -->
    <span class="hit-icon">
      {#if item.kind === "dm" && (item.g.dmMembers ?? 2) > 2}
        <GroupAvatar faces={item.g.dmFaces || []} size={20} />
      {:else if item.kind === "dm" && !item.g.dmNotes}
        <Avatar name={item.label} image={item.g.dmPeerAvatar || item.g.dmFaces?.[0]?.avatar || item.g.icon} size={20} />
      {:else if item.kind === "person"}
        <Avatar name={item.label} image={item.avatar || ""} color={item.color || ""} emoji={item.emoji || ""} size={20} />
      {:else}
        <Icon name={item.icon} size={16} />
      {/if}
    </span>
    <span class="hit-label">
      {#if item.parts}
        {#each item.parts as p, j (j)}{#if p.hit}<span class="hl">{p.t}</span>{:else}{p.t}{/if}{/each}
      {:else}
        {item.label}
      {/if}
    </span>
    {#if item.sub && item.kind !== "action"}
      <span class="hit-sub">{item.sub}</span>
    {/if}
    <span class="hit-end">
      {#if item.kind === "action"}
        {#if item.sub}<Kbd>{item.sub}</Kbd>{/if}
      {:else}
        {@const u = unreadFor(item)}
        {#if u?.mentions > 0}
          <Badge count={u.mentions} />
        {:else if u?.count}
          <Badge dot label="Unread" />
        {/if}
      {/if}
    </span>
  </button>
{/snippet}

<div class="overlay" role="presentation" onclick={() => (S.quickSwitcher = false)}>
  <div class="switcher" role="dialog" aria-modal="true" aria-label="Command palette" onclick={(e) => e.stopPropagation()}>
    <!-- A combobox over a listbox: the highlighted result is announced as you
         arrow (aria-activedescendant) while the caret stays in the field. -->
    <div class="qs-input">
      <Icon name="search" size={20} />
      {#if scopeChar}
        <span class="qs-chip">
          {SCOPE_NAME[scopeChar]}
          <button type="button" class="qs-chip-x" aria-label="Remove the {SCOPE_NAME[scopeChar]} filter" onclick={() => ((scopeChar = ""), inputEl?.focus())}>
            <Icon name="close" size={10} />
          </button>
        </span>
      {/if}
      <input
        role="combobox"
        aria-expanded="true"
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={flat[sel] ? optId(sel) : undefined}
        bind:this={inputEl}
        bind:value={query}
        oninput={onInput}
        placeholder={scopeChar ? `Search ${SCOPE_NAME[scopeChar].toLowerCase()}` : "Jump to a conversation, person or action"}
        aria-label="Jump to a conversation, person or action"
        onkeydown={onKeydown}
      />
    </div>
    {#if empty}
      <!-- The scopes, said out loud: typing the prefix does the same. -->
      <div class="scopes" role="group" aria-label="Narrow to">
        {#each SCOPES as [p, name] (p)}
          <button
            type="button"
            class="scope"
            class:on={scopeChar === p}
            onclick={() => {
              if (p === "?") {
                S.quickSwitcher = false;
                S.modal = { kind: "shortcuts" };
                return;
              }
              scopeChar = p;
              query = "";
              inputEl?.focus();
            }}><span class="scope-p">{p}</span> {name}</button
          >
        {/each}
      </div>
    {/if}
    <div class="results" bind:this={listEl} id={listId} role="listbox" aria-label="Results">
      {#each sections as sec, si (sec.title + si)}
        {#if sec.title}<div class="sec" role="presentation">{sec.title}</div>{/if}
        {#each sec.items as item, i (item.key)}
          {@render row(item, offsets[si] + i)}
        {/each}
      {/each}
      {#if !flat.length}
        <div class="none">
          Nothing matches “{query}”.
          <span class="none-tip">Start with # for channels, @ for people, ! for voice, * for guilds, &gt; for actions.</span>
        </div>
      {/if}
    </div>
    <span class="sr-only" role="status">{flat.length ? `${flat.length} results` : "No results"}</span>
    <div class="hint">↑ ↓ to move · ↵ to open · Esc to close</div>
  </div>
</div>

<style>
  /* The palette: one lifted surface over a scrim, centred near the top. */
  .overlay {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    display: flex;
    justify-content: center;
    padding-top: 96px;
    /* Above a dialog's overlay, not level with it (lib/shortcuts.js refuses
       Ctrl+K behind a modal, but a tie lost the keyboard on DOM order). */
    z-index: 110;
    animation: qs-fade var(--dur-quick) ease;
  }
  .switcher {
    width: 640px;
    max-width: calc(100% - 32px);
    height: fit-content;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    display: flex;
    flex-direction: column;
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    animation: qs-pop var(--dur-quick) var(--ease-spring);
  }
  @keyframes qs-fade {
    from {
      opacity: 0;
    }
  }
  @keyframes qs-pop {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .overlay,
    .switcher {
      animation: none;
    }
  }
  .qs-chip {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    flex: none;
    padding: 2px 4px 2px 9px;
    border-radius: 999px;
    background: var(--bg-3);
    border: 1px solid var(--border-strong);
    color: var(--text);
    font-size: var(--fs-small);
    font-weight: 600;
  }
  .qs-chip-x {
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    padding: 0;
    border-radius: 50%;
    background: transparent;
    color: var(--text-muted);
  }
  .qs-input {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    height: 56px;
    padding: 0 var(--sp-4);
    color: var(--text-muted);
    border-bottom: 1px solid var(--hairline);
  }
  .qs-input input {
    flex: 1;
    min-width: 0;
    height: 100%;
    border: none;
    background: transparent;
    box-shadow: none;
    padding: 0;
    color: var(--text);
    font-size: var(--fs-title);
  }
  .qs-input input:focus {
    outline: none;
    box-shadow: none;
  }
  .scopes {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 10px var(--sp-4) 2px;
  }
  .scope {
    height: 26px;
    padding: 0 10px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: var(--fs-small);
    font-weight: 500;
  }
  .scope:hover,
  .scope.on {
    color: var(--text);
  }
  .scope-p {
    font-family: var(--mono-font);
    color: var(--text);
  }
  .results {
    display: flex;
    flex-direction: column;
    max-height: min(56 * var(--vh), 440px);
    overflow-y: auto;
    overscroll-behavior: contain;
    padding: 6px 8px 8px;
  }
  .sec {
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
    padding: 10px 10px 4px;
  }
  .hit {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-shrink: 0;
    min-height: 40px;
    padding: 0 10px;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text);
    text-align: start;
    font-size: var(--fs-ui);
  }
  /* One active row: pointer and keyboard move the same selection. */
  .hit.sel {
    background: var(--bg-3);
  }
  .hit-icon {
    display: grid;
    place-items: center;
    width: 24px;
    flex: none;
    color: var(--text-muted);
  }
  .hit-label {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 0 1 auto;
  }
  /* Matched characters: weight, not colour. */
  .hl {
    font-weight: 600;
    color: var(--text);
  }
  .hit-label:has(.hl) {
    color: var(--text-muted);
  }
  .hit-sub {
    min-width: 0;
    flex: 1 1 0;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hit-end {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: none;
  }
  .none {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    padding: 18px 12px;
    color: var(--text-muted);
    font-size: var(--fs-ui);
  }
  .none-tip {
    font-size: var(--fs-small);
  }
  .hint {
    height: 36px;
    display: flex;
    align-items: center;
    padding: 0 var(--sp-4);
    border-top: 1px solid var(--hairline);
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }
  @media (max-width: 768px) {
    .overlay {
      padding-top: var(--safe-top, 0px);
    }
    .switcher {
      width: 100%;
      max-width: 100%;
      border-radius: 0 0 var(--radius-lg) var(--radius-lg);
    }
    .hit {
      min-height: 48px;
    }
    .hint {
      display: none;
    }
  }
</style>
