<script>
  // The advanced composer: a roomy editor with a full formatting toolbar, a text
  // colour picker, a rich-embed builder, and a live preview — for the times the
  // one-line composer isn't enough. Everything it produces is ordinary message
  // content (markdown + an optional embed token), so it sends like any message.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EmbedView from "../EmbedView.svelte";
  import { S, flash, customEmojiMap } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { renderMarkdown, COLOR_NAMES } from "../lib/markdown.js";
  import { encodeEmbed } from "../lib/richembed.js";

  let { onClose, initial = "" } = $props();

  let body = $state(initial);
  let ta = $state(null);
  let busy = $state(false);

  // Embed builder (off until the user adds one).
  let embedOn = $state(false);
  let embed = $state({ color: "#14a394", title: "", desc: "", fields: [] });

  const cemoji = $derived(customEmojiMap());
  const previewEmbed = $derived(
    embedOn && (embed.title || embed.desc || embed.fields.some((f) => f.name || f.value))
      ? embed
      : null,
  );
  const canPost = $derived(!busy && (body.trim() || previewEmbed) && !!S.activeChannelId);

  const PALETTE = Object.entries(COLOR_NAMES); // [name, hex]

  // --- formatting: wrap or line-prefix the current selection ---
  function surround(before, after = before) {
    const el = ta;
    if (!el) return;
    const s = el.selectionStart,
      e = el.selectionEnd;
    const sel = body.slice(s, e) || "text";
    body = body.slice(0, s) + before + sel + after + body.slice(e);
    queueMicrotask(() => {
      el.focus();
      el.selectionStart = s + before.length;
      el.selectionEnd = s + before.length + sel.length;
    });
  }
  function linePrefix(prefix) {
    const el = ta;
    if (!el) return;
    const s = el.selectionStart;
    const lineStart = body.lastIndexOf("\n", s - 1) + 1;
    body = body.slice(0, lineStart) + prefix + body.slice(lineStart);
    queueMicrotask(() => {
      el.focus();
      el.selectionStart = el.selectionEnd = s + prefix.length;
    });
  }
  function applyColor(hexOrName) {
    surround(`{${hexOrName}|`, "}");
  }

  const TOOLS = [
    { icon: "bold", title: "Bold", run: () => surround("**") },
    { icon: "italic", title: "Italic", run: () => surround("*") },
    { icon: "underline", title: "Underline", run: () => surround("__") },
    { icon: "strike", title: "Strikethrough", run: () => surround("~~") },
    { icon: "spoiler", title: "Spoiler", run: () => surround("||") },
    { icon: "code", title: "Code", run: () => surround("`") },
    { icon: "codeblock", title: "Code block", run: () => surround("```\n", "\n```") },
    { icon: "quote", title: "Quote", run: () => linePrefix("> ") },
    { icon: "list", title: "List", run: () => linePrefix("- ") },
    { icon: "heading", title: "Heading", run: () => linePrefix("## ") },
  ];

  function addField() {
    if (embed.fields.length < 8) embed.fields = [...embed.fields, { name: "", value: "" }];
  }
  function removeField(i) {
    embed.fields = embed.fields.filter((_, j) => j !== i);
  }

  async function post() {
    if (!canPost) return;
    busy = true;
    try {
      let content = body.trim();
      if (previewEmbed) {
        const token = encodeEmbed(previewEmbed);
        content = content ? `${content}\n${token}` : token;
      }
      await api.sendMessage(S.activeChannelId, content, "");
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title="Advanced composer" wide {onClose}>
  <div class="ac">
    <div class="editor">
      <div class="toolbar" role="toolbar" aria-label="Formatting">
        {#each TOOLS as t (t.icon)}
          <button type="button" class="tb" title={t.title} aria-label={t.title} onmousedown={(e) => e.preventDefault()} onclick={t.run}>
            <Icon name={t.icon} size={15} />
          </button>
        {/each}
        <span class="tb-sep"></span>
        <div class="swatches" role="group" aria-label="Text colour">
          {#each PALETTE as [name, hex] (name)}
            <button
              type="button"
              class="sw"
              style="background:{hex}"
              title={`Colour: ${name}`}
              aria-label={`Colour ${name}`}
              onmousedown={(e) => e.preventDefault()}
              onclick={() => applyColor(name)}
            ></button>
          {/each}
        </div>
      </div>
      <textarea
        bind:this={ta}
        bind:value={body}
        class="draft"
        rows="7"
        placeholder="Write something — select text, then hit a tool or a colour swatch."
      ></textarea>

      <button type="button" class="embed-toggle" class:on={embedOn} onclick={() => (embedOn = !embedOn)}>
        <Icon name={embedOn ? "close" : "plus"} size={14} />
        {embedOn ? "Remove rich embed" : "Add a rich embed"}
      </button>

      {#if embedOn}
        <div class="embed-builder">
          <div class="row">
            <label class="color-field" title="Embed accent colour">
              <span class="clabel">Accent</span>
              <input type="color" bind:value={embed.color} />
            </label>
            <input class="grow" maxlength="200" placeholder="Embed title" bind:value={embed.title} />
          </div>
          <textarea class="edesc" rows="3" maxlength="2000" placeholder="Embed description (markdown supported)" bind:value={embed.desc}></textarea>
          {#each embed.fields as f, i (i)}
            <div class="row field-row">
              <input class="fname" maxlength="100" placeholder="Field name" bind:value={f.name} />
              <input class="grow" maxlength="400" placeholder="Field value" bind:value={f.value} />
              <button type="button" class="fx" aria-label="Remove field" onclick={() => removeField(i)}>
                <Icon name="close" size={13} />
              </button>
            </div>
          {/each}
          {#if embed.fields.length < 8}
            <button type="button" class="add-field" onclick={addField}>
              <Icon name="plus" size={13} /> Add field
            </button>
          {/if}
        </div>
      {/if}
    </div>

    <div class="preview">
      <div class="p-label">Preview</div>
      <div class="p-body">
        {#if body.trim()}
          <div class="md">{@html renderMarkdown(body, [], cemoji)}</div>
        {:else if !previewEmbed}
          <p class="muted tiny">Your message will appear here.</p>
        {/if}
        {#if previewEmbed}
          <EmbedView embed={previewEmbed} customEmoji={cemoji} />
        {/if}
      </div>
    </div>
  </div>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Cancel</button>
    <button onclick={post} disabled={!canPost}>Send</button>
  </div>
</Modal>

<style>
  .ac {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 14px;
    text-align: left;
  }
  @media (max-width: 720px) {
    .ac {
      grid-template-columns: 1fr;
    }
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 3px;
    padding: 6px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md) var(--radius-md) 0 0;
    border-bottom: none;
  }
  .tb {
    width: 30px;
    height: 30px;
    display: grid;
    place-items: center;
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
  }
  .tb:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .tb-sep {
    width: 1px;
    align-self: stretch;
    margin: 2px 4px;
    background: var(--border);
  }
  .swatches {
    display: flex;
    gap: 3px;
  }
  .sw {
    width: 18px;
    height: 18px;
    padding: 0;
    border-radius: 50%;
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.25);
    transition: transform 0.1s ease;
  }
  .sw:hover {
    transform: scale(1.18);
  }
  .draft {
    width: 100%;
    resize: vertical;
    border-radius: 0 0 var(--radius-md) var(--radius-md);
    font-family: inherit;
    line-height: 1.45;
  }
  .embed-toggle {
    margin-top: 10px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 7px 12px;
    font-size: 13px;
    font-weight: 600;
    color: var(--text);
    background: transparent;
    border: 1px dashed color-mix(in srgb, var(--accent) 55%, var(--border));
    border-radius: var(--radius-sm);
  }
  .embed-toggle :global(svg) {
    color: var(--accent);
  }
  .embed-toggle:hover {
    background: var(--accent-soft);
    border-color: var(--accent);
  }
  .embed-builder {
    margin-top: 10px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .row {
    display: flex;
    gap: 8px;
    align-items: center;
  }
  .grow {
    flex: 1;
    min-width: 0;
  }
  .color-field {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }
  .clabel {
    font-size: 12px;
    color: var(--text-muted);
  }
  .color-field input[type="color"] {
    width: 30px;
    height: 30px;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: none;
    cursor: pointer;
  }
  .edesc {
    width: 100%;
    resize: vertical;
    font-family: inherit;
  }
  .fname {
    width: 34%;
    min-width: 0;
  }
  .fx {
    flex-shrink: 0;
    width: 28px;
    height: 28px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    color: var(--text-muted);
    background: transparent;
  }
  .fx:hover {
    color: var(--danger);
    background: color-mix(in srgb, var(--danger) 14%, transparent);
  }
  .add-field {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 5px 9px;
    font-size: 12px;
    color: var(--accent);
    background: transparent;
    border-radius: var(--radius-sm);
  }
  .add-field:hover {
    background: var(--accent-soft);
  }
  .preview {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .p-label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
    margin-bottom: 6px;
  }
  .p-body {
    flex: 1;
    padding: 12px;
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow-y: auto;
    max-height: 380px;
  }
  .md {
    font-size: 14px;
    line-height: 1.5;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 16px;
  }
</style>
