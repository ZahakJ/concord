<script>
  // The leftmost rail: a Concord "home" bubble (opens DMs), one bubble per DM
  // and per guild with unread badges, plus create/join. Guilds can be dragged
  // to reorder, dropped onto each other to form folders, and grouped/renamed —
  // Discord-style. The layout is a device-local preference (lib/rail.js), not
  // part of any guild's crypto state.
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
    commitRail,
  } from "./lib/state.svelte.js";
  import {
    reconcile,
    combineGuilds,
    moveGuild,
    moveFolder,
    toggleFolder,
    renameFolder,
    setFolderColor,
    dissolveFolder,
    DEFAULT_FOLDER_COLOR,
  } from "./lib/rail.js";
  import { playFlyby } from "./lib/sounds.js";
  import { longpress } from "./lib/touch.js";
  import { tooltip } from "./lib/tooltip.js";
  import FxLayer from "./FxLayer.svelte";

  // Seasonal touches: the FX engine has shipped snow, petals and leaves for a
  // year, and nothing ever read the calendar. Local clock ONLY (no network —
  // the no-runtime-fetch rule), a sparse field over just this 64px rail, and
  // an Appearance toggle to turn it off. Quiet months return null and the
  // layer simply isn't there.
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
  const season = seasonFx(); // computed once — nobody keeps the app open across an equinox
  const seasonOn = $derived(!!season && S.prefs.seasonal !== false);
  import { RADAR, guildLiveSet } from "./lib/radar.svelte.js";

  // The rail is an icon-only column — identifying a bubble is the whole point
  // of hovering it, so the tip comes fast and sits to the right, off the strip.
  // Text falls through to each pill's aria-label (see lib/tooltip.js).
  const railTip = { side: "right", delay: 80 };

  // Touch: long-press opens the rail menus. iOS/WKWebView never synthesizes
  // `contextmenu` for a plain element, so mute / leave / invite / guild settings
  // / folder colour were unreachable from the rail entirely there; Android's
  // synthesized one would double-fire alongside the longpress. Same pattern
  // ChannelList and MemberPanel already use.
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  // Every rail rearrangement — reorder, file into a folder, and (since a folder
  // is only ever born by dropping one guild onto another) creating a folder at
  // all — is built on HTML5 drag-and-drop, which no mobile WebView synthesizes
  // from touch. Folders are a device-local preference, so a phone-only user
  // could never have one. These give every drag a menu equivalent, which also
  // hands the same operations to keyboard-only desktop users.
  function arrangeItems(sv, folder) {
    const items = [];
    if (folder) {
      const i = folder.ids.indexOf(sv.id);
      if (i > 0)
        items.push({ label: "Move up", icon: "chevron", onClick: () => moveInFolder(sv, folder, i - 1) });
      if (i > -1 && i < folder.ids.length - 1)
        // moveGuild reads its index against the layout BEFORE the guild is
        // lifted out, so one step down is i+2, not i+1.
        items.push({ label: "Move down", icon: "chevron", onClick: () => moveInFolder(sv, folder, i + 2) });
      items.push({
        label: "Take out of folder",
        icon: "door",
        onClick: () => commitRail(moveGuild(S.rail, sv.id, { kind: "top", index: topIndexOfFolder(folder.id) + 1 })),
      });
      return items;
    }
    const i = S.rail.findIndex((e) => e.t === "g" && e.id === sv.id);
    if (i > 0) items.push({ label: "Move up", icon: "chevron", onClick: () => moveTop(sv, i - 1) });
    if (i > -1 && i < S.rail.length - 1)
      items.push({ label: "Move down", icon: "chevron", onClick: () => moveTop(sv, i + 2) });
    const folders = S.rail.filter((e) => e.t === "f");
    if (folders.length) {
      items.push({ label: "Add to folder", header: true });
      for (const f of folders)
        items.push({
          label: f.name || "Folder",
          icon: "folder",
          onClick: () => commitRail(moveGuild(S.rail, sv.id, { kind: "folder", folderId: f.id })),
        });
    }
    const others = S.rail
      .filter((e) => e.t === "g" && e.id !== sv.id)
      .map((e) => guildById.get(e.id))
      .filter(Boolean);
    if (others.length) {
      items.push({ label: "New folder with…", header: true });
      for (const o of others)
        items.push({
          label: o.name,
          icon: "diamond",
          onClick: () => commitRail(combineGuilds(S.rail, sv.id, o.id, DEFAULT_FOLDER_COLOR)),
        });
    }
    return items;
  }
  const moveTop = (sv, index) => commitRail(moveGuild(S.rail, sv.id, { kind: "top", index }));
  const moveInFolder = (sv, folder, index) =>
    commitRail(moveGuild(S.rail, sv.id, { kind: "folder", folderId: folder.id, index }));

  // openContextMenu positions at the event, so a menu opened FROM a menu item
  // needs the original point carried forward.
  const atPoint = (e) => ({
    clientX: e.clientX,
    clientY: e.clientY,
    preventDefault() {},
    stopPropagation() {},
  });

  function guildMenu(e, sv, folder = null) {
    const pt = atPoint(e);
    const arrange = arrangeItems(sv, folder);
    openContextMenu(
      e,
      [
        ...guildMenuItems(sv),
        // Behind one entry rather than inline: "New folder with…" lists every
        // other guild, and a menu you have to scroll past to reach "Leave" is
        // worse than one more tap.
        arrange.length && { sep: true },
        arrange.length && {
          label: "Rearrange…",
          icon: "folder",
          onClick: () => openContextMenu(pt, arrange, { title: `Rearrange ${sv.name}` }),
        },
      ].filter(Boolean),
      { title: sv.name },
    );
  }

  function addMenu(e) {
    openContextMenu(
      e,
      [
        { label: "Create a guild", icon: "spark", onClick: () => (S.modal = { kind: "create" }) },
        { label: "Join with an invite code", icon: "download", onClick: () => (S.modal = { kind: "join", code: "" }) },
        { label: "Instant meeting (1 hour – 30 days)", icon: "bolt", onClick: startMeeting },
      ],
      { title: "Add a guild" },
    );
  }

  // Hammering the jet flies a Concorde past you. Tuned so ordinary use can't
  // reach it: eight clicks with no more than FLYBY_GAP between any two, i.e. a
  // deliberate burst of under three seconds. A pause resets the run — no timer
  // needed, since the count is only ever read on the next click.
  const FLYBY_CLICKS = 8;
  const FLYBY_GAP = 400;
  let clicks = 0;
  let lastClick = 0;

  let rolling = $state(false);
  function homeClick() {
    // Already in the DM area: openDMs() would re-select the FIRST dm in raw
    // order, which is rarely the one you're reading — so a click that should be
    // a no-op re-fetches the right panel and can bump you into another
    // conversation. The barrel roll still plays; it's local feedback, not
    // navigation, and the egg below needs the button to feel alive.
    if (!inDMs) openDMs();

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

  // Event radar surfaces (lib/radar.svelte.js): guilds with a meeting live
  // RIGHT NOW wear a pulsing --ok dot; guilds where someone scheduled or moved
  // an event you haven't looked at yet wear a quiet accent dot; the calendar
  // pill below totals the unseen count so "click the calendar thingy" has a
  // visible reason. All of it clears by opening the relevant calendar.
  const liveGuilds = $derived(guildLiveSet());
  const unseenTotal = $derived(Object.values(RADAR.unseen).reduce((a, b) => a + b, 0));

  const g = $derived(S.guilds.find((x) => x.id === S.activeGuildId) || null);
  const inDMs = $derived(g?.kind === "dm");
  const allGuilds = $derived(S.guilds.filter((x) => x.kind !== "dm"));
  const guildById = $derived(new Map(allGuilds.map((s) => [s.id, s])));
  const dms = $derived(
    S.guilds.filter(
      (x) =>
        x.kind === "dm" &&
        !x.dmNotes &&
        (guildUnread(x).count > 0 || x.id === S.activeGuildId),
    ),
  );

  // Keep the persisted layout in step with the live guild set (adds/removes,
  // dissolved folders). Runs whenever the guild list changes.
  $effect(() => {
    const ids = allGuilds.map((s) => s.id);
    const next = reconcile(S.rail, ids);
    if (JSON.stringify(next) !== JSON.stringify(S.rail)) commitRail(next);
  });

  // The renderable layout: resolve guild objects; skip any the reconcile pass
  // hasn't caught yet (defensive).
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

  const initials = (name) =>
    name
      .split(/\s+/)
      .map((w) => w[0])
      .join("")
      .slice(0, 2);

  function guildTint(id) {
    let h = 0;
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
    const hue = h % 360;
    return `background:linear-gradient(135deg, hsl(${hue} 42% 34%), hsl(${(hue + 45) % 360} 48% 25%));color:#fff`;
  }

  function folderUnread(guilds) {
    let count = 0,
      mentions = 0;
    for (const gg of guilds) {
      const u = guildUnread(gg);
      count += u.count;
      mentions += u.mentions;
    }
    return { count, mentions };
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

  // ————————————————— folder management —————————————————
  // Concord-native folder swatches — deliberately not Discord's blurple palette.
  const SWATCHES = [
    ["Accent", "var(--accent)"],
    ["Teal", "#2dd4bf"],
    ["Violet", "#a78bfa"],
    ["Amber", "#f5a524"],
    ["Rose", "#f472b6"],
    ["Sage", "#7bb87b"],
    ["Slate", "#7c8798"],
  ];
  // The folder's name field is a 46px box in a 64px column — findable with a
  // mouse, invisible as an affordance under a finger (its only cue was a hover
  // border). Renaming gets a menu entry that opens the folder and puts the
  // caret in the field, so the phone has a route to it at all.
  function renameFromMenu(folder) {
    if (!folder.open) commitRail(toggleFolder(S.rail, folder.id, true));
    // Two frames: one for the rail to commit, one for the folder body to mount.
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        const el = document.querySelector(`[data-folder="${folder.id}"] .folder-name`);
        el?.focus();
        el?.select?.();
      }),
    );
  }

  function folderMenu(e, folder) {
    const idx = topIndexOfFolder(folder.id);
    openContextMenu(
      e,
      [
        { label: folder.open ? "Collapse folder" : "Expand folder", icon: "folder", onClick: () => commitRail(toggleFolder(S.rail, folder.id)) },
        { label: "Rename folder", icon: "edit", onClick: () => renameFromMenu(folder) },
        idx > 0 && { label: "Move up", icon: "chevron", onClick: () => commitRail(moveFolder(S.rail, folder.id, idx - 1)) },
        idx > -1 && idx < S.rail.length - 1 && { label: "Move down", icon: "chevron", onClick: () => commitRail(moveFolder(S.rail, folder.id, idx + 2)) },
        { sep: true },
        ...SWATCHES.map(([name, color]) => ({
          label: name,
          swatch: color,
          active: (folder.color || "var(--accent)") === color,
          onClick: () => commitRail(setFolderColor(S.rail, folder.id, color)),
        })),
        { label: "Ungroup folder", icon: "trash", danger: true, onClick: () => commitRail(dissolveFolder(S.rail, folder.id)) },
      ],
      { title: folder.name || "Folder" },
    );
  }
  function onRename(e, folder) {
    commitRail(renameFolder(S.rail, folder.id, e.target.value.slice(0, 40)));
  }
</script>

<nav class="rail" aria-label="Guilds">
  {#if seasonOn}
    <div class="season" aria-hidden="true"><FxLayer fx={season} seed="season" scale={0.6} /></div>
  {/if}
  <!-- The list scrolls; the add/meeting buttons below do not. They used to be
       rendered last, after every guild, inside a 64px strip whose scrollbar is
       hidden — so at ~13 bubbles (one phone screenful) the only way to create
       or join anything was to guess that an unmarked column scrolls. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="rail-scroll"
    ondragover={overRail}
    ondrop={dropOnRail}
    class:dragging={!!drag}
  >
    <button
      class="pill home"
      class:active={inDMs}
      use:tooltip={railTip}
      aria-label={S.requests.length && !inDMs
        ? `Home / Direct messages — ${S.requests.length} message request${S.requests.length === 1 ? "" : "s"} waiting`
        : "Home / Direct messages"}
      onclick={homeClick}
    >
      <span class="jet" class:roll={rolling} onanimationend={() => (rolling = false)}>
        <Icon name="concorde" size={24} />
      </span>
      {#if S.requests.length && !inDMs}
        <!-- Message requests get a dot, never the red count badge: a stranger who
             has not been accepted must not be able to make the app look like a
             friend is waiting. -->
        <span class="req-dot" title="Message requests waiting"></span>
      {/if}
    </button>
    <div class="divider"></div>

    {#each dms as dm, i (dm.id)}
      {@const u = guildUnread(dm)}
      <div class="bubble-wrap" style="--i:{i}">
        <button
          class="pill dm"
          class:active={dm.id === S.activeGuildId}
          use:tooltip={railTip}
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

    {#each view as entry, idx (entry.t === "f" ? entry.folder.id : entry.id)}
      {#if entry.t === "g"}
        {@const sv = entry.g}
        {@const u = guildUnread(sv)}
        {#if dropHint?.k === "bar" && dropHint.index === idx}<div class="dropbar"></div>{/if}
        <div class="bubble-wrap" style="--i:{idx}">
          <button
            class="pill"
            class:active={sv.id === S.activeGuildId}
            class:hasicon={sv.icon}
            class:combine={dropHint?.k === "combine" && dropHint.id === sv.id}
            use:tooltip={railTip}
            aria-label={sv.name}
            draggable="true"
            ondragstart={(e) => startDrag(e, { kind: "guild", id: sv.id })}
            ondragend={endDrag}
            ondragover={(e) => overGuild(e, sv, idx)}
            ondrop={(e) => dropOnGuild(e, sv, idx)}
            onclick={() => selectGuild(sv.id)}
            oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => guildMenu(e, sv)}
            use:longpress={{ handler: (e) => guildMenu(e, sv) }}
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
          {#if liveGuilds.has(sv.id)}
            <span class="live-dot" title="A scheduled event is live in {sv.name} — a channel inside wears LIVE"></span>
          {:else if RADAR.unseen[sv.id]}
            <span class="ev-dot" title="New event in {sv.name} — open its calendar"></span>
          {/if}
        </div>
      {:else}
        {@const folder = entry.folder}
        {@const fu = folderUnread(entry.guilds)}
        {#if dropHint?.k === "bar" && dropHint.index === idx}<div class="dropbar"></div>{/if}
        <div class="folder" class:open={folder.open} data-folder={folder.id}>
          <div class="bubble-wrap folder-wrap" style="--fc:{folder.color}">
            <button
              class="pill folder-tile"
              class:combine={dropHint?.k === "folder" && dropHint.id === folder.id}
              class:hasactive={entry.guilds.some((x) => x.id === S.activeGuildId) && !folder.open}
              use:tooltip={railTip}
              aria-label={folder.name || "Folder"}
              draggable="true"
              ondragstart={(e) => startDrag(e, { kind: "folder", id: folder.id })}
              ondragend={endDrag}
              ondragover={(e) => overFolder(e, folder)}
              ondrop={(e) => dropOnFolder(e, folder)}
              onclick={() => commitRail(toggleFolder(S.rail, folder.id))}
              oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => folderMenu(e, folder)}
              use:longpress={{ handler: (e) => folderMenu(e, folder) }}
            >
              {#if folder.open}
                <Icon name="folder" size={20} />
              {:else}
                <span class="mini-grid">
                  {#each entry.guilds.slice(0, 4) as gg (gg.id)}
                    {#if gg.icon}
                      <img class="mini" src={gg.icon} alt="" />
                    {:else}
                      <span class="mini face" style={guildTint(gg.id)}>{initials(gg.name)[0]}</span>
                    {/if}
                  {/each}
                </span>
              {/if}
            </button>
            {#if !folder.open && fu.count > 0}
              <span class="badge" class:mention={fu.mentions > 0}>{fu.count > 99 ? "99+" : fu.count}</span>
            {/if}
          </div>

          {#if folder.open}
            <div
              class="folder-body"
              ondragover={(e) => { if (drag?.kind === "guild") { e.preventDefault(); } }}
              ondrop={(e) => dropInFolder(e, folder)}
              role="group"
            >
              <input
                class="folder-name"
                value={folder.name}
                placeholder="Folder"
                aria-label="Folder name"
                onclick={(e) => e.stopPropagation()}
                oninput={(e) => onRename(e, folder)}
              />
              {#each entry.guilds as gg, mi (gg.id)}
                {@const u = guildUnread(gg)}
                {#if dropHint?.k === "fbar" && dropHint.fid === folder.id && dropHint.index === mi}<div class="dropbar dropbar--in"></div>{/if}
                <div class="bubble-wrap" style="--i:{mi}">
                  <button
                    class="pill"
                    class:active={gg.id === S.activeGuildId}
                    class:hasicon={gg.icon}
                    use:tooltip={railTip}
                    aria-label={gg.name}
                    draggable="true"
                    ondragstart={(e) => startDrag(e, { kind: "guild", id: gg.id })}
                    ondragend={endDrag}
                    ondragover={(e) => overInFolder(e, folder, mi)}
                    ondrop={(e) => dropInFolder(e, folder)}
                    onclick={() => selectGuild(gg.id)}
                    oncontextmenu={coarse ? (e) => e.preventDefault() : (e) => guildMenu(e, gg, folder)}
                    use:longpress={{ handler: (e) => guildMenu(e, gg, folder) }}
                  >
                    {#if gg.icon}
                      <img class="icon" src={gg.icon} alt="" />
                    {:else}
                      <span class="face" style={guildTint(gg.id)}>{initials(gg.name)}</span>
                    {/if}
                  </button>
                  {#if gg.id !== S.activeGuildId && u.count > 0}
                    <span class="badge" class:mention={u.mentions > 0}>{u.count > 99 ? "99+" : u.count}</span>
                  {/if}
                  {#if liveGuilds.has(gg.id)}
                    <span class="live-dot" title="A scheduled event is live in {gg.name} — a channel inside wears LIVE"></span>
                  {:else if RADAR.unseen[gg.id]}
                    <span class="ev-dot" title="New event in {gg.name} — open its calendar"></span>
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
    <!-- The blended "Your calendar" used to hide behind the Welcome screen and
         the jump palette — invisible once you live in a channel. The rail is
         the one strip on screen in every layout (desktop column, phone
         drawer), and the pinned footer keeps it reachable at any guild count.
         Same pill DNA as the guilds above; tapping again puts it away. -->
    <button
      class="pill cal"
      class:active={S.modal?.kind === "myCalendar"}
      use:tooltip={{ ...railTip, text: "Your calendar — everything you're part of, one river" }}
      aria-label="Your calendar"
      onclick={() => (S.modal = S.modal?.kind === "myCalendar" ? null : { kind: "myCalendar" })}
    >
      <Icon name="calendar" size={20} />
      {#if unseenTotal > 0}
        <!-- Accent, not danger: new plans are an invitation, and red is spent
             on "people are talking at you". Opening the calendar clears it. -->
        <span class="cal-badge" aria-label="{unseenTotal} new or changed {unseenTotal === 1 ? 'event' : 'events'}">
          {unseenTotal > 99 ? "99+" : unseenTotal}
        </span>
      {/if}
    </button>
    <div class="divider"></div>
    {#if S.isMobile}
      <button class="pill add" use:tooltip={railTip} aria-label="Add a guild" onclick={addMenu}>
        <Icon name="plus" />
      </button>
      <button
        class="pill add meet"
        use:tooltip={{ ...railTip, text: "Instant meeting — a disposable room + invite to send anyone" }}
        aria-label="Start an instant meeting"
        onclick={startMeeting}
      >
        <Icon name="bolt" />
      </button>
    {:else}
      <button class="pill add" use:tooltip={railTip} aria-label="Create a guild" onclick={() => (S.modal = { kind: "create" })}>
        <Icon name="plus" />
      </button>
      <button
        class="pill add meet"
        use:tooltip={{ ...railTip, text: "Instant meeting — a disposable room + invite to send anyone" }}
        aria-label="Start an instant meeting"
        onclick={startMeeting}
      >
        <Icon name="bolt" />
      </button>
      <button class="pill add" use:tooltip={railTip} aria-label="Join with an invite code" onclick={() => (S.modal = { kind: "join", code: "" })}>
        <Icon name="download" />
      </button>
    {/if}
  </div>
</nav>

<style>
  .rail {
    background: var(--bg-0);
    display: flex;
    flex-direction: column;
    min-height: 0;
    position: relative;
  }
  /* Seasonal field floats over the rail's chrome but under nothing clickable —
     pointer-events off, and the pills stack above it. */
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
  /* `0 1 auto`, not `1`: a rail that fits keeps the add buttons directly under
     the last guild, exactly as before. Only once the list outgrows the column
     does the scroller claim the remaining height and leave the footer pinned. */
  .rail-scroll {
    flex: 0 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    padding: 10px 0;
    overflow-y: auto;
    /* A fling off the end of the rail must not continue into the chat feed
       showing past the open drawer. */
    overscroll-behavior: contain;
    scrollbar-width: none;
  }
  .rail-scroll::-webkit-scrollbar {
    display: none;
  }
  /* Creating or joining is always one tap away, whatever the guild count. The
     hairline is also the only cue this strip has that the list above it
     scrolls — its scrollbar is hidden, so at 20 guilds the way to add one was
     to guess that an unmarked 64px column scrolls. */
  .rail-foot {
    flex-shrink: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    padding: 8px 0 calc(10px + var(--safe-bottom));
    border-top: 1px solid var(--border);
    background: var(--bg-0);
  }
  .pill {
    position: relative;
    width: 42px;
    height: 42px;
    border-radius: 50%;
    background: var(--bg-2);
    color: var(--text);
    font-weight: 600;
    font-size: var(--fs-ui);
    text-transform: uppercase;
    display: grid;
    place-items: center;
    padding: 0;
    transition:
      border-radius 0.18s var(--ease-out),
      background 0.18s ease,
      transform var(--dur-quick) ease,
      box-shadow 0.18s ease;
    flex-shrink: 0;
  }
  /* Mouse only: a tap synthesises :hover and leaves the pill stuck in its
     squircle-and-lifted state until you tap elsewhere. :active is the touch
     answer, and it is already unconditional below. */
  @media (pointer: fine) {
    .pill:hover {
      border-radius: var(--radius-lg);
      background: var(--bg-3);
      transform: translateY(-1px) scale(1.04);
    }
  }
  .pill:active {
    transform: scale(0.92);
  }
  .pill.active {
    border-radius: var(--radius-lg);
    background: var(--accent);
    color: var(--accent-fg);
    box-shadow: var(--accent-glow);
  }
  /* A DM bubble IS its avatar, so its container has to take the avatar's shape
     from the theme. Left at a hard 50% it stayed a circle while the picture
     inside squared off, and the corners spilled past the outline — a square
     drawn over a circle, which reads as broken CSS rather than as a style.
     Guild pills keep the 50% -> 14px squircle morph above: those are tiles, not
     faces, and Discord's rounding cue on them is deliberate. */
  .pill.dm,
  .pill.dm:hover,
  .pill.dm.active {
    border-radius: var(--avatar-radius);
  }
  .pill.dm.active {
    background: var(--bg-2);
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .pill.dm .badge {
    top: -3px;
    bottom: auto;
  }
  .pill.home {
    border-radius: var(--radius-lg);
    background: var(--bg-3);
    color: var(--accent-hover);
  }
  @media (pointer: fine) {
    .pill.home:hover {
      border-radius: var(--radius-lg);
      color: var(--accent-fg);
      background: var(--accent);
    }
  }
  .pill.home :global(svg) {
    transition: transform 0.18s var(--ease-spring);
  }
  .pill.home:hover :global(svg) {
    transform: rotate(-10deg) translateY(-1px);
  }
  .req-dot {
    position: absolute;
    top: -1px;
    right: -1px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--text-muted);
    border: 2px solid var(--bg-0);
    pointer-events: none;
  }
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
    border-radius: var(--radius-lg);
    background: var(--accent);
    color: var(--accent-fg);
  }
  .pill .icon {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: inherit;
  }
  .pill.hasicon {
    background: var(--bg-2);
    overflow: hidden;
  }
  .pill.meet {
    color: var(--warn);
  }
  @media (pointer: fine) {
    .pill.meet:hover {
      background: color-mix(in srgb, var(--warn) 24%, var(--bg-3));
      color: var(--text);
    }
  }
  .divider {
    width: 28px;
    height: 2px;
    border-radius: 2px;
    background: var(--border);
    margin: 2px 0;
    flex-shrink: 0;
  }
  /* The calendar pill: home's squircle voice (a place you go, not a guild),
     accent ink so it reads as "yours". .pill.active supplies the open state —
     the same fill a selected guild wears. */
  .pill.cal {
    border-radius: var(--radius-lg);
    background: var(--bg-3);
    color: var(--accent-hover);
  }
  @media (pointer: fine) {
    .pill.cal:hover {
      border-radius: var(--radius-lg);
      background: var(--accent);
      color: var(--accent-fg);
    }
  }
  .pill.cal.active {
    border-radius: var(--radius-lg);
  }
  .pill.add {
    background: transparent;
    border: 1px dashed var(--border);
    color: var(--text-muted);
  }
  .pill.add :global(svg) {
    transition: transform 0.22s var(--ease-spring);
  }
  @media (pointer: fine) {
    .pill.add:hover {
      color: var(--accent-hover);
      border-color: var(--accent-hover);
      background: var(--bg-2);
    }
    .pill.add:hover :global(svg) {
      transform: rotate(90deg);
    }
  }
  .bubble-wrap {
    position: relative;
    display: flex;
    flex-shrink: 0;
    animation: rail-in 0.3s var(--ease-out) both;
    animation-delay: calc(40ms + var(--i, 0) * 25ms);
  }
  .pill.home,
  .pill.add,
  .pill.cal {
    animation: rail-in 0.3s var(--ease-out) both;
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
      height 0.22s var(--ease-spring),
      opacity 0.18s ease;
  }
  @media (pointer: fine) {
    .bubble-wrap:hover::before {
      height: 16px;
      opacity: 0.7;
    }
  }
  .bubble-wrap:has(.pill.active)::before {
    height: 30px;
    opacity: 1;
  }
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
    border-radius: var(--radius-md);
    background: var(--danger);
    color: var(--danger-fg);
    font-size: var(--fs-small);
    font-weight: 700;
    line-height: 1;
    display: grid;
    place-items: center;
    border: 2px solid var(--bg-0);
    pointer-events: none;
    animation: badge-pop 0.25s var(--ease-spring) both;
  }
  @keyframes badge-pop {
    from {
      transform: scale(0.4);
      opacity: 0;
    }
  }
  /* ---- event radar dots (bottom-right, opposite corner from unread) ----
     live-dot: a meeting is happening inside — pulsing --ok, mirror of the
     channel row's LIVE chip. ev-dot: unseen new/changed event — still accent,
     quiet, cleared by opening that calendar. Both ride the bubble corner so
     they can coexist with the red unread count at the top. */
  .live-dot,
  .ev-dot {
    position: absolute;
    bottom: -2px;
    right: -2px;
    width: 11px;
    height: 11px;
    border-radius: 50%;
    border: 2px solid var(--bg-0);
    pointer-events: none;
    animation: badge-pop 0.25s var(--ease-spring) both;
  }
  .live-dot {
    background: var(--ok);
    animation:
      badge-pop 0.25s var(--ease-spring) both,
      rail-live-pulse 1.4s ease-in-out 0.3s infinite;
  }
  @keyframes rail-live-pulse {
    50% {
      box-shadow: 0 0 0 4px color-mix(in srgb, var(--ok) 25%, transparent);
    }
  }
  .ev-dot {
    background: var(--accent);
    box-shadow: 0 0 6px color-mix(in srgb, var(--accent) 55%, transparent);
  }
  /* The rail calendar's unseen-events count: same geometry as the unread
     badge, accent ink — a nudge toward plans, not an alarm. */
  .cal-badge {
    position: absolute;
    top: -3px;
    right: -3px;
    min-width: 18px;
    height: 18px;
    padding: 0 5px;
    border-radius: var(--radius-md);
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fs-small);
    font-weight: 700;
    line-height: 1;
    display: grid;
    place-items: center;
    border: 2px solid var(--bg-0);
    pointer-events: none;
    animation: badge-pop 0.25s var(--ease-spring) both;
  }
  .badge.mention {
    animation:
      badge-pop 0.25s var(--ease-spring) both,
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
      animation: badge-pop 0.25s var(--ease-spring) both;
    }
    .live-dot,
    .ev-dot,
    .cal-badge {
      animation: none;
    }
    .bubble-wrap,
    .pill.home,
    .pill.add,
    .pill.cal,
    .jet.roll {
      animation: none;
    }
    .pill:hover,
    .pill:active {
      transform: none;
    }
    .pill.add:hover :global(svg),
    .pill.home:hover :global(svg) {
      transform: none;
    }
  }
  @media (pointer: coarse), (max-width: 768px) {
    .rail-scroll {
      gap: 10px;
      padding: 12px 0;
    }
    .pill {
      width: 48px;
      height: 48px;
    }
    .bubble-wrap::before {
      left: -8px;
    }
    /* An open folder has to be wide enough for a name field that will not
       trigger iOS focus zoom, which starts under 16px. The underline stays
       drawn: hover was the only thing that ever said this was editable, and a
       finger cannot produce one. */
    .folder.open {
      width: 60px; /* the rail column is 64px — leave it a rim */
    }
    .folder-name {
      width: 54px;
      font-size: 16px;
      border-bottom-color: color-mix(in srgb, var(--border) 70%, transparent);
      padding: 3px 0;
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

  /* ————————————————— folders ————————————————— */
  .folder {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-2);
    flex-shrink: 0;
  }
  .folder.open {
    background: color-mix(in srgb, var(--fc, var(--accent)) 16%, transparent);
    border-radius: 18px;
    padding: 6px 0 8px;
    width: 52px;
  }
  .folder-tile {
    background: color-mix(in srgb, var(--fc, var(--accent)) 26%, var(--bg-2));
    color: var(--text);
    overflow: hidden;
  }
  @media (pointer: fine) {
    .folder-tile:hover {
      background: color-mix(in srgb, var(--fc, var(--accent)) 40%, var(--bg-2));
    }
  }
  .folder-tile.hasactive {
    box-shadow: 0 0 0 2px var(--fc, var(--accent));
  }
  .folder-tile :global(svg) {
    color: var(--fc, var(--accent));
    filter: brightness(1.6);
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
    border-radius: var(--radius-sm);
    object-fit: cover;
    display: grid;
    place-items: center;
    font-size: var(--fs-micro);
    font-weight: 700;
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
    border-bottom-color: var(--fc, var(--accent));
  }

  /* drag feedback */
  .pill.combine {
    box-shadow: 0 0 0 3px var(--accent);
    transform: scale(1.06);
  }
  .folder-tile.combine {
    box-shadow: 0 0 0 3px var(--fc, var(--accent));
    transform: scale(1.06);
  }
  .dropbar {
    width: 30px;
    height: 3px;
    border-radius: 3px;
    background: var(--accent);
    box-shadow: var(--accent-glow);
    flex-shrink: 0;
    animation: badge-pop var(--dur-standard) ease both;
  }
  .dropbar--in {
    width: 24px;
  }
  .rail-scroll.dragging .pill {
    cursor: grab;
  }
</style>
