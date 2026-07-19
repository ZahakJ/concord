<script>
  // Renders a rich embed card: an accent bar, a title, a markdown description,
  // and name/value fields — Discord-style. Text goes through the same XSS-safe
  // renderMarkdown as messages, so **bold**, {color|text}, links etc. all work
  // and nothing user-authored can inject.
  import { renderMarkdown } from "./lib/markdown.js";

  let { embed, mentionNames = [], customEmoji = null } = $props();

  const accent = $derived(embed.color || "var(--accent)");
  const titleHtml = $derived(embed.title ? renderMarkdown(embed.title, mentionNames, customEmoji) : "");
  const descHtml = $derived(embed.desc ? renderMarkdown(embed.desc, mentionNames, customEmoji) : "");
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
            {#if f.name}<div class="f-name">{@html renderMarkdown(f.name, mentionNames, customEmoji)}</div>{/if}
            {#if f.value}<div class="f-val md">{@html renderMarkdown(f.value, mentionNames, customEmoji)}</div>{/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .embed {
    display: flex;
    margin-top: 4px;
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
    font-size: 14px;
    line-height: 1.3;
    margin-bottom: 4px;
  }
  .desc {
    font-size: 13px;
    line-height: 1.45;
    color: var(--text);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .fields {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
    gap: 8px 16px;
    margin-top: 10px;
  }
  .f-name {
    font-size: 12px;
    font-weight: 700;
    margin-bottom: 2px;
  }
  .f-val {
    font-size: 12px;
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
