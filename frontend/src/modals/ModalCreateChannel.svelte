<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Select from "../Select.svelte";
  import { S, activeGuild } from "../lib/state.svelte.js";
  import { slugChannelName } from "../lib/guildtemplates.js";
  let { onSubmit, onClose } = $props();

  let name = $state("");
  let topic = $state("");
  let type = $state("text");

  // A voice room is not addressed with a hash and is not named like an
  // address ("Study Hall", not "study-hall") — the empty-channel hero already
  // branches on exactly that — so slugging one would enforce a convention the
  // rest of the app does not follow.
  const slugged = $derived(type === "voice" ? name.trim() : slugChannelName(name));
  // Only worth showing when it CHANGED something. A live preview under a field
  // that reads back what you just typed is noise.
  const showSlug = $derived(!!name.trim() && slugged !== name.trim());
  // Pre-select the category if the modal was opened from a category's "+".
  let category = $state(S.modal?.category || "");

  const categories = $derived(
    [...(activeGuild()?.categories || [])].sort((a, b) => a.position - b.position),
  );

  // Nothing on either side of the wire refuses a name that already exists, so a
  // guild could end up with five channels called #general and no way to tell
  // which conversation was in which. It stays ALLOWED — a forum board and a
  // voice room can reasonably share a name, and a rename is not worth blocking
  // over — but it stops being something you do by accident.
  const clash = $derived(
    !!slugged &&
      (activeGuild()?.channels || []).some(
        (c) => !c.parent && c.name.toLowerCase() === slugged.toLowerCase(),
      ),
  );

  const TYPES = [
    { id: "text", label: "Text", icon: "hash", hint: "Send messages, files, images" },
    { id: "voice", label: "Voice", icon: "speaker", hint: "Talk together in a call" },
    { id: "announcement", label: "Announce", icon: "megaphone", hint: "A text channel for updates" },
    { id: "forum", label: "Forum", icon: "forum", hint: "A board of posts, each its own thread" },
  ];

  function submit(e) {
    e?.preventDefault();
    if (!slugged) return;
    onSubmit({ name: slugged, type, category, topic: topic.trim() });
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
      aria-invalid={clash ? "true" : undefined}
      autofocus={!S.isMobile}
    />
    {#if showSlug}
      <!-- Live, under the field, the way every mainstream app does it. A guild
           with #general sitting beside #Welcome & Rules reads as one nobody is
           in charge of. -->
      <p class="slug-line" role="status">
        Will be created as <strong>#{slugged}</strong>
      </p>
    {/if}
    {#if clash}
      <p class="warn-line" role="status">
        <Icon name="alert" size={13} />
        This guild already has a #{slugged} — two channels with one name are hard
        to tell apart.
      </p>
    {/if}
    {#if categories.length}
      <div class="cat-label">
        <span class="muted">Category</span>
        <Select
          label="Category"
          value={category}
          onPick={(v) => (category = v)}
          options={[{ value: "", label: "No category" }, ...categories.map((c) => ({ value: c.id, label: c.name }))]}
        />
      </div>
    {/if}
    {#if type !== "voice"}
      <!-- The topic was reachable only afterwards, through a menu item named
           after a different field. Setting up ten channels cost thirty
           interactions. -->
      <label class="cat-label">
        <span class="muted">Topic <span class="opt">optional</span></span>
        <input bind:value={topic} maxlength="180" placeholder="What this channel is for" />
      </label>
    {/if}
    <p class="muted hint">{TYPES.find((t) => t.id === type)?.hint}</p>
    <div class="actions">
      <button type="button" class="ghost" onclick={onClose}>Cancel</button>
      <button type="submit" disabled={!slugged}>Create</button>
    </div>
  </form>
</Modal>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .type-row {
    display: flex;
    gap: var(--sp-2);
  }
  .slug-line {
    margin: -6px 0 0;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    text-align: left;
  }
  .slug-line strong {
    color: var(--text);
    font-family: var(--font-mono, monospace);
  }
  .opt {
    color: var(--text-faint);
  }
  .warn-line {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    margin: -6px 0 0;
    color: var(--warn-text);
    font-size: var(--fs-compact);
    text-align: left;
  }
  .warn-line :global(svg) {
    flex: none;
    margin-top: 1px;
    color: var(--warn);
  }
  input[aria-invalid] {
    border-color: var(--warn);
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
  /* The ring is a box-shadow, not a thicker border: a border that grows on
     selection changes the tile's box and nudges the other three sideways, so
     the row twitches every time you change your mind. A shadow is painted
     outside the layout entirely. */
  .type.sel {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px var(--accent-soft);
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .hint {
    margin: 0;
    font-size: 12px;
  }
  .cat-label {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    font-size: 12px;
  }
</style>
