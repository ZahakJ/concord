<script>
  // Floating Discord-style profile card. Reads S.profilePopover ({fingerprint,
  // rect}); positions itself above the anchor (or below when there's no room),
  // clamped to the viewport. Rendered once at the app root so nothing clips it.
  import Icon from "./Icon.svelte";
  import Avatar from "./Avatar.svelte";
  import {
    S,
    memberByFpr,
    holdProfilePopover,
    scheduleCloseProfilePopover,
    closeProfilePopover,
    popoverJustOpened,
    refreshRightPanel,
    startDM,
    flash,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";

  let dmText = $state("");
  let dmBusy = $state(false);

  async function sendDM(e) {
    e?.preventDefault();
    if (dmBusy) return;
    dmBusy = true;
    const text = dmText;
    dmText = "";
    try {
      await startDM(mem.fingerprint, text);
      closeProfilePopover();
    } catch (err) {
      dmText = text;
      flash(err);
    } finally {
      dmBusy = false;
    }
  }

  const mem = $derived(S.profilePopover ? memberByFpr(S.profilePopover.fingerprint) : null);
  // Clear the quick-message box when the card switches to a different person.
  $effect(() => {
    S.profilePopover?.fingerprint;
    dmText = "";
  });

  let card = $state(null);
  let pos = $state(null); // {left, top} once measured

  // Measure then place: above the anchor if it fits, else below; clamp to
  // the viewport with an 8px margin.
  $effect(() => {
    const pop = S.profilePopover;
    if (!pop || !card) {
      pos = null;
      return;
    }
    const cw = card.offsetWidth;
    const ch = card.offsetHeight;
    let left = pop.rect.x + pop.rect.w / 2 - cw / 2;
    left = Math.max(8, Math.min(left, window.innerWidth - cw - 8));
    let top = pop.rect.y - ch - 8;
    if (top < 8) top = pop.rect.y + pop.rect.h + 8;
    top = Math.min(top, window.innerHeight - ch - 8);
    pos = { left, top };
  });

  async function verify() {
    try {
      await api.verifyFingerprint(mem.fingerprint);
      await refreshRightPanel();
      flash("Member verified ✓");
    } catch (err) {
      flash(err);
    }
  }

  const fprShort = $derived(mem ? mem.fingerprint.replace(/(.{4})/g, "$1 ").trim() : "");
</script>

<svelte:window
  onscroll={closeProfilePopover}
  onresize={closeProfilePopover}
  onkeydown={(e) => e.key === "Escape" && closeProfilePopover()}
  onpointerdown={(e) => {
    if (S.profilePopover && !e.target.closest(".pop") && !popoverJustOpened()) closeProfilePopover();
  }}
/>

{#if S.profilePopover && mem}
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="pop"
    bind:this={card}
    style={pos ? `left:${pos.left}px;top:${pos.top}px` : "opacity:0;pointer-events:none"}
    role="dialog"
    aria-label="{mem.name || 'Member'} profile"
    onmouseenter={holdProfilePopover}
    onmouseleave={scheduleCloseProfilePopover}
  >
    <div class="banner" style={mem.color ? `background:${mem.color}` : ""}></div>
    <div class="head">
      <div class="av-wrap">
        <Avatar
          name={mem.name || mem.fingerprint}
          emoji={mem.emoji}
          color={mem.color}
          image={mem.avatar}
          size={56}
          online={mem.online}
        />
      </div>
    </div>

    <div class="body">
      <div class="name-row">
        <strong>{mem.name || mem.fingerprint.slice(0, 9)}</strong>
        {#if mem.isSelf}<span class="tag">you</span>{/if}
        {#if mem.verified && !mem.isSelf}
          <span class="verified" title="Identity verified"><Icon name="check" size={12} /> verified</span>
        {/if}
      </div>
      {#if mem.status}<div class="status">{mem.status}</div>{/if}

      <div class="divider"></div>

      <div class="fpr-label muted">Safety number</div>
      <code class="fpr">{fprShort}</code>

      {#if mem.isSelf}
        <p class="hint muted">Others confirm it's really you by comparing this out-of-band.</p>
      {:else if mem.verified}
        <p class="hint muted">You've verified this fingerprint — no one can impersonate them.</p>
      {:else}
        <p class="hint muted">Compare this with them over a call; if it matches, verify.</p>
        <button class="verify-btn" onclick={verify}>Verify identity</button>
      {/if}

      {#if !mem.isSelf}
        <form class="dm-box" onsubmit={sendDM}>
          <input
            bind:value={dmText}
            placeholder="Message @{mem.name || 'them'}"
            disabled={dmBusy}
          />
          <button type="submit" class="dm-send" disabled={dmBusy} aria-label="Send message">
            <Icon name={dmBusy ? "spark" : "reply"} size={15} />
          </button>
        </form>
      {/if}
    </div>
  </div>
{/if}

<style>
  .pop {
    position: fixed;
    z-index: 250;
    width: 260px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    overflow: hidden;
    animation: pop-in 0.12s ease;
  }
  @keyframes pop-in {
    from {
      opacity: 0;
      transform: translateY(4px) scale(0.98);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .pop {
      animation: none;
    }
  }
  .banner {
    height: 48px;
    background: linear-gradient(120deg, var(--accent), var(--accent-hover));
  }
  .head {
    padding: 0 14px;
  }
  .av-wrap {
    width: fit-content;
    margin-top: -28px;
    padding: 3px;
    background: var(--bg-1);
    border-radius: 50%;
  }
  .body {
    padding: 6px 14px 14px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .name-row {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .name-row strong {
    font-size: 16px;
  }
  .tag {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    background: var(--bg-3);
    color: var(--text-muted);
    padding: 1px 6px;
    border-radius: 8px;
  }
  .verified {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: 11px;
    color: var(--ok);
  }
  .status {
    font-size: 13px;
    color: var(--text-muted);
  }
  .divider {
    height: 1px;
    background: var(--border);
    margin: 8px 0 4px;
  }
  .fpr-label {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .fpr {
    font-family: ui-monospace, monospace;
    font-size: 11px;
    line-height: 1.5;
    word-break: break-word;
    background: var(--bg-0);
    border-radius: var(--radius-sm);
    padding: 6px 8px;
    color: var(--text);
  }
  .hint {
    font-size: 11px;
    line-height: 1.4;
    margin: 4px 0 0;
  }
  .verify-btn {
    margin-top: 8px;
    font-size: 13px;
    padding: 7px;
  }
  .dm-box {
    display: flex;
    gap: 6px;
    margin-top: 10px;
    padding-top: 10px;
    border-top: 1px solid var(--border);
  }
  .dm-box input {
    flex: 1;
    padding: 8px 10px;
    font-size: 13px;
    border-radius: var(--radius-sm);
  }
  .dm-send {
    padding: 0 12px;
    display: grid;
    place-items: center;
  }
</style>
