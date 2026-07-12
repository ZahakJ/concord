<script>
  // The just-created instant meeting: a ready-to-paste invitation for anyone
  // — including people who've never heard of Concord (the blurb carries the
  // download link). This IS the ad. 🚀
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";

  let { code, onClose } = $props();

  const DOWNLOAD = "https://github.com/ZahakJ/concord-dist/releases/latest";
  const blurb =
    `Hop on a Concord meeting with me ⚡\n` +
    `\n` +
    `1. Grab Concord (free, no account needed): ${DOWNLOAD}\n` +
    `2. Open it, pick any passphrase, and paste this invite:\n` +
    `\n` +
    `${code}\n` +
    `\n` +
    `See you there! (End-to-end encrypted — no server ever sees the call.)`;

  let copied = $state(false);
  let copiedCode = $state(false);
  function copyBlurb() {
    navigator.clipboard?.writeText(blurb);
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }
  function copyCode() {
    navigator.clipboard?.writeText(code);
    copiedCode = true;
    setTimeout(() => (copiedCode = false), 1600);
  }
</script>

<Modal title="Your meeting is live" {onClose}>
  <p class="muted intro">
    The room's ready — voice and chat, end-to-end encrypted, self-destructs
    after 24 hours. Send this to anyone:
  </p>

  <div class="blurb">
    <pre>{blurb}</pre>
  </div>

  <div class="actions">
    <button class="ghost" class:done={copiedCode} onclick={copyCode}>
      {copiedCode ? "Copied ✓" : "Copy invite code only"}
    </button>
    <button class:done={copied} onclick={copyBlurb}>
      <Icon name="copy" size={14} />
      {copied ? "Copied ✓" : "Copy invitation"}
    </button>
  </div>
  <p class="muted tiny">
    Friends who already have Concord just paste the code into “Join with invite”.
  </p>
</Modal>

<style>
  .intro {
    font-size: 13px;
    margin: 0 0 8px;
    line-height: 1.5;
  }
  .blurb {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    background: var(--bg-0);
    padding: 10px 12px;
    max-height: 220px;
    overflow-y: auto;
  }
  .blurb pre {
    margin: 0;
    font-family: inherit;
    font-size: 12.5px;
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 10px;
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .actions .done {
    background: var(--ok);
  }
  .tiny {
    font-size: 11px;
    margin: 8px 0 0;
  }
</style>
