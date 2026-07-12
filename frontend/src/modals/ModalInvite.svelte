<script>
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  let { code, onCopy, onClose } = $props();

  let copied = $state(false);
  function copy() {
    onCopy(code);
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }
</script>

<Modal title="Invite a friend" {onClose}>
  <p class="muted lead">
    This code carries everything your friend needs — the guild, how to reach you, and your
    relay. They pick a passphrase, paste it into <strong>Join with invite</strong>, and they're in.
  </p>

  <div class="code-well">
    <code>{code}</code>
    <button class="copy" class:copied onclick={copy}>
      <Icon name={copied ? "check" : "spark"} size={14} />
      {copied ? "Copied" : "Copy code"}
    </button>
  </div>

  <p class="hint muted">
    <Icon name="spark" size={12} /> Anyone with this code can join, so share it directly with people
    you trust.
  </p>

  <div class="actions">
    <button class="ghost" onclick={onClose}>Done</button>
  </div>
</Modal>

<style>
  .lead {
    margin: 0;
    font-size: 13px;
    line-height: 1.55;
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
    font-size: 12px;
    line-height: 1.5;
    word-break: break-all;
    color: var(--text);
    max-height: 120px;
    overflow-y: auto;
  }
  .copy {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    font-size: 13px;
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
    font-size: 12px;
  }
  /* Phone: the copy action is the whole point — make it unmissable. */
  @media (pointer: coarse), (max-width: 700px) {
    .copy {
      align-self: stretch;
      min-height: 48px;
    }
  }
</style>
