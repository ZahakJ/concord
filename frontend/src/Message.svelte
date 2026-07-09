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
  import { untrack } from "svelte";
  import { renderMarkdown, emojiOnly } from "./lib/markdown.js";
  import { parseAttachTokens, parseFileTokens, stripAttachTokens, previewText } from "./lib/attachments.js";
  import { extractLinks, youtubeID } from "./lib/embeds.js";
  import {
    S,
    memberByFpr,
    nameColorFor,
    nameFor,
    customEmojiMap,
    react,
    deleteMsg,
    saveEdit,
    scrollToMessage,
    flash,
    openProfilePopover,
    scheduleCloseProfilePopover,
    openContextMenu,
    markUnread,
    activeGuild,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PERM, has } from "./lib/perms.js";
  import { recentEmoji, pushRecentEmoji } from "./lib/emoji.js";

  // `entering` is set by MessageList for the newest appended message only, so
  // it fades/slides in once — history rows never animate.
  let { m, compact = false, replyRef = null, entering = false } = $props();

  // Moderators (Manage Messages) can delete anyone's message.
  const canDeleteOthers = $derived(has(activeGuild()?.myPerms || 0, PERM.MANAGE_MESSAGES));

  // Quick-reaction bar: the viewer's recently-used emoji padded with a
  // default set, capped at 5. Computed once per row (fresh rows pick up new
  // recents; recents only ever hold unicode chars).
  const DEFAULT_QUICK = ["👍", "❤️", "😂", "🎉", "🔥"];
  const quickEmojis = [...new Set([...recentEmoji(), ...DEFAULT_QUICK])].slice(0, 5);

  // The emoji the user just tapped bounces briefly (quick bar + pills share
  // this, keyed by emoji char).
  let bounced = $state(null);
  let bounceTimer;
  function reactWithBounce(emoji) {
    clearTimeout(bounceTimer);
    bounced = null; // restart the CSS animation on rapid re-clicks
    requestAnimationFrame(() => {
      bounced = emoji;
      bounceTimer = setTimeout(() => (bounced = null), 500);
    });
    if (!emoji.startsWith(":")) pushRecentEmoji(emoji); // unicode only (custom emoji are guild-scoped)
    react(m, emoji);
  }

  const mem = $derived(memberByFpr(m.sender));
  const cemoji = $derived(customEmojiMap());
  // Highlight every member's @name; the viewer's own name gets the self style.
  const mentionNames = $derived([
    // @everyone / @here highlight for everyone (self:true so they stand out).
    { name: "everyone", self: true },
    { name: "here", self: true },
    ...S.members.filter((mm) => mm.name).map((mm) => ({ name: mm.name, self: mm.isSelf })),
  ]);

  // @mentions open a floating profile card — on hover (with intent delay) and
  // immediately on click.
  function mentionMember(target) {
    const el = target.closest?.(".mention");
    if (!el) return null;
    const m = S.members.find((mm) => mm.name === el.dataset.mention);
    return m ? { el, fpr: m.fingerprint } : null;
  }
  // Reveal a focused spoiler with Enter/Space (it's role=button tabindex=0).
  function onBodyKeydown(e) {
    if (e.key !== "Enter" && e.key !== " ") return;
    const spoiler = e.target.closest?.(".spoiler");
    if (spoiler && !spoiler.classList.contains("revealed")) {
      e.preventDefault();
      spoiler.classList.add("revealed");
    }
  }

  function onBodyClick(e) {
    // Reveal a spoiler on click (first click only).
    const spoiler = e.target.closest?.(".spoiler");
    if (spoiler && !spoiler.classList.contains("revealed")) {
      e.preventDefault();
      spoiler.classList.add("revealed");
      return;
    }
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
  let editCancelled = false;
  let wasEditing = false;

  // Seed the edit draft ONCE, when this message becomes the edit target (via the
  // menu or ArrowUp in an empty composer). untrack keeps a later reaction/edit
  // event that swaps `m` from wiping what the user is typing.
  $effect(() => {
    const editing = S.editing?.id === m.id;
    if (editing && !wasEditing) {
      editDraft = untrack(() => m.content);
      editCancelled = false;
    }
    wasEditing = editing;
  });

  function startEdit() {
    S.editing = m;
  }
  function cancelEdit() {
    editCancelled = true; // so the textarea's blur handler doesn't save it
    S.editing = null;
  }
  function commitEdit() {
    if (!editCancelled) saveEdit(m, editDraft);
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

  // Reply preview: the original message with attachment tokens turned into a
  // readable placeholder, whitespace collapsed, and capped for one line.
  const replySnippet = $derived(
    replyRef && !replyRef.deleted
      ? previewText(replyRef.content).replace(/\s+/g, " ").trim().slice(0, 80) || "(empty message)"
      : "",
  );

  function copy(text, ok) {
    navigator.clipboard?.writeText(text);
    flash(ok, "success");
  }

  const isOwn = $derived(m.sender === S.identity.fingerprint);

  function messageMenu(e) {
    if (m.deleted) return;
    openContextMenu(e, [
      { label: "Reply", icon: "reply", onClick: () => (S.replyingTo = m) },
      isOwn && { label: "Edit", icon: "edit", onClick: startEdit },
      { label: "Add Reaction", icon: "smile", onClick: () => (S.pickerTarget = m) },
      { sep: true },
      { label: "Copy Text", icon: "edit", onClick: () => copy(stripAttachTokens(m.content).trim() || previewText(m.content), "Copied text") },
      {
        label: "Copy Message Link",
        icon: "forward",
        onClick: () => copy(`concord://msg/${m.channelId}/${m.id}`, "Copied message link"),
      },
      { label: m.pinned ? "Unpin" : "Pin", icon: "pin", onClick: () => api.pinMessage(m.channelId, m.id) },
      { label: "Forward", icon: "forward", onClick: () => (S.modal = { kind: "forward", message: m }) },
      { label: "Mark Unread", icon: "bell", onClick: () => markUnread(m.channelId, m) },
      (isOwn || canDeleteOthers) && { sep: true },
      (isOwn || canDeleteOthers) && {
        label: "Delete",
        icon: "trash",
        danger: true,
        onClick: () => deleteMsg(m),
      },
    ]);
  }
</script>

<div class="msg" class:compact class:enter={entering} data-msg-id={m.id} oncontextmenu={messageMenu}>
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
      <button class="reply-ref" title="Jump to original message" onclick={jumpToReply}>
        <span class="reply-icon"><Icon name="reply" size={11} /></span>
        {#if replyRef}
          <span
            class="reply-author"
            style={nameColorFor(replyRef.sender) ? `color:${nameColorFor(replyRef.sender)}` : ""}
            >{nameFor(replyRef.sender, replyRef.senderName)}</span
          >
          {#if replyRef.deleted}
            <em class="reply-snippet faded">message deleted</em>
          {:else}
            <span class="reply-snippet">{replySnippet}</span>
          {/if}
        {:else}
          <em class="reply-snippet faded">original message not loaded</em>
        {/if}
      </button>
    {/if}
    {#if !compact}
      <div class="msg-head">
        <button
          class="sender"
          style={nameColorFor(m.sender) ? `color:${nameColorFor(m.sender)}` : ""}
          onclick={(e) => openProfilePopover(m.sender, e.currentTarget)}
          >{nameFor(m.sender, m.senderName)}</button
        >
        <span
          class="muted mono verify-fpr"
          class:verified={memberByFpr(m.sender)?.verified}
          title={memberByFpr(m.sender)?.verified
            ? "Identity verified"
            : "Sender fingerprint — not verified"}>{m.sender.slice(0, 9)}</span
        >
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
      <textarea
        class="edit-input"
        rows="1"
        bind:value={editDraft}
        autofocus
        onkeydown={(e) => {
          if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
            e.preventDefault();
            commitEdit();
          } else if (e.key === "Escape") {
            e.preventDefault();
            cancelEdit();
          }
        }}
        onblur={commitEdit}
      ></textarea>
      <div class="edit-hint muted">escape to cancel · enter to save</div>
    {:else}
      {#if bodyText}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="body" class:jumbo={emojiOnly(bodyText)} onclick={onBodyClick} onkeydown={onBodyKeydown} onmouseover={onBodyOver} onmouseout={onBodyOut} onfocusin={onBodyOver}>
          {@html renderMarkdown(bodyText, mentionNames, cemoji)}{#if m.edited}<span
              class="edited-tag">(edited)</span
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
          {@const cimg = /^:([a-z0-9_]{2,32}):$/.test(emoji) ? cemoji[emoji.slice(1, -1)] : null}
          <span class="react-wrap">
            <button
              class="reaction"
              class:mine={fprs.includes(S.identity.fingerprint)}
              onclick={() => reactWithBounce(emoji)}
            >
              <span class="remoji" class:bounce={bounced === emoji}>
                {#if cimg}<img class="cemoji" src={cimg} alt={emoji} />{:else}{emoji}{/if}
              </span>
              <!-- keyed so the count re-mounts (and animates) when it changes -->
              {#key fprs.length}<span class="rcount">{fprs.length}</span>{/key}
            </button>
            <!-- who reacted, on hover -->
            <span class="react-who">
              <strong>
                {#if cimg}<img class="cemoji" src={cimg} alt={emoji} />{:else}{emoji}{/if}
                · {fprs.length}
              </strong>
              {#each fprs.slice(0, 12) as f (f)}
                <span class="rw-row">{memberByFpr(f)?.name || f.slice(0, 9)}</span>
              {/each}
              {#if fprs.length > 12}<span class="rw-more">+{fprs.length - 12} more</span>{/if}
            </span>
          </span>
        {/each}
      </div>
    {/if}
  </div>

  {#if !m.deleted && S.editing?.id !== m.id}
    <div class="msg-actions" role="toolbar" aria-label="Message actions">
      <div class="grp">
        {#each quickEmojis as e (e)}
          <button class="emoji-btn" class:bounce={bounced === e} title="React {e}" aria-label="React {e}" onclick={() => reactWithBounce(e)}>{e}</button>
        {/each}
        <button class="add-react" title="More reactions" aria-label="More reactions" onclick={() => (S.pickerTarget = m)}>
          <Icon name="smile" size={15} />
          <span class="plus" aria-hidden="true">+</span>
        </button>
      </div>
      <span class="sep"></span>
      <div class="grp">
        <button title="Reply" aria-label="Reply" onclick={() => (S.replyingTo = m)}>
          <Icon name="reply" size={15} />
        </button>
        <button title="Forward" aria-label="Forward" onclick={() => (S.modal = { kind: "forward", message: m })}>
          <Icon name="forward" size={15} />
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
      {:else if canDeleteOthers}
        <span class="sep"></span>
        <div class="grp">
          <button class="danger" title="Delete (moderator)" aria-label="Delete message" onclick={() => deleteMsg(m)}>
            <Icon name="trash" size={15} />
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  /* Row rhythm comes from the density vars in app.css (Appearance setting):
     cozy is today's spacing, compact tightens padding + group pull together. */
  .msg {
    display: flex;
    gap: 12px;
    position: relative;
    padding: var(--msg-pad-y, 2px) 0;
    border-radius: var(--radius-sm);
  }
  .msg:hover {
    background: color-mix(in srgb, var(--bg-3) 40%, transparent);
  }
  .msg.compact {
    margin-top: var(--msg-group-pull, -10px);
  }
  /* Newest appended message only (see MessageList): quick fade + slide-up.
     The global reduced-motion override in app.css zeroes the duration. */
  .msg.enter {
    animation: msg-in 0.26s cubic-bezier(0.2, 0.8, 0.2, 1) backwards;
  }
  @keyframes msg-in {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
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
    padding: 1px 8px 1px 8px;
    margin-bottom: 2px;
    background: transparent;
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
    max-width: 100%;
    min-width: 0;
    transition:
      background 0.12s ease,
      border-color 0.12s ease,
      color 0.12s ease;
  }
  .reply-ref:hover {
    background: color-mix(in srgb, var(--bg-3) 65%, transparent);
    border-left-color: var(--accent);
    color: var(--text);
  }
  .reply-icon {
    display: inline-flex;
    flex-shrink: 0;
    opacity: 0.75;
  }
  .reply-author {
    font-weight: 600;
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  .reply-snippet {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .reply-snippet.faded {
    color: var(--text-faint);
  }
  .reply-ref:hover .reply-snippet:not(.faded) {
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
    margin-left: 5px;
    font-size: 10px;
    color: var(--text-faint);
    user-select: none;
    vertical-align: baseline;
  }
  .edit-input {
    margin-top: 2px;
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    min-height: 38px;
    font-family: inherit;
    line-height: 1.4;
  }
  .edit-hint {
    font-size: 11px;
    margin-top: 3px;
  }
  .reactions {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 4px;
  }
  .reaction {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 1px 8px;
    font-size: 12px;
    border-radius: 10px;
    /* springy pop when a new pill appears (overshoot bezier) */
    animation: pill-in 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
    transition:
      transform 0.1s ease,
      border-color 0.12s ease,
      background 0.12s ease,
      box-shadow 0.12s ease;
  }
  .reaction:hover {
    transform: translateY(-1px);
    border-color: var(--text-faint);
    box-shadow: 0 2px 6px rgb(0 0 0 / 0.18);
  }
  .reaction:active {
    transform: scale(0.93);
  }
  .reaction.mine {
    border-color: var(--accent);
    background: var(--accent-soft);
    box-shadow: 0 0 0 1px var(--accent); /* accent ring: you reacted */
  }
  .reaction.mine:hover {
    border-color: var(--accent);
    box-shadow:
      0 0 0 1px var(--accent),
      0 2px 6px rgb(0 0 0 / 0.18);
  }
  .reaction.mine .rcount {
    color: var(--accent-hover);
    font-weight: 600;
  }
  .remoji {
    display: inline-flex;
    line-height: 1;
  }
  .rcount {
    display: inline-block;
    min-width: 1ch;
    text-align: center;
    font-variant-numeric: tabular-nums;
    animation: count-in 0.18s ease; /* replays on {#key} re-mount */
  }
  /* Click-bounce for the emoji you just tapped (pill glyph + quick bar). */
  .remoji.bounce,
  .msg-actions .emoji-btn.bounce {
    animation: emoji-bounce 0.45s ease;
  }
  @keyframes pill-in {
    from {
      transform: scale(0.5);
      opacity: 0;
    }
  }
  @keyframes count-in {
    from {
      transform: translateY(-7px);
      opacity: 0;
    }
  }
  @keyframes emoji-bounce {
    30% {
      transform: scale(1.35) rotate(-8deg);
    }
    55% {
      transform: scale(0.92) rotate(6deg);
    }
  }
  .react-wrap {
    position: relative;
    display: inline-flex;
  }
  /* Who-reacted popover, on hover. */
  .react-who {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    z-index: 30;
    min-width: 120px;
    max-width: 220px;
    padding: 7px 10px;
    display: none;
    flex-direction: column;
    gap: 2px;
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    font-size: 12px;
    white-space: nowrap;
  }
  .react-wrap:hover .react-who {
    display: flex;
  }
  .react-who strong {
    font-size: 11px;
    color: var(--text-muted);
    margin-bottom: 2px;
  }
  .rw-more {
    color: var(--text-faint);
    font-size: 11px;
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
  /* "smiley +" picker opener */
  .msg-actions .add-react {
    position: relative;
  }
  .msg-actions .add-react .plus {
    position: absolute;
    top: 0;
    right: 2px;
    font-size: 10px;
    font-weight: 700;
    line-height: 1;
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
  /* Spoiler: blacked-out until clicked. */
  .body :global(.spoiler) {
    background: var(--text);
    color: transparent;
    border-radius: 4px;
    padding: 0 3px;
    cursor: pointer;
    user-select: none;
    transition: background 0.15s ease;
  }
  .body :global(.spoiler:hover) {
    background: color-mix(in srgb, var(--text) 82%, var(--bg-2));
  }
  .body :global(.spoiler.revealed) {
    background: color-mix(in srgb, var(--text-muted) 22%, transparent);
    color: inherit;
    cursor: text;
    user-select: text;
  }
  /* Inline headers (chat-sized). */
  .body :global(.md-h) {
    margin: 4px 0 2px;
    font-weight: 700;
    line-height: 1.25;
  }
  .body :global(h3.md-h) {
    font-size: 1.25em;
  }
  .body :global(h4.md-h) {
    font-size: 1.1em;
  }
  .body :global(h5.md-h) {
    font-size: 1em;
  }
  .body :global(u) {
    text-decoration: underline;
  }
  .body :global(s) {
    text-decoration: line-through;
    opacity: 0.85;
  }
  /* Code-fence language label. */
  .body :global(pre[data-lang]) {
    position: relative;
    padding-top: 20px;
  }
  .body :global(pre[data-lang])::before {
    content: attr(data-lang);
    position: absolute;
    top: 3px;
    right: 8px;
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .body :global(img.attachment) {
    max-width: 380px;
    max-height: 280px;
    border-radius: var(--radius-sm);
    display: block;
    margin-top: 4px;
  }
  /* Inline unicode emoji: a touch larger than text and vertically centered,
     so they read as emoji rather than shrunken glyphs (Discord-style). */
  .body :global(.emoji) {
    font-size: 1.375em;
    line-height: 1;
    vertical-align: -0.25em;
  }
  .body :global(img.cemoji) {
    height: 1.4em;
    width: auto;
    vertical-align: -0.3em;
    margin: 0 1px;
  }
  /* Emoji-only messages render jumbo, like Discord. */
  .body.jumbo :global(.emoji) {
    font-size: 2.6em;
    vertical-align: -0.15em;
  }
  .body.jumbo :global(img.cemoji) {
    height: 2.6em;
  }
  .reaction :global(img.cemoji),
  .reaction .cemoji {
    height: 16px;
    width: auto;
    vertical-align: -3px;
  }
</style>
