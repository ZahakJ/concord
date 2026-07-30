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

  const filled = $derived(opts.map((o) => o.text.trim()).filter(Boolean));
  const canPost = $derived(!!q.trim() && filled.length >= 2 && !busy);
  const full = $derived(opts.length >= POLL_EMOJI.length);

  // What the poll will look like once posted, with nobody having voted yet.
  const previewPoll = $derived({
    q: q.trim() || "Your question",
    opts: filled.length ? filled : ["First option", "Second option"],
    multi,
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
      await api.sendMessage(S.activeChannelId, encodePoll({ q: q.trim(), opts: filled, multi }), "");
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
        <span class="opt-num">{POLL_EMOJI[i]}</span>
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
    <span class="switch" class:on={multi}><span class="knob"></span></span>
    <span>Allow selecting multiple options</span>
  </button>

  <div class="preview-wrap">
    <span class="lbl">Preview</span>
    <div class="preview" transition:fade={{ duration: 120 }}>
      <PollView m={{ reactions: {} }} poll={previewPoll} preview />
    </div>
  </div>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={post} disabled={!canPost}>Post poll</button>
  </div>
</Modal>

<style>
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
    text-align: left;
  }
  .lbl {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .opt-row {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
  }
  .opt-row input {
    flex: 1;
    min-width: 0;
  }
  .opt-num {
    display: grid;
    place-items: center;
    font-size: 15px;
    width: 30px;
    height: 30px;
    flex-shrink: 0;
    border-radius: 50%;
    background: var(--accent-soft);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--accent) 35%, transparent);
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
      background 0.12s ease,
      color 0.12s ease;
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
      background 0.12s ease,
      border-color 0.12s ease;
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
      gap: 12px;
    }
    .opt-x {
      width: var(--tap-min);
      height: var(--tap-min);
    }
  }
  .switch {
    width: 34px;
    height: 20px;
    border-radius: 10px;
    background: var(--bg-3);
    position: relative;
    flex-shrink: 0;
    transition: background 0.15s ease;
  }
  .switch.on {
    background: var(--accent);
  }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: #fff;
    transition: transform 0.15s ease;
  }
  .switch.on .knob {
    transform: translateX(14px);
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
    gap: 8px;
    margin-top: 16px;
  }
  @media (prefers-reduced-motion: reduce) {
    .knob,
    .switch,
    .preview {
      transition: none;
    }
  }
</style>
