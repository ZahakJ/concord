<script>
  // One row of a guild's imported archive.
  //
  // This is deliberately NOT Message.svelte with a flag. Two rules govern
  // archived content and both of them are easier to guarantee by construction
  // than by remembering an `if` in a two-and-a-half-thousand line component:
  //
  //   1. An @name in an archived body must not resolve. The importer keeps
  //      what people wrote, and what they wrote refers to a community that is
  //      not this one — highlighting it would attach a stranger's sentence to a
  //      member of THIS guild, and (worse) offer their profile card as if the
  //      archive vouched for the match. So the mention table handed to the
  //      renderer is empty and the role/channel table is null. Nothing here can
  //      ping anybody: an archived row is not a message and never notifies.
  //   2. No link preview is fetched over an archived body. A decade of history
  //      is a decade of links, and scrolling through it must not turn into an
  //      outbound request per row to hosts the reader has never heard of. This
  //      file simply never mounts LinkPreview or YouTubeEmbed. URLs still
  //      linkify — that is text rendering, not a fetch.
  //
  // The row is also inert: no reply, no react, no edit, no pin, because none of
  // those operations exist for something that was written elsewhere and signed
  // as a block. Copy Text is the one thing that still means something.
  import Avatar from "./Avatar.svelte";
  import Icon from "./Icon.svelte";
  import Attachment from "./Attachment.svelte";
  import FileAttachment from "./FileAttachment.svelte";
  import VideoAttachment from "./VideoAttachment.svelte";
  import { renderMarkdown } from "./lib/markdown.js";
  import { highlightCode } from "./lib/highlight.js";
  import { parseAttachTokens, parseFileTokens, stripAttachTokens } from "./lib/attachments.js";
  import { splitPlaceholders } from "./lib/chronicle.js";
  import { S, customEmojiMap, openContextMenu, flash, clockOpts, tintFor } from "./lib/state.svelte.js";
  import { longpress } from "./lib/touch.js";

  // `m` is a ChronicleMessageView: author and avatar already resolved from the
  // manifest's table, so this component never sees an index — and never looks a
  // name up among the live members, which is the same rule as the mention one.
  // `first` marks the first row of an author's run, which is where the name,
  // the face and the time go.
  let { m, first = true, channelId = "" } = $props();

  const cemoji = $derived(customEmojiMap());

  // Attachment tokens are REAL tokens: the importer sealed those files into
  // ordinary blobs, so they fetch and decrypt through exactly the components a
  // live message uses. The archive carries them in their own field rather than
  // inline, so the body is joined back together here.
  const attachContent = $derived((m.attach || []).join("\n"));
  const images = $derived(parseAttachTokens(attachContent));
  const files = $derived(parseFileTokens(attachContent));

  // …and the files it could NOT carry are a line of text in the body, which is
  // lifted back out so the feed can draw a stub rather than leaving square
  // brackets mid-sentence.
  const split = $derived(splitPlaceholders(m.content || ""));
  const bodyText = $derived(stripAttachTokens(split.text).trim());
  const missing = $derived(split.files);

  // The time only. A day divider sits above every run of these rows and says
  // "SATURDAY, DECEMBER 25" — the row 40px under it said "Dec 25, 2021,
  // 03:35 PM", the same date twice, in two formats and two typefaces, on every
  // one of two thousand rows. The divider carries the day; the row carries the
  // moment, exactly as a live row does.
  //
  // The full reading stays on the hover title and in the context menu's header,
  // because "when exactly" is the whole point of scrolling this far.
  const stampFull = $derived.by(() => {
    const d = new Date((Number(m.nano) || 0) / 1e6);
    if (isNaN(d)) return "";
    return d.toLocaleString([], {
      year: "numeric",
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      ...clockOpts(),
    });
  });
  const stamp = $derived.by(() => {
    const d = new Date((Number(m.nano) || 0) / 1e6);
    if (isNaN(d)) return "";
    return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", ...clockOpts() });
  });

  const plain = $derived(bodyText || missing.map((f) => f.name).join(", "));

  function rowMenu(e) {
    if (!plain) return;
    openContextMenu(
      e,
      [
        {
          label: "Copy text",
          icon: "copy",
          onClick: () => {
            navigator.clipboard?.writeText(plain);
            flash("Copied text", "success");
          },
        },
      ],
      { title: `${m.author} · ${stampFull}` },
    );
  }
</script>

<div class="msg arc" class:compact={!first} role="article" use:longpress={{ handler: rowMenu }} oncontextmenu={rowMenu}>
  {#if first}
    <span class="av-slot">
      <!-- The face comes from the manifest, never from the member list: an
           archived author is a name out of another community's history and may
           happen to collide with somebody here.
           The colour comes from the NAME. Without one every author without a
           picture drew on the same default plate, so BA, AD, GR and LI were
           four different people in four identical discs and three years of
           somebody's history read as one anonymous voice. -->
      <Avatar
        name={m.author || "?"}
        image={m.avatar || ""}
        color={tintFor(m.author || "?")}
        size={38}
      />
    </span>
  {:else}
    <span class="gutter"></span>
  {/if}

  <div class="msg-main">
    {#if first}
      <div class="msg-head">
        <span class="sender">{m.author || "unknown"}</span>
        <!-- No per-row ARCHIVE chip. The import produces one divider that says
             the whole thing once — "imported archive · Jan 2019 – Dec 2021" —
             and the day dividers under it carry pre-2022 dates, so repeating
             the word on all 1,981 rows, between the name and the time where
             the eye lands first, told the reader nothing they had not already
             been told and pushed the time out of its column. -->
        <span class="time" title={stampFull}>{stamp}</span>
      </div>
    {/if}

    {#if bodyText}
      <div class="body" use:highlightCode={bodyText}>{@html renderMarkdown(bodyText, [], cemoji, null)}</div>
    {/if}

    {#each images as tok (tok.blobId)}
      <Attachment {channelId} {tok} />
    {/each}
    {#each files as tok (tok.blobId)}
      {#if tok.mime?.startsWith("video/")}
        <VideoAttachment {channelId} {tok} />
      {:else}
        <FileAttachment {channelId} {tok} />
      {/if}
    {/each}

    {#each missing as f, i (f.name + i)}
      <!-- Not an error. The export named this file and did not bring it, or the
           import policy chose to leave it behind; either way the archive is
           telling the truth about what it holds. A grey slip, not a red one. -->
      <div class="ghost-file">
        <span class="gf-ic"><Icon name="attach" size={13} /></span>
        <span class="gf-name">{f.name}</span>
        {#if f.size}<span class="gf-size">{f.size}</span>{/if}
        <span class="gf-note">not in the archive</span>
      </div>
    {/each}

    {#if m.reactions && Object.keys(m.reactions).length}
      <!-- Counts, not buttons. Nobody can add to a reaction on a message that
           was written in another place five years ago, and a pill that looks
           pressable and isn't is worse than a number. -->
      <div class="arc-reactions">
        {#each Object.entries(m.reactions) as [emoji, count] (emoji)}
          {@const cimg = /^:([a-z0-9_]{2,32}):$/.test(emoji) ? cemoji[emoji.slice(1, -1)] : null}
          <span class="arc-reaction">
            {#if cimg}<img class="cemoji" src={cimg} alt={emoji} />{:else}<span>{emoji}</span>{/if}
            <span class="rcount">{count}</span>
          </span>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  /* The same skeleton as a live row — avatar gutter, then a column — so the
     archive reads as continuous history rather than a different app. What
     changes is the ink: everything here is one step quieter, which is what
     "these are not live messages" looks like without a banner on every row. */
  .msg {
    display: flex;
    gap: var(--sp-3);
    position: relative;
    padding: var(--msg-pad-y, 2px) 0;
    border-radius: var(--radius-sm);
  }
  .msg.compact {
    margin-top: var(--msg-group-pull, -10px);
  }
  .av-slot {
    flex-shrink: 0;
    opacity: 0.75;
  }
  /* Keeps a grouped row's text aligned under the author's name. Empty on
     purpose: there is no hover timestamp to reveal, because the row above
     already prints an absolute one. */
  .gutter {
    width: 38px;
    flex-shrink: 0;
  }
  .msg-main {
    min-width: 0;
    flex: 1;
  }
  .msg-head {
    display: flex;
    gap: var(--sp-2);
    align-items: baseline;
    min-width: 0;
    flex-wrap: wrap;
  }
  /* Muted, and not a button: an archived author has no profile card here. */
  .sender {
    font-weight: 600;
    color: var(--text-muted);
    font-size: var(--fs-ui);
  }
  .time {
    color: var(--text-faint);
    font-size: var(--fs-tiny);
  }
  /* Same metrics as a live body (Message.svelte), one shade quieter. */
  .body {
    margin-top: 2px;
    color: var(--text-muted);
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
    max-width: var(--measure, 92ch);
  }
  .body :global(a) {
    color: var(--accent-hover);
  }
  .body :global(code) {
    background: var(--bg-2);
    border-radius: var(--radius-sm);
    padding: 1px 4px;
    font-family: var(--mono-font);
    font-size: 0.92em;
  }
  .body :global(pre) {
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: var(--sp-2);
    overflow-x: auto;
    margin: var(--sp-1) 0;
  }
  .body :global(pre code) {
    background: transparent;
    padding: 0;
  }
  .body :global(img.emoji),
  .body :global(img.cemoji) {
    width: 1.25em;
    height: 1.25em;
    vertical-align: -0.22em;
    object-fit: contain;
  }
  .body :global(blockquote) {
    border-left: 3px solid var(--border);
    margin: 2px 0;
    padding-left: var(--sp-2);
  }
  /* A file the archive names and does not hold. File-shaped, so the eye reads
     "there was something here" rather than "something went wrong". */
  .ghost-file {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    max-width: 420px;
    margin-top: var(--sp-1);
    padding: 6px var(--sp-2);
    border: 1px dashed var(--border);
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--bg-2) 60%, transparent);
    color: var(--text-faint);
    font-size: var(--fs-compact);
  }
  .gf-ic {
    display: inline-flex;
    opacity: 0.7;
    flex-shrink: 0;
  }
  .gf-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-muted);
  }
  .gf-size {
    flex-shrink: 0;
  }
  .gf-note {
    flex-shrink: 0;
    margin-left: auto;
    font-size: var(--fs-tiny);
    font-style: italic;
  }
  .arc-reactions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
    margin-top: var(--sp-1);
  }
  .arc-reaction {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 1px 7px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--bg-2);
    color: var(--text-faint);
    font-size: var(--fs-small);
    line-height: 1.6;
  }
  .arc-reaction .cemoji {
    width: 15px;
    height: 15px;
    object-fit: contain;
  }
  .rcount {
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
</style>
