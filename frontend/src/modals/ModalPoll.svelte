<script>
  // Compose a poll: a question and 2–10 options. Posts as a single poll-token
  // message (see lib/polls); people vote by reacting, which this app renders as
  // bars. No backend involvement — it's an ordinary message.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { encodePoll, POLL_EMOJI } from "../lib/polls.js";

  let { onClose } = $props();

  let q = $state("");
  let opts = $state(["", ""]);
  let multi = $state(false);
  let busy = $state(false);

  const filled = $derived(opts.map((o) => o.trim()).filter(Boolean));
  const canPost = $derived(!!q.trim() && filled.length >= 2 && !busy);

  function addOpt() {
    if (opts.length < POLL_EMOJI.length) opts = [...opts, ""];
  }
  function removeOpt(i) {
    if (opts.length > 2) opts = opts.filter((_, j) => j !== i);
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

<Modal title="Create a poll" {onClose}>
  <label class="field">
    <span class="muted">Question</span>
    <input bind:value={q} maxlength="300" placeholder="What should we play tonight?" />
  </label>

  <div class="field">
    <span class="muted">Options</span>
    {#each opts as _, i (i)}
      <div class="opt-row">
        <span class="opt-num">{POLL_EMOJI[i]}</span>
        <input bind:value={opts[i]} maxlength="100" placeholder={`Option ${i + 1}`} />
        {#if opts.length > 2}
          <button type="button" class="opt-x" aria-label="Remove option" onclick={() => removeOpt(i)}>
            <Icon name="close" size={13} />
          </button>
        {/if}
      </div>
    {/each}
    {#if opts.length < POLL_EMOJI.length}
      <button type="button" class="add-opt" onclick={addOpt}>
        <Icon name="plus" size={14} /> Add option
      </button>
    {/if}
  </div>

  <button type="button" class="multi" role="switch" aria-checked={multi} onclick={() => (multi = !multi)}>
    <span class="switch" class:on={multi}><span class="knob"></span></span>
    <span>Allow selecting multiple options</span>
  </button>

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
    transition: background 0.12s ease, color 0.12s ease;
  }
  .opt-x:hover {
    background: color-mix(in srgb, var(--danger) 16%, transparent);
    color: var(--danger);
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
    font-size: 13px;
    font-weight: 600;
    color: var(--text);
    border: 1px dashed color-mix(in srgb, var(--accent) 55%, var(--border));
    border-radius: var(--radius-sm);
    transition: background 0.12s ease, border-color 0.12s ease;
  }
  .add-opt :global(svg) {
    color: var(--accent);
  }
  .add-opt:hover {
    background: var(--accent-soft);
    border-color: var(--accent);
  }
  .multi {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
    color: var(--text);
    margin-bottom: 4px;
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
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }
  @media (prefers-reduced-motion: reduce) {
    .knob,
    .switch {
      transition: none;
    }
  }
</style>
