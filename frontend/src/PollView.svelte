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
  const pct = (c) => (total ? Math.round((c / total) * 100) : 0);

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
  <div class="poll-head">
    <span class="poll-badge">📊 Poll</span>
    <div class="poll-q">{poll.q}</div>
  </div>
  <div class="poll-opts">
    {#each rows as r (r.i)}
      <button
        class="opt"
        class:mine={r.mine}
        class:lead={r.count > 0 && r.count === leader}
        onclick={() => vote(r)}
        aria-pressed={r.mine}
      >
        <span class="fill" style="width:{pct(r.count)}%"></span>
        <span class="opt-body">
          <span class="opt-emoji">{r.emoji}</span>
          <span class="opt-text">{r.text}</span>
          {#if r.mine}<span class="opt-check" aria-hidden="true">✓</span>{/if}
          <span class="opt-pct">{pct(r.count)}%</span>
          <span class="opt-count">{r.count}</span>
        </span>
      </button>
    {/each}
  </div>
  <div class="poll-foot muted">
    {total} vote{total === 1 ? "" : "s"}{poll.multi ? " · pick as many as you like" : " · pick one"}
  </div>
</div>

<style>
  .poll {
    margin-top: 4px;
    max-width: 440px;
    padding: 13px 15px 12px;
    background: linear-gradient(180deg, color-mix(in srgb, var(--accent) 5%, var(--bg-1)), var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .poll-head {
    margin-bottom: 12px;
  }
  .poll-badge {
    display: inline-block;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--accent);
    background: var(--accent-soft);
    padding: 2px 8px;
    border-radius: 999px;
    margin-bottom: 7px;
  }
  .poll-q {
    font-weight: 700;
    font-size: 15px;
    line-height: 1.3;
  }
  .poll-opts {
    display: flex;
    flex-direction: column;
    gap: 7px;
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
      transform 0.08s ease,
      box-shadow 0.12s ease;
  }
  .opt:hover {
    border-color: var(--accent);
  }
  .opt:active {
    transform: scale(0.99);
  }
  .opt.mine {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }
  .fill {
    position: absolute;
    inset: 0 auto 0 0;
    background: linear-gradient(90deg, var(--accent-soft), color-mix(in srgb, var(--accent) 18%, transparent));
    transition: width 0.45s cubic-bezier(0.4, 0, 0.2, 1);
  }
  .opt.mine .fill {
    background: linear-gradient(90deg, color-mix(in srgb, var(--accent) 30%, transparent), color-mix(in srgb, var(--accent) 20%, transparent));
  }
  .opt.lead .fill {
    background: linear-gradient(90deg, color-mix(in srgb, var(--accent) 34%, transparent), color-mix(in srgb, var(--accent) 22%, transparent));
  }
  .opt-body {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
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
  .opt.mine .opt-text,
  .opt.lead .opt-text {
    font-weight: 600;
  }
  .opt-check {
    flex-shrink: 0;
    font-size: 11px;
    font-weight: 800;
    color: var(--accent);
  }
  .opt-pct {
    flex-shrink: 0;
    font-size: 12px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: var(--text);
  }
  .opt-count {
    flex-shrink: 0;
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
    min-width: 1.2em;
    text-align: right;
  }
  .poll-foot {
    margin-top: 10px;
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
