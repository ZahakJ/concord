<script>
  // One message on its way out: dimmed, with a clock where the timestamp will
  // be, and in the exact place the real row is about to appear.
  //
  // Its own component, the way ArchiveMessage is. Message.svelte is 2,900 lines
  // of reactions, hover toolbar, context menu, embeds, seals and reveals, all
  // of which need an id and a stored `sent` — a pending row has neither. The
  // things it must get right are the gutter width, the body colour and the
  // grouping, and those are cheap to state exactly once.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { S, memberByFpr } from "./lib/state.svelte.js";
  import { renderMarkdown } from "./lib/markdown.js";
  import { previewText } from "./lib/attachments.js";
  import { stripEphemeral } from "./lib/ephemeral.svelte.js";
  import { stripTimestamp } from "./lib/timestamp.js";
  import { retry, discard } from "./lib/outbox.svelte.js";

  let { entry, first = false } = $props();

  const me = $derived(memberByFpr(S.identity.fingerprint));
  const myName = $derived(S.displayName || me?.name || "You");
  const failed = $derived(entry.state === "failed");

  // The same two strips the feed applies before rendering a body, so the
  // pending row shows the message and not the tokens that ride in front of it.
  const bodyText = $derived(
    entry.kind === "text" ? stripTimestamp(stripEphemeral(entry.body || "")) : "",
  );
  // Deliberately no mention table and no link previews: a row that is not a
  // message yet must not ping anybody or fetch anything on its behalf.
  const html = $derived(bodyText ? renderMarkdown(bodyText, [], null, null) : "");
</script>

<div class="pmsg" class:compact={!first} class:failed role="article" aria-busy={!failed}>
  {#if first}
    <span class="pav" aria-hidden="true">
      <Avatar
        name={myName}
        emoji={me?.emoji}
        color={me?.color}
        color2={me?.color2}
        image={me?.avatar}
        size={38}
        frame={me?.frame}
        decoration={me?.style?.dec || ""}
        dc={me?.style?.dc || ""}
        style={me?.style}
      />
    </span>
  {:else}
    <span class="pgutter" aria-hidden="true">
      <Icon name="clock" size={11} />
    </span>
  {/if}

  <div class="pmain">
    {#if first}
      <div class="phdr">
        <strong>{myName}</strong>
        <span class="pstate">
          {#if failed}Not sent{:else}<Icon name="clock" size={10} /> Sending…{/if}
        </span>
      </div>
    {/if}
    {#if entry.kind === "att"}
      <div class="patt">
        {#if entry.att?.isImage}
          <img src={entry.att.dataUrl} alt="" />
        {:else}
          <span class="pfile"><Icon name="attach" size={14} /> {entry.att?.name || "file"}</span>
        {/if}
      </div>
    {:else if html}
      <!-- eslint-disable-next-line svelte/no-at-html-tags -->
      <div class="pbody">{@html html}</div>
    {:else}
      <div class="pbody muted">{previewText(entry.body || "")}</div>
    {/if}
    {#if failed}
      <div class="pfail" role="status">
        <Icon name="alert" size={12} />
        <span>{entry.error || "Couldn't send"}</span>
        <button type="button" class="pbtn" onclick={() => retry(entry.id)}>Retry</button>
        <button type="button" class="pbtn quiet" onclick={() => discard(entry.id)}>Discard</button>
      </div>
    {:else if !first}
      <span class="psending" aria-hidden="true">Sending…</span>
    {/if}
  </div>
</div>

<style>
  /* Geometry copied from .msg deliberately, not shared: the gutter is 38px
     because that is the avatar's width, and a pending row that does not line up
     with the row it is about to become makes the feed step sideways on
     promotion. */
  .pmsg {
    display: flex;
    gap: var(--sp-3);
    position: relative;
    padding: var(--msg-pad-y, 2px) 0;
    border-radius: var(--radius-sm);
    /* The whole point: it is here, and it is not landed yet. */
    opacity: 0.6;
  }
  .pmsg.failed {
    opacity: 1;
  }
  .pav {
    flex: none;
    align-self: flex-start;
  }
  .pgutter {
    width: 38px;
    flex: none;
    display: flex;
    justify-content: flex-end;
    padding-top: var(--sp-1);
    color: var(--text-faint);
  }
  .pmain {
    min-width: 0;
    flex: 1;
  }
  .phdr {
    display: flex;
    align-items: baseline;
    gap: var(--sp-2);
  }
  .pstate,
  .psending {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    color: var(--text-faint);
    font-size: var(--fs-micro);
  }
  .pbody {
    word-break: break-word;
    white-space: pre-wrap;
  }
  .patt img {
    max-width: 220px;
    max-height: 160px;
    border-radius: var(--radius-sm);
  }
  .pfile {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--text-muted);
    font-size: var(--fs-compact);
  }
  .pfail {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin-top: var(--sp-1);
    color: var(--danger-text);
    font-size: var(--fs-compact);
  }
  .pbtn {
    padding: 2px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-2);
    color: var(--text);
    font-size: var(--fs-compact);
  }
  .pbtn:hover {
    background: var(--bg-3);
  }
  .pbtn.quiet {
    border-color: transparent;
    background: transparent;
    color: var(--text-muted);
  }
</style>
