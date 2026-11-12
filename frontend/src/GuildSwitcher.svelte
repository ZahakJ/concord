<script>
  // The guild switcher: the inventory behind the sidebar's shortcut strip.
  // Every guild, searchable, in the user's own order with folders opening in
  // place (the old vertical rail as a list — lib/rail.js's order and folder
  // logic, reused unchanged), the pinned ones first, and the two ways to get a
  // new guild in the footer.
  //
  // A popover under the place header on desktop and tablet, a sheet on a phone.
  // It is one listbox driven from the search field (aria-activedescendant), so
  // the arrow keys move, Enter opens, Space pins, Alt+↑/↓ reorders and the
  // context-menu key opens the row's menu without focus ever leaving the field.
  import Icon from "./Icon.svelte";
  import Badge from "./Badge.svelte";
  import GuildMark from "./GuildMark.svelte";
  import OwnerMark from "./OwnerMark.svelte";
  import { ownsPlace } from "./lib/ownermark.js";
  import {
    S,
    selectGuild,
    guildUnread,
    guildNotifLevel,
    guildMenuItems,
    commitRail,
    openContextMenu,
    startMeeting,
    setPref,
  } from "./lib/state.svelte.js";
  import { layer } from "./lib/navstack.svelte.js";
  import { combineGuilds, moveGuild, moveFolder, toggleFolder, DEFAULT_FOLDER_COLOR } from "./lib/rail.js";
  import { guildMenu, folderMenu, renameFolderTo, stripPins, isPinned, togglePin, pinAt } from "./lib/guildmenu.js";
  import { closeGuildSwitcher, GSW } from "./lib/guildswitch.svelte.js";
  import { tileColor } from "./lib/guildaccent.js";
  import { plural } from "./lib/plural.js";
  import { focusOnMount } from "./lib/focus.js";
  import { viewport } from "./lib/place.js";
  import { sheetdrag } from "./lib/sheet.js";
  // On a phone the switcher is a sheet, and a field that takes the caret on
  // open raises the keyboard over half of it: with no guilds yet, over the two
  // things the sheet is for (Create a guild, Join with an invite code). The
  // field is still one tap away; a desktop keeps typing-to-filter at once.
  const focusUnlessPhone = (node) => (S.isMobile ? undefined : focusOnMount(node));

  const L = layer("popover", closeGuildSwitcher);
  void L;
  let sheetEl = $state(null);
  let scrimEl = $state(null);
  const phone = $derived(S.isMobile);
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  let query = $state("");
  let active = $state(0); // index into `options`
  let listEl = $state(null);
  let renaming = $state(""); // folder id whose name is being edited

  const guilds = $derived(S.guilds.filter((g) => g.kind !== "dm"));
  const byId = $derived(new Map(guilds.map((g) => [g.id, g])));

  // Accent- and case-insensitive, the way the palette matches (C-33).
  const fold = (s) =>
    String(s || "")
      .normalize("NFD")
      .replace(/\p{M}/gu, "")
      .toLowerCase();

  function marks(g) {
    const u = guildUnread(g);
    const muted = guildNotifLevel(g.id) === "none";
    let raw = 0;
    if (muted) for (const c of g.channels) raw += S.unread[c.id]?.mentions || 0;
    return { muted, count: muted ? raw : u.attention, dot: !muted && !u.attention && u.count > 0 };
  }
  function folderMarks(ids) {
    let count = 0;
    let dot = false;
    for (const id of ids) {
      const g = byId.get(id);
      if (!g) continue;
      const m = marks(g);
      if (m.muted) continue;
      count += m.count;
      dot = dot || m.dot;
    }
    return { count, dot: !count && dot };
  }

  // The rows, in order. Sections are rows too (they are drawn in the list),
  // but only guilds and folders are options.
  const rows = $derived.by(() => {
    const out = [];
    const q = fold(query.trim());
    if (q) {
      const hits = guilds
        .map((g) => ({ g, at: fold(g.name).indexOf(q) }))
        .filter((h) => h.at !== -1)
        // Names that START with the query first; otherwise the user's order.
        .sort((a, b) => (a.at === 0 ? 0 : 1) - (b.at === 0 ? 0 : 1));
      for (const { g } of hits) out.push({ kind: "guild", key: "q" + g.id, g });
      return out;
    }
    const pins = stripPins().filter((id) => byId.has(id));
    if (pins.length) {
      out.push({ kind: "section", key: "s-pin", label: "Pinned" });
      pins.forEach((id, pi) => out.push({ kind: "guild", key: "p" + id, g: byId.get(id), pinIndex: pi }));
      out.push({ kind: "section", key: "s-all", label: "All guilds" });
    }
    S.rail.forEach((e, top) => {
      if (e.t === "g") {
        const g = byId.get(e.id);
        if (g) out.push({ kind: "guild", key: "g" + g.id, g, top });
        return;
      }
      const members = e.ids.filter((id) => byId.has(id));
      if (!members.length) return;
      out.push({ kind: "folder", key: "f" + e.id, folder: e, top, members });
      if (e.open)
        members.forEach((id, mi) => out.push({ kind: "guild", key: "fg" + id, g: byId.get(id), folder: e, mi, depth: 1 }));
    });
    return out;
  });
  const options = $derived(rows.filter((r) => r.kind !== "section"));
  const optId = (r) => "gsw-" + r.key;

  // Keep the active row on something that exists, and on the current guild
  // when the switcher opens.
  let seeded = false;
  $effect(() => {
    const n = options.length;
    if (!seeded && n) {
      seeded = true;
      const at = options.findIndex((r) => r.kind === "guild" && r.g.id === S.activeGuildId && r.pinIndex == null);
      active = at === -1 ? 0 : at;
    }
    if (active >= n) active = Math.max(0, n - 1);
  });
  // A new query starts at the top of its results (not on mount: the switcher
  // opens on the guild you are in).
  let lastQuery = "";
  $effect(() => {
    if (query === lastQuery) return;
    lastQuery = query;
    active = 0;
  });
  $effect(() => {
    const r = options[active];
    if (!r || !listEl) return;
    listEl.querySelector(`#${CSS.escape(optId(r))}`)?.scrollIntoView({ block: "nearest" });
  });

  function open(r) {
    if (!r) return;
    if (r.kind === "folder") {
      commitRail(toggleFolder(S.rail, r.folder.id));
      return;
    }
    closeGuildSwitcher();
    selectGuild(r.g.id, { browse: S.isMobile });
  }

  // A menu opened from the keyboard has no pointer: it opens at the row.
  function pointAt(r) {
    const el = listEl?.querySelector(`#${CSS.escape(optId(r))}`);
    const b = el?.getBoundingClientRect();
    return {
      clientX: b ? b.left + 24 : 0,
      clientY: b ? b.bottom : 0,
      currentTarget: el,
      preventDefault() {},
      stopPropagation() {},
    };
  }
  function rowMenu(e, r) {
    if (r.kind === "folder") folderMenu(e, r.folder, { onRename: (f) => (renaming = f.id) });
    else guildMenu(e, r.g, r.folder || null);
  }

  // Alt+↑/↓: the keyboard's drag. Pins reorder among pins; guilds and folders
  // among the top level; a guild inside a folder within its folder.
  function nudge(r, dir) {
    if (r.pinIndex != null) {
      pinAt(r.g.id, r.pinIndex + dir);
    } else if (r.kind === "folder") {
      commitRail(moveFolder(S.rail, r.folder.id, dir < 0 ? r.top - 1 : r.top + 2));
    } else if (r.folder) {
      commitRail(moveGuild(S.rail, r.g.id, { kind: "folder", folderId: r.folder.id, index: dir < 0 ? r.mi - 1 : r.mi + 2 }));
    } else if (r.top != null) {
      commitRail(moveGuild(S.rail, r.g.id, { kind: "top", index: dir < 0 ? Math.max(0, r.top - 1) : r.top + 2 }));
    } else return;
    // Follow the row that moved.
    requestAnimationFrame(() => {
      const at = options.findIndex((o) => o.key === r.key);
      if (at !== -1) active = at;
    });
  }

  function onKey(e) {
    const r = options[active];
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (e.altKey && r) return nudge(r, e.key === "ArrowUp" ? -1 : 1);
      const n = options.length;
      if (n) active = (active + (e.key === "ArrowDown" ? 1 : -1) + n) % n;
    } else if (e.key === "Home" && !query) {
      e.preventDefault();
      active = 0;
    } else if (e.key === "End" && !query) {
      e.preventDefault();
      active = Math.max(0, options.length - 1);
    } else if (e.key === "Enter") {
      e.preventDefault();
      open(r);
    } else if (e.key === " " && !query && r?.kind === "guild") {
      e.preventDefault();
      togglePin(r.g.id);
      // Pinning adds (or removes) a row above: stay on the same row.
      requestAnimationFrame(() => {
        const at = options.findIndex((o) => o.key === r.key);
        if (at !== -1) active = at;
      });
    } else if ((e.key === "ArrowRight" || e.key === "ArrowLeft") && r?.kind === "folder" && !query) {
      e.preventDefault();
      if (r.folder.open !== (e.key === "ArrowRight")) commitRail(toggleFolder(S.rail, r.folder.id));
    } else if ((e.key === "ContextMenu" || (e.shiftKey && e.key === "F10")) && r) {
      e.preventDefault();
      rowMenu(pointAt(r), r);
    }
  }

  // Escape clears the search before it closes the switcher. Capture phase:
  // the global Escape (lib/shortcuts.js) would pop the layer first.
  function onEscape(e) {
    if (e.key !== "Escape" || !query || renaming) return;
    e.preventDefault();
    e.stopPropagation();
    query = "";
  }

  function headMenu(e) {
    openContextMenu(
      e,
      [
        {
          label: "Mark all guilds read",
          icon: "check",
          onClick: () => guilds.forEach((g) => guildMenuItems(g).find((it) => it.label === "Mark as read")?.onClick()),
        },
        { label: "Your calendar", icon: "calendar", onClick: () => (S.modal = { kind: "myCalendar" }) },
        { label: "Instant meeting", sub: "Lasts 1 hour to 30 days", icon: "bolt", onClick: startMeeting },
        !phone && { sep: true },
        !phone && {
          label: S.prefs.navModel === "rail" ? "Show guilds as a strip" : "Show guilds as a rail",
          icon: "grid4",
          onClick: () => setPref("navModel", S.prefs.navModel === "rail" ? "sidebar" : "rail"),
        },
        !phone && {
          label: "Hide sidebar",
          icon: "menu",
          onClick: () => {
            closeGuildSwitcher();
            setPref("sidebarHidden", true);
          },
        },
      ].filter(Boolean),
      { title: "Guilds" },
    );
  }

  // ---- drag to reorder (pointer only; the keyboard has Alt+↑/↓) ----
  let drag = $state(null); // { kind: "guild"|"folder", id }
  let hint = $state(null); // { key, z: "before"|"center"|"after" }
  function zone(e) {
    const b = e.currentTarget.getBoundingClientRect();
    const y = (e.clientY - b.top) / b.height;
    return y < 0.33 ? "before" : y > 0.67 ? "after" : "center";
  }
  function over(e, r) {
    if (!drag || r.pinIndex != null) return;
    e.preventDefault();
    let z = zone(e);
    // Only a guild can make or join a folder; a folder moves between rows.
    if (z === "center" && (drag.kind !== "guild" || r.folder || (r.kind === "guild" && r.g.id === drag.id))) z = "after";
    hint = { key: r.key, z };
  }
  function drop(e, r) {
    if (!drag || !hint) return;
    e.preventDefault();
    const { z } = hint;
    if (drag.kind === "folder") {
      const top = r.top ?? S.rail.findIndex((x) => x.t === "f" && x.id === r.folder?.id);
      if (top >= 0) commitRail(moveFolder(S.rail, drag.id, z === "after" ? top + 1 : top));
    } else if (z === "center" && r.kind === "folder") {
      commitRail(moveGuild(S.rail, drag.id, { kind: "folder", folderId: r.folder.id }));
    } else if (z === "center" && r.kind === "guild") {
      commitRail(combineGuilds(S.rail, drag.id, r.g.id, DEFAULT_FOLDER_COLOR));
    } else if (r.folder && r.kind === "guild") {
      commitRail(moveGuild(S.rail, drag.id, { kind: "folder", folderId: r.folder.id, index: z === "after" ? r.mi + 1 : r.mi }));
    } else if (r.top != null) {
      commitRail(moveGuild(S.rail, drag.id, { kind: "top", index: z === "after" ? r.top + 1 : r.top }));
    }
    drag = null;
    hint = null;
  }

  // ---- where it sits ----
  const pos = $derived.by(() => {
    if (phone) return "";
    const r = GSW.rect;
    const vp = viewport();
    const w = Math.min(360, vp.w - 16);
    const left = Math.max(8, Math.min(vp.w - w - 8, (r?.x ?? 0) + 8));
    const top = Math.max(8, (r ? r.y + r.h : 52) - 4);
    return `left:${left}px;top:${top}px;width:${w}px;max-height:min(640px, calc(var(--vh, 1vh) * 80), ${vp.h - top - 12}px)`;
  });
</script>

<svelte:window
  onkeydowncapture={onEscape}
  onpointerdown={(e) => {
    if (!e.target.closest?.(".gsw, .cm, .bs-sheet, [data-place-head], .strip .more")) closeGuildSwitcher();
  }}
  onresize={() => !phone && closeGuildSwitcher()}
/>

{#if phone}
  <button class="gsw-scrim" aria-label="Close guild switcher" onclick={closeGuildSwitcher} bind:this={scrimEl}></button>
{/if}
<div class="gsw" class:sheet={phone} style={pos} role="dialog" aria-label="Guilds" bind:this={sheetEl}>
  {#if phone}
    <!-- A sheet has a grip, and pulling it down dismisses it, like every
         other sheet on the phone. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="gsw-grab"
      use:sheetdrag={{ sheet: () => sheetEl, scrim: () => scrimEl, scroller: () => listEl, onDismiss: closeGuildSwitcher }}
    >
      <span class="gsw-handle"></span>
    </div>
  {/if}
  <div class="gsw-head">
    <label class="gsw-find">
      <Icon name="search" size={14} />
      <input
        type="text"
        role="combobox"
        aria-expanded="true"
        aria-controls="gsw-list"
        aria-autocomplete="list"
        aria-activedescendant={options[active] ? optId(options[active]) : undefined}
        aria-label="Find a guild"
        placeholder="Find a guild"
        autocomplete="off"
        spellcheck="false"
        bind:value={query}
        onkeydown={onKey}
        use:focusUnlessPhone
      />
      <span class="gsw-count" aria-hidden="true">{guilds.length}</span>
    </label>
    <button class="iconbtn" aria-label="More guild actions" aria-haspopup="menu" onclick={headMenu}>
      <Icon name="dots" size={16} />
    </button>
  </div>

  <div class="gsw-list" id="gsw-list" role="listbox" aria-label="Guilds" bind:this={listEl}>
    {#each rows as r (r.key)}
      {#if r.kind === "section"}
        <div class="gsw-sec" role="presentation">{r.label}</div>
      {:else if r.kind === "folder"}
        {@const fm = folderMarks(r.members)}
        {@const i = options.indexOf(r)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <div
          class="gsw-row folder"
          class:act={i === active}
          class:hint-before={hint?.key === r.key && hint.z === "before"}
          class:hint-after={hint?.key === r.key && hint.z === "after"}
          class:hint-in={hint?.key === r.key && hint.z === "center"}
          id={optId(r)}
          role="option"
          aria-selected={i === active}
          tabindex="-1"
          draggable={!coarse && !query}
          ondragstart={(e) => {
            drag = { kind: "folder", id: r.folder.id };
            e.dataTransfer.effectAllowed = "move";
            e.dataTransfer.setData("text/plain", r.folder.id);
          }}
          ondragend={() => ((drag = null), (hint = null))}
          ondragover={(e) => over(e, r)}
          ondrop={(e) => drop(e, r)}
          onpointermove={() => (active = i)}
          onclick={() => open(r)}
          oncontextmenu={(e) => rowMenu(e, r)}
        >
          <span class="chev" class:open={r.folder.open}><Icon name="chevron" size={12} /></span>
          <span class="fglyph" style={r.folder.color && r.folder.color !== DEFAULT_FOLDER_COLOR ? `--fc:${r.folder.color}` : undefined}><Icon name="folder" size={16} /></span>
          {#if renaming === r.folder.id}
            <!-- svelte-ignore a11y_autofocus -->
            <input
              class="gsw-rename"
              value={r.folder.name}
              aria-label="Folder name"
              autofocus
              onclick={(e) => e.stopPropagation()}
              onkeydown={(e) => {
                e.stopPropagation();
                if (e.key === "Enter" || e.key === "Escape") {
                  if (e.key === "Enter") renameFolderTo(r.folder, e.currentTarget.value);
                  renaming = "";
                }
              }}
              onblur={(e) => {
                renameFolderTo(r.folder, e.currentTarget.value);
                renaming = "";
              }}
            />
          {:else}
            <span class="name">{r.folder.name || "Folder"}<span class="vh">, {r.folder.open ? "open" : "closed"} folder</span></span>
            <span class="sub">{r.members.length}</span>
          {/if}
          <span class="trail">
            {#if fm.count}<Badge count={fm.count} label={plural(fm.count, "mention")} />
            {:else if fm.dot}<Badge dot label="Unread" />{/if}
          </span>
        </div>
      {:else}
        {@const g = r.g}
        {@const m = marks(g)}
        {@const i = options.indexOf(r)}
        {@const pinned = isPinned(g.id)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <div
          class="gsw-row"
          class:act={i === active}
          class:current={g.id === S.activeGuildId}
          class:deep={r.depth === 1}
          class:hint-before={hint?.key === r.key && hint.z === "before"}
          class:hint-after={hint?.key === r.key && hint.z === "after"}
          class:hint-in={hint?.key === r.key && hint.z === "center"}
          id={optId(r)}
          role="option"
          aria-selected={i === active}
          aria-current={g.id === S.activeGuildId ? "true" : undefined}
          tabindex="-1"
          draggable={!coarse && !query && r.pinIndex == null}
          ondragstart={(e) => {
            drag = { kind: "guild", id: g.id };
            e.dataTransfer.effectAllowed = "move";
            e.dataTransfer.setData("text/plain", g.id);
          }}
          ondragend={() => ((drag = null), (hint = null))}
          ondragover={(e) => over(e, r)}
          ondrop={(e) => drop(e, r)}
          onpointermove={() => (active = i)}
          onclick={() => open(r)}
          oncontextmenu={(e) => rowMenu(e, r)}
        >
          <span class="gicon" class:muted={m.muted} style={g.icon ? undefined : `--guild-accent:${tileColor(g)}`}>
            {#if g.icon}<img src={g.icon} alt="" />{:else}<GuildMark {g} />{/if}
          </span>
          <span class="name" class:dim={m.muted} class:owned={ownsPlace(g)}>{g.name}</span>
          {#if ownsPlace(g)}
            <OwnerMark size={12} label="You own this guild" />
          {/if}
          <span class="trail">
            {#if m.count}<Badge count={m.count} muted={m.muted} label={plural(m.count, "mention")} />
            {:else if m.dot}<Badge dot label="Unread" />{/if}
            {#if m.muted}<span class="mut" role="img" aria-label="Muted"><Icon name="bellOff" size={14} /></span>{/if}
            <button
              class="pin"
              class:on={pinned}
              tabindex="-1"
              aria-label={pinned ? `Unpin ${g.name} from shortcuts` : `Pin ${g.name} to shortcuts`}
              aria-pressed={pinned}
              onclick={(e) => {
                e.stopPropagation();
                togglePin(g.id);
              }}
            >
              <Icon name={pinned ? "pinFilled" : "pin"} size={14} />
            </button>
          </span>
        </div>
      {/if}
    {:else}
      <p class="gsw-empty">{query ? `No guild called “${query}”` : "No guilds yet"}</p>
    {/each}
  </div>

  <div class="gsw-foot">
    <button
      class="gsw-act"
      onclick={() => {
        closeGuildSwitcher();
        S.modal = { kind: "create" };
      }}
    >
      <Icon name="plus" size={16} /> Create a guild
    </button>
    <button
      class="gsw-act"
      onclick={() => {
        closeGuildSwitcher();
        S.modal = { kind: "join", code: "" };
      }}
    >
      <Icon name="link" size={16} /> Join with an invite code
    </button>
  </div>
</div>

<style>
  .gsw {
    position: fixed;
    z-index: var(--z-layer);
    display: flex;
    flex-direction: column;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    animation: gsw-in var(--dur-quick) var(--ease-spring) both;
  }
  @keyframes gsw-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
  }
  .gsw-grab {
    padding: 8px 0 4px;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
  }
  .gsw-handle {
    display: block;
    width: 40px;
    height: 5px;
    margin: 0 auto;
    border-radius: 999px;
    background: var(--border);
  }
  .gsw.sheet {
    left: 0;
    right: 0;
    bottom: 0;
    top: calc(var(--safe-top) + 40px);
    border-radius: var(--radius-sheet) var(--radius-sheet) 0 0;
    border-bottom: 0;
    padding-bottom: var(--safe-bottom);
    animation-name: gsw-rise;
  }
  @keyframes gsw-rise {
    from {
      transform: translateY(24px);
      opacity: 0;
    }
  }
  .gsw-scrim {
    position: fixed;
    inset: 0;
    z-index: var(--z-layer);
    background: var(--scrim-sheet);
    border: 0;
    padding: 0;
  }
  .gsw-head {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 10px 10px 6px;
    flex: none;
  }
  .gsw-find {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    height: 34px;
    padding: 0 var(--sp-2) 0 10px;
    border-radius: var(--radius-md);
    background: var(--bg-input);
    box-shadow: inset 0 0 0 1px var(--border-strong);
    color: var(--text-muted);
  }
  .gsw-find:focus-within {
    box-shadow: inset 0 0 0 1px var(--accent-hover);
  }
  .gsw-find input {
    flex: 1;
    min-width: 0;
    height: 100%;
    border: 0;
    padding: 0;
    background: transparent;
    box-shadow: none;
    color: var(--text);
    font-size: var(--fs-ui);
    outline: none;
  }
  .gsw-count {
    flex: none;
    padding: 0 6px;
    border-radius: var(--radius-sm);
    box-shadow: inset 0 0 0 1px var(--border);
    color: var(--text-faint);
    font-size: var(--fs-tiny);
    line-height: 18px;
    font-variant-numeric: tabular-nums;
  }
  .gsw-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 2px 6px 6px;
    overscroll-behavior: contain;
  }
  .gsw-sec {
    padding: 10px var(--sp-2) var(--sp-1);
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .gsw-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 40px;
    padding: 0 6px 0 var(--sp-2);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--fs-ui);
    cursor: pointer;
    outline: none;
  }
  .gsw-row.deep {
    padding-left: 20px;
  }
  .gsw-row.act {
    background: var(--bg-3);
  }
  .gsw-row.current {
    background: var(--bg-3);
    box-shadow: inset 2px 0 0 var(--text);
  }
  .gicon {
    flex: none;
    width: 28px;
    height: 28px;
    border-radius: 7px;
    overflow: hidden;
    display: grid;
    place-items: center;
    background: color-mix(in srgb, var(--guild-accent, var(--text-muted)) 18%, var(--bg-3));
    color: color-mix(in srgb, var(--guild-accent, var(--text-muted)) 70%, var(--text));
    font-size: var(--fs-compact);
    font-weight: 600;
  }
  .gicon img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .gicon.muted {
    opacity: 0.55;
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    unicode-bidi: plaintext;
  }
  /* A guild you own: the name takes only its own width so the crown sits
     right after it, as it does beside a name everywhere else, well clear of
     the unread marks in the trail. */
  .name.owned {
    flex: 0 1 auto;
  }
  .name.owned + :global(.owner-mark) {
    margin-inline-end: auto;
  }
  .name.dim {
    color: var(--text-muted);
  }
  .vh {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .sub {
    flex: none;
    color: var(--text-faint);
    font-size: var(--fs-tiny);
    font-variant-numeric: tabular-nums;
  }
  .trail {
    flex: none;
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 40px;
    justify-content: flex-end;
  }
  .mut {
    display: grid;
    color: var(--text-faint);
  }
  .pin {
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    padding: 0;
    border-radius: var(--radius-sm);
    color: var(--text-faint);
    visibility: hidden;
  }
  .gsw-row.act .pin,
  .pin.on {
    visibility: visible;
  }
  .pin.on {
    color: var(--text-muted);
  }
  @media (pointer: fine) {
    .pin:hover {
      color: var(--text);
      background: var(--bg-2);
    }
  }
  @media (pointer: coarse) {
    .pin {
      visibility: visible;
      min-width: var(--tap-min);
      min-height: var(--tap-min);
    }
  }
  .chev {
    display: grid;
    width: 16px;
    color: var(--text-faint);
    transition: rotate var(--dur-quick) var(--ease-out);
  }
  .chev.open {
    rotate: 90deg;
  }
  .fglyph {
    display: grid;
    color: var(--fc, var(--text-muted));
  }
  .gsw-rename {
    flex: 1;
    min-width: 0;
    height: 28px;
    font-size: var(--fs-ui);
  }
  .hint-before::before,
  .hint-after::after {
    content: "";
    position: absolute;
    left: var(--sp-2);
    right: var(--sp-2);
    height: 2px;
    border-radius: 2px;
    background: var(--accent-hover);
  }
  .hint-before::before {
    top: -1px;
  }
  .hint-after::after {
    bottom: -1px;
  }
  .hint-in {
    box-shadow: inset 0 0 0 2px var(--accent-hover);
  }
  .gsw-empty {
    margin: 0;
    padding: var(--sp-4) var(--sp-2);
    color: var(--text-muted);
    font-size: var(--fs-ui);
  }
  .gsw-foot {
    flex: none;
    border-top: 1px solid var(--hairline);
    padding: var(--sp-1) 6px 6px;
  }
  .gsw-act {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 40px;
    padding: 0 var(--sp-2);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: var(--fs-ui);
    text-align: start;
  }
  @media (pointer: fine) {
    .gsw-act:hover {
      background: var(--bg-3);
      color: var(--text);
    }
  }
  .gsw-act:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -2px;
  }
  @media (max-width: 768px) {
    .gsw-row,
    .gsw-act {
      min-height: 48px;
    }
    .gsw-find {
      height: 40px;
    }
    .gsw-find input {
      font-size: 16px;
    }
  }
  :global(:root[data-motion="reduced"]) .gsw,
  :global(:root[data-motion="reduced"]) .chev {
    animation: none;
    transition: none;
  }
</style>
