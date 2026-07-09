<script>
  // Discord-style presence + custom-status popover, opened from the self row.
  // Positioning mirrors ProfilePopover: measure the card, place it above the
  // anchor (below when there's no room), clamped to the viewport. Every change
  // applies via api.setProfile while preserving the rest of the profile.
  import Icon from "./Icon.svelte";
  import { S, flash, refreshRightPanel } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { PRESENCE_OPTIONS, splitStatus, joinStatus } from "./lib/presence.js";

  let { anchor, onClose } = $props(); // anchor: {x, y, w, h} of the trigger button

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

  // One writer: patch presence and/or status, keeping every other profile
  // field exactly as S.identity holds it.
  async function save(patch) {
    if (busy) return;
    busy = true;
    try {
      const id = S.identity;
      await api.setProfile(
        id.displayName || "",
        patch.status ?? id.status ?? "",
        id.emoji || "",
        id.color || "",
        id.avatar || "",
        id.banner || "",
        patch.presence ?? id.presence ?? "",
        id.bio || "",
      );
      S.identity = await api.identity();
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

  // Measure then place (see ProfilePopover): above the anchor if it fits,
  // else below; clamp to the viewport with an 8px margin.
  let card = $state(null);
  let pos = $state(null);
  $effect(() => {
    if (!card || !anchor) {
      pos = null;
      return;
    }
    const cw = card.offsetWidth;
    const ch = card.offsetHeight;
    const left = Math.max(8, Math.min(anchor.x, window.innerWidth - cw - 8));
    let top = anchor.y - ch - 8;
    if (top < 8) top = anchor.y + anchor.h + 8;
    top = Math.min(top, window.innerHeight - ch - 8);
    pos = { left, top };
  });
</script>

<svelte:window
  onresize={onClose}
  onkeydown={(e) => e.key === "Escape" && onClose()}
  onpointerdown={(e) => {
    // The trigger toggles the popover itself; closing here too would reopen it.
    if (!e.target.closest(".status-pop, .me-status-trigger")) onClose();
  }}
/>

<div
  class="status-pop"
  bind:this={card}
  style={pos ? `left:${pos.left}px;top:${pos.top}px` : "opacity:0;pointer-events:none"}
  role="dialog"
  aria-label="Set status"
>
  <div class="sec-label muted">Presence</div>
  {#each PRESENCE_OPTIONS as p (p.id)}
    <button class="presence" class:sel={myPresence === p.id} onclick={() => pickPresence(p.id)} disabled={busy}>
      <span class="dot" style="background:{p.color}"></span>
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
    <input bind:value={statusText} placeholder="What's happening?" maxlength="80" disabled={busy} />
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
</div>

<style>
  .status-pop {
    position: fixed;
    z-index: 250;
    width: 248px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 3px;
    animation: pop-in 0.12s ease;
  }
  @keyframes pop-in {
    from {
      opacity: 0;
      transform: translateY(4px) scale(0.98);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .status-pop {
      animation: none;
    }
  }
  .sec-label {
    font-size: 10px;
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
  .presence:hover {
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
  }
  .p-text {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .p-text strong {
    font-size: 13px;
  }
  .p-desc {
    font-size: 11px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .p-check {
    color: var(--accent);
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
    font-size: 15px;
    background: transparent;
    border-radius: var(--radius-sm);
    line-height: 1.2;
    transition: background 0.12s ease;
  }
  .em:hover {
    background: var(--bg-3);
  }
  .em.sel {
    background: var(--accent-soft);
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
    font-size: 13px;
    border-radius: var(--radius-sm);
  }
  .st-save {
    padding: 0 11px;
    display: grid;
    place-items: center;
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
    font-size: 12px;
    background: var(--bg-3);
    color: var(--text-muted);
    border-radius: 10px;
    white-space: nowrap;
    transition: background 0.12s ease, color 0.12s ease;
  }
  .preset:hover {
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
    font-size: 12px;
    background: transparent;
    color: var(--text-faint);
    border-radius: var(--radius-sm);
  }
  .clear:hover {
    color: var(--danger);
    background: var(--danger-soft);
  }
</style>
