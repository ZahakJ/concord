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

  function guildMenu(e, sv) {
    openContextMenu(e, guildMenuItems(sv), { title: sv.name });
  }

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

  const g = $derived(S.guilds.find((x) => x.id === S.activeGuildId) || null);
  const inDMs = $derived(g?.kind === "dm");
  const servers = $derived(S.guilds.filter((x) => x.kind !== "dm"));
  const guildById = $derived(new Map(servers.map((s) => [s.id, s])));
  const dms = $derived(
    S.guilds.filter(
      (x) =>
        x.kind === "dm" &&
        !x.dmNotes &&
        (guildUnread(x).count > 0 || x.id === S.activeGuildId),
    ),
  );

  // Keep the persisted layout in step with the live guild set (adds/removes,
  // dissolved folders). Runs whenever the server list changes.
  $effect(() => {
    const ids = servers.map((s) => s.id);
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
  function folderMenu(e, folder) {
    openContextMenu(
      e,
      [
        { label: folder.open ? "Collapse folder" : "Expand folder", icon: "folder", onClick: () => commitRail(toggleFolder(S.rail, folder.id)) },
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

<nav
  class="rail"
  aria-label="Servers"
  ondragover={overRail}
  ondrop={dropOnRail}
  class:dragging={!!drag}
>
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
          title={sv.name}
          aria-label={sv.name}
          draggable="true"
          ondragstart={(e) => startDrag(e, { kind: "guild", id: sv.id })}
          ondragend={endDrag}
          ondragover={(e) => overGuild(e, sv, idx)}
          ondrop={(e) => dropOnGuild(e, sv, idx)}
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
    {:else}
      {@const folder = entry.folder}
      {@const fu = folderUnread(entry.guilds)}
      {#if dropHint?.k === "bar" && dropHint.index === idx}<div class="dropbar"></div>{/if}
      <div class="folder" class:open={folder.open}>
        <div class="bubble-wrap folder-wrap" style="--fc:{folder.color}">
          <button
            class="pill folder-tile"
            class:combine={dropHint?.k === "folder" && dropHint.id === folder.id}
            class:hasactive={entry.guilds.some((x) => x.id === S.activeGuildId) && !folder.open}
            title={folder.name || "Folder"}
            aria-label={folder.name || "Folder"}
            draggable="true"
            ondragstart={(e) => startDrag(e, { kind: "folder", id: folder.id })}
            ondragend={endDrag}
            ondragover={(e) => overFolder(e, folder)}
            ondrop={(e) => dropOnFolder(e, folder)}
            onclick={() => commitRail(toggleFolder(S.rail, folder.id))}
            oncontextmenu={(e) => folderMenu(e, folder)}
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
                  title={gg.name}
                  aria-label={gg.name}
                  draggable="true"
                  ondragstart={(e) => startDrag(e, { kind: "guild", id: gg.id })}
                  ondragend={endDrag}
                  ondragover={(e) => overInFolder(e, folder, mi)}
                  ondrop={(e) => dropInFolder(e, folder)}
                  onclick={() => selectGuild(gg.id)}
                  oncontextmenu={(e) => guildMenu(e, gg)}
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
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  {/each}
  {#if dropHint?.k === "bar" && dropHint.index >= view.length}<div class="dropbar"></div>{/if}

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
    color: var(--accent-fg);
    box-shadow: var(--accent-glow);
  }
  .pill.dm:hover,
  .pill.dm.active {
    border-radius: 50%;
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
    border-radius: 15px;
    background: var(--bg-3);
    color: var(--accent-hover);
  }
  .pill.home:hover {
    border-radius: 15px;
    color: var(--accent-fg);
    background: var(--accent);
  }
  .pill.home :global(svg) {
    transition: transform 0.18s cubic-bezier(0.34, 1.56, 0.64, 1);
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
    border-radius: 15px;
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
  .pill.meet:hover {
    background: color-mix(in srgb, var(--warn) 24%, var(--bg-3));
    color: var(--text);
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
  .pill.add:hover :global(svg) {
    transform: rotate(90deg);
  }
  .bubble-wrap {
    position: relative;
    display: flex;
    flex-shrink: 0;
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
    background: var(--danger);
    color: var(--danger-fg);
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
  @media (pointer: coarse), (max-width: 700px) {
    .rail {
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
    gap: 8px;
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
  .folder-tile:hover {
    background: color-mix(in srgb, var(--fc, var(--accent)) 40%, var(--bg-2));
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
    border-radius: 5px;
    object-fit: cover;
    display: grid;
    place-items: center;
    font-size: 8px;
    font-weight: 700;
    overflow: hidden;
  }
  .folder-body {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
  }
  .folder-name {
    width: 46px;
    background: transparent;
    border: none;
    border-bottom: 1px solid transparent;
    color: var(--text-muted);
    font-size: 9px;
    text-align: center;
    padding: 1px 0;
    outline: none;
  }
  .folder-name:hover {
    border-bottom-color: var(--border);
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
    animation: badge-pop 0.15s ease both;
  }
  .dropbar--in {
    width: 24px;
  }
  .rail.dragging .pill {
    cursor: grab;
  }
</style>
