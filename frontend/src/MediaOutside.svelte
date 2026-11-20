<script>
  // A clip or voice message on an engine that must not be handed a media
  // element (lib/mediasupport.svelte.js): the ordinary file card, to save it
  // and play it elsewhere, and one line saying why it is not played here.
  import FileAttachment from "./FileAttachment.svelte";
  import { MEDIA_OUTSIDE_NOTE } from "./lib/mediasupport.svelte.js";

  let { channelId, tok, sender = "" } = $props();
</script>

<div class="media-outside">
  <FileAttachment {channelId} {tok} {sender} />
  <p class="muted note">{MEDIA_OUTSIDE_NOTE}</p>
</div>

<style>
  .note {
    margin: 4px 0 0;
    max-width: 340px;
    font-size: var(--fs-small);
    line-height: 1.35;
  }
</style>
