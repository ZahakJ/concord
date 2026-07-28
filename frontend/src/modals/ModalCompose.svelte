<script>
  // The advanced composer: a large editor workspace with a full formatting
  // toolbar, a text-colour picker, a rich-embed builder, and a live preview —
  // for the times the one-line composer isn't enough. Everything it produces is
  // ordinary message content (markdown + an optional embed token), so it sends
  // like any message.
  import { tick } from "svelte";
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import EmbedView from "../EmbedView.svelte";
  import { S, flash, customEmojiMap } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { renderMarkdown, COLOR_NAMES } from "../lib/markdown.js";
  import { encodeEmbed, parseEmbed, stripEmbedToken } from "../lib/richembed.js";
  import { stampEphemeral, stripEphemeral, EPH_RE } from "../lib/ephemeral.svelte.js";

  // editId set ⇒ we're editing an existing message rather than composing a new
  // one: `initial` is that message's raw content, which we decode back into the
  // editor (body + embed builder) and save with api.editMessage.
  let { onClose, onSent, initial = "", editId = "" } = $props();

  // Decode an embed already present in the seed/edited content, and keep the
  // original disappearing-message token verbatim so an edit preserves the
  // message's own expiry instead of resetting it to the channel default.
  const seededEmbed = parseEmbed(initial);
  const ephToken = initial.match(EPH_RE)?.[0] || "";

  let body = $state(stripEphemeral(stripEmbedToken(initial)));
  let ta = $state(null);
  let busy = $state(false);

  // Embed builder (open when the message already carries one).
  let embedOn = $state(!!seededEmbed);
  let embed = $state(
    seededEmbed
      ? { ...seededEmbed, color: seededEmbed.color || "#14a394" }
      : { color: "#14a394", title: "", desc: "", fields: [] },
  );

  const cemoji = $derived(customEmojiMap());
  const previewEmbed = $derived(
    embedOn && (embed.title || embed.desc || embed.fields.some((f) => f.name || f.value))
      ? embed
      : null,
  );
  const canPost = $derived(!busy && (body.trim() || previewEmbed) && !!S.activeChannelId);
  const meInitial = $derived((S.displayName || "You").trim()[0]?.toUpperCase() || "Y");

  const PALETTE = Object.entries(COLOR_NAMES); // [name, hex]
  const noSelect = (e) => e.preventDefault(); // keep the textarea selection on tool click

  // --- formatting: wrap or line-prefix the current selection ---
  function surround(before, after = before) {
    const el = ta;
    if (!el) return;
    const s = el.selectionStart,
      e = el.selectionEnd;
    const sel = body.slice(s, e) || "text";
    body = body.slice(0, s) + before + sel + after + body.slice(e);
    // tick(), not queueMicrotask: the selection must be set against the
    // textarea AFTER Svelte flushes the new value, and microtask ordering
    // relative to that flush isn't guaranteed.
    tick().then(() => {
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
    tick().then(() => {
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
  ];
  const TOOLS2 = [
    { icon: "code", title: "Inline code", run: () => surround("`") },
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
      if (editId) {
        // Preserve the original disappearing-message expiry (ephToken), never
        // re-stamp — editing shouldn't reset or newly impose a TTL.
        await api.editMessage(S.activeChannelId, editId, ephToken + content);
      } else {
        // Same stamping as the one-line composer: in a disappearing-messages
        // channel this message must expire like any other.
        await api.sendMessage(S.activeChannelId, stampEphemeral(S.activeChannelId, content), "");
      }
      onSent?.();
      onClose();
    } catch (err) {
      flash(err);
      busy = false;
    }
  }
</script>

<Modal title={editId ? "Edit message" : "Advanced composer"} size="xl" {onClose}>
  <div class="ac">
    <!-- LEFT: the editor -->
    <section class="pane editor">
      <div class="toolbar" role="toolbar" aria-label="Formatting">
        <div class="tgroup">
          {#each TOOLS as t (t.icon)}
            <button type="button" class="tb" title={t.title} aria-label={t.title} onmousedown={noSelect} onclick={t.run}>
              <Icon name={t.icon} size={16} />
            </button>
          {/each}
        </div>
        <span class="tsep"></span>
        <div class="tgroup">
          {#each TOOLS2 as t (t.icon)}
            <button type="button" class="tb" title={t.title} aria-label={t.title} onmousedown={noSelect} onclick={t.run}>
              <Icon name={t.icon} size={16} />
            </button>
          {/each}
        </div>
        <span class="tsep"></span>
        <div class="swatches" role="group" aria-label="Text colour">
          {#each PALETTE as [name, hex] (name)}
            <button
              type="button"
              class="sw"
              style="--sw:{hex}"
              title={`Colour: ${name}`}
              aria-label={`Colour ${name}`}
              onmousedown={noSelect}
              onclick={() => applyColor(name)}
            ></button>
          {/each}
        </div>
      </div>

      <div class="surface">
        <textarea
          bind:this={ta}
          bind:value={body}
          class="draft"
          placeholder="Write your message…&#10;&#10;Select text, then tap a tool or a colour. Markdown works too: **bold**, *italic*, > quotes, - lists, ## headings, `code`, ||spoilers||."
        ></textarea>
      </div>

      <div class="belowbar">
        <button type="button" class="embed-toggle" class:on={embedOn} onclick={() => (embedOn = !embedOn)}>
          <Icon name={embedOn ? "close" : "plus"} size={14} />
          {embedOn ? "Remove rich embed" : "Add a rich embed"}
        </button>
        <span class="hint">Markdown supported · {body.length} chars</span>
      </div>

      {#if embedOn}
        <div class="embed-builder">
          <div class="row">
            <label class="color-field" title="Embed accent colour">
              <span class="clabel">Accent</span>
              <input type="color" bind:value={embed.color} />
            </label>
            <input class="grow" maxlength="200" placeholder="Embed title" bind:value={embed.title} />
          </div>
          <textarea class="edesc" rows="2" maxlength="2000" placeholder="Embed description (markdown supported)" bind:value={embed.desc}></textarea>
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
    </section>

    <!-- RIGHT: the live preview -->
    <aside class="pane preview">
      <div class="p-label"><span class="dot"></span> Live preview</div>
      <div class="p-body">
        {#if body.trim() || previewEmbed}
          <div class="pmsg">
            <div class="pav">{meInitial}</div>
            <div class="pbody">
              <div class="phead"><span class="pname">{S.displayName || "You"}</span><span class="ptime">now</span></div>
              {#if body.trim()}
                <!-- eslint-disable-next-line svelte/no-at-html-tags -->
                <div class="md">{@html renderMarkdown(body, [], cemoji)}</div>
              {/if}
              {#if previewEmbed}
                <EmbedView embed={previewEmbed} customEmoji={cemoji} />
              {/if}
            </div>
          </div>
        {:else}
          <p class="empty">Your message will appear here, exactly as others will see it.</p>
        {/if}
      </div>
    </aside>
  </div>

  <div class="actions">
    <button type="button" class="ghost" onclick={onClose}>Cancel</button>
    <button type="button" onclick={post} disabled={!canPost}>{editId ? "Save changes" : "Send message"}</button>
  </div>
</Modal>

<style>
  .ac {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 16px;
    text-align: left;
  }
  .pane {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .editor {
    flex: 1.12;
  }
  .preview {
    flex: 0.88;
  }
  @media (max-width: 760px) {
    .ac {
      flex-direction: column;
    }
    /* The 1.12/0.88 split carries a flex-basis of 0 into the column axis, so
       both panes were sized off the sheet's height rather than their content:
       the editor's toolbar + surface + footer overran the preview by 40px at
       390px and by 378px with the keyboard up. Let them be their own height
       and let the sheet scroll. */
    .editor,
    .preview {
      flex: none;
    }
    .preview {
      min-height: 180px;
    }
  }

  /* ---- toolbar ---- */
  .toolbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 3px;
    padding: 6px;
    margin-bottom: 10px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .tgroup {
    display: flex;
    gap: 2px;
  }
  .tb {
    width: 32px;
    height: 32px;
    display: grid;
    place-items: center;
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    transition: background 0.12s ease, color 0.12s ease;
  }
  .tb:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .tsep {
    width: 1px;
    height: 20px;
    background: var(--border);
    margin: 0 5px;
  }
  .swatches {
    display: flex;
    gap: 4px;
  }
  /* Descendant selector on purpose: Modal's mobile sheet puts a 44px
     min-height on `.dialog :global(button)`, which outranks a bare `.sw` — and
     a colour swatch is a dot, so 19×44 renders as an ellipse. */
  .swatches .sw {
    min-height: 19px;
  }
  .sw {
    width: 19px;
    height: 19px;
    padding: 0;
    border-radius: 50%;
    background: var(--sw);
    box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.28);
    transition: transform 0.1s ease;
  }
  .sw:hover {
    transform: scale(1.22);
  }

  /* ---- the writing surface (the star) ---- */
  .surface {
    flex: 1;
    min-height: 180px;
    display: flex;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    transition: border-color 0.15s ease, box-shadow 0.15s ease;
  }
  .surface:focus-within {
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent);
  }
  .draft {
    flex: 1;
    width: 100%;
    resize: none;
    background: transparent !important;
    border: none !important;
    box-shadow: none !important;
    outline: none !important;
    padding: 16px 18px;
    font-family: inherit;
    font-size: 15px;
    line-height: 1.75;
    color: var(--text);
  }

  .belowbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-top: 10px;
  }
  .hint {
    font-size: 11px;
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  /* Readable at rest: a dashed accent frame with neutral label + accent glyph. */
  .embed-toggle {
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
    transition: background 0.12s ease, border-color 0.12s ease;
  }
  .embed-toggle :global(svg) {
    color: var(--accent-hover);
  }
  .embed-toggle:hover {
    background: var(--accent-soft);
    border-color: var(--accent);
  }

  .embed-builder {
    margin-top: 10px;
    max-height: 240px;
    overflow-y: auto;
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
    color: var(--danger-text);
    background: color-mix(in srgb, var(--danger) 14%, transparent);
  }
  .add-field {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 5px 9px;
    font-size: 12px;
    color: var(--accent-hover);
    background: transparent;
    border-radius: var(--radius-sm);
  }
  .add-field:hover {
    background: var(--accent-soft);
  }

  /* ---- preview ---- */
  .p-label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
    margin-bottom: 8px;
  }
  .p-label .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--ok, #3ba55d);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--ok) 22%, transparent);
  }
  .p-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 14px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .pmsg {
    display: flex;
    gap: 11px;
  }
  .pav {
    width: 38px;
    height: 38px;
    flex-shrink: 0;
    border-radius: 50%;
    display: grid;
    place-items: center;
    font-weight: 700;
    font-size: 14px;
    color: var(--accent-hover);
    background: var(--accent-soft);
  }
  .pbody {
    min-width: 0;
    flex: 1;
  }
  .phead {
    display: flex;
    align-items: baseline;
    gap: 7px;
    margin-bottom: 2px;
  }
  .pname {
    font-weight: 600;
    font-size: 14px;
  }
  .ptime {
    font-size: 11px;
    color: var(--text-muted);
  }
  .md {
    font-size: 14px;
    line-height: 1.5;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
  }
  /* Size Twemoji/custom-emoji images to the text, exactly like the feed —
     without this they render at their full SVG size. */
  .md :global(img.emoji) {
    width: 1.375em;
    height: 1.375em;
    vertical-align: -0.3em;
    margin: 0 0.5px;
    object-fit: contain;
  }
  .md :global(img.cemoji) {
    height: 1.375em;
    width: auto;
    vertical-align: -0.2em;
    margin: 0 1px;
    object-fit: contain;
  }
  .empty {
    color: var(--text-muted);
    font-size: 13px;
    font-style: italic;
    margin: 4px 0 0;
  }

  /* ---- actions ---- */
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding-top: 14px;
    margin-top: 2px;
    border-top: 1px solid var(--border);
  }
</style>
