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
  import { renderMarkdown, emojiOnly } from "./lib/markdown.js";
  import { previewText } from "./lib/attachments.js";
  import { stripEphemeral } from "./lib/ephemeral.svelte.js";
  import { stripTimestamp } from "./lib/timestamp.js";
  import { retry, discard } from "./lib/outbox.svelte.js";

  let { entry, first = false } = $props();

  const me = $derived(memberByFpr(S.identity.fingerprint));
  const myName = $derived(S.displayName || me?.name || "You");
  const failed = $derived(entry.state === "failed");
  const avSize = $derived(S.isMobile ? 34 : S.prefs.density === "comfortable" ? 40 : 36);

  // A slow-mode refusal carries the MOMENT it lifts, not the number the core
  // happened to say. The number was captured once and never ticked: read at
  // t+0, t+5s, t+10s and t+15s the row said "wait 14s" every time, so ten
  // seconds later it was stating a falsehood and thirty seconds later it still
  // said wait when Retry would have worked. One second-tick, shared by every
  // row that has one, and only while one does.
  let now = $state(Date.now());
  $effect(() => {
    if (!entry.retryAt) return;
    const t = setInterval(() => (now = Date.now()), 500);
    return () => clearInterval(t);
  });
  const waitLeft = $derived(
    entry.retryAt ? Math.max(0, Math.ceil((entry.retryAt - now) / 1000)) : 0,
  );

  // The same two strips the feed applies before rendering a body, so the
  // pending row shows the message and not the tokens that ride in front of it.
  const bodyText = $derived(
    entry.kind === "text" ? stripTimestamp(stripEphemeral(entry.body || "")) : "",
  );
  // Deliberately no mention table and no link previews: a row that is not a
  // message yet must not ping anybody or fetch anything on its behalf.
  const html = $derived(bodyText ? renderMarkdown(bodyText, [], null, null) : "");
  // The feed draws an emoji-only message jumbo; so does this row, or the
  // emoji would jump in size the moment the message is stored.
  const jumbo = $derived(!!bodyText && emojiOnly(bodyText));
</script>

<div class="pmsg" class:compact={!first} class:failed role="article" aria-busy={!failed} style:--av="{avSize}px">
  {#if first}
    <span class="pav" aria-hidden="true">
      <Avatar
        name={myName}
        emoji={me?.emoji}
        color={me?.color}
        color2={me?.color2}
        image={me?.avatar}
        size={avSize}
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
      <div class="pbody" class:jumbo>{@html html}</div>
    {:else}
      <div class="pbody muted">{previewText(entry.body || "")}</div>
    {/if}
    {#if failed}
      <div class="pfail" role="status" class:waiting={waitLeft > 0}>
        <Icon name={entry.retryAt ? "clock" : "alert"} size={12} />
        <span>
          {#if waitLeft > 0}
            Slow mode — you can send this in {waitLeft}s
          {:else if entry.retryAt}
            Slow mode has passed
          {:else}
            {entry.error || "Couldn't send"}
          {/if}
        </span>
        <button type="button" class="pbtn" disabled={waitLeft > 0} onclick={() => retry(entry.id)}>
          Retry
        </button>
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
  /* Groups are made in the markup (see Message.svelte): the first row of a
     run carries the group's space, the rest sit flush. */
  .pmsg:not(.compact) {
    margin-top: var(--msg-gap, 16px);
  }
  .pmsg.failed {
    opacity: 1;
    box-shadow: inset 2px 0 0 var(--danger-soft);
  }
  :global(:root[data-density="compact"]) .pav,
  :global(:root[data-density="compact"]) .pgutter {
    display: none;
  }
  .pav {
    flex: none;
    align-self: flex-start;
  }
  .pgutter {
    width: var(--av);
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
    font-size: var(--fs-tiny);
  }
  .phdr strong {
    font-weight: 600;
  }
  .pbody {
    word-break: break-word;
    white-space: pre-wrap;
  }
  /* Message.svelte's sizes, so promotion to a stored row changes nothing. */
  .pbody :global(img.emoji) {
    margin: 0 0.5px;
  }
  .pbody.jumbo :global(img.emoji) {
    width: 3em;
    height: 3em;
    vertical-align: bottom;
    margin: 0 1.5px;
  }
  .pbody.jumbo :global(img.cemoji) {
    height: 2.6em;
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
  /* Waiting is not failing. The row keeps its place and its Retry, in the
     app's "hold on" colour rather than its "something broke" one. */
  .pfail.waiting {
    color: var(--warn-text);
  }
  .pbtn:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .pfail {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin-top: var(--sp-1);
    color: var(--danger-text);
    font-size: var(--fs-small);
  }
  /* Text buttons in the sentence, the way an inline action reads. */
  .pbtn {
    padding: 0 2px;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--accent-hover);
    font-size: var(--fs-small);
    font-weight: 500;
  }
  .pbtn:hover {
    text-decoration: underline;
  }
  .pbtn.quiet {
    color: var(--text-muted);
  }
</style>
