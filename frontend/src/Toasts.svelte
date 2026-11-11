<script>
  // Toast stack (bottom-right). Renders S.toasts — pushed via flash()/toastOk()/
  // toastError() in state.svelte.js — newest at the bottom, capped at 3 visible
  // (older ones stay queued and appear as newer ones expire). One surface for
  // every kind: the lifted ground, a hairline and the float shadow, with the
  // kind carried by the icon's ink alone — no coloured edge, no wash, no
  // draining rail. Enter/exit animations collapse to instant when the user
  // prefers reduced motion.
  import { fly } from "svelte/transition";
  import { cubicOut } from "svelte/easing";
  import { S, dismissToast } from "./lib/state.svelte.js";
  import Icon from "./Icon.svelte";

  const MAX_VISIBLE = 3;
  const visible = $derived(S.toasts.slice(-MAX_VISIBLE));

  const reducedMotion =
    typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
  const dur = reducedMotion ? 0 : 150;

  const ICONS = { info: "info", success: "check", error: "alert" };

  // Announcing. Each toast used to be its own role=status/alert element,
  // created with its text already inside it — and a live region that arrives
  // already holding its message is one screen readers routinely never read.
  // Two regions now stay mounted for the life of the app, empty, and a new
  // toast's text is written INTO them: a change to a region that was already
  // there is the case every screen reader announces. Cleared first and filled
  // a frame later so the same message twice is still two announcements.
  let polite = $state("");
  let urgent = $state("");
  let said = 0;
  $effect(() => {
    const fresh = S.toasts.filter((t) => t.id > said);
    if (!fresh.length) return;
    said = Math.max(...fresh.map((t) => t.id));
    const last = fresh[fresh.length - 1];
    const err = last.kind === "error";
    if (err) urgent = "";
    else polite = "";
    requestAnimationFrame(() => {
      if (err) urgent = last.text;
      else polite = last.text;
    });
  });
</script>

<div class="sr-only" role="status" aria-live="polite" aria-atomic="true">{polite}</div>
<div class="sr-only" role="alert" aria-live="assertive" aria-atomic="true">{urgent}</div>

<div class="toasts">
  {#each visible as t (t.id)}
    <div
      class="toast {t.kind}"
      in:fly={{ y: 8, duration: dur, easing: cubicOut }}
      out:fly={{ y: 4, duration: dur * 0.8 }}
    >
      <span class="t-icon"><Icon name={ICONS[t.kind] || "info"} size={16} /></span>
      <span class="t-text">{t.text}</span>
      {#if t.action}
        <button
          class="t-action"
          onclick={() => {
            dismissToast(t.id);
            t.action.run();
          }}>{t.action.label}</button
        >
      {/if}
      <button class="t-close" onclick={() => dismissToast(t.id)} aria-label="Dismiss notification">
        <Icon name="close" size={12} />
      </button>
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    /* Clear of the composer, which is what the bottom-right corner belongs to:
       a 16px offset put every toast on the composer's icon row. --composer-h is
       published by Composer.svelte and unset wherever there is no composer (a
       forum board, the empty state), which is what the fallback covers. */
    bottom: calc(var(--composer-h, 0px) + var(--sp-5));
    right: var(--sp-5);
    /* Above the bottom sheets/scrims (400/401) so a toast fired from a sheet
       action is visible; still under the app lock (500). */
    z-index: var(--z-toast);
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    width: min(360px, calc(100 * var(--vw) - 32px));
    /* The container spans the corner even when empty; only toasts take clicks. */
    pointer-events: none;
  }
  .toast {
    position: relative;
    pointer-events: auto;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 8px 10px 12px;
    color: var(--text);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    font-size: var(--fs-compact);
    line-height: 1.4;
    box-shadow: var(--shadow-pop);
  }
  .toast.error .t-icon {
    color: var(--danger-text);
  }
  .toast.success .t-icon {
    color: var(--ok-text);
  }
  .toast.info .t-icon {
    color: var(--text-muted);
  }
  .t-icon {
    flex-shrink: 0;
    display: flex;
    margin-top: 1px;
  }
  .t-text {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .t-action {
    flex-shrink: 0;
    align-self: center;
    padding: var(--sp-1) var(--sp-2);
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--accent);
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  @media (pointer: fine) {
    .t-action:hover {
      background: var(--bg-3);
    }
  }
  .t-close {
    flex-shrink: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 20px;
    height: 20px;
    padding: 0;
    border: none;
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
  }
  @media (pointer: fine) {
    .t-close:hover {
      color: var(--text);
      background: var(--bg-3);
    }
  }
  .t-close:active {
    color: var(--text);
    background: var(--bg-3);
  }

  /* ---- touch adjustments: top-center, below the mobile top bar, so toasts
     never cover the composer or hide behind the keyboard. ---- */
  @media (max-width: 768px) {
    .toasts {
      bottom: auto;
      /* Anchored to the shell's chrome, not to a number. The hardcoded 62px was
         measured against the bare top bar, so the moment the connection bar
         appeared — "Catching up…", "Offline — reconnecting…", the two most
         common phone states — a toast was drawn straight over it, and over the
         search row underneath that. --mchrome is declared in app.css and kept
         up to date by MobileShell, which knows how tall its own chrome is. */
      top: calc(var(--mchrome) + 10px);
      right: 50%;
      transform: translateX(50%);
      width: min(420px, calc(100 * var(--vw) - 24px));
    }
    .toast {
      font-size: var(--fs-ui);
    }
    /* Toasts are short-lived and stacked, so the glyph stays 32px and an
       invisible overlay carries the tap area to 44px. */
    .t-close {
      width: 32px;
      height: 32px;
      position: relative;
    }
    .t-close::after {
      content: "";
      position: absolute;
      inset: -6px;
    }
  }
</style>
