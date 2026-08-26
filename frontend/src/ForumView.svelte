<script>
  // The forum BOARD. A post is a real channel (type "thread", parent = the
  // forum), so it already has encryption, unread badges and mutes; what this
  // view adds is everything a board needs to be worth browsing — author, body
  // preview, media, tags, reply count, pinning, search, sort and three card
  // layouts.
  //
  // Two data sources, deliberately:
  //   • ForumBoard (api.forumBoard) — the cards. Only it carries the derived
  //     author, reply count and excerpt.
  //   • the guild snapshot — unread badges, and the CHANGE SIGNAL. Every
  //     mutation on any peer ends in a guild update, so watching a cheap
  //     signature of this forum's channels tells us when to refetch the board
  //     instead of polling it.
  // Both are built from the same in-memory channel on the backend, so they
  // cannot disagree.
  //
  // The design tension in the brief — "so many more options and knobs" against
  // "best design principles" — is resolved as progressive disclosure, not as a
  // compromise. At rest the toolbar shows four controls; sort/layout/tags/board
  // art all exist, one obvious gesture away (a menu, the ⋯ on a card, the gear).
  // Nothing here is a wall of forty switches.
  import { onMount } from "svelte";
  import { flip } from "svelte/animate";
  import { cubicOut } from "svelte/easing";
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import EmptyState from "./EmptyState.svelte";
  import Banner from "./Banner.svelte";
  import BottomSheet from "./BottomSheet.svelte";
  import { syncLayer } from "./lib/navstack.svelte.js";
  import { api, on } from "./lib/api.js";
  import { longpress, haptic } from "./lib/touch.js";
  import {
    S,
    activeGuild,
    selectChannel,
    memberByFpr,
    nameFor,
    flash,
    openContextMenu,
  } from "./lib/state.svelte.js";
  import { PERM, has } from "./lib/perms.js";
  import { loadAttachment } from "./lib/attachments.js";
  import {
    SORTS,
    LAYOUTS,
    TAG_LIMITS,
    arrangePosts,
    boardStats,
    resolveTags,
    postPreview,
    firstImage,
    relTime,
    absTime,
    isPending,
    washFor,
    tileFor,
    BOARD_PREFS_EVENT,
    readBoardPrefs,
    writeBoardPrefs,
    normalizeBoardPrefs,
  } from "./lib/forum.js";

  let { forum } = $props();

  // Android's WebView fires BOTH contextmenu and our long-press on a held
  // finger; letting both run opens the sheet twice. Same guard Message.svelte
  // uses — mouse right-click keeps the native event, touch uses long-press.
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;

  // Motion is read once: the CSS handles the rest through media queries, this is
  // only for the JS-driven FLIP duration (which has no CSS to opt out of).
  const reduceMotion =
    typeof matchMedia !== "undefined" && matchMedia("(prefers-reduced-motion: reduce)").matches;
  const flipMs = reduceMotion ? 0 : 260;

  // ---- device-local board preferences ------------------------------------
  // Layout, sort and the board's art are reading preferences, not guild state:
  // per forum, per device, never broadcast. localStorage can hold anything, so
  // everything that comes out of it goes through normalizeBoardPrefs.
  let prefs = $state(normalizeBoardPrefs(null));
  const savePrefs = (patch) => (prefs = writeBoardPrefs(forum.id, patch, prefs));

  // ---- board data ---------------------------------------------------------
  let board = $state(null);
  let phase = $state("loading"); // loading | ready | error
  let errMsg = $state("");
  // Every fetch carries a generation. A board that arrives after you've walked
  // to another forum (or after a newer fetch was fired) is dropped instead of
  // painting stale posts under the right title.
  let gen = 0;

  async function refresh({ quiet = false } = {}) {
    const gd = activeGuild();
    if (!gd || !forum?.id) return;
    const mine = ++gen;
    const forumId = forum.id;
    if (!quiet && !board) phase = "loading";
    try {
      const b = await api.forumBoard(gd.id, forumId);
      if (mine !== gen || forumId !== forum.id) return;
      board = b;
      phase = "ready";
      errMsg = "";
    } catch (err) {
      if (mine !== gen || forumId !== forum.id) return;
      errMsg = err?.message || String(err);
      // A refresh that fails behind an already-painted board must not blank it:
      // the cards on screen are still true, they're just not the newest truth.
      if (!board) phase = "error";
    }
  }

  // The change signal. Cheap string over exactly the fields a card renders from
  // the channel record; when it moves, something a reader can see moved.
  const boardSig = $derived.by(() => {
    const gd = activeGuild();
    if (!gd) return "";
    let sig = `${forum.id}|${(forum.forumTags || []).map((t) => `${t.id}${t.name}${t.color}${t.emoji}`).join(",")}|`;
    for (const c of gd.channels) {
      if (c.parent !== forum.id) continue;
      sig += `${c.id}:${c.lastActivity || 0}:${c.pinned ? 1 : 0}:${c.solved ? 1 : 0}:${(c.tags || []).join("+")};`;
    }
    return sig;
  });

  $effect(() => {
    boardSig; // tracked: refetch whenever the guild snapshot moves under us
    refresh({ quiet: true });
  });

  // Switching forums is a different board: drop the old one so its cards never
  // flash under the new header, and pick up that forum's own layout choice.
  let lastForumId = "";
  $effect(() => {
    if (forum.id === lastForumId) return;
    lastForumId = forum.id;
    board = null;
    phase = "loading";
    query = "";
    tagFilter = [];
    unansweredOnly = false;
    tagsExpanded = false;
    media = {};
    mediaGen++;
    prefs = readBoardPrefs(forum.id);
  });

  // A reply inside a post does NOT refresh the guild snapshot (nothing else
  // needs it to), so the board would show a stale reply count until something
  // else happened. This is the one view that cares, so this is where it belongs.
  onMount(() => {
    const adopt = (e) => {
      if (e.detail?.forumId === forum.id && e.detail?.prefs) prefs = e.detail.prefs;
    };
    window.addEventListener(BOARD_PREFS_EVENT, adopt);
    let timer = 0;
    const off = on("message", (m) => {
      if (!m?.channelId) return;
      const ch = activeGuild()?.channels?.find((c) => c.id === m.channelId);
      if (!ch || ch.parent !== forum.id) return;
      // Coalesce: a sync backfill can deliver a hundred messages in a second and
      // that is one board refresh, not a hundred.
      clearTimeout(timer);
      timer = setTimeout(() => refresh({ quiet: true }), 400);
    });
    return () => {
      window.removeEventListener(BOARD_PREFS_EVENT, adopt);
      clearTimeout(timer);
      off?.();
    };
  });

  // ---- filters ------------------------------------------------------------
  let query = $state("");
  let tagFilter = $state([]);
  let unansweredOnly = $state(false);
  let tagsExpanded = $state(false);
  let searchEl = $state(null);

  const palette = $derived(board?.tags || forum.forumTags || []);
  const posts = $derived(board?.posts || []);
  const stats = $derived(boardStats(posts));
  const visible = $derived(
    arrangePosts(posts, { query, tagIds: tagFilter, unansweredOnly, sort: prefs.sort }),
  );
  const filtering = $derived(!!query || tagFilter.length > 0 || unansweredOnly);
  // How many tag chips the row shows before it folds: about one line's worth of
  // a mouse-sized screen. The rest are one click away rather than a wrapped wall
  // of colour.
  //
  // The phone doesn't fold at all any more: the chip row there is a single-line
  // horizontal scroller, so folding would hide tags behind a "+N more" tap when
  // a flick already reaches them — and the fold was what made that row wrap to
  // two lines and pin ~70px of chrome above the board.
  const CHIP_FOLD = $derived(S.isMobile ? TAG_LIMITS.perForum : 6);
  const chips = $derived(tagsExpanded ? palette : palette.slice(0, CHIP_FOLD));

  function toggleTag(id) {
    tagFilter = tagFilter.includes(id) ? tagFilter.filter((t) => t !== id) : [...tagFilter, id];
  }
  function clearFilters() {
    query = "";
    tagFilter = [];
    unansweredOnly = false;
  }

  // ---- permissions --------------------------------------------------------
  const guild = $derived(activeGuild());
  const canPin = $derived(!!guild && (guild.isOwner || has(guild.myPerms || 0, PERM.MANAGE_MESSAGES)));
  const canManageTags = $derived(
    !!guild && (guild.isOwner || has(guild.myPerms || 0, PERM.MANAGE_CHANNELS)),
  );
  // Tagging and answering also accept the post's own author — a member may close
  // their own question without being handed a moderator bit. A post with no
  // synced opening message has no provable author, so nobody but a moderator
  // curates it (matches the backend).
  const mayCurate = (p) => canPin || (!!p.authorFingerprint && p.authorFingerprint === S.identity.fingerprint);

  // ---- lazy card media ----------------------------------------------------
  // A board of fifty posts must not fetch fifty encrypted blobs. Media loads
  // only for cards that come near the viewport, through the shared
  // attachments.js cache (so opening the post afterwards reuses the decrypt),
  // and a result that arrives for a board we've left is thrown away.
  let media = $state({}); // postId -> { state: "loading"|"ok"|"err", src }
  const MEDIA_KEEP = 36; // bounded: data URLs are megabytes, not kilobytes
  // Separate from `gen`: a board refetch (which happens on every guild update)
  // must not orphan an in-flight decrypt. Only walking to a different forum does.
  let mediaGen = 0;
  // The token travels with the NODE, not through a map keyed on post id: an
  // observer callback can fire before any bookkeeping keyed on the render has
  // caught up, and then the picture would silently never load.
  const nodeTok = new WeakMap();

  function wantMedia(node) {
    const tok = nodeTok.get(node);
    const postId = node.dataset.postId;
    if (!tok || !postId || media[postId]) return;
    const mine = mediaGen;
    media = { ...media, [postId]: { state: "loading", src: "" } };
    loadAttachment(postId, tok)
      .then((src) => {
        if (mine !== mediaGen) return; // different board now — drop it
        const next = { ...media, [postId]: { state: "ok", src } };
        const keys = Object.keys(next);
        // Insertion-order eviction: only a handful are ever on screen, so this
        // trims the ones you scrolled past long ago.
        for (const k of keys.slice(0, Math.max(0, keys.length - MEDIA_KEEP))) delete next[k];
        media = next;
      })
      .catch(() => {
        if (mine !== mediaGen) return;
        media = { ...media, [postId]: { state: "err", src: "" } };
      });
  }

  // Created eagerly, not in onMount: actions run as their element mounts, which
  // is BEFORE the component's onMount, so the first screenful of cards would
  // never be observed.
  const io =
    typeof IntersectionObserver === "undefined"
      ? null
      : new IntersectionObserver(
          (entries) => {
            for (const e of entries) if (e.isIntersecting) wantMedia(e.target);
          },
          // A generous margin so a picture is decrypted just before you reach
          // it, not as you stare at a placeholder.
          { rootMargin: "400px 0px" },
        );
  onMount(() => () => io?.disconnect());

  // Svelte action: watch a card's media box, but only if there is a picture to
  // fetch — a generated tile costs nothing and needs no observer.
  function watchMedia(node, tok) {
    const set = (t) => {
      if (t) {
        nodeTok.set(node, t);
        io?.observe(node);
      } else {
        nodeTok.delete(node);
        io?.unobserve(node);
      }
    };
    set(tok);
    return { update: set, destroy: () => set(null) };
  }

  // ---- keyboard -----------------------------------------------------------
  // Keyboard parity with the mouse, without stealing global keys: everything
  // here only fires while focus is inside the board.
  let root = $state(null);
  function onWindowKey(e) {
    if (tagPick && e.key === "Escape") {
      e.preventDefault();
      tagPick = null;
      return;
    }
    if (!root || !root.contains(document.activeElement)) return;
    const inField = /^(INPUT|TEXTAREA)$/.test(document.activeElement?.tagName || "");
    if (e.key === "/" && !inField) {
      e.preventDefault();
      searchEl?.focus();
      return;
    }
    if (e.key === "Escape" && inField && query) {
      e.preventDefault();
      query = "";
      return;
    }
    if ((e.key === "ArrowDown" || e.key === "ArrowUp") && !inField) {
      const hits = [...root.querySelectorAll(".hit")];
      if (!hits.length) return;
      e.preventDefault();
      const i = hits.indexOf(document.activeElement);
      const next = e.key === "ArrowDown" ? Math.min(hits.length - 1, i + 1) : Math.max(0, i - 1);
      hits[i === -1 ? 0 : next]?.focus();
    }
  }

  // ---- actions ------------------------------------------------------------
  // openMenu at an ELEMENT rather than at a cursor, so a menu opened with the
  // keyboard lands on its button instead of in the corner of the screen.
  function menuAt(el, items, opts = {}) {
    const r = el.getBoundingClientRect();
    openContextMenu(
      { clientX: r.left, clientY: r.bottom + 6, preventDefault() {}, stopPropagation() {} },
      items,
      opts,
    );
  }

  // Curation lands with a toast, which a thumb on a phone is usually covering.
  // haptic() is the confirmation that reaches the hand; it no-ops off-device.
  async function setPinned(p, pinned) {
    try {
      await api.setPostPinned(guild.id, p.id, pinned);
      haptic("medium");
      flash(pinned ? "Post pinned" : "Post unpinned", "success");
      refresh({ quiet: true });
    } catch (err) {
      flash(err);
    }
  }
  async function setSolved(p, solved) {
    try {
      await api.setPostSolved(guild.id, p.id, solved);
      haptic("medium");
      flash(solved ? "Marked answered" : "Reopened", "success");
      refresh({ quiet: true });
    } catch (err) {
      flash(err);
    }
  }

  async function setLocked(p, locked) {
    try {
      await api.setPostLocked(guild.id, p.id, locked);
      haptic("medium");
      flash(locked ? "Post closed" : "Post reopened", "success");
      refresh({ quiet: true });
    } catch (err) {
      flash(err);
    }
  }

  // Deleting a post takes the whole thread with it, so it asks first. The
  // confirm names the post: "Delete this post?" on a board of forty is a
  // question nobody can answer safely.
  function confirmDelete(p) {
    S.modal = {
      kind: "confirm",
      title: `Delete "${p.title || "this post"}"?`,
      body: "The post and every reply in it are removed for everyone. This can't be undone.",
      confirmLabel: "Delete post",
      danger: true,
      onConfirm: async () => {
        S.modal = null;
        try {
          await api.deleteChannel(guild.id, p.id);
          haptic("heavy");
          flash("Post deleted", "success");
          refresh({ quiet: true });
        } catch (err) {
          flash(err);
        }
      },
    };
  }

  function postMenu(el, p) {
    menuAt(
      el,
      [
        { label: "Open post", icon: "forum", onClick: () => selectChannel(p.id) },
        { sep: true },
        canPin && {
          label: p.pinned ? "Unpin from board" : "Pin to board",
          icon: "pin",
          onClick: () => setPinned(p, !p.pinned),
        },
        mayCurate(p) && {
          label: p.solved ? "Mark unanswered" : "Mark answered",
          icon: "check",
          onClick: () => setSolved(p, !p.solved),
        },
        mayCurate(p) &&
          palette.length > 0 && {
            label: "Edit tags…",
            icon: "spark",
            onClick: () => openTagPicker(p),
          },
        !palette.length &&
          canManageTags && {
            label: "Create tags for this forum…",
            icon: "spark",
            onClick: () => (S.modal = { kind: "forumSettings", forum }),
          },
        // Closing is moderation — it silences other people, which is why the
        // author alone cannot do it (see SetPostLocked).
        canPin && { sep: true },
        canPin && {
          label: p.locked ? "Reopen post" : "Close post",
          icon: "lock",
          onClick: () => setLocked(p, !p.locked),
        },
        // Deleting is offered to the AUTHOR as well: starting a post needs no
        // permission, so needing one to take it back would leave a member unable
        // to undo their own.
        mayCurate(p) && { sep: true },
        mayCurate(p) && {
          label: "Delete post",
          icon: "trash",
          danger: true,
          onClick: () => confirmDelete(p),
        },
      ],
      { title: p.title || "Post" },
    );
  }

  function boardMenu(el) {
    menuAt(el, [
      { header: true, label: "Sort" },
      ...SORTS.map((s) => ({
        label: s.label,
        icon: s.icon,
        active: prefs.sort === s.id,
        onClick: () => savePrefs({ sort: s.id }),
      })),
      { header: true, label: "Layout" },
      ...LAYOUTS.map((l) => ({
        label: l.label,
        icon: layoutIcon(l.id),
        active: prefs.layout === l.id,
        onClick: () => savePrefs({ layout: l.id }),
      })),
      { sep: true },
      { label: "Board settings…", icon: "gear", onClick: () => (S.modal = { kind: "forumSettings", forum }) },
    ]);
  }

  const layoutIcon = (id) => (id === "list" ? "list" : id === "gallery" ? "imagetext" : "screen");

  // ---- per-post tag picker -----------------------------------------------
  // A popover, not a dialog: choosing two chips is not a task that deserves a
  // modal, and anchoring it to the card keeps the post you're tagging in view.
  let tagPick = $state(null); // { post, chosen: string[], busy }
  syncLayer("picker", () => !!tagPick, () => (tagPick = null));

  function openTagPicker(p) {
    tagPick = { post: p, chosen: [...(p.tags || [])], busy: false };
  }
  function togglePicked(id) {
    if (!tagPick) return;
    const on = tagPick.chosen.includes(id);
    if (!on && tagPick.chosen.length >= TAG_LIMITS.perPost) return;
    tagPick = { ...tagPick, chosen: on ? tagPick.chosen.filter((x) => x !== id) : [...tagPick.chosen, id] };
  }
  async function saveTags() {
    if (!tagPick) return;
    tagPick = { ...tagPick, busy: true };
    try {
      await api.setPostTags(guild.id, tagPick.post.id, tagPick.chosen);
      tagPick = null;
      refresh({ quiet: true });
    } catch (err) {
      flash(err);
      tagPick = { ...tagPick, busy: false };
    }
  }

  // ---- header art ---------------------------------------------------------
  // "Auto" derives a two-tone wash from the forum's ID: every forum is
  // recognisably its own place with nothing stored and nothing synced. A chosen
  // banner overrides it — read from the CHANNEL record, not from the board
  // prefs: the art moved onto the channel when it became shared (SetForumBanner),
  // and the settings dialog stopped writing prefs.banner, so a hero that kept
  // reading the prefs showed every pick as doing nothing.
  const wash = $derived(washFor(forum.id));
  const art = $derived(forum.banner || "");

  // The FAB opens a modal ~200ms later; without the tap confirmation the press
  // feels dropped.
  const newPost = () => {
    haptic("light");
    S.modal = { kind: "newPost", forum };
  };
</script>

<svelte:window onkeydown={onWindowKey} />

<div class="board" bind:this={root}>
  <!-- HEADER. The brief's "title needs to be cleaner": one identity block —
       art, glyph, name, description, and the three numbers that describe the
       place — instead of an icon, an h2 and a grey rule. -->
  <header class="hero">
    <Banner
      banner={art}
      color={art ? "" : wash.color}
      color2={art ? "" : wash.color2}
      style={{ angle: wash.angle }}
      scale={0.7}
    />
    <!-- Two layers over the art, in this order. The sheen lights one corner so a
         plain gradient reads as a designed surface; the scrim on top of it is the
         CONTRAST GUARANTEE, not decoration — white ink over SCRIM_FLOOR clears
         4.5:1 even if the art underneath is pure white, which forum.test.mjs
         composites and asserts. All hero text sits below the 38% stop, where the
         gradient has reached that floor. -->
    <span class="hero-sheen" aria-hidden="true"></span>
    <span class="hero-scrim" aria-hidden="true"></span>
    <div class="hero-in">
      <div class="hero-title">
        <span class="hero-glyph"><Icon name="forum" size={18} /></span>
        <div class="hero-words">
          <h1>{forum.name}</h1>
          <p class="hero-topic">
            {forum.topic || "Every post is its own thread — ask, answer, and it stays tidy."}
          </p>
        </div>
      </div>
      <div class="hero-foot">
        <div class="hero-stats">
          {#if phase === "ready"}
            <span class="stat"><b>{stats.total}</b> {stats.total === 1 ? "post" : "posts"}</span>
          {/if}
          {#if stats.unanswered}
            <!-- Three pills, identical to the eye, and on a phone only one of
                 them used to be tappable — a 24px target between two inert
                 lookalikes, whose job the "Unanswered" chip in the toolbar
                 already does. On touch this one is a statistic like its
                 neighbours; the chip below is the filter. -->
            {#if S.isMobile}
              <span class="stat"><b>{stats.unanswered}</b> unanswered</span>
            {:else}
              <button
                class="stat"
                class:on={unansweredOnly}
                aria-pressed={unansweredOnly}
                onclick={() => (unansweredOnly = !unansweredOnly)}
              >
                <b>{stats.unanswered}</b> unanswered
              </button>
            {/if}
          {/if}
          {#if stats.pinned}
            <span class="stat"><Icon name="pin" size={11} /> <b>{stats.pinned}</b> pinned</span>
          {/if}
        </div>
        <div class="hero-acts">
          <button
            class="glass"
            aria-label="Board settings"
            title="Board settings"
            onclick={() => (S.modal = { kind: "forumSettings", forum })}
          >
            <Icon name="gear" size={15} />
          </button>
          {#if !S.isMobile}
            <button class="cta" onclick={newPost}>
              <Icon name="plus" size={14} /> <span>New Post</span>
            </button>
          {/if}
        </div>
      </div>
    </div>
  </header>

  <!-- TOOLBAR. Sticky, because a board is a place you scroll and losing the
       search box at post nine is the moment it stops being interactive. -->
  <div class="bar">
    <div class="bar-row">
      <div class="find">
        <Icon name="search" size={14} />
        <input
          bind:this={searchEl}
          bind:value={query}
          type="text"
          placeholder="Search posts"
          aria-label="Search posts"
          spellcheck="false"
        />
        {#if query}
          <button class="clear" aria-label="Clear search" onclick={() => (query = "")}>
            <Icon name="close" size={12} />
          </button>
        {/if}
      </div>

      {#if S.isMobile}
        <!-- One control instead of six: on a 390px screen the knobs live behind
             a menu that presents as a bottom sheet. -->
        <button class="pill" onclick={(e) => boardMenu(e.currentTarget)}>
          <Icon name={layoutIcon(prefs.layout)} size={14} />
          <span>View</span>
        </button>
      {:else}
        <div class="seg" role="group" aria-label="Sort posts">
          {#each SORTS as s (s.id)}
            <button
              class="seg-b"
              class:on={prefs.sort === s.id}
              aria-pressed={prefs.sort === s.id}
              title={s.label}
              onclick={() => savePrefs({ sort: s.id })}
            >
              <Icon name={s.icon} size={13} />
              <span>{s.short}</span>
            </button>
          {/each}
        </div>
        <div class="seg" role="group" aria-label="Card layout">
          {#each LAYOUTS as l (l.id)}
            <button
              class="seg-b icon"
              class:on={prefs.layout === l.id}
              aria-pressed={prefs.layout === l.id}
              aria-label={`${l.label} layout`}
              title={`${l.label} — ${l.hint}`}
              onclick={() => savePrefs({ layout: l.id })}
            >
              <Icon name={layoutIcon(l.id)} size={14} />
            </button>
          {/each}
        </div>
      {/if}
    </div>

    {#if palette.length || filtering || stats.unanswered}
      <!-- MobileShell listens for touchstart on the whole shell and claims any
           mostly-horizontal drag as a drawer swipe. On a phone this row IS a
           horizontal scroller, so its touches have to stop here or every flick
           through the tags would pull the channel drawer open instead. -->
      <div
        class="bar-row chips"
        role="group"
        aria-label="Filter posts"
        ontouchstart={(e) => e.stopPropagation()}
      >
        <button
          class="chip"
          class:on={unansweredOnly}
          aria-pressed={unansweredOnly}
          onclick={() => (unansweredOnly = !unansweredOnly)}
        >
          <Icon name="spark" size={11} /> Unanswered
        </button>
        {#each chips as t (t.id)}
          <button
            class="chip tag"
            class:on={tagFilter.includes(t.id)}
            aria-pressed={tagFilter.includes(t.id)}
            style="--tc:{t.color}"
            onclick={() => toggleTag(t.id)}
          >
            {#if t.emoji}<span class="chip-em">{t.emoji}</span>{/if}
            {t.name}
          </button>
        {/each}
        {#if palette.length > CHIP_FOLD}
          <button class="chip ghost" onclick={() => (tagsExpanded = !tagsExpanded)}>
            {tagsExpanded ? "Fewer" : `+${palette.length - CHIP_FOLD} more`}
          </button>
        {/if}
        {#if filtering}
          <button class="chip ghost" onclick={clearFilters}>
            <Icon name="close" size={11} /> Clear
          </button>
        {/if}
      </div>
    {/if}
  </div>

  <!-- BODY -->
  {#if phase === "loading"}
    <!-- Skeletons rather than a spinner: the board's shape appears first, so the
         layout doesn't jump when the posts land. -->
    <div class="posts list" aria-busy="true">
      {#each [0, 1, 2, 3] as i (i)}
        <div class="card skel" style="--i:{i}">
          <div class="sk-media"></div>
          <div class="sk-lines">
            <span class="sk-line w70"></span>
            <span class="sk-line w90"></span>
            <span class="sk-line w40"></span>
          </div>
        </div>
      {/each}
    </div>
  {:else if phase === "error"}
    <div class="state">
      <div class="state-badge bad"><Icon name="alert" size={26} /></div>
      <h3>This board didn't load</h3>
      <p class="muted">{errMsg}</p>
      <button onclick={() => refresh()}>Try again</button>
    </div>
  {:else if !posts.length}
    <div class="state">
      <EmptyState
        icon="forum"
        headline="Nothing here yet"
        sub="Start the first post — it becomes its own thread, with its own unread badge."
      />
      <button class="cta solid" onclick={newPost}><Icon name="plus" size={14} /> New Post</button>
    </div>
  {:else if !visible.length}
    <div class="state">
      <div class="state-badge"><Icon name="search" size={24} /></div>
      <h3>No posts match</h3>
      <p class="muted">
        {#if query}Nothing matches “{query}”{:else}No post carries those filters{/if}
        — {posts.length} {posts.length === 1 ? "post is" : "posts are"} hidden.
      </p>
      <button onclick={clearFilters}>Clear filters</button>
    </div>
  {:else}
    <ul class="posts {prefs.layout}">
      {#each visible as p, i (p.id)}
        {@const pv = postPreview(p.excerpt)}
        {@const tok = firstImage(p)}
        {@const tile = tileFor(p.id, p.title)}
        {@const tags = resolveTags(p.tags, palette)}
        {@const unread = S.unread[p.id]}
        {@const pending = isPending(p)}
        {@const mem = p.authorFingerprint ? memberByFpr(p.authorFingerprint) : null}
        {@const m = media[p.id]}
        {@const shot = tok && m?.state === "ok" ? m.src : ""}
        <li
          class="card"
          class:pinned={p.pinned}
          class:unread={!!unread?.count}
          class:pending
          class:has-shot={!!shot}
          style="--i:{Math.min(i, 10)};--tile-a:{tile.color};--tile-b:{tile.color2};--tile-ang:{tile.angle}deg"
          animate:flip={{ duration: flipMs, easing: cubicOut }}
          use:longpress={{ handler: (e) => postMenu(e.target?.closest?.(".card") || root, p) }}
        >
          <!-- The whole card is clickable, but only ONE thing in it is a
               control: a full-bleed button under the content. That keeps the ⋯
               menu from being a button inside a button, gives the card a real
               focus ring (:focus-within), and makes Enter work for free.
               Right-click is the mouse's way in; the long-press on the <li>
               above is the finger's, because iOS never fires contextmenu on a
               held finger and Android fires it with no haptic. -->
          <button
            class="hit"
            aria-label={`Open post: ${p.title}`}
            onclick={() => selectChannel(p.id)}
            oncontextmenu={coarse
              ? (e) => e.preventDefault()
              : (e) => {
                  e.preventDefault();
                  postMenu(e.currentTarget, p);
                }}
          ></button>

          <div class="media" use:watchMedia={tok} data-post-id={p.id}>
            {#if shot}
              <img src={shot} alt="" loading="lazy" />
            {:else}
              <!-- Designed first, because most posts land here: not a grey hole
                   but the post's own colour and initial, derived from its id so
                   it looks the same on every device. -->
              <span class="tile">{tile.letter}</span>
              {#if tok}
                <span class="tile-load" class:err={m?.state === "err"} aria-hidden="true">
                  <Icon name={m?.state === "err" ? "alert" : "imagetext"} size={14} />
                </span>
              {:else if pv.kind === "file"}
                <span class="tile-load" aria-hidden="true"><Icon name="attach" size={14} /></span>
              {:else if pv.kind === "poll"}
                <span class="tile-load" aria-hidden="true"><Icon name="poll" size={14} /></span>
              {/if}
            {/if}
          </div>

          <div class="body">
            <div class="head">
              <h3 class="title">{p.title}</h3>
              <span class="flags">
                {#if p.pinned}
                  <span class="flag pin" title="Pinned to the top of this board">
                    <Icon name="pin" size={10} /> Pinned
                  </span>
                {/if}
                {#if p.solved}
                  <span class="flag ok" title="Marked answered">
                    <Icon name="check" size={10} /> Answered
                  </span>
                {/if}
                {#if unread?.count}
                  <span class="flag new" title="{unread.count} unread">
                    {unread.count > 99 ? "99+" : unread.count} new
                  </span>
                {/if}
              </span>
            </div>

            {#if pending}
              <!-- A post's channel record and its opening message are two
                   separate gossip frames, so this state is normal and brief. It
                   says what is happening instead of claiming "0 replies · just
                   now" or a post by nobody. -->
              <p class="pend"><span class="pend-bar"></span><span class="pend-bar short"></span></p>
              <div class="meta"><span class="muted">Syncing this post…</span></div>
            {:else}
              {#if pv.text}
                <p class="excerpt">{pv.text}</p>
              {:else if pv.kind}
                <p class="excerpt faint">
                  {pv.kind === "image" ? "Image post" : pv.kind === "file" ? "File" : "Poll"}
                </p>
              {:else}
                <p class="excerpt faint">No preview available</p>
              {/if}

              {#if tags.length}
                <div class="tags">
                  {#each tags as t (t.id)}
                    <span class="chip tag sm" style="--tc:{t.color}">
                      {#if t.emoji}<span class="chip-em">{t.emoji}</span>{/if}{t.name}
                    </span>
                  {/each}
                </div>
              {/if}

              <div class="meta">
                <span class="who">
                  <Avatar
                    name={mem?.name || p.authorName || p.authorFingerprint}
                    emoji={mem?.emoji || ""}
                    color={mem?.color || ""}
                    image={mem?.avatar || ""}
                    size={S.isMobile ? 22 : 18}
                  />
                  <span class="wname">{nameFor(p.authorFingerprint, p.authorName)}</span>
                </span>
                <span class="dot" aria-hidden="true">·</span>
                <span class="mi" title="{p.replies} {p.replies === 1 ? 'reply' : 'replies'}">
                  <Icon name="reply" size={11} /> {p.replies}
                </span>
                <span class="dot" aria-hidden="true">·</span>
                <span class="mi" title={absTime(p.lastActivity)}>{relTime(p.lastActivity)}</span>
              </div>
            {/if}
          </div>

          <button
            class="more"
            aria-label="Post actions"
            title="Post actions"
            onclick={(e) => postMenu(e.currentTarget, p)}
          >
            <Icon name="dots" size={14} />
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<!-- Mobile: starting a post is the point of a forum, so it gets a thumb-reachable
     target that survives scrolling. -->
{#if S.isMobile}
  <button class="fab" aria-label="New post" onclick={newPost}><Icon name="plus" size={20} /></button>
{/if}

{#snippet pickBody()}
  <p class="pick-sub muted">{tagPick.post.title}</p>
  <div class="pick-list">
    {#each palette as t (t.id)}
      {@const on = tagPick.chosen.includes(t.id)}
      <button
        class="pick-row"
        class:on
        disabled={!on && tagPick.chosen.length >= TAG_LIMITS.perPost}
        style="--tc:{t.color}"
        onclick={() => togglePicked(t.id)}
      >
        <span class="swatch"></span>
        <span class="pick-name">{t.emoji ? `${t.emoji} ` : ""}{t.name}</span>
        {#if on}<Icon name="check" size={14} />{/if}
      </button>
    {/each}
  </div>
  <div class="pick-acts">
    <button class="ghost" onclick={() => (tagPick = null)}>Cancel</button>
    <button disabled={tagPick.busy} onclick={saveTags}>Save</button>
  </div>
{/snippet}

{#if tagPick}
  {#if S.isMobile}
    <!-- The one picker in the app that was built by hand rather than routed
         through BottomSheet, so on a phone it rendered as a 320px card floating
         mid-screen with no grip, no fling-to-dismiss and no safe-area padding.
         Same body, the app's own touch presentation. -->
    <BottomSheet
      title={`Tags · ${tagPick.chosen.length}/${TAG_LIMITS.perPost}`}
      onClose={() => (tagPick = null)}
    >
      {@render pickBody()}
    </BottomSheet>
  {:else}
    <!-- Centred dialog rather than an anchored popover: a card can be anywhere
         in a scrolling list, so anchoring would put the picker off-screen. -->
    <div class="pick-wrap" role="presentation" onclick={() => (tagPick = null)}>
      <div class="pick" role="presentation" onclick={(e) => e.stopPropagation()}>
        <div class="pick-head">
          <strong>Tags</strong>
          <span class="muted">{tagPick.chosen.length}/{TAG_LIMITS.perPost}</span>
        </div>
        {@render pickBody()}
      </div>
    </div>
  {/if}
{/if}

<style>
  /* ---- shell ------------------------------------------------------------ */
  .board {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    /* One rhythm for the whole board: every gap and pad below is a multiple of
       4, and the two horizontal insets are the same number. --sp-edge tightens
       itself on a phone, so the board no longer needs a breakpoint to do it. */
    --gap: var(--sp-3);
    --pad: var(--sp-edge);
    /* Placeholder fill for skeletons and the pending card. NOT --bg-3: in the
       dark theme --bg-3 IS --bg-elevated, so a bar painted in it on a card was
       perfectly invisible. Derived from the ink instead, which is guaranteed to
       contrast with whatever surface the pack puts behind it. */
    --ph: color-mix(in srgb, var(--text) 14%, transparent);
    padding-bottom: 28px;
  }

  /* ---- hero ------------------------------------------------------------- */
  .hero {
    position: relative;
    isolation: isolate;
    margin: var(--pad) var(--pad) 0;
    border-radius: var(--radius-lg);
    overflow: hidden;
    min-height: 172px;
    display: flex;
    align-items: flex-end;
    border: 1px solid var(--border);
  }
  /* Banner renders its own root; place it as the art layer. */
  .hero :global(.bnr) {
    position: absolute;
    inset: 0;
    z-index: 0;
  }
  /* Below the scrim on purpose: it brightens the ART, and the scrim on top of it
     is what carries the measured contrast floor. Above the scrim it would eat
     into that guarantee. */
  .hero-sheen {
    position: absolute;
    inset: 0;
    z-index: 1;
    pointer-events: none;
    background:
      radial-gradient(90% 120% at 8% -10%, rgba(255, 255, 255, 0.26), rgba(255, 255, 255, 0) 62%),
      radial-gradient(70% 90% at 100% 110%, rgba(0, 0, 0, 0.3), rgba(0, 0, 0, 0) 70%);
  }
  .hero-scrim {
    position: absolute;
    inset: 0;
    z-index: 2;
    pointer-events: none;
    /* 0.62 by 38% of the height — the floor forum.test.mjs proves. Every word in
       the hero sits below that stop. */
    background:
      linear-gradient(180deg, rgba(0, 0, 0, 0.18) 0%, rgba(0, 0, 0, 0.62) 38%, rgba(0, 0, 0, 0.8) 100%),
      linear-gradient(90deg, rgba(0, 0, 0, 0.3) 0%, rgba(0, 0, 0, 0) 62%);
  }
  .hero-in {
    position: relative;
    z-index: 3;
    width: 100%;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .hero-title {
    display: flex;
    align-items: flex-start;
    gap: 10px;
  }
  .hero-glyph {
    flex: none;
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    border-radius: var(--radius-md);
    color: #fff;
    background: rgba(0, 0, 0, 0.34);
    border: 1px solid rgba(255, 255, 255, 0.18);
  }
  .hero-words {
    min-width: 0;
  }
  .hero h1 {
    margin: 0;
    font-size: var(--fs-display);
    line-height: 1.15;
    font-weight: 700;
    letter-spacing: -0.01em;
    color: #fff;
    /* Static, not animated: a 1px shadow buys legibility on a busy preset for
       no per-frame cost. */
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.5);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hero-topic {
    margin: 3px 0 0;
    font-size: var(--fs-ui);
    line-height: 1.45;
    /* 0.88 white over the proven floor still clears 4.5:1; it separates the
       description from the name without inventing a second colour. */
    color: rgba(255, 255, 255, 0.88);
    text-shadow: 0 1px 2px rgba(0, 0, 0, 0.45);
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
  }
  .hero-foot {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--gap);
    flex-wrap: wrap;
  }
  .hero-stats {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  /* Stat pills DARKEN the art rather than lightening it: a translucent white
     pill raises the backdrop's luminance and eats into the contrast the scrim
     just guaranteed. Dark glass can only ever improve it. */
  .stat {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 4px 9px;
    border-radius: 999px;
    font-size: var(--fs-small);
    color: rgba(255, 255, 255, 0.9);
    background: rgba(0, 0, 0, 0.34);
    border: 1px solid rgba(255, 255, 255, 0.16);
  }
  .stat b {
    color: #fff;
    font-weight: 700;
  }
  button.stat {
    cursor: pointer;
    transition:
      transform var(--dur-standard) var(--ease-spring),
      background var(--dur-standard) ease;
  }
  @media (pointer: fine) {
    button.stat:hover {
      transform: translateY(-1px);
      background: rgba(0, 0, 0, 0.5);
    }
  }
  button.stat.on {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: transparent;
  }
  button.stat.on b {
    color: var(--accent-fg);
  }
  .hero-acts {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin-left: auto;
  }
  .glass {
    position: relative;
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    padding: 0;
    border-radius: var(--radius-md);
    color: #fff;
    background: rgba(0, 0, 0, 0.34);
    border: 1px solid rgba(255, 255, 255, 0.18);
    transition:
      transform var(--dur-standard) var(--ease-spring),
      background var(--dur-standard) ease;
  }
  /* On a phone this is the ONLY visible control in the hero (New Post is the
     FAB), and it opens the whole settings sheet. A 32px glass square is right
     for the art it sits on, so the paint stays 32 and the target grows around
     it: 32 + 2×6 = 44, without moving anything in the hero. */
  .glass::before {
    content: "";
    position: absolute;
    inset: -6px;
  }
  @media (pointer: fine) {
    .glass:hover {
      background: rgba(0, 0, 0, 0.52);
      transform: translateY(-1px);
    }
  }
  .cta {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: none;
    padding: 8px 15px;
    border-radius: 999px;
    font-weight: 600;
    box-shadow: var(--accent-glow);
    transition: transform var(--dur-standard) var(--ease-spring);
  }
  @media (pointer: fine) {
    .cta:hover {
      transform: translateY(-1px);
    }
  }

  /* ---- toolbar --------------------------------------------------------- */
  .bar {
    position: sticky;
    top: 0;
    z-index: 6;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding: 10px var(--pad);
    /* Opaque: it slides over cards. Matches the chat column's own surface. */
    background: var(--bg-2);
    border-bottom: 1px solid var(--border);
  }
  .bar-row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .bar-row.chips {
    flex-wrap: wrap;
    gap: 6px;
  }
  .find {
    position: relative;
    display: flex;
    align-items: center;
    gap: 7px;
    flex: 1;
    min-width: 0;
    padding: 0 10px;
    height: 34px;
    border-radius: 999px;
    background: var(--bg-3);
    border: 1px solid transparent;
    color: var(--text-muted);
    transition:
      border-color var(--dur-standard) ease,
      background var(--dur-standard) ease;
  }
  .find:focus-within {
    border-color: var(--accent);
    background: var(--bg-1);
  }
  .find input {
    flex: 1;
    min-width: 0;
    background: transparent;
    border: none;
    outline: none;
    color: var(--text);
    font-size: var(--fs-ui);
    padding: 0;
  }
  .clear {
    position: relative;
    display: grid;
    place-items: center;
    padding: 3px;
    border-radius: 50%;
    background: var(--bg-2);
    color: var(--text-muted);
  }
  /* An 18px dot inside a pill — the paint has to stay small or it crowds the
     query, so the target is grown around it instead. */
  .clear::before {
    content: "";
    position: absolute;
    inset: -13px;
  }
  .clear:hover {
    color: var(--text);
  }
  .seg {
    display: flex;
    flex: none;
    padding: 2px;
    gap: 2px;
    border-radius: 999px;
    background: var(--bg-3);
  }
  .seg-b {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 5px 10px;
    border-radius: 999px;
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-compact);
    font-weight: 600;
    transition:
      background var(--dur-standard) ease,
      color var(--dur-standard) ease;
  }
  .seg-b.icon {
    padding: 5px 8px;
  }
  .seg-b:hover {
    color: var(--text);
  }
  .seg-b.on {
    background: var(--bg-1);
    color: var(--text);
  }
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: none;
    padding: 0 13px;
    height: 34px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text);
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  /* ---- chips ----------------------------------------------------------- */
  /* A tag's colour IDENTIFIES the chip; the theme's ink READS it. A member picks
     that colour, so painting the label in it would be a contrast lottery — the
     tint is bounded (TINT_ALPHA) and forum.test.mjs composites the worst case
     over both card surfaces to prove --text still clears 4.5:1. */
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 4px 10px;
    border-radius: 999px;
    font-size: var(--fs-compact);
    font-weight: 600;
    background: var(--bg-3);
    color: var(--text-muted);
    border: 1px solid transparent;
    transition:
      transform var(--dur-standard) var(--ease-spring),
      background var(--dur-standard) ease,
      color var(--dur-standard) ease;
  }
  @media (pointer: fine) {
    button.chip:hover {
      color: var(--text);
      transform: translateY(-1px);
    }
  }
  .chip.tag {
    background: color-mix(in srgb, var(--tc) 16%, var(--bg-2));
    color: var(--text);
  }
  .chip.tag.on {
    background: color-mix(in srgb, var(--tc) 22%, var(--bg-2));
    border-color: color-mix(in srgb, var(--tc) 60%, transparent);
  }
  @media (pointer: fine) {
    button.chip.tag:hover {
      background: color-mix(in srgb, var(--tc) 22%, var(--bg-2));
      border-color: color-mix(in srgb, var(--tc) 60%, transparent);
    }
  }
  .chip.on {
    background: var(--accent-soft);
    color: var(--text);
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
  }
  .chip.ghost {
    background: transparent;
    border-color: var(--border);
  }
  .chip.sm {
    padding: 2px 8px;
    font-size: var(--fs-small);
    background: color-mix(in srgb, var(--tc) 20%, var(--bg-elevated));
  }
  .chip-em {
    font-size: var(--fs-small);
    line-height: 1;
  }

  /* ---- post lists ------------------------------------------------------ */
  .posts {
    list-style: none;
    margin: 0;
    padding: var(--gap) var(--pad) 0;
    display: grid;
    gap: var(--gap);
  }
  .posts.list {
    grid-template-columns: 1fr;
  }
  .posts.gallery {
    grid-template-columns: repeat(auto-fill, minmax(196px, 1fr));
  }
  .posts.cover {
    grid-template-columns: repeat(auto-fill, minmax(264px, 1fr));
  }

  .card {
    position: relative;
    isolation: isolate;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    display: grid;
    /* Cards settle in on a stagger, capped so a long board doesn't take a
       second to appear. Transform + opacity only. */
    animation: card-in 0.3s var(--ease-out) both;
    animation-delay: calc(var(--i, 0) * 26ms);
    transition:
      transform var(--dur-standard) var(--ease-spring),
      border-color var(--dur-standard) ease;
  }
  @keyframes card-in {
    from {
      opacity: 0;
      transform: translateY(8px) scale(0.995);
    }
  }
  /* Hover only where a hover can end. Android's WebView applies :hover on tap
     and holds it until you tap somewhere else, so a bare rule left the post you
     just came back from permanently lifted, outlined and zoomed — reading as
     "this row is stuck selected". */
  @media (pointer: fine) {
    .card:hover {
      transform: translateY(-2px);
      border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
    }
    .card:hover .media img {
      transform: scale(1.05);
    }
  }
  .card:focus-within {
    border-color: var(--accent);
    /* An outline, not a shadow: no repaint of the card's whole box. */
    outline: 2px solid color-mix(in srgb, var(--accent) 55%, transparent);
    outline-offset: -1px;
  }
  .card.pinned {
    border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
  }
  /* Unread earns the accent, and only unread: one meaning per colour. */
  .card.unread::after {
    content: "";
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 3px;
    background: var(--accent);
    z-index: 3;
  }
  .hit {
    position: absolute;
    inset: 0;
    z-index: 1;
    padding: 0;
    border: none;
    background: transparent;
    cursor: pointer;
  }
  .hit:focus-visible {
    outline: none; /* the ring is drawn on the card via :focus-within */
  }
  .more {
    position: relative;
    z-index: 2;
    align-self: start;
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    margin: 8px 8px 0 0;
    padding: 0;
    border-radius: var(--radius-md);
    background: transparent;
    color: var(--text-faint);
    opacity: 0;
    transition:
      opacity var(--dur-standard) ease,
      background var(--dur-standard) ease,
      color var(--dur-standard) ease;
  }
  .card:hover .more,
  .card:focus-within .more,
  .more:focus-visible {
    opacity: 1;
  }
  .more:hover {
    background: var(--bg-3);
    color: var(--text);
  }

  /* Media. A thumbnail, not a backdrop, in the list layout: at 88px the art
     cannot carry text, and a title on a solid surface stays scannable down a
     column of twenty. The "cover" layout is where art goes behind words, and it
     is the layout with the room to do it safely. */
  .media {
    position: relative;
    overflow: hidden;
    background: linear-gradient(var(--tile-ang), var(--tile-a), var(--tile-b));
    display: grid;
    place-items: center;
  }
  .media img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    /* Compositor-only zoom on hover — see .card:hover .media img. */
    transition: transform 0.35s var(--ease-out);
    animation: shot-in 0.24s ease both;
  }
  @keyframes shot-in {
    from {
      opacity: 0;
    }
  }
  .tile {
    font-size: 26px;
    font-weight: 800;
    color: rgba(255, 255, 255, 0.5);
    letter-spacing: -0.02em;
    user-select: none;
  }
  .tile-load {
    position: absolute;
    right: 5px;
    bottom: 5px;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    border-radius: var(--radius-sm);
    color: rgba(255, 255, 255, 0.85);
    background: rgba(0, 0, 0, 0.42);
    /* A pulse, not a spinner: opacity only, and it says "coming" rather than
       "busy". */
    animation: pulse 1.6s ease-in-out infinite;
  }
  .tile-load.err {
    animation: none;
    color: #fff;
    background: rgba(0, 0, 0, 0.62);
  }
  @keyframes pulse {
    0%,
    100% {
      opacity: 0.45;
    }
    50% {
      opacity: 1;
    }
  }

  .body {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--sp-2);
  }
  .title {
    margin: 0;
    /* A URL or a 130-character word has to break somewhere, and the card's edge
       is not somewhere. */
    overflow-wrap: anywhere;
    font-size: var(--fs-body);
    font-weight: 650;
    line-height: 1.3;
    color: var(--text);
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
  }
  .flags {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: none;
  }
  .flag {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 2px 7px;
    border-radius: 999px;
    font-size: var(--fs-tiny);
    font-weight: 700;
    letter-spacing: 0.02em;
    white-space: nowrap;
  }
  .flag.pin {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .flag.ok {
    background: var(--ok-soft);
    color: var(--ok-text);
  }
  .flag.new {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .excerpt {
    margin: 0;
    overflow-wrap: anywhere;
    font-size: var(--fs-ui);
    line-height: 1.5;
    color: var(--text-muted);
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    overflow: hidden;
  }
  .excerpt.faint {
    font-style: italic;
    color: var(--text-faint);
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .who {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .wname {
    max-width: 160px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text);
    font-weight: 600;
  }
  .mi {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
  }
  .dot {
    color: var(--text-faint);
  }
  .muted {
    color: var(--text-muted);
  }

  /* Pending: the opening message hasn't arrived. Bars, not numbers. */
  .pend {
    margin: 2px 0 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .pend-bar {
    height: 9px;
    border-radius: var(--radius-sm);
    background: var(--ph);
    width: 82%;
    animation: pulse 1.7s ease-in-out infinite;
  }
  .pend-bar.short {
    width: 44%;
    animation-delay: 0.2s;
  }
  .card.pending .title {
    color: var(--text);
  }

  /* ---- layout: list ---------------------------------------------------- */
  .posts.list .card {
    grid-template-columns: 92px 1fr auto;
    align-items: stretch;
  }
  .posts.list .media {
    /* A fixed column, so twenty titles start on the same vertical line. */
    width: 92px;
    align-self: stretch;
    min-height: 84px;
    border-radius: 0;
  }
  .posts.list .body {
    /* The card is a grid with NO column gap and the media column is a
       full-bleed image, so a zero left padding put the title literally against
       the edge of the artwork. It reads as a rendering fault rather than a
       layout — most visible on a narrow window, where the text column is
       tightest, but it was wrong at every width. */
    padding: 11px var(--sp-2) 11px var(--sp-3);
  }

  /* ---- layout: gallery ------------------------------------------------- */
  .posts.gallery .card,
  .posts.cover .card {
    grid-template-columns: 1fr auto;
    grid-template-rows: auto 1fr;
  }
  .posts.gallery .media {
    /* Row 1 EXPLICITLY: .more owns row 1 / column 2, and a spanning item can't
       auto-place into a row that is already partly taken — it silently fell to
       row 2 and left a dead band across the top of every card. */
    grid-area: 1 / 1 / 2 / -1;
    aspect-ratio: 16 / 10;
    width: 100%;
  }
  .posts.gallery .body {
    grid-area: 2 / 1 / 3 / -1;
    padding: 11px 12px 12px;
  }
  .posts.gallery .wname,
  .posts.cover .wname {
    max-width: 84px;
  }
  .posts.gallery .more,
  .posts.cover .more {
    grid-column: 2;
    grid-row: 1;
    margin: 8px 8px 0 0;
    color: #fff;
    background: rgba(0, 0, 0, 0.42);
    opacity: 0;
  }

  /* ---- layout: cover --------------------------------------------------- */
  /* The brief's "background images … so we have boxes". The art fills the card
     and the words sit on it — which is only safe because the same measured scrim
     as the hero goes between them. */
  .posts.cover .card {
    min-height: 188px;
    grid-template-rows: 1fr;
  }
  .posts.cover .media {
    grid-area: 1 / 1 / 2 / 3;
    width: 100%;
    height: 100%;
  }
  .posts.cover .media::after {
    content: "";
    position: absolute;
    inset: 0;
    background: linear-gradient(
      180deg,
      rgba(0, 0, 0, 0.1) 0%,
      rgba(0, 0, 0, 0.62) 46%,
      rgba(0, 0, 0, 0.82) 100%
    );
  }
  .posts.cover .tile {
    position: absolute;
    right: -4px;
    top: 46%;
    transform: translateY(-50%);
    font-size: 104px;
    line-height: 0.8;
    /* Faint enough to be a watermark: at 0.5 it fought the title it sits behind,
       which is the one thing a cover card cannot afford. */
    color: rgba(255, 255, 255, 0.16);
  }
  .posts.cover .body {
    grid-area: 1 / 1 / 2 / 3;
    z-index: 2;
    align-self: end;
    padding: 12px 14px 13px;
    /* The text has to sit ABOVE the art, which puts it above the full-bleed hit
       button too — so it must not swallow the click. Nothing inside .body is
       interactive, so this costs nothing and keeps the whole card clickable. */
    pointer-events: none;
  }
  .posts.cover .title,
  .posts.cover .excerpt,
  .posts.cover .meta,
  .posts.cover .wname,
  .posts.cover .dot {
    color: #fff;
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.5);
  }
  .posts.cover .excerpt {
    color: rgba(255, 255, 255, 0.9);
  }
  .posts.cover .excerpt.faint {
    color: rgba(255, 255, 255, 0.72);
  }
  .posts.cover .chip.sm {
    background: rgba(0, 0, 0, 0.44);
    color: #fff;
    border: 1px solid color-mix(in srgb, var(--tc) 70%, transparent);
  }
  .posts.cover .flag.ok,
  .posts.cover .flag.pin {
    background: rgba(0, 0, 0, 0.5);
    color: #fff;
  }
  .posts.cover .card.unread::after {
    z-index: 3;
  }

  /* ---- skeletons + empty/error states ---------------------------------- */
  .card.skel {
    grid-template-columns: 92px 1fr;
    animation: card-in 0.3s ease both;
    animation-delay: calc(var(--i, 0) * 60ms);
  }
  .sk-media {
    width: 92px;
    min-height: 84px;
    background: var(--ph);
    animation: pulse 1.6s ease-in-out infinite;
  }
  .sk-lines {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding: 14px 14px 14px 12px;
  }
  .sk-line {
    height: 10px;
    border-radius: var(--radius-sm);
    background: var(--ph);
    animation: pulse 1.6s ease-in-out infinite;
  }
  .sk-line.w70 {
    width: 70%;
  }
  .sk-line.w90 {
    width: 90%;
    animation-delay: var(--dur-standard);
  }
  .sk-line.w40 {
    width: 40%;
    animation-delay: 0.3s;
  }

  .state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
    text-align: center;
    padding: 32px 24px 48px;
  }
  .state h3 {
    margin: 4px 0 0;
    font-size: var(--fs-title);
  }
  .state p {
    margin: 0;
    max-width: 42ch;
    font-size: var(--fs-ui);
    line-height: 1.55;
  }
  .state button {
    margin-top: 6px;
  }
  .state .cta.solid {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 9px 18px;
    border-radius: 999px;
    box-shadow: var(--accent-glow);
  }
  .state-badge {
    display: grid;
    place-items: center;
    width: 60px;
    height: 60px;
    border-radius: 50%;
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .state-badge.bad {
    background: var(--danger-soft);
    color: var(--danger-text);
  }
  /* ---- tag picker ------------------------------------------------------- */
  .pick-wrap {
    position: fixed;
    inset: 0;
    z-index: 120;
    display: grid;
    place-items: center;
    background: rgba(0, 0, 0, 0.5);
    animation: fade-in var(--dur-quick) ease;
  }
  @keyframes fade-in {
    from {
      opacity: 0;
    }
  }
  .pick {
    width: 320px;
    max-width: 92vw;
    max-height: 78vh;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding: 14px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    box-shadow: var(--shadow-pop);
    animation: pick-in 0.2s var(--ease-spring);
  }
  @keyframes pick-in {
    from {
      opacity: 0;
      transform: translateY(10px) scale(0.97);
    }
  }
  .pick-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    font-size: var(--fs-ui);
  }
  .pick-sub {
    margin: -4px 0 2px;
    font-size: var(--fs-compact);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pick-list {
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .pick-row {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 8px 10px;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    font-size: var(--fs-ui);
    text-align: left;
  }
  @media (pointer: fine) {
    .pick-row:hover:not(:disabled) {
      background: var(--bg-3);
      color: var(--text);
    }
  }
  .pick-row.on {
    background: color-mix(in srgb, var(--tc) 20%, var(--bg-elevated));
    color: var(--text);
  }
  .pick-row:disabled {
    opacity: 0.45;
  }
  .swatch {
    flex: none;
    width: 12px;
    height: 12px;
    border-radius: var(--radius-sm);
    background: var(--tc);
  }
  .pick-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pick-acts {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin-top: 2px;
  }

  /* ---- mobile ---------------------------------------------------------- */
  .fab {
    position: fixed;
    right: 16px;
    bottom: calc(18px + var(--safe-bottom));
    z-index: 40;
    display: grid;
    place-items: center;
    width: 54px;
    height: 54px;
    padding: 0;
    border-radius: 50%;
    box-shadow: var(--float-shadow);
    transition: transform var(--dur-standard) var(--ease-spring);
  }
  .fab:active {
    transform: scale(0.94);
  }

  /* THE phone query — the same condition S.isMobile uses, so the FAB is never
     drawn under a layout that hasn't made room for it. It previously rendered
     from 768px down while its clearance lived at 620px, which left every
     viewport in between (a phone in landscape, a 768px tablet) with an opaque
     54px circle parked on the last card's ⋯ button. */
  @media (pointer: coarse), (max-width: 768px) {
    .board {
      /* The FAB's own offset is 18px + the home indicator, and it is 54px tall,
         so a flat 88px left it sitting ~18px over the last card on any
         gesture-nav phone. Reserve what the FAB actually occupies. */
      padding-bottom: calc(88px + var(--safe-bottom));
    }
    .hero {
      min-height: 148px;
    }
    .hero h1 {
      /* --fs-display would GROW the name on a phone; the hero is the screen
         with the fewest lines to spare. Two clamped lines at title size beat
         one ellipsised line — the name was truncating at ~17 characters with a
         whole empty line underneath it. */
      font-size: var(--fs-title);
      white-space: normal;
      display: -webkit-box;
      -webkit-box-orient: vertical;
      -webkit-line-clamp: 2;
      line-clamp: 2;
    }
    .hero-topic {
      -webkit-line-clamp: 1;
      line-clamp: 1;
    }
    .posts.list .card {
      grid-template-columns: 72px 1fr auto;
    }
    .posts.list .media {
      width: 72px;
      min-height: 76px;
    }
    .wname {
      max-width: 110px;
    }
    /* The card's metadata layer is the desktop scale in 63% of the width: at
       393px the text column is ~247px, so author / replies / time wrapped onto
       a second line of fine print. The tokens grow it; one line keeps the card
       the height it was. */
    .meta {
      flex-wrap: nowrap;
      overflow: hidden;
    }
    /* Touch has no hover, so the ⋯ can't hide behind one. The gallery/cover
       rule is listed separately because it outranks a bare `.more` — the ⋯ on
       those two layouts stayed at opacity 0 on a phone while remaining fully
       hit-testable, which is worse than hidden: an invisible control sitting on
       the corner of every card. */
    .more,
    .posts.gallery .more,
    .posts.cover .more {
      opacity: 1;
    }
    .more {
      width: 40px;
      height: 40px;
    }
    .cta,
    .pill {
      min-height: var(--tap-min);
    }
    /* +2 for the pill's own border, so the INPUT inside it clears the touch
       floor rather than the wrapper doing so at the field's expense. */
    .find {
      min-height: calc(var(--tap-min) + 2px);
    }
    /* 32px was the old compromise for a row that had to fit two lines of chips
       inside a sticky bar. The row is one flickable line now, so the targets can
       be the real size — and the bar's height stops growing as you add filters,
       which is what actually cost the board space. */
    .chip {
      min-height: var(--tap-min);
    }
    .pick-row {
      min-height: var(--tap-min);
    }
    /* The sheet's Cancel/Save were the smallest targets in the picker — the
       one row the coarse floor never reached. */
    .pick-acts {
      gap: var(--sp-3);
    }
    .pick-acts button {
      flex: 1;
      min-height: var(--tap-min);
    }
    /* iOS zooms the page when a focused field is under 16px and never zooms
       back; --fs-ui is 14 here, so the board's search box needs the floor
       spelled out. Same trick MobileShell applies to its own search. */
    .find input {
      font-size: 16px;
      /* The pill is 44px on touch, but the INPUT inside it was 21px and the
         rest of the pill is an inert div — so four fifths of what looks like a
         search box did nothing when tapped. Stretch the field to fill it. */
      align-self: stretch;
    }
    /* Both measured under the touch floor (41x41 and 39x39 of effective hit
       area). The ⋯ sits on the corner of every card and the glass buttons ride
       the banner, so a miss lands on the card behind them. */
    .glass,
    .more {
      width: var(--tap-min);
      height: var(--tap-min);
    }
    /* The toolbar is sticky, so its height is a permanent tax on the board. A
       wrapping chip row cost two lines (~70px) of a ~770px pane before the
       first card; one flickable line costs 34px and reaches every tag rather
       than the first four. */
    .bar-row.chips {
      flex-wrap: nowrap;
      overflow-x: auto;
      overscroll-behavior-x: contain;
      -webkit-overflow-scrolling: touch;
      scrollbar-width: none;
      /* Bleed to the screen edges so the row reads as "there is more this way"
         instead of ending inside a margin. */
      margin: 0 calc(var(--pad) * -1);
      padding: 0 var(--pad);
    }
    .bar-row.chips::-webkit-scrollbar {
      display: none;
    }
    .bar-row.chips .chip {
      flex: none;
      white-space: nowrap;
    }
    /* Touch feedback the hover rules can no longer provide. */
    .card:active {
      transform: scale(0.985);
    }
  }
  /* The genuine narrow floor: at 360px a 72px thumbnail leaves ~200px for a
     title, two lines of excerpt and a metadata row. */
  @media (max-width: 400px) {
    .posts.list .card {
      grid-template-columns: 64px 1fr auto;
    }
    .posts.list .media {
      width: 64px;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .card,
    .card.skel,
    .ghost-card,
    .media img,
    .tile-load,
    .sk-media,
    .sk-line,
    .pend-bar,
    .pick,
    .pick-wrap {
      animation: none;
    }
    .card,
    .media img,
    .cta,
    .glass,
    button.stat,
    .chip,
    .fab {
      transition: none;
    }
    .card:hover,
    .cta:hover,
    .glass:hover,
    button.stat:hover,
    button.chip:hover {
      transform: none;
    }
    .card:hover .media img {
      transform: none;
    }
  }
</style>
