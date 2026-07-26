<script>
  // How this node reaches the outside world: the rendezvous address, and the
  // local-only extras that depend on what's installed on this machine.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import { onMount } from "svelte";
  import { api } from "../lib/api.js";
  import { S, flash, refreshOcr } from "../lib/state.svelte.js";

  let { onClose, onSaved } = $props();

  let bootstrap = $state("");
  onMount(async () => {
    refreshOcr(); // refresh the image-text search readout while this is open
    try {
      bootstrap = ((await api.getBootstrap()) || []).join("\n");
    } catch {
      /* ignore */
    }
  });

  // Display-first: the address shows as a copyable chip; editing (a once-ever
  // action for self-hosters) hides behind the pencil.
  let editing = $state(false);
  let draft = $state("");
  let copied = $state(false);

  function startEdit() {
    draft = bootstrap;
    editing = true;
  }
  function copyAddr() {
    navigator.clipboard?.writeText(bootstrap);
    copied = true;
    setTimeout(() => (copied = false), 1600);
  }
  async function save() {
    try {
      await api.setBootstrapLive(draft);
      bootstrap = draft;
      editing = false;
      onSaved?.(); // parent toasts the confirmation
    } catch (err) {
      flash(err);
    }
  }

  // Live shape-check while editing: every non-blank line should be a
  // /…/p2p/<PeerID> multiaddr.
  const lines = $derived(draft.split("\n").filter((l) => l.trim()));
  const ok = $derived(lines.length > 0 && lines.every((l) => l.trim().startsWith("/") && l.includes("/p2p/")));
  const bad = $derived(lines.length > 0 && !ok);
</script>

<Modal title="Connection" {onClose} wide>
  <SettingGroup
    label="Rendezvous server"
    note="The tiny relay that lets friends on other networks find you. You only
          need to set this if you host it yourself — friends get it automatically
          from your invite code. Blank means same-Wi-Fi only."
  >
    <div class="conn">
      {#if editing}
        <div class="code-wrap" class:ok class:bad>
          <textarea
            class="code-box"
            rows="3"
            placeholder="/dns/your-app.fly.dev/tcp/4001/p2p/12D3Koo…"
            bind:value={draft}
          ></textarea>
          {#if ok}
            <span class="code-state"><Icon name="check" size={13} /> address looks good</span>
          {:else if bad}
            <span class="code-state">should start with /dns or /ip4 and contain /p2p/…</span>
          {/if}
        </div>
        <div class="foot">
          <span class="hint">Applies live to new connections.</span>
          <button class="ghost cancel" onclick={() => (editing = false)}>Cancel</button>
          <button class="save" disabled={bad} onclick={save}>Save</button>
        </div>
      {:else if bootstrap.trim()}
        <div class="addr-row">
          <code class="addr" title={bootstrap}>{bootstrap.trim()}</code>
          <button class="addr-act" onclick={copyAddr} aria-label="Copy address">
            {#if copied}<Icon name="check" size={15} />{:else}<Icon name="copy" size={15} />{/if}
          </button>
          <button class="addr-act" onclick={startEdit} aria-label="Edit address">
            <Icon name="edit" size={15} />
          </button>
        </div>
      {:else}
        <div class="addr-row empty">
          <span class="hint">Not set — you can only reach friends on the same Wi-Fi.</span>
          <button class="save" onclick={startEdit}>Set address</button>
        </div>
      {/if}
    </div>
  </SettingGroup>

  <SettingGroup label="Diagnostics">
    <SettingRow
      icon="poll"
      title="Stats &amp; diagnostics"
      sub="Storage, peers &amp; connection health"
      to="stats"
      from="connection"
    />
  </SettingGroup>

  <SettingGroup
    label="Local search"
    note={S.ocr?.available
      ? `Text in shared screenshots is read out on this machine (${S.ocr.engine}) and joins search. Extracted text is sealed at rest like your messages, and never leaves this device.`
      : "Optional: install a local OCR engine and Concord will search the text inside shared screenshots. Run `pip install rapidocr-onnxruntime`, then put scripts/concord-ocr on your PATH. It runs entirely on this machine — nothing is ever uploaded."}
  >
    <SettingRow
      icon="imagetext"
      title="Search inside images"
      sub={S.ocr?.available
        ? `${S.ocr.counts?.ok || 0} image${(S.ocr.counts?.ok || 0) === 1 ? "" : "s"} indexed`
        : "Not installed on this machine"}
    />
  </SettingGroup>
</Modal>

<style>
  .conn {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px 14px;
  }
  .hint {
    font-size: 12px;
    line-height: 1.5;
    color: var(--text-muted);
  }
  /* Display state: the address as a copyable chip with quiet icon actions. */
  .addr-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .addr {
    flex: 1;
    min-width: 0;
    font-family: var(--mono-font);
    font-size: 12px;
    line-height: 1.4;
    padding: 10px 12px;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--bg-0) 42%, var(--bg-3));
    border: 1px solid color-mix(in srgb, var(--border) 62%, transparent);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .addr-act {
    flex-shrink: 0;
    width: 36px;
    height: 36px;
    padding: 0;
    display: grid;
    place-items: center;
    border-radius: var(--radius-md);
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .addr-act:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .addr-row.empty {
    flex-direction: column;
    align-items: stretch;
    gap: 10px;
  }
  .code-wrap {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .code-box {
    width: 100%;
    box-sizing: border-box;
    min-height: 84px;
    font-family: var(--mono-font);
    font-size: 12.5px;
    line-height: 1.65;
    white-space: pre-wrap;
    word-break: break-all;
    resize: none; /* it's an address, not an essay */
    border-radius: var(--radius-md);
    padding: 12px 14px;
  }
  /* The field answers back while you type: a green ring when the address
     parses, a warm hint when it doesn't. */
  .code-wrap.ok .code-box {
    border-color: color-mix(in srgb, var(--ok) 55%, transparent);
  }
  .code-wrap.bad .code-box {
    border-color: color-mix(in srgb, var(--danger) 45%, transparent);
  }
  .code-state {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 11.5px;
    color: var(--text-muted);
  }
  .code-wrap.ok .code-state {
    color: var(--ok);
  }
  .code-wrap.bad .code-state {
    color: color-mix(in srgb, var(--danger) 80%, var(--text));
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  .foot .hint {
    flex: 1;
    min-width: 0;
  }
  .cancel {
    padding: 7px 14px;
    font-size: 13px;
  }
  .save {
    flex-shrink: 0;
    padding: 7px 18px;
    font-size: 13px;
    font-weight: 600;
  }
</style>
