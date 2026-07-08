<script>
  // A compact, draggable call window that pins your ongoing call while you
  // browse other channels/DMs. Drag it by the header so it never traps your
  // composer; click the header (or expand) to jump back to the full call view.
  import Avatar from "./Avatar.svelte";
  import Icon from "./Icon.svelte";
  import { S, memberByFpr } from "./lib/state.svelte.js";

  let { label = "", onLeave, onToggleMute, onReturn } = $props();

  // Position (top-right by default), clamped to the viewport, draggable.
  let pos = $state({ x: Math.max(12, window.innerWidth - 250), y: 70 });
  let drag = null;

  function clamp(x, y) {
    return {
      x: Math.max(8, Math.min(window.innerWidth - 230, x)),
      y: Math.max(8, Math.min(window.innerHeight - 90, y)),
    };
  }
  function onDown(e) {
    drag = { dx: e.clientX - pos.x, dy: e.clientY - pos.y };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }
  function onMove(e) {
    if (drag) pos = clamp(e.clientX - drag.dx, e.clientY - drag.dy);
  }
  function onUp() {
    drag = null;
    window.removeEventListener("pointermove", onMove);
    window.removeEventListener("pointerup", onUp);
  }
  // Keep the dock on-screen when the window is resized smaller.
  function onResize() {
    pos = clamp(pos.x, pos.y);
  }

  const roster = $derived(["self", ...S.voiceParticipants]);
  function part(pid) {
    if (pid === "self") {
      return {
        name: S.displayName || "You",
        emoji: S.identity.emoji,
        color: S.identity.color,
        image: S.identity.avatar,
        speaking: S.voiceSpeaking.includes("self"),
      };
    }
    const fpr = S.voicePeerFpr[pid];
    const m = fpr ? memberByFpr(fpr) : null;
    return {
      name: m?.name || (fpr ? fpr.slice(0, 9) : pid.slice(0, 6)),
      emoji: m?.emoji || "",
      color: m?.color || "",
      image: m?.avatar || "",
      speaking: S.voiceSpeaking.includes(pid),
    };
  }
</script>

<svelte:window onresize={onResize} />

<div class="dock" style="left:{pos.x}px; top:{pos.y}px">
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="head" onpointerdown={onDown} ondblclick={onReturn} title="Drag to move · double-click to open">
    <span class="live"></span>
    <span class="lbl">{label || "In call"}</span>
    <button
      class="ico expand"
      title="Return to call"
      aria-label="Return to call"
      onpointerdown={(e) => e.stopPropagation()}
      onclick={onReturn}
    >
      <Icon name="speaker" size={14} />
    </button>
  </div>

  <div class="faces">
    {#each roster as pid (pid)}
      {@const p = part(pid)}
      <div class="face" class:speaking={p.speaking} title={p.name}>
        <Avatar name={p.name} emoji={p.emoji} color={p.color} image={p.image} size={30} />
      </div>
    {/each}
  </div>

  <div class="ctl">
    <button class="ico" title={S.muted ? "Unmute" : "Mute"} aria-label={S.muted ? "Unmute" : "Mute"} onclick={onToggleMute}>
      <Icon name={S.muted ? "micOff" : "mic"} size={15} />
    </button>
    <button class="ico hang" title="Leave call" aria-label="Leave call" onclick={onLeave}>
      <Icon name="door" size={15} />
    </button>
  </div>
</div>

<style>
  .dock {
    position: fixed;
    width: 214px;
    z-index: 90; /* above chat, but BELOW modals (100) so dialogs aren't covered */
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    user-select: none;
  }
  .head {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 7px 8px 7px 10px;
    background: var(--ok-soft);
    cursor: move;
  }
  .live {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--ok);
    flex-shrink: 0;
    animation: blink 1.4s ease-in-out infinite;
  }
  @keyframes blink {
    50% {
      opacity: 0.3;
    }
  }
  .lbl {
    flex: 1;
    min-width: 0;
    font-size: 12px;
    font-weight: 600;
    color: var(--ok);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .faces {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding: 10px;
    max-height: 120px; /* ~3 rows; a big call scrolls instead of growing off-screen */
    overflow-y: auto;
  }
  .face :global(.avatar) {
    border: 2px solid transparent;
    transition: border-color 0.1s ease;
  }
  .face.speaking :global(.avatar) {
    border-color: var(--ok);
    box-shadow: 0 0 0 2px var(--ok-soft);
  }
  .ctl {
    display: flex;
    justify-content: center;
    gap: 10px;
    padding: 0 10px 10px;
  }
  .ico {
    width: 34px;
    height: 34px;
    padding: 0;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--bg-3);
    color: var(--text);
    border: 1px solid var(--border);
  }
  .ico:hover {
    background: var(--bg-1);
  }
  .ico.expand {
    width: 24px;
    height: 24px;
    background: transparent;
    border: none;
    color: var(--ok);
  }
  .ico.hang {
    background: var(--danger);
    color: #fff;
    border-color: transparent;
  }
  .ico.hang:hover {
    background: color-mix(in srgb, var(--danger) 85%, #000);
  }
</style>
