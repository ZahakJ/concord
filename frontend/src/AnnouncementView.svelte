<script>
  // A published announcement, rendered as the server speaking.
  //
  // It used to be a bordered card with a badge and the original author's face
  // inside it — which is the visual language of a QUOTE: "look what someone
  // said over there". An announcement is the opposite. It is not a forward, it
  // is the server telling you something, so it wears the server's name and icon
  // in the message header (see Message.svelte) and the body here is just the
  // text, formatted like any other message. Nothing to draw a box around.
  //
  // The publisher's optional note goes above it, because that part genuinely is
  // someone's aside about the announcement rather than the announcement itself.
  import { customEmojiMap, mentionRefs, S } from "./lib/state.svelte.js";
  import { renderMarkdown } from "./lib/markdown.js";

  let { announce } = $props();

  const names = $derived(Object.fromEntries((S.members || []).map((m) => [m.fingerprint, m.name])));
</script>

{#if announce.note}
  <div class="note">{@html renderMarkdown(announce.note, names, customEmojiMap(), mentionRefs())}</div>
{/if}
<div class="body">{@html renderMarkdown(announce.body, names, customEmojiMap(), mentionRefs())}</div>

<style>
  .body {
    font-size: 14px;
    line-height: 1.55;
    word-wrap: break-word;
  }
  /* The publisher's own words, set apart from the announcement they are
     introducing without being boxed off from it. */
  .note {
    margin-bottom: 4px;
    font-size: 14px;
    line-height: 1.55;
    color: var(--text-muted);
  }
  .body :global(p:first-child),
  .note :global(p:first-child) {
    margin-top: 0;
  }
  .body :global(p:last-child),
  .note :global(p:last-child) {
    margin-bottom: 0;
  }
</style>
