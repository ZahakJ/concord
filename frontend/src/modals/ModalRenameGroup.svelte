<script>
  import Modal from "./Modal.svelte";
  import { api } from "../lib/api.js";
  import { refreshGuilds, flash } from "../lib/state.svelte.js";

  let { guildId, current = "", onClose } = $props();
  let name = $state(current);
  let busy = $state(false);

  async function save() {
    if (busy) return;
    busy = true;
    try {
      await api.renameDM(guildId, name.trim());
      await refreshGuilds();
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title="Rename group" {onClose}>
  <p class="muted">Give this group DM a name. Leave it blank to go back to the members' names.</p>
  <input
    placeholder="Group name"
    bind:value={name}
    autofocus
    onkeydown={(e) => e.key === "Enter" && save()}
  />
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save} disabled={busy}>Save</button>
  </div>
</Modal>

<style>
  p {
    margin: 0 0 10px;
    font-size: 13px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 12px;
  }
</style>
