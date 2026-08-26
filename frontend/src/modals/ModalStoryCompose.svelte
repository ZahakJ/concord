<script>
  // Share a moment: pick a scene — any live banner preset, or your own image —
  // write the headline, and read — in plain words — exactly who gets it and
  // for how long. The audience line is mandatory honesty, not decoration: a
  // story fans out to whole guilds, and "who can see this" must be answered
  // before Post, not discovered after.
  import Modal from "./Modal.svelte";
  import Banner from "../Banner.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { plural } from "../lib/plural.js";
  import { BANNERS, BANNER_GROUPS } from "../lib/banners.js";
  import { confettiBurst } from "../lib/burst.js";

  let { onClose } = $props();

  let sel = $state("galaxy"); // chosen preset id (idle while a custom image is set)
  let custom = $state(""); // uploaded raster data URI — the whole scene, verbatim
  let caption = $state("");
  let posting = $state(false);

  // What actually posts and previews: the server's 'preset' field takes either
  // form (story.go validStoryScene), and Banner.svelte paints either.
  const scene = $derived(custom || `preset:${sel}`);

  // Server truth: 250KB of DATA URI, not of file (story.go maxStorySceneBytes
  // caps the whole string) — base64 costs a third, hence "~180 KB" of image.
  const MAX_SCENE = 250 * 1024;

  // Read an image file to a data URI. Kept raw (no canvas re-encode) so
  // animated GIF scenes keep animating — same trade as ModalGuildSettings'
  // applyImageFile; rejected if too big for a story record.
  function applyImageFile(file) {
    if (!file || !file.type.startsWith("image/")) return;
    const reader = new FileReader();
    reader.onload = () => {
      if (String(reader.result).length > MAX_SCENE) {
        flash("Image too large — keep it under ~180 KB", "error");
        return;
      }
      custom = String(reader.result);
    };
    reader.readAsDataURL(file);
  }
  function pickImage() {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = "image/*";
    input.onchange = () => applyImageFile(input.files?.[0]);
    input.click();
  }

  // The full library, shelved by group like ModalGuildSettings' picker — and
  // like there, the shelf starts folded on a phone so the caption and Post
  // button aren't pushed under eight rows of tiles.
  let picking = $state(!S.isMobile);

  // Server truth: 300 BYTES, not characters (story.go maxStoryCaptionBytes) —
  // an emoji costs four apiece, so the meter counts what the server counts.
  const MAX_BYTES = 300;
  const bytes = $derived(new TextEncoder().encode(caption).length);

  // ---- audience ----
  const myGuilds = $derived(S.guilds.filter((g) => g.kind !== "dm"));
  const ag = activeGuild();
  // Active guild pre-checked; the tray only exists on non-DM guilds, but a
  // DM-open edge (deep link) simply starts with nothing checked.
  let selected = $state(ag && ag.kind !== "dm" ? { [ag.id]: true } : {});

  // Member counts for the honesty line. The active guild's roster is already
  // in S.members; each other guild costs one Members call, made once and only
  // when you actually belong to several.
  let counts = $state({});
  $effect(() => {
    if (ag && ag.kind !== "dm") counts[ag.id] = S.members.length;
    for (const g of myGuilds) {
      if (g.id === ag?.id || counts[g.id] != null) continue;
      counts[g.id] = -1; // in flight — render as "…", never as 0
      api
        .members(g.id)
        .then((ms) => (counts[g.id] = (ms || []).length))
        .catch(() => (counts[g.id] = -1));
    }
  });

  const chosen = $derived(myGuilds.filter((g) => selected[g.id]));
  const total = $derived(chosen.reduce((n, g) => n + Math.max(0, counts[g.id] ?? 0), 0));
  const countOf = (id) => (counts[id] == null || counts[id] < 0 ? "…" : counts[id]);

  const canPost = $derived(
    !!scene && !!caption.trim() && bytes <= MAX_BYTES && chosen.length > 0 && !posting,
  );

  async function post() {
    if (!canPost) return;
    posting = true;
    try {
      await api.postStory(
        chosen.map((g) => g.id),
        scene,
        caption.trim(),
      );
      // Local echo for the tray: the "story" event covers arrivals from
      // peers; your own post deserves an instant repaint. And the confetti is
      // for YOUR story going up — a one-shot, never a loop (lib/burst.js
      // bails on its own under prefers-reduced-motion).
      window.dispatchEvent(new CustomEvent("concord:stories-changed"));
      confettiBurst();
      flash("Moment shared", "success");
      onClose?.();
    } catch (err) {
      flash(err);
      posting = false;
    }
  }
</script>

<Modal title="Share a moment" {onClose} wide>
  <!-- Live preview: exactly the scene + caption the viewer will paint — an
       uploaded image full-bleed, a preset with all its fx. -->
  <div class="hero-wrap">
    <Banner banner={scene} scrim="light" class="story-hero">
      {#if caption.trim()}
        <span class="hero-caption">{caption.trim()}</span>
      {/if}
    </Banner>
  </div>

  <div class="tpl-head">
    <button class="tpl-toggle" onclick={() => (picking = !picking)} aria-expanded={picking}>
      <Icon name="chevron" size={12} />
      <span>Scenes</span>
      <span class="muted tiny">{BANNERS.length} drawn scenes, animated — or your own image</span>
    </button>
  </div>
  {#if picking}
    <!-- Every tile is the real thing at 0.45 scale, and only the hovered/chosen
         one animates — the shelves trade ModalGuildSettings makes, for the
         same laptop fan. -->
    <div class="shelves">
      <div class="grid">
        <button class="tile upload" class:sel={!!custom} title="Upload image" onclick={pickImage}>
          {#if custom}
            <img class="art-img" src={custom} alt="" />
          {:else}
            <span class="art-none"><Icon name="attach" size={13} /></span>
          {/if}
          <span class="tname">Upload image</span>
        </button>
      </div>
      {#each BANNER_GROUPS as grp (grp)}
        <div class="gtitle">{grp}</div>
        <div class="grid">
          {#each BANNERS.filter((b) => b.group === grp) as b (b.id)}
            <button
              class="tile"
              class:sel={!custom && sel === b.id}
              title={b.name}
              onclick={() => {
                custom = "";
                sel = b.id;
              }}
            >
              <Banner banner={`preset:${b.id}`} scale={0.45} class="art" />
              <span class="tname">{b.name}</span>
            </button>
          {/each}
        </div>
      {/each}
    </div>
  {/if}

  <label class="cap-label">
    <textarea
      rows="2"
      placeholder="Say the thing…"
      bind:value={caption}
      maxlength={MAX_BYTES}
    ></textarea>
    <span class="count" class:over={bytes > MAX_BYTES}>{bytes}/{MAX_BYTES}</span>
  </label>

  <!-- The audience, spelled out. One guild: a single line. Several: a
       checkbox per guild, each with its own member count, so widening the
       audience is an explicit act — never a default. -->
  {#if myGuilds.length > 1}
    <div class="audience-list">
      <span class="aud-head">Sharing to:</span>
      {#each myGuilds as g (g.id)}
        <label class="aud-row">
          <input type="checkbox" bind:checked={selected[g.id]} />
          <span class="aud-name">{g.name}</span>
          <span class="aud-count muted">{countOf(g.id)} members</span>
        </label>
      {/each}
      <span class="aud-total muted">
        {chosen.length ? `${plural(chosen.length, "guild")} — ${plural(total, "member")}` : "Pick at least one guild"}
      </span>
    </div>
  {:else if myGuilds.length === 1}
    <p class="audience">
      Sharing to: <strong>{myGuilds[0].name}</strong> — {countOf(myGuilds[0].id)} members
    </p>
  {:else}
    <p class="audience muted">You're not in any guild to share to.</p>
  {/if}

  <p class="honesty muted">
    Disappears from honest apps after 24 hours — people you share it with could still save it.
  </p>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button disabled={!canPost} onclick={post}>{posting ? "Posting…" : "Post"}</button>
  </div>
</Modal>

<style>
  .hero-wrap {
    border-radius: var(--radius-md);
    overflow: hidden;
    border: 1px solid var(--border);
  }
  .hero-wrap :global(.story-hero) {
    height: 120px;
    border-radius: var(--radius-md);
    display: grid;
    place-items: center;
  }
  .hero-caption {
    position: relative;
    z-index: 1;
    color: #fff;
    font-weight: 700;
    font-size: var(--fs-body);
    text-shadow: 0 1px 6px rgba(0, 0, 0, 0.55);
    text-align: center;
    max-width: 90%;
    word-break: break-word;
    white-space: pre-wrap;
  }
  /* ---- scene picker: ModalGuildSettings' shelves idiom, replicated ---- */
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
  .tiny {
    font-size: var(--fs-tiny);
  }
  .shelves {
    /* overflow-y zeroes a flex item's automatic minimum size, so on a short
       window this shelf did not scroll — it collapsed, and forty scene tiles
       became an 8px sliver with a fade over it. A floor of one whole row plus
       its heading means the worst case is a small shelf that scrolls, and the
       dialog takes the rest. */
    flex: 1 1 auto;
    min-height: 148px;
    max-height: 40vh;
    max-height: 40dvh; /* fallback line above; dvh shrinks with the keyboard */
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
  /* Mouse only: on touch there is no hover, and content-visibility already
     keeps the offscreen rows asleep. */
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
  .art-img {
    display: block;
    width: 100%;
    height: 44px;
    object-fit: cover;
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
  .cap-label {
    position: relative;
    display: block;
  }
  textarea {
    width: 100%;
    resize: vertical;
    min-height: 54px;
    padding-right: 64px;
  }
  .count {
    position: absolute;
    right: 8px;
    bottom: 8px;
    font-size: var(--fs-micro);
    color: var(--text-muted);
    pointer-events: none;
  }
  .count.over {
    color: var(--danger-text);
  }
  .audience {
    margin: 0;
    font-size: var(--fs-ui);
  }
  .audience-list {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    font-size: var(--fs-ui);
  }
  .aud-head {
    font-weight: 600;
  }
  .aud-row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 3px 0;
    cursor: pointer;
  }
  .aud-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .aud-count,
  .aud-total {
    font-size: var(--fs-small);
  }
  .honesty {
    margin: 0;
    font-size: var(--fs-small);
    line-height: 1.45;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
  }
</style>
