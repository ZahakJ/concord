<script>
  // The GIF picker. Two tabs, two different privacy stories, both stated on
  // screen because a picker that implies more privacy than it delivers is worse
  // than one that offers less.
  //
  //   This server — the guild's own pack. Records travel P2P to members, images
  //     ride the encrypted-attachment path, and searching it is a substring
  //     match over a list already in memory. Nothing leaves the machine.
  //
  //   Search — Tenor, fetched BY THE USER'S OWN RENDEZVOUS. The rendezvous sees
  //     the search terms; Google sees only the rendezvous. Critically, the
  //     images come through it too: a result carries an opaque handle, never a
  //     URL, and every thumbnail and full GIF arrives as an inline data URL from
  //     the Go side. If anything here ever put a tenor.com address into an
  //     <img src>, every member's browser would connect to Google and the tab's
  //     privacy claim would become a lie. Don't.
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

  // Decrypted previews, keyed by blob id. `started` is a plain Set, not state:
  // it only guards against firing the same fetch twice, and making it reactive
  // would re-run this effect on every resolution.
  let srcs = $state({});
  let failed = $state({});
  const started = new Set();
  $effect(() => {
    const chId = S.activeChannelId;
    if (!chId || tab !== "pack") return;
    for (const x of filtered) {
      if (started.has(x.id)) continue;
      started.add(x.id);
      loadAttachment(chId, { blobId: x.id, keys: x.keys, subtype: x.subtype })
        .then((src) => (srcs[x.id] = src))
        .catch(() => (failed[x.id] = true));
    }
  });

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

  let sq = $state(""); // what's in the search box, NOT yet sent anywhere
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
  let saving = $state("");

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
  const source = $derived(status?.source || "Tenor");

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
        return "Your rendezvous is reachable but has no GIF API key, so it can't search Tenor. Whoever runs it can set CONCORD_TENOR_KEY on the rendezvous to turn this on.";
      case "rate_limited":
        return "Your rendezvous is limiting GIF requests right now. Wait a few seconds and try again.";
      case "expired":
        return "Those results went stale — the rendezvous restarted since you searched. Search again.";
      case "upstream":
        return `Your rendezvous couldn't reach the GIF service${st.detail ? ` (${st.detail})` : ""}. That's between it and Tenor; nothing on your machine is wrong.`;
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

  // Thumbnails come back as inline data URLs from the Go side — the browser
  // issues no request of its own. A small pool rather than 24 at once: each one
  // is a round trip through the rendezvous, and flooding it just makes the
  // first row slower to appear.
  async function loadThumbs(list) {
    const queue = [...list];
    const worker = async () => {
      for (;;) {
        const x = queue.shift();
        if (!x) return;
        try {
          thumbs[x.id] = await api.gifSearchMedia(x.preview, false);
        } catch {
          thumbFailed[x.id] = true;
        }
      }
    };
    await Promise.all([worker(), worker(), worker(), worker()]);
  }

  // Runs only on submit — Enter or the button. Deliberately not debounced-as-
  // you-type: that would hand the rendezvous a query for every prefix of what
  // was typed, including the ones the user thought better of.
  async function runSearch(more = false) {
    const q = sq.trim();
    if (!q || searching) return;
    searching = true;
    try {
      const res = await api.searchGifs(q, more ? next : "");
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
      hits = more ? [...hits, ...(res.results || [])] : res.results || [];
      next = res.next || "";
      searched = true;
      if (!more) {
        thumbs = {};
        thumbFailed = {};
      }
      await loadThumbs(res.results || []);
    } catch (err) {
      status = { status: "unreachable", detail: String(err) };
    } finally {
      searching = false;
    }
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
    const t = (x.title || sent || "gif").replace(/[ -]/g, " ").trim();
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
      <input
        class="q"
        autofocus
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

    <div class="grid" class:empty={filtered.length === 0}>
      {#each filtered as x (x.id)}
        <div class="cell">
          <button
            class="tile"
            disabled={!S.activeChannelId || sending === x.id}
            title={x.name}
            onclick={() => send(x)}
          >
            {#if srcs[x.id]}
              <img src={srcs[x.id]} alt={x.name} />
            {:else if failed[x.id]}
              <span class="ph">offline</span>
            {:else}
              <span class="ph shimmer"></span>
            {/if}
            <span class="cap">{x.name}</span>
          </button>
          {#if canManage}
            <button class="rm" aria-label="Remove {x.name}" title="Remove" onclick={() => remove(x)}>
              <Icon name="trash" size={12} />
            </button>
          {/if}
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
      {/each}
    </div>

    <p class="muted foot">
      Shared by this server's members, peer to peer. Searching this tab sends nothing anywhere — the
      filter runs on GIFs already on your machine.
    </p>
  {:else}
    <div class="bar">
      <span class="find"><Icon name="search" size={14} /></span>
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="q"
        autofocus
        bind:value={sq}
        disabled={!usable}
        placeholder={usable ? "Search Tenor via your rendezvous…" : "Search unavailable"}
        aria-label="Search Tenor through your rendezvous"
        onkeydown={(e) => {
          if (e.key === "Enter") runSearch(false);
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
      <div class="grid" class:empty={hits.length === 0}>
        {#each hits as x (x.id)}
          <div class="cell">
            <button
              class="tile"
              disabled={!S.activeChannelId || sending === x.id}
              title={x.title || "GIF"}
              onclick={() => sendHit(x)}
            >
              {#if thumbs[x.id]}
                <img src={thumbs[x.id]} alt={x.title || "GIF"} />
              {:else if thumbFailed[x.id]}
                <span class="ph">no preview</span>
              {:else}
                <span class="ph shimmer"></span>
              {/if}
              <span class="cap">{x.title || "GIF"}</span>
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
        {:else}
          <p class="muted none">
            {#if searching}
              Asking your rendezvous…
            {:else if searched}
              Your rendezvous found nothing for “{sent}”.
            {:else}
              Type something and press Enter. Nothing is sent until you do.
            {/if}
          </p>
        {/each}
      </div>

      {#if next && hits.length > 0}
        <button class="more" onclick={() => runSearch(true)} disabled={searching}>
          {searching ? "Loading…" : "More results"}
        </button>
      {/if}
    {/if}

    <!-- Phrased in the present tense only when it is actually happening: a
         "proxied by …" line under a tab that cannot search would be claiming a
         protection nothing is currently exercising. -->
    <p class="muted foot">
      {#if usable}
        Results from {source}, fetched by <strong>your rendezvous</strong> — it sees your search
        terms. Google sees only your rendezvous, never you: the pictures come through it too, so
        your browser never connects to Tenor.{#if status?.via}{" "}Proxied by
          <code>{status.via.slice(0, 12)}…</code>.{/if}
        Sending one posts it as an ordinary encrypted attachment, so nobody else fetches it from
        Tenor either.
      {:else}
        When this works, results come from Tenor fetched by <strong>your rendezvous</strong> — it
        would see your search terms, Google would see only it, and the pictures would come through
        it too so your browser never connects to Tenor. Right now nothing is being sent anywhere.
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
    font-size: 13px;
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
    font-size: 13px;
  }
  .addbtn,
  .go {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12.5px;
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
    font-size: 12.5px;
  }
  .notice {
    margin: 8px 0 0;
    padding: 8px 10px;
    font-size: 12.5px;
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
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    gap: 8px;
    max-height: 46vh;
    overflow-y: auto;
    /* Room for the remove button to sit proud of the tile. */
    padding: 2px;
  }
  .grid.empty {
    display: block;
  }
  .cell {
    position: relative;
  }
  .tile {
    width: 100%;
    padding: 0;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    overflow: hidden;
    display: block;
    cursor: pointer;
  }
  .tile:hover:not(:disabled) {
    border-color: var(--accent);
  }
  .tile img {
    display: block;
    width: 100%;
    height: 90px;
    object-fit: cover;
    background: var(--bg-3);
  }
  .ph {
    display: grid;
    place-items: center;
    height: 90px;
    font-size: 11px;
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
  .cap {
    display: block;
    padding: 4px 6px;
    font-size: 11.5px;
    text-align: left;
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
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
  .more {
    margin-top: 8px;
    width: 100%;
    font-size: 12.5px;
  }
  .none {
    font-size: 13px;
    padding: 18px 8px;
    text-align: center;
  }
  .foot {
    margin: 0;
    font-size: 11.5px;
    line-height: 1.5;
  }
  .foot code {
    font-size: 11px;
  }
</style>
