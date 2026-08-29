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
  import { guildInitials } from "../lib/rail.js";

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
  //
  // …and never on a phone. The shelf drops its own scroller on touch (see
  // .shelves below), so open-by-default put roughly eight rows of tiles —
  // ~600px — between the name field and the Save button, with nothing to
  // suggest a Save button existed. It's one tap to open.
  // Closed by default now that identity is above it: the gallery is the
  // optional half of this panel, and open-by-default was what pushed the guild's
  // own name below the fold.
  let picking = $state(false);

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
  <!-- IDENTITY FIRST. The panel is called "Name, icon, banner & description"
       and used to put them in the reverse of that order: a 24-tile animated
       template gallery ate three hundred pixels and the guild's own NAME
       started below the fold. -->
  <div class="id-row">
    <!-- The control had no label and no title, and revealed the word "CHANGE"
         only on hover — so to a screen reader it was a button called nothing,
         and to a finger it was a picture. The initials are the RAIL's rule now
         (lib/rail.js), because the same guild reading "RM" in the rail and "R"
         here, three inches apart, looked like two different places. -->
    <button
      class="icon-btn"
      aria-label={icon ? "Change the guild icon" : "Choose a guild icon"}
      onclick={() => canEdit && pickImage((v) => (icon = v))}
      onmouseenter={() => (pasteTarget = "icon")}
      disabled={!canEdit}
    >
      {#if icon}<img src={icon} alt="" />{:else}<span>{guildInitials(name) || "?"}</span>{/if}
      {#if canEdit}<span class="cam-badge" aria-hidden="true"><Icon name="camera" size={13} /></span>{/if}
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

  <details class="art-fold" bind:open={picking}>
    <summary>
      <span class="afchev"><Icon name="chevron" size={13} /></span>
      Banner
      <span class="muted tiny">{art ? "one is set" : "none yet"} · {GUILD_BANNERS.length} drawn scenes</span>
    </summary>
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
    <!-- Every tile is the real thing at 0.45 scale, and only the hovered/chosen
         one animates — two dozen live scenes at once is a laptop fan (the same
         trade BannerStudio's shelves make). -->
    {#if true}
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

  </details>

  {#if canEdit}
    <div class="actions">
      <button class="ghost" onclick={onClose}>Cancel</button>
      <button onclick={save} disabled={busy || !name.trim()}>Save</button>
    </div>
  {:else}
    <p class="muted tiny">Only members with Manage Guild can edit this.</p>
  {/if}

</Modal>

<style>
  /* The disclosure that keeps the 24-tile gallery from pushing the guild's own
     NAME below the fold, which is what the panel is for. */
  .art-fold {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-1);
    padding: 0 var(--sp-3);
  }
  .art-fold > summary {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 10px 0;
    cursor: pointer;
    font-size: var(--fs-ui);
    font-weight: 600;
    /* Both halves: Firefox honours list-style, WebKit wants the pseudo. Without
       BOTH you get the browser's triangle beside the app's chevron. */
    list-style: none;
  }
  .art-fold > summary::-webkit-details-marker {
    display: none;
  }
  .afchev {
    display: inline-grid;
    place-items: center;
    color: var(--text-faint);
    transition: transform var(--dur-quick) var(--ease-out);
  }
  .art-fold[open] .afchev {
    transform: rotate(90deg);
  }
  .privacy-note {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin-top: 14px;
    padding-top: var(--sp-3);
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
    padding: var(--sp-2);
    transition: box-shadow var(--dur-standard) ease;
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
    gap: var(--sp-2);
    min-width: 0;
    color: #fff;
    font-size: var(--fs-body);
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
    border-radius: var(--radius-md);
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
    font-size: var(--fs-compact);
    font-weight: 600;
  }
  .tpl-toggle :global(svg) {
    transition: transform var(--dur-standard) ease;
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
    /* The fade at the bottom says "keep scrolling". It used to land THROUGH a
       row of tile labels, which reads as a rendering fault rather than as an
       affordance — the padding gives it empty space to fade over. */
    padding-bottom: 26px;
    max-height: calc(40 * var(--vh));
    max-height: calc(40 * var(--dvh)); /* fallback line above; dvh shrinks with the keyboard */
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
  @media (pointer: coarse), (max-width: 768px) {
    .shelves {
      flex: none;
      max-height: none;
      overflow-y: visible;
      mask-image: none;
    }
  }
  .gtitle {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--text-faint);
    margin: 9px 0 4px;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
    gap: var(--sp-2);
  }
  /* Offscreen tiles don't paint and don't animate — the same trade BannerStudio
     makes: a shelf of live scenes is only cheap if the ones you can't see are
     asleep. */
  .tile {
    content-visibility: auto;
    contain-intrinsic-size: 100px 62px;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    padding: 0;
    background: transparent;
    border: 2px solid transparent;
    border-radius: var(--radius-md);
    overflow: hidden;
    min-height: 0; /* the mobile 44px button floor would stretch the tiles */
  }
  .tile.sel {
    border-color: var(--accent);
  }
  /* Mouse only. On touch there is no hover, so the only tile a phone user ever
     saw move was the one already chosen — while the heading above sells them as
     "drawn scenes, animated". `content-visibility: auto` on .tile means only
     the tiles actually on screen paint at all, so letting them all run costs
     nothing a phone can feel. */
  @media (pointer: fine) {
    .tile:not(:hover):not(.sel) :global(.fxfield) {
      display: none;
    }
    .tile:not(:hover):not(.sel) :global(.drift) {
      animation-play-state: paused;
    }
  }
  .tile :global(.art) {
    display: block;
    width: 100%;
    height: 44px;
    border-radius: var(--radius-sm);
  }
  .art-none {
    display: grid;
    place-items: center;
    height: 44px;
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-faint);
  }
  .tname {
    font-size: var(--fs-tiny);
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
    font-size: var(--fs-compact);
    padding: 4px 9px;
    background: rgba(0, 0, 0, 0.55);
    color: #fff;
    border-radius: var(--radius-sm);
  }
  .id-row {
    display: flex;
    align-items: flex-end;
    gap: var(--sp-3);
  }
  .icon-btn {
    position: relative;
    width: 56px;
    height: 56px;
    border-radius: var(--radius-lg);
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fs-display);
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
  /* Permanent, at every pointer type: a control has to say it is a control
     before the pointer is on it, and a finger never hovers. */
  .cam-badge {
    position: absolute;
    /* INSIDE the box: .icon-btn clips (it has to, so an uploaded picture takes
       the rounded corner), so a badge hung off the edge would be sliced. */
    right: 2px;
    bottom: 2px;
    width: 22px;
    height: 22px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .icon-btn:hover .cam-badge,
  .icon-btn:focus-visible .cam-badge {
    color: var(--text);
    border-color: var(--accent);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    text-align: left;
    font-size: var(--fs-compact);
  }
  .grow {
    flex: 1;
  }
  textarea {
    resize: vertical;
    font-family: inherit;
    font-size: var(--fs-ui);
    padding: 8px 10px;
  }
  .tiny {
    font-size: var(--fs-small);
  }
</style>
