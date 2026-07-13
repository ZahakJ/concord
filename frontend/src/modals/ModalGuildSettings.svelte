<script>
  // Edit a guild's identity: name, logo, banner (image or GIF), description.
  // Requires Manage Guild; the backend re-checks. Read-only-ish for others
  // (they just see the current values and can't save).
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { PERM, has } from "../lib/perms.js";

  let { onClose } = $props();

  const g = activeGuild();
  let name = $state(g?.name || "");
  let icon = $state(g?.icon || "");
  let banner = $state(g?.banner || "");
  let description = $state(g?.description || "");
  let busy = $state(false);

  const canEdit = has(g?.myPerms || 0, PERM.MANAGE_GUILD);
  const MAX = 500 * 1024;

  // Read an image file to a data URI. Kept raw (no canvas re-encode) so animated
  // GIF banners keep animating; rejected if too big for a gossip frame.
  function applyImageFile(file, setter) {
    if (!file || !file.type.startsWith("image/")) return;
    const reader = new FileReader();
    reader.onload = () => {
      if (String(reader.result).length > MAX) {
        flash("Image too large — keep it under ~350 KB", "error");
        return;
      }
      setter(reader.result);
    };
    reader.readAsDataURL(file);
  }
  function pickImage(setter) {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = "image/*";
    input.onchange = () => applyImageFile(input.files?.[0], setter);
    input.click();
  }

  // Paste an image straight into whichever target the pointer is over.
  let pasteTarget = $state("banner");
  function onPaste(e) {
    if (!canEdit) return;
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (!item) return;
    e.preventDefault();
    applyImageFile(item.getAsFile(), pasteTarget === "icon" ? (v) => (icon = v) : (v) => (banner = v));
  }

  async function save() {
    if (busy) return;
    busy = true;
    try {
      await api.setGuildProfile(S.activeGuildId, name.trim(), icon, banner, description.trim());
      await refreshGuilds();
      flash("Guild updated", "success");
      onClose();
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }
</script>

<svelte:window onpaste={onPaste} />

<Modal title="Guild settings" {onClose}>
  <!-- Banner preview -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="banner-box"
    style={banner ? `background-image:url(${banner})` : ""}
    onmouseenter={() => (pasteTarget = "banner")}
  >
    {#if canEdit}
      <div class="banner-actions">
        <button class="chip" onclick={() => pickImage((v) => (banner = v))}>
          <Icon name="attach" size={13} /> {banner ? "Change" : "Add"} banner
        </button>
        {#if banner}<button class="chip" onclick={() => (banner = "")}>Remove</button>{/if}
      </div>
    {/if}
  </div>

  <div class="id-row">
    <button
      class="icon-btn"
      onclick={() => canEdit && pickImage((v) => (icon = v))}
      onmouseenter={() => (pasteTarget = "icon")}
      disabled={!canEdit}
    >
      {#if icon}<img src={icon} alt="icon" />{:else}<span>{(name || "?").slice(0, 1)}</span>{/if}
      {#if canEdit}<span class="cam-overlay">Change</span>{/if}
    </button>
    <label class="field grow">
      <span class="muted">Guild name</span>
      <input bind:value={name} maxlength="40" disabled={!canEdit} />
    </label>
  </div>

  <label class="field">
    <span class="muted">Description</span>
    <textarea bind:value={description} rows="3" maxlength="1000" disabled={!canEdit}
      placeholder="What's this guild about?"></textarea>
  </label>

  {#if canEdit}
    <div class="actions">
      <button class="ghost" onclick={onClose}>Cancel</button>
      <button onclick={save} disabled={busy || !name.trim()}>Save</button>
    </div>
  {:else}
    <p class="muted tiny">Only members with Manage Guild can edit this.</p>
  {/if}

  <p class="muted tiny privacy-note">
    <Icon name="info" size={12} /> Deleting a message hides it from members, but
    moderators (Manage Messages) can still view the original — this keeps
    moderation honest in a shared space. Direct messages are different: there,
    deleting erases the content for both people.
  </p>
</Modal>

<style>
  .privacy-note {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin-top: 14px;
    padding-top: 12px;
    border-top: 1px solid var(--border);
    line-height: 1.5;
  }
  .banner-box {
    height: 100px;
    border-radius: var(--radius-md);
    background: var(--bg-3);
    background-size: cover;
    background-position: center;
    display: flex;
    align-items: flex-end;
    justify-content: flex-end;
    padding: 8px;
    transition: box-shadow 0.15s ease;
  }
  .banner-box:hover {
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  @media (prefers-reduced-motion: reduce) {
    .banner-box {
      transition: none;
    }
  }
  .banner-actions {
    display: flex;
    gap: 6px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 12px;
    padding: 4px 9px;
    background: rgba(0, 0, 0, 0.55);
    color: #fff;
    border-radius: var(--radius-sm);
  }
  .id-row {
    display: flex;
    align-items: flex-end;
    gap: 12px;
  }
  .icon-btn {
    position: relative;
    width: 56px;
    height: 56px;
    border-radius: 14px;
    background: var(--accent);
    color: #fff;
    font-size: 22px;
    font-weight: 700;
    display: grid;
    place-items: center;
    overflow: hidden;
    flex-shrink: 0;
    padding: 0;
  }
  .icon-btn img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  /* Same centered hover hint as the profile avatar — no clipped edge badge. */
  .cam-overlay {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #fff;
    background: rgba(0, 0, 0, 0.5);
    opacity: 0;
    transition: opacity 0.12s ease;
  }
  .icon-btn:hover .cam-overlay,
  .icon-btn:focus-visible .cam-overlay {
    opacity: 1;
  }
  /* Touch has no hover — keep a faint persistent hint so it's discoverable. */
  @media (pointer: coarse) {
    .cam-overlay {
      opacity: 0.55;
      background: rgba(0, 0, 0, 0.35);
    }
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    text-align: left;
    font-size: 12px;
  }
  .grow {
    flex: 1;
  }
  textarea {
    resize: vertical;
    font-family: inherit;
    font-size: 13px;
    padding: 8px 10px;
  }
  .tiny {
    font-size: 11px;
  }
</style>
