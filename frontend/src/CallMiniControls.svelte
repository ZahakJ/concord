<script>
  // Mute, over the top of whatever dialog is open.
  //
  // Opening Voice & video during a call puts a 1000x660 settings dialog with
  // the whole left rail — Account, Appearance, Notifications, Privacy,
  // Bookings, Connection, Link a device — over the entire stage. Hit-tested,
  // every one of the thirteen call controls underneath it came back
  // `reachable: false`: the sidebar bar behind `.overlay`, the stage bar behind
  // the dialog. Ctrl+Shift+M still worked, so the call was not unmutable — it
  // was unmutable with a mouse, at the exact moment somebody says your name,
  // opened from the one dialog you go to BECAUSE of your microphone.
  //
  // A mute row inside the Voice & video panel would answer that one dialog. It
  // is not that dialog's fault: every modal in the app covers the call, and the
  // rule "mute is reachable from anywhere" is the whole reason there are four
  // call bars. So this is the fifth, at its smallest — the same .callbtn
  // language, the same order, above the scrim.
  import Icon from "./Icon.svelte";
  import { S } from "./lib/state.svelte.js";
  import { tooltip } from "./lib/tooltip.js";
  import { haptic } from "./lib/touch.js";

  let { onToggleMute, onLeave } = $props();

  const tap = (fn, style = "light") => () => {
    haptic(style);
    fn?.();
  };
</script>

<div class="callmini" role="group" aria-label="Call controls">
  <span class="cm-live" aria-hidden="true"></span>
  <button
    class="callbtn sm cut"
    class:on={S.muted}
    use:tooltip
    aria-label={S.muted ? "Unmute" : "Mute"}
    aria-pressed={S.muted}
    onclick={tap(onToggleMute)}
  >
    <Icon name={S.muted ? "micOff" : "mic"} size={14} />
  </button>
  <button class="callbtn sm hang" use:tooltip aria-label="Leave call" onclick={tap(onLeave, "heavy")}>
    <Icon name="door" size={14} />
  </button>
</div>

<style>
  .callmini {
    position: fixed;
    /* Above the modal scrim (100), below the context menu's backdrop (400) and
       the weather (420) — it is chrome for the call, not a layer of its own. */
    z-index: 105;
    top: calc(12px + var(--safe-top, 0px));
    right: 12px;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 7px;
    border-radius: 999px;
    /* Opaque, like every other floating surface: --bg-0..3 carry an alpha in
       thirty-one of the theme packs and this one is over a dialog. */
    background: var(--bg-elevated);
    border: 1px solid color-mix(in srgb, var(--ok) 28%, var(--border));
    box-shadow: var(--shadow-pop);
  }
  .cm-live {
    width: 7px;
    height: 7px;
    margin-left: 3px;
    border-radius: 50%;
    flex: none;
    background: var(--ok);
    animation: cm-blink 1.4s ease-in-out infinite;
  }
  @keyframes cm-blink {
    50% {
      opacity: 0.3;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .cm-live {
      animation: none;
    }
  }
  @media (pointer: coarse), (max-width: 768px) {
    .callmini .callbtn.sm {
      width: var(--tap-min);
      height: var(--tap-min);
    }
  }
</style>
