<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  let name = $state("");
  let pending = $state(null); // { dataURI } after picking an image
  let fileInput;
  let busy = $state(false);

  // Downscale to a 64px PNG (keeps transparency) — small enough to distribute.
  async function pick(file) {
    if (!file || !file.type.startsWith("image/")) {
      flash("Pick an image");
      return;
    }
    try {
      const bmp = await createImageBitmap(file);
      const size = 64;
      const canvas = document.createElement("canvas");
      canvas.width = canvas.height = size;
      const ctx = canvas.getContext("2d");
      const scale = Math.min(size / bmp.width, size / bmp.height);
      const w = bmp.width * scale;
      const h = bmp.height * scale;
      ctx.drawImage(bmp, (size - w) / 2, (size - h) / 2, w, h);
      pending = { dataURI: canvas.toDataURL("image/png") };
      if (!name) name = (file.name || "").replace(/\.[^.]+$/, "").toLowerCase().replace(/[^a-z0-9_]/g, "").slice(0, 32);
    } catch {
      flash("Couldn't read that image");
    }
  }

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

  async function remove(n) {
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
    square.
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
      {:else}
        <Icon name="plus" size={18} />
      {/if}
    </button>
    <span class="colon">:</span>
    <input class="name-in" bind:value={name} maxlength="32" placeholder="name" />
    <span class="colon">:</span>
    <button onclick={add} disabled={busy || !pending || !name.trim()}>Add</button>
  </div>

  <div class="list">
    {#each g?.emoji ?? [] as e (e.name)}
      <div class="item" title=":{e.name}:">
        <img src={e.image} alt=":{e.name}:" />
        <span class="ename">:{e.name}:</span>
        <button class="rm" aria-label="Remove :{e.name}:" onclick={() => remove(e.name)}>
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

<style>
  .lead {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
  }
  .lead code {
    background: var(--bg-0);
    padding: 1px 5px;
    border-radius: 4px;
    font-size: 12px;
  }
  .add-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .preview {
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
    font-size: 13px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 240px;
    overflow-y: auto;
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
    font-family: ui-monospace, monospace;
    font-size: 13px;
  }
  .rm {
    background: transparent;
    color: var(--danger);
    padding: 4px 6px;
    opacity: 0;
  }
  .item:hover .rm {
    opacity: 1;
  }
  .empty {
    font-size: 13px;
    padding: 8px;
  }
</style>
