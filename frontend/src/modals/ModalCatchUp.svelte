<script>
  // "Catch me up": the assistant summarizes this channel's recent history.
  // Opens, asks the backend, shows the bullets. The summary is ephemeral —
  // closed is gone.
  //
  // Which engine answered is NOT assumed here. It used to be: this modal
  // hardcoded "summarized on this device by your local model", which was true
  // while Ollama was the only engine and would have become a false privacy
  // claim the moment the shared brain answered instead. Both the badge and the
  // provenance line now come from the response — `engine` and the `note` the
  // backend writes — so a brain answer can never present itself as a local one.
  import { onMount } from "svelte";
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { api } from "../lib/api.js";
  import { engineLabel, awaitBrainJob } from "../lib/assist.js";

  let { channelId, channelName, onClose } = $props();
  let summary = $state("");
  let engine = $state("");
  let note = $state("");
  let error = $state("");
  let loading = $state(true);
  // Set while a queued brain job is being polled, so the wait line can say what
  // it is actually waiting for instead of implying the local model is thinking.
  let waitingOnBrain = $state(false);
  let closed = false;

  // Adopt whatever the backend reported, in one place, so text and label can't
  // drift apart.
  function adopt(out) {
    summary = out?.text || "";
    engine = out?.engine || "";
    note = out?.note || "";
  }

  onMount(async () => {
    try {
      const out = await api.assistCatchUp(channelId);
      adopt(out);
      if (out?.pending && out?.jobId) {
        waitingOnBrain = true;
        engine = out.engine || "brain";
        const done = await awaitBrainJob(out.jobId, { cancelled: () => closed });
        if (done) adopt(done);
      }
    } catch (err) {
      error = String(err?.message || err);
    }
    waitingOnBrain = false;
    loading = false;
  });

  // The modal is torn down on close; stop polling with it.
  $effect(() => () => (closed = true));
</script>

<Modal title={`Catch me up — #${channelName || "channel"}`} {onClose}>
  {#if engine || note}
    <p class="muted">
      <Icon name="spark" size={11} />
      <span class="cu-engine" class:brain={engine === "brain"}>{engineLabel(engine)}</span>
      {note}
    </p>
  {/if}
  {#if loading}
    <div class="cu-wait">
      <span class="cu-dot"></span><span class="cu-dot"></span><span class="cu-dot"></span>
      {#if waitingOnBrain}
        waiting on the shared brain… (queued until a session picks it up)
      {:else}
        reading the conversation… (a local model on CPU takes a moment)
      {/if}
    </div>
  {:else if error}
    <div class="cu-error">{error}</div>
  {:else}
    <div class="cu-body">{summary}</div>
  {/if}
  <div class="actions">
    <button class="ghost" onclick={onClose}>Close</button>
  </div>
</Modal>

<style>
  p.muted {
    margin: 0;
    font-size: 12.5px;
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  /* The engine badge. Two visually distinct states on purpose: "local" and
     "shared brain" mean different things about where the conversation went,
     so they must not be mistakable for each other at a glance. */
  .cu-engine {
    padding: 1px 8px;
    border-radius: 999px;
    background: var(--accent-soft);
    border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent);
    color: var(--accent-hover);
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    white-space: nowrap;
  }
  .cu-engine.brain {
    background: color-mix(in srgb, var(--warn, #e0a13a) 14%, transparent);
    border-color: color-mix(in srgb, var(--warn, #e0a13a) 45%, transparent);
    color: var(--warn, #e0a13a);
  }
  .cu-body {
    margin-top: 10px;
    padding: 12px 14px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font-size: 13.5px;
    line-height: 1.6;
    white-space: pre-wrap;
    max-height: 46vh;
    overflow-y: auto;
  }
  .cu-error {
    margin-top: 10px;
    padding: 10px 12px;
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--danger, #e5484d) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--danger, #e5484d) 35%, transparent);
    font-size: 13px;
  }
  .cu-wait {
    margin-top: 12px;
    display: flex;
    align-items: center;
    gap: 5px;
    color: var(--text-muted);
    font-size: 13px;
  }
  .cu-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--accent);
    animation: cu-pulse 1.1s ease-in-out infinite;
  }
  .cu-dot:nth-child(2) { animation-delay: 0.18s; }
  .cu-dot:nth-child(3) { animation-delay: 0.36s; }
  @keyframes cu-pulse {
    0%, 100% { opacity: 0.25; transform: scale(0.85); }
    50% { opacity: 1; transform: scale(1); }
  }
</style>
