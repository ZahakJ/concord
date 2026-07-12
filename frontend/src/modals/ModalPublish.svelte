<script>
  // Publish an announcement: copy a message from an announcement channel into
  // every linked consumer channel, optionally topped with a comment —
  // Discord's "Publish" flow, guild-local.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { previewText } from "../lib/attachments.js";

  let { message, channel, onClose } = $props();

  const g = activeGuild();
  const targets = (channel.links || [])
    .map((id) => g?.channels.find((c) => c.id === id))
    .filter(Boolean);
  let comment = $state("");
  let busy = $state(false);

  async function publish() {
    if (!targets.length) return;
    busy = true;
    const body =
      (comment.trim() ? comment.trim() + "\n" : "") +
      `>>> ↪ *published from* **#${channel.name}**\n` +
      message.content;
    let sent = 0;
    for (const t of targets) {
      try {
        await api.sendMessage(t.id, body, "");
        sent++;
      } catch {
        /* keep going; report the tally */
      }
    }
    busy = false;
    flash(sent === targets.length ? `Published to ${sent} channel${sent > 1 ? "s" : ""} 📣` : `Published to ${sent}/${targets.length} channels`, sent ? "success" : "error");
    onClose();
  }
</script>

<Modal title="Publish announcement" {onClose}>
  <div class="orig">
    <span class="muted small">
      <Icon name="megaphone" size={12} />
      {message.senderName || message.sender.slice(0, 9)} in #{channel.name}
    </span>
    <div class="body">{previewText(message.content)}</div>
  </div>

  <label class="field">
    <span class="muted">Add a comment (optional)</span>
    <textarea bind:value={comment} rows="2" maxlength="500" placeholder="Say something about this…"></textarea>
  </label>

  <div class="targets muted small">
    Publishing to:
    {#each targets as t, i (t.id)}
      <span class="chip">#{t.name}</span>
    {/each}
    {#if !targets.length}
      <em>no linked channels — set them via the channel's “Linked Channels” menu.</em>
    {/if}
  </div>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={publish} disabled={busy || !targets.length}>
      <Icon name="megaphone" size={14} /> Publish
    </button>
  </div>
</Modal>

<style>
  .orig {
    border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 10px;
  }
  .orig .body {
    font-size: 13px;
    line-height: 1.45;
    max-height: 90px;
    overflow: hidden;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .small {
    font-size: 11.5px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 10px;
  }
  .field textarea {
    resize: vertical;
    font-family: inherit;
  }
  .targets {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 5px;
    margin-bottom: 10px;
  }
  .chip {
    background: var(--accent-soft);
    color: var(--accent);
    border-radius: 999px;
    padding: 1px 8px;
    font-size: 11.5px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
</style>
