<script>
  // Publish an announcement: copy a message from an announcement channel into
  // every linked consumer channel, optionally topped with a comment —
  // Discord's "Publish" flow, guild-local.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { S, activeGuild, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { plural } from "../lib/plural.js";
  import { encodeAnnounce } from "../lib/announce.js";
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
    // A token, not a blockquote: the renderer gives it its own shape, and the
    // original author's name rides along instead of being replaced by whoever
    // pressed Publish.
    const body = encodeAnnounce({
      from: channel.name,
      author: message.sender || "", // MessageView.sender is the authenticated fingerprint
      body: message.content,
      note: comment.trim(),
    });
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
    flash(
      sent === targets.length
        ? `Published to ${plural(sent, "channel")} 📣`
        : `Published to ${sent}/${targets.length} channels`,
      sent ? "success" : "error",
    );
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
    font-size: var(--fs-ui);
    line-height: 1.45;
    max-height: 90px;
    overflow: hidden;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .small {
    font-size: var(--fs-small);
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
    color: var(--accent-hover);
    border-radius: 999px;
    padding: 1px 8px;
    font-size: var(--fs-small);
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
