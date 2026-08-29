<script>
  // Presence + custom-status popover, opened from the self row.
  // Positioning mirrors ProfilePopover: measure the card, place it above the
  // anchor (below when there's no room), clamped to the viewport. Every change
  // applies via api.setProfile while preserving the rest of the profile.
  import Icon from "./Icon.svelte";
  import GameShelf from "./GameShelf.svelte";
  import { S, flash, refreshRightPanel, patchProfile } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PRESENCE_OPTIONS, splitStatus, joinStatus } from "./lib/presence.js";
  import { pushLayer } from "./lib/navstack.svelte.js";
  import { place as placeCard, rectOf, sizeOf } from "./lib/place.js";
  import { sheetdrag } from "./lib/sheet.js";
  // Escape with the caret in "What\'s happening?" steps out of the field rather
  // than throwing the sentence away. See lib/fieldescape.js.
  import { fieldEscape } from "./lib/fieldescape.js";

  let { anchor, onClose } = $props(); // anchor: {x, y, w, h} of the trigger button

  // Unregistered until now: hardware back walked straight past an open status
  // sheet and out of the app.
  $effect(() => pushLayer("popover", onClose));

  const EMOJI = ["🎮", "🎧", "🌙", "💻", "📚", "☕", "🏃", "😴"];
  const PRESETS = [
    { emoji: "🎮", text: "Gaming" },
    { emoji: "🎧", text: "Focusing" },
    { emoji: "🌙", text: "Away" },
  ];

  // Seed the editor from the saved status once, at open.
  const current = splitStatus(S.identity.status);
  let statusEmoji = $state(current.emoji);
  let statusText = $state(current.text);
  let busy = $state(false);

  const myPresence = $derived(S.identity.presence || "online");
  const dirty = $derived(joinStatus(statusEmoji, statusText.trim()) !== (S.identity.status || "").trim());

  // Presence and/or status, with the popover's busy state around it. The
  // carry-everything-else part lives in patchProfile, shared with the other
  // places that change one field.
  async function save(patch) {
    if (busy) return;
    busy = true;
    try {
      await patchProfile(patch);
      await refreshRightPanel(); // your own dot in the member list
    } catch (err) {
      flash(err);
    } finally {
      busy = false;
    }
  }

  // Presence applies instantly and keeps the popover open (the check moves),
  // so you can set a status in the same visit.
  function pickPresence(id) {
    if (id !== myPresence) save({ presence: id });
  }

  async function submitStatus(e) {
    e?.preventDefault();
    if (!dirty) return;
    await save({ status: joinStatus(statusEmoji, statusText.trim()) });
    onClose();
  }

  async function usePreset(p) {
    statusEmoji = p.emoji;
    statusText = p.text;
    await save({ status: joinStatus(p.emoji, p.text) });
    onClose();
  }

  async function clearStatus() {
    statusEmoji = "";
    statusText = "";
    await save({ status: "" });
  }

  // Game collection: editable right from your own card (same shelf as the
  // profile popover). Saving re-announces the profile to every guild.
  async function saveGames(next) {
    await api.setGames(next);
    S.identity = await api.identity();
    await refreshRightPanel();
  }

  // Measure then place (see ProfilePopover): above the anchor if it fits,
  // else below; clamp to the viewport with an 8px margin. (Desktop only —
  // the mobile presentation is a bottom sheet and ignores the anchor.)
  let card = $state(null);
  let scrimEl = $state(null);
  let pos = $state(null);
  $effect(() => {
    if (!card || !anchor || S.isMobile) {
      pos = null;
      return;
    }
    // The card's height is NOT reactive state — expanding the game shelf (or
    // any other section) changes it without re-running this effect, and the
    // card then grows DOWNWARD from a stale top until its bottom leaves the
    // screen. Re-place on every size change instead, so growth pushes the top
    // up and the card stays pinned just above the trigger.
    // Left-aligned on the trigger, above it when there is room. The whole sum
    // is in layout pixels (lib/place.js) — the anchor arrives as client
    // coordinates, which are a different unit once the UI scale is not 100%.
    // Never let the clamp push the top off-screen: a card taller than the
    // viewport sits at the margin and scrolls internally (see max-height).
    const place = () => {
      const p = placeCard({
        anchor: rectOf(anchor),
        ...sizeOf(card),
        side: "top",
        align: "start",
        gap: 8,
      });
      pos = { left: p.left, top: p.top };
    };
    place();
    const ro = new ResizeObserver(place);
    ro.observe(card);
    window.addEventListener("resize", place);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", place);
    };
  });
</script>

<svelte:window
  onresize={onClose}

  onpointerdown={(e) => {
    // The trigger toggles the popover itself; closing here too would reopen it.
    if (!e.target.closest(".status-pop, .me-status-trigger")) onClose();
  }}
/>

{#if S.isMobile}
  <!-- Sheet presentation gets a dimming scrim; tap anywhere on it to close. -->
  <button bind:this={scrimEl} class="sp-scrim" onclick={onClose} aria-label="Close status picker"></button>
{/if}
<div
  class="status-pop"
  class:sheet={S.isMobile}
  bind:this={card}
  style={S.isMobile ? "" : pos ? `left:${pos.left}px;top:${pos.top}px` : "opacity:0;pointer-events:none"}
  role="dialog"
  aria-label="Set status"
  use:fieldEscape
>
  {#if S.isMobile}
    <!-- This sheet had no grip at all — no handle, and nothing to pull. The
         strip is sticky so a scrolled sheet keeps its own way out within
         thumb reach; the physics is the app's one set (lib/sheet.js). -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="sp-grab"
      use:sheetdrag={{ sheet: () => card, scrim: () => scrimEl, scroller: () => card, onDismiss: onClose }}
    >
      <span class="sp-handle"></span>
    </div>
  {/if}
  <div class="sec-label muted">Presence</div>
  {#each PRESENCE_OPTIONS as p (p.id)}
    <button class="presence" class:sel={myPresence === p.id} onclick={() => pickPresence(p.id)} disabled={busy}>
      <span class="dot" style="--pc:{p.color};background:var(--pc)"></span>
      <span class="p-text">
        <strong>{p.label}</strong>
        <span class="muted p-desc">{p.desc}</span>
      </span>
      {#if myPresence === p.id}<span class="p-check"><Icon name="check" size={13} /></span>{/if}
    </button>
  {/each}

  <div class="divider"></div>
  <div class="sec-label muted">Custom status</div>
  <div class="emoji-row">
    {#each EMOJI as em (em)}
      <button
        class="em"
        class:sel={statusEmoji === em}
        title={statusEmoji === em ? "Remove emoji" : `Use ${em}`}
        onclick={() => (statusEmoji = statusEmoji === em ? "" : em)}>{em}</button
      >
    {/each}
  </div>
  <form class="st-box" onsubmit={submitStatus}>
    <input
      bind:value={statusText}
      placeholder="What's happening?"
      aria-label="Status message"
      maxlength="80"
      disabled={busy}
    />
    <button type="submit" class="st-save" disabled={busy || !dirty} aria-label="Save status">
      <Icon name="check" size={14} />
    </button>
  </form>
  <div class="presets">
    {#each PRESETS as p (p.text)}
      <button class="preset" onclick={() => usePreset(p)} disabled={busy}>{p.emoji} {p.text}</button>
    {/each}
  </div>
  {#if S.identity.status}
    <button class="clear" onclick={clearStatus} disabled={busy}>
      <Icon name="close" size={12} /> Clear status
    </button>
  {/if}

  <div class="divider"></div>
  <GameShelf games={S.identity.games || []} editable={true} onchange={saveGames} />
</div>

<style>
  .status-pop {
    position: fixed;
    z-index: 250;
    width: 248px;
    /* Opaque, like every lifted surface. The sticky grab strip below already
       used --bg-elevated, so on a glass pack the handle was solid and the
       sheet under it was not. */
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 3px;
    /* An expanded game shelf can outgrow the viewport. Cap it and scroll
       inside rather than letting the card run off the bottom of the screen. */
    max-height: calc(100 * var(--dvh) - 16px);
    overflow-y: auto;
    overscroll-behavior: contain;
    animation: pop-in 0.18s var(--ease-spring);
  }
  @keyframes pop-in {
    from {
      opacity: 0;
      transform: translateY(6px) scale(0.96);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .status-pop {
      animation: none;
    }
  }
  .sec-label {
    font-size: var(--fs-tiny);
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin: 2px 4px 3px;
  }
  .presence {
    display: flex;
    align-items: center;
    gap: 9px;
    width: 100%;
    padding: 6px 8px;
    background: transparent;
    color: var(--text);
    text-align: left;
    border-radius: var(--radius-sm);
  }
  .presence {
    transition: background var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .presence:hover {
      background: var(--bg-3);
    }
    /* The dot answers the hover: grows and glows in its own presence color. */
    .presence:hover .dot {
      transform: scale(1.35);
      box-shadow: 0 0 8px 1px color-mix(in srgb, var(--pc) 65%, transparent);
    }
    .em:hover {
      background: var(--bg-3);
      transform: scale(1.18);
    }
  }
  .presence:active {
    background: var(--bg-3);
  }
  .presence.sel {
    background: var(--accent-soft);
  }
  .dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    flex-shrink: 0;
    transition: transform var(--dur-standard) var(--ease-spring), box-shadow var(--dur-standard) ease;
  }
  .presence.sel .dot {
    box-shadow: 0 0 6px 1px color-mix(in srgb, var(--pc) 55%, transparent);
  }
  .p-text {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .p-text strong {
    font-size: var(--fs-ui);
  }
  .p-desc {
    font-size: var(--fs-small);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .p-check {
    color: var(--accent-hover);
    display: inline-grid;
    place-items: center;
    flex-shrink: 0;
  }
  .divider {
    height: 1px;
    background: var(--border);
    margin: 7px 0 4px;
  }
  .emoji-row {
    display: flex;
    gap: 2px;
    padding: 0 2px;
  }
  .em {
    flex: 1;
    padding: 4px 0;
    font-size: var(--fs-body);
    background: transparent;
    border-radius: var(--radius-sm);
    line-height: 1.2;
    transition: background var(--dur-quick) ease;
  }
  .em {
    transition: background var(--dur-quick) ease, transform var(--dur-standard) var(--ease-spring);
  }
  .em:active {
    transform: scale(0.95);
  }
  .em.sel {
    background: var(--accent-soft);
    color: var(--text);
  }
  /* After the hover rules, not before them: this block used to sit above the
     transforms it was meant to cancel, at the same specificity, so it lost. */
  @media (prefers-reduced-motion: reduce) {
    .presence:hover .dot,
    .em:hover,
    .em:active {
      transform: none;
    }
  }
  .st-box {
    display: flex;
    gap: 6px;
    margin-top: 5px;
  }
  .st-box input {
    flex: 1;
    min-width: 0;
    padding: 7px 9px;
    font-size: var(--fs-ui);
    border-radius: var(--radius-sm);
  }
  .st-save {
    padding: 0 11px;
    display: grid;
    place-items: center;
    transition: box-shadow 0.2s ease, opacity var(--dur-standard) ease;
  }
  /* A ready-to-save glow the moment the draft differs from the saved status. */
  .st-save:not(:disabled) {
    box-shadow: 0 0 12px color-mix(in srgb, var(--accent) 40%, transparent);
  }
  .st-save:disabled {
    opacity: 0.45;
  }
  .presets {
    display: flex;
    gap: 5px;
    margin-top: 6px;
  }
  .preset {
    flex: 1;
    padding: 5px 4px;
    font-size: var(--fs-compact);
    background: var(--bg-3);
    color: var(--text-muted);
    border-radius: var(--radius-md);
    white-space: nowrap;
    transition: background var(--dur-quick) ease, color var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .preset:hover {
      background: var(--border);
      color: var(--text);
    }
  }
  .preset:active {
    background: var(--border);
    color: var(--text);
  }
  .clear {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 5px;
    margin-top: 6px;
    padding: 5px;
    font-size: var(--fs-compact);
    background: transparent;
    color: var(--text-faint);
    border-radius: var(--radius-sm);
  }
  .clear:hover {
    color: var(--danger-text);
    background: var(--danger-soft);
  }
  .sp-scrim {
    position: fixed;
    inset: 0;
    background: var(--scrim);
    z-index: 400;
    border: none;
    animation: sp-fade var(--dur-standard) ease;
  }
  @keyframes sp-fade {
    from {
      opacity: 0;
    }
  }
  /* The grab strip, matching every other sheet's pill. Sticky, with the
     sheet's own top padding pulled back onto it, so it survives a scroll. */
  .sp-grab {
    position: sticky;
    top: -14px;
    z-index: 3;
    margin: -14px -14px 2px;
    padding: 10px 14px 6px;
    background: var(--bg-elevated, var(--bg-1));
    /* lib/sheet.js overwrites this as the sheet scrolls; see Modal.svelte. */
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
    cursor: grab;
  }
  .sp-handle {
    display: block;
    width: 40px;
    height: 5px;
    margin: 0 auto;
    border-radius: 999px;
    background: var(--border);
  }
  /* Mobile: present as a full-width bottom sheet with finger-sized rows. */
  .status-pop.sheet {
    left: 0;
    right: 0;
    top: auto;
    bottom: 0;
    width: auto;
    z-index: 401; /* above its scrim */
    /* Presence rows + emoji row + input + presets + the whole game shelf can
       easily outgrow the screen, and an unbounded sheet grows UPWARD off the
       top edge, where the presence options it starts with become unreachable.
       dvh because the status input opens the keyboard. */
    max-height: calc(88 * var(--dvh));
    overflow-y: auto;
    overscroll-behavior: contain;
    border: none;
    border-radius: var(--radius-sheet) var(--radius-sheet) 0 0;
    padding: 14px 14px calc(16px + var(--safe-bottom));
    animation: sheet-up 0.22s var(--ease-out);
  }
  @keyframes sheet-up {
    from {
      transform: translateY(100%);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .status-pop.sheet {
      animation: none;
    }
  }
  @media (pointer: coarse), (max-width: 768px) {
    .presence {
      min-height: 46px;
    }
    .em {
      font-size: 20px;
      padding: 8px 0;
      min-height: var(--tap-min);
    }
    .st-box input {
      font-size: 16px; /* stops iOS auto-zoom on focus */
      padding: 10px 12px;
    }
    .st-save {
      min-width: 48px;
    }
    .preset {
      padding: 10px 6px;
      font-size: var(--fs-ui);
      min-height: var(--tap-min);
    }
    .clear {
      min-height: var(--tap-min);
    }
  }
</style>
