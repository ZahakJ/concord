<script>
  // The guilds, in one of two arrangements (the device-local `navModel` pref,
  // decided in App.svelte):
  //
  //   layout="strip" — the sidebar's SHELF (the default): Messages fixed at
  //     the leading end, Add at the trailing end, and between them every guild
  //     in a row that scrolls sideways, in the one order the switcher uses too
  //     (pins, then the rail order — lib/rail.js sharedOrder), so a tile never
  //     moves because it was read. The pointer or the arrow keys move a
  //     highlight along it; only a click or Enter switches guild.
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
  import OwnerMark from "./OwnerMark.svelte";
  import { ownsPlace } from "./lib/ownermark.js";
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
    seedOrder,
    sharedOrder,
    shelfView,
    edgeNews,
    revealLeft,
    snapLeft,
    wheelNotch,
    nextStop,
    glideStep,
  } from "./lib/rail.js";
  import { guildMenu, folderMenu, renameFolderTo, stripPins, pinAt } from "./lib/guildmenu.js";
  import { listNav } from "./lib/listnav.js";
  import { reducedMotion } from "./lib/motion.js";
  import { openGuildSwitcher } from "./lib/guildswitch.svelte.js";
  import { tileColor } from "./lib/guildaccent.js";
  import { playFlyby } from "./lib/sounds.js";
  import { longpress } from "./lib/touch.js";
  import { MOM } from "./lib/momentsunseen.svelte.js";
  import { tooltip } from "./lib/tooltip.js";
  import FxLayer from "./FxLayer.svelte";
  import { tick } from "svelte";
  import { RADAR, guildLiveSet } from "./lib/radar.svelte.js";
  import { failedInGuild } from "./lib/outbox.svelte.js";

  let { layout = "rail" } = $props();
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

  // Keep the persisted layout in step with the live guild set. Guilds it has
  // not placed yet are appended in seedOrder, never in the order the core
  // happened to list them (lib/rail.js).
  $effect(() => {
    const ids = seedOrder(allGuilds);
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
    if (MOM.unseen[gg.id]) parts.push("new moments");
    if (ownsPlace(gg)) parts.push("you own this guild");
    return parts.join(" · ");
  }

  // The strip's caption: the name and what is waiting there, without the
  // ownership note (the description keeps it for a screen reader).
  function capLabel(gg) {
    const m = marks(gg);
    const parts = [gg.name];
    if (m.count) parts.push(plural(m.count, "mention"));
    else if (m.dot) parts.push("unread");
    if (m.muted) parts.push("muted");
    if (MOM.unseen[gg.id]) parts.push("new moments");
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

  // ————————————————— the strip: a shelf of every guild —————————————————
  // Every guild, pins first, then the rail order — never reordered by what is
  // unread, so a hand can learn where a guild sits.
  const order = $derived(sharedOrder(guildIdsInLayout(S.rail).filter((id) => guildById.has(id)), stripPins()));
  const shelfGuilds = $derived(order.map((id) => guildById.get(id)));
  const curIdx = $derived(order.indexOf(S.activeGuildId));

  // The shelf's geometry. Every tile has the same pitch, so where a tile is
  // and which ones are in view is arithmetic on a few numbers measured when
  // the shelf changes size or contents — a scroll reads scrollLeft and nothing
  // else, and touches the DOM only when the answer changes.
  // The faded band at a scrollable end: wide enough to hold that end's news
  // chip, so a tile counted as in view (or revealed by the keys) is never
  // under it. --shelf-fade in the CSS is the same number.
  const FADE = 36;
  let shelf = $state(null);
  let box = { lead: 0, pitch: 32, size: 28 };
  let seen = $state({ first: 0, last: -1 });
  let fadeL = $state(false);
  let fadeR = $state(false);
  function geo() {
    return {
      ...box,
      left: shelf.scrollLeft,
      width: shelf.clientWidth,
      max: Math.max(0, shelf.scrollWidth - shelf.clientWidth),
      fade: FADE,
    };
  }
  function measure() {
    if (!shelf) return;
    const t = shelf.querySelectorAll(":scope > .tile-wrap");
    if (t[0]) box = { lead: t[0].offsetLeft, size: t[0].offsetWidth, pitch: t[1] ? t[1].offsetLeft - t[0].offsetLeft : t[0].offsetWidth + 4 };
    observe();
  }
  function observe() {
    if (!shelf) return;
    const g = geo();
    const v = shelfView(order.length, g);
    if (v.first !== seen.first || v.last !== seen.last) seen = v;
    const l = g.left > 0.5;
    const r = g.left < g.max - 0.5;
    if (l !== fadeL) fadeL = l;
    if (r !== fadeR) fadeR = r;
    relight();
  }
  let frame = 0;
  function onShelfScroll() {
    if (!frame) frame = requestAnimationFrame(() => ((frame = 0), observe()));
  }
  $effect(() => {
    if (!shelf) return;
    const ro = new ResizeObserver(measure);
    ro.observe(shelf);
    return () => ro.disconnect();
  });
  // What the faded ends are holding back: a number for mentions, else a dot.
  const news = $derived(edgeNews(order, seen, (id) => marks(guildById.get(id))));

  const edgeCap = (n, side) =>
    n.count ? `${plural(n.count, "mention")} further ${side}` : `Unread further ${side}`;
  // The chip's click: glide to the nearest guild past that edge with news.
  function toNews(dir) {
    const hasNews = (id) => {
      const m = marks(guildById.get(id));
      return m.count > 0 || (!m.muted && m.dot);
    };
    const idx = dir < 0
      ? order.slice(0, seen.first).findLastIndex(hasNews)
      : order.findIndex((id, i) => i > seen.last && hasNews(id));
    if (idx >= 0) reveal(idx);
  }

  const scrollBehavior = () => (reducedMotion() ? "instant" : "smooth");
  function reveal(i, behavior = scrollBehavior()) {
    if (!shelf || i < 0) return;
    // Clear of the band by a little more than a lifted tile's focus ring.
    const to = revealLeft(i, { ...geo(), fade: FADE + 14 });
    if (Math.abs(to - shelf.scrollLeft) > 0.5) shelf.scrollTo({ left: to, behavior });
  }
  // The open guild is brought into view when the shelf first draws (at once)
  // and whenever you change guild by any road — the switcher, the palette, a
  // link, the keyboard (gliding, unless motion is reduced).
  let shown = false;
  $effect(() => {
    const i = curIdx;
    void order.length;
    if (!shelf) return;
    tick().then(() => {
      measure();
      reveal(i, shown ? scrollBehavior() : "instant");
      shown = true;
    });
  });

  // A vertical wheel moves the shelf sideways. A sideways gesture (a
  // trackpad's two-finger swipe) is left to the platform, which brings its own
  // momentum and lands on the CSS snap. Everything else is the shelf's own
  // glide: ONE animation frame loop easing scrollLeft toward an accumulated
  // target (lib/rail.js glideStep), so a run of notches carries its speed
  // instead of restarting a smooth scroll from rest on every event — that
  // restart was a 66ms dead start and a flat spot between notches. A mouse
  // notch is exactly one tile (wheelNotch); a trackpad's vertical stream
  // moves in proportion. Snap is off while it glides (it would pull each step
  // back) and the glide lands on the nearest tile as drawn once the wheel is
  // quiet. Nothing here selects anything: only a click switches guild.
  function stopsOf(node) {
    const base = node.getBoundingClientRect().left - node.scrollLeft + FADE;
    return [...node.children].map((w) => w.getBoundingClientRect().left - base);
  }
  function shelfWheel(node) {
    let target = null;
    let raf = 0;
    let idle = 0;
    let quiet = true;
    const maxOf = () => Math.max(0, node.scrollWidth - node.clientWidth);
    function frame() {
      const next = glideStep(node.scrollLeft, target);
      node.scrollLeft = next;
      if (Math.abs(node.scrollLeft - target) > 1) {
        raf = requestAnimationFrame(frame);
        return;
      }
      raf = 0;
      if (quiet) finish();
    }
    function finish() {
      target = null;
      node.classList.remove("wheeling");
      observe();
    }
    function stop() {
      if (target === null) return;
      cancelAnimationFrame(raf);
      clearTimeout(idle);
      raf = 0;
      finish();
    }
    function go(to) {
      target = to;
      node.classList.add("wheeling");
      if (reducedMotion()) {
        node.scrollLeft = to;
        if (quiet) finish();
        return;
      }
      if (!raf) frame();
    }
    function onWheel(e) {
      if (e.ctrlKey) return;
      if (Math.abs(e.deltaY) <= Math.abs(e.deltaX)) return stop();
      const max = maxOf();
      if (max <= 0) return;
      const from = target ?? node.scrollLeft;
      const dir = Math.sign(e.deltaY);
      const to = wheelNotch(e.deltaY, e.deltaMode)
        ? nextStop(from, stopsOf(node), dir, max, box.pitch / 2)
        : Math.max(0, Math.min(max, from + e.deltaY * (e.deltaMode === 2 ? node.clientWidth : 1)));
      if (to === from && target === null) return;
      e.preventDefault();
      quiet = false;
      clearTimeout(idle);
      idle = setTimeout(() => {
        quiet = true;
        // Land on the nearest tile; a notch's target already is one.
        go(snapLeft(target ?? node.scrollLeft, stopsOf(node), maxOf()));
      }, 140);
      go(to);
    }
    // Anything else that moves the row takes over from the glide.
    const others = ["pointerdown", "touchstart", "keydown"];
    node.addEventListener("wheel", onWheel, { passive: false });
    for (const t of others) node.addEventListener(t, stop, { passive: true });
    return {
      destroy() {
        cancelAnimationFrame(raf);
        clearTimeout(idle);
        node.removeEventListener("wheel", onWheel);
        for (const t of others) node.removeEventListener(t, stop);
      },
    };
  }

  // The caption: the lit tile's name, from the pointer or the keyboard. It
  // keeps its last text and place while it fades out, so leaving the strip
  // does not snap it back to the start. Recomputed after the shelf moves
  // under a still pointer, so a wheel glide walks it along.
  let navEl = $state(null);
  let peek = $state(null);
  let capText = $state("");
  let capX = $state(0);
  let capEl = $state(null);
  let ptr = null;
  let keyLit = false;
  // Slides between tiles, but appears in place: from nothing it fades in where
  // it is wanted instead of travelling from wherever it last was.
  let glide = $state(false);
  function light(tile) {
    if (!tile || !navEl) return;
    const r = tile.getBoundingClientRect();
    const n = navEl.getBoundingClientRect();
    const text = tile.dataset.cap || tile.getAttribute("aria-label") || "";
    const x = Math.round(r.left + r.width / 2 - n.left);
    if (peek && peek.text === text && peek.x === x) return;
    glide = !!peek;
    peek = { text, x };
    capText = text;
  }
  // Centred under the tile, but kept inside the sidebar: the caption's width
  // is only known once its text is drawn, so the clamp runs after.
  $effect(() => {
    if (!peek || !capEl || !navEl) return;
    const half = capEl.offsetWidth / 2;
    const w = navEl.clientWidth;
    const x = Math.round(Math.max(half + 4, Math.min(w - half - 4, peek.x)));
    if (x !== capX) capX = x;
  });
  function onStripPointer(e) {
    if (e.pointerType !== "mouse") return;
    ptr = { x: e.clientX, y: e.clientY };
    const t = e.target.closest?.(".tile, .edge");
    if (t) light(t);
    else if (!keyLit) peek = null;
  }
  function onStripLeave() {
    ptr = null;
    if (!keyLit) peek = null;
  }
  function onStripFocus(e) {
    const t = e.target.closest?.(".tile");
    keyLit = !!t && t.matches(":focus-visible");
    if (keyLit) light(t);
  }
  function onStripBlur(e) {
    if (navEl?.contains(e.relatedTarget)) return;
    keyLit = false;
    if (!ptr) peek = null;
  }
  // After the shelf moves: the tile under a still pointer has changed, and a
  // keyboard-lit tile has moved.
  function relight() {
    if (keyLit && document.activeElement?.closest?.(".strip")) return light(document.activeElement);
    if (!ptr) return;
    const t = document.elementFromPoint(ptr.x, ptr.y)?.closest?.(".strip .tile");
    if (t) light(t);
  }

  // Arrow keys move the highlight (focus) along the whole strip — Messages,
  // the guilds, Add — as one Tab stop; the shelf glides the tile clear of the
  // faded edge. Enter or Space is the button's own click.
  function revealRow(row) {
    const wrap = row.closest(".shelf > .tile-wrap");
    if (wrap) reveal([...shelf.children].indexOf(wrap));
  }

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

  // Drag on the strip: only pins reorder here — drop a pinned tile among the
  // pins to move it, drop an unpinned one to pin it at that spot. Folders and
  // the full order are the switcher's.
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
       landmark whichever arrangement is chosen, and one Tab stop: the arrows
       walk it. -->
  <nav
    class="strip"
    class:phone
    aria-label="Guilds"
    bind:this={navEl}
    use:listNav={{ rows: ".tile", current: ".tile.current", axis: "x", name: "aria-label", reveal: revealRow }}
    onpointermove={onStripPointer}
    onpointerleave={onStripLeave}
    onfocusin={onStripFocus}
    onfocusout={onStripBlur}
  >
    <span class="tile-wrap">
      <button
        class="tile msg"
        class:current={inDMs}
        data-cap={dmMarks.count ? `Messages · ${plural(dmMarks.count, "unread message")}` : "Messages"}
        aria-label={dmMarks.count ? `Messages · ${plural(dmMarks.count, "unread message")}` : "Messages"}
        aria-current={inDMs ? "page" : undefined}
        onclick={messagesClick}
      >
        <Icon name="bubble" size={phone ? 20 : 16} />
      </button>
      {#if dmMarks.count}<span class="t-badge"><Badge count={dmMarks.count} max={9} ring="var(--bg-1)" /></span>
      {:else if dmMarks.dot}<span class="t-dot"><Badge dot /></span>{/if}
    </span>
    {#if shelfGuilds.length}
      <div class="shelf-box">
        <div
          class="shelf"
          class:fade-l={fadeL}
          class:fade-r={fadeR}
          bind:this={shelf}
          onscroll={onShelfScroll}
          use:shelfWheel
        >
          {#each shelfGuilds as gg, i (gg.id)}
            {@const m = marks(gg)}
            <span
              class="tile-wrap"
              class:pre={curIdx >= 0 && i < curIdx}
              class:post={curIdx >= 0 && i > curIdx}
              class:drop-before={sdrop?.id === gg.id && !sdrop.after}
              class:drop-after={sdrop?.id === gg.id && sdrop.after}
            >
              <button
                class="tile"
                class:current={gg.id === S.activeGuildId}
                class:muted={m.muted}
                class:gone={!!gg.evicted}
                class:hasicon={!!gg.icon}
                class:moments={MOM.unseen[gg.id] > 0}
                style={gg.icon ? undefined : faceStyle(gg)}
                data-cap={capLabel(gg)}
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
        </div>
        <!-- News past a faded end: a quiet dot and a chevron on the faded
             ground, in your accent when something there is aimed at you. A
             number here read as the half-hidden tile's own count; the count is
             in its caption and label instead. A click glides to that guild. -->
        {#if fadeL && (news.left.count || news.left.dot)}
          <button
            class="edge l"
            class:you={news.left.count > 0}
            tabindex="-1"
            data-cap={edgeCap(news.left, "left")}
            aria-label={edgeCap(news.left, "left")}
            onclick={() => toNews(-1)}
          >
            <span class="chev"><Icon name="chevron" size={9} /></span><span class="edot"></span>
          </button>
        {/if}
        {#if fadeR && (news.right.count || news.right.dot)}
          <button
            class="edge r"
            class:you={news.right.count > 0}
            tabindex="-1"
            data-cap={edgeCap(news.right, "right")}
            aria-label={edgeCap(news.right, "right")}
            onclick={() => toNews(1)}
          >
            <span class="edot"></span><span class="chev"><Icon name="chevron" size={9} /></span>
          </button>
        {/if}
      </div>
    {:else}
      <span class="shelf-box"></span>
    {/if}
    <span class="tile-wrap">
      {#if allGuilds.length}
        <!-- Add: the same three doors the classic rail's + has. Instant meeting
             lived only inside the switcher's ⋯ in this arrangement, which is
             three clicks and an unlabelled icon away from a thing people
             start on a whim. -->
        <button
          class="tile more add"
          data-cap="Add a guild or start a meeting"
          aria-label="Add a guild"
          aria-haspopup="menu"
          onclick={addMenu}
        >
          <Icon name="plus" size={phone ? 18 : 14} />
        </button>
      {:else}
        <!-- No guilds yet: the strip's last tile is the way to the first one. -->
        <button
          class="tile more"
          data-cap="Create or join a guild"
          aria-label="Create or join a guild"
          aria-haspopup="dialog"
          onclick={openSwitcher}
        >
          <Icon name="plus" size={phone ? 18 : 14} />
        </button>
      {/if}
    </span>
    <!-- The highlighted tile's name: under the pointer or the keyboard's
         highlight, a caption that slides along under the strip to whichever
         tile is lit, so a row of look-alike marks can be read. It floats over
         the row below rather than taking room, so nothing reflows. -->
    {#if !phone}
      <span class="caption" bind:this={capEl} class:on={!!peek} class:glide style:--cx="{capX}px" aria-hidden="true">{peek?.text ?? capText}</span>
    {/if}
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
        <!-- The Messages glyph, as on the strip and the phone's tab bar. It was
             the app's own mark, so the rail's first tile read as a logo rather
             than as the place your direct messages are. -->
        <Icon name="bubble" size={20} />
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
            class:moments={MOM.unseen[sv.id] > 0}
            class:combine={dropHint?.k === "combine" && dropHint.id === sv.id}
            style={sv.icon ? undefined : faceStyle(sv)}
            use:tooltip={{ ...railTip, text: tileLabel(sv) }}
            aria-label={sv.name}
            aria-description={ownsPlace(sv) ? "you own this guild" : undefined}
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
          {#if ownsPlace(sv)}<span class="r-own"><OwnerMark size={10} decorative /></span>{/if}
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
                    class:moments={MOM.unseen[gg.id] > 0}
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
  /* ————————————————— the strip: a shelf ————————————————— */
  /* 46px: the 28px tiles with room above for a lifted tile's count and below
     for the unread dot, inside the shelf's scroll box (which clips both axes
     once it scrolls one). */
  .strip {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    height: 46px;
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
  /* Sized by its tiles, shrinking only when they overflow: a short shelf
     keeps Add right after its last guild instead of across an empty gap. */
  .shelf-box {
    position: relative;
    flex: 0 1 auto;
    min-width: 0;
    align-self: stretch;
    display: flex;
  }
  /* The row of guilds. It scrolls sideways and snaps to tiles; its ends fade
     where more tiles wait, so a cut-off tile reads as "more this way" rather
     than as a broken edge. The side padding is room for the outermost tiles
     to ease aside without changing what can scroll. */
  .shelf {
    --shelf-fade: 36px;
    position: relative;
    flex: 0 1 auto;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 0 var(--sp-1);
    overflow-x: auto;
    overflow-y: hidden;
    overscroll-behavior-x: contain;
    scroll-snap-type: x mandatory;
    scroll-padding-inline: var(--shelf-fade);
    scrollbar-width: none;
  }
  .shelf::-webkit-scrollbar {
    display: none;
  }
  .shelf:global(.wheeling) {
    scroll-snap-type: none;
  }
  .shelf.fade-l {
    mask-image: linear-gradient(to right, transparent, black var(--shelf-fade));
  }
  .shelf.fade-r {
    mask-image: linear-gradient(to left, transparent, black var(--shelf-fade));
  }
  .shelf.fade-l.fade-r {
    mask-image: linear-gradient(
      to right,
      transparent,
      black var(--shelf-fade),
      black calc(100% - var(--shelf-fade)),
      transparent
    );
  }
  .shelf > .tile-wrap {
    scroll-snap-align: start;
  }
  /* What the hidden end holds: a count or a dot, with a chevron saying which
     way (next to a tile a bare number read as that tile's own count). It
     sits on a band of the sidebar's ground the full height of the shelf,
     outside the mask so it stays crisp, and the band covers the half-faded
     tile's own marks, which otherwise peeked out above it. */
  .edge {
    position: absolute;
    top: 0;
    bottom: 0;
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 0;
    border-radius: 0;
    color: var(--text-muted);
    animation: edge-in var(--dur-standard) var(--ease-out) both;
  }
  @keyframes edge-in {
    from {
      opacity: 0;
    }
  }
  /* Its own ground: solid under the chip, then a short fade, so whatever
     tile is half-hidden there is not what the dot appears to belong to. */
  .edge.l {
    left: 0;
    padding: 0 14px 0 4px;
    background: linear-gradient(to right, var(--bg-1) 62%, transparent);
  }
  .edge.r {
    right: 0;
    padding: 0 4px 0 14px;
    background: linear-gradient(to left, var(--bg-1) 62%, transparent);
  }
  @media (pointer: fine) {
    .edge:hover {
      color: var(--text);
    }
  }
  .edot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--text);
  }
  .edge.you .edot {
    background: var(--accent);
  }
  .edge.l .chev {
    rotate: 180deg;
  }
  .chev {
    display: flex;
  }
  /* The lit tile's name, floating just under the strip over the row below. */
  .caption {
    position: absolute;
    top: 100%;
    left: 0;
    z-index: var(--z-raised);
    max-width: 220px;
    padding: 2px var(--sp-2);
    border-radius: var(--radius-sm);
    background: var(--bg-elevated);
    box-shadow: var(--shadow-pop);
    color: var(--text);
    font-size: var(--fs-small);
    line-height: 1.5;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    pointer-events: none;
    opacity: 0;
    translate: calc(var(--cx) - 50%) 2px;
    transition: opacity var(--dur-quick) var(--ease-out);
  }
  .caption.on {
    opacity: 1;
  }
  .caption.glide {
    transition:
      opacity var(--dur-quick) var(--ease-out),
      translate var(--dur-move) var(--ease-out);
  }

  /* A tile and its marks. The WRAP carries the position — the lift and the
     ease aside — so the count and the dot travel with the tile; the BUTTON
     carries the scale, so the marks never scale with it and stay crisp. Both
     are transform-only (translate, scale): nothing here changes layout, and
     the room for them is reserved (the shelf's padding, the strip's height). */
  .tile-wrap {
    position: relative;
    display: flex;
    flex: none;
    translate: var(--tx, 0px) var(--ty, 0px);
    transition: translate var(--dur-move) var(--ease-out);
  }
  /* The open guild sits a little larger, so its neighbours make room. */
  .tile-wrap.pre {
    --tx: calc(-1 * var(--aside-x));
  }
  .tile-wrap.post {
    --tx: var(--aside-x);
  }
  @media (hover: hover) and (pointer: fine) {
    .tile-wrap:hover {
      --ty: var(--shelf-lift-y);
      transition: translate var(--dur-standard) var(--ease-lift);
    }
    .tile-wrap:hover > .tile:not(.current) {
      --ts: var(--shelf-lift-scale);
      transition:
        scale var(--dur-standard) var(--ease-lift),
        filter var(--dur-quick) var(--ease-out);
    }
  }
  /* The keyboard's highlight is the pointer's: the same lift, plus the ring. */
  .tile-wrap:has(> .tile:focus-visible) {
    --ty: var(--shelf-lift-y);
    transition: translate var(--dur-standard) var(--ease-lift);
  }
  .tile-wrap > .tile:focus-visible:not(.current) {
    --ts: var(--shelf-lift-scale);
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
    scale: var(--ts, 1);
    transition:
      scale var(--dur-move) var(--ease-out),
      filter var(--dur-quick) var(--ease-out);
  }
  .tile.current {
    --ts: var(--shelf-current-scale);
    transition:
      scale var(--dur-standard) var(--ease-lift),
      filter var(--dur-quick) var(--ease-out);
  }
  .tile:active {
    scale: calc(var(--ts, 1) * var(--press-scale));
    transition: scale var(--dur-quick) var(--ease-out);
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
  /* The one "you are here" mark: a 2px ink ring INSIDE the tile, on a 1px gap
     in the sidebar's colour, so nothing reaches into the gaps between tiles.
     Drawn over the icon (::after), which an inset shadow is not. */
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
  /* Moments nobody here has watched yet: a thin ring in your accent just
     outside the tile, on a 1px gap — outside, so it never meets the "you are
     here" ring drawn inside it. */
  .tile.moments {
    overflow: visible;
    box-shadow:
      0 0 0 1px var(--bg-1),
      0 0 0 2.5px var(--accent);
  }
  .pill.moments {
    box-shadow:
      0 0 0 1px var(--bg-1),
      0 0 0 2.5px var(--accent);
  }
  .tile.moments .icon {
    border-radius: inherit;
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
     a dot just under the tile. One mark per tile, never two in one spot. A
     mark arrives once (Badge scales it in) and then holds still. */
  .t-badge {
    position: absolute;
    top: -5px;
    right: -6px;
    pointer-events: none;
    display: flex;
  }
  /* A guild you own, on the rail's larger tiles: a small filled crown cut
     into the bottom-left corner (OwnerMark). The count has the top-right,
     the unread dot the left edge and the event/failed mark the bottom-right,
     so none share a spot. The 28px strip tiles carry no owner mark at all. */
  .r-own {
    position: absolute;
    left: -3px;
    bottom: -3px;
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--bg-1);
    pointer-events: none;
    /* Quiet until you point at the tile: with many guilds of your own the
       column was a stack of crowns. Ownership is a fact, not news. */
    opacity: 0.55;
    transition: opacity var(--dur-quick) var(--ease-out);
  }
  .bubble-wrap:hover .r-own,
  .bubble-wrap:has(> .pill:focus-visible) .r-own {
    opacity: 1;
  }
  .t-dot {
    position: absolute;
    left: 50%;
    bottom: -8px;
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
  /* Phone: 40px tiles in 48px hit areas; the shelf swipes sideways with the
     same snap. No hover here, so no lift: a press sinks the tile instead. */
  .strip.phone {
    height: 60px;
    gap: var(--sp-2);
    padding: 0 var(--sp-3);
  }
  .strip.phone .shelf {
    gap: var(--sp-2);
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
  .divider {
    width: 24px;
    height: 1px;
    background: var(--hairline);
    flex-shrink: 0;
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
