<script>
  // The markdown formatting bar, shared by the composer and the edit box.
  //
  // It used to be markup inside Composer.svelte, which is why editing — the one
  // place you go to FIX formatting — was the one place with no formatting
  // controls at all. It is a component now, and it takes the textarea it works
  // on as a prop, so the same bar and the same chords serve whichever field has
  // the caret.
  //
  // It is also a real toolbar. `role="toolbar"` was already here, announcing a
  // keyboard contract the bar did not implement: eight separate tab stops
  // instead of one, and no ←/→ between them. Announcing a role without its
  // behaviour is worse than not announcing it, so `use:roving` supplies the
  // other half of that promise — Tab reaches the bar once, arrows walk it, Tab
  // leaves. The composer's icon cluster wears the same action.
  import Icon from "./Icon.svelte";
  import { tooltip } from "./lib/tooltip.js";
  import { FMT_GROUPS } from "./lib/mdformat.js";
  import { roving } from "./lib/roving.js";

  let {
    onFormat,
    disabled = false,
    label = "Text formatting",
    // Composer-only: the draft's direction override, rendered as the last
    // control in the bar. Left undefined the bar has no direction button.
    dirMode = undefined,
    onCycleDir = undefined,
    compact = false,
  } = $props();

  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent || "");
  const MOD_LABEL = isMac ? "⌘" : "Ctrl+";
  const fmtTitle = (b) => (b.keys ? `${b.label} (${MOD_LABEL}${b.keys})` : b.label);

  const dirLabel = $derived(
    dirMode === "rtl" ? "رل" : dirMode === "ltr" ? "LR" : "⇄",
  );
  // What the control actually does. It sets the paragraph's BASE direction,
  // which decides the order of a mixed line — where the trailing English clause
  // and the sentence-ending punctuation of "مرحبا CI build رقم 42 failed" sit.
  // It does not move the paragraph to the other edge of the pane: alignment is
  // the app's, in every language (see the bidi block in app.css), and saying
  // "right to left" while nothing visibly moves for ordinary Arabic prose was
  // promising the wrong thing.
  const dirWord = $derived(
    dirMode === "rtl" ? "right to left" : dirMode === "ltr" ? "left to right" : "per line",
  );
  const dirHint = $derived(
    dirMode ? `reads ${dirWord}` : "each line reads whichever way it is written",
  );
</script>

<div class="fmt-bar" class:compact role="toolbar" aria-label={label} use:roving>
  {#each FMT_GROUPS as group, gi (gi)}
    {#if gi > 0}<span class="fmt-sep" aria-hidden="true"></span>{/if}
    {#each group as b (b.kind)}
      <button
        type="button"
        class="fmtbtn"
        use:tooltip={{ text: fmtTitle(b) }}
        aria-label={b.label}
        {disabled}
        onmousedown={(e) => e.preventDefault()}
        onclick={() => onFormat(b.kind)}
      >
        <Icon name={b.kind} size={15} />
      </button>
    {/each}
  {/each}
  {#if onCycleDir}
    <span class="fmt-sep" aria-hidden="true"></span>
    <!-- The direction control was a chord and nothing else: no button, no hint,
         and the pill that shows the current mode only appeared AFTER you had
         already pressed the chord that reveals it. For a product that bundles
         three Arabic faces and ships an Arabic channel, the only discovery path
         for bidi being the cheat sheet is the wrong trade. It costs everyone
         else one glyph, behind the separator the bar already had. -->
    <button
      type="button"
      class="fmtbtn dirbtn"
      class:on={!!dirMode}
      use:tooltip={{ text: `Base direction: ${dirHint} — Ctrl+Shift+L, or click` }}
      aria-label="Base direction: {dirWord}. Activate to change."
      {disabled}
      onmousedown={(e) => e.preventDefault()}
      onclick={onCycleDir}
    >
      {dirLabel}
    </button>
  {/if}
</div>

<style>
  .fmt-bar {
    display: flex;
    align-items: center;
    gap: 1px;
    padding: 5px 8px 4px;
    /* Quiet until the composer is hovered or focused, but not invisible: at
       0.45 these icons sat at 1.9:1 against the input they float over. */
    opacity: 0.8;
    transition: opacity var(--dur-standard) ease;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
  }
  .fmt-bar.compact {
    padding: 2px 2px 3px;
    border-bottom: none;
    opacity: 0.85;
  }
  :global(.composer:hover) .fmt-bar,
  :global(.composer:focus-within) .fmt-bar,
  .fmt-bar:focus-within {
    opacity: 1;
  }
  .fmtbtn {
    display: grid;
    place-items: center;
    width: 26px;
    height: 22px;
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    transition:
      background var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  .fmtbtn:hover:not(:disabled) {
    background: var(--bg-3);
    color: var(--text);
  }
  .fmtbtn:active:not(:disabled) {
    background: var(--bg-3);
  }
  .fmtbtn:disabled {
    opacity: 0.35;
  }
  .dirbtn {
    width: auto;
    min-width: 26px;
    padding: 0 5px;
    font-size: var(--fs-tiny);
    font-weight: 700;
    letter-spacing: 0.02em;
  }
  .dirbtn.on {
    color: var(--accent);
    background: color-mix(in srgb, var(--accent) 14%, transparent);
  }
  .fmt-sep {
    width: 1px;
    height: 14px;
    background: var(--border);
    margin: 0 5px;
    flex: none;
  }
</style>
