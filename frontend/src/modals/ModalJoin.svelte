<script>
  import Modal from "./Modal.svelte";
  let { onSubmit, onClose, error } = $props();
  let code = $state("");
</script>

<Modal title="Join a guild" {onClose}>
  <p class="muted">Paste an invite code shared by the guild owner.</p>
  <textarea rows="4" placeholder="Invite code…" bind:value={code}></textarea>
  {#if error}<div class="error">{error}</div>{/if}
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={() => onSubmit(code)} disabled={!code.trim()}>Join</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    font-size: 13px;
  }
  textarea {
    resize: vertical;
    font-family: ui-monospace, monospace;
    font-size: 12px;
  }
</style>
