<script>
  // The inspector: the people, pins, files and threads of the conversation
  // beside it. One surface with underline tabs; the Members view is the member
  // list (MemberPanel), the others are read off what this device already holds.
  //
  // Docked (there is room beside the conversation) it is a flat column on the
  // sidebar's ground; as an overlay it is a lifted, shadowed panel that starts
  // below the conversation header, so the header and its toggle stay reachable.
  // The shell decides which (it owns the width test) and passes `docked`.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import Tabs from "./Tabs.svelte";
  import MemberPanel from "./MemberPanel.svelte";
  import {
    S,
    activeGuild,
    memberByFpr,
    nameFor,
    fmtClock as fmtTime,
    jumpToMessage,
    jumpToChannel,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PERM, has } from "./lib/perms.js";
  import { plainSnippet } from "./lib/snippet.js";
  import { parseAttachTokens, parseFileTokens } from "./lib/attachments.js";
  import { tooltip } from "./lib/tooltip.js";
  import { insp } from "./lib/inspector.svelte.js";

  let { docked = true, onClose = () => {} } = $props();

  const g = $derived(activeGuild());
  const pinned = $derived(S.messages.filter((m) => m.pinned && !m.deleted));
  const canPin = $derived(!!g?.kind || has(g?.myPerms || 0, PERM.MANAGE_MESSAGES));
  // Files: every attachment in the history this device has loaded, newest
  // first. Images and files alike; the row jumps to the message that carried it.
  const files = $derived.by(() => {
    const out = [];
    for (let i = S.messages.length - 1; i >= 0 && out.length < 200; i--) {
      const m = S.messages[i];
      if (m.deleted || !m.content) continue;
      for (const t of parseAttachTokens(m.content)) out.push({ m, key: t.blobId, name: t.name || "Image", kind: "image" });
      for (const t of parseFileTokens(m.content)) out.push({ m, key: t.blobId, name: t.name || "File", kind: "file" });
    }
    return out;
  });
  // Threads: the channels parented to this one, most recent first.
  const threads = $derived(
    (g?.channels || []).filter((c) => c.parent && c.parent === S.activeChannelId),
  );

  const tabs = $derived([
    { id: "members", label: "Members" },
    { id: "pins", label: "Pins", count: pinned.length || undefined },
    { id: "files", label: "Files" },
    { id: "threads", label: "Threads", count: threads.length || undefined },
  ]);

  function onKey(e) {
    if (e.key === "Escape" && !docked) {
      e.stopPropagation();
      onClose();
    }
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<aside class="insp" class:docked class:overlay={!docked} aria-label="Inspector" onkeydown={onKey}>
  <div class="insp-head">
    <Tabs label="Inspector" value={insp.tab} onChange={(id) => (insp.tab = id)} {tabs} idPrefix="insp" />
    <!-- On a phone the inspector is a pushed page and its Back bar closes it. -->
    {#if !S.isMobile}
    <button type="button" class="insp-x" use:tooltip={"Close · Ctrl+U"} aria-label="Close the inspector" onclick={onClose}>
      <Icon name="close" size={16} />
    </button>
    {/if}
  </div>
  <div class="insp-body" role="tabpanel" id="insp-{insp.tab}" aria-labelledby="insp-tab-{insp.tab}">
    {#if insp.tab === "members"}
      <MemberPanel embedded />
    {:else if insp.tab === "pins"}
      {#each pinned as m (m.id)}
        {@const mem = memberByFpr(m.sender)}
        <div class="row">
          <button type="button" class="row-main" onclick={() => jumpToMessage(m.channelId, m.id)}>
            <Avatar name={nameFor(m.sender, m.senderName)} emoji={mem?.emoji} color={mem?.color} image={mem?.avatar} size={28} />
            <span class="row-text">
              <span class="row-title">{nameFor(m.sender, m.senderName)} <span class="row-meta">{fmtTime(m.sent)}</span></span>
              <span class="row-sub">{plainSnippet(m.content, 160) || "Attachment"}</span>
            </span>
          </button>
          {#if canPin}
            <button type="button" class="row-act" use:tooltip={"Unpin"} aria-label="Unpin message" onclick={() => api.pinMessage(m.channelId, m.id)}>
              <Icon name="close" size={14} />
            </button>
          {/if}
        </div>
      {:else}
        <p class="empty">
          <strong>No pinned messages yet</strong>
          {canPin ? "Pin a message from its ⋯ menu and it shows up here for everyone." : "People who can manage messages can pin one, and it shows up here for everyone."}
        </p>
      {/each}
    {:else if insp.tab === "files"}
      {#each files as f (f.key + f.m.id)}
        <button type="button" class="row row-main" onclick={() => jumpToMessage(f.m.channelId, f.m.id)}>
          <span class="file-ic"><Icon name={f.kind === "image" ? "image" : "attach"} size={16} /></span>
          <span class="row-text">
            <span class="row-title"><bdi>{f.name}</bdi></span>
            <span class="row-sub">{nameFor(f.m.sender, f.m.senderName)} · {fmtTime(f.m.sent)}</span>
          </span>
        </button>
      {:else}
        <p class="empty">
          <strong>No files yet</strong>
          Pictures and files shared here are listed in this tab.
        </p>
      {/each}
    {:else}
      {#each threads as t (t.id)}
        <button type="button" class="row row-main" onclick={() => jumpToChannel(t.id)}>
          <span class="file-ic"><Icon name="forum" size={16} /></span>
          <span class="row-text">
            <span class="row-title"><bdi>{t.name}</bdi></span>
          </span>
        </button>
      {:else}
        <p class="empty">
          <strong>No threads yet</strong>
          Start one from a message's toolbar and it is listed here.
        </p>
      {/each}
    {/if}
  </div>
</aside>

<style>
  .insp {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    height: 100%;
    background: var(--bg-1);
    border-left: 1px solid var(--hairline);
    animation: insp-fade var(--dur-quick) var(--ease-out);
  }
  /* Overlay: lifted and shadowed over the conversation, below its header. */
  .insp.overlay {
    background: var(--bg-elevated);
    border-left: 1px solid var(--border);
    box-shadow: var(--shadow-pop);
    animation: insp-slide var(--dur-standard) var(--ease-out);
  }
  @keyframes insp-fade {
    from {
      opacity: 0;
    }
  }
  @keyframes insp-slide {
    from {
      opacity: 0;
      transform: translateX(12px);
    }
  }
  .insp-head {
    display: flex;
    align-items: flex-end;
    gap: var(--sp-2);
    min-height: var(--header-h);
    padding: 0 8px 0 12px;
    border-bottom: 1px solid var(--border);
    box-sizing: border-box;
  }
  .insp-head :global(.tabs) {
    flex: 1;
    min-width: 0;
    gap: var(--sp-1);
    box-shadow: none;
  }
  .insp-x {
    align-self: center;
    display: grid;
    place-items: center;
    width: 32px;
    height: 32px;
    flex: none;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
  }
  .insp-x:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .insp-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
  }
  .insp-body > :global(.panel) {
    flex: 1;
    min-height: 0;
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin: 0 8px;
    border-radius: var(--radius-sm);
  }
  .insp-body > .row:first-child,
  .insp-body > .row-main:first-child {
    margin-top: var(--sp-3);
  }
  .row-main {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 8px;
    border-radius: var(--radius-sm);
    text-align: start;
    color: var(--text);
  }
  .row-main:hover,
  .row:hover {
    background: color-mix(in srgb, var(--bg-3) 60%, transparent);
  }
  .row-text {
    min-width: 0;
    display: flex;
    flex-direction: column;
  }
  .row-title {
    font-size: var(--fs-ui);
    font-weight: 500;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row-meta,
  .row-sub {
    font-size: var(--fs-small);
    color: var(--text-muted);
    font-weight: 400;
  }
  .row-sub {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row-meta {
    color: var(--text-faint);
    font-variant-numeric: tabular-nums;
  }
  .row-act {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    flex: none;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
  }
  .row-act:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .file-ic {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    flex: none;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .empty {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    margin: 16px 20px;
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .empty strong {
    font-family: var(--display-font);
    font-size: var(--fs-title);
    font-weight: 600;
    color: var(--text);
  }
  @media (pointer: coarse) {
    .row-main {
      min-height: 44px;
    }
  }
</style>
