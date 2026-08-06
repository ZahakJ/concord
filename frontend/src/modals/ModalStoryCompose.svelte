<script>
  // Share a moment: pick a live banner preset, write the headline, and read —
  // in plain words — exactly who gets it and for how long. The audience line
  // is mandatory honesty, not decoration: a story fans out to whole guilds,
  // and "who can see this" must be answered before Post, not discovered after.
  import Modal from "./Modal.svelte";
  import Banner from "../Banner.svelte";
  import { S, activeGuild, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { BANNER_BY_ID } from "../lib/banners.js";
  import { confettiBurst } from "../lib/burst.js";

  let { onClose } = $props();

  // A dozen curated presets, one per mood. The full 40-strong library lives in
  // BannerStudio; a story composer needs a shelf, not a warehouse.
  const PRESET_IDS = [
    "galaxy",
    "meteors",
    "aurora",
    "sakura",
    "sunrise",
    "ocean",
    "campfire",
    "fireflies",
    "synthwave",
    "equalizer",
    "hearts",
    "midnight",
  ];
  const PRESETS = PRESET_IDS.map((id) => BANNER_BY_ID[id]).filter(Boolean);

  let sel = $state("galaxy");
  let caption = $state("");
  let posting = $state(false);

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
    !!sel && !!caption.trim() && bytes <= MAX_BYTES && chosen.length > 0 && !posting,
  );

  async function post() {
    if (!canPost) return;
    posting = true;
    try {
      await api.postStory(
        chosen.map((g) => g.id),
        `preset:${sel}`,
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
  <!-- Live preview: exactly the banner + caption the viewer will paint. -->
  <div class="hero-wrap">
    <Banner banner={`preset:${sel}`} scrim="light" class="story-hero">
      {#if caption.trim()}
        <span class="hero-caption">{caption.trim()}</span>
      {/if}
    </Banner>
  </div>

  <div class="grid">
    {#each PRESETS as p (p.id)}
      <button class="tile" class:sel={sel === p.id} title={p.name} onclick={() => (sel = p.id)}>
        <Banner banner={`preset:${p.id}`} scale={0.4} class="tile-art" />
      </button>
    {/each}
  </div>

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
        {chosen.length ? `${chosen.length} guild${chosen.length === 1 ? "" : "s"} — ${total} members` : "Pick at least one guild"}
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
  .grid {
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: 6px;
  }
  @media (max-width: 768px) {
    .grid {
      grid-template-columns: repeat(4, 1fr);
    }
  }
  .tile {
    padding: 0;
    border-radius: var(--radius-sm);
    overflow: hidden;
    border: 2px solid transparent;
    background: transparent;
    aspect-ratio: 2 / 1;
  }
  .tile.sel {
    border-color: var(--accent);
  }
  .tile :global(.tile-art) {
    width: 100%;
    height: 100%;
    border-radius: calc(var(--radius-sm) - 2px);
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
    gap: 4px;
    font-size: var(--fs-ui);
  }
  .aud-head {
    font-weight: 600;
  }
  .aud-row {
    display: flex;
    align-items: center;
    gap: 8px;
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
    gap: 8px;
  }
</style>
