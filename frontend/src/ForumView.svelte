<script>
  // Forum channel view: a board of POSTS (threads) instead of a chat feed.
  // Each post is a real channel nested under the forum — same encryption,
  // unread badges, and mutes as everything else — presented as cards sorted
  // by latest activity. Clicking one opens its thread.
  import Icon from "./Icon.svelte";
  import { S, activeGuild, selectChannel } from "./lib/state.svelte.js";

  let { forum } = $props();

  const posts = $derived.by(() => {
    const g = activeGuild();
    if (!g) return [];
    return g.channels
      .filter((c) => c.parent === forum.id)
      .sort((a, b) => (b.lastActivity || 0) - (a.lastActivity || 0));
  });

  function ago(ns) {
    if (!ns) return "no messages yet";
    const ms = ns / 1e6;
    const d = Date.now() - ms;
    if (d < 60e3) return "just now";
    if (d < 3600e3) return `${Math.floor(d / 60e3)}m ago`;
    if (d < 86400e3) return `${Math.floor(d / 3600e3)}h ago`;
    return `${Math.floor(d / 86400e3)}d ago`;
  }
</script>

<div class="forum">
  <div class="forum-head">
    <div class="fh-text">
      <h2><Icon name="forum" size={20} /> {forum.name}</h2>
      <p class="muted">{forum.topic || "Start a post — every post is its own discussion."}</p>
    </div>
    <button class="new-post" onclick={() => (S.modal = { kind: "newPost", forum })}>
      <Icon name="plus" size={14} /> New Post
    </button>
  </div>

  {#if posts.length}
    <div class="posts">
      {#each posts as p, i (p.id)}
        {@const u = S.unread[p.id]}
        <button class="post" style="--i:{Math.min(i, 12)}" onclick={() => selectChannel(p.id)}>
          <span class="post-title">
            {p.name}
            {#if u?.count}<span class="badge">{u.count > 99 ? "99+" : u.count}</span>{/if}
          </span>
          <span class="post-meta muted">
            <Icon name="reply" size={11} />
            {ago(p.lastActivity)}
          </span>
        </button>
      {/each}
    </div>
  {:else}
    <div class="empty">
      <div class="empty-badge"><Icon name="forum" size={28} /></div>
      <h3>No posts yet</h3>
      <p class="muted">Be the first — posts keep each discussion in its own tidy thread.</p>
    </div>
  {/if}
</div>

<style>
  .forum {
    flex: 1;
    overflow-y: auto;
    padding: 18px 20px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .forum-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px;
  }
  .fh-text h2 {
    margin: 0 0 2px;
    font-size: 19px;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .fh-text p {
    margin: 0;
    font-size: 13px;
  }
  .new-post {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: none;
    padding: 8px 16px;
    border-radius: 999px;
    box-shadow: var(--accent-glow);
  }
  .posts {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .post {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    gap: 4px;
    text-align: left;
    padding: 12px 14px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    color: var(--text);
    cursor: pointer;
    transition:
      border-color 0.13s ease,
      background 0.13s ease,
      transform 0.13s ease;
    animation: post-in 0.25s ease both;
    animation-delay: calc(var(--i, 0) * 0.03s);
  }
  @keyframes post-in {
    from {
      opacity: 0;
      transform: translateY(5px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .post {
      animation: none;
    }
  }
  .post:hover {
    background: var(--bg-2);
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
    transform: translateY(-1px);
  }
  .post-title {
    font-weight: 600;
    font-size: 14.5px;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .badge {
    background: var(--accent);
    color: var(--accent-fg);
    font-size: 10.5px;
    font-weight: 700;
    border-radius: 999px;
    padding: 0 6px;
    line-height: 16px;
  }
  .post-meta {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11.5px;
  }
  .empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    flex: 1;
    text-align: center;
  }
  .empty-badge {
    display: grid;
    place-items: center;
    width: 64px;
    height: 64px;
    border-radius: 50%;
    background: var(--accent-soft);
    color: var(--accent-hover);
    margin-bottom: 4px;
  }
  .empty h3 {
    margin: 0;
  }
  .empty p {
    margin: 0;
    font-size: 13px;
  }
  /* Starting a post is the whole point of a forum channel — it can't be a 38px
     target on the surface where you'd use it most. */
  @media (pointer: coarse) {
    .new-post {
      min-height: 44px;
    }
  }
</style>
