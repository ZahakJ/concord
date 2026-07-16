<script>
  import Modal from "./Modal.svelte";
  let { onClose } = $props();

  const groups = [
    {
      name: "Navigation",
      keys: [
        [["Ctrl/⌘", "K"], "Command palette (jump anywhere, run actions)"],
        [["Ctrl/⌘", "F"], "Search messages"],
        [["Ctrl/⌘", ","], "User settings"],
        [["Alt", "↑/↓"], "Previous / next channel"],
        [["Alt", "Shift", "↑/↓"], "Previous / next unread channel"],
        [["Ctrl", "Alt", "↑/↓"], "Previous / next server"],
      ],
    },
    {
      name: "Reading",
      keys: [
        [["Esc"], "Close what's open — or mark this channel read"],
        [["Shift", "Esc"], "Mark all channels read"],
      ],
    },
    {
      name: "Voice",
      keys: [[["Ctrl", "Shift", "M"], "Toggle mute (while in a call)"]],
    },
    {
      name: "Composer",
      keys: [
        [["Enter"], "Send message"],
        [["Shift", "Enter"], "New line"],
        [["↑"], "Edit your last message (empty composer)"],
        [["/shrug", "/me", "/spoiler", "…"], "Slash commands"],
      ],
    },
    {
      name: "Formatting",
      keys: [
        [["**text**"], "Bold"],
        [["*text*"], "Italic"],
        [["__text__"], "Underline"],
        [["~~text~~"], "Strikethrough"],
        [["||text||"], "Spoiler"],
        [["`code`", "```block```"], "Code"],
        [["> ", ">>> "], "Quote (line / rest of message)"],
        [["# ", "## ", "### "], "Headers"],
        [["[text](url)"], "Masked link"],
      ],
    },
  ];
</script>

<Modal title="Keyboard shortcuts & formatting" wide {onClose}>
  <div class="sc-grid">
    {#each groups as grp (grp.name)}
      <section class="sc-group">
        <h4>{grp.name}</h4>
        <div class="sc-rows">
          {#each grp.keys as [keys, desc] (desc)}
            <div class="sc-row">
              <span class="sc-desc">{desc}</span>
              <span class="sc-keys">
                {#each keys as k, i (k)}
                  {#if i > 0}<span class="sc-plus">+</span>{/if}<kbd>{k}</kbd>
                {/each}
              </span>
            </div>
          {/each}
        </div>
      </section>
    {/each}
  </div>
</Modal>

<style>
  .sc-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
  }
  .sc-group {
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 12px 14px;
  }
  .sc-group h4 {
    margin: 0 0 8px;
    font-size: 10.5px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--accent);
  }
  .sc-rows {
    display: flex;
    flex-direction: column;
  }
  .sc-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 5px 0;
    font-size: 12.5px;
    border-top: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
  }
  .sc-row:first-child {
    border-top: none;
  }
  .sc-desc {
    color: var(--text);
    min-width: 0;
  }
  .sc-keys {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .sc-plus {
    color: var(--text-faint);
    font-size: 10px;
  }
  /* Proper keycaps: raised, subtle top-highlight, pressed-looking bottom edge. */
  kbd {
    font-family: ui-monospace, monospace;
    font-size: 11px;
    font-weight: 600;
    min-width: 20px;
    text-align: center;
    background: linear-gradient(var(--bg-3), var(--bg-2));
    border: 1px solid var(--border);
    border-bottom: 2px solid color-mix(in srgb, var(--border) 70%, black);
    border-radius: 5px;
    box-shadow:
      inset 0 1px 0 color-mix(in srgb, var(--text) 8%, transparent),
      0 1px 1px rgb(0 0 0 / 0.12);
    padding: 2px 6px;
    color: var(--text);
    white-space: nowrap;
  }
  @media (max-width: 640px) {
    .sc-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
