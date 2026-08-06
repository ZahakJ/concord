<script>
  // Renders a poll message: the question, and each option as a click-to-vote
  // row. Votes ARE reactions (option i ⇒ POLL_EMOJI[i]) so this is a nicer view
  // over m.reactions; toggling calls the same react() everything else uses.
  //
  // The thing that makes a poll feel alive is seeing WHO voted, not just how
  // many — the fingerprints are already in m.reactions, so each row shows the
  // faces behind its number.
  import Avatar from "./Avatar.svelte";
  import { S, react, memberByFpr, nameFor, openContextMenu } from "./lib/state.svelte.js";
  import { haptic } from "./lib/touch.js";
  import { POLL_EMOJI } from "./lib/polls.js";
  import { radialBurst } from "./lib/burst.js";

  // `preview` renders the poll as it will look without letting anyone vote —
  // used by the composer so you can see what you're about to post.
  let { m, poll, preview = false } = $props();

  const me = $derived(S.identity.fingerprint);
  // How many voters to show before "+N". One fewer on a phone: four overlapping
  // 19px faces are ~61px of a ~300px card, taken straight out of the option
  // label — and the label is the thing you are voting on.
  const FACES = $derived(S.isMobile ? 3 : 4);

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

  // ---- closing time ----
  // `until` runs on each client's OWN clock — the same local-clock honesty as
  // ephemeral expiry: the author sealed an absolute time into the content, and
  // every reader compares it to their own Date.now(). No coordination, no
  // authority; a skewed clock freezes a little early or late and that's fine.
  let now = $state(Date.now() / 1000);
  $effect(() => {
    if (!poll.until || Date.now() / 1000 >= poll.until) return;
    let t;
    const tick = () => {
      now = Date.now() / 1000;
      if (now < poll.until) schedule();
    };
    const schedule = () => {
      clearTimeout(t);
      // Land exactly on the boundary when it's near; otherwise a lazy 30s beat
      // keeps the "closes in" line honest — it's minute-grained, not a stopwatch.
      t = setTimeout(tick, Math.min(Math.max(poll.until * 1000 - Date.now(), 250), 30000));
    };
    schedule();
    return () => clearTimeout(t);
  });
  const closed = $derived(!!poll.until && now >= poll.until);
  // Forward-looking twin of timestamp.js's sealAgo, same coarse grain — a
  // deadline you glance at, not a countdown you watch.
  function closesIn(until, nowS) {
    const s = Math.max(0, Math.round(until - nowS));
    if (s < 60) return "under a minute";
    const mins = Math.round(s / 60);
    if (mins < 60) return `${mins}m`;
    const h = Math.round(mins / 60);
    if (h < 24) return `${h}h`;
    return `${Math.round(h / 24)}d`;
  }

  // The crown: once closed, the single front-runner wears it. A tie crowns
  // nobody — two winners is no winner, and half a trophy helps no one.
  const winner = $derived.by(() => {
    if (!closed || !total) return -1;
    const top = rows.filter((r) => r.count === leader);
    return top.length === 1 ? top[0].i : -1;
  });

  // ---- the flip to CLOSED ----
  // One trophy burst out of the crown, only for the transition you actually
  // watched: prevClosed starts null so a poll that MOUNTS already-closed
  // (scrolling back through history) renders frozen and stays quiet — the
  // burst is for "it just closed", never "it already was". Same edge-tracker
  // as EventCard's live flip; effects run post-render, so by the time the
  // flip is observed the crown is in the DOM to measure.
  let crownEl = $state(null);
  let prevClosed = null;
  $effect(() => {
    const isClosed = closed;
    if (prevClosed === false && isClosed && winner >= 0 && !preview) {
      const r = crownEl?.getBoundingClientRect();
      if (r)
        radialBurst(r.left + r.width / 2, r.top + r.height / 2, {
          glyphs: ["🏆"],
          n: 6,
          dist: [18, 46],
          size: [11, 15],
          dur: [0.5, 0.8],
          seed: `poll-close-${m.id}`,
        });
    }
    prevClosed = isClosed;
  });

  // ---- trivia ----
  // The answer key rides in the token from the start, but stays face-down
  // until YOU have committed (voted) or the poll has: knowing the answer
  // before you pick would make the quiz a formality.
  const hasAnswer = $derived(poll.answer != null);
  const revealed = $derived(hasAnswer && (voted || closed) && !preview);
  const mineRight = $derived(hasAnswer && !!rows[poll.answer]?.mine);

  const who = (fpr) => {
    const mem = memberByFpr(fpr);
    return { name: nameFor(fpr), emoji: mem?.emoji || "", color: mem?.color || "", image: mem?.avatar || "" };
  };

  function vote(r, e) {
    if (preview || closed) return;
    // On a single-choice poll a mis-tap doesn't just add a vote, it MOVES one —
    // silently, since the row you left simply stops being highlighted. The tick
    // of vibration is the confirmation that something changed at all.
    haptic("light");
    // Single-choice: picking a new option clears your other picks.
    if (!poll.multi && !r.mine) {
      for (const other of rows) {
        if (other.i !== r.i && other.mine) react(m, other.emoji);
      }
    }
    // A vote LANDING gets a tick-sized burst from the row's marker; retracting
    // is a correction, not a moment, so it gets nothing. On a single-choice
    // move the burst fires only where the vote arrives, which is the half of
    // the silent move worth pointing at. Keyboard "clicks" report (0,0), so the
    // origin comes from the marker's rect, never the pointer.
    if (!r.mine) {
      const mark = e?.currentTarget?.querySelector?.(".mark");
      const box = (mark || e?.currentTarget)?.getBoundingClientRect?.();
      if (box) {
        radialBurst(box.left + box.width / 2, box.top + box.height / 2, {
          glyphs: ["✓"],
          colors: ["var(--accent)"],
          n: 5,
          dist: [14, 32],
          size: [8, 11],
          dur: [0.4, 0.65],
        });
      }
    }
    react(m, r.emoji);
  }

  // Seeing WHO voted is the point of this component, and past the first few
  // faces that was a `title` tooltip — which touch never renders, so a poll with
  // twelve voters showed "+8" and no gesture could ever reveal them. The tally
  // opens the full list, grouped by option, in the same sheet the message
  // long-press uses.
  function showVoters(e) {
    const items = [];
    for (const r of rows) {
      if (!r.count) continue;
      items.push({ header: true, label: `${r.emoji} ${r.text} · ${r.count}` });
      for (const f of r.voters) items.push({ label: nameFor(f), onClick: () => {} });
    }
    openContextMenu(e, items, { title: poll.q });
  }
</script>

<div class="poll" class:preview>
  <div class="poll-head">
    <div class="poll-q">{poll.q}</div>
    <span class="poll-kind">{hasAnswer ? "Quiz · " : ""}{poll.multi ? "Pick as many as you like" : "Pick one"}</span>
    {#if poll.until}
      <!-- The clock line: quiet while it counts, a flat verdict once it's over. -->
      <span class="poll-close" class:over={closed}>{closed ? "Closed" : `Closes in ${closesIn(poll.until, now)}`}</span>
    {/if}
  </div>

  <div class="poll-opts">
    {#each rows as r (r.i)}
      <button
        class="opt"
        class:mine={r.mine}
        class:lead={r.count > 0 && r.count === leader}
        class:correct={revealed && r.i === poll.answer}
        class:wrong={revealed && r.mine && r.i !== poll.answer}
        onclick={(e) => vote(r, e)}
        aria-pressed={r.mine}
        disabled={preview || closed}
        title={r.count ? r.voters.map(who).map((w) => w.name).join(", ") : "No votes yet"}
      >
        <span class="fill" style="width:{pct(r.count)}%"></span>
        <span class="opt-body">
          <!-- The emoji is the option's identity everywhere else (it's the
               underlying reaction), so it stays — as a marker that fills in
               when it's yours, rather than a loose glyph. Once a quiz is
               revealed the badge grades instead of confirms: the correct row
               wears ✓ in --ok whether or not it's yours, and your miss wears
               ✕ in danger ink. -->
          <span class="mark" class:on={r.mine} class:ok-on={revealed && r.i === poll.answer}>
            <span class="mark-emoji">{r.emoji}</span>
            {#if revealed && r.i === poll.answer}
              <span class="mark-check ok" aria-hidden="true">✓</span>
            {:else if revealed && r.mine}
              <span class="mark-check bad" aria-hidden="true">✕</span>
            {:else if r.mine}
              <span class="mark-check" aria-hidden="true">✓</span>
            {/if}
          </span>
          {#if r.i === winner}<span class="crown" bind:this={crownEl} title="Winner">🏆</span>{/if}
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
    {#if total && !preview}
      <button class="tally" onclick={showVoters}>{total} vote{total === 1 ? "" : "s"} ›</button>
    {:else}
      <span class="tally flat">{total || "No"} vote{total === 1 ? "" : "s"}</span>
    {/if}
    {#if !preview}
      <span class="state" class:done={voted} class:right={revealed && voted && mineRight} class:miss={revealed && voted && !mineRight}>
        {#if revealed && voted}{mineRight ? "You got it" : "Not this time"}
        {:else if closed}Poll closed
        {:else}{voted ? "You voted" : "Tap an option to vote"}{/if}
      </span>
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
    font-size: var(--fs-body);
    line-height: 1.3;
  }
  /* The rules of the poll, stated once, quietly — not repeated on every row. */
  .poll-kind {
    font-size: var(--fs-small);
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  /* Deadline, stated once and quietly — the countdown is minute-grained so it
     never demands attention the way a ticking stopwatch would. */
  .poll-close {
    font-size: var(--fs-small);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  .poll-close.over {
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-faint);
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
  /* Pointer-only: Chromium latches :hover after a tap, so an option you merely
     brushed past stayed accent-ringed and read as "selected" — on the one
     control in the app where selection is the actual state being displayed. */
  @media (pointer: fine) {
    .opt:hover:not(:disabled) {
      border-color: var(--accent);
    }
  }
  .opt:active:not(:disabled) {
    transform: scale(0.995);
  }
  .opt.mine {
    border-color: var(--accent);
  }
  /* Trivia reveal: the verdict speaks in a different COLOR channel from the
     accent that means "yours" — --ok for the right row, danger for your miss —
     so right-and-mine reads as both at a glance. Declared after .mine so the
     verdict's border wins once revealed. */
  .opt.correct {
    border-color: var(--ok);
  }
  .opt.correct .fill {
    background: color-mix(in srgb, var(--ok) 16%, transparent);
  }
  .opt.correct .opt-text {
    font-weight: 600;
  }
  .opt.wrong {
    border-color: var(--danger);
  }
  .opt.wrong .fill {
    background: color-mix(in srgb, var(--danger) 12%, transparent);
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
  .mark.ok-on {
    background: var(--ok-soft);
    box-shadow: inset 0 0 0 1.5px var(--ok);
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
  .mark-check.ok {
    background: var(--ok);
    color: var(--ok-fg, #fff);
  }
  .mark-check.bad {
    background: var(--danger);
    color: #fff;
  }
  /* The trophy sits in the row, not on it — a small fact, not a banner. */
  .crown {
    flex-shrink: 0;
    font-size: 14px;
    line-height: 1;
  }
  /* Wraps rather than truncates. Between the marker, the faces cluster and the
     percentage there were about 143px left in a phone-width card — roughly 20
     characters — so options read "Should we move the mee…" and you voted on an
     ellipsis. A taller row is the cheap half of that trade. */
  .opt-text {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-ui);
    line-height: 1.35;
    overflow-wrap: anywhere;
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
    font-size: var(--fs-tiny);
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
  }
  .opt-pct {
    flex-shrink: 0;
    min-width: 2.6em;
    text-align: right;
    font-size: var(--fs-compact);
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
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .tally {
    /* `font` is a shorthand and resets font-variant — it has to come first. */
    font: inherit;
    font-variant-numeric: tabular-nums;
    padding: 0;
    background: transparent;
    border: none;
    color: var(--text-muted);
    text-align: left;
    cursor: pointer;
  }
  .tally.flat {
    cursor: default;
  }
  @media (pointer: fine) {
    .tally:not(.flat):hover {
      color: var(--text);
      background: transparent;
      text-decoration: underline;
    }
  }
  .state.done {
    color: var(--accent-hover);
    font-weight: 600;
  }
  .state.right {
    color: var(--ok-text);
    font-weight: 600;
  }
  .state.miss {
    color: var(--danger-text);
    font-weight: 600;
  }
  /* ---- phone ---- */
  @media (pointer: coarse), (max-width: 768px) {
    /* 43px rows 6px apart, on a control where a mis-tap MOVES your vote to the
       neighbouring option with no confirmation and nothing that says it moved. */
    .opt-body {
      padding: 13px 12px;
    }
    .poll-opts {
      gap: var(--sp-2);
    }
    /* The tally is now a real control, not a caption. */
    .tally:not(.flat) {
      min-height: var(--tap-min);
      display: inline-flex;
      align-items: center;
    }
    .poll-foot {
      align-items: center;
    }
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
