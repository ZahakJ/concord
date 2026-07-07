<script>
  import Modal from "./Modal.svelte";
  let {
    onSubmit,
    onClose,
    title = "Create a guild",
    hint = "A guild is your own end-to-end-encrypted space with channels.",
    placeholder = "Guild name",
  } = $props();
  let name = $state("");
</script>

<Modal {title} {onClose}>
  <p class="muted">{hint}</p>
  <input
    {placeholder}
    bind:value={name}
    autofocus
    onkeydown={(e) => e.key === "Enter" && name.trim() && onSubmit(name)}
  />
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={() => onSubmit(name)} disabled={!name.trim()}>Create</button>
  </div>
</Modal>

<style>
  p {
    margin: 0;
    font-size: 13px;
  }
</style>
