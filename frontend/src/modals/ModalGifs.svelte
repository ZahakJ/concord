<script>
  // The GIF picker: a guild's own pack, searched locally.
  //
  // There is no Tenor/Giphy call here and there never will be — a hosted GIF
  // search sends every keystroke and your IP to Google or Shutterstock, which
  // is the exact thing this app exists to avoid. A guild builds its own
  // collection instead; the records travel P2P to its members and the images
  // ride the ordinary encrypted-attachment path.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, flash, refreshGuilds } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { loadAttachment } from "../lib/attachments.js";
  import { PERM, has } from "../lib/perms.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const canManage = $derived(has(g?.myPerms || 0, PERM.MANAGE_GUILD));

  let gifs = $state([]);
  let query = $state("");
  let busy = $state(false);
  let sending = $state("");
  let adding = $state(false); // the add form is open
  let pending = $state(null); // { dataUrl, w, h, bytes } after picking a file
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
    if (!chId) return;
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
</script>

<Modal title="GIFs — {g?.name ?? ''}" {onClose} wide>
  <div class="bar">
    <span class="find"><Icon name="search" size={14} /></span>
    <!-- svelte-ignore a11y_autofocus -->
    <input
      class="q"
      autofocus
      bind:value={query}
      placeholder="Search this guild's GIFs…"
      aria-label="Search GIFs"
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
          This guild has no GIFs yet — add the first one.
        {:else}
          This guild has no GIFs yet. Someone who can manage it can add some.
        {/if}
      </p>
    {/each}
  </div>

  <p class="muted foot">
    Shared by this guild's members, peer to peer. Nothing is searched or fetched from Tenor, Giphy
    or anywhere else.
  </p>
</Modal>

<style>
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
</style>
