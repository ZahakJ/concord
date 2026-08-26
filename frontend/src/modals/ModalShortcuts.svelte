<script>
  import Modal from "./Modal.svelte";
  import { S } from "../lib/state.svelte.js";
  import { SHORTCUTS, SHORTCUT_GROUPS } from "../lib/shortcuts.js";
  let { onClose } = $props();

  // Rendered from the keymap's own registry rather than from a copy of it. The
  // copy had gone nine bindings out of date — the member panel, deafen,
  // push-to-talk, the six composer formatting chords and the direction cycle,
  // and `?` itself, so the one sheet that exists to teach shortcuts could not
  // teach how to reopen itself.
  //
  // A phone has no Ctrl, no Alt and no Esc, so most of this describes chords
  // the device cannot produce — screens of them before reaching the part
  // (markdown, slash commands) that does apply. Show only what is typeable
  // there, and say so in the title.
  const shown = $derived(
    SHORTCUT_GROUPS.map((name) => ({
      name,
      rows: SHORTCUTS.filter((s) => s.group === name && (!S.isMobile || s.typed)),
    })).filter((g) => g.rows.length),
  );
</script>

<Modal title={S.isMobile ? "Formatting" : "Keyboard shortcuts & formatting"} wide {onClose}>
  <div class="sc">
    {#each shown as grp (grp.name)}
      <section class="sc-group">
        <h4>{grp.name}</h4>
        {#each grp.rows as row (row.label)}
          <div class="sc-row">
            <span class="sc-desc">{row.label}</span>
            <span class="sc-keys">
              <!-- Alternatives read "or"; the parts of one chord are joined by
                   "+". Printing both as "+" is what made "/shrug + /me" and
                   "Alt + \u2191" look like the same instruction. -->
              {#each row.chords as chord, ci (ci)}
                {#if ci > 0}<span class="sc-or">or</span>{/if}
                {#each chord as k, i (i)}
                  {#if i > 0}<span class="sc-plus">+</span>{/if}<kbd class:typed={row.typed}>{k}</kbd>
                {/each}
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
    font-size: var(--fs-tiny);
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
    font-size: var(--fs-ui);
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
    font-size: var(--fs-tiny);
  }
  .sc-or {
    color: var(--text-muted);
    font-size: var(--fs-tiny);
    font-style: italic;
    padding: 0 2px;
  }
  /* Proper keycaps: raised, subtle top-highlight, pressed-looking bottom edge. */
  kbd {
    font-family: ui-monospace, monospace;
    font-size: var(--fs-small);
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
  /* Markdown is typed, not pressed: leave the leading and trailing spaces of
     "> " and "# " visible, or the sheet quietly teaches the wrong thing. */
  kbd.typed {
    white-space: pre;
  }
</style>
