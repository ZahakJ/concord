<script>
  // Renders a poll message: the question and each option as a click-to-vote bar.
  // Votes ARE reactions (option i ⇒ POLL_EMOJI[i]) so this is just a nicer view
  // over m.reactions; toggling calls the same react() everything else uses.
  import { S, react } from "./lib/state.svelte.js";
  import { POLL_EMOJI } from "./lib/polls.js";

  let { m, poll } = $props();

  const me = $derived(S.identity.fingerprint);

  const rows = $derived(
    poll.opts.map((text, i) => {
      const voters = m.reactions?.[POLL_EMOJI[i]] || [];
      return { text, i, emoji: POLL_EMOJI[i], count: voters.length, mine: voters.includes(me) };
    }),
  );
  const total = $derived(rows.reduce((n, r) => n + r.count, 0));
  const leader = $derived(Math.max(0, ...rows.map((r) => r.count)));

  function vote(r) {
    // Single-choice: picking a new option clears your other picks.
    if (!poll.multi && !r.mine) {
      for (const other of rows) {
        if (other.i !== r.i && other.mine) react(m, other.emoji);
      }
    }
    react(m, r.emoji);
  }
</script>

<div class="poll">
  <div class="poll-q">{poll.q}</div>
  <div class="poll-opts">
    {#each rows as r (r.i)}
      <button class="opt" class:mine={r.mine} class:lead={r.count > 0 && r.count === leader} onclick={() => vote(r)}>
        <span class="fill" style="width:{total ? (r.count / total) * 100 : 0}%"></span>
        <span class="opt-body">
          <span class="opt-emoji">{r.emoji}</span>
          <span class="opt-text">{r.text}</span>
          <span class="opt-count">{r.count}</span>
        </span>
      </button>
    {/each}
  </div>
  <div class="poll-foot muted">
    {total} vote{total === 1 ? "" : "s"}{poll.multi ? " · pick as many as you like" : ""}
  </div>
</div>

<style>
  .poll {
    margin-top: 4px;
    max-width: 420px;
    padding: 12px 14px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .poll-q {
    font-weight: 700;
    font-size: 14px;
    margin-bottom: 10px;
  }
  .poll-opts {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .opt {
    position: relative;
    display: block;
    width: 100%;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--bg-2);
    overflow: hidden;
    text-align: left;
    transition:
      border-color 0.12s ease,
      transform 0.08s ease;
  }
  .opt:hover {
    border-color: var(--accent);
  }
  .opt:active {
    transform: scale(0.99);
  }
  .opt.mine {
    border-color: var(--accent);
  }
  .fill {
    position: absolute;
    inset: 0 auto 0 0;
    background: var(--accent-soft);
    transition: width 0.35s cubic-bezier(0.4, 0, 0.2, 1);
  }
  .opt.mine .fill {
    background: color-mix(in srgb, var(--accent) 26%, transparent);
  }
  .opt-body {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 9px 11px;
  }
  .opt-emoji {
    flex-shrink: 0;
    font-size: 15px;
  }
  .opt-text {
    flex: 1;
    min-width: 0;
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .opt.mine .opt-text {
    font-weight: 600;
  }
  .opt-count {
    flex-shrink: 0;
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
  }
  .poll-foot {
    margin-top: 8px;
    font-size: 11px;
  }
  @media (prefers-reduced-motion: reduce) {
    .fill {
      transition: none;
    }
    .opt:active {
      transform: none;
    }
  }
</style>
