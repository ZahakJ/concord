<script>
  // One message row. `compact` renders the grouped continuation form (no
  // avatar/header, hover timestamp). The action bar is keyboard-reachable
  // (focus-within) with labelled icon buttons.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import Attachment from "./Attachment.svelte";
  import FileAttachment from "./FileAttachment.svelte";
  import YouTubeEmbed from "./YouTubeEmbed.svelte";
  import LinkPreview from "./LinkPreview.svelte";
  import { renderMarkdown } from "./lib/markdown.js";
  import { parseAttachTokens, parseFileTokens, stripAttachTokens, previewText } from "./lib/attachments.js";
  import { extractLinks, youtubeID } from "./lib/embeds.js";
  import {
    S,
    memberByFpr,
    nameFor,
    react,
    deleteMsg,
    saveEdit,
    scrollToMessage,
    flash,
    openProfilePopover,
    scheduleCloseProfilePopover,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";

  let { m, compact = false, replyRef = null } = $props();

  const QUICK_EMOJIS = ["👍", "❤️", "😂", "🎉"];

  const mem = $derived(memberByFpr(m.sender));
  // Highlight every member's @name; the viewer's own name gets the self style.
  const mentionNames = $derived(
    S.members
      .filter((mm) => mm.name)
      .map((mm) => ({ name: mm.name, self: mm.isSelf })),
  );

  // @mentions open a floating profile card — on hover (with intent delay) and
  // immediately on click.
  function mentionMember(target) {
    const el = target.closest?.(".mention");
    if (!el) return null;
    const m = S.members.find((mm) => mm.name === el.dataset.mention);
    return m ? { el, fpr: m.fingerprint } : null;
  }
  function onBodyClick(e) {
    const hit = mentionMember(e.target);
    if (hit) {
      e.preventDefault();
      openProfilePopover(hit.fpr, hit.el);
    }
  }
  function onBodyOver(e) {
    const hit = mentionMember(e.target);
    if (hit) openProfilePopover(hit.fpr, hit.el, { delay: 320 });
  }
  function onBodyOut(e) {
    if (mentionMember(e.target)) scheduleCloseProfilePopover();
  }
  const atts = $derived(m.deleted ? [] : parseAttachTokens(m.content));
  const files = $derived(m.deleted ? [] : parseFileTokens(m.content));
  const bodyText = $derived(atts.length || files.length ? stripAttachTokens(m.content) : m.content);
  // One embed per message: the first YouTube link gets a player; otherwise
  // the first link gets a preview card.
  const embed = $derived.by(() => {
    if (m.deleted || m.kind !== "") return null;
    for (const url of extractLinks(m.content)) {
      const yt = youtubeID(url);
      if (yt) return { kind: "yt", id: yt, url };
      return { kind: "card", url };
    }
    return null;
  });
  let editDraft = $state("");

  // Seed the edit draft whenever this message becomes the edit target — the
  // trigger can come from elsewhere too (ArrowUp in an empty composer).
  $effect(() => {
    if (S.editing?.id === m.id) editDraft = m.content;
  });

  function startEdit() {
    S.editing = m;
  }

  function fmtTime(iso) {
    try {
      return new Date(iso).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    } catch {
      return "";
    }
  }

  function jumpToReply() {
    if (m.replyTo && !scrollToMessage(m.replyTo)) flash("Original message not loaded");
  }
</script>

<div class="msg" class:compact data-msg-id={m.id}>
  {#if compact}
    <span class="gutter-time muted">{fmtTime(m.sent)}</span>
  {:else}
    <button
      class="av-btn"
      title="View profile"
      onclick={(e) => openProfilePopover(m.sender, e.currentTarget)}
    >
      <Avatar
        name={nameFor(m.sender, m.senderName)}
        emoji={mem?.emoji}
        color={mem?.color}
        image={mem?.avatar}
        size={38}
      />
    </button>
  {/if}

  <div class="msg-main">
    {#if m.replyTo && !compact}
      <button class="reply-ref" onclick={jumpToReply}>
        <Icon name="reply" size={11} />
        {replyRef
          ? `${nameFor(replyRef.sender, replyRef.senderName)}: ${replyRef.deleted ? "(deleted)" : previewText(replyRef.content).slice(0, 60)}`
          : "(original message)"}
      </button>
    {/if}
    {#if !compact}
      <div class="msg-head">
        <button class="sender" onclick={(e) => openProfilePopover(m.sender, e.currentTarget)}
          >{nameFor(m.sender, m.senderName)}</button
        >
        <span class="muted mono verify-fpr" title="verified identity">{m.sender.slice(0, 9)}</span>
        <span class="muted time">{fmtTime(m.sent)}</span>
        {#if m.pinned}<span class="pin-mark" title="Pinned"><Icon name="pin" size={11} /></span>{/if}
      </div>
    {:else if m.pinned}
      <span class="pin-mark inline" title="Pinned"><Icon name="pin" size={11} /></span>
    {/if}

    {#if m.deleted}
      <div class="body deleted"><em>message deleted</em></div>
    {:else if S.editing?.id === m.id}
      <!-- svelte-ignore a11y_autofocus -->
      <input
        class="edit-input"
        bind:value={editDraft}
        autofocus
        onkeydown={(e) => {
          if (e.key === "Enter") saveEdit(m, editDraft);
          else if (e.key === "Escape") S.editing = null;
        }}
        onblur={() => saveEdit(m, editDraft)}
      />
    {:else}
      {#if bodyText}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="body" onclick={onBodyClick} onmouseover={onBodyOver} onmouseout={onBodyOut} onfocusin={onBodyOver}>
          {@html renderMarkdown(bodyText, mentionNames)}{#if m.edited}<span class="edited-tag">
              (edited)</span
            >{/if}
        </div>
      {/if}
      {#each atts as tok (tok.blobId)}
        <Attachment channelId={m.channelId} {tok} />
      {/each}
      {#each files as tok (tok.blobId)}
        <FileAttachment channelId={m.channelId} {tok} />
      {/each}
      {#if embed?.kind === "yt"}
        <YouTubeEmbed videoId={embed.id} />
      {:else if embed?.kind === "card"}
        {#key embed.url}
          <LinkPreview url={embed.url} />
        {/key}
      {/if}
    {/if}

    {#if m.reactions && Object.keys(m.reactions).length}
      <div class="reactions">
        {#each Object.entries(m.reactions) as [emoji, fprs] (emoji)}
          <button
            class="reaction"
            class:mine={fprs.includes(S.identity.fingerprint)}
            onclick={() => react(m, emoji)}
            title={fprs.map((f) => memberByFpr(f)?.name || f.slice(0, 9)).join(", ")}
          >
            {emoji} {fprs.length}
          </button>
        {/each}
      </div>
    {/if}
  </div>

  {#if !m.deleted}
    <div class="msg-actions" role="toolbar" aria-label="Message actions">
      <div class="grp">
        {#each QUICK_EMOJIS as e (e)}
          <button class="emoji-btn" title="React {e}" aria-label="React {e}" onclick={() => react(m, e)}>{e}</button>
        {/each}
        <button title="More reactions" aria-label="More reactions" onclick={() => (S.pickerTarget = m)}>
          <Icon name="smile" size={15} />
        </button>
      </div>
      <span class="sep"></span>
      <div class="grp">
        <button title="Reply" aria-label="Reply" onclick={() => (S.replyingTo = m)}>
          <Icon name="reply" size={15} />
        </button>
        <button
          class:on={m.pinned}
          title={m.pinned ? "Unpin" : "Pin"}
          aria-label={m.pinned ? "Unpin" : "Pin"}
          onclick={() => api.pinMessage(m.channelId, m.id)}
        >
          <Icon name="pin" size={15} />
        </button>
      </div>
      {#if m.sender === S.identity.fingerprint}
        <span class="sep"></span>
        <div class="grp">
          <button title="Edit" aria-label="Edit" onclick={startEdit}><Icon name="edit" size={15} /></button>
          <button class="danger" title="Delete" aria-label="Delete" onclick={() => deleteMsg(m)}>
            <Icon name="trash" size={15} />
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .msg {
    display: flex;
    gap: 12px;
    position: relative;
    padding: 2px 0;
    border-radius: var(--radius-sm);
  }
  .msg:hover {
    background: color-mix(in srgb, var(--bg-3) 40%, transparent);
  }
  .msg.compact {
    margin-top: -10px;
  }
  .gutter-time {
    width: 38px;
    font-size: 10px;
    text-align: right;
    opacity: 0;
    flex-shrink: 0;
    padding-top: 4px;
  }
  .msg.compact:hover .gutter-time {
    opacity: 1;
  }
  .msg-main {
    min-width: 0;
    flex: 1;
  }
  .reply-ref {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 12px;
    color: var(--text-muted);
    border-left: 2px solid var(--border);
    padding: 0 0 0 8px;
    margin-bottom: 2px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    background: transparent;
    border-radius: 0;
    max-width: 100%;
  }
  .reply-ref:hover {
    background: transparent;
    color: var(--text);
  }
  .msg-head {
    display: flex;
    gap: 8px;
    align-items: baseline;
  }
  .av-btn {
    background: transparent;
    border: none;
    padding: 0;
    border-radius: 50%;
    cursor: pointer;
    flex-shrink: 0;
    align-self: flex-start;
  }
  .av-btn:hover {
    background: transparent;
  }
  .av-btn :global(.avatar) {
    transition:
      box-shadow 0.12s ease,
      transform 0.12s ease;
  }
  .av-btn:hover :global(.avatar) {
    box-shadow: 0 0 0 2px var(--accent);
  }
  .sender {
    background: transparent;
    border: none;
    padding: 0;
    font: inherit;
    font-weight: 600;
    color: var(--accent-hover);
    cursor: pointer;
  }
  .sender:hover {
    background: transparent;
    text-decoration: underline;
  }
  .verify-fpr {
    font-size: 10px;
  }
  .time {
    font-size: 11px;
  }
  .pin-mark {
    color: var(--warn);
  }
  .pin-mark.inline {
    float: right;
  }
  .body {
    margin-top: 2px;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .body.deleted {
    color: var(--text-muted);
  }
  .edited-tag {
    font-size: 10px;
    color: var(--text-muted);
  }
  .edit-input {
    margin-top: 2px;
  }
  .reactions {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 4px;
  }
  .reaction {
    background: var(--bg-3);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 1px 8px;
    font-size: 12px;
    border-radius: 10px;
  }
  .reaction.mine {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .msg-actions {
    position: absolute;
    top: -16px;
    right: 10px;
    display: flex;
    align-items: center;
    gap: 3px;
    opacity: 0;
    transform: translateY(2px);
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: 10px;
    padding: 3px;
    box-shadow: var(--shadow-pop);
    z-index: 5;
    transition:
      opacity 0.1s ease,
      transform 0.1s ease;
  }
  .msg:hover .msg-actions,
  .msg:focus-within .msg-actions {
    opacity: 1;
    transform: none;
  }
  .grp {
    display: flex;
    gap: 1px;
  }
  .sep {
    width: 1px;
    align-self: stretch;
    margin: 3px 2px;
    background: var(--border);
  }
  .msg-actions button {
    background: transparent;
    border: none;
    color: var(--text-muted);
    padding: 5px;
    min-width: 28px;
    height: 28px;
    font-size: 14px;
    display: grid;
    place-items: center;
    border-radius: 7px;
    transition:
      background 0.1s ease,
      transform 0.08s ease;
  }
  .msg-actions button:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .msg-actions .emoji-btn:hover {
    transform: scale(1.18);
    background: transparent;
  }
  .msg-actions button.on {
    color: var(--warn);
  }
  .msg-actions button.danger:hover {
    background: var(--danger-soft);
    color: var(--danger);
  }
  .body :global(code) {
    background: var(--bg-3);
    padding: 1px 5px;
    border-radius: 4px;
    font-family: ui-monospace, monospace;
    font-size: 12px;
  }
  .body :global(pre) {
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    overflow-x: auto;
    margin: 4px 0;
  }
  .body :global(pre code) {
    background: transparent;
    padding: 0;
  }
  .body :global(blockquote) {
    margin: 2px 0;
    padding: 0 0 0 8px;
    border-left: 3px solid var(--border);
    color: var(--text-muted);
  }
  .body :global(ul),
  .body :global(ol) {
    margin: 2px 0;
    padding-left: 22px;
  }
  .body :global(a) {
    color: var(--accent-hover);
  }
  .body :global(.mention) {
    background: color-mix(in srgb, var(--text-muted) 22%, transparent);
    color: var(--text);
    border-radius: 4px;
    padding: 0 3px;
    font-weight: 600;
    cursor: pointer;
  }
  .body :global(.mention:hover) {
    background: color-mix(in srgb, var(--text-muted) 34%, transparent);
  }
  .body :global(.mention-self) {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .body :global(.mention-self:hover) {
    background: color-mix(in srgb, var(--accent) 26%, transparent);
  }
  .body :global(img.attachment) {
    max-width: 380px;
    max-height: 280px;
    border-radius: var(--radius-sm);
    display: block;
    margin-top: 4px;
  }
</style>
