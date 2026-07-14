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

<Modal title="Keyboard shortcuts & formatting" {onClose}>
  <div class="sc-grid">
    {#each groups as grp (grp.name)}
      <div class="sc-group">
        <h4>{grp.name}</h4>
        {#each grp.keys as [keys, desc] (desc)}
          <div class="sc-row">
            <span class="sc-keys">
              {#each keys as k, i (k)}
                {#if i > 0}<span class="sc-plus">+</span>{/if}<kbd>{k}</kbd>
              {/each}
            </span>
            <span class="sc-desc">{desc}</span>
          </div>
        {/each}
      </div>
    {/each}
  </div>
</Modal>

<style>
  .sc-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 14px 24px;
  }
  .sc-group h4 {
    margin: 0 0 6px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--text-muted);
  }
  .sc-row {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 5px;
    font-size: 12px;
  }
  .sc-keys {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 3px;
    min-width: 96px;
  }
  .sc-plus {
    color: var(--text-faint);
    font-size: 10px;
  }
  kbd {
    font-family: ui-monospace, monospace;
    font-size: 11px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    border-bottom-width: 2px;
    border-radius: 4px;
    padding: 1px 5px;
    color: var(--text);
    white-space: nowrap;
  }
  .sc-desc {
    color: var(--text-muted);
  }
  @media (max-width: 640px) {
    .sc-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
