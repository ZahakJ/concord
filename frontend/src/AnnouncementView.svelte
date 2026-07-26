<script>
  // A published announcement, rendered as a broadcast rather than a quotation.
  //
  // The distinction is the whole point: a forwarded message says "look what
  // someone else said over there", while an announcement says "this is being
  // told to you, here". So it gets an accent spine, a megaphone header naming
  // the channel it came from, and the original author's own name and face —
  // not the name of whoever pressed Publish.
  import Avatar from "./Avatar.svelte";
  import Icon from "./Icon.svelte";
  import { memberByFpr, nameFor, customEmojiMap, S } from "./lib/state.svelte.js";
  import { renderMarkdown } from "./lib/markdown.js";

  let { announce } = $props();

  const author = $derived(announce.author ? memberByFpr(announce.author) : null);
  const authorName = $derived(announce.author ? nameFor(announce.author) : "");
  const names = $derived(Object.fromEntries((S.members || []).map((m) => [m.fingerprint, m.name])));
</script>

{#if announce.note}
  <div class="note">{announce.note}</div>
{/if}

<div class="ann">
  <div class="head">
    <span class="badge"><Icon name="megaphone" size={12} /> Announcement</span>
    {#if announce.from}
      <span class="src">from <strong>#{announce.from}</strong></span>
    {/if}
  </div>

  {#if authorName}
    <div class="by">
      <Avatar
        name={authorName}
        emoji={author?.emoji || ""}
        color={author?.color || ""}
        image={author?.avatar || ""}
        size={20}
      />
      <span class="by-name">{authorName}</span>
    </div>
  {/if}

  <div class="body">{@html renderMarkdown(announce.body, names, customEmojiMap())}</div>
</div>

<style>
  .note {
    margin-bottom: 6px;
    font-size: 14px;
    line-height: 1.5;
  }
  .ann {
    position: relative;
    margin-top: 4px;
    max-width: 560px;
    padding: 11px 14px 12px 16px;
    border: 1px solid color-mix(in srgb, var(--accent) 30%, var(--border));
    border-radius: var(--radius-md);
    /* A wash of the accent rather than a plain panel: this is the one message
       type that's meant to catch your eye on the way past. */
    background: linear-gradient(
      180deg,
      color-mix(in srgb, var(--accent) 9%, var(--bg-1)),
      var(--bg-1) 60%
    );
    overflow: hidden;
  }
  /* The spine — the same device the app uses for anything "official". */
  .ann::before {
    content: "";
    position: absolute;
    inset: 0 auto 0 0;
    width: 3px;
    background: var(--accent);
  }
  .head {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
  }
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 9px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: 10px;
    font-weight: 800;
    letter-spacing: 0.07em;
    text-transform: uppercase;
  }
  .src {
    font-size: 11.5px;
    color: var(--text-muted);
  }
  .src strong {
    color: var(--text);
    font-weight: 600;
  }
  .by {
    display: flex;
    align-items: center;
    gap: 7px;
    margin-bottom: 7px;
  }
  .by-name {
    font-size: 12.5px;
    font-weight: 600;
    color: var(--text);
  }
  .body {
    font-size: 14px;
    line-height: 1.55;
    word-wrap: break-word;
  }
  .body :global(p:first-child) {
    margin-top: 0;
  }
  .body :global(p:last-child) {
    margin-bottom: 0;
  }
</style>
