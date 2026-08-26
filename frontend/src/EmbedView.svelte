<script>
  // Renders a rich embed card: an accent bar, a title, a markdown description,
  // and name/value fields — Discord-style. Text goes through the same XSS-safe
  // renderMarkdown as messages, so **bold**, {color|text}, links etc. all work
  // and nothing user-authored can inject.
  import { renderMarkdown } from "./lib/markdown.js";

  let { embed, mentionNames = [], customEmoji = null, refs = null } = $props();

  const accent = $derived(embed.color || "var(--accent)");
  const titleHtml = $derived(embed.title ? renderMarkdown(embed.title, mentionNames, customEmoji, refs) : "");
  const descHtml = $derived(embed.desc ? renderMarkdown(embed.desc, mentionNames, customEmoji, refs) : "");
</script>

<div class="embed" style="--embed-accent:{accent}">
  <div class="bar"></div>
  <div class="body">
    {#if embed.title}
      <!-- eslint-disable-next-line svelte/no-at-html-tags -->
      <div class="title">{@html titleHtml}</div>
    {/if}
    {#if embed.desc}
      <!-- eslint-disable-next-line svelte/no-at-html-tags -->
      <div class="desc md">{@html descHtml}</div>
    {/if}
    {#if embed.fields?.length}
      <div class="fields">
        <!-- Keyed by index: fields are free text, so any content-derived key
             can collide (two identical fields, or "a"+"bc" vs "ab"+"c"),
             which Svelte rejects at render time — a crash an author can cause
             from the compose form. Order is stable and rows are never
             reordered, so the index is the honest identity here. -->
        {#each embed.fields as f, i (i)}
          <div class="field">
            {#if f.name}<div class="f-name">{@html renderMarkdown(f.name, mentionNames, customEmoji, refs)}</div>{/if}
            {#if f.value}<div class="f-val md">{@html renderMarkdown(f.value, mentionNames, customEmoji, refs)}</div>{/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .embed {
    display: flex;
    margin-top: var(--sp-1);
    max-width: 460px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .bar {
    width: 4px;
    flex-shrink: 0;
    background: var(--embed-accent);
  }
  .body {
    padding: 11px 14px;
    min-width: 0;
  }
  .title {
    font-weight: 700;
    font-size: var(--fs-ui);
    line-height: 1.3;
    margin-bottom: var(--sp-1);
  }
  .desc {
    font-size: var(--fs-ui);
    line-height: 1.45;
    color: var(--text);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .fields {
    display: grid;
    /* min() so a phone column narrower than 140px gets one field per row
       instead of a grid track wider than the card. */
    grid-template-columns: repeat(auto-fit, minmax(min(140px, 100%), 1fr));
    gap: var(--sp-2) var(--sp-4);
    margin-top: 10px;
  }
  .f-name {
    font-size: var(--fs-compact);
    font-weight: 700;
    margin-bottom: 2px;
  }
  .f-val {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .md :global(a) {
    color: var(--embed-accent);
  }
  .embed :global(img.emoji) {
    width: 1.3em;
    height: 1.3em;
    vertical-align: -0.28em;
    object-fit: contain;
  }
  .embed :global(img.cemoji) {
    height: 1.3em;
    width: auto;
    vertical-align: -0.2em;
    object-fit: contain;
  }
</style>
