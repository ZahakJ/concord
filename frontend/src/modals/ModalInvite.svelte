<script>
  import Modal from "./Modal.svelte";
  let { code, onCopy, onClose } = $props();
</script>

<Modal title="Invite to guild" {onClose}>
  <p class="muted">
    Share this code out-of-band. The recipient pastes it into “Join with invite”.
  </p>
  <textarea rows="5" readonly value={code}></textarea>
  <div class="actions">
    <button class="ghost" onclick={onClose}>Close</button>
    <button onclick={() => onCopy(code)}>Copy code</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    font-size: 13px;
  }
  textarea {
    resize: none;
    font-family: ui-monospace, monospace;
    font-size: 11px;
    word-break: break-all;
  }
</style>
