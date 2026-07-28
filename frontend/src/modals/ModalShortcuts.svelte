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
  <div class="sc">
    {#each groups as grp (grp.name)}
      <section class="sc-group">
        <h4>{grp.name}</h4>
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
      </section>
    {/each}
  </div>
</Modal>

<style>
  /* Single column: descriptions are full sentences, so two narrow columns
     forced nowrap keycaps to overflow the modal (horizontal scroll). One wide
     column reads cleanly and can never scroll sideways. */
  .sc {
    display: flex;
    flex-direction: column;
    gap: 18px;
    max-width: 100%;
  }
  .sc-group h4 {
    margin: 0 0 4px;
    font-size: 10.5px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--accent-hover);
  }
  .sc-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    padding: 7px 0;
    font-size: 13px;
    border-top: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
  }
  .sc-group h4 + .sc-row {
    border-top: none;
  }
  .sc-desc {
    color: var(--text);
    min-width: 0; /* allow long descriptions to wrap instead of pushing wide */
  }
  .sc-keys {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .sc-plus {
    color: var(--text-faint);
    font-size: 10px;
  }
  /* Proper keycaps: raised, subtle top-highlight, pressed-looking bottom edge. */
  kbd {
    font-family: ui-monospace, monospace;
    font-size: 11.5px;
    font-weight: 600;
    min-width: 22px;
    text-align: center;
    background: linear-gradient(var(--bg-3), var(--bg-2));
    border: 1px solid var(--border);
    border-bottom: 2px solid color-mix(in srgb, var(--border) 70%, black);
    border-radius: 5px;
    box-shadow:
      inset 0 1px 0 color-mix(in srgb, var(--text) 8%, transparent),
      0 1px 1px rgb(0 0 0 / 0.12);
    padding: 2px 7px;
    color: var(--text);
    white-space: nowrap;
  }
</style>
