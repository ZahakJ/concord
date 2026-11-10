<script>
  import Modal from "./Modal.svelte";
  import { api } from "../lib/api.js";
  import { S, refreshGuilds, flash } from "../lib/state.svelte.js";
  import { slugChannelName } from "../lib/guildtemplates.js";
  import { untrack } from "svelte";

  let { guildId, channelId, current = "", onClose } = $props();
  let name = $state(untrack(() => current));
  let busy = $state(false);
  // The same naming rule as Create a channel: text channels are slugged, voice
  // channels keep the words they were given. Rename used to store whatever was
  // typed, so the sidebar mixed "#feature-ideas" with "#Feature Ideas!!".
  const isVoice = $derived(
    (S.guilds.find((g) => g.id === guildId)?.channels || []).find((c) => c.id === channelId)?.type === "voice",
  );
  const final = $derived(isVoice ? name.trim() : slugChannelName(name));
  const showSlug = $derived(!!name.trim() && final !== name.trim());

  async function save() {
    if (busy || !final || final === current) return;
    busy = true;
    try {
      await api.renameChannel(guildId, channelId, final);
      await refreshGuilds();
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title="Rename channel" {onClose}>
  <p class="muted">Everyone in the guild sees the new name.</p>
  <input
    placeholder="channel-name"
    maxlength="80"
    bind:value={name}
    autofocus={!S.isMobile}
    aria-label="Channel name"
    onkeydown={(e) => e.key === "Enter" && save()}
  />
  {#if showSlug}
    <p class="slug-line" role="status">Will be renamed to <strong>#{final}</strong></p>
  {/if}
  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={save} disabled={busy || !final || final === current}>Rename</button>
  </div>
</Modal>

<style>
  p {
    margin: 0 0 10px;
  }
  .slug-line {
    margin: var(--sp-2) 0 0;
    font-size: var(--fs-compact);
    color: var(--text-muted);
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
