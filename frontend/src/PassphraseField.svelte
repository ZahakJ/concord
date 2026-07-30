<script>
  // A passphrase input with a show/hide toggle.
  //
  // Worth having on every one of these fields, not just the unlock: a Concord
  // passphrase is long, typed blind, and unrecoverable — there's no reset link
  // behind it. Being able to check what you typed before committing to it is
  // the difference between a typo you catch and an account you can't open.
  //
  // The toggle is a button, not a checkbox, so it never lands in the tab order
  // between the field and the submit button.
  import Icon from "./Icon.svelte";

  let {
    value = $bindable(""),
    placeholder = "Passphrase",
    autofocus = false,
    autocomplete = "current-password",
    onkeydown = undefined,
  } = $props();

  let shown = $state(false);
  let input = $state(null);

  function toggle() {
    // Keep the caret where it was: swapping the type moves focus off the field
    // in some engines, which is a jarring way to lose your place mid-passphrase.
    const pos = input?.selectionStart ?? null;
    shown = !shown;
    requestAnimationFrame(() => {
      input?.focus();
      if (pos !== null) input?.setSelectionRange?.(pos, pos);
    });
  }
</script>

<div class="wrap">
  <!-- svelte-ignore a11y_autofocus -->
  <input
    bind:this={input}
    type={shown ? "text" : "password"}
    {placeholder}
    {autofocus}
    {autocomplete}
    {onkeydown}
    bind:value
  />
  <button
    type="button"
    class="peek"
    onclick={toggle}
    aria-pressed={shown}
    aria-label={shown ? "Hide passphrase" : "Show passphrase"}
    title={shown ? "Hide passphrase" : "Show passphrase"}
    tabindex="-1"
  >
    <Icon name={shown ? "eyeOff" : "eye"} size={16} />
  </button>
</div>

<style>
  .wrap {
    position: relative;
    display: flex;
  }
  .wrap input {
    width: 100%;
    /* Room for the button, so a long passphrase never slides under it. */
    padding-right: 40px;
  }
  .peek {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: 38px;
    display: grid;
    place-items: center;
    padding: 0;
    background: transparent;
    border: none;
    color: var(--text-faint);
    transition: color 0.14s ease;
  }
  @media (pointer: fine) {
    .peek:hover {
      color: var(--text);
    }
  }
  .peek:active {
    color: var(--text);
  }
  /* A passphrase you can't re-read is a passphrase you can't recover from a
     typo in, so this toggle carries real weight on the one screen a phone user
     may never get past. It stretches to the field's height already; the width
     was 38px, just under the target. */
  @media (pointer: coarse), (max-width: 768px) {
    .peek {
      width: 46px;
    }
    .wrap input {
      padding-right: 48px;
    }
  }
  .peek[aria-pressed="true"] {
    color: var(--accent-hover);
  }
  /* The field owns the focus ring; the button inside it shouldn't draw a
     second one on top. */
  .peek:focus-visible {
    outline: none;
    color: var(--accent-hover);
  }
  @media (prefers-reduced-motion: reduce) {
    .peek {
      transition: none;
    }
  }
</style>
