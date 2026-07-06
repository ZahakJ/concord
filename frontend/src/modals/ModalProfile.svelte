<script>
  import Modal from "./Modal.svelte";
  let { identity, onSubmit, onClose } = $props();
  let name = $state(identity.displayName || "");
  let status = $state(identity.status || "");
  let emoji = $state(identity.emoji || "");
  let color = $state(identity.color || "#5b6ef5");

  const EMOJIS = ["😀", "😎", "🦊", "🐸", "👾", "🧙", "🚀", "🌸", "⚡", "🔥", "🌙", "🎮"];

  function save() {
    onSubmit({ name: name.trim(), status: status.trim(), emoji, color });
  }
</script>

<Modal title="Your profile" {onClose}>
  <div class="preview">
    <div class="avatar" style="background:{color}">
      {emoji || (name || "?").slice(0, 2)}
    </div>
    <div class="preview-text">
      <strong>{name || "Your name"}</strong>
      {#if status}<span class="muted">{status}</span>{/if}
    </div>
  </div>

  <label class="field">
    <span class="muted">Display name</span>
    <input bind:value={name} maxlength="32" placeholder="Your name" />
  </label>
  <label class="field">
    <span class="muted">Status</span>
    <input bind:value={status} maxlength="64" placeholder="e.g. building something epic" />
  </label>
  <div class="field">
    <span class="muted">Avatar emoji</span>
    <div class="emoji-row">
      <button class="emoji" class:sel={emoji === ""} onclick={() => (emoji = "")} title="Use initials">Aa</button>
      {#each EMOJIS as e (e)}
        <button class="emoji" class:sel={emoji === e} onclick={() => (emoji = e)}>{e}</button>
      {/each}
    </div>
  </div>
  <label class="field row-field">
    <span class="muted">Accent color</span>
    <input type="color" bind:value={color} />
  </label>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save} disabled={!name.trim()}>Save</button>
  </div>
</Modal>

<style>
  .preview {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px;
    background: var(--bg-input);
    border-radius: 8px;
  }
  .avatar {
    width: 44px;
    height: 44px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    font-size: 20px;
    color: white;
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
  }
  .preview-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: 13px;
    text-align: left;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    text-align: left;
    font-size: 12px;
  }
  .row-field {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }
  .row-field input[type="color"] {
    width: 48px;
    height: 30px;
    padding: 2px;
    cursor: pointer;
  }
  .emoji-row {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .emoji {
    background: var(--bg-input);
    border: 1px solid var(--border);
    padding: 4px 8px;
    font-size: 16px;
    border-radius: 6px;
    color: var(--text);
  }
  .emoji.sel {
    border-color: var(--accent);
    background: rgba(91, 110, 245, 0.2);
  }
</style>
