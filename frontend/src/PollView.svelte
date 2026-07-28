<script>
  // Renders a poll message: the question, and each option as a click-to-vote
  // row. Votes ARE reactions (option i ⇒ POLL_EMOJI[i]) so this is a nicer view
  // over m.reactions; toggling calls the same react() everything else uses.
  //
  // The thing that makes a poll feel alive is seeing WHO voted, not just how
  // many — the fingerprints are already in m.reactions, so each row shows the
  // faces behind its number.
  import Avatar from "./Avatar.svelte";
  import { S, react, memberByFpr, nameFor } from "./lib/state.svelte.js";
  import { POLL_EMOJI } from "./lib/polls.js";

  // `preview` renders the poll as it will look without letting anyone vote —
  // used by the composer so you can see what you're about to post.
  let { m, poll, preview = false } = $props();

  const me = $derived(S.identity.fingerprint);
  const FACES = 4; // how many voters to show before "+N"

  const rows = $derived(
    poll.opts.map((text, i) => {
      const voters = m.reactions?.[POLL_EMOJI[i]] || [];
      return {
        text,
        i,
        emoji: POLL_EMOJI[i],
        voters,
        count: voters.length,
        mine: voters.includes(me),
      };
    }),
  );
  const total = $derived(rows.reduce((n, r) => n + r.count, 0));
  const leader = $derived(Math.max(0, ...rows.map((r) => r.count)));
  const voted = $derived(rows.some((r) => r.mine));
  const pct = (c) => (total ? Math.round((c / total) * 100) : 0);

  const who = (fpr) => {
    const mem = memberByFpr(fpr);
    return { name: nameFor(fpr), emoji: mem?.emoji || "", color: mem?.color || "", image: mem?.avatar || "" };
  };

  function vote(r) {
    if (preview) return;
    // Single-choice: picking a new option clears your other picks.
    if (!poll.multi && !r.mine) {
      for (const other of rows) {
        if (other.i !== r.i && other.mine) react(m, other.emoji);
      }
    }
    react(m, r.emoji);
  }
</script>

<div class="poll" class:preview>
  <div class="poll-head">
    <div class="poll-q">{poll.q}</div>
    <span class="poll-kind">{poll.multi ? "Pick as many as you like" : "Pick one"}</span>
  </div>

  <div class="poll-opts">
    {#each rows as r (r.i)}
      <button
        class="opt"
        class:mine={r.mine}
        class:lead={r.count > 0 && r.count === leader}
        onclick={() => vote(r)}
        aria-pressed={r.mine}
        disabled={preview}
        title={r.count ? r.voters.map(who).map((w) => w.name).join(", ") : "No votes yet"}
      >
        <span class="fill" style="width:{pct(r.count)}%"></span>
        <span class="opt-body">
          <!-- The emoji is the option's identity everywhere else (it's the
               underlying reaction), so it stays — as a marker that fills in
               when it's yours, rather than a loose glyph. -->
          <span class="mark" class:on={r.mine}>
            <span class="mark-emoji">{r.emoji}</span>
            {#if r.mine}<span class="mark-check" aria-hidden="true">✓</span>{/if}
          </span>
          <span class="opt-text">{r.text}</span>
          {#if r.count}
            <span class="faces">
              {#each r.voters.slice(0, FACES) as fpr (fpr)}
                {@const w = who(fpr)}
                <span class="face" title={w.name}>
                  <Avatar name={w.name} emoji={w.emoji} color={w.color} image={w.image} size={19} />
                </span>
              {/each}
              {#if r.count > FACES}<span class="more">+{r.count - FACES}</span>{/if}
            </span>
          {/if}
          <span class="opt-pct" class:dim={!r.count}>{pct(r.count)}%</span>
        </span>
      </button>
    {/each}
  </div>

  <div class="poll-foot">
    <span class="tally">{total || "No"} vote{total === 1 ? "" : "s"}</span>
    {#if !preview}
      <span class="state" class:done={voted}>{voted ? "You voted" : "Tap an option to vote"}</span>
    {/if}
  </div>
</div>

<style>
  .poll {
    margin-top: 4px;
    max-width: 460px;
    padding: 14px 15px 11px;
    background: linear-gradient(180deg, color-mix(in srgb, var(--accent) 5%, var(--bg-1)), var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .poll-head {
    display: flex;
    flex-direction: column;
    gap: 3px;
    margin-bottom: 12px;
  }
  .poll-q {
    font-weight: 700;
    font-size: 15.5px;
    line-height: 1.3;
  }
  /* The rules of the poll, stated once, quietly — not repeated on every row. */
  .poll-kind {
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-muted);
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
    /* An option is a button with its own surface, so it needs its own ink: the
       global button rule hands out --accent-fg, which is sized for an accent
       FILL and disappears on --bg-2. */
    color: var(--text);
    overflow: hidden;
    text-align: left;
    transition:
      border-color 0.14s ease,
      transform 0.08s ease;
  }
  .opt:hover:not(:disabled) {
    border-color: var(--accent);
  }
  .opt:active:not(:disabled) {
    transform: scale(0.995);
  }
  .opt.mine {
    border-color: var(--accent);
  }
  .opt:disabled {
    cursor: default;
  }
  .fill {
    position: absolute;
    inset: 0 auto 0 0;
    background: color-mix(in srgb, var(--accent) 13%, transparent);
    transition: width 0.5s cubic-bezier(0.4, 0, 0.2, 1);
  }
  .opt.mine .fill {
    background: color-mix(in srgb, var(--accent) 26%, transparent);
  }
  /* The front-runner gets a bright left edge — a rank you can see at a glance
     without another number to read. */
  .opt.lead::before {
    content: "";
    position: absolute;
    inset: 0 auto 0 0;
    width: 3px;
    background: var(--accent);
  }
  .opt-body {
    position: relative;
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 9px 12px;
  }
  .mark {
    position: relative;
    display: grid;
    place-items: center;
    flex-shrink: 0;
    width: 25px;
    height: 25px;
    border-radius: 50%;
    background: var(--bg-3);
    box-shadow: inset 0 0 0 1px var(--border);
    transition:
      background 0.14s ease,
      box-shadow 0.14s ease;
  }
  .mark.on {
    background: var(--accent-soft);
    box-shadow: inset 0 0 0 1.5px var(--accent);
  }
  .mark-emoji {
    font-size: 13px;
    line-height: 1;
  }
  .mark-check {
    position: absolute;
    right: -3px;
    bottom: -3px;
    display: grid;
    place-items: center;
    width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--accent);
    color: var(--accent-fg);
    font-size: 8px;
    font-weight: 900;
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
  /* Who voted, not just how many. Overlapped so a long list stays compact. */
  .faces {
    display: flex;
    flex-shrink: 0;
    align-items: center;
    padding-left: 3px;
  }
  .face {
    display: block;
    margin-left: -6px;
    border-radius: 50%;
    box-shadow: 0 0 0 2px var(--bg-2);
  }
  .opt.mine .face {
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 26%, var(--bg-2));
  }
  .more {
    margin-left: 3px;
    font-size: 10.5px;
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
  }
  .opt-pct {
    flex-shrink: 0;
    min-width: 2.6em;
    text-align: right;
    font-size: 12px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: var(--text);
  }
  .opt-pct.dim {
    color: var(--text-muted);
    font-weight: 500;
  }
  .poll-foot {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 10px;
    margin-top: 10px;
    font-size: 11.5px;
    color: var(--text-muted);
  }
  .tally {
    font-variant-numeric: tabular-nums;
  }
  .state.done {
    color: var(--accent-hover);
    font-weight: 600;
  }
  @media (prefers-reduced-motion: reduce) {
    .fill,
    .mark {
      transition: none;
    }
    .opt:active:not(:disabled) {
      transform: none;
    }
  }
</style>
