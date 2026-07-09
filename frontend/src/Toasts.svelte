<script>
  // Toast stack (bottom-right). Renders S.toasts — pushed via flash()/toastOk()/
  // toastError() in state.svelte.js — newest at the bottom, capped at 4 visible
  // (older ones stay queued and appear as newer ones expire). Each toast is its
  // own live region so screen readers announce it: errors are assertive alerts,
  // the rest polite status. Enter/exit animations collapse to instant when the
  // user prefers reduced motion.
  import { fly } from "svelte/transition";
  import { S, dismissToast } from "./lib/state.svelte.js";
  import Icon from "./Icon.svelte";

  const MAX_VISIBLE = 4;
  const visible = $derived(S.toasts.slice(-MAX_VISIBLE));

  const reducedMotion =
    typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
  const dur = reducedMotion ? 0 : 150;

  const ICONS = { info: "info", success: "check", error: "alert" };
</script>

<div class="toasts">
  {#each visible as t (t.id)}
    <div
      class="toast {t.kind}"
      role={t.kind === "error" ? "alert" : "status"}
      aria-live={t.kind === "error" ? "assertive" : "polite"}
      transition:fly={{ y: 8, duration: dur }}
    >
      <span class="t-icon"><Icon name={ICONS[t.kind] || "info"} size={15} /></span>
      <span class="t-text">{t.text}</span>
      <button class="t-close" onclick={() => dismissToast(t.id)} aria-label="Dismiss notification">
        <Icon name="close" size={12} />
      </button>
    </div>
  {/each}
</div>

<style>
  .toasts {
    position: fixed;
    bottom: 16px;
    right: 16px;
    z-index: 210;
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: min(360px, calc(100vw - 32px));
    /* The container spans the corner even when empty; only toasts take clicks. */
    pointer-events: none;
  }
  .toast {
    pointer-events: auto;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 8px 10px 12px;
    background: var(--bg-1);
    color: var(--text);
    border: 1px solid var(--border);
    border-left-width: 3px;
    border-radius: var(--radius-md);
    font-size: 13px;
    line-height: 1.4;
    box-shadow: var(--shadow-pop);
  }
  .toast.error {
    border-left-color: var(--danger);
  }
  .toast.error .t-icon {
    color: var(--danger);
  }
  .toast.success {
    border-left-color: var(--ok);
  }
  .toast.success .t-icon {
    color: var(--ok);
  }
  .toast.info {
    border-left-color: var(--accent);
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
</style>
