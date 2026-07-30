<script>
  // Edit a guild's identity: name, logo, banner, description. Requires Manage
  // Guild; the backend re-checks. Read-only-ish for others (they just see the
  // current values and can't save).
  //
  // The banner is either one of the drawn templates from lib/guildbanners.js
  // (a dozen bytes on the wire, and it animates) or an uploaded image/GIF. Both
  // render through Banner.svelte here and in the channel-list header, so the
  // tile you click is exactly what every member's sidebar gets — including the
  // legibility scrim, which is why the preview below prints the real guild name
  // and icon over the art rather than showing it bare.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Banner from "../Banner.svelte";
  import { S, activeGuild, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { PERM, has } from "../lib/perms.js";
  import { GUILD_BANNERS, GUILD_BANNER_GROUPS, guildBannerArt } from "../lib/guildbanners.js";

  let { onClose } = $props();

  const g = activeGuild();
  let name = $state(g?.name || "");
  let icon = $state(g?.icon || "");
  let banner = $state(g?.banner || "");
  let description = $state(g?.description || "");
  let busy = $state(false);

  const canEdit = has(g?.myPerms || 0, PERM.MANAGE_GUILD);
  const MAX = 500 * 1024;

  // What the header will actually paint (and in which ink) — null for "nothing",
  // which is also what an unrecognised template id degrades to.
  const art = $derived(guildBannerArt(banner));
  const selected = $derived(art?.kind === "preset" ? art.template.id : "");
  // The shelves start open unless this guild already uses its own picture: then
  // the picker would be shouting over a decision that's already been made.
  let picking = $state(!(g?.banner && !guildBannerArt(g.banner)?.template));

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

<Modal title="Guild settings" {onClose} wide>
  <!-- Banner preview: the art, the scrim, and the guild's own name and icon on
       top of it, laid out like the channel-list header — so "can you read the
       name over this?" is answered here rather than after saving. -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="banner-box" class:empty={!art} class:ink-dark={art?.ink === "dark"}
    onmouseenter={() => (pasteTarget = "banner")}>
    {#if art}
      <Banner {banner} scrim={art.ink} class="bp-art" />
    {/if}
    <span class="bp-name">
      {#if icon}<img class="bp-icon" src={icon} alt="" />{/if}
      <strong>{name || "Your guild"}</strong>
    </span>
    {#if canEdit}
      <div class="banner-actions">
        <button class="chip" onclick={() => pickImage((v) => (banner = v))}>
          <Icon name="attach" size={13} /> Upload image
        </button>
        {#if banner}<button class="chip" onclick={() => (banner = "")}>Remove</button>{/if}
      </div>
    {/if}
  </div>

  {#if canEdit}
    <div class="tpl-head">
      <button class="tpl-toggle" onclick={() => (picking = !picking)} aria-expanded={picking}>
        <Icon name="chevron" size={12} />
        <span>Templates</span>
        <span class="muted tiny">{GUILD_BANNERS.length} drawn scenes, animated</span>
      </button>
    </div>
    {#if picking}
      <!-- Every tile is the real thing at 0.45 scale, and only the hovered/chosen
           one animates — two dozen live scenes at once is a laptop fan (the same
           trade BannerStudio's shelves make). -->
      <div class="shelves">
        <div class="grid">
          <button class="tile none" class:sel={!banner} onclick={() => (banner = "")}>
            <span class="art-none"><Icon name="close" size={13} /></span>
            <span class="tname">No banner</span>
          </button>
        </div>
        {#each GUILD_BANNER_GROUPS as grp (grp)}
          <div class="gtitle">{grp}</div>
          <div class="grid">
            {#each GUILD_BANNERS.filter((t) => t.group === grp) as t (t.id)}
              <button
                class="tile"
                class:sel={selected === t.id}
                onclick={() => (banner = `preset:${t.id}`)}
                title={t.name}
              >
                <!-- No scrim on the tiles: they carry no text, and the artwork
                     should read at 110px. The preview above is where you see it
                     with the scrim and the name over it. -->
                <Banner banner={`preset:${t.id}`} scale={0.45} class="art" />
                <span class="tname">{t.name}</span>
              </button>
            {/each}
          </div>
        {/each}
      </div>
    {/if}
  {/if}

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

  <!-- Cut to the two facts, and still printed rather than moved behind an info
       dot: that "deleted" means something different here than in a DM is
       something you have to know BEFORE you delete, not after you go looking.
       (A dot is also the wrong control this low in a short dialog — InfoDot
       flips its popover against the VIEWPORT, so here it opens downward and the
       dialog's own edge cuts it in half.) -->
  <p class="muted tiny privacy-note">
    <Icon name="info" size={12} /> Deleting a message here hides it from members, but
    moderators can still read the original. In a DM, deleting erases it for both
    people.
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
    position: relative;
    height: 100px;
    /* The dialog is a flex column: without this the preview is the first thing
       the browser squeezes when the shelves make the content tall, and on a
       phone sheet it collapsed to ~34px with the chips hanging out of it. */
    flex: none;
    border-radius: var(--radius-md);
    background: var(--bg-3);
    overflow: hidden;
    display: flex;
    align-items: flex-end;
    padding: 8px;
    transition: box-shadow 0.15s ease;
  }
  .banner-box :global(.bp-art) {
    position: absolute;
    inset: 0;
  }
  /* The name row mirrors the channel-list header: same 26px icon, same weight,
     same corner. If it's legible here it's legible there. */
  .bp-name {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    color: #fff;
    font-size: 15px;
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  }
  .banner-box.ink-dark .bp-name {
    color: #12161a;
    text-shadow: 0 1px 2px rgba(255, 255, 255, 0.65);
  }
  .banner-box.empty .bp-name {
    color: var(--text-muted);
    text-shadow: none;
  }
  .bp-name strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bp-icon {
    width: 26px;
    height: 26px;
    border-radius: 8px;
    object-fit: cover;
    flex-shrink: 0;
  }
  /* ---- template picker ---- */
  .tpl-head {
    display: flex;
    margin-top: -4px;
  }
  .tpl-toggle {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 4px 0;
    background: transparent;
    color: var(--text);
    font-size: 12.5px;
    font-weight: 600;
  }
  .tpl-toggle :global(svg) {
    transition: transform 0.15s ease;
  }
  .tpl-toggle[aria-expanded="true"] :global(svg) {
    transform: rotate(90deg);
  }
  @media (prefers-reduced-motion: reduce) {
    .tpl-toggle :global(svg) {
      transition: none;
    }
  }
  .shelves {
    max-height: 40vh;
    overflow-y: auto;
    margin: -4px -2px 0;
    padding: 0 2px;
    /* The last row is always cut off somewhere; fading it says "keep scrolling"
       instead of "this tile is broken". */
    mask-image: linear-gradient(#000 calc(100% - 22px), transparent);
  }
  /* A phone sheet already scrolls, and it gives the dialog its own scrollbar: a
     150px scroller nested inside that is a trap for a thumb. On touch the whole
     shelf just lays out and the sheet scrolls it. */
  @media (pointer: coarse), (max-width: 700px) {
    .shelves {
      flex: none;
      max-height: none;
      overflow-y: visible;
      mask-image: none;
    }
  }
  .gtitle {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-faint);
    margin: 9px 0 4px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
    gap: 8px;
  }
  /* Offscreen tiles don't paint and don't animate — the same trade BannerStudio
     makes: a shelf of live scenes is only cheap if the ones you can't see are
     asleep. */
  .tile {
    content-visibility: auto;
    contain-intrinsic-size: 100px 62px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 0;
    background: transparent;
    border: 2px solid transparent;
    border-radius: 9px;
    overflow: hidden;
    min-height: 0; /* the mobile 44px button floor would stretch the tiles */
  }
  .tile.sel {
    border-color: var(--accent);
  }
  .tile:not(:hover):not(.sel) :global(.fxfield) {
    display: none;
  }
  .tile:not(:hover):not(.sel) :global(.drift) {
    animation-play-state: paused;
  }
  .tile :global(.art) {
    display: block;
    width: 100%;
    height: 44px;
    border-radius: 6px;
  }
  .art-none {
    display: grid;
    place-items: center;
    height: 44px;
    border: 1px dashed var(--border);
    border-radius: 6px;
    color: var(--text-faint);
  }
  .tname {
    font-size: 10.5px;
    color: var(--text-muted);
    text-align: center;
    padding-bottom: 3px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .tile.sel .tname {
    color: var(--text);
  }
  /* With no image, --bg-3 on the sheet is all but invisible and the "Add
     banner" chip floated in what read as dead space. The dashed outline is the
     same empty-state cue the channel list uses. */
  .banner-box.empty {
    border: 1px dashed var(--border);
  }
  .banner-box:hover {
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  @media (prefers-reduced-motion: reduce) {
    .banner-box {
      transition: none;
    }
  }
  /* Pinned to the top corner rather than sharing the bottom row with the name:
     on a 390px sheet the two chips and the name fought for the same line and
     the chips won, printing over it. */
  .banner-actions {
    position: absolute;
    top: 8px;
    right: 8px;
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
    color: var(--accent-fg);
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
  /* Touch has no hover, so the hint has to be permanent — but covering the tile
     printed "CHANGE" through the guild initial and neither could be read. A
     bottom strip says the same thing and leaves the tile legible. */
  @media (pointer: coarse) {
    .cam-overlay {
      inset: auto 0 0 0;
      align-content: center;
      height: 18px;
      font-size: 9px;
      opacity: 1;
      background: rgba(0, 0, 0, 0.62);
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
