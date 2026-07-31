<script>
  // The GIF picker. Two tabs, two different privacy stories, both stated on
  // screen because a picker that implies more privacy than it delivers is worse
  // than one that offers less.
  //
  //   This server — the guild's own pack. Records travel P2P to members, images
  //     ride the encrypted-attachment path, and searching it is a substring
  //     match over a list already in memory. Nothing leaves the machine.
  //
  //   Search — a public GIF service, fetched BY THE USER'S OWN RENDEZVOUS. The
  //     rendezvous sees the search terms; the service sees only the rendezvous.
  //     Critically, the images come through it too: a result carries an opaque
  //     handle, never a URL, and every thumbnail and full GIF arrives as an
  //     inline data URL from the Go side. If anything here ever put a provider
  //     address into an <img src>, every member's browser would connect to that
  //     provider and the tab's privacy claim would become a lie. Don't.
  //
  //   WHICH service is the node's, not ours. This tab used to say "Tenor"
  //   everywhere; Google decommissioned the public Tenor API on 30 June 2026 and
  //   every one of those sentences became false at once — including the
  //   "unavailable" notice, which told people to go and get a key that could no
  //   longer be issued. The node now reports the provider it actually uses in
  //   `source`, and every sentence below is built from that. Do not put a vendor
  //   name back into this file.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, flash, refreshGuilds } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { loadAttachment } from "../lib/attachments.js";
  import { PERM, has } from "../lib/perms.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const canManage = $derived(has(g?.myPerms || 0, PERM.MANAGE_GUILD));

  let tab = $state("pack"); // pack | search

  let gifs = $state([]);
  let query = $state("");
  let busy = $state(false);
  let sending = $state("");
  let adding = $state(false); // the add form is open
  let pending = $state(null); // { dataUrl, w, h } after picking a file
  let newName = $state("");
  let newTags = $state("");
  let fileInput = $state(null);

  // Must match maxGifPlain in internal/app/gifs.go (the decoded byte count —
  // a data URL is ~4/3 of it, which is why the file size is what's checked).
  const MAX_BYTES = 5 << 20;

  // Search is a plain substring match over name and tags, run in memory over a
  // list we already hold. Deliberately dumb: nothing to send anywhere.
  const filtered = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return gifs;
    const terms = q.split(/\s+/);
    return gifs.filter((x) => {
      const hay = `${x.name} ${(x.tags || []).join(" ")}`.toLowerCase();
      return terms.every((t) => hay.includes(t));
    });
  });

  async function load() {
    if (!g) return;
    try {
      gifs = (await api.guildGifs(g.id)) || [];
    } catch (err) {
      flash(err);
    }
  }
  $effect(() => {
    if (g?.id) load();
  });

  // ---- The grid, shared by both tabs ----
  //
  // Masonry, not squares: GIFs are every shape, and cropping them all to one
  // 90px strip (the old grid) wasted half the sheet on letterboxing and cost a
  // caption row per tile besides. Tiles keep their own aspect ratio, so the
  // name moves into title/alt where screen readers and mouse hover still get
  // it, and roughly twice as many results fit the same height.
  //
  // Columns are balanced in JS (shortest column takes the next tile) rather
  // than CSS `columns`, because CSS columns reflow EVERY tile when a page of
  // results is appended — the grid the user was looking at would shuffle under
  // their finger. Balancing in order is deterministic: the same prefix of the
  // list always lands in the same place, so paging in more only ever adds.
  let gridW = $state(0);
  const nCols = $derived(Math.max(2, Math.min(5, Math.round(gridW / 150) || 2)));

  // One number for both the packer's height estimate and the tile's rendered
  // aspect-ratio — if they ever disagree, the column balance is a lie. The
  // clamp stops a 10:1 banner or a 1:10 strip from wrecking a column; 0x0
  // means the record didn't know, and 4:3 is the least-wrong guess.
  function ratio(x) {
    const r = x.w > 0 && x.h > 0 ? x.w / x.h : 4 / 3;
    return Math.min(2.2, Math.max(0.55, r));
  }
  function balance(list, n) {
    const cols = Array.from({ length: n }, () => []);
    const hts = Array(n).fill(0);
    for (const x of list) {
      let k = 0;
      for (let i = 1; i < n; i++) if (hts[i] < hts[k]) k = i;
      cols[k].push(x);
      hts[k] += 1 / ratio(x);
    }
    return cols;
  }
  const packColumns = $derived(balance(filtered, nCols));

  // Tiles fetch their image only when scrolled within a screen of view: one
  // observer per mounted grid (use:scroller on the container), tiles register
  // via use:lazy. A 100-result search therefore costs 100 round trips through
  // the rendezvous only if the user actually scrolls past all 100 — the old
  // grid fetched every thumbnail of every page up front.
  let io = null;
  const loaders = new Map();
  function scroller(node) {
    io?.disconnect();
    io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting) continue;
          const load = loaders.get(e.target);
          if (!load) continue;
          // A tile loads once; forgetting it here is what makes that true.
          loaders.delete(e.target);
          io?.unobserve(e.target);
          enqueue(load);
        }
      },
      { root: node, rootMargin: "240px 0px" },
    );
    // Tiles mount before their container's action runs — pick up strays.
    for (const n of loaders.keys()) io.observe(n);
    return {
      destroy() {
        io?.disconnect();
        io = null;
      },
    };
  }
  function lazy(node, load) {
    loaders.set(node, load);
    io?.observe(node);
    return {
      // Each render hands in a fresh closure; keep the newest, but never
      // resurrect a tile that already fired.
      update(l) {
        if (loaders.has(node)) loaders.set(node, l);
      },
      destroy() {
        loaders.delete(node);
        io?.unobserve(node);
      },
    };
  }

  // A small pool rather than firing everything that scrolls in at once: each
  // image is a round trip through the rendezvous (or a decrypt, on the pack
  // tab), and flooding it just makes the first row slower to appear.
  const fetchQ = [];
  let inflight = 0;
  function enqueue(f) {
    fetchQ.push(f);
    pump();
  }
  function pump() {
    while (inflight < 4 && fetchQ.length) {
      inflight++;
      Promise.resolve(fetchQ.shift()()).finally(() => {
        inflight--;
        pump();
      });
    }
  }

  // Decrypted pack previews, keyed by blob id. `started` is a plain Set, not
  // state: it only guards against firing the same fetch twice (a tile
  // re-registers every time its tab is re-opened).
  let srcs = $state({});
  let failed = $state({});
  const started = new Set();
  function loadPackThumb(x) {
    const chId = S.activeChannelId;
    if (!chId || started.has(x.id)) return;
    started.add(x.id);
    return loadAttachment(chId, { blobId: x.id, keys: x.keys, subtype: x.subtype })
      .then((src) => (srcs[x.id] = src))
      .catch(() => (failed[x.id] = true));
  }

  async function send(x) {
    if (sending) return;
    sending = x.id;
    try {
      // Posted as an ordinary image attachment message, so every client —
      // including ones that know nothing about GIF packs — renders it.
      await api.sendGuildGif(S.activeChannelId, x.id, S.replyingTo?.id || "");
      S.replyingTo = null;
      onClose();
    } catch (err) {
      flash(err);
    } finally {
      sending = "";
    }
  }

  async function pick(file) {
    if (!file || !file.type.startsWith("image/")) {
      flash("Pick an image", "error");
      return;
    }
    if (file.size > MAX_BYTES) {
      flash(`That's ${Math.round(file.size / (1 << 20))} MB — the limit is ${MAX_BYTES >> 20} MB`, "error");
      return;
    }
    try {
      // Read the file byte-for-byte. Drawing it through a canvas would flatten
      // an animated GIF to a single frame (the same trap ModalEmoji documents),
      // and re-encoding an animation in the browser would mean shipping a GIF
      // encoder — a dependency this app doesn't take.
      const dataUrl = await new Promise((res, rej) => {
        const r = new FileReader();
        r.onload = () => res(String(r.result));
        r.onerror = rej;
        r.readAsDataURL(file);
      });
      let w = 0;
      let h = 0;
      try {
        const bmp = await createImageBitmap(file);
        w = bmp.width;
        h = bmp.height;
        bmp.close?.();
      } catch {
        /* dimensions are only a layout hint; 0x0 means "unknown" */
      }
      pending = { dataUrl, w, h };
      if (!newName) newName = (file.name || "").replace(/\.[^.]+$/, "").slice(0, 64).trim();
    } catch {
      flash("Couldn't read that image", "error");
    }
  }

  async function add() {
    if (busy || !pending || !newName.trim()) return;
    busy = true;
    try {
      await api.addGuildGif(
        g.id,
        newName.trim(),
        newTags.split(/[,\s]+/).filter(Boolean),
        pending.dataUrl,
        pending.w,
        pending.h,
      );
      pending = null;
      newName = "";
      newTags = "";
      adding = false;
      await load();
      await refreshGuilds();
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }

  async function remove(x) {
    try {
      await api.removeGuildGif(g.id, x.id);
      await load();
    } catch (err) {
      flash(err);
    }
  }

  // ---- Search tab ----

  let sq = $state(""); // what's in the search box
  let sent = $state(""); // the terms actually handed to the rendezvous
  let hits = $state([]);
  let next = $state("");
  let searching = $state(false);
  let searched = $state(false); // a search has completed at least once
  // status: null until the tab's pre-flight probe answers. Every non-ok value
  // has to produce a sentence — an empty grid with no explanation is the one
  // outcome this tab is not allowed to have.
  let status = $state(null);
  // No rendezvous and no API key are properties of the deployment: retrying
  // cannot change them, so those are the only two that disable the controls.
  const DEAD_END = new Set(["no_rendezvous", "unavailable"]);
  let thumbs = $state({});
  let thumbFailed = $state({});
  const startedThumbs = new Set();
  let saving = $state("");
  const hitColumns = $derived(balance(hits, nCols));

  // The last SEARCH's failure, kept apart from the tab's own readiness.
  //
  // Folding them into one field made the tab brick itself: a transient reply —
  // a rate limit, a stale page token — overwrote the probe's verdict, which
  // disabled the input and the button and hid the results, while the notice
  // said "wait a few seconds and try again". Advice next to controls it has
  // just greyed out is worse than no advice, and the only way out was closing
  // the modal.
  let searchErr = $state(null);

  // Only the two states nothing in this tab can fix should take the controls
  // away. Everything else — unreachable, rate-limited, stale — is worth another
  // press of Enter, so the input stays live.
  const usable = $derived(status?.status === "ok" || !DEAD_END.has(status?.status));
  // The provider the NODE says it used. The fallback is deliberately generic:
  // guessing a vendor name is what made this tab lie once already, and an older
  // node that sends no source is a node we genuinely do not know this about.
  const source = $derived(status?.source || "the GIF service");

  // explain turns a status into the sentence shown in place of results. It also
  // says what to DO about it, because "unavailable" on its own tells the user
  // nothing they can act on.
  function explain(st) {
    if (!st) return "";
    switch (st.status) {
      case "no_rendezvous":
        return "You have no rendezvous configured, so there is nothing to search through. Add one under Settings → Connection — or use this server's own GIFs, which need no server at all.";
      case "unreachable":
        return "Your rendezvous didn't answer, so there is nothing to search through right now. This server's own GIFs still work — they come from members, not from a server.";
      case "unavailable":
        // The node's own detail names the provider it is configured for and the
        // variable to set — including, if that provider is Tenor, that its
        // public API no longer exists. Preferred over anything written here,
        // because only the node knows how it was configured.
        return st.detail
          ? `Your rendezvous is reachable but can't search: ${st.detail}.`
          : "Your rendezvous is reachable but has no GIF search key, so it can't search. Whoever runs it can set CONCORD_GIF_KEY on the rendezvous — with a key from developers.giphy.com — to turn this on.";
      case "rate_limited":
        return "Your rendezvous is limiting GIF requests right now. Wait a few seconds and try again.";
      case "expired":
        return "Those results went stale — the rendezvous restarted since you searched. Search again.";
      case "upstream":
        return `Your rendezvous couldn't reach ${source}${st.detail ? ` (${st.detail})` : ""}. That's between it and ${source}; nothing on your machine is wrong.`;
      case "bad_request":
        return st.detail || "That search couldn't be run.";
      default:
        return st.detail || "GIF search isn't working right now.";
    }
  }

  // Probed when the tab is first opened, so an unusable tab explains itself
  // BEFORE the user types rather than after they've handed over a query.
  async function probe() {
    if (status) return;
    try {
      status = await api.gifSearchStatus();
    } catch (err) {
      status = { status: "unreachable", detail: String(err) };
    }
  }
  $effect(() => {
    if (tab === "search") probe();
  });

  function loadHitThumb(x) {
    if (startedThumbs.has(x.id)) return;
    startedThumbs.add(x.id);
    return api
      .gifSearchMedia(x.preview, false)
      .then((d) => (thumbs[x.id] = d))
      .catch(() => (thumbFailed[x.id] = true));
  }

  // Search-as-you-type. The terms go to the user's OWN rendezvous and never to
  // the provider directly, so typing ahead exposes prefixes only to a machine
  // that already sees every query the user submits — the debounce exists to
  // keep half-typed junk and burst load off it, not to guard a secret. Two
  // characters before anything fires: a single letter is never the query the
  // user means, and each prefix is still a round trip for the rendezvous.
  let typeTimer = 0;
  function onType() {
    clearTimeout(typeTimer);
    const q = sq.trim();
    if (!q) {
      // Clearing the box returns the tab to rest AND invalidates any reply
      // still in flight — otherwise a slow search repopulates an emptied grid.
      gen++;
      searching = false;
      hits = [];
      next = "";
      sent = "";
      searched = false;
      searchErr = null;
      return;
    }
    if (q.length < 2) return;
    typeTimer = setTimeout(() => runSearch(false), 300);
  }
  $effect(() => () => clearTimeout(typeTimer));

  // With searches now overlapping (type, wait, type again), a slow reply must
  // not clobber a newer one: each request takes a generation, and only the
  // latest generation is allowed to touch state.
  let gen = 0;

  async function runSearch(more = false) {
    // Paging continues the query the results came FROM, not whatever has been
    // typed since — those results belong to `sent`.
    const q = more ? sent : sq.trim();
    if (!q) return;
    if (more && (!next || searching)) return;
    // Same terms, same results — unless the last attempt failed, in which case
    // Enter is the retry path every transient-failure notice points at.
    if (!more && q === sent && searched && !searchErr) return;
    const my = ++gen;
    searching = true;
    try {
      const res = await api.searchGifs(q, more ? next : "");
      if (my !== gen) return;
      // Keep source/via — they feed the provenance line — but do NOT let a
      // search result redefine whether the tab works. Only the probe does that.
      if (res.source) status = { ...(status || {}), source: res.source, via: res.via };
      if (res.status !== "ok") {
        searchErr = { status: res.status, detail: res.detail };
        if (!more) hits = [];
        return;
      }
      searchErr = null;
      sent = q;
      if (!more) {
        thumbs = {};
        thumbFailed = {};
        startedThumbs.clear();
      }
      // Providers repeat results across page boundaries now and then, and a
      // duplicate id would blow up the keyed {#each} — drop them.
      const seen = new Set((more ? hits : []).map((h) => h.id));
      const fresh = (res.results || []).filter((r) => {
        if (seen.has(r.id)) return false;
        seen.add(r.id);
        return true;
      });
      hits = more ? [...hits, ...fresh] : fresh;
      next = res.next || "";
      searched = true;
    } catch (err) {
      if (my === gen) status = { status: "unreachable", detail: String(err) };
    } finally {
      if (my === gen) searching = false;
    }
  }

  // The next page fetches itself when the tail of the grid scrolls near — but
  // the trigger stays a real, pressable button: a short page can leave it
  // visible with no new intersection ever firing, and that must not strand the
  // user.
  function autopage(node) {
    const obs = new IntersectionObserver(
      (es) => {
        if (es.some((e) => e.isIntersecting)) runSearch(true);
      },
      { root: node.closest(".grid"), rootMargin: "160px 0px" },
    );
    obs.observe(node);
    return { destroy: () => obs.disconnect() };
  }

  async function sendHit(x) {
    if (sending) return;
    sending = x.id;
    try {
      await api.sendSearchedGif(S.activeChannelId, x.full, S.replyingTo?.id || "", x.w || 0, x.h || 0);
      S.replyingTo = null;
      onClose();
    } catch (err) {
      flash(err);
    } finally {
      sending = "";
    }
  }

  // The name and tags a saved result gets. Both are validated again in Go
  // (validGuildGif); this keeps the common case from bouncing off that.
  function saveName(x) {
    const t = (x.title || sent || "gif").replace(/[ -]/g, " ").trim();
    return (t || "gif").slice(0, 64);
  }
  function saveTags() {
    return sent
      .toLowerCase()
      .split(/[^a-z0-9_-]+/)
      .filter((t) => /^[a-z0-9][a-z0-9_-]{0,31}$/.test(t))
      .slice(0, 12);
  }

  async function saveHit(x) {
    if (saving) return;
    saving = x.id;
    try {
      await api.saveSearchedGif(g.id, saveName(x), saveTags(), x.full, x.w || 0, x.h || 0);
      await load();
      await refreshGuilds();
      flash("Saved to this server's GIFs", "ok");
    } catch (err) {
      flash(err);
    } finally {
      saving = "";
    }
  }
</script>

<Modal title="GIFs — {g?.name ?? ''}" {onClose} wide>
  <div class="tabs" role="tablist">
    <button
      role="tab"
      aria-selected={tab === "pack"}
      class:on={tab === "pack"}
      onclick={() => (tab = "pack")}>This server</button
    >
    <button
      role="tab"
      aria-selected={tab === "search"}
      class:on={tab === "search"}
      onclick={() => (tab = "search")}>Search</button
    >
  </div>

  {#if tab === "pack"}
    <div class="bar">
      <span class="find"><Icon name="search" size={14} /></span>
      <!-- svelte-ignore a11y_autofocus -->
      <!-- Not on a phone: this tab is usually opened to LOOK at the twelve GIFs
           the server has, and the IME would cover the grid to no purpose. -->
      <input
        class="q"
        autofocus={!S.isMobile}
        bind:value={query}
        placeholder="Search this server's GIFs…"
        aria-label="Search this server's GIFs"
      />
      {#if canManage}
        <button class="addbtn" onclick={() => (adding = !adding)} title="Add a GIF to this guild">
          <Icon name="plus" size={14} /> Add
        </button>
      {/if}
    </div>

    {#if adding}
      <div class="add">
        <input
          type="file"
          accept="image/gif,image/webp,image/png,image/jpeg"
          bind:this={fileInput}
          style="display:none"
          onchange={(e) => {
            pick(e.target.files?.[0]);
            e.target.value = "";
          }}
        />
        <button class="drop" onclick={() => fileInput.click()} title="Choose a GIF">
          {#if pending}
            <img src={pending.dataUrl} alt="new GIF" />
          {:else}
            <Icon name="plus" size={18} />
          {/if}
        </button>
        <div class="fields">
          <input bind:value={newName} maxlength="64" placeholder="Name (e.g. cat vibing)" />
          <input bind:value={newTags} placeholder="Tags, space or comma separated" />
        </div>
        <button class="go" onclick={add} disabled={busy || !pending || !newName.trim()}>Add</button>
      </div>
    {/if}

    <div class="grid" bind:clientWidth={gridW} use:scroller>
      {#if filtered.length > 0}
        <div class="masonry">
          {#each packColumns as col, ci (ci)}
            <div class="col">
              {#each col as x (x.id)}
                <div class="cell">
                  <!-- The name lives in title/alt now, not a caption row: on
                       hover and to a screen reader it is still the name, and
                       the grid gets the row back. -->
                  <button
                    class="tile"
                    style:aspect-ratio={ratio(x)}
                    disabled={!S.activeChannelId || sending === x.id}
                    title={x.name}
                    use:lazy={() => loadPackThumb(x)}
                    onclick={() => send(x)}
                  >
                    {#if srcs[x.id]}
                      <img src={srcs[x.id]} alt={x.name} />
                    {:else if failed[x.id]}
                      <span class="ph">offline</span>
                    {:else}
                      <span class="ph shimmer"></span>
                    {/if}
                  </button>
                  {#if canManage}
                    <button class="rm" aria-label="Remove {x.name}" title="Remove" onclick={() => remove(x)}>
                      <Icon name="trash" size={12} />
                    </button>
                  {/if}
                </div>
              {/each}
            </div>
          {/each}
        </div>
      {:else}
        <p class="muted none">
          {#if query.trim()}
            Nothing matches “{query.trim()}”.
          {:else if canManage}
            This server has no GIFs yet — add the first one, or find one under Search.
          {:else}
            This server has no GIFs yet. Someone who can manage it can add some.
          {/if}
        </p>
      {/if}
    </div>

    <p class="muted foot">
      Shared by this server's members, peer to peer. Searching this tab sends nothing anywhere — the
      filter runs on GIFs already on your machine.
    </p>
  {:else}
    <div class="bar">
      <span class="find"><Icon name="search" size={14} /></span>
      <!-- svelte-ignore a11y_autofocus -->
      <!-- Same on this tab: the results grid sits below the box, and an IME on
           open leaves nothing to browse. Typing is still one tap away. -->
      <input
        class="q"
        autofocus={!S.isMobile}
        bind:value={sq}
        disabled={!usable}
        placeholder={usable ? `Search ${source} via your rendezvous…` : "Search unavailable"}
        aria-label="Search for GIFs through your rendezvous"
        enterkeyhint="search"
        oninput={onType}
        onkeydown={(e) => {
          if (e.key === "Enter") {
            clearTimeout(typeTimer);
            runSearch(false);
          }
        }}
      />
      <button class="go" onclick={() => runSearch(false)} disabled={!usable || searching || !sq.trim()}>
        {searching ? "Searching…" : "Search"}
      </button>
    </div>

    <!-- A search that failed is reported ahead of the tab's own state: it is the
         more recent and more specific thing that happened, and unlike the probe
         it does not take the controls away. -->
    {#if searchErr}
      <p class="notice warn">{explain(searchErr)}</p>
    {:else if status && status.status !== "ok"}
      <p class="notice" class:warn={status.status !== "unavailable"}>{explain(status)}</p>
    {/if}

    {#if usable}
      <div class="grid" bind:clientWidth={gridW} use:scroller>
        {#if hits.length > 0}
          <div class="masonry">
            {#each hitColumns as col, ci (ci)}
              <div class="col">
                {#each col as x (x.id)}
                  <div class="cell">
                    <button
                      class="tile"
                      style:aspect-ratio={ratio(x)}
                      disabled={!S.activeChannelId || sending === x.id}
                      title={x.title || "GIF"}
                      use:lazy={() => loadHitThumb(x)}
                      onclick={() => sendHit(x)}
                    >
                      {#if thumbs[x.id]}
                        <img src={thumbs[x.id]} alt={x.title || "GIF"} />
                      {:else if thumbFailed[x.id]}
                        <span class="ph">no preview</span>
                      {:else}
                        <span class="ph shimmer"></span>
                      {/if}
                    </button>
                    {#if canManage}
                      <button
                        class="rm save"
                        aria-label="Save {x.title || 'this GIF'} to this server's GIFs"
                        title="Save to this server's GIFs"
                        disabled={saving === x.id}
                        onclick={() => saveHit(x)}
                      >
                        <Icon name="plus" size={12} />
                      </button>
                    {/if}
                  </div>
                {/each}
              </div>
            {/each}
          </div>
          {#if next}
            <button class="more" use:autopage onclick={() => runSearch(true)} disabled={searching}>
              {searching ? "Loading…" : "More results"}
            </button>
          {/if}
        {:else}
          <p class="muted none">
            {#if searching}
              Asking your rendezvous…
            {:else if searched}
              Your rendezvous found nothing for “{sent}”.
            {:else}
              Results appear as you type. Nothing is sent anywhere until you type.
            {/if}
          </p>
        {/if}
      </div>
    {/if}

    <!-- Phrased in the present tense only when it is actually happening: a
         "proxied by …" line under a tab that cannot search would be claiming a
         protection nothing is currently exercising. -->
    <p class="muted foot">
      {#if usable}
        Results from {source}, fetched by <strong>your rendezvous</strong> — it sees your search
        terms. {source} sees only your rendezvous, never you: the pictures come through it too, so
        your browser never connects to {source}.{#if status?.via}{" "}Proxied by
          <code>{status.via.slice(0, 12)}…</code>.{/if}
        Sending one posts it as an ordinary encrypted attachment, so nobody else fetches it from
        {source} either.
      {:else}
        When this works, results come from a GIF service fetched by <strong>your rendezvous</strong>
        — it would see your search terms, the service would see only it, and the pictures would come
        through it too so your browser never connects to the service. Right now nothing is being
        sent anywhere.
      {/if}
    </p>
  {/if}
</Modal>

<style>
  .tabs {
    display: flex;
    gap: 4px;
    margin-bottom: 8px;
    border-bottom: 1px solid var(--border);
  }
  .tabs button {
    padding: 6px 10px;
    font-size: var(--fs-compact);
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: var(--text-muted);
    border-radius: 0;
  }
  .tabs button.on {
    color: var(--text);
    border-bottom-color: var(--accent);
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .find {
    color: var(--text-faint);
    display: grid;
    place-items: center;
  }
  .q {
    flex: 1;
    font-size: var(--fs-ui);
  }
  .addbtn,
  .go {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: var(--fs-compact);
    white-space: nowrap;
  }
  .add {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px;
    background: var(--bg-3);
    border-radius: var(--radius-sm);
  }
  .drop {
    width: 56px;
    height: 56px;
    flex: none;
    display: grid;
    place-items: center;
    padding: 0;
    overflow: hidden;
    background: var(--bg-0);
    border: 1px dashed var(--border);
    color: var(--text-muted);
    border-radius: var(--radius-sm);
  }
  .drop img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .fields {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .fields input {
    font-size: var(--fs-compact);
  }
  .notice {
    margin: 8px 0 0;
    padding: 8px 10px;
    /* Every failure state in this tab is explained here and nowhere else. */
    font-size: var(--fs-compact);
    line-height: 1.5;
    background: var(--bg-3);
    border-left: 2px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
  }
  .notice.warn {
    border-left-color: var(--warn, var(--accent));
  }
  .grid {
    margin-top: 8px;
    max-height: 52vh;
    overflow-y: auto;
    /* A flick that reaches the end of the results must stop there, not start
       dragging the sheet underneath — on a phone that gesture dismissed the
       whole modal mid-browse. */
    overscroll-behavior: contain;
    /* Room for the remove button to sit proud of the tile. */
    padding: 2px;
  }
  .masonry {
    display: flex;
    gap: 6px;
    align-items: flex-start;
  }
  .col {
    flex: 1 1 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .cell {
    position: relative;
  }
  .tile {
    width: 100%;
    /* aspect-ratio is set per tile from the record's own dimensions, so the
       placeholder occupies exactly the space the image will: nothing shifts
       when a thumbnail lands, and the column heights the packer predicted are
       the ones actually rendered. */
    padding: 0;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    display: grid;
    cursor: pointer;
  }
  .tile:hover:not(:disabled) {
    border-color: var(--accent);
  }
  .tile img {
    display: block;
    width: 100%;
    height: 100%;
    object-fit: cover;
    background: var(--bg-3);
  }
  .ph {
    display: grid;
    place-items: center;
    width: 100%;
    height: 100%;
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    background: var(--bg-3);
  }
  .shimmer {
    animation: pulse 1.2s ease-in-out infinite;
  }
  @keyframes pulse {
    50% {
      opacity: 0.45;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .shimmer {
      animation: none;
    }
  }
  .rm {
    position: absolute;
    top: 4px;
    right: 4px;
    padding: 3px 5px;
    background: rgba(0, 0, 0, 0.6);
    color: #fff;
    border-radius: 6px;
    opacity: 0;
  }
  .cell:hover .rm,
  .rm:focus-visible {
    opacity: 1;
  }
  /* This button is "Remove" in the pack tab and "Save to this server's GIFs" in
     the search tab, and a finger generates no hover — so on a phone two of the
     three management actions in this picker did not exist at all. Show it, and
     give it a target: 3px/5px around a 12px icon is ~22x18. */
  @media (pointer: coarse) {
    .rm {
      opacity: 1;
      min-width: 36px;
      min-height: 36px;
      display: grid;
      place-items: center;
      padding: 0;
      background: rgba(0, 0, 0, 0.72);
    }
    /* Destructive, sitting on a thumbnail whose whole face SENDS the GIF — so
       it has to look different from the save button that shares its class. */
    .rm:not(.save) {
      background: color-mix(in srgb, var(--danger) 78%, rgba(0, 0, 0, 0.8));
    }
  }
  .more {
    margin-top: 8px;
    width: 100%;
    min-height: var(--tap-min);
    font-size: var(--fs-compact);
  }
  .none {
    font-size: var(--fs-ui);
    padding: 18px 8px;
    text-align: center;
  }
  /* The longest and most consequential prose in the picker — the paragraph
     saying that search terms reach the rendezvous and the images are proxied.
     At the old flat 11.5px that was nine lines of sub-legible type on a 360px
     sheet, i.e. a privacy explanation nobody reads. --fs-small carries it to
     12.5px on a phone; the tab's claims are only worth making if they're read. */
  .foot {
    margin: 8px 0 0;
    font-size: var(--fs-small);
    line-height: 1.5;
  }
  .foot code {
    font-size: var(--fs-small);
  }
</style>
