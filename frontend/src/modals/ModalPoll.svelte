<script>
  // Compose a poll: a question and 2–10 options. Posts as a single poll-token
  // message (see lib/polls); people vote by reacting, which this app renders as
  // bars. No backend involvement — it's an ordinary message.
  //
  // The preview is the point: a poll is a thing other people will look at, so
  // you get to look at it first, in exactly the form they'll see.
  import { flip } from "svelte/animate";
  import { fade } from "svelte/transition";
  import Modal from "./Modal.svelte";
  import Segmented from "../Segmented.svelte";
  import Switch from "../Switch.svelte";
  import Icon from "../Icon.svelte";
  import PollView from "../PollView.svelte";
  import { S, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { encodePoll, POLL_EMOJI } from "../lib/polls.js";

  let { onClose } = $props();

  let q = $state("");
  // Keyed rows: Svelte needs a stable identity per option to animate a removal
  // from the middle without the fields below it appearing to shuffle.
  let seq = 2;
  let opts = $state([
    { id: 0, text: "" },
    { id: 1, text: "" },
  ]);
  let multi = $state(false);
  let busy = $state(false);
  let inputs = {}; // id -> element, so a new row can take focus

  // Optional close time, as a duration from "now" — pinned to an absolute
  // epoch second only at post time, so a modal left open doesn't quietly
  // shorten the poll.
  const CLOSE_OPTS = [
    { secs: 0, label: "Never" },
    { secs: 3600, label: "1 hour" },
    { secs: 86400, label: "24 hours" },
    { secs: 259200, label: "3 days" },
  ];
  let closeIn = $state(0);

  // Quiz mode: one option is the right answer, revealed after you vote (or
  // the poll closes). The pick is tracked by row ID, not index — deleting an
  // option above the answer must not silently re-aim the quiz.
  let quiz = $state(false);
  let answerId = $state(-1);

  // Rows that will actually post (blank rows drop out), keeping their ids so
  // the quiz answer can be resolved to its FINAL index in the encoded poll.
  const filledRows = $derived(opts.map((o) => ({ id: o.id, text: o.text.trim() })).filter((o) => o.text));
  const filled = $derived(filledRows.map((o) => o.text));
  const answerIdx = $derived(quiz ? filledRows.findIndex((o) => o.id === answerId) : -1);
  // A quiz without an answer key is just a poll wearing a costume — hold Post
  // until the author has picked the right row.
  const canPost = $derived(!!q.trim() && filled.length >= 2 && !busy && (!quiz || answerIdx >= 0));
  const full = $derived(opts.length >= POLL_EMOJI.length);

  // What the poll will look like once posted, with nobody having voted yet.
  const previewPoll = $derived({
    q: q.trim() || "Your question",
    opts: filled.length ? filled : ["First option", "Second option"],
    multi,
    // The preview shows the "Closes in …" line and the Quiz kicker exactly as
    // readers will see them; the answer itself stays hidden there because the
    // preview has no votes and hasn't closed — same rule as the real thing.
    until: closeIn ? Math.floor(Date.now() / 1000) + closeIn : undefined,
    answer: quiz && answerIdx >= 0 ? answerIdx : undefined,
  });

  function addOpt() {
    if (full) return;
    const id = seq++;
    opts = [...opts, { id, text: "" }];
    // Focus the row we just made — typing should continue, not require a click.
    queueMicrotask(() => inputs[id]?.focus());
  }
  function removeOpt(id) {
    if (opts.length > 2) opts = opts.filter((o) => o.id !== id);
  }
  // Enter moves to the next option, adding one if you're at the end — the
  // rhythm of writing a list, without reaching for the mouse each time.
  function onKey(e, i) {
    if (e.key !== "Enter") return;
    e.preventDefault();
    if (i === opts.length - 1) addOpt();
    else inputs[opts[i + 1].id]?.focus();
  }

  async function post() {
    if (!canPost || !S.activeChannelId) return;
    busy = true;
    try {
      // The duration becomes an absolute close time HERE, at the moment of
      // posting — every reader's client compares it to its own clock.
      const until = closeIn ? Math.floor(Date.now() / 1000) + closeIn : 0;
      await api.sendMessage(
        S.activeChannelId,
        encodePoll({ q: q.trim(), opts: filled, multi, until, answer: quiz ? answerIdx : -1 }),
        "",
      );
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title="Create a poll" {onClose} wide>
  <label class="field">
    <span class="lbl">Question</span>
    <!-- svelte-ignore a11y_autofocus -->
    <!-- Not on a phone: this is a bottom sheet, so the IME opens straight over
         the option rows, the multi-select switch, and the live preview that is
         the point of this dialog — before any of them has been seen. -->
    <input bind:value={q} maxlength="300" placeholder="What should we play tonight?" autofocus={!S.isMobile} />
  </label>

  <div class="field">
    <span class="lbl">Options</span>
    {#each opts as o, i (o.id)}
      <div class="opt-row" animate:flip={{ duration: 180 }}>
        <span class="opt-num" aria-hidden="true">{i + 1}</span>
        <input
          bind:this={inputs[o.id]}
          bind:value={o.text}
          maxlength="100"
          placeholder={`Option ${i + 1}`}
          onkeydown={(e) => onKey(e, i)}
        />
        {#if opts.length > 2}
          <button type="button" class="opt-x" aria-label="Remove option" onclick={() => removeOpt(o.id)}>
            <Icon name="close" size={13} />
          </button>
        {/if}
      </div>
    {/each}
    {#if !full}
      <button type="button" class="add-opt" onclick={addOpt}>
        <Icon name="plus" size={14} /> Add option
      </button>
    {:else}
      <span class="cap">That's all {POLL_EMOJI.length} options.</span>
    {/if}
  </div>

  <button type="button" class="multi" role="switch" aria-checked={multi} onclick={() => (multi = !multi)}>
    <span>Allow selecting multiple options</span>
    <Switch on={multi} />
  </button>

  <div class="field closes">
    <span class="lbl">Closes</span>
    <!-- Same segmented control as the calendar's — a closing time is a small
         setting, not a form of its own. -->
    <Segmented
      label="Poll closes"
      value={closeIn}
      onChange={(v) => (closeIn = Number(v))}
      options={CLOSE_OPTS.map((c) => ({ value: c.secs, label: c.label }))}
    />
  </div>

  <button type="button" class="multi" role="switch" aria-checked={quiz} onclick={() => (quiz = !quiz)}>
    <span>Quiz mode — one option is the right answer</span>
    <Switch on={quiz} />
  </button>

  {#if quiz}
    <div class="field ans-field" transition:fade={{ duration: 120 }}>
      <span class="lbl">Correct answer</span>
      {#if filledRows.length}
        <div class="ans-list" role="radiogroup" aria-label="Correct answer">
          {#each filledRows as o, i (o.id)}
            <button
              type="button"
              class="ans"
              class:sel={answerId === o.id}
              role="radio"
              aria-checked={answerId === o.id}
              onclick={() => (answerId = o.id)}
            >
              <span class="ans-num" aria-hidden="true">{i + 1}</span>
              <span class="ans-text">{o.text}</span>
              {#if answerId === o.id}<Icon name="check" size={14} />{/if}
            </button>
          {/each}
        </div>
      {:else}
        <span class="cap">Write the options first, then pick the right one.</span>
      {/if}
    </div>
  {/if}

  <div class="preview-wrap">
    <span class="lbl">Preview</span>
    <div class="preview" transition:fade={{ duration: 120 }}>
      <PollView m={{ reactions: {} }} poll={previewPoll} preview />
    </div>
  </div>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button class="primary" onclick={post} disabled={!canPost}>Post poll</button>
  </div>
</Modal>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: var(--sp-3);
    text-align: left;
  }
  .lbl {
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .opt-row {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin-bottom: 6px;
  }
  .opt-row input {
    flex: 1;
    min-width: 0;
  }
  /* Plain numerals in the faint tier, as the posted poll numbers its
     options — not keycap emoji on accent discs. */
  .opt-num {
    display: grid;
    place-items: center;
    width: 20px;
    flex-shrink: 0;
    font-size: var(--fs-compact);
    font-variant-numeric: tabular-nums;
    color: var(--text-faint);
  }
  .opt-x {
    flex-shrink: 0;
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    color: var(--text-muted);
    transition:
      background var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  .opt-x:hover {
    background: color-mix(in srgb, var(--danger) 16%, transparent);
    color: var(--danger-text);
  }
  /* Readable at rest: a dashed accent frame with neutral label text (not
     accent-on-transparent, which vanishes on pale theme accents) and an accent
     plus-glyph that fills in on hover. */
  .add-opt {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 9px 10px;
    margin-top: 2px;
    font-size: var(--fs-ui);
    font-weight: 600;
    color: var(--text);
    /* The global button style fills with the accent; this one is an affordance,
       not the action — the dashed frame IS the whole look. */
    background: transparent;
    border: 1px dashed color-mix(in srgb, var(--accent) 55%, var(--border));
    border-radius: var(--radius-sm);
    transition:
      background var(--dur-quick) ease,
      border-color var(--dur-quick) ease;
  }
  .add-opt :global(svg) {
    color: var(--accent-hover);
  }
  .add-opt:hover {
    background: var(--accent-soft);
    border-color: var(--accent);
  }
  .cap {
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  /* A switch row, not a button — the switch is the control, so the row itself
     has to drop the global accent fill. */
  .multi {
    display: flex;
    align-items: center;
    gap: 10px;
    /* Label first, control last — the reading order of every settings row in
       the app, and of the switch rows these two sit two lines away from. */
    justify-content: space-between;
    width: 100%;
    text-align: left;
    padding: 6px 2px;
    font-size: var(--fs-ui);
    color: var(--text);
    background: transparent;
  }
  /* Modal.svelte's mobile floor stretches .opt-x to 28x44 — tall enough, still
     36% too narrow — and 8px from an input whose own target now runs the full
     44px height. It's the destructive control on the row, and it sits right
     where the thumb comes off the keyboard. */
  @media (pointer: coarse) {
    .opt-row {
      gap: var(--sp-3);
    }
    .opt-x {
      width: var(--tap-min);
      height: var(--tap-min);
    }
  }
  /* The closing-time picker rides BELOW the multi switch, so it reads as one
     settings block rather than a second form. */
  .closes {
    margin: 4px 0 0;
  }
  /* The answer key: the same rows you just wrote, re-listed as radios — picking
     the truth should look like pointing at it, not re-typing it. */
  .ans-field {
    margin-top: var(--sp-1);
  }
  .ans-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .ans {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 7px 10px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--fs-ui);
    text-align: left;
    transition:
      background var(--dur-quick) ease,
      border-color var(--dur-quick) ease;
  }
  .ans:hover {
    border-color: var(--ok);
  }
  /* Chosen answer wears --ok, not the accent: it's the same "correct" ink
     PollView reveals later, previewed here for the author. */
  .ans.sel {
    border-color: var(--ok);
    background: var(--ok-soft);
  }
  .ans :global(svg) {
    color: var(--ok-text);
    flex-shrink: 0;
    margin-left: auto;
  }
  .ans-num {
    flex-shrink: 0;
    width: 16px;
    font-size: var(--fs-compact);
    font-variant-numeric: tabular-nums;
    color: var(--text-faint);
  }
  .ans-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (pointer: coarse) {
    .ans {
      min-height: var(--tap-min);
    }
  }
  .preview-wrap {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 14px;
    padding-top: 14px;
    border-top: 1px solid var(--border);
    text-align: left;
  }
  /* Not dimmed: the "Preview" label above already says this is a sample, and
     an opacity here paints the poll people will actually read at 3.7:1. */
  .preview {
    text-align: left;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin-top: var(--sp-4);
  }
  @media (prefers-reduced-motion: reduce) {
    .preview {
      transition: none;
    }
  }
</style>
