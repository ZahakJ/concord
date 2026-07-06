<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  let { onSubmit, onClose } = $props();

  let name = $state("");
  let type = $state("text");

  const TYPES = [
    { id: "text", label: "Text", icon: "hash", hint: "Send messages, files, images" },
    { id: "voice", label: "Voice", icon: "speaker", hint: "Talk together in a call" },
    { id: "announcement", label: "Announce", icon: "megaphone", hint: "A text channel for updates" },
  ];

  function submit(e) {
    e?.preventDefault();
    if (!name.trim()) return;
    onSubmit({ name: name.trim(), type });
  }
</script>

<Modal title="Create a channel" {onClose}>
  <form onsubmit={submit}>
    <div class="type-row">
      {#each TYPES as t (t.id)}
        <button
          type="button"
          class="type"
          class:sel={type === t.id}
          onclick={() => (type = t.id)}
          title={t.hint}
        >
          <Icon name={t.icon} size={16} />
          {t.label}
        </button>
      {/each}
    </div>
    <!-- svelte-ignore a11y_autofocus -->
    <input
      bind:value={name}
      maxlength="40"
      placeholder={type === "voice" ? "voice channel name" : "channel-name"}
      autofocus
    />
    <p class="muted hint">{TYPES.find((t) => t.id === type)?.hint}</p>
    <div class="actions">
      <button type="button" class="ghost" onclick={onClose}>Cancel</button>
      <button type="submit" disabled={!name.trim()}>Create</button>
    </div>
  </form>
</Modal>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .type-row {
    display: flex;
    gap: 8px;
  }
  .type {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 5px;
    padding: 12px 6px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    color: var(--text-muted);
    border-radius: var(--radius-md);
    font-size: 12px;
  }
  .type:hover {
    background: var(--bg-2);
    color: var(--text);
  }
  .type.sel {
    border-color: var(--accent);
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .hint {
    margin: 0;
    font-size: 12px;
  }
</style>
