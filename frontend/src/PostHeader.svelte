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
    closePost,
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
    <!-- Folds, rather than navigating: this is the same dismissal the board
         card and Escape perform, so the panel leaves the way it arrived. -->
    <button class="back" onclick={closePost} title="Close this post">
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
        <span class="tag" style="--tc:{t.color}">{t.name}</span>
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
          {t.name}
        </button>
      {/each}
      <span class="spring"></span>
      <button class="ghost mini" onclick={() => (picking = null)}>Cancel</button>
      <button class="primary mini" disabled={busy} onclick={saveTags}>Save tags</button>
    </div>
  {/if}
</header>

<style>
  .op {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: var(--sp-3) var(--sp-4) 10px;
    border-bottom: 1px solid var(--hairline);
    background: var(--bg-1);
  }
  .crumbs {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  /* A link back to the board, not a chip. */
  .back {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 2px 4px;
    margin-left: -4px;
    background: transparent;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    font-size: var(--fs-small);
  }
  .back:hover {
    color: var(--text);
    background: var(--bg-3);
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
    font-weight: 600;
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
  /* States are words on the meta line, the way the board's rows say them. */
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    color: var(--text-faint);
    font-size: var(--fs-small);
    font-weight: 500;
  }
  .chip.ok {
    color: var(--ok-text);
  }
  .tags,
  .picker {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 5px;
  }
  /* The board's quiet tag: a grey chip in muted ink, the tag's colour kept
     as a 6px dot. */
  .tag {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 22px;
    padding: 0 8px;
    border-radius: 999px;
    font-size: var(--fs-small);
    color: var(--text-muted);
    background: var(--bg-3);
  }
  .tag:not(.edit)::before {
    content: "";
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--tc, var(--text-faint));
  }
  .tag.edit {
    color: var(--text-muted);
    background: transparent;
  }
  .tag.edit:hover {
    color: var(--text);
    background: var(--bg-3);
  }
  .tag.pick.on {
    background: var(--accent-soft);
    color: var(--accent-hover);
    font-weight: 600;
  }
  .picker {
    padding-top: var(--sp-2);
    border-top: 1px solid var(--hairline);
  }
  .mini {
    padding: 4px 10px;
    font-size: var(--fs-small);
  }
  @media (max-width: 768px) {
    .op {
      padding: 10px var(--sp-3) 8px;
    }
    /* The post's only menu, at the screen's edge: a 28px square there was
       a thumb's width short of a target. */
    .dots {
      width: var(--tap-min);
      height: var(--tap-min);
    }
  }
</style>
