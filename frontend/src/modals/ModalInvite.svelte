<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { S, flash, refreshGuilds, nameFor } from "../lib/state.svelte.js";
  import { api } from "../lib/api.js";
  import { haptic } from "../lib/touch.js";
  let { code, onCopy, onClose } = $props();

  let copied = $state(false);
  function copy() {
    onCopy(code);
    haptic("light");
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }

  // On a phone the whole point of this screen is to get the code INTO
  // WhatsApp/Signal/Messages. Copy-then-switch-apps-then-paste is the desktop
  // metaphor; the OS share sheet is one tap. Offered only where it exists, and
  // it falls back to Copy if the sheet is dismissed or the API is missing.
  const canShare = typeof navigator !== "undefined" && !!navigator.share && S.isMobile;
  async function share() {
    try {
      await navigator.share({ text: code });
      haptic("light");
    } catch {
      /* dismissed, or the platform refused — the Copy button is still there */
    }
  }

  // Add verified contacts straight in — no code needed. Their client
  // auto-accepts only because they verified US, so this is safe both ways.
  const memberFprs = $derived(new Set(S.members.map((m) => m.fingerprint)));
  const candidates = $derived(
    S.contacts.filter((c) => c.verified && !memberFprs.has(c.fingerprint)),
  );
  let busy = $state("");
  let added = $state(new Set());
  async function add(c) {
    busy = c.fingerprint;
    try {
      await api.addMember(S.activeGuildId, c.fingerprint);
      added = new Set([...added, c.fingerprint]);
      flash(`Added ${nameFor(c.fingerprint) || "them"} — they'll appear once they accept`, "success");
      setTimeout(refreshGuilds, 2500);
    } catch (err) {
      flash(err);
    } finally {
      busy = "";
    }
  }
</script>

<Modal title="Invite a friend" {onClose}>
  <p class="muted lead">
    This code carries everything your friend needs — the guild, how to reach you, and your
    relay. They pick a passphrase, paste it into <strong>Join with invite</strong>, and they're in.
  </p>

  <div class="code-well">
    <code>{code}</code>
    <div class="give">
      {#if canShare}
        <button class="share" onclick={share}>
          <Icon name="spark" size={14} /> Share code…
        </button>
      {/if}
      <button class="copy" class:copied class:secondary={canShare} onclick={copy}>
        <Icon name={copied ? "check" : "spark"} size={14} />
        {copied ? "Copied" : "Copy code"}
      </button>
    </div>
  </div>

  <p class="hint muted">
    <Icon name="spark" size={12} /> Anyone with this code can join, so share it directly with people
    you trust.
  </p>

  {#if candidates.length}
    <div class="divider"></div>
    <strong class="add-head">Or add a verified contact directly</strong>
    <p class="hint muted">No code needed — they drop straight in.</p>
    <div class="add-list">
      {#each candidates as c (c.fingerprint)}
        <div class="add-row">
          <Avatar name={nameFor(c.fingerprint)} size={30} />
          <span class="who">
            <strong>{nameFor(c.fingerprint)}</strong>
            <span class="tiny muted mono">{c.fingerprint.slice(0, 9)}</span>
          </span>
          {#if added.has(c.fingerprint)}
            <span class="done tiny"><Icon name="check" size={12} /> Added</span>
          {:else}
            <button class="add-btn" disabled={busy === c.fingerprint} onclick={() => add(c)}>
              {busy === c.fingerprint ? "Adding…" : "Add"}
            </button>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  <div class="actions">
    <button class="ghost" onclick={onClose}>Done</button>
  </div>
</Modal>

<style>
  .lead {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.55;
  }
  .divider {
    border-top: 1px solid var(--border);
    margin: 4px 0;
  }
  .add-head {
    font-size: var(--fs-ui);
  }
  .add-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    max-height: 240px;
    overflow-y: auto;
  }
  .add-row {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 7px 9px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
  }
  .who {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .who strong {
    font-size: var(--fs-ui);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mono {
    font-family: ui-monospace, monospace;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  .add-btn {
    flex-shrink: 0;
    padding: 6px 14px;
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: var(--radius-sm);
    font-size: var(--fs-ui);
  }
  .add-btn:disabled {
    opacity: 0.6;
  }
  .done {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--ok-text);
  }
  .code-well {
    position: relative;
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow: hidden;
  }
  /* One shimmer sweep on open — "here's the valuable thing". */
  .code-well::after {
    content: "";
    position: absolute;
    inset: 0;
    background: linear-gradient(
      105deg,
      transparent 30%,
      color-mix(in srgb, var(--accent) 14%, transparent) 50%,
      transparent 70%
    );
    transform: translateX(-100%);
    animation: shimmer 1.1s ease 0.25s 1 forwards;
    pointer-events: none;
  }
  @keyframes shimmer {
    to {
      transform: translateX(100%);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .code-well::after {
      animation: none;
      opacity: 0;
    }
  }
  code {
    font-family: ui-monospace, monospace;
    font-size: var(--fs-compact);
    line-height: 1.5;
    word-break: break-all;
    color: var(--text);
    max-height: 120px;
    overflow-y: auto;
  }
  .give {
    display: flex;
    gap: 8px;
  }
  .share {
    flex: 1;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: var(--fs-ui);
    font-weight: 600;
    padding: 7px 16px;
  }
  .copy {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: var(--fs-ui);
    font-weight: 600;
    padding: 7px 16px;
    transition:
      transform 0.12s ease,
      background 0.25s ease,
      box-shadow 0.25s ease;
  }
  .copy:active {
    transform: scale(0.97);
  }
  .copy.copied {
    background: var(--ok);
    box-shadow: 0 0 14px color-mix(in srgb, var(--ok) 45%, transparent);
    animation: copied-pop 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  @keyframes copied-pop {
    40% {
      transform: scale(1.06);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .copy.copied {
      animation: none;
    }
  }
  .hint {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    font-size: var(--fs-compact);
  }
  /* Phone: handing the code over is the whole point — make it unmissable, and
     demote Copy to a quiet partner once the OS share sheet is available. */
  @media (pointer: coarse), (max-width: 768px) {
    .copy {
      align-self: stretch;
      flex: 1;
      min-height: 48px;
    }
    .share {
      min-height: 48px;
    }
    .copy.secondary {
      flex: 0 0 auto;
      background: var(--bg-3);
      color: var(--text);
    }
    /* The code is already inside a sheet that scrolls; a 120px window on it was
       one more thumb trap, and it made a long code look truncated. */
    code {
      max-height: none;
      overflow-y: visible;
    }
    .add-list {
      max-height: none;
      overflow-y: visible;
    }
  }
</style>
