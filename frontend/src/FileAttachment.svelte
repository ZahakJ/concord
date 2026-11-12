<script>
  // A non-image file attachment: a compact card with icon, name, size, and a
  // download button that fetches + decrypts the blob on demand.
  import Icon from "./Icon.svelte";
  import { saveBlob } from "./lib/savefile.js";
  import { api } from "./lib/api.js";
  import { flash, S, memberByFpr, nameFor } from "./lib/state.svelte.js";
  import { fmtBytes, fileLabel, midTrunc, unavailableNote, worthRetrying } from "./lib/attachments.js";

  // `sender` is the fingerprint of whoever posted the file — see
  // Attachment.svelte for why a failed fetch has to be able to name them.
  let { channelId, tok, sender = "" } = $props();

  let busy = $state(false);
  // Set when a download failed for want of a source. Unlike a picture this is
  // not fetched until asked for, so the card can't know in advance — but once
  // the answer is "nobody has it", saying so on the card beats a toast that
  // disappears in five seconds and leaves the plain meta line standing there
  // as if nothing happened.
  let errMsg = $state("");

  const senderRow = $derived(sender ? memberByFpr(sender) : undefined);
  const note = $derived(
    unavailableNote(errMsg, {
      name: senderRow ? nameFor(sender) : "",
      self: !!sender && sender === S.identity?.fingerprint,
    }),
  );

  // A file is never fetched behind the reader's back, so an arrival doesn't
  // start a download — it clears the stale verdict, putting "click to
  // download" back on a card that can now actually deliver.
  let seenOnline = null;
  let failedAt = 0;
  $effect(() => {
    const now = new Set(
      (S.members || []).filter((m) => m.online).map((m) => m.fingerprint),
    );
    const prev = seenOnline;
    seenOnline = now;
    if (errMsg && worthRetrying(prev, now, Date.now() - failedAt)) errMsg = "";
  });

  const sizeLabel = $derived(fmtBytes(tok.size));

  async function download() {
    if (busy) return;
    busy = true;
    errMsg = "";
    try {
      const dataUrl = await api.fetchFile(channelId, tok.blobId, tok.keys, tok.mime);
      const how = await saveBlob(tok.name, await (await fetch(dataUrl)).blob());
      if (!how) flash("Couldn't save that file");
    } catch (err) {
      errMsg = String(err?.message || err);
      failedAt = Date.now();
      flash(note, "error");
    } finally {
      busy = false;
    }
  }
</script>

<button
  class="file-card"
  class:gone={!!errMsg}
  onclick={download}
  title={errMsg ? note : `Download ${tok.name}`}
>
  <span class="thumb">
    {#if busy}
      <span class="spin"></span>
    {:else}
      <Icon name="attach" size={18} />
    {/if}
  </span>
  <span class="meta">
    <!-- Cut from the middle, so the extension survives (lib/attachments.js);
         the full name is the card's title. The type leads the meta line when
         the name has one ("TXT · 120 B"), else the size alone. -->
    <span class="name"><bdi>{midTrunc(tok.name, 36)}</bdi></span>
    <span class="muted sub">{errMsg ? `${sizeLabel} · ${note}` : fileLabel(tok.name, tok.size)}</span>
  </span>
  <span class="dl"><Icon name="download" size={16} /></span>
</button>

<style>
  .file-card {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: var(--sp-1);
    max-width: 340px;
    padding: 10px 12px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    text-align: left;
    color: var(--text);
    transition:
      background var(--dur-quick) ease,
      border-color var(--dur-quick) ease,
      box-shadow var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .file-card:hover {
      border-color: var(--border-strong);
    }
  }
  .file-card:active {
    background: var(--bg-3);
  }
  .thumb {
    position: relative;
    width: 36px;
    height: 36px;
    flex-shrink: 0;
    display: grid;
    place-items: center;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .meta {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .name {
    font-size: var(--fs-ui);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    font-size: var(--fs-small);
  }
  /* A card that just failed keeps its name and size but drops the invitation:
     it wraps, because the reason is a sentence and truncating it to one line
     would put the useful half off the end. */
  .file-card.gone {
    border-color: var(--danger);
    align-items: flex-start;
  }
  .file-card.gone .sub {
    white-space: normal;
    line-height: 1.35;
  }
  .dl {
    color: var(--text-muted);
    flex-shrink: 0;
    transition:
      color var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .file-card:hover .dl {
      color: var(--text);
    }
  }
  .spin {
    width: 18px;
    height: 18px;
    border: 2px solid var(--border);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: rot 0.8s linear infinite;
  }
  @keyframes rot {
    to {
      transform: rotate(360deg);
    }
  }
</style>
