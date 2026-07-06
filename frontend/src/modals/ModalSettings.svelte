<script>
  import Modal from "./Modal.svelte";
  import { onMount } from "svelte";
  import { api } from "../lib/api.js";

  let { onClose, onSaved } = $props();
  let bootstrap = $state("");
  let saved = $state(false);

  onMount(async () => {
    try {
      bootstrap = ((await api.getBootstrap()) || []).join("\n");
    } catch {
      /* ignore */
    }
  });

  async function save() {
    try {
      await api.setBootstrapLive(bootstrap);
      saved = true;
      onSaved?.();
      setTimeout(() => onClose(), 700);
    } catch (err) {
      alert(String(err?.message || err));
    }
  }
</script>

<Modal title="Network settings" {onClose}>
  <p class="muted">
    <strong>Rendezvous server</strong> — the address of the tiny relay that lets
    friends on other networks find you. You only need this if <em>you</em> host
    the server; friends get it automatically from your invite code.
  </p>
  <textarea
    rows="3"
    placeholder="/dns/your-app.fly.dev/tcp/4001/p2p/12D3Koo…"
    bind:value={bootstrap}
  ></textarea>
  <p class="muted tiny">
    Leave blank for same-Wi-Fi only. Takes effect immediately for new
    connections; a full restart applies it everywhere.
  </p>
  <div class="actions">
    <button class="ghost" onclick={onClose}>Close</button>
    <button onclick={save}>{saved ? "Saved ✓" : "Save"}</button>
  </div>

  <hr />
  <button
    class="ghost signout"
    onclick={async () => {
      await api.logout();
      location.reload();
    }}
  >
    Sign out (lock this device)
  </button>
</Modal>

<style>
  p {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
    text-align: left;
  }
  .tiny {
    font-size: 11px;
  }
  textarea {
    font-family: ui-monospace, monospace;
    font-size: 11px;
    resize: vertical;
  }
  hr {
    border: none;
    border-top: 1px solid var(--border);
    margin: 4px 0;
  }
  .signout {
    color: var(--danger);
    align-self: flex-start;
    font-size: 13px;
  }
</style>
