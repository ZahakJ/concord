<script>
  import Modal from "./Modal.svelte";
  import { api } from "../lib/api.js";
  import { S, refreshGuilds, flash } from "../lib/state.svelte.js";

  let { guildId, channelId, current = "", onClose } = $props();
  let name = $state(current);
  let busy = $state(false);

  async function save() {
    if (busy || !name.trim()) return;
    busy = true;
    try {
      await api.renameChannel(guildId, channelId, name.trim());
      await refreshGuilds();
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title="Rename channel" {onClose}>
  <p class="muted">Everyone in the server sees the new name.</p>
  <input
    placeholder="channel-name"
    maxlength="80"
    bind:value={name}
    autofocus={!S.isMobile}
    onkeydown={(e) => e.key === "Enter" && save()}
  />
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save} disabled={busy || !name.trim() || name.trim() === current}>Rename</button>
  </div>
</Modal>

<style>
  p {
    margin: 0 0 10px;
  }
  input {
    width: 100%;
    box-sizing: border-box;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin-top: var(--sp-3);
  }
</style>
