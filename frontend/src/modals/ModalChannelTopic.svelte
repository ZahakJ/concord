<script>
  import Modal from "./Modal.svelte";
  import { S } from "../lib/state.svelte.js";
  let { channel, onSubmit, onClose } = $props();
  let topic = $state(channel?.topic || "");
</script>

<Modal title="Channel topic" {onClose}>
  <p class="muted">
    Shown in the header of <strong>#{channel?.name}</strong>. Leave blank to clear it.
  </p>
  <!-- svelte-ignore a11y_autofocus -->
  <textarea
    bind:value={topic}
    rows="3"
    maxlength="300"
    placeholder="What's this channel about?"
    autofocus={!S.isMobile}
  ></textarea>
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={() => onSubmit(topic)}>Save</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    font-size: 13px;
  }
  textarea {
    width: 100%;
    resize: vertical;
    font-family: inherit;
    font-size: 13px;
    padding: 8px 10px;
    margin-top: 8px;
    box-sizing: border-box;
  }
</style>
