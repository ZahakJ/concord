<script>
  import Modal from "./Modal.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { isAnimated } from "../lib/animated.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  let name = $state("");
  let pending = $state(null); // { dataURI } after picking an image
  let fileInput;
  let busy = $state(false);

  // The backend's whitelist already accepts GIF and WebP (internal/app/emoji.go),
  // but a canvas only ever holds ONE frame — drawing an animated GIF through it
  // and calling toDataURL("image/png") silently throws the animation away. So an
  // animated source is passed through byte-for-byte instead, and only static
  // images take the downscale path. Re-encoding an animation in the browser
  // would mean shipping a GIF encoder, which is exactly the kind of dependency
  // this app doesn't take.
  const MAX_BYTES = 256 << 10; // must match maxEmojiBytes in internal/app/emoji.go

  async function pick(file) {
    if (!file || !file.type.startsWith("image/")) {
      flash("Pick an image", "error");
      return;
    }
    try {
      const bytes = new Uint8Array(await file.arrayBuffer());
      if (isAnimated(bytes)) {
        const uri = await asDataURI(file);
        if (uri.length > MAX_BYTES) {
          // We can't shrink it for them, so say what would fix it.
          flash(
            `That animation is ${Math.round(uri.length / 1024)} KB — the limit is ${MAX_BYTES / 1024} KB. Try fewer frames or a smaller size.`,
            "error",
          );
          return;
        }
        pending = { dataURI: uri, animated: true };
        nameFrom(file);
        return;
      }
      const bmp = await createImageBitmap(file);
      const size = 64;
      const canvas = document.createElement("canvas");
      canvas.width = canvas.height = size;
      const ctx = canvas.getContext("2d");
      const scale = Math.min(size / bmp.width, size / bmp.height);
      const w = bmp.width * scale;
      const h = bmp.height * scale;
      ctx.drawImage(bmp, (size - w) / 2, (size - h) / 2, w, h);
      pending = { dataURI: canvas.toDataURL("image/png"), animated: false };
      nameFrom(file);
    } catch {
      flash("Couldn't read that image", "error");
    }
  }

  function nameFrom(file) {
    if (!name)
      name = (file.name || "").replace(/\.[^.]+$/, "").toLowerCase().replace(/[^a-z0-9_]/g, "").slice(0, 32);
  }

  const asDataURI = (file) =>
    new Promise((res, rej) => {
      const r = new FileReader();
      r.onload = () => res(String(r.result));
      r.onerror = rej;
      r.readAsDataURL(file);
    });


  async function add() {
    if (busy || !pending) return;
    busy = true;
    try {
      await api.addCustomEmoji(g.id, name.trim(), pending.dataURI);
      await refreshGuilds();
      name = "";
      pending = null;
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }

  // Removing an emoji is guild-wide and breaks every :name: already typed, so
  // it asks first — the trash button used to be a one-tap unlabelled hit target
  // that a phone could not even see (.rm below).
  let confirmRm = $state("");

  async function remove(n) {
    confirmRm = "";
    try {
      await api.removeCustomEmoji(g.id, n);
      await refreshGuilds();
    } catch (err) {
      flash(err);
    }
  }
</script>

<Modal title="Guild emoji — {g?.name ?? ''}" {onClose}>
  <p class="muted lead">
    Upload emoji anyone in this guild can use by typing <code>:name:</code>. Keep them small and
    square. Animated GIF and WebP keep moving — they're passed through as-is, so they have to
    arrive under 256&nbsp;KB rather than being shrunk for you.
  </p>

  <div class="add-row">
    <input
      type="file"
      accept="image/*"
      bind:this={fileInput}
      style="display:none"
      onchange={(e) => {
        pick(e.target.files?.[0]);
        e.target.value = "";
      }}
    />
    <button class="preview" onclick={() => fileInput.click()} title="Choose image">
      {#if pending}
        <img src={pending.dataURI} alt="new emoji" />
        {#if pending.animated}
          <span class="gif" title="Animation preserved">GIF</span>
        {/if}
      {:else}
        <Icon name="plus" size={18} />
      {/if}
    </button>
    <span class="colon">:</span>
    <input dir="auto" class="name-in" bind:value={name} maxlength="32" placeholder="name" />
    <span class="colon">:</span>
    <button onclick={add} disabled={busy || !pending || !name.trim()}>Add</button>
  </div>

  <div class="list">
    {#each g?.emoji ?? [] as e (e.name)}
      <div class="item" title=":{e.name}:">
        <img src={e.image} alt=":{e.name}:" />
        <span class="ename">:{e.name}:</span>
        <button class="rm" aria-label="Remove :{e.name}:" onclick={() => (confirmRm = e.name)}>
          <Icon name="trash" size={13} />
        </button>
      </div>
    {:else}
      <p class="muted empty">No custom emoji yet — add your first above.</p>
    {/each}
  </div>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Done</button>
  </div>
</Modal>

{#if confirmRm}
  <ConfirmDialog
    title="Remove :{confirmRm}:?"
    body="Nobody in this guild will be able to use it, and messages that already do will show the plain text."
    confirmLabel="Remove"
    onConfirm={() => remove(confirmRm)}
    onClose={() => (confirmRm = "")}
  />
{/if}

<style>
  .lead {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.5;
  }
  .lead code {
    background: var(--bg-0);
    padding: 1px 5px;
    border-radius: 4px;
    font-size: var(--fs-compact);
  }
  .add-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  /* Corner badge on the preview, so it's obvious the animation survived the
     upload rather than being silently flattened as it used to be. */
  .gif {
    position: absolute;
    right: 0;
    bottom: 0;
    padding: 0 3px;
    font-size: 8px;
    font-weight: 700;
    letter-spacing: 0.3px;
    line-height: 12px;
    color: #fff;
    background: var(--accent);
    border-top-left-radius: 4px;
  }
  .preview {
    position: relative;
    width: 40px;
    height: 40px;
    flex-shrink: 0;
    display: grid;
    place-items: center;
    background: var(--bg-3);
    border: 1px dashed var(--border);
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    padding: 0;
    overflow: hidden;
  }
  .preview img {
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  .colon {
    color: var(--text-faint);
    font-family: ui-monospace, monospace;
  }
  .name-in {
    flex: 1;
    font-family: ui-monospace, monospace;
    font-size: var(--fs-ui);
    min-width: 0; /* the flex default keeps the field at its size attribute and pushes Add off the row */
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 240px;
    overflow-y: auto;
  }
  /* Nested scrollers inside a sheet that already scrolls are thumb traps — the
     sheet's own scroll covers this list. */
  @media (pointer: coarse), (max-width: 768px) {
    .list {
      max-height: none;
      overflow-y: visible;
    }
  }
  .item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 5px 8px;
    border-radius: var(--radius-sm);
  }
  .item:hover {
    background: var(--bg-3);
  }
  .item img {
    width: 28px;
    height: 28px;
    object-fit: contain;
  }
  .ename {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: ui-monospace, monospace;
    font-size: var(--fs-ui);
  }
  .rm {
    background: transparent;
    color: var(--danger-text);
    padding: 4px 6px;
  }
  /* Hidden-until-hover is a MOUSE affordance. Off a mouse, opacity:0 hides the
     control without removing it from hit-testing, so the phone got an invisible
     live "remove this emoji" target at the right edge of every row. */
  @media (pointer: fine) {
    .rm {
      opacity: 0;
    }
    .item:hover .rm,
    .item:focus-within .rm {
      opacity: 1;
    }
  }
  .rm:active {
    background: color-mix(in srgb, var(--danger) 18%, transparent);
    border-radius: var(--radius-sm);
  }
  .empty {
    font-size: var(--fs-ui);
    padding: 8px;
  }
</style>
