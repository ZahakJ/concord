<script>
  // The guilds, in one of two arrangements (the device-local `navModel` pref,
  // decided in App.svelte):
  //
  //   layout="strip" — the sidebar's SHORTCUT row (the default): Messages, as
  //     many guild tiles as fit the sidebar's width, and an overflow tile that
  //     opens the guild switcher. Which guilds earn a tile is the stated rule in
  //     lib/rail.js (stripTiles): pinned, then mentions, then unread, then most
  //     recently opened — laid out in the one order the switcher uses too
  //     (pins, then the rail order), so a tile never moves because it was read.
  //   layout="rail"  — the classic slim column beside the channel list, every
  //     guild in the user's order with folders, kept whole as the alternative
  //     arrangement.
  //
  // Both read the same marks: a number is something aimed at you, in your
  // accent (Badge); plain unread is a quiet dot; never red. The rail order and
  // folders are a device-local preference (lib/rail.js), not guild state.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import GroupAvatar from "./GroupAvatar.svelte";
  import Badge from "./Badge.svelte";
  import GuildMark from "./GuildMark.svelte";
  import {
    S,
    selectGuild,
    openDMs,
    guildUnread,
    guildNotifLevel,
    openContextMenu,
    startMeeting,
    commitRail,
    openInbox,
    dmList,
  } from "./lib/state.svelte.js";
  import { plural } from "./lib/plural.js";
  import {
    reconcile,
    combineGuilds,
    moveGuild,
    moveFolder,
    toggleFolder,
    DEFAULT_FOLDER_COLOR,
    guildInitials,
    guildIdsInLayout,
    stripFit,
    stripTiles,
  } from "./lib/rail.js";
  import { guildMenu, folderMenu, renameFolderTo, stripPins, pinAt, noteOpened, openedTimes } from "./lib/guildmenu.js";
  import { openGuildSwitcher, GSW } from "./lib/guildswitch.svelte.js";
  import { tileColor } from "./lib/guildaccent.js";
  import { playFlyby } from "./lib/sounds.js";
  import { longpress } from "./lib/touch.js";
  import { tooltip } from "./lib/tooltip.js";
  import FxLayer from "./FxLayer.svelte";
  import { untrack } from "svelte";
  import { RADAR, guildLiveSet } from "./lib/radar.svelte.js";
  import { failedInGuild } from "./lib/outbox.svelte.js";

  // width: the sidebar's own width, which decides how many tiles fit.
  let { layout = "rail", width = 280 } = $props();
  const strip = $derived(layout === "strip");
  const phone = $derived(S.isMobile);

  // Seasonal touches over the classic rail. Opt-in now (Appearance, A-17): a
  // drift of petals across a navigation column is a celebration, and
  // celebrations are something you ask for. Local clock only, no network.
  function seasonFx() {
    const m = new Date().getMonth();
    if (m === 11 || m === 0)
      return { kind: "fall", n: 8, glyphs: ["❄"], colors: ["#e8f1ff", "#ffffff"], size: [3, 5], dur: [8, 14], opacity: [0.3, 0.6], drift: 6 };
    if (m === 2 || m === 3)
      return { kind: "fall", tumble: true, n: 6, glyphs: ["🌸"], colors: ["#f9a8d4"], size: [3, 5], dur: [9, 15], opacity: [0.3, 0.55], drift: 8 };
    if (m === 9 || m === 10)
      return { kind: "fall", tumble: true, n: 6, glyphs: ["🍂"], colors: ["#d97706"], size: [3, 5], dur: [9, 15], opacity: [0.3, 0.55], drift: 8 };
    return null;
  }
  const season = seasonFx();
  const seasonOn = $derived(!strip && !!season && S.prefs.seasonal === true);

  const railTip = { side: "right", delay: 80 };
  const stripTip = { side: "bottom", delay: 80 };
  const tip = $derived(strip ? stripTip : railTip);

  // Touch: long-press opens the menus. iOS never synthesizes `contextmenu` for
  // a plain element, and Android's synthesized one would double-fire alongside
  // the longpress, so on a coarse pointer the long-press is the only path.
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  function addMenu(e) {
    openContextMenu(
      e,
      [
        { label: "Create a guild", icon: "spark", onClick: () => (S.modal = { kind: "create" }) },
        { label: "Join with an invite code", icon: "download", onClick: () => (S.modal = { kind: "join", code: "" }) },
        { label: "Instant meeting", sub: "Lasts 1 hour to 30 days", icon: "bolt", onClick: startMeeting },
      ],
      { title: "Add a guild" },
    );
  }

  // Hammering the jet flies a Concorde past you: eight clicks with no more than
  // FLYBY_GAP between any two. A pause resets the run.
  const FLYBY_CLICKS = 8;
  const FLYBY_GAP = 400;
  let clicks = 0;
  let lastClick = 0;
  let rolling = $state(false);
  function messagesClick() {
    // Already in the DM area: openDMs() would re-select the FIRST dm, which is
    // rarely the one you are reading. On a phone a tap browses the list rather
    // than opening the last conversation in it.
    if (!inDMs) openDMs({ browse: S.isMobile });
    if (strip) return;
    const now = performance.now();
    clicks = now - lastClick < FLYBY_GAP ? clicks + 1 : 1;
    lastClick = now;
    if (clicks >= FLYBY_CLICKS) {
      clicks = 0;
      playFlyby();
    }
    rolling = false;
    requestAnimationFrame(() => (rolling = true));
  }

  const liveGuilds = $derived(guildLiveSet());
  const unseenTotal = $derived(Object.values(RADAR.unseen).reduce((a, b) => a + b, 0));

  const g = $derived(S.guilds.find((x) => x.id === S.activeGuildId) || null);
  const inDMs = $derived(g?.kind === "dm");
  const allGuilds = $derived(S.guilds.filter((x) => x.kind !== "dm"));
  const guildById = $derived(new Map(allGuilds.map((s) => [s.id, s])));
  // The classic rail's floating DM faces: unread conversations and the open one.
  const dms = $derived(
    S.guilds.filter((x) => x.kind === "dm" && !x.dmNotes && (guildUnread(x).count > 0 || x.id === S.activeGuildId)),
  );

  // Keep the persisted layout in step with the live guild set.
  $effect(() => {
    const ids = allGuilds.map((s) => s.id);
    const next = reconcile(S.rail, ids);
    if (JSON.stringify(next) !== JSON.stringify(S.rail)) commitRail(next);
  });

  const view = $derived(
    S.rail
      .map((e) => {
        if (e.t === "g") {
          const gg = guildById.get(e.id);
          return gg ? { t: "g", id: e.id, g: gg } : null;
        }
        const guilds = e.ids.map((id) => guildById.get(id)).filter(Boolean);
        return guilds.length ? { t: "f", folder: e, guilds } : null;
      })
      .filter(Boolean),
  );

  const initials = guildInitials;
  // A tile without an icon wears its guild's colour — decoration, owned by the
  // guild, never the user's accent (lib/guildaccent.js).
  const faceStyle = (gg) => `--guild-accent:${tileColor(gg)}`;

  // Muted guild: everything off. guildUnread skips muted channels, so the raw
  // mentions are counted here for the one mark a muted guild still shows: an
  // outlined count when somebody @-ed you anyway.
  const guildMuted = (gg) => guildNotifLevel(gg.id) === "none";
  function rawMentions(gg) {
    let n = 0;
    for (const c of gg.channels) n += S.unread[c.id]?.mentions || 0;
    return n;
  }
  function marks(gg) {
    const u = guildUnread(gg);
    const muted = guildMuted(gg);
    return {
      muted,
      count: muted ? rawMentions(gg) : u.attention,
      dot: !muted && u.attention === 0 && u.count > 0,
    };
  }
  function tileLabel(gg) {
    const m = marks(gg);
    const parts = [gg.name];
    if (m.count) parts.push(plural(m.count, "mention"));
    else if (m.dot) parts.push("unread");
    if (m.muted) parts.push("muted");
    return parts.join(" · ");
  }

  function folderUnread(guilds) {
    let count = 0,
      mentions = 0,
      attention = 0;
    for (const gg of guilds) {
      const u = guildUnread(gg);
      count += u.count;
      mentions += u.mentions;
      attention += u.attention;
    }
    return { count, mentions, attention };
  }

  // ————————————————— the strip —————————————————
  // Recency: every guild you open is noted on this device, for the strip's
  // "most recently opened" class. A counter stands in for reactivity: the
  // times live in localStorage, read through lib/guildmenu.js.
  let openedRev = $state(0);
  $effect(() => {
    const id = S.activeGuildId;
    const gg = id && guildById.get(id);
    if (!gg) return;
    noteOpened(id);
    untrack(() => openedRev++);
  });

  // The strip does not reorder while you are reaching into it: the pointer is
  // over it, or the switcher (the strip's inventory) is open. What changed
  // meanwhile applies when the pointer leaves.
  let hovering = $state(false);
  let lastShown = [];
  let tiles = $state({ shown: [], hidden: [] });
  const DMS_N = 3;
  $effect(() => {
    if (!strip) return;
    void openedRev;
    const ids = guildIdsInLayout(S.rail).filter((id) => guildById.has(id));
    const mentions = new Set();
    const unread = new Set();
    for (const id of ids) {
      const m = marks(guildById.get(id));
      if (m.count) mentions.add(id);
      else if (m.dot) unread.add(id);
    }
    const ctx = { pins: stripPins(), mentions, unread, opened: openedTimes() };
    const cur = guildById.has(S.activeGuildId) ? S.activeGuildId : "";
    // A phone's strip scrolls sideways, so it holds more than fits: the rule
    // still orders it, and the overflow tile is still last.
    const n = phone ? Math.max(DMS_N * 4, 12) : stripFit(width);
    if (hovering || GSW.open) return;
    const next = stripTiles(ids, ctx, lastShown, cur, n);
    lastShown = next.shown;
    tiles = next;
  });
  const shownGuilds = $derived(tiles.shown.map((id) => guildById.get(id)).filter(Boolean));
  // The overflow tile carries the hidden guilds' combined news: their mentions
  // as one number, else a dot if any is unread. Never how MANY are hidden.
  const overflow = $derived.by(() => {
    let count = 0;
    let dot = false;
    const shown = new Set(shownGuilds.map((x) => x.id));
    for (const gg of allGuilds) {
      if (shown.has(gg.id)) continue;
      const m = marks(gg);
      if (m.muted) continue;
      count += m.count;
      dot = dot || m.dot;
    }
    return { count, dot };
  });
  // Messages: every message in a DM is aimed at you, so its number is the
  // DMs' attention; message requests are a dot (a stranger who has not been
  // accepted must not be able to make the app look like a friend is waiting).
  const dmMarks = $derived.by(() => {
    let count = 0;
    for (const d of dmList()) if (!d.dmNotes) count += guildUnread(d).attention;
    return { count, dot: !count && S.requests.length > 0 };
  });

  function openSwitcher() {
    openGuildSwitcher(document.querySelector("[data-place-head]"));
  }

  // Drag on the strip: a short fixed row, so only pins reorder here — drop a
  // pinned tile among the pins to move it, drop an unpinned one to pin it at
  // that spot. Folders and the full order are the switcher's.
  let sdrag = $state("");
  let sdrop = $state(null); // { id, after }
  function stripOver(e, gg) {
    if (!sdrag) return;
    e.preventDefault();
    const r = e.currentTarget.getBoundingClientRect();
    sdrop = { id: gg.id, after: e.clientX > r.left + r.width / 2 };
  }
  function stripDrop(e) {
    if (!sdrag || !sdrop) return;
    e.preventDefault();
    const pins = stripPins();
    let at = pins.indexOf(sdrop.id);
    if (at === -1) at = pins.length;
    else if (sdrop.after) at += 1;
    const from = pins.indexOf(sdrag);
    if (from !== -1 && from < at) at -= 1;
    pinAt(sdrag, at);
    sdrag = "";
    sdrop = null;
  }

  // Folder names on the classic rail: renamed in place, from the field in the
  // open folder or from the folder's menu.
  function renameFromMenu(folder) {
    if (!folder.open) commitRail(toggleFolder(S.rail, folder.id, true));
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        const el = document.querySelector(`[data-folder="${folder.id}"] .folder-name`);
        el?.focus();
        el?.select?.();
      }),
    );
  }

  // ————————————————— drag & drop —————————————————
  // drag: what's being dragged. dropHint: the current visual target.
  //   dropHint kinds:
  //     { k:'bar', index }              insertion bar before top-level index
  //     { k:'combine', id }             ring on a top-level guild → make folder
  //     { k:'folder', id }              ring on a folder tile → add to it
  //     { k:'fbar', fid, index }        insertion bar inside an open folder
  let drag = $state(null);
  let dropHint = $state(null);

  function startDrag(e, payload) {
    drag = payload;
    e.dataTransfer.effectAllowed = "move";
    try {
      e.dataTransfer.setData("text/plain", payload.id);
    } catch {
      /* some browsers are picky pre-drag */
    }
  }
  function endDrag() {
    drag = null;
    dropHint = null;
  }

  // Which third of an element the cursor is in.
  function zone(e, el) {
    const r = el.getBoundingClientRect();
    const y = (e.clientY - r.top) / r.height;
    if (y < 0.33) return "before";
    if (y > 0.67) return "after";
    return "center";
  }

  // Top-level guild pill as a drop target.
  function overGuild(e, entry, index) {
    if (!drag) return;
    e.preventDefault();
    const z = zone(e, e.currentTarget);
    if (drag.kind === "guild" && drag.id !== entry.id && z === "center") {
      dropHint = { k: "combine", id: entry.id };
    } else {
      dropHint = { k: "bar", index: z === "after" ? index + 1 : index };
    }
  }
  function dropOnGuild(e, entry, index) {
    if (!drag) return;
    e.preventDefault();
    if (dropHint?.k === "combine" && drag.kind === "guild") {
      commitRail(combineGuilds(S.rail, drag.id, entry.id, DEFAULT_FOLDER_COLOR));
    } else {
      applyReorder(dropHint ?? { k: "bar", index });
    }
    endDrag();
  }

  // Top-level folder tile as a drop target.
  function overFolder(e, folder) {
    if (!drag) return;
    e.preventDefault();
    const z = zone(e, e.currentTarget);
    if (drag.kind === "guild" && z === "center") dropHint = { k: "folder", id: folder.id };
    else {
      const index = topIndexOfFolder(folder.id);
      dropHint = { k: "bar", index: z === "after" ? index + 1 : index };
    }
  }
  function dropOnFolder(e, folder) {
    if (!drag) return;
    e.preventDefault();
    if (dropHint?.k === "folder" && drag.kind === "guild") {
      commitRail(moveGuild(S.rail, drag.id, { kind: "folder", folderId: folder.id }));
    } else {
      applyReorder(dropHint ?? { k: "bar", index: topIndexOfFolder(folder.id) });
    }
    endDrag();
  }

  // A guild inside an open folder as a reorder target.
  function overInFolder(e, folder, memberIndex) {
    if (!drag || drag.kind !== "guild") return;
    e.preventDefault();
    const z = zone(e, e.currentTarget);
    dropHint = { k: "fbar", fid: folder.id, index: z === "after" ? memberIndex + 1 : memberIndex };
  }
  function dropInFolder(e, folder) {
    if (!drag || drag.kind !== "guild") return;
    e.preventDefault();
    if (dropHint?.k === "fbar" && dropHint.fid === folder.id) {
      commitRail(moveGuild(S.rail, drag.id, { kind: "folder", folderId: folder.id, index: dropHint.index }));
    }
    endDrag();
  }

  // The rail itself catches drops in the gaps (reorder to start/end).
  function overRail(e) {
    if (!drag) return;
    e.preventDefault();
    if (!dropHint) dropHint = { k: "bar", index: S.rail.length };
  }
  function dropOnRail(e) {
    if (!drag) return;
    e.preventDefault();
    applyReorder(dropHint ?? { k: "bar", index: S.rail.length });
    endDrag();
  }

  function topIndexOfFolder(fid) {
    return S.rail.findIndex((e) => e.t === "f" && e.id === fid);
  }
  function applyReorder(hint) {
    if (hint.k !== "bar") return;
    if (drag.kind === "guild") commitRail(moveGuild(S.rail, drag.id, { kind: "top", index: hint.index }));
    else if (drag.kind === "folder") commitRail(moveFolder(S.rail, drag.id, hint.index));
  }

</script>

{#if strip}
  <!-- The strip. A nav of its own (inside the sidebar's) so "Guilds" stays a
       landmark whichever arrangement is chosen. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <nav
    class="strip"
    class:phone
    aria-label="Guilds"
    onpointerenter={(e) => e.pointerType === "mouse" && (hovering = true)}
    onpointerleave={() => (hovering = false)}
  >
    <span class="tile-wrap">
      <button
        class="tile msg"
        class:current={inDMs}
        use:tooltip={tip}
        aria-label={dmMarks.count ? `Messages · ${plural(dmMarks.count, "unread message")}` : "Messages"}
        aria-current={inDMs ? "page" : undefined}
        onclick={messagesClick}
      >
        <Icon name="bubble" size={phone ? 20 : 16} />
      </button>
      {#if dmMarks.count}<span class="t-badge"><Badge count={dmMarks.count} max={9} ring="var(--bg-1)" /></span>
      {:else if dmMarks.dot}<span class="t-dot"><Badge dot /></span>{/if}
    </span>
    {#each shownGuilds as gg (gg.id)}
      {@const m = marks(gg)}
      <span
        class="tile-wrap"
        class:drop-before={sdrop?.id === gg.id && !sdrop.after}
        class:drop-after={sdrop?.id === gg.id && sdrop.after}
      >
        <button
          class="tile"
          class:current={gg.id === S.activeGuildId}
          class:muted={m.muted}
          class:gone={!!gg.evicted}
          class:hasicon={!!gg.icon}
          style={gg.icon ? undefined : faceStyle(gg)}
          use:tooltip={{ ...tip, text: tileLabel(gg) }}
          aria-label={gg.name}
          aria-describedby={tileLabel(gg) !== gg.name ? `strip-d-${gg.id}` : undefined}
          aria-current={gg.id === S.activeGuildId ? "page" : undefined}
          draggable={!coarse}
          ondragstart={(e) => {
            sdrag = gg.id;
            e.dataTransfer.effectAllowed = "move";
            e.dataTransfer.setData("text/plain", gg.id);
          }}
          ondragend={() => ((sdrag = ""), (sdrop = null))}
          ondragover={(e) => stripOver(e, gg)}
          ondrop={stripDrop}
          onclick={() => selectGuild(gg.id, { browse: S.isMobile })}
          oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => guildMenu(e, gg)}
          use:longpress={{ handler: (e) => guildMenu(e, gg) }}
        >
          {#if gg.icon}
            <img class="icon" src={gg.icon} alt="" />
          {:else}
            <span class="face"><GuildMark g={gg} /></span>
          {/if}
        </button>
        {#if tileLabel(gg) !== gg.name}<span class="vh" id="strip-d-{gg.id}">{tileLabel(gg).slice(gg.name.length + 3)}</span>{/if}
        {#if m.count}<span class="t-badge"><Badge count={m.count} max={9} muted={m.muted} ring="var(--bg-1)" /></span>
        {:else if m.dot}<span class="t-dot"><Badge dot /></span>{/if}
      </span>
    {/each}
    <span class="tile-wrap">
      {#if allGuilds.length}
        <button
          class="tile more"
          class:open={GSW.open}
          use:tooltip={{ ...tip, text: "All guilds" }}
          aria-label={overflow.count ? `All guilds · ${plural(overflow.count, "mention")} in guilds not shown` : "All guilds"}
          aria-haspopup="dialog"
          aria-expanded={GSW.open}
          onclick={openSwitcher}
        >
          <Icon name="grid4" size={phone ? 18 : 14} />
        </button>
        {#if overflow.count}<span class="t-badge"><Badge count={overflow.count} max={9} ring="var(--bg-1)" /></span>
        {:else if overflow.dot}<span class="t-dot"><Badge dot /></span>{/if}
      {:else}
        <!-- No guilds yet: the strip's last tile is the way to the first one. -->
        <button
          class="tile more"
          use:tooltip={{ ...tip, text: "Create or join a guild" }}
          aria-label="Create or join a guild"
          aria-haspopup="dialog"
          onclick={openSwitcher}
        >
          <Icon name="plus" size={phone ? 18 : 14} />
        </button>
      {/if}
    </span>
  </nav>
{:else}
<nav class="rail" aria-label="Guilds">
  {#if seasonOn}
    <div class="season" aria-hidden="true"><FxLayer fx={season} seed="season" scale={0.6} /></div>
  {/if}
  <!-- The list scrolls; the inbox, calendar and add buttons below do not. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="rail-scroll" ondragover={overRail} ondrop={dropOnRail} class:dragging={!!drag}>
    <div class="bubble-wrap">
      <button
        class="pill home"
        class:active={inDMs}
        use:tooltip={railTip}
        aria-label={S.requests.length && !inDMs
          ? `Messages — ${plural(S.requests.length, "message request")} waiting`
          : "Messages"}
        onclick={messagesClick}
      >
        <span class="jet" class:roll={rolling} onanimationend={() => (rolling = false)}>
          <Icon name="concorde" size={22} />
        </span>
      </button>
      {#if S.requests.length && !inDMs}<span class="p-dot"><Badge dot ring="var(--bg-1)" /></span>{/if}
    </div>
    <div class="divider"></div>

    {#each dms as dm (dm.id)}
      {@const u = guildUnread(dm)}
      <div class="bubble-wrap">
        <button
          class="pill dm"
          class:active={dm.id === S.activeGuildId}
          use:tooltip={railTip}
          aria-label={dm.name}
          onclick={() => selectGuild(dm.id, { browse: S.isMobile })}
        >
          {#if (dm.dmMembers ?? 2) > 2}
            <GroupAvatar faces={dm.dmFaces || []} size={40} />
          {:else}
            <Avatar
              name={dm.name}
              image={dm.dmPeerAvatar || dm.dmFaces?.[0]?.avatar || dm.icon}
              size={40}
              online={dm.dmPeer ? !!dm.dmPeerOnline : null}
              presence={dm.dmPeerPresence || ""}
            />
          {/if}
        </button>
        {#if dm.id !== S.activeGuildId && u.attention > 0}
          <span class="p-badge"><Badge count={u.attention} ring="var(--bg-1)" /></span>
        {/if}
      </div>
    {/each}

    {#if dms.length}<div class="divider"></div>{/if}

    {#each view as entry, idx (entry.t === "f" ? entry.folder.id : entry.id)}
      {#if entry.t === "g"}
        {@const sv = entry.g}
        {@const m = marks(sv)}
        {#if dropHint?.k === "bar" && dropHint.index === idx}<div class="dropbar"></div>{/if}
        <div class="bubble-wrap">
          <button
            class="pill"
            class:active={sv.id === S.activeGuildId}
            class:hasicon={sv.icon}
            class:muted={m.muted}
            class:gone={!!sv.evicted}
            class:combine={dropHint?.k === "combine" && dropHint.id === sv.id}
            style={sv.icon ? undefined : faceStyle(sv)}
            use:tooltip={{ ...railTip, text: tileLabel(sv) }}
            aria-label={sv.name}
            draggable="true"
            ondragstart={(e) => startDrag(e, { kind: "guild", id: sv.id })}
            ondragend={endDrag}
            ondragover={(e) => overGuild(e, sv, idx)}
            ondrop={(e) => dropOnGuild(e, sv, idx)}
            onclick={() => selectGuild(sv.id, { browse: S.isMobile })}
            oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => guildMenu(e, sv)}
            use:longpress={{ handler: (e) => guildMenu(e, sv) }}
          >
            {#if sv.icon}
              <img class="icon" src={sv.icon} alt="" />
            {:else}
              <span class="face"><GuildMark g={sv} size={16} /></span>
            {/if}
          </button>
          {#if sv.id !== S.activeGuildId && m.count}
            <span class="p-badge"><Badge count={m.count} muted={m.muted} ring="var(--bg-1)" /></span>
          {:else if sv.id !== S.activeGuildId && m.dot}
            <span class="p-dot"><Badge dot ring="var(--bg-1)" /></span>
          {/if}
          {#if failedInGuild(sv)}
            <span class="corner fail" title="A message in {sv.name} didn't send — open it to retry"></span>
          {:else if liveGuilds.has(sv.id)}
            <span class="corner live" title="A scheduled event is live in {sv.name}"></span>
          {:else if RADAR.unseen[sv.id]}
            <span class="corner ev" title="New event in {sv.name} — open its calendar"></span>
          {/if}
        </div>
      {:else}
        {@const folder = entry.folder}
        {@const fu = folderUnread(entry.guilds)}
        {#if dropHint?.k === "bar" && dropHint.index === idx}<div class="dropbar"></div>{/if}
        <div
          class="folder"
          class:open={folder.open}
          data-folder={folder.id}
          style={folder.color && folder.color !== DEFAULT_FOLDER_COLOR ? `--fc:${folder.color}` : undefined}
        >
          <div class="bubble-wrap">
            <button
              class="pill folder-tile"
              class:combine={dropHint?.k === "folder" && dropHint.id === folder.id}
              class:hasactive={entry.guilds.some((x) => x.id === S.activeGuildId) && !folder.open}
              use:tooltip={railTip}
              aria-label={folder.name || "Folder"}
              aria-expanded={folder.open}
              draggable="true"
              ondragstart={(e) => startDrag(e, { kind: "folder", id: folder.id })}
              ondragend={endDrag}
              ondragover={(e) => overFolder(e, folder)}
              ondrop={(e) => dropOnFolder(e, folder)}
              onclick={() => commitRail(toggleFolder(S.rail, folder.id))}
              oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => folderMenu(e, folder, { onRename: renameFromMenu })}
              use:longpress={{ handler: (e) => folderMenu(e, folder, { onRename: renameFromMenu }) }}
            >
              {#if folder.open}
                <Icon name="folder" size={18} />
              {:else}
                <span class="mini-grid">
                  {#each entry.guilds.slice(0, 4) as gg (gg.id)}
                    {#if gg.icon}
                      <img class="mini" src={gg.icon} alt="" />
                    {:else}
                      <span class="mini face" style={faceStyle(gg)}>{initials(gg.name)[0]}</span>
                    {/if}
                  {/each}
                </span>
              {/if}
            </button>
            {#if !folder.open && fu.attention > 0}
              <span class="p-badge"><Badge count={fu.attention} ring="var(--bg-1)" /></span>
            {:else if !folder.open && fu.count > 0}
              <span class="p-dot"><Badge dot ring="var(--bg-1)" /></span>
            {/if}
          </div>

          {#if folder.open}
            <div
              class="folder-body"
              ondragover={(e) => {
                if (drag?.kind === "guild") e.preventDefault();
              }}
              ondrop={(e) => dropInFolder(e, folder)}
              role="group"
            >
              <input
                class="folder-name"
                value={folder.name}
                placeholder="Folder"
                aria-label="Folder name"
                onclick={(e) => e.stopPropagation()}
                oninput={(e) => renameFolderTo(folder, e.currentTarget.value)}
              />
              {#each entry.guilds as gg, mi (gg.id)}
                {@const m = marks(gg)}
                {#if dropHint?.k === "fbar" && dropHint.fid === folder.id && dropHint.index === mi}<div class="dropbar"></div>{/if}
                <div class="bubble-wrap">
                  <button
                    class="pill"
                    class:active={gg.id === S.activeGuildId}
                    class:hasicon={gg.icon}
                    class:muted={m.muted}
                    class:gone={!!gg.evicted}
                    style={gg.icon ? undefined : faceStyle(gg)}
                    use:tooltip={{ ...railTip, text: tileLabel(gg) }}
                    aria-label={gg.name}
                    draggable="true"
                    ondragstart={(e) => startDrag(e, { kind: "guild", id: gg.id })}
                    ondragend={endDrag}
                    ondragover={(e) => overInFolder(e, folder, mi)}
                    ondrop={(e) => dropInFolder(e, folder)}
                    onclick={() => selectGuild(gg.id, { browse: S.isMobile })}
                    oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => guildMenu(e, gg, folder)}
                    use:longpress={{ handler: (e) => guildMenu(e, gg, folder) }}
                  >
                    {#if gg.icon}
                      <img class="icon" src={gg.icon} alt="" />
                    {:else}
                      <span class="face"><GuildMark g={gg} /></span>
                    {/if}
                  </button>
                  {#if gg.id !== S.activeGuildId && m.count}
                    <span class="p-badge"><Badge count={m.count} muted={m.muted} ring="var(--bg-1)" /></span>
                  {:else if gg.id !== S.activeGuildId && m.dot}
                    <span class="p-dot"><Badge dot ring="var(--bg-1)" /></span>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    {/each}
    {#if dropHint?.k === "bar" && dropHint.index >= view.length}<div class="dropbar"></div>{/if}
  </div>

  <div class="rail-foot">
    <div class="bubble-wrap">
      <button
        class="pill quiet-pill"
        class:active={S.modal?.kind === "inbox"}
        use:tooltip={{ ...railTip, text: "Inbox — every mention, reply and alert word aimed at you" }}
        aria-label={S.inbox.unread > 0 ? `Inbox — ${plural(S.inbox.unread, "unread item")}` : "Inbox"}
        onclick={() => (S.modal?.kind === "inbox" ? (S.modal = null) : openInbox())}
      >
        <Icon name="bell" size={18} />
      </button>
      {#if S.inbox.unread > 0}<span class="p-badge"><Badge count={S.inbox.unread} ring="var(--bg-1)" /></span>{/if}
    </div>
    <div class="bubble-wrap">
      <button
        class="pill quiet-pill"
        class:active={S.modal?.kind === "myCalendar"}
        use:tooltip={{ ...railTip, text: "Your calendar — everything you're part of, one river" }}
        aria-label={unseenTotal > 0 ? `Your calendar — ${plural(unseenTotal, "new or changed event")}` : "Your calendar"}
        onclick={() => (S.modal = S.modal?.kind === "myCalendar" ? null : { kind: "myCalendar" })}
      >
        <Icon name="calendar" size={18} />
      </button>
      {#if unseenTotal > 0}<span class="p-badge"><Badge count={unseenTotal} ring="var(--bg-1)" /></span>{/if}
    </div>
    <div class="divider"></div>
    <button class="pill quiet-pill" use:tooltip={railTip} aria-label="Add a guild" aria-haspopup="menu" onclick={addMenu}>
      <Icon name="plus" size={18} />
    </button>
  </div>
</nav>
{/if}

<style>
  /* ————————————————— the strip ————————————————— */
  .strip {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    height: 40px;
    padding: 0 var(--sp-2);
    flex: none;
    border-bottom: 1px solid var(--hairline);
    background: var(--bg-1);
    position: relative;
  }
  .vh {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .tile-wrap {
    position: relative;
    display: flex;
    flex: none;
  }
  .tile {
    position: relative;
    width: 28px;
    height: 28px;
    padding: 0;
    border-radius: 7px;
    display: grid;
    place-items: center;
    background: color-mix(in srgb, var(--guild-accent, var(--text-muted)) 18%, var(--bg-3));
    color: var(--text-muted);
    overflow: hidden;
    transition: filter var(--dur-quick) var(--ease-out);
  }
  /* A guild without an icon: its colour as an 18% tint with the mark in its
     ink, not a flat swatch with white initials. */
  .tile .face {
    color: color-mix(in srgb, var(--guild-accent, var(--text-muted)) 70%, var(--text));
    font-size: var(--fs-compact);
    font-weight: 600;
    letter-spacing: 0;
  }
  .tile .icon {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .tile.msg,
  .tile.more {
    background: var(--bg-3);
  }
  @media (pointer: fine) {
    .tile:hover {
      filter: brightness(1.12);
    }
    .tile.msg:hover,
    .tile.more:hover {
      color: var(--text);
      filter: none;
    }
  }
  .tile.more.open {
    color: var(--text);
  }
  /* The one "you are here" mark: a 2px ink ring INSIDE the tile, on a 1px gap
     in the sidebar's colour, so nothing reaches into the 4px gaps between
     tiles. Drawn over the icon (::after), which an inset shadow is not. */
  .tile::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
    pointer-events: none;
  }
  .tile.current::after {
    box-shadow:
      inset 0 0 0 1px var(--bg-1),
      inset 0 0 0 3px var(--text);
  }
  .tile.msg.current {
    color: var(--text);
  }
  .tile.muted {
    opacity: 0.55;
  }
  .tile.gone {
    filter: grayscale(1);
    opacity: 0.45;
  }
  /* A count at the top-right corner, cut out of the sidebar; a plain unread is
     a dot 3px under the tile. One mark per tile, never two in one spot. */
  .t-badge {
    position: absolute;
    top: -6px;
    right: -6px;
    pointer-events: none;
    display: flex;
  }
  .t-dot {
    position: absolute;
    left: 50%;
    bottom: -9px;
    translate: -50% 0;
    pointer-events: none;
    display: flex;
  }
  .tile-wrap.drop-before::before,
  .tile-wrap.drop-after::after {
    content: "";
    position: absolute;
    top: 2px;
    bottom: 2px;
    width: 2px;
    border-radius: 2px;
    background: var(--accent-hover);
  }
  .tile-wrap.drop-before::before {
    left: -3px;
  }
  .tile-wrap.drop-after::after {
    right: -3px;
  }
  /* Phone: 40px tiles in 48px hit areas, a row that scrolls sideways. */
  .strip.phone {
    height: 52px;
    gap: var(--sp-2);
    padding: 0 var(--sp-3);
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scrollbar-width: none;
  }
  .strip.phone::-webkit-scrollbar {
    display: none;
  }
  .strip.phone .tile {
    width: 40px;
    height: 40px;
    border-radius: var(--radius-md);
  }
  .strip.phone .tile .face {
    font-size: var(--fs-ui);
  }
  .strip.phone .tile::before {
    content: "";
    position: absolute;
    inset: -4px;
  }
  .strip.phone .t-badge {
    top: -4px;
    right: -6px;
  }
  .strip.phone .t-dot {
    bottom: -7px;
  }

  /* ————————————————— the classic rail ————————————————— */
  .rail {
    background: var(--bg-1);
    border-right: 1px solid var(--hairline);
    display: flex;
    flex-direction: column;
    min-height: 0;
    position: relative;
  }
  .season {
    position: absolute;
    inset: 0;
    overflow: hidden;
    pointer-events: none;
    z-index: 0;
  }
  .season :global(.fxfield) {
    position: absolute;
    inset: 0;
  }
  /* `0 1 auto`: a rail that fits keeps its footer right under the last guild;
     only a rail that outgrows the column scrolls with the footer pinned. */
  .rail-scroll {
    flex: 0 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    padding: 10px 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: none;
  }
  .rail-scroll::-webkit-scrollbar {
    display: none;
  }
  .rail-foot {
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-2) 0 calc(10px + var(--safe-bottom));
    border-top: 1px solid var(--hairline);
  }
  .bubble-wrap {
    position: relative;
    display: flex;
    flex-shrink: 0;
  }
  .pill {
    position: relative;
    width: 40px;
    height: 40px;
    border-radius: var(--radius-md);
    background: var(--guild-accent, var(--bg-3));
    color: var(--text-muted);
    display: grid;
    place-items: center;
    padding: 0;
    overflow: hidden;
    flex-shrink: 0;
    transition: filter var(--dur-quick) var(--ease-out);
  }
  .pill .face {
    width: 100%;
    height: 100%;
    display: grid;
    place-items: center;
    background: color-mix(in srgb, var(--guild-accent, var(--text-muted)) 18%, var(--bg-3));
    color: color-mix(in srgb, var(--guild-accent, var(--text-muted)) 70%, var(--text));
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .pill .icon {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  @media (pointer: fine) {
    .pill:hover {
      filter: brightness(1.12);
    }
    .pill.home:hover,
    .pill.quiet-pill:hover {
      color: var(--text);
      filter: none;
    }
  }
  .pill::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
    pointer-events: none;
  }
  .pill.active::after {
    box-shadow:
      inset 0 0 0 1px var(--bg-1),
      inset 0 0 0 3px var(--text);
  }
  .pill.home,
  .pill.quiet-pill {
    background: var(--bg-3);
  }
  .pill.home.active,
  .pill.quiet-pill.active {
    color: var(--text);
  }
  .pill.muted {
    opacity: 0.55;
  }
  .pill.gone {
    opacity: 0.45;
    filter: grayscale(1);
  }
  /* A DM bubble IS its avatar: it takes the avatar's shape from the theme. */
  .pill.dm {
    border-radius: var(--avatar-radius);
    background: transparent;
    overflow: visible;
  }
  .p-badge {
    position: absolute;
    top: -5px;
    right: -5px;
    pointer-events: none;
    display: flex;
  }
  .p-dot {
    position: absolute;
    top: 50%;
    left: -9px;
    translate: 0 -50%;
    pointer-events: none;
    display: flex;
  }
  /* Event radar and failed sends: a small corner mark, opposite the count. */
  .corner {
    position: absolute;
    bottom: -2px;
    right: -2px;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    border: 2px solid var(--bg-1);
    pointer-events: none;
  }
  .corner.live {
    background: var(--ok);
  }
  .corner.ev {
    background: var(--text-muted);
  }
  .corner.fail {
    background: var(--danger);
  }
  .jet {
    display: inline-flex;
    transform-origin: 50% 55%;
  }
  .jet.roll {
    animation: barrel-roll 0.72s cubic-bezier(0.5, 0.05, 0.2, 1);
  }
  @keyframes barrel-roll {
    55% {
      transform: rotate(235deg) scale(1.28);
    }
    100% {
      transform: rotate(360deg) scale(1);
    }
  }
  .divider {
    width: 24px;
    height: 1px;
    background: var(--hairline);
    flex-shrink: 0;
  }
  @media (prefers-reduced-motion: reduce) {
    .jet.roll {
      animation: none;
    }
  }
  @media (max-width: 768px) {
    .pill {
      width: 44px;
      height: 44px;
    }
  }

  /* ————————————————— rail folders ————————————————— */
  .folder {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    flex-shrink: 0;
  }
  .folder.open {
    background: var(--bg-3);
    border-radius: var(--radius-lg);
    padding: 6px 0 var(--sp-2);
    width: 52px;
  }
  .folder-tile {
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .folder.open .folder-tile {
    background: transparent;
  }
  .folder-tile :global(svg) {
    color: var(--fc, var(--text-muted));
  }
  .folder-tile.hasactive::after {
    box-shadow:
      inset 0 0 0 1px var(--bg-1),
      inset 0 0 0 3px var(--text);
  }
  .mini-grid {
    width: 100%;
    height: 100%;
    padding: 5px;
    display: grid;
    grid-template-columns: 1fr 1fr;
    grid-template-rows: 1fr 1fr;
    gap: 3px;
    box-sizing: border-box;
  }
  .mini {
    width: 100%;
    height: 100%;
    border-radius: 3px;
    background: var(--guild-accent, var(--bg-2));
    object-fit: cover;
    display: grid;
    place-items: center;
    color: #fff;
    font-size: var(--fs-micro);
    font-weight: 600;
    overflow: hidden;
  }
  .folder-body {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
  }
  .folder-name {
    width: 46px;
    background: transparent;
    border: none;
    border-bottom: 1px solid transparent;
    color: var(--text-muted);
    font-size: var(--fs-micro);
    text-align: center;
    padding: 1px 0;
    outline: none;
    text-overflow: ellipsis;
  }
  @media (pointer: fine) {
    .folder-name:hover {
      border-bottom-color: var(--border);
    }
  }
  .folder-name:focus {
    color: var(--text);
    border-bottom-color: var(--accent-hover);
  }
  @media (max-width: 768px) {
    .folder.open {
      width: 60px;
    }
    .folder-name {
      width: 54px;
      font-size: 16px;
      border-bottom-color: var(--border);
      padding: 3px 0;
    }
  }
  .pill.combine,
  .folder-tile.combine {
    box-shadow: 0 0 0 2px var(--accent-hover);
  }
  .dropbar {
    width: 28px;
    height: 2px;
    border-radius: 2px;
    background: var(--accent-hover);
    flex-shrink: 0;
  }
  .rail-scroll.dragging .pill {
    cursor: grab;
  }
</style>
