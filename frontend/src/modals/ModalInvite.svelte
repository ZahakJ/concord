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
    This code carries everything your friend needs — the server, how to reach you, and your
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
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
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
    gap: 6px;
    font-size: 13px;
    padding: 7px 14px;
  }
  .copy.copied {
    background: var(--ok);
  }
  .hint {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    font-size: 12px;
  }
</style>
