<script>
  // The post, inside the post.
  //
  // Opening one used to give you a plain chat channel: the tags chosen when it
  // was created were nowhere on screen, the answered and pinned state were
  // nowhere, the title existed only as truncated header text, and there was no
  // way to mark it answered, pin it, close it or re-tag it without going back
  // to the board and right-clicking the card — at exactly the moment you were
  // reading the answer.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import {
    S,
    activeGuild,
    selectChannel,
    memberByFpr,
    nameFor,
    openContextMenu,
    flash,
    refreshGuilds,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { resolveTags, relTime } from "./lib/forum.js";
  import {
    canModeratePosts,
    mayCuratePost,
    setPostPinned,
    setPostSolved,
    setPostLocked,
    confirmDeletePost,
  } from "./lib/postactions.svelte.js";

  let { channel } = $props();

  const g = $derived(activeGuild());
  const parent = $derived((g?.channels || []).find((c) => c.id === channel?.parent) || null);
  const isForumPost = $derived(parent?.type === "forum");
  const palette = $derived(parent?.forumTags || []);
  const tags = $derived(resolveTags(channel?.tags || [], palette));
  const canMod = $derived(canModeratePosts(g));
  // The board is the authority on WHO opened a post (it reads the opening
  // message); the channel record is not. The header asks the board once.
  let opener = $state(null);
  $effect(() => {
    const gid = g?.id;
    const pid = channel?.id;
    const board = channel?.parent;
    if (!gid || !pid || !board || !isForumPost) return;
    let live = true;
    api
      .forumBoard(gid, board)
      .then((b) => {
        if (!live) return;
        opener = (b?.posts || []).find((p) => p.id === pid) || null;
      })
      .catch(() => {});
    return () => (live = false);
  });
  const curate = $derived(mayCuratePost(g, opener || { id: channel?.id }));
  const author = $derived(opener?.authorFingerprint ? memberByFpr(opener.authorFingerprint) : null);

  const after = async () => {
    await refreshGuilds();
  };

  function menu(e) {
    const post = { ...(opener || {}), id: channel.id, title: channel.name };
    openContextMenu(
      e,
      [
        canMod && {
          label: channel.pinned ? "Unpin from board" : "Pin to board",
          icon: "pin",
          onClick: () => setPostPinned(g, post, !channel.pinned, after),
        },
        curate && {
          label: channel.solved ? "Mark unanswered" : "Mark answered",
          icon: "check",
          onClick: () => setPostSolved(g, post, !channel.solved, after),
        },
        curate && palette.length > 0 && { label: "Edit tags…", icon: "spark", onClick: openTags },
        canMod && { sep: true },
        canMod && {
          label: channel.locked ? "Reopen post" : "Close post",
          icon: "lock",
          onClick: () => setPostLocked(g, post, !channel.locked, after),
        },
        curate && { sep: true },
        curate && {
          label: "Delete post",
          icon: "trash",
          danger: true,
          onClick: () =>
            confirmDeletePost(g, post, async () => {
              await refreshGuilds();
              selectChannel(channel.parent);
            }),
        },
      ].filter(Boolean),
      { title: channel.name || "Post" },
    );
  }

  // Tags edit IN PLACE: a popover of the forum's palette with the post's own
  // chips ticked. Not a dialog — choosing two chips is not a task that
  // deserves one, and the post you are tagging should stay on screen.
  let picking = $state(null); // string[] | null
  let busy = $state(false);
  function openTags() {
    picking = [...(channel.tags || [])];
  }
  function toggleTag(id) {
    picking = picking.includes(id) ? picking.filter((x) => x !== id) : [...picking, id];
  }
  async function saveTags() {
    if (busy) return;
    busy = true;
    try {
      await api.setPostTags(g.id, channel.id, picking);
      await refreshGuilds();
      picking = null;
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }
</script>

<header class="op">
  <div class="crumbs">
    <button class="back" onclick={() => selectChannel(channel.parent)}>
      <Icon name={isForumPost ? "forum" : "hash"} size={13} />
      {parent?.name || "back"}
    </button>
    <span class="sep">›</span>
    <span class="here">{isForumPost ? "Post" : "Thread"}</span>
    <span class="spring"></span>
    <button class="dots" aria-label="Post actions" onclick={menu}><Icon name="dots" size={16} /></button>
  </div>

  <h2 class="title">{channel.name}</h2>

  <div class="line">
    {#if opener?.authorFingerprint || opener?.authorName}
      <span class="who">
        <Avatar
          name={opener.authorName || nameFor(opener.authorFingerprint)}
          image={author?.avatar}
          emoji={author?.emoji}
          color={author?.color}
          size={20}
        />
        <span>{opener.authorName || nameFor(opener.authorFingerprint)}</span>
      </span>
    {/if}
    {#if opener?.created}
      <span class="dotsep">·</span>
      <span>{relTime(opener.created)}</span>
    {/if}
    {#if opener?.replies}
      <span class="dotsep">·</span>
      <span>{opener.replies} {opener.replies === 1 ? "reply" : "replies"}</span>
    {/if}
    {#if channel.solved}
      <span class="chip ok"><Icon name="check" size={11} /> Answered</span>
    {/if}
    {#if channel.pinned}
      <span class="chip"><Icon name="pin" size={11} /> Pinned</span>
    {/if}
    {#if channel.locked}
      <span class="chip closed"><Icon name="lock" size={11} /> Closed</span>
    {/if}
  </div>

  {#if tags.length || (curate && palette.length)}
    <div class="tags">
      {#each tags as t (t.id)}
        <span class="tag" style="--tc:{t.color}">{t.emoji ? `${t.emoji} ` : ""}{t.name}</span>
      {/each}
      {#if curate && palette.length}
        <button class="tag edit" onclick={openTags}>
          <Icon name="spark" size={11} />
          {tags.length ? "Edit tags" : "Add tags"}
        </button>
      {/if}
    </div>
  {/if}

  {#if picking}
    <div class="picker" role="group" aria-label="Post tags">
      {#each palette as t (t.id)}
        <button
          class="tag pick"
          class:on={picking.includes(t.id)}
          style="--tc:{t.color}"
          aria-pressed={picking.includes(t.id)}
          onclick={() => toggleTag(t.id)}
        >
          {#if picking.includes(t.id)}<Icon name="check" size={10} />{/if}
          {t.emoji ? `${t.emoji} ` : ""}{t.name}
        </button>
      {/each}
      <span class="spring"></span>
      <button class="ghost mini" onclick={() => (picking = null)}>Cancel</button>
      <button class="mini" disabled={busy} onclick={saveTags}>Save tags</button>
    </div>
  {/if}
</header>

<style>
  .op {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: var(--sp-3) var(--sp-4) 10px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-1);
  }
  .crumbs {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  .back {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 2px 7px;
    background: var(--bg-3);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: var(--fs-small);
  }
  .back:hover {
    color: var(--text);
    background: var(--bg-2);
  }
  .spring {
    flex: 1;
  }
  .dots {
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    background: transparent;
    color: var(--text-faint);
    border-radius: var(--radius-sm);
  }
  .dots:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .title {
    margin: 0;
    font-size: var(--fs-title);
    line-height: 1.25;
    /* The board's own title wraps; so does this one. Truncating the thing the
       whole panel is about was the header's original sin. */
    overflow-wrap: anywhere;
  }
  .line {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .who {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .dotsep {
    color: var(--text-faint);
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: var(--fs-small);
    font-weight: 600;
  }
  .chip.ok {
    background: color-mix(in srgb, var(--ok) 20%, transparent);
    color: var(--ok);
  }
  .chip.closed {
    background: color-mix(in srgb, var(--warn) 20%, transparent);
    color: var(--warn-text);
  }
  .tags,
  .picker {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 5px;
  }
  .tag {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    padding: 2px 8px;
    border-radius: 999px;
    font-size: var(--fs-small);
    color: var(--tc, var(--text-muted));
    border: 1px solid color-mix(in srgb, var(--tc, var(--border)) 55%, transparent);
    background: color-mix(in srgb, var(--tc, var(--bg-3)) 14%, transparent);
  }
  .tag.edit {
    color: var(--text-faint);
    border-style: dashed;
    border-color: var(--border);
    background: transparent;
  }
  .tag.edit:hover {
    color: var(--text);
    border-color: var(--accent);
  }
  .tag.pick {
    opacity: 0.55;
  }
  .tag.pick.on {
    opacity: 1;
    font-weight: 600;
  }
  .picker {
    padding-top: var(--sp-1);
    border-top: 1px dashed var(--border);
  }
  .mini {
    padding: 4px 10px;
    font-size: var(--fs-small);
  }
  @media (max-width: 768px) {
    .op {
      padding: 10px var(--sp-3) 8px;
    }
  }
</style>
