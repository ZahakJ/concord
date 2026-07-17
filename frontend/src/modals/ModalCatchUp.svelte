<script>
  // "Catch me up": the LOCAL assistant summarizes this channel's recent
  // history. Opens, asks the backend (which talks only to 127.0.0.1 Ollama),
  // and shows the bullets. Nothing leaves the machine; the summary is
  // ephemeral — closed is gone.
  import { onMount } from "svelte";
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { channelId, channelName, onClose } = $props();
  let summary = $state("");
  let error = $state("");
  let loading = $state(true);

  onMount(async () => {
    try {
      summary = await api.assistCatchUp(channelId);
    } catch (err) {
      error = String(err?.message || err);
    }
    loading = false;
  });
</script>

<Modal title={`Catch me up — #${channelName || "channel"}`} {onClose}>
  <p class="muted">
    <Icon name="spark" size={11} />
    Summarized on this device by your local model ({S.assist?.model || "Ollama"})
    — the conversation never leaves this machine.
  </p>
  {#if loading}
    <div class="cu-wait">
      <span class="cu-dot"></span><span class="cu-dot"></span><span class="cu-dot"></span>
      reading the conversation… (a local model on CPU takes a moment)
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
