<script>
  // New forum post: a title + opening message → a thread channel under the
  // forum, then jump straight into it.
  import Modal from "./Modal.svelte";
  import { S, refreshGuilds, selectChannel, flash } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";

  let { forum, onClose } = $props();
  let title = $state("");
  let body = $state("");
  let busy = $state(false);

  async function create(e) {
    e?.preventDefault();
    if (!title.trim() || busy) return;
    busy = true;
    try {
      const ch = await api.createThread(S.activeGuildId, forum.id, title.trim(), body.trim());
      await refreshGuilds();
      onClose();
      await selectChannel(ch.id);
      flash("Post created", "success");
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }
</script>

<Modal title="New post in {forum.name}" {onClose}>
  <form onsubmit={create} class="np">
    <label class="field">
      <span class="muted">Title</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input bind:value={title} maxlength="64" placeholder="What's this post about?" autofocus />
    </label>
    <label class="field">
      <span class="muted">First message</span>
      <textarea bind:value={body} rows="4" maxlength="4000" placeholder="Start the discussion… (markdown works)"></textarea>
    </label>
    <div class="actions">
      <button type="button" class="ghost" onclick={onClose}>Cancel</button>
      <button type="submit" disabled={!title.trim() || busy}>Post</button>
    </div>
  </form>
</Modal>

<style>
  .np {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field textarea {
    resize: vertical;
    font-family: inherit;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
