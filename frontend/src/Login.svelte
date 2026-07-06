<script>
  import { onMount } from "svelte";
  import { api } from "./lib/api.js";

  let { onLogin } = $props();
  let passphrase = $state("");
  let error = $state("");
  let busy = $state(false);
  let showConn = $state(false);
  let bootstrap = $state("");

  onMount(async () => {
    try {
      const list = await api.getBootstrap();
      bootstrap = (list || []).join("\n");
      if (bootstrap) showConn = true;
    } catch {
      /* connection settings are optional */
    }
  });

  async function submit(e) {
    e?.preventDefault();
    if (!passphrase || busy) return;
    busy = true;
    error = "";
    try {
      await api.setBootstrap(bootstrap); // persist before unlock so it takes effect
      await api.login(passphrase);
      onLogin();
    } catch (err) {
      error = String(err?.message || err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="login">
  <form class="card" onsubmit={submit}>
    <div class="logo">◆</div>
    <h1>Concord</h1>
    <p class="muted">
      Unlock your encrypted identity. On first run this passphrase creates it —
      remember it, there is no recovery.
    </p>
    <input type="password" placeholder="Passphrase" bind:value={passphrase} autofocus />

    <button
      type="button"
      class="conn-toggle"
      onclick={() => (showConn = !showConn)}
    >
      {showConn ? "▾" : "▸"} Connect with friends (server address)
    </button>
    {#if showConn}
      <div class="conn">
        <p class="muted small">
          To chat with friends over the internet, paste the rendezvous server
          address they gave you (one per line). Leave blank for same-network
          (Wi-Fi) only.
        </p>
        <textarea
          rows="2"
          placeholder="/dns4/your-app.fly.dev/tcp/4001/p2p/12D3Koo…"
          bind:value={bootstrap}
        ></textarea>
      </div>
    {/if}

    {#if error}<div class="error">{error}</div>{/if}
    <button type="submit" disabled={!passphrase || busy}>
      {busy ? "Unlocking…" : "Unlock"}
    </button>
  </form>
</div>

<style>
  .login {
    height: 100%;
    display: grid;
    place-items: center;
    background: radial-gradient(circle at 50% 30%, #26282d, var(--bg));
  }
  .card {
    width: 360px;
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 32px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    text-align: center;
  }
  .logo {
    font-size: 40px;
    color: var(--accent);
  }
  h1 {
    margin: 0;
    font-size: 22px;
  }
  p {
    margin: 0;
    font-size: 13px;
    line-height: 1.5;
  }
  .small {
    font-size: 12px;
    text-align: left;
  }
  .conn-toggle {
    background: transparent;
    color: var(--text-muted);
    text-align: left;
    padding: 4px 2px;
    font-size: 13px;
  }
  .conn-toggle:hover {
    color: var(--text);
  }
  .conn {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  textarea {
    font-family: ui-monospace, monospace;
    font-size: 11px;
    resize: vertical;
  }
</style>
