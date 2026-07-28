<script>
  // Toast stack (bottom-right). Renders S.toasts — pushed via flash()/toastOk()/
  // toastError() in state.svelte.js — newest at the bottom, capped at 4 visible
  // (older ones stay queued and appear as newer ones expire). Each toast is its
  // own live region so screen readers announce it: errors are assertive alerts,
  // the rest polite status. Enter/exit animations collapse to instant when the
  // user prefers reduced motion.
  import { fly } from "svelte/transition";
  import { backOut } from "svelte/easing";
  import { S, dismissToast } from "./lib/state.svelte.js";
  import Icon from "./Icon.svelte";

  const MAX_VISIBLE = 4;
  const visible = $derived(S.toasts.slice(-MAX_VISIBLE));

  const reducedMotion =
    typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
  const dur = reducedMotion ? 0 : 150;

  const ICONS = { info: "info", success: "check", error: "alert" };
  // Mirrors the auto-dismiss timers in flash() so the progress rail drains in
  // sync with the real timeout (they start within the same frame).
  const TTL = { error: 5000, success: 3000, info: 3000 };
</script>

<div class="toasts">
  {#each visible as t (t.id)}
    <div
      class="toast {t.kind}"
      role={t.kind === "error" ? "alert" : "status"}
      aria-live={t.kind === "error" ? "assertive" : "polite"}
      in:fly={{ y: 14, duration: dur * 2, easing: backOut }}
      out:fly={{ x: 24, duration: dur }}
    >
      <span class="t-icon"><Icon name={ICONS[t.kind] || "info"} size={15} /></span>
      <span class="t-text">{t.text}</span>
      <button class="t-close" onclick={() => dismissToast(t.id)} aria-label="Dismiss notification">
        <Icon name="close" size={12} />
      </button>
      {#if !reducedMotion}
        <span
          class="t-rail"
          aria-hidden="true"
          style="animation-duration: {TTL[t.kind] || 3000}ms"
        ></span>
      {/if}
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    bottom: 16px;
    right: 16px;
    /* Above the bottom sheets/scrims (400/401) so a toast fired from a sheet
       action is visible; still under the app lock (500). */
    z-index: 450;
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: min(360px, calc(100vw - 32px));
    /* The container spans the corner even when empty; only toasts take clicks. */
    pointer-events: none;
  }
  .toast {
    position: relative;
    overflow: hidden; /* clips the progress rail to the rounded corners */
    pointer-events: auto;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 8px 10px 12px;
    color: var(--text);
    border: 1px solid var(--border);
    border-left-width: 3px;
    border-radius: var(--radius-md);
    font-size: 13px;
    line-height: 1.4;
    box-shadow: var(--shadow-pop);
  }
  /* Each variant gets a whisper of its color washed into the surface, plus
     the colored edge — reads at a glance without shouting. */
  .toast.error {
    border-left-color: var(--danger);
    background: linear-gradient(90deg, color-mix(in srgb, var(--danger) 9%, var(--bg-1)), var(--bg-1) 55%);
  }
  .toast.error .t-icon {
    color: var(--danger-text);
  }
  .toast.success {
    border-left-color: var(--ok);
    background: linear-gradient(90deg, color-mix(in srgb, var(--ok) 9%, var(--bg-1)), var(--bg-1) 55%);
  }
  .toast.success .t-icon {
    color: var(--ok-text);
  }
  .toast.info {
    border-left-color: var(--accent);
    background: linear-gradient(90deg, color-mix(in srgb, var(--accent) 8%, var(--bg-1)), var(--bg-1) 55%);
  }
  .toast.info .t-icon {
    color: var(--text-muted);
  }
  /* Time-left rail along the bottom, draining toward dismissal. Duration is
     set inline to match the flash() timer for this kind. */
  .t-rail {
    position: absolute;
    left: 0;
    bottom: 0;
    height: 2px;
    width: 100%;
    transform-origin: left;
    background: color-mix(in srgb, currentColor 28%, transparent);
    animation-name: t-drain;
    animation-timing-function: linear;
    animation-fill-mode: forwards;
  }
  .toast.error .t-rail {
    color: var(--danger-text);
  }
  .toast.success .t-rail {
    color: var(--ok-text);
  }
  .toast.info .t-rail {
    color: var(--accent-hover);
  }
  @keyframes t-drain {
    to {
      transform: scaleX(0);
    }
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
  .t-close:hover {
    color: var(--text);
    background: var(--bg-3);
  }

  /* ---- touch adjustments: top-center, below the mobile top bar, so toasts
     never cover the composer or hide behind the keyboard. ---- */
  @media (pointer: coarse) {
    .toasts {
      bottom: auto;
      top: calc(62px + env(safe-area-inset-top));
      right: 50%;
      transform: translateX(50%);
      width: min(420px, calc(100vw - 24px));
    }
    .toast {
      font-size: 14px;
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
