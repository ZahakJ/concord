<script>
  import Modal from "./Modal.svelte";
  import { onMount } from "svelte";
  import { api } from "../lib/api.js";
  import { soundsEnabled, setSoundsEnabled } from "../lib/sounds.js";
  import { flash } from "../lib/state.svelte.js";

  let { onClose, onSaved } = $props();
  let bootstrap = $state("");
  let saved = $state(false);
  let sounds = $state(soundsEnabled());

  function toggleSounds() {
    sounds = !sounds;
    setSoundsEnabled(sounds);
  }

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
      flash(err);
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
  <button class="toggle-row" onclick={toggleSounds} role="switch" aria-checked={sounds}>
    <span>
      <strong>Sounds</strong>
      <span class="muted tiny">Voice join/leave chimes and @mention pings</span>
    </span>
    <span class="switch" class:on={sounds}><span class="knob"></span></span>
  </button>

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
  .toggle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 6px 2px;
  }
  .toggle-row:hover {
    background: transparent;
  }
  .toggle-row span {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .switch {
    flex-shrink: 0;
    width: 38px;
    height: 22px;
    border-radius: 11px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    display: block;
    position: relative;
    transition: background 0.15s ease;
  }
  .switch.on {
    background: var(--accent);
    border-color: var(--accent);
  }
  .knob {
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: white;
    transition: transform 0.15s ease;
  }
  .switch.on .knob {
    transform: translateX(16px);
  }
</style>
