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

  let { onToggleMute, onLeave, label = "" } = $props();

  const tap = (fn, style = "light") => () => {
    haptic(style);
    fn?.();
  };
</script>

<div class="callmini" role="group" aria-label="Call controls">
  <span class="cm-live" aria-hidden="true"></span>
  <span class="cm-lbl">{label || "In call"}</span>
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
    border: 1px solid var(--border);
    box-shadow: var(--shadow-pop);
  }
  .cm-live {
    width: 7px;
    height: 7px;
    margin-left: 3px;
    border-radius: 50%;
    flex: none;
    background: var(--ok);
  }
  .cm-lbl {
    display: none;
  }
  @media (max-width: 768px) {
    .callmini .callbtn.sm {
      width: var(--tap-min);
      height: var(--tap-min);
    }
    /* A phone's dialogs are full-width sheets and full-screen pages, and a
       pill floating at the top-right corner landed on exactly what every page
       keeps there: its × — Voice & video's too, the page people open because
       of the call — and the right end of the Inbox's tabs. On a phone the
       controls are a band of their own under the status bar, named for the
       call, and what is open below steps down by the band's height. */
    .callmini {
      top: 0;
      left: 0;
      right: 0;
      box-sizing: content-box;
      height: calc(var(--tap-min) + var(--sp-2));
      padding: var(--safe-top) var(--sp-3) 0;
      gap: var(--sp-2);
      border-width: 0 0 1px;
      border-radius: 0;
      box-shadow: none;
    }
    .cm-lbl {
      display: block;
      flex: 1;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-size: var(--fs-compact);
      font-weight: 600;
      color: var(--text);
    }
    :global(body:has(.callmini) .dialog) {
      max-height: calc(100 * var(--dvh) - var(--kb, 0px) - var(--safe-top) - var(--tap-min) - 2 * var(--sp-2));
    }
    :global(body:has(.callmini) .dialog.page) {
      height: calc(100 * var(--dvh) - var(--kb, 0px) - var(--tap-min) - var(--sp-2) - 1px);
    }
    :global(body:has(.callmini) .dialog.page.tabpage) {
      height: calc(100 * var(--dvh) - var(--tabbar-h, 0px) - var(--kb, 0px) - var(--tap-min) - var(--sp-2) - 1px);
    }
  }
</style>
