<script>
  // One message row. `compact` renders the grouped continuation form (no
  // avatar/header, hover timestamp). The action bar is keyboard-reachable
  // (focus-within) with labelled icon buttons.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import { renderMarkdown } from "./lib/markdown.js";
  import {
    S,
    memberByFpr,
    react,
    deleteMsg,
    saveEdit,
    scrollToMessage,
    flash,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";

  let { m, compact = false, replyRef = null } = $props();

  const QUICK_EMOJIS = ["👍", "❤️", "😂", "🎉"];

  const mem = $derived(memberByFpr(m.sender));
  const mentionNames = $derived(S.displayName ? [S.displayName] : []);
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
    <Avatar
      name={m.senderName || m.sender}
      emoji={mem?.emoji}
      color={mem?.color}
      image={mem?.avatar}
      size={38}
    />
  {/if}

  <div class="msg-main">
    {#if m.replyTo && !compact}
      <button class="reply-ref" onclick={jumpToReply}>
        <Icon name="reply" size={11} />
        {replyRef
          ? `${replyRef.senderName || replyRef.sender.slice(0, 9)}: ${replyRef.deleted ? "(deleted)" : replyRef.content.slice(0, 60)}`
          : "(original message)"}
      </button>
    {/if}
    {#if !compact}
      <div class="msg-head">
        <span class="sender">{m.senderName || m.sender}</span>
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
      <div class="body">
        {@html renderMarkdown(m.content, mentionNames)}{#if m.edited}<span class="edited-tag">
            (edited)</span
          >{/if}
      </div>
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
      {#each QUICK_EMOJIS as e (e)}
        <button title="React {e}" aria-label="React {e}" onclick={() => react(m, e)}>{e}</button>
      {/each}
      <button title="More reactions" aria-label="More reactions" onclick={() => (S.pickerTarget = m)}>
        <Icon name="smile" size={14} />
      </button>
      <button title="Reply" aria-label="Reply" onclick={() => (S.replyingTo = m)}>
        <Icon name="reply" size={14} />
      </button>
      <button
        title={m.pinned ? "Unpin" : "Pin"}
        aria-label={m.pinned ? "Unpin" : "Pin"}
        onclick={() => api.pinMessage(m.channelId, m.id)}
      >
        <Icon name="pin" size={14} />
      </button>
      {#if m.sender === S.identity.fingerprint}
        <button title="Edit" aria-label="Edit" onclick={startEdit}><Icon name="edit" size={14} /></button>
        <button title="Delete" aria-label="Delete" onclick={() => deleteMsg(m)}>
          <Icon name="trash" size={14} />
        </button>
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
  .sender {
    font-weight: 600;
    color: var(--accent-hover);
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
    top: -12px;
    right: 8px;
    display: flex;
    gap: 2px;
    opacity: 0;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 2px;
    box-shadow: var(--shadow-pop);
    z-index: 5;
  }
  .msg:hover .msg-actions,
  .msg:focus-within .msg-actions {
    opacity: 1;
  }
  .msg-actions button {
    background: transparent;
    border: none;
    color: var(--text-muted);
    padding: 3px 6px;
    font-size: 13px;
    display: grid;
    place-items: center;
  }
  .msg-actions button:hover {
    background: var(--bg-3);
    color: var(--text);
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
    background: var(--accent-soft);
    color: var(--accent-hover);
    border-radius: 4px;
    padding: 0 3px;
    font-weight: 600;
  }
  .body :global(img.attachment) {
    max-width: 380px;
    max-height: 280px;
    border-radius: var(--radius-sm);
    display: block;
    margin-top: 4px;
  }
</style>
