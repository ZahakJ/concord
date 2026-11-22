<script>
  // A conversation's media panel: what was shared in it, in four kinds —
  // photos and videos as a square grid, files, links and voice messages as
  // rows — newest first under month headings. Photos, videos, files and
  // voice come from the core's media index (api.mediaPage), so they reach the
  // whole history this device holds, not only what the feed has loaded; links
  // come from a search of the conversation.
  //
  // Everything is virtual: rows have fixed heights, so where everything goes
  // is arithmetic (lib/gallery.js), and only the rows within a screen of the
  // viewport are in the DOM. The row blocks are not keyed, so scrolling
  // reuses the same nodes with new contents rather than building new ones.
  //
  // Tiles show a picture only when this renderer already holds it decrypted
  // (the feed or the viewer opened it); until then a shimmering square. A
  // tile whose blob is not on this device asks the core to fetch it while it
  // is on screen, one at a time; on a billed connection the core waits for a
  // tap instead.
  //
  // The panel: a header naming the conversation with its counts, a segmented
  // switch whose thumb slides, a month heading that stays at the top while
  // its month scrolls under it, and on a long grid a scrubber to jump by
  // date. Pinch (or Ctrl + wheel) changes the number of columns. Long-press,
  // drag across tiles, or Select, picks several items for Save, Forward and
  // Go to message. On a desktop the panel opens out to a large view.
  import { onMount, untrack, tick } from "svelte";
  import { SvelteMap, SvelteSet } from "svelte/reactivity";
  import Icon from "./Icon.svelte";
  import EmptyState from "./EmptyState.svelte";
  import GalleryViewer from "./GalleryViewer.svelte";
  import { api, on } from "./lib/api.js";
  import { S, nameFor, fmtClock, jumpToMessage, openContextMenu, flash } from "./lib/state.svelte.js";
  import { humanError } from "./lib/errors.js";
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
  import { UNSIGNED_LABEL, UNSIGNED_TIP } from "./lib/unsigned.js";
  import { tooltip } from "./lib/tooltip.js";
  import { cardFor } from "./lib/linkcard.js";
  import { extractLinks } from "./lib/embeds.js";
  import { linkDomain } from "./lib/regdomain.js";
  import {
    PAGE,
    PAGE_MAX,
    TAB_VISUAL,
    TAB_FILES,
    KIND_PHOTO,
    KIND_VIDEO,
    itemKey,
    mergePage,
    mergeHead,
    unsignedItem,
    withoutMessage,
    liveItems,
    nextExpiry,
    columnsFor,
    buildRows,
    layout,
    visibleRange,
    needMore,
    durationOf,
    fmtDuration,
    isVoice,
    FetchQueue,
  } from "./lib/gallery.js";

  let { channelId, onGoto = () => {} } = $props();

  // ---- the four kinds ----
  const TABS = [
    { id: "visual", short: "Photos", label: "Photos and videos", icon: "image" },
    { id: "files", short: "Files", label: "Files", icon: "attach" },
    { id: "links", short: "Links", label: "Links", icon: "link" },
    { id: "voice", short: "Voice", label: "Voice", icon: "mic" },
  ];
  let tab = $state("visual");
  const tabIndex = $derived(TABS.findIndex((t) => t.id === tab));
  const visualTab = $derived(tab === "visual");
  const coreTab = (t) => (t === "visual" ? TAB_VISUAL : TAB_FILES);
  let tabsEl = $state(null);
  function tabKey(e) {
    const d = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (!d) return;
    e.preventDefault();
    const i = (tabIndex + d + TABS.length) % TABS.length;
    tab = TABS[i].id;
    tabsEl?.querySelectorAll('[role="radio"]')[i]?.focus();
  }

  // ---- where it is ----
  const place = $derived.by(() => {
    for (const g of S.guilds) {
      const c = g.channels.find((x) => x.id === channelId);
      if (c) return g.kind === "dm" ? g.name : `#${c.name}`;
    }
    return "This conversation";
  });
  let counts = $state(null);
  function loadCounts() {
    const ch = channelId;
    api.mediaCounts(ch).then(
      (c) => ch === channelId && (counts = c),
      () => {},
    );
  }
  const plural = (n, one, many) => `${n.toLocaleString()} ${n === 1 ? one : many}`;
  const countLine = $derived.by(() => {
    if (tab === "links") return linksLoaded ? plural(linkItems.length, "link", "links") : "";
    if (!counts) return "";
    const parts = [];
    if (counts.visual) parts.push(plural(counts.visual, "photo or video", "photos and videos"));
    if (counts.files) parts.push(plural(counts.files, "file", "files"));
    return parts.join(" · ") || "Nothing shared yet";
  });

  // ---- paging ----
  let items = $state([]);
  let next = $state("");
  let loading = $state(false);
  let failed = $state("");
  let loaded = $state(false); // the first page has answered
  let gen = 0;

  async function loadPage(reset) {
    if (tab === "links") return reset ? loadLinks() : undefined;
    const g = reset ? ++gen : gen;
    const ch = channelId;
    const t = coreTab(tab);
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

  // Links: the conversation's messages that carry one, each link a row with
  // its site and, when the sender's device attached a card, its title.
  let linkItems = $state([]);
  let linksLoaded = $state(false);
  async function loadLinks() {
    const g = ++gen;
    const ch = channelId;
    loading = true;
    failed = "";
    try {
      const res = (await api.searchMessages("http", "", ch)) || [];
      if (g !== gen) return;
      const out = [];
      for (const m of res) {
        if (m.deleted || m.expired || m.channelId !== ch) continue;
        const card = cardFor(m);
        const urls = card ? [card.url] : extractLinks(m.content || "", 3);
        urls.forEach((url, ord) => {
          let host = "";
          try {
            host = new URL(url).hostname;
          } catch {
            return;
          }
          const title = card && card.url === url ? card.title : "";
          out.push({
            messageId: m.id,
            channelId: m.channelId,
            ord,
            sent: m.sent,
            sender: m.sender,
            senderName: m.senderName,
            unverified: !!m.unverified,
            url,
            domain: linkDomain(url) || host,
            title,
            site: card && card.url === url ? card.site : "",
          });
        });
      }
      out.sort((a, b) => (a.sent < b.sent ? 1 : a.sent > b.sent ? -1 : 0));
      linkItems = out;
      linksLoaded = true;
      loaded = true;
    } catch (e) {
      if (g === gen) failed = String(e?.message || e);
      loaded = true;
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
      failed = "";
      zoom = 0;
      tileState.clear();
      endSelect();
      queue?.close();
      queue = makeQueue();
      if (scroller) scroller.scrollTop = 0;
      scrollTop = 0;
      loadPage(true);
    });
  });
  $effect(() => {
    channelId;
    untrack(loadCounts);
  });

  // ---- staying current while open ----
  // A delete, an erase, an expiry or a hide removes the message's items at
  // once. A new message with an attachment fetches the first page again and
  // puts it in front of what is loaded (mergeHead), a moment later; an edit,
  // or an unhide, which can change an item anywhere, fetches again as much as
  // is loaded, a page at a time. Neither cuts what is loaded, so the grid
  // keeps its height and its scroll position.
  let refreshTimer = 0;
  let refreshAll = false;
  function refreshSoon(all) {
    refreshAll ||= all;
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(async () => {
      loadCounts();
      if (tab === "links") return loadLinks();
      const g = gen;
      const whole = refreshAll;
      refreshAll = false;
      try {
        if (!whole) {
          const page = await api.mediaPage(channelId, coreTab(tab), "", PAGE);
          if (g !== gen) return;
          const m = mergeHead(items, next, page?.items || [], page?.next || "");
          if (m) {
            items = m.items;
            next = m.next;
            return;
          }
        }
        await reloadLoaded(g);
      } catch {
        /* the next change or reopen tries again */
      }
    }, 400);
  }
  async function reloadLoaded(g) {
    const want = Math.max(PAGE, items.length);
    let got = [];
    let cur = "";
    do {
      const page = await api.mediaPage(channelId, coreTab(tab), cur, Math.min(PAGE_MAX, want - got.length));
      if (g !== gen) return;
      got = mergePage(got, page?.items || []);
      cur = page?.next || "";
    } while (cur && got.length < want);
    items = got;
    next = cur;
  }
  onMount(() => {
    const offMsg = on("message", (m) => {
      if (!m || m.channelId !== channelId) return;
      if (m.deleted || m.expired) {
        items = withoutMessage(items, m.id);
        linkItems = linkItems.filter((l) => l.messageId !== m.id);
        loadCounts();
        return;
      }
      if (tab === "links") {
        if (/https?:\/\//.test(m.content || "")) refreshSoon(false);
        return;
      }
      const held = items.find((it) => it.messageId === m.id);
      // A held item's message changes the gallery only by an edit or by its
      // signature arriving; a reaction or a pin does not.
      if (held) {
        if (m.edited || !!m.unverified !== !!held.unverified) refreshSoon(true);
      } else if (hasAttachment(m.content || "")) refreshSoon(!!m.edited);
    });
    const offHide = on("message-hidden", (h) => {
      if (!h || h.channelId !== channelId) return;
      if (h.hidden) {
        items = withoutMessage(items, h.messageId);
        linkItems = linkItems.filter((l) => l.messageId !== h.messageId);
      } else refreshSoon(true);
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
      clearTimeout(scrubIdle);
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
  const live = $derived(liveItems(items, nowSec));
  const shown = $derived(
    tab === "links" ? linkItems : tab === "files" ? live.filter((it) => !isVoice(it)) : tab === "voice" ? live.filter(isVoice) : live,
  );

  // ---- the virtual grid ----
  let scroller = $state(null);
  let width = $state(0);
  let viewH = $state(0);
  let scrollTop = $state(0);
  let raf = 0;
  function onScroll() {
    wakeScrub();
    if (raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      if (scroller) scrollTop = scroller.scrollTop;
    });
  }
  let landscape = $state(window.innerWidth > window.innerHeight);
  let wide = $state(false); // the desktop's large view
  const GAP = 2;
  const HEAD_H = 44;
  const ROW_H = 64;
  // Pinch / Ctrl + wheel: columns away from the width's own count. A phone
  // holds three to five; a desktop panel two to eight, but never tiles under
  // 64 px.
  let zoom = $state(0);
  const baseCols = $derived(
    wide ? Math.max(4, Math.min(10, Math.floor((width || 0) / 150))) : columnsFor(width, { phone: S.isMobile, landscape }),
  );
  const colRange = $derived(
    S.isMobile ? [landscape ? 4 : 3, landscape ? 7 : 5] : [2, Math.max(2, Math.min(wide ? 12 : 8, Math.floor((width || 0) / 64)))],
  );
  const visualCols = $derived(Math.max(colRange[0], Math.min(colRange[1], baseCols + zoom)));
  const cols = $derived(visualTab ? visualCols : 1);
  const tileW = $derived(cols > 1 ? Math.max(40, (width - GAP * (cols - 1)) / cols) : width);
  const rowH = $derived(visualTab ? tileW + GAP : ROW_H);
  const rows = $derived(buildRows(shown, cols));
  const lay = $derived(layout(rows, rowH, HEAD_H));
  // One screen of rows either side of the viewport is mounted (half a screen
  // when the grid is denser than three across, so the count stays the same).
  const over = $derived((viewH || 600) * (cols > 3 ? 0.5 : 1));
  const range = $derived(visibleRange(lay.tops, lay.height, scrollTop, viewH || 600, over));
  // Primitives, so a scroll that stays within the same rows changes nothing
  // downstream.
  const rStart = $derived(range[0]);
  const rEnd = $derived(range[1]);
  const mounted = $derived(rows.slice(rStart, rEnd).map((row, i) => ({ row, top: lay.tops[rStart + i] })));

  $effect(() => {
    if (loaded && tab !== "links" && needMore([rStart, rEnd], rows.length, next) && !loading) untrack(() => loadPage(false));
  });

  // The month heading that stays at the top: the last heading above the
  // scroll position, pushed up by the next one as it arrives.
  const heads = $derived(rows.reduce((a, r, i) => (r.kind === "head" && a.push(i), a), []));
  const stick = $derived.by(() => {
    if (scrollTop <= 1 || !heads.length) return null;
    let a = 0;
    let b = heads.length - 1;
    while (a < b) {
      const mid = (a + b + 1) >> 1;
      if (lay.tops[heads[mid]] <= scrollTop) a = mid;
      else b = mid - 1;
    }
    const nextTop = a + 1 < heads.length ? lay.tops[heads[a + 1]] - scrollTop : Infinity;
    return { label: rows[heads[a]].label, push: Math.min(0, nextTop - HEAD_H) };
  });

  // Pinch and Ctrl + wheel keep the item at the top of the screen in place.
  async function setZoom(d) {
    const nz = zoom + d;
    const target = Math.max(colRange[0], Math.min(colRange[1], baseCols + nz));
    if (target === visualCols) return;
    const at = topItem();
    zoom = target - baseCols;
    await tick();
    if (!at || !scroller) return;
    const i = rows.findIndex((r) => r.kind === "tiles" && r.items.some((x) => itemKey(x) === at));
    if (i >= 0) scroller.scrollTop = scrollTop = Math.max(0, lay.tops[i] - HEAD_H);
  }
  function topItem() {
    for (let i = rStart; i < rEnd; i++) {
      if (lay.tops[i] + rowH > scrollTop && rows[i].kind === "tiles") return itemKey(rows[i].items[0]);
    }
    return "";
  }
  $effect(() => {
    const el = scroller;
    if (!el) return;
    let acc = 0;
    const wheel = (e) => {
      if (!e.ctrlKey || !visualTab) return;
      e.preventDefault();
      acc += e.deltaY;
      if (Math.abs(acc) < 60) return;
      setZoom(acc > 0 ? 1 : -1);
      acc = 0;
    };
    let pinch = 0;
    const dist = (t) => Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY);
    const tstart = (e) => {
      if (e.touches.length === 2 && visualTab) pinch = dist(e.touches);
    };
    const tmove = (e) => {
      if (!pinch || e.touches.length !== 2) return;
      e.preventDefault();
      const r = dist(e.touches) / pinch;
      if (r > 1.25 || r < 0.8) {
        setZoom(r > 1 ? -1 : 1);
        pinch = dist(e.touches);
      }
    };
    const tend = (e) => {
      if (e.touches.length < 2) pinch = 0;
    };
    el.addEventListener("wheel", wheel, { passive: false });
    el.addEventListener("touchstart", tstart, { passive: true });
    el.addEventListener("touchmove", tmove, { passive: false });
    el.addEventListener("touchend", tend);
    el.addEventListener("touchcancel", tend);
    return () => {
      el.removeEventListener("wheel", wheel);
      el.removeEventListener("touchstart", tstart);
      el.removeEventListener("touchmove", tmove);
      el.removeEventListener("touchend", tend);
      el.removeEventListener("touchcancel", tend);
    };
  });

  // ---- the scrubber: a long grid's date jump ----
  const scrubbable = $derived(lay.height > (viewH || 600) * 4 && heads.length > 0);
  let scrubAwake = $state(false);
  let scrubbing = $state(false);
  let scrubIdle = 0;
  let railEl = $state(null);
  const THUMB_H = 40;
  const maxScroll = $derived(Math.max(1, lay.height - (viewH || 600)));
  const scrubFrac = $derived(Math.min(1, Math.max(0, scrollTop / maxScroll)));
  const railH = $derived(Math.max(0, (viewH || 600) - 2 * HEAD_H));
  function wakeScrub() {
    if (!scrubbable) return;
    scrubAwake = true;
    clearTimeout(scrubIdle);
    scrubIdle = setTimeout(() => !scrubbing && (scrubAwake = false), 1400);
  }
  function scrubTo(clientY) {
    if (!railEl || !scroller) return;
    const r = railEl.getBoundingClientRect();
    const f = Math.min(1, Math.max(0, (clientY - r.top - THUMB_H / 2) / Math.max(1, r.height - THUMB_H)));
    scroller.scrollTop = f * maxScroll;
  }
  function scrubDown(e) {
    e.preventDefault();
    scrubbing = true;
    scrubAwake = true;
    e.currentTarget.setPointerCapture?.(e.pointerId);
    scrubTo(e.clientY);
  }
  function scrubMove(e) {
    if (scrubbing) scrubTo(e.clientY);
  }
  function scrubUp() {
    scrubbing = false;
    wakeScrub();
  }
  function scrubKey(e) {
    if (!scroller) return;
    const v = viewH || 600;
    const by = { PageDown: v, PageUp: -v, ArrowDown: v / 4, ArrowUp: -v / 4 }[e.key];
    if (by) scroller.scrollTop += by;
    else if (e.key === "Home") scroller.scrollTop = 0;
    else if (e.key === "End") scroller.scrollTop = maxScroll;
    else return;
    e.preventDefault();
  }
  // The month at the scrubber's position, for its bubble and its value.
  const scrubLabel = $derived(stick?.label || (heads.length ? rows[heads[0]].label : ""));

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
    if (!visualTab || !queue) return;
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
  // Pictures already faded in once: a tile reused for one of them by the
  // scroll shows it at once.
  const seen = new Set();

  // ---- selecting ----
  let selecting = $state(false);
  const selected = new SvelteSet();
  const selItems = $derived(selecting ? shown.filter((it) => selected.has(itemKey(it))) : []);
  function endSelect() {
    selecting = false;
    selected.clear();
  }
  function toggle(it) {
    const k = itemKey(it);
    if (selected.has(k)) selected.delete(k);
    else selected.add(k);
  }
  function selectAt(e) {
    const it = byKey.get(e?.target?.closest?.("[data-k]")?.dataset.k);
    if (!it || tab === "links") return;
    selecting = true;
    selected.add(itemKey(it));
  }
  // Drag across tiles (a mouse) selects the run between where it started and
  // where it is.
  let drag = null; // { from, x, y, moved, base }
  let eatClick = false;
  function dragDown(e) {
    if (e.pointerType !== "mouse" || e.button !== 0 || !visualTab) return;
    const k = e.target.closest?.(".tile")?.dataset.k;
    if (!k) return;
    drag = { from: k, x: e.clientX, y: e.clientY, moved: false, base: null };
  }
  function dragMove(e) {
    if (!drag) return;
    if (!drag.moved) {
      if (Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 8) return;
      drag.moved = true;
      selecting = true;
      drag.base = new Set(selected);
    }
    const k = document.elementFromPoint(e.clientX, e.clientY)?.closest?.('[data-testid="gallery-scroll"] .tile')?.dataset.k;
    if (!k) return;
    const keys = shown.map(itemKey);
    const a = keys.indexOf(drag.from);
    const b = keys.indexOf(k);
    if (a < 0 || b < 0) return;
    selected.clear();
    for (const x of drag.base) selected.add(x);
    for (let i = Math.min(a, b); i <= Math.max(a, b); i++) selected.add(keys[i]);
  }
  function dragUp() {
    if (drag?.moved) eatClick = true;
    drag = null;
  }

  // ---- actions ----
  let viewing = $state(-1); // index into the visual items, -1 = closed
  const visual = $derived(visualTab ? shown : []);
  function open(it) {
    const k = itemKey(it);
    // On a billed connection a tap is the go-ahead for this one.
    const st = tileState.get(k);
    if (st === "metered" || st === "missing") queue?.push(it, true);
    viewing = visual.findIndex((x) => itemKey(x) === k);
  }
  function tap(it, opener) {
    if (eatClick) {
      eatClick = false;
      return;
    }
    if (selecting) toggle(it);
    else opener(it);
  }
  function goTo(it) {
    viewing = -1;
    wide = false;
    endSelect();
    onGoto(it);
    jumpToMessage(it.channelId, it.messageId);
  }
  async function saveOne(it) {
    if (it.kind === KIND_PHOTO) {
      const src = await loadAttachment(it.channelId, it);
      const how = await saveImageSrc(src, it.name || `concord-${it.blobId.slice(0, 8)}.${it.subtype}`);
      cacheTick++;
      return how;
    }
    const url = await api.fetchFile(it.channelId, it.blobId, it.keys, it.mime);
    return saveBlob(it.name || "file", await (await fetch(url)).blob());
  }
  async function save(list) {
    let ok = 0;
    let gallery = false;
    for (const it of list) {
      try {
        const how = await saveOne(it);
        if (how) ok++;
        if (how === "gallery") gallery = true;
      } catch (e) {
        flash(`Couldn't save: ${humanError(e)}`, "error");
        return;
      }
    }
    if (gallery) flash(ok === 1 ? "Saved to your gallery" : `${ok} saved to your gallery`, "success");
    else if (!ok) flash(list.length === 1 ? "Couldn't save that" : "Couldn't save those");
    else if (list.length > 1) flash(`${ok} saved`, "success");
  }
  async function copy(it) {
    try {
      await copyImageToClipboard(await loadAttachment(it.channelId, it));
      cacheTick++;
      flash("Image copied", "success");
    } catch (e) {
      flash(`Couldn't copy image: ${humanError(e)}`, "error");
    }
  }
  function forward(list) {
    if (!list.length) return;
    S.modal = { kind: "forward", items: list.map((it) => ({ ...it })) };
    endSelect();
  }
  function menu(e, it) {
    if (e.pointerType === "touch" || selecting) {
      e.preventDefault();
      return;
    }
    openContextMenu(e, [
      { label: "Go to message", icon: "bubble", onClick: () => goTo(it) },
      { label: "Save", icon: "download", onClick: () => save([it]) },
      it.kind === KIND_PHOTO && { label: "Copy image", icon: "copy", onClick: () => copy(it) },
      { label: "Forward…", icon: "forward", onClick: () => forward([it]) },
      {
        label: "Select",
        icon: "check",
        onClick: () => {
          selecting = true;
          selected.add(itemKey(it));
        },
      },
    ]);
  }
  function linkMenu(e, it) {
    if (e.pointerType === "touch") return;
    openContextMenu(e, [
      { label: "Go to message", icon: "bubble", onClick: () => goTo(it) },
      {
        label: "Copy link",
        icon: "copy",
        onClick: () => navigator.clipboard?.writeText(it.url).then(() => flash("Link copied", "success")),
      },
    ]);
  }
  function linkPress(e) {
    const it = linkItems.find((l) => `${l.messageId}:${l.ord}` === e?.target?.closest?.("[data-k]")?.dataset.k);
    if (it) linkMenu({ ...e, pointerType: "mouse" }, it);
  }

  // A long-press's item, from the node it landed on (see the tile markup).
  const byKey = $derived(new Map(shown.map((it) => [itemKey(it), it])));
  const who = (it) => nameFor(it.sender, it.senderName);
  const unsigned = (it) => unsignedItem(it, S.identity.fingerprint);
  const tileLabel = (it) =>
    (it.spoiler ? "Spoiler" : `${it.kind === KIND_VIDEO ? "Video" : "Photo"} from ${who(it)}, ${fmtClock(it.sent)}`) +
    (unsigned(it) ? `. ${UNSIGNED_LABEL}` : "");
  const DAY = { month: "short", day: "numeric" };
  const dayOf = (iso) => {
    try {
      return new Date(iso).toLocaleDateString(undefined, DAY);
    } catch {
      return "";
    }
  };
  // The type, written on a file's badge: "PDF", "ZIP". Four letters at most.
  const extOf = (name) => /\.([a-z0-9]{1,4})$/i.exec(name || "")?.[1].toUpperCase() || "";
  // A colour family per kind of file, so a list reads at a glance.
  const toneOf = (name, mime) => {
    const e = extOf(name).toLowerCase();
    if (/^(pdf)$/.test(e)) return "t-doc";
    if (/^(zip|rar|7z|tar|gz|xz|bz2)$/.test(e)) return "t-arc";
    if (/^(xls|xlsx|csv|ods|numb)$/.test(e)) return "t-sheet";
    if (/^(mp3|wav|flac|ogg|m4a|opus)$/.test(e) || /^audio\//.test(mime || "")) return "t-audio";
    if (/^(js|ts|go|py|rs|c|h|cpp|java|json|html|css|sh|md)$/.test(e)) return "t-code";
    return "";
  };

  function onKey(e) {
    if (e.key !== "Escape" || viewing >= 0) return;
    if (selecting) {
      e.stopPropagation();
      endSelect();
    } else if (wide) {
      e.stopPropagation();
      wide = false;
    }
  }
  // The large view sits over the whole window, so it lives in <body> while
  // open (an ancestor's transform or containment would otherwise hold it).
  function lift(node, on) {
    const anchor = document.createComment("gallery");
    node.before(anchor);
    const put = (v) => (v ? document.body.appendChild(node) : anchor.after(node));
    put(on);
    return {
      update: put,
      destroy() {
        anchor.remove();
      },
    };
  }
  const multi = $derived(selItems.length);
</script>

<svelte:window
  onresize={() => (landscape = window.innerWidth > window.innerHeight)}
  onpointermove={dragMove}
  onpointerup={dragUp}
  onpointercancel={dragUp}
/>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div class="gal-host" class:wide use:lift={wide} onkeydown={onKey} role="presentation">
  {#if wide}
    <button type="button" class="gal-scrim" aria-label="Close the large view" onclick={() => (wide = false)}></button>
  {/if}
  <section class="gal" class:phone={S.isMobile} aria-label="Shared in {place}">
    <header class="gal-top">
      <div class="gal-title">
        <h2 class="gal-name"><bdi>{place}</bdi></h2>
        <p class="gal-count" aria-live="polite">{countLine}</p>
      </div>
      <div class="gal-acts">
        {#if tab !== "links" && shown.length}
          <button
            type="button"
            class="gal-btn"
            class:on={selecting}
            aria-pressed={selecting}
            onclick={() => (selecting ? endSelect() : (selecting = true))}>{selecting ? "Done" : "Select"}</button
          >
        {/if}
        {#if !S.isMobile}
          <button
            type="button"
            class="gal-icon"
            aria-label={wide ? "Back to the side panel" : "Open the large view"}
            use:tooltip={wide ? "Back to the side panel" : "Open the large view"}
            onclick={() => (wide = !wide)}
          >
            <Icon name={wide ? "close" : "grid4"} size={16} />
          </button>
        {/if}
      </div>
    </header>

    <div class="gal-tabs" role="radiogroup" aria-label="Show" bind:this={tabsEl} style="--n:{TABS.length};--i:{tabIndex}">
      <span class="gal-thumb" aria-hidden="true"></span>
      {#each TABS as t, i (t.id)}
        <button
          type="button"
          role="radio"
          class="gal-tab"
          class:on={t.id === tab}
          aria-checked={t.id === tab}
          aria-label={t.label}
          tabindex={t.id === tab ? 0 : -1}
          onclick={() => (tab = t.id)}
          onkeydown={tabKey}
        >
          <span>{t.short}</span>
        </button>
      {/each}
    </div>

    <div class="gal-body">
      <div
        class="gal-scroll"
        class:grid={visualTab}
        bind:this={scroller}
        bind:clientWidth={width}
        bind:clientHeight={viewH}
        onscroll={onScroll}
        data-testid="gallery-scroll"
      >
        {#if loaded && !shown.length && !(tab !== "links" && next)}
          <div class="gal-empty">
            {#if failed}
              <EmptyState compact icon="alert" headline="Couldn't read this conversation's media" sub={failed} />
            {:else if tab === "visual"}
              <EmptyState compact icon="image" headline="No photos or videos yet" sub="Pictures and clips shared in {place} gather here, newest first." />
            {:else if tab === "files"}
              <EmptyState compact icon="attach" headline="No files yet" sub="Documents and archives sent here are listed with their size, who sent them and when." />
            {:else if tab === "links"}
              <EmptyState compact icon="link" headline="No links yet" sub="Links shared here collect with their site and title, so one is easy to find again." />
            {:else}
              <EmptyState compact icon="mic" headline="No voice messages yet" sub="Voice messages sent here are kept in one list." />
            {/if}
          </div>
        {:else if !loaded && failed}
          <div class="gal-empty">
            <EmptyState compact icon="alert" headline="Couldn't read this conversation's media" sub={failed} />
          </div>
        {:else if !loaded}
          <!-- The first page on its way: the shape of what is coming. -->
          <div class="gal-skel" class:grid={visualTab} style="--cols:{cols}" aria-hidden="true">
            {#each { length: visualTab ? cols * 4 : 5 } as _}
              <span class="sk"></span>
            {/each}
          </div>
        {/if}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="gal-space" style="height:{lay.height}px" onpointerdown={dragDown}>
          {#each mounted as m}
            {#if m.row.kind === "head"}
              <div class="gal-head" style="transform:translateY({m.top}px);height:{HEAD_H}px">{m.row.label}</div>
            {:else if visualTab}
              <!-- data-audit="bleed": the grid runs edge to edge on purpose,
                   like a photo library; headings and rows keep the padding. -->
              <div class="gal-row" data-audit="bleed" style="transform:translateY({m.top}px);height:{tileW}px;grid-template-columns:repeat({cols}, minmax(0, 1fr));gap:{GAP}px">
                <!-- Not keyed: scrolling hands the same tile nodes new items rather
                     than building new ones. Handlers find their item through
                     data-k, since an action's parameters are not updated. -->
                {#each m.row.items as it}
                  {@const st = tileState.get(itemKey(it)) || ""}
                  {@const src = it.kind === KIND_PHOTO && cacheTick >= 0 ? cachedAttachment(it.blobId) : ""}
                  {@const k = itemKey(it)}
                  <button
                    type="button"
                    class="tile"
                    class:spoiler={it.spoiler}
                    class:wait={!src}
                    class:sel={selecting && selected.has(k)}
                    class:picking={selecting}
                    aria-label={tileLabel(it)}
                    aria-pressed={selecting ? selected.has(k) : undefined}
                    data-msg={it.messageId}
                    data-k={k}
                    onclick={() => tap(it, open)}
                    oncontextmenu={(e) => menu(e, it)}
                    use:longpress={{ handler: selectAt }}
                  >
                    {#if src}
                      <img
                        {src}
                        alt=""
                        decoding="async"
                        draggable="false"
                        class:fresh={!seen.has(it.blobId)}
                        onload={() => seen.add(it.blobId)}
                      />
                    {:else if it.kind === KIND_VIDEO}
                      <span class="ph"><Icon name="play" size={20} /></span>
                    {/if}
                    {#if it.spoiler}<span class="tag">Spoiler</span>{/if}
                    {#if unsigned(it)}<span class="uns" data-testid="unsigned-mark"><Icon name="info" size={11} /></span>{/if}
                    {#if it.kind === KIND_VIDEO}
                      <span class="dur"><Icon name="play" size={10} />{fmtDuration(durationOf(it.blobId)) || fmtBytes(it.size)}</span>
                    {/if}
                    {#if st === "asking"}
                      <span class="state"><span class="spin"></span></span>
                    {:else if st === "metered"}
                      <span class="state">Tap to load</span>
                    {:else if st === "missing"}
                      <span class="state">Not reachable</span>
                    {/if}
                    {#if selecting}<span class="chk" aria-hidden="true"><Icon name="check" size={12} /></span>{/if}
                  </button>
                {/each}
              </div>
            {:else if tab === "links"}
              {@const it = m.row.items[0]}
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div
                class="frow lrow"
                style="transform:translateY({m.top}px);height:{ROW_H}px"
                data-k={itemKey(it)}
                oncontextmenu={(e) => {
                  e.preventDefault();
                  linkMenu(e, it);
                }}
                use:longpress={{ handler: linkPress }}
              >
                <a class="lmain" href={it.url} target="_blank" rel="noopener noreferrer">
                  <span class="badge t-link"><span class="lfirst">{(it.domain[0] || "?").toUpperCase()}</span></span>
                  <span class="ftext">
                    <span class="ftitle"><bdi>{it.title || it.domain}</bdi></span>
                    <span class="fsub"><bdi>{it.site || it.domain}</bdi> · {who(it)}</span>
                  </span>
                  <span class="fdate">{dayOf(it.sent)}</span>
                </a>
                <button type="button" class="lgo" aria-label="Go to message" use:tooltip={"Go to message"} onclick={() => goTo(it)}>
                  <Icon name="bubble" size={15} />
                </button>
              </div>
            {:else}
              {@const it = m.row.items[0]}
              {@const k = itemKey(it)}
              {@const voice = isVoice(it)}
              <button
                type="button"
                class="frow"
                class:sel={selecting && selected.has(k)}
                class:picking={selecting}
                aria-pressed={selecting ? selected.has(k) : undefined}
                style="transform:translateY({m.top}px);height:{ROW_H}px"
                data-k={k}
                onclick={() => tap(it, goTo)}
                oncontextmenu={(e) => menu(e, it)}
                use:longpress={{ handler: selectAt }}
              >
                {#if selecting}<span class="rchk" aria-hidden="true"><Icon name="check" size={12} /></span>{/if}
                {#if voice}
                  <span class="badge round t-voice"><Icon name="mic" size={16} /></span>
                {:else}
                  <span class="badge {toneOf(it.name, it.mime)}">
                    {#if extOf(it.name)}<span class="ext">{extOf(it.name)}</span>{:else}<Icon name="attach" size={16} />{/if}
                  </span>
                {/if}
                <span class="ftext">
                  <span class="ftitle"><bdi>{voice ? "Voice message" : midTrunc(it.name, 40)}</bdi></span>
                  <span class="fsub"
                    >{voice ? fmtDuration(durationOf(it.blobId)) || fmtBytes(it.size) : [extOf(it.name), fmtBytes(it.size)].filter(Boolean).join(" · ") || "File"} · {who(it)}{#if unsigned(it)}<span
                        class="uns-inline"
                        role="img"
                        data-testid="unsigned-mark"
                        use:tooltip={UNSIGNED_TIP}
                        aria-label={UNSIGNED_LABEL}><Icon name="info" size={11} /></span
                      >{/if}</span
                  >
                </span>
                <span class="fdate">{dayOf(it.sent)}</span>
              </button>
            {/if}
          {/each}
        </div>
        {#if loading && loaded}<p class="gal-more">Loading…</p>{/if}
      </div>

      <!-- The month at the top of the screen, over what scrolls under it. -->
      <div class="gal-stick" class:on={!!stick} aria-hidden="true">
        <div class="gal-stick-bar" style="transform:translateY({stick?.push || 0}px)">
          <span>{stick?.label || ""}</span>
        </div>
      </div>

      {#if scrubbable && visualTab}
        <div
          class="gal-scrub"
          class:on={scrubAwake || scrubbing}
          class:drag={scrubbing}
          bind:this={railEl}
          role="slider"
          tabindex="0"
          aria-label="Jump to a date"
          aria-valuemin="0"
          aria-valuemax="100"
          aria-valuenow={Math.round(scrubFrac * 100)}
          aria-valuetext={scrubLabel}
          onpointerdown={scrubDown}
          onpointermove={scrubMove}
          onpointerup={scrubUp}
          onpointercancel={scrubUp}
          onpointerenter={wakeScrub}
          onfocus={wakeScrub}
          onkeydown={scrubKey}
        >
          <span class="knob" style="transform:translateY({scrubFrac * Math.max(0, railH - THUMB_H)}px)">
            <span class="bubble">{scrubLabel}</span>
          </span>
        </div>
      {/if}

      <div class="gal-selbar" class:on={selecting} aria-hidden={!selecting} inert={!selecting}>
        <span class="sel-n" aria-live="polite">{multi ? `${multi} selected` : "Select items"}</span>
        <div class="sel-acts">
          <button type="button" class="sel-btn" disabled={!multi} onclick={() => save(selItems)}>
            <Icon name="download" size={16} /><span>Save</span>
          </button>
          <button type="button" class="sel-btn" disabled={!multi} onclick={() => forward(selItems)}>
            <Icon name="forward" size={16} /><span>Forward</span>
          </button>
          <button type="button" class="sel-btn" disabled={multi !== 1} onclick={() => goTo(selItems[0])}>
            <Icon name="bubble" size={16} /><span>Go to message</span>
          </button>
        </div>
      </div>
    </div>
  </section>
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
  /* ---- the shell ---- */
  .gal-host {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .gal {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    position: relative;
  }
  /* A phone opens the panel as a sheet rising into place. */
  .gal.phone {
    animation: gal-sheet var(--dur-stage) var(--ease-spring);
  }
  @keyframes gal-sheet {
    from {
      opacity: 0;
      transform: translateY(24px);
    }
  }
  /* The desktop's large view: a full-height surface over the window. */
  .gal-host.wide {
    position: fixed;
    inset: 0;
    z-index: var(--z-layer);
    display: grid;
    place-items: center;
  }
  .gal-scrim {
    position: absolute;
    inset: 0;
    background: var(--scrim);
    animation: gal-fade var(--dur-standard) var(--ease-out);
  }
  .wide .gal {
    width: min(1180px, calc(100vw - 2 * var(--sp-6)));
    height: calc(100vh - 2 * var(--sp-6));
    background: var(--bg-1);
    border: 1px solid var(--hairline);
    border-radius: var(--radius-xl);
    box-shadow: var(--shadow-stage);
    overflow: hidden;
    animation: gal-rise var(--dur-stage) var(--ease-spring);
  }
  @keyframes gal-rise {
    from {
      opacity: 0;
      transform: translateY(16px) scale(0.98);
    }
  }
  @keyframes gal-fade {
    from {
      opacity: 0;
    }
  }

  .gal-top {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-4) var(--sp-4) var(--sp-3);
  }
  .wide .gal-top {
    padding: var(--sp-5) var(--sp-5) var(--sp-3);
  }
  .gal-title {
    flex: 1;
    min-width: 0;
  }
  .gal-name {
    margin: 0;
    font-family: var(--display-font);
    font-size: var(--fs-title);
    font-weight: 600;
    letter-spacing: -0.015em;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gal-count {
    margin: 2px 0 0;
    min-height: 1.3em;
    font-size: var(--fs-small);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gal-acts {
    display: flex;
    align-items: center;
    gap: var(--sp-1);
    flex: none;
  }
  .gal-btn {
    height: 30px;
    padding: 0 var(--sp-3);
    border-radius: 999px;
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--text);
    background: var(--bg-3);
    transition: background var(--dur-quick) ease;
  }
  .gal-btn.on {
    color: var(--accent-fg);
    background: var(--accent);
  }
  .gal-icon {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    border-radius: 999px;
    color: var(--text-muted);
  }
  @media (pointer: fine) {
    .gal-icon:hover {
      color: var(--text);
      background: var(--bg-3);
    }
  }

  /* ---- the switch: a thumb that slides to the chosen kind ---- */
  .gal-tabs {
    position: relative;
    display: grid;
    grid-template-columns: repeat(var(--n), minmax(0, 1fr));
    margin: 0 var(--sp-4) var(--sp-2);
    padding: 3px;
    border-radius: calc(var(--radius-md) + 3px);
    background: var(--bg-3);
  }
  .wide .gal-tabs {
    margin: 0 var(--sp-5) var(--sp-3);
    max-width: 520px;
  }
  .gal-thumb {
    position: absolute;
    top: 3px;
    bottom: 3px;
    left: 3px;
    width: calc((100% - 6px) / var(--n));
    border-radius: var(--radius-md);
    background: var(--bg-1);
    box-shadow:
      0 1px 2px color-mix(in srgb, black 18%, transparent),
      inset 0 0 0 1px var(--hairline);
    transform: translateX(calc(var(--i) * 100%));
    transition: transform var(--dur-move) var(--ease-spring);
  }
  .gal-tab {
    position: relative;
    height: 30px;
    padding: 0 var(--sp-1);
    border-radius: var(--radius-md);
    font-size: var(--fs-compact);
    font-weight: 500;
    color: var(--text-muted);
    transition: color var(--dur-quick) ease;
  }
  .gal-tab span {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gal-tab.on {
    color: var(--text);
    font-weight: 600;
  }
  .gal-tab:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -1px;
  }

  /* ---- the scrolling body ---- */
  .gal-body {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .gal-scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
    position: relative;
    contain: strict;
    overscroll-behavior: contain;
    scrollbar-width: none;
  }
  .gal-scroll:not(.grid) {
    scrollbar-width: thin;
  }
  .gal-space {
    position: relative;
    user-select: none;
    -webkit-user-select: none;
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
  .gal-head,
  .gal-stick-bar {
    display: flex;
    align-items: center;
    padding: 0 var(--sp-4);
    box-sizing: border-box;
    font-family: var(--display-font);
    font-size: var(--fs-ui);
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--text);
  }
  .wide .gal-head,
  .wide .gal-stick-bar {
    padding: 0 var(--sp-5);
  }
  /* The month that stays: frosted, so the grid shows through as it passes. */
  .gal-stick {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 44px;
    overflow: hidden;
    pointer-events: none;
    opacity: 0;
    transition: opacity var(--dur-quick) ease;
    z-index: 2;
  }
  .gal-stick.on {
    opacity: 1;
  }
  .gal-stick-bar {
    height: 100%;
    background: color-mix(in srgb, var(--bg-1) 72%, transparent);
    backdrop-filter: blur(16px) saturate(1.4);
    -webkit-backdrop-filter: blur(16px) saturate(1.4);
    border-bottom: 1px solid var(--hairline);
    will-change: transform;
  }
  :global(.insp.overlay) .gal-stick-bar {
    background: color-mix(in srgb, var(--bg-elevated) 72%, transparent);
  }

  /* ---- tiles: square, edge to edge, two pixels apart ---- */
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
    background: color-mix(in srgb, var(--bg-3) 80%, var(--bg-1));
    color: var(--text-faint);
    contain: strict;
  }
  .tile:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -2px;
    z-index: 1;
  }
  .tile img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
    transition:
      transform var(--dur-move) var(--ease-out),
      opacity var(--dur-move) var(--ease-out);
    -webkit-touch-callout: none; /* the long-press is ours: it selects */
    user-select: none;
    -webkit-user-select: none;
  }
  .tile img.fresh {
    animation: gal-fade var(--dur-calm) var(--ease-out);
  }
  /* Waiting for its picture: a light passing over the square. */
  .tile.wait::before {
    content: "";
    position: absolute;
    inset: 0;
    background: linear-gradient(
      100deg,
      transparent 30%,
      color-mix(in srgb, var(--text) 6%, transparent) 50%,
      transparent 70%
    );
    transform: translateX(-100%);
    animation: gal-shimmer 1.6s linear infinite;
  }
  @keyframes gal-shimmer {
    to {
      transform: translateX(100%);
    }
  }
  @media (pointer: fine) {
    .tile:not(.spoiler):not(.picking):hover img {
      transform: scale(1.04);
    }
  }
  .tile.spoiler img {
    filter: blur(18px);
    transform: scale(1.15);
  }
  .ph {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border-radius: 50%;
    color: var(--text-muted);
    background: color-mix(in srgb, var(--bg-1) 70%, transparent);
  }
  .tag,
  .dur,
  .state,
  .uns {
    position: absolute;
    font-size: var(--fs-small);
    color: #fff;
    background: rgba(0, 0, 0, 0.5);
    border-radius: 999px;
    padding: 1px 7px;
    pointer-events: none;
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
  }
  .tag {
    font-weight: 700;
    padding: 3px 10px;
  }
  /* The feed's "unsigned copy" mark, in the tile's free corner. */
  .uns {
    top: 6px;
    left: 6px;
    display: inline-flex;
    padding: 3px;
  }
  .uns-inline {
    display: inline-flex;
    vertical-align: -1px;
    margin-left: 3px;
    color: var(--text-faint);
  }
  .dur {
    right: 6px;
    bottom: 6px;
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-weight: 600;
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
  /* Selecting: a ring in the corner, filled when chosen; the chosen picture
     steps back a little under an accent wash. */
  .chk,
  .rchk {
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    color: transparent;
    border: 2px solid rgba(255, 255, 255, 0.9);
    background: rgba(0, 0, 0, 0.18);
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.3);
    transition: transform var(--dur-quick) var(--ease-spring);
  }
  .chk {
    position: absolute;
    top: 6px;
    right: 6px;
    animation: gal-pop var(--dur-move) var(--ease-spring);
  }
  @keyframes gal-pop {
    from {
      opacity: 0;
      transform: scale(0.6);
    }
  }
  .tile.sel img,
  .tile.sel .ph {
    transform: scale(0.9);
  }
  .tile.sel {
    background: color-mix(in srgb, var(--accent) 22%, var(--bg-2));
  }
  .tile.sel .chk,
  .frow.sel .rchk {
    color: var(--accent-fg);
    border-color: var(--accent);
    background: var(--accent);
  }
  .rchk {
    flex: none;
    border-color: var(--border-strong);
    background: transparent;
    box-shadow: none;
  }

  /* ---- rows: files, voice, links ---- */
  .frow {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: 0 var(--sp-4);
    box-sizing: border-box;
    text-align: start;
    color: var(--text);
    border-radius: 0;
  }
  .wide .frow {
    padding: 0 var(--sp-5);
  }
  .frow.sel {
    background: color-mix(in srgb, var(--accent) 10%, transparent);
  }
  @media (pointer: fine) {
    .frow:hover {
      background: color-mix(in srgb, var(--bg-3) 60%, transparent);
    }
  }
  .frow::after {
    content: "";
    position: absolute;
    left: calc(var(--sp-4) + 44px + var(--sp-3));
    right: 0;
    bottom: 0;
    height: 1px;
    background: var(--hairline);
    pointer-events: none;
  }
  .badge {
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    flex: none;
    border-radius: var(--radius-md);
    --tone: var(--aurora-2);
    background: color-mix(in srgb, var(--tone) 16%, var(--bg-2));
    color: color-mix(in srgb, var(--tone) 72%, var(--text));
  }
  .badge.round {
    border-radius: 50%;
  }
  .t-doc {
    --tone: var(--danger);
  }
  .t-arc {
    --tone: var(--warn);
  }
  .t-sheet {
    --tone: var(--ok);
  }
  .t-audio,
  .t-voice {
    --tone: var(--accent);
  }
  .t-code {
    --tone: var(--aurora-1);
  }
  .t-link {
    --tone: var(--aurora-1);
  }
  .ext,
  .lfirst {
    font-size: var(--fs-micro);
    font-weight: 800;
    letter-spacing: 0.02em;
  }
  .lfirst {
    font-family: var(--display-font);
    font-size: var(--fs-title);
  }
  .ftext {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
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
  .fdate {
    flex: none;
    align-self: flex-start;
    margin-top: 14px;
    font-size: var(--fs-small);
    color: var(--text-faint);
    font-variant-numeric: tabular-nums;
  }
  .lrow {
    padding-right: var(--sp-2);
    gap: 0;
  }
  .lmain {
    flex: 1;
    min-width: 0;
    height: 100%;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    color: var(--text);
    text-decoration: none;
  }
  .lmain:focus-visible,
  .lgo:focus-visible,
  .frow:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -2px;
  }
  .lgo {
    display: grid;
    place-items: center;
    flex: none;
    width: 32px;
    height: 32px;
    margin-left: var(--sp-2);
    border-radius: 999px;
    color: var(--text-muted);
  }
  @media (pointer: fine) {
    .lgo:hover {
      color: var(--text);
      background: var(--bg-3);
    }
  }

  /* ---- first load, empty, more ---- */
  .gal-skel {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-4);
  }
  .gal-skel.grid {
    display: grid;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    gap: 2px;
    padding: 44px 0 0;
  }
  .sk {
    position: relative;
    overflow: hidden;
    height: 44px;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--bg-3) 80%, var(--bg-1));
  }
  .gal-skel.grid .sk {
    height: auto;
    aspect-ratio: 1;
    border-radius: 0;
  }
  .sk::before {
    content: "";
    position: absolute;
    inset: 0;
    background: linear-gradient(100deg, transparent 30%, color-mix(in srgb, var(--text) 6%, transparent) 50%, transparent 70%);
    transform: translateX(-100%);
    animation: gal-shimmer 1.6s linear infinite;
  }
  .gal-empty {
    padding: var(--sp-5) var(--sp-4);
  }
  .gal-more {
    margin: var(--sp-3) auto var(--sp-4);
    width: max-content;
    padding: var(--sp-1) var(--sp-3);
    border-radius: 999px;
    background: var(--bg-3);
    font-size: var(--fs-small);
    color: var(--text-muted);
  }

  /* ---- the scrubber ---- */
  .gal-scrub {
    position: absolute;
    top: 44px;
    bottom: 44px;
    right: 0;
    width: 36px;
    z-index: 3;
    opacity: 0;
    touch-action: none;
    transition: opacity var(--dur-standard) ease;
  }
  .gal-scrub.on,
  .gal-scrub:focus-visible {
    opacity: 1;
  }
  .gal-scrub:focus-visible {
    outline: none;
  }
  .knob {
    position: absolute;
    top: 0;
    right: var(--sp-2);
    width: 6px;
    height: 40px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--text) 55%, transparent);
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--bg-1) 50%, transparent);
    will-change: transform;
  }
  .gal-scrub:focus-visible .knob {
    outline: var(--focus-ring);
    outline-offset: 2px;
  }
  .bubble {
    position: absolute;
    right: calc(100% + var(--sp-2));
    top: 50%;
    padding: var(--sp-1) var(--sp-3);
    border-radius: 999px;
    white-space: nowrap;
    font-family: var(--display-font);
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--text);
    background: var(--bg-elevated);
    box-shadow: var(--shadow-pop);
    opacity: 0;
    transform: translate(6px, -50%);
    transition:
      opacity var(--dur-quick) ease,
      transform var(--dur-move) var(--ease-spring);
    pointer-events: none;
  }
  .gal-scrub.drag .bubble,
  .gal-scrub:focus-visible .bubble {
    opacity: 1;
    transform: translate(0, -50%);
  }

  /* ---- the selection bar ---- */
  .gal-selbar {
    position: absolute;
    left: var(--sp-3);
    right: var(--sp-3);
    bottom: calc(var(--sp-3) + var(--safe-bottom, 0px));
    z-index: 4;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-2) var(--sp-2) var(--sp-2) var(--sp-4);
    border-radius: var(--radius-lg);
    background: color-mix(in srgb, var(--bg-elevated) 86%, transparent);
    backdrop-filter: blur(18px) saturate(1.4);
    -webkit-backdrop-filter: blur(18px) saturate(1.4);
    border: 1px solid var(--hairline);
    box-shadow: var(--shadow-pop);
    opacity: 0;
    transform: translateY(calc(100% + var(--sp-4)));
    pointer-events: none;
    transition:
      transform var(--dur-move) var(--ease-spring),
      opacity var(--dur-quick) ease;
  }
  .gal-selbar.on {
    opacity: 1;
    transform: none;
    pointer-events: auto;
  }
  .sel-n {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--text);
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sel-acts {
    display: flex;
    gap: 2px;
  }
  .sel-btn {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    min-width: 52px;
    padding: var(--sp-1) var(--sp-2);
    border-radius: var(--radius-md);
    font-size: var(--fs-micro);
    font-weight: 500;
    color: var(--text);
  }
  .sel-btn span {
    white-space: nowrap;
  }
  .sel-btn:disabled {
    color: var(--text-faint);
  }
  @media (pointer: fine) {
    .sel-btn:not(:disabled):hover {
      background: var(--bg-3);
    }
  }
  .wide .gal-selbar {
    left: 50%;
    right: auto;
    width: min(520px, calc(100% - 2 * var(--sp-5)));
    transform: translate(-50%, calc(100% + var(--sp-5)));
  }
  .wide .gal-selbar.on {
    transform: translate(-50%, 0);
  }
</style>
