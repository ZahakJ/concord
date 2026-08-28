<script>
  // Asked BEFORE the meeting exists.
  //
  // The one-click meeting is the flow you use to call somebody who does not
  // have Concord: they click a link and land in a browser. That link needs a
  // rendezvous — Concord has no server of its own to hand strangers, so the
  // gateway is whichever rendezvous node you point it at — and when there was
  // none configured the app said so *after* creating the guild, the channel and
  // the call, at the point of sharing, which is to say after the only thing the
  // feature is for had already failed.
  //
  // So it is asked first, with the two honest answers. There is deliberately no
  // "use the public one" button: this project ships no public rendezvous, and a
  // button offering somebody else's node as though it were ours would be the
  // one place in the app that quietly contradicts the front page.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { api } from "../lib/api.js";
  import { flash } from "../lib/state.svelte.js";

  let { onClose, onReady } = $props();
  // Snapshotted at setup. The parent reads this prop off S.modal, and the first
  // thing both buttons here do is clear S.modal — so reading it afterwards
  // reads a property of null.
  const go = onReady;

  let addr = $state("");
  let saving = $state(false);
  const lines = $derived(addr.split("\n").filter((l) => l.trim()));
  const ok = $derived(lines.length > 0 && lines.every((l) => l.trim().startsWith("/") && l.includes("/p2p/")));

  async function saveAndGo() {
    if (!ok || saving) return;
    saving = true;
    try {
      await api.setBootstrapLive(addr);
      onClose?.();
      go?.();
    } catch (err) {
      flash(err);
      saving = false;
    }
  }

  function anyway() {
    onClose?.();
    go?.();
  }
</script>

<Modal title="Before we open the room" {onClose}>
  <p class="lede">
    A meeting can go two ways. People who have Concord join with an invite code,
    fully end-to-end encrypted — that works right now. A <strong>guest link</strong>,
    the one that opens in somebody's browser with no install, needs a rendezvous
    address to hand them, and this device hasn't got one.
  </p>

  <section class="way">
    <div class="way-head">
      <span class="chip"><Icon name="link" size={15} /></span>
      <div class="way-text">
        <strong>Point it at a rendezvous</strong>
        <span class="muted">
          Yours, or one a friend runs. It's a tiny relay — see docs/RENDEZVOUS.md
          for the twenty-minute version.
        </span>
      </div>
    </div>
    <textarea
      class="code-box"
      rows="2"
      placeholder="/dns/your-app.fly.dev/tcp/4001/p2p/12D3Koo…"
      bind:value={addr}
    ></textarea>
    {#if lines.length && !ok}
      <span class="hint bad">should start with /dns or /ip4 and contain /p2p/…</span>
    {/if}
    <div class="btn-row">
      <button class="primary" disabled={!ok || saving} onclick={saveAndGo}>
        {saving ? "Saving…" : "Save and start the meeting"}
      </button>
    </div>
  </section>

  <section class="way">
    <div class="way-head">
      <span class="chip"><Icon name="concorde" size={15} /></span>
      <div class="way-text">
        <strong>Start without guest links</strong>
        <span class="muted">
          The room and the invite code work as normal. Anyone you invite will
          need the app.
        </span>
      </div>
    </div>
    <div class="btn-row">
      <button class="ghost" onclick={anyway}>Start the meeting anyway</button>
    </div>
  </section>
</Modal>

<style>
  .lede {
    margin: 0 0 var(--sp-3);
    font-size: var(--fs-ui);
    line-height: 1.55;
    color: var(--text-muted);
  }
  .way {
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-3);
    display: flex;
    flex-direction: column;
    gap: 9px;
  }
  .way + .way {
    margin-top: 10px;
  }
  .way-head {
    display: flex;
    align-items: flex-start;
    gap: 10px;
  }
  .chip {
    display: grid;
    place-items: center;
    width: 30px;
    height: 30px;
    flex: none;
    border-radius: var(--radius-md);
    background: var(--bg-3);
    color: var(--text-muted);
  }
  .way-text {
    display: flex;
    flex-direction: column;
    gap: 1px;
    font-size: var(--fs-ui);
  }
  .way-text .muted {
    font-size: var(--fs-compact);
    line-height: 1.45;
  }
  .code-box {
    width: 100%;
    font-family: var(--font-mono, monospace);
    font-size: var(--fs-small);
    resize: vertical;
  }
  .hint {
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .hint.bad {
    color: var(--danger-text);
  }
  .btn-row {
    display: flex;
    justify-content: flex-end;
  }
  .primary {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .primary:disabled {
    opacity: 0.5;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .btn-row button {
      flex: 1;
    }
  }
</style>
