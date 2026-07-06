<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  let { onSubmit, onClose, error } = $props();
  let code = $state("");
  let busy = $state(false);

  async function join() {
    if (!code.trim() || busy) return;
    busy = true;
    await onSubmit(code.trim());
    busy = false; // if the modal is still open, the parent set an error
  }

  async function paste() {
    try {
      code = (await navigator.clipboard.readText()).trim();
    } catch {
      /* clipboard blocked — user can paste manually */
    }
  }
</script>

<Modal title="Join a server" {onClose}>
  <p class="muted lead">Paste the invite code a friend sent you.</p>

  <div class="input-well" class:err={!!error}>
    <textarea rows="4" placeholder="Paste invite code here…" bind:value={code}></textarea>
    <button class="paste" title="Paste from clipboard" aria-label="Paste from clipboard" onclick={paste}>
      <Icon name="download" size={13} /> Paste
    </button>
  </div>

  {#if error}<div class="error"><Icon name="close" size={12} /> {error}</div>{/if}

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={join} disabled={!code.trim() || busy}>{busy ? "Joining…" : "Join"}</button>
  </div>
</Modal>

<style>
  .lead {
    margin: 0;
    font-size: 13px;
  }
  .input-well {
    position: relative;
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-0);
    overflow: hidden;
  }
  .input-well:focus-within {
    border-color: var(--accent);
  }
  .input-well.err {
    border-color: var(--danger);
  }
  textarea {
    border: none;
    background: transparent;
    resize: vertical;
    font-family: ui-monospace, monospace;
    font-size: 12px;
    min-height: 80px;
  }
  textarea:focus {
    border: none;
  }
  .paste {
    position: absolute;
    top: 8px;
    right: 8px;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 12px;
    padding: 4px 10px;
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .paste:hover {
    background: var(--border);
    color: var(--text);
  }
  .error {
    display: flex;
    align-items: center;
    gap: 6px;
  }
</style>
