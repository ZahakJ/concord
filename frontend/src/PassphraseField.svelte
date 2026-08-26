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
  import { strength } from "./lib/passphrase.js";

  let {
    value = $bindable(""),
    placeholder = "Passphrase",
    autofocus = false,
    autocomplete = "current-password",
    onkeydown = undefined,
    // The last attempt with this field's contents failed. Marked for assistive
    // tech as well as painted: a red sentence under the card said nothing at
    // all about WHICH field a screen-reader user had to go back to.
    invalid = false,
    // Show the strength readout. Opt-in, and set only on the fields that CHOOSE
    // a passphrase — never on an unlock (where it would be scoring a
    // passphrase the person already has and cannot change from there) and never
    // on a Confirm field (where it would score the same string twice).
    //
    // It gates nothing. There is deliberately no minimum length anywhere in
    // this app: one imposed now could not be applied to the accounts that
    // already exist, and refusing to open an account whose passphrase the owner
    // chose last year is a worse outcome than a weak passphrase.
    meter = false,
  } = $props();

  const score = $derived(meter ? strength(value) : null);

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
    aria-label={placeholder}
    {autofocus}
    {autocomplete}
    {onkeydown}
    aria-invalid={invalid ? "true" : undefined}
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
{#if score?.label}
  <div class="strength" data-level={score.level}>
    <div class="bar" aria-hidden="true"><span style:width="{score.percent}%"></span></div>
    <!-- Spoken as well as drawn, and politely: it changes on every keystroke,
         so an assertive region would interrupt the typing it is describing. -->
    <span class="slabel" role="status" aria-live="polite">{score.label}</span>
  </div>
{/if}

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
  .wrap input[aria-invalid] {
    border-color: var(--danger);
  }
  .wrap input[aria-invalid]:focus {
    border-color: var(--danger);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--danger) 16%, transparent);
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
    .strength .bar span {
      transition: none;
    }
  }
  /* ---- strength readout ----
     A bar and a sentence. The sentence is the part that does the work: "Fair"
     on its own is a grade, and a grade invites arguing with it, whereas
     "fine unless someone is really trying" says what it actually means. */
  .strength {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 6px;
  }
  .bar {
    flex: 1;
    height: 4px;
    min-width: 60px;
    border-radius: 999px;
    background: var(--bg-3);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    border-radius: 999px;
    background: var(--danger);
    transition:
      width 0.18s ease,
      background 0.18s ease;
  }
  .slabel {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    text-align: right;
  }
  /* Tokens, so the six light packs get a readable bar without their own rules;
     --warn is the same amber the mention accent uses, and --ok the same green
     as every other "this is fine" in the app. */
  .strength[data-level="2"] .bar span {
    background: var(--warn);
  }
  .strength[data-level="3"] .bar span,
  .strength[data-level="4"] .bar span {
    background: var(--ok);
  }
  .strength[data-level="3"] .slabel,
  .strength[data-level="4"] .slabel {
    color: var(--ok-text);
  }
</style>
