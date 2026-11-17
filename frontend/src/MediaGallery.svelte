<script>
  // A conversation's Media tab: "Photos and videos" as a grid with month
  // headings, "Files" as a list, newest first, built from the core's media
  // index (api.mediaPage) so it reaches the whole history this device holds,
  // not only what the feed has loaded.
  //
  // Both are virtual: rows have fixed heights, so where everything goes is
  // arithmetic (lib/gallery.js), and only the rows within a screen of the
  // viewport are in the DOM. The row blocks are not keyed, so scrolling
  // reuses the same nodes with new contents rather than building new ones.
  //
  // Tiles show a picture only when this renderer already holds it decrypted
  // (the feed or the viewer opened it); otherwise a placeholder shaped by the
  // token's dimensions. A tile whose blob is not on this device asks the core
  // to fetch it while it is on screen, one at a time; on a billed connection
  // the core waits for a tap instead.
  import { onMount, untrack } from "svelte";
  import { SvelteMap } from "svelte/reactivity";
  import Icon from "./Icon.svelte";
  import Segmented from "./Segmented.svelte";
  import GalleryViewer from "./GalleryViewer.svelte";
  import { api, on } from "./lib/api.js";
  import { S, nameFor, fmtClock, jumpToMessage, openContextMenu, flash } from "./lib/state.svelte.js";
  import {
    cachedAttachment,
    onAttachmentCached,
    loadAttachment,
    copyImageToClipboard,
    saveImageSrc,
    hasAttachment,
    midTrunc,
    fmtBytes,
  } from "./lib/attachments.js";
  import { saveBlob } from "./lib/savefile.js";
  import { longpress } from "./lib/touch.js";
  import {
    PAGE,
    TAB_VISUAL,
    TAB_FILES,
    KIND_PHOTO,
    KIND_VIDEO,
    itemKey,
    mergePage,
    withoutMessage,
    liveItems,
    nextExpiry,
    columnsFor,
    buildRows,
    layout,
    visibleRange,
    needMore,
    tileRatio,
    durationOf,
    fmtDuration,
    fileSub,
    isVoice,
    FetchQueue,
  } from "./lib/gallery.js";

  let { channelId, onGoto = () => {} } = $props();

  let tab = $state(TAB_VISUAL);
  let items = $state([]);
  let next = $state("");
  let loading = $state(false);
  let failed = $state("");
  let loaded = $state(false); // the first page has answered
  let gen = 0;

  async function loadPage(reset) {
    const g = reset ? ++gen : gen;
    const ch = channelId;
    const t = tab;
    if (!reset && (loading || !next)) return;
    loading = true;
    failed = "";
    try {
      const page = await api.mediaPage(ch, t, reset ? "" : next, PAGE);
      if (g !== gen) return;
      items = reset ? page?.items || [] : mergePage(items, page?.items || []);
      next = page?.next || "";
      loaded = true;
    } catch (e) {
      if (g === gen) failed = String(e?.message || e);
    } finally {
      if (g === gen) loading = false;
    }
  }

  // A new conversation or tab starts from the top.
  $effect(() => {
    channelId;
    tab;
    untrack(() => {
      items = [];
      next = "";
      loaded = false;
      tileState.clear();
      queue?.close();
      queue = makeQueue();
      if (scroller) scroller.scrollTop = 0;
      scrollTop = 0;
      loadPage(true);
    });
  });

  // ---- staying current while open ----
  // A delete, an erase, an expiry or a hide removes the message's items at
  // once; anything that may have added or changed one refetches what is
  // loaded, a moment later, in one call.
  let refreshTimer = 0;
  function refreshSoon() {
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(async () => {
      const g = gen;
      try {
        const page = await api.mediaPage(channelId, tab, "", Math.max(PAGE, items.length));
        if (g !== gen) return;
        items = page?.items || [];
        next = page?.next || "";
      } catch {
        /* the next change or reopen tries again */
      }
    }, 400);
  }
  onMount(() => {
    const offMsg = on("message", (m) => {
      if (!m || m.channelId !== channelId) return;
      if (m.deleted || m.expired) items = withoutMessage(items, m.id);
      else if (hasAttachment(m.content || "") || items.some((it) => it.messageId === m.id)) refreshSoon();
    });
    const offHide = on("message-hidden", (h) => {
      if (!h || h.channelId !== channelId) return;
      if (h.hidden) items = withoutMessage(items, h.messageId);
      else refreshSoon();
    });
    // A picture the feed (or the viewer) has just decrypted: its tile can
    // draw it now. Batched to one redraw per frame.
    let pending = 0;
    const offCached = onAttachmentCached(() => {
      if (pending) return;
      pending = requestAnimationFrame(() => {
        pending = 0;
        cacheTick++;
      });
    });
    return () => {
      offCached();
      cancelAnimationFrame(pending);
      offMsg?.();
      offHide?.();
      clearTimeout(refreshTimer);
      clearTimeout(expiryTimer);
      clearTimeout(restTimer);
      queue?.close();
    };
  });

  // A disappearing item leaves the grid at its deadline even with the grid
  // open and nothing else happening.
  let nowSec = $state(Math.floor(Date.now() / 1000));
  let expiryTimer = 0;
  $effect(() => {
    const at = nextExpiry(items, nowSec);
    clearTimeout(expiryTimer);
    if (!at) return;
    expiryTimer = setTimeout(() => (nowSec = Math.floor(Date.now() / 1000)), Math.max(0, at * 1000 - Date.now()) + 50);
  });
  const shown = $derived(liveItems(items, nowSec));

  // ---- the virtual grid ----
  let scroller = $state(null);
  let width = $state(0);
  let viewH = $state(0);
  let scrollTop = $state(0);
  let raf = 0;
  function onScroll() {
    if (raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      if (scroller) scrollTop = scroller.scrollTop;
    });
  }
  let landscape = $state(window.innerWidth > window.innerHeight);
  const GAP = 2;
  const HEAD_H = 34;
  const FILE_H = 56;
  const cols = $derived(tab === TAB_VISUAL ? columnsFor(width, { phone: S.isMobile, landscape }) : 1);
  const tileW = $derived(cols > 1 ? Math.max(40, (width - GAP * (cols - 1)) / cols) : width);
  const rowH = $derived(tab === TAB_VISUAL ? tileW + GAP : FILE_H);
  const rows = $derived(buildRows(shown, cols));
  const lay = $derived(layout(rows, rowH, HEAD_H));
  // One screen of rows either side of the viewport is mounted.
  const range = $derived(visibleRange(lay.tops, lay.height, scrollTop, viewH || 600, viewH || 600));
  // Primitives, so a scroll that stays within the same rows changes nothing
  // downstream.
  const rStart = $derived(range[0]);
  const rEnd = $derived(range[1]);
  const mounted = $derived(rows.slice(rStart, rEnd).map((row, i) => ({ row, top: lay.tops[rStart + i] })));

  $effect(() => {
    if (loaded && needMore([rStart, rEnd], rows.length, next) && !loading) untrack(() => loadPage(false));
  });

  // ---- what is not on this device ----
  // itemKey -> asking | local | metered | missing. Per key, so one tile's
  // change redraws that tile and not the grid.
  const tileState = new SvelteMap();
  let queue = null;
  let cacheTick = $state(0); // bumped when the viewer has decrypted something
  function onScreen(it) {
    const i = rows.findIndex((r) => r.kind === "tiles" && r.items.includes(it));
    if (i < 0) return false;
    const top = lay.tops[i];
    return top + rowH > scrollTop && top < scrollTop + (viewH || 600);
  }
  function makeQueue() {
    return new FetchQueue({
      ensure: (it, allowMetered) => api.ensureMedia(it.channelId, it.blobId, allowMetered),
      stillWanted: (it) => onScreen(it),
      onState: (k, st) => tileState.set(k, st),
    });
  }
  // Asked once the grid has come to rest (150 ms without a scroll): a fling
  // passes thousands of tiles nobody looked at, and asking the core about
  // each one would cost the frames the fling needs.
  let restTimer = 0;
  $effect(() => {
    if (tab !== TAB_VISUAL || !queue) return;
    const lo = scrollTop;
    const hi = scrollTop + (viewH || 600);
    const rows_ = mounted;
    clearTimeout(restTimer);
    restTimer = setTimeout(() => {
      for (const { row, top } of rows_) {
        if (row.kind !== "tiles" || top + rowH <= lo || top >= hi) continue;
        for (const it of row.items) {
          if (tileState.has(itemKey(it)) || cachedAttachment(it.blobId)) continue;
          queue.push(it);
        }
      }
    }, 150);
  });

  // ---- actions ----
  let viewing = $state(-1); // index into the visual items, -1 = closed
  const visual = $derived(tab === TAB_VISUAL ? shown : []);
  function open(it) {
    const k = itemKey(it);
    // On a billed connection a tap is the go-ahead for this one.
    const st = tileState.get(k);
    if (st === "metered" || st === "missing") queue?.push(it, true);
    viewing = visual.findIndex((x) => itemKey(x) === k);
  }
  function goTo(it) {
    viewing = -1;
    onGoto(it);
    jumpToMessage(it.channelId, it.messageId);
  }
  async function save(it) {
    try {
      if (it.kind === KIND_PHOTO) {
        const src = await loadAttachment(it.channelId, it);
        const how = await saveImageSrc(src, it.name || `concord-${it.blobId.slice(0, 8)}.${it.subtype}`);
        if (how === "gallery") flash("Saved to your gallery", "success");
        else if (!how) flash("Couldn't save that image");
        cacheTick++;
      } else {
        const url = await api.fetchFile(it.channelId, it.blobId, it.keys, it.mime);
        const how = await saveBlob(it.name || "file", await (await fetch(url)).blob());
        if (!how) flash("Couldn't save that file");
      }
    } catch (e) {
      flash(`Couldn't save: ${e?.message || e}`);
    }
  }
  async function copy(it) {
    try {
      await copyImageToClipboard(await loadAttachment(it.channelId, it));
      cacheTick++;
      flash("Image copied", "success");
    } catch (e) {
      flash(`Couldn't copy image: ${e?.message || e}`);
    }
  }
  function menu(e, it) {
    openContextMenu(e, [
      { label: "Go to message", icon: "bubble", onClick: () => goTo(it) },
      { label: "Save", icon: "download", onClick: () => save(it) },
      it.kind === KIND_PHOTO && { label: "Copy image", icon: "copy", onClick: () => copy(it) },
    ]);
  }

  // A long-press's item, from the node it landed on (see the tile markup).
  const byKey = $derived(new Map(shown.map((it) => [itemKey(it), it])));
  function menuAt(e) {
    const it = byKey.get(e?.target?.closest?.("[data-k]")?.dataset.k);
    if (it) menu(e, it);
  }
  const who = (it) => nameFor(it.sender, it.senderName);
  const tileLabel = (it) =>
    it.spoiler
      ? "Spoiler"
      : `${it.kind === KIND_VIDEO ? "Video" : "Photo"} from ${who(it)}, ${fmtClock(it.sent)}`;
</script>

<svelte:window onresize={() => (landscape = window.innerWidth > window.innerHeight)} />

<div class="gal">
  <div class="gal-switch">
    <Segmented
      label="Show"
      size="sm"
      value={tab}
      options={[
        { value: TAB_VISUAL, label: "Photos and videos" },
        { value: TAB_FILES, label: "Files" },
      ]}
      onChange={(v) => (tab = v)}
    />
  </div>
  <div
    class="gal-scroll"
    bind:this={scroller}
    bind:clientWidth={width}
    bind:clientHeight={viewH}
    onscroll={onScroll}
    data-testid="gallery-scroll"
  >
    {#if loaded && !shown.length}
      <p class="gal-empty">
        {#if failed}
          Couldn't read this conversation's media: {failed}
        {:else if tab === TAB_VISUAL}
          No photos or videos here yet.
        {:else}
          No files here yet.
        {/if}
      </p>
    {:else if !loaded && failed}
      <p class="gal-empty">Couldn't read this conversation's media: {failed}</p>
    {/if}
    <div class="gal-space" style="height:{lay.height}px">
      {#each mounted as m}
        {#if m.row.kind === "head"}
          <div class="gal-head" style="transform:translateY({m.top}px);height:{HEAD_H}px">{m.row.label}</div>
        {:else if tab === TAB_VISUAL}
          <div class="gal-row" style="transform:translateY({m.top}px);height:{tileW}px;grid-template-columns:repeat({cols}, {tileW}px);gap:{GAP}px">
            <!-- Not keyed: scrolling hands the same tile nodes new items rather
                 than building new ones. Handlers find their item through
                 data-k, since an action's parameters are not updated. -->
            {#each m.row.items as it}
              {@const st = tileState.get(itemKey(it)) || ""}
              {@const src = it.kind === KIND_PHOTO && cacheTick >= 0 ? cachedAttachment(it.blobId) : ""}
              <button
                type="button"
                class="tile"
                class:spoiler={it.spoiler}
                aria-label={tileLabel(it)}
                data-msg={it.messageId}
                data-k={itemKey(it)}
                onclick={() => open(it)}
                oncontextmenu={(e) => menu(e, it)}
                use:longpress={{ handler: menuAt }}
              >
                {#if it.kind === KIND_PHOTO && src}
                  <img {src} alt="" decoding="async" draggable="false" />
                {:else}
                  <span class="ph" style="{tileRatio(it) >= 1 ? 'width' : 'height'}:70%;aspect-ratio:{tileRatio(it)}">
                    {#if it.kind === KIND_VIDEO}<Icon name="play" size={22} />{:else}<Icon name="image" size={18} />{/if}
                  </span>
                {/if}
                {#if it.spoiler}<span class="tag">Spoiler</span>{/if}
                {#if it.kind === KIND_VIDEO}
                  <span class="dur">{fmtDuration(durationOf(it.blobId)) || fmtBytes(it.size)}</span>
                {/if}
                {#if st === "asking"}
                  <span class="state"><span class="spin"></span></span>
                {:else if st === "metered"}
                  <span class="state">Tap to load</span>
                {:else if st === "missing"}
                  <span class="state">Not reachable</span>
                {/if}
              </button>
            {/each}
          </div>
        {:else}
          {@const it = m.row.items[0]}
          <button
            type="button"
            class="frow"
            style="transform:translateY({m.top}px);height:{FILE_H}px"
            data-k={itemKey(it)}
            onclick={() => goTo(it)}
            oncontextmenu={(e) => menu(e, it)}
            use:longpress={{ handler: menuAt }}
          >
            <span class="fic"><Icon name={isVoice(it) ? "mic" : "attach"} size={16} /></span>
            <span class="ftext">
              <span class="ftitle"><bdi>{isVoice(it) ? "Voice message" : midTrunc(it.name, 40)}</bdi></span>
              <span class="fsub">{isVoice(it) ? fmtBytes(it.size) : fileSub(it)} · {who(it)} · {fmtClock(it.sent)}</span>
            </span>
          </button>
        {/if}
      {/each}
    </div>
    {#if loading && loaded}<p class="gal-more">Loading…</p>{/if}
  </div>
</div>

{#if viewing >= 0 && visual[viewing]}
  <GalleryViewer
    item={visual[viewing]}
    index={viewing}
    count={visual.length}
    onClose={() => (viewing = -1)}
    onGoto={() => goTo(visual[viewing])}
    onStep={(d) => (viewing = Math.max(0, Math.min(visual.length - 1, viewing + d)))}
    onLoaded={() => cacheTick++}
  />
{/if}

<style>
  .gal {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .gal-switch {
    padding: var(--sp-2) 8px;
  }
  .gal-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    position: relative;
    contain: strict;
  }
  .gal-space {
    position: relative;
  }
  .gal-head,
  .gal-row,
  .frow {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    will-change: transform;
  }
  .gal-head {
    display: flex;
    align-items: flex-end;
    padding: 0 10px 6px;
    box-sizing: border-box;
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .gal-row {
    display: grid;
  }
  .tile {
    position: relative;
    display: grid;
    place-items: center;
    width: 100%;
    height: 100%;
    padding: 0;
    border-radius: 0;
    overflow: hidden;
    background: var(--bg-3);
    color: var(--text-faint);
    contain: strict;
  }
  .tile img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }
  .tile.spoiler img {
    filter: blur(18px);
  }
  .ph {
    display: grid;
    place-items: center;
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--bg-1) 70%, transparent);
  }
  .tag,
  .dur,
  .state {
    position: absolute;
    font-size: var(--fs-small);
    color: #fff;
    background: rgba(0, 0, 0, 0.55);
    border-radius: 999px;
    padding: 1px 7px;
    pointer-events: none;
  }
  .tag {
    top: 6px;
    left: 6px;
    font-weight: 700;
    letter-spacing: 0.6px;
  }
  .dur {
    right: 6px;
    bottom: 6px;
    font-variant-numeric: tabular-nums;
  }
  .state {
    left: 6px;
    bottom: 6px;
  }
  .spin {
    display: block;
    width: 12px;
    height: 12px;
    border: 2px solid rgba(255, 255, 255, 0.35);
    border-top-color: #fff;
    border-radius: 50%;
    animation: gal-rot 0.8s linear infinite;
  }
  @keyframes gal-rot {
    to {
      transform: rotate(360deg);
    }
  }
  .frow {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0 16px;
    box-sizing: border-box;
    text-align: start;
    color: var(--text);
    border-radius: 0;
  }
  @media (pointer: fine) {
    .frow:hover {
      background: color-mix(in srgb, var(--bg-3) 60%, transparent);
    }
  }
  .fic {
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    flex: none;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .ftext {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .ftitle {
    font-size: var(--fs-ui);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .fsub {
    font-size: var(--fs-small);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gal-empty,
  .gal-more {
    margin: 16px 20px;
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
</style>
