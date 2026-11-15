<script>
  // The lock screen's one way to say yes to erasing this device: the word
  // typed out, then a danger button that stays disabled until it is. Starting
  // over and letting a phrase or a link code replace the account here both
  // use it, so erasing by either route takes the same. The safe choice is the
  // caller's, placed before this; the button is not full width.
  import { ERASE_WORD, eraseWordMatches } from "./lib/eraseword.js";

  let {
    typed = $bindable(""),
    label,
    busy = false,
    ready = true, // anything else the caller needs first (a new link code)
    onconfirm,
  } = $props();

  const matches = $derived(eraseWordMatches(typed));
  const go = () => {
    if (matches && ready && !busy) onconfirm?.();
  };
</script>

<p class="muted erase-prompt">Type <b>{ERASE_WORD}</b> to confirm.</p>
<input
  type="text"
  bind:value={typed}
  autocomplete="off"
  autocapitalize="off"
  spellcheck="false"
  aria-label="Type {ERASE_WORD} to confirm"
  placeholder="Type the word above"
  disabled={busy}
  onkeydown={(e) => {
    // Enter here is this confirmation's, never the form's: the form's
    // submit would try to unlock or create with the passphrase fields.
    if (e.key === "Enter") {
      e.preventDefault();
      go();
    }
  }}
/>
<button type="button" class="danger lg erase-go" disabled={busy || !matches || !ready} onclick={go}>
  {busy ? "Erasing…" : label}
</button>

<style>
  /* The lock screen's paragraph size (Login.svelte's p), which does not
     reach into this component. */
  .erase-prompt {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.5;
  }
  .erase-go {
    align-self: flex-start;
    max-width: 100%;
    white-space: normal;
    text-align: center;
  }
  @media (max-width: 768px) {
    .erase-go {
      min-height: 48px;
    }
  }
</style>
