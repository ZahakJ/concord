<script>
  import { api } from "./lib/api.js";

  let { onLogin } = $props();
  let passphrase = $state("");
  let error = $state("");
  let busy = $state(false);

  async function submit(e) {
    e?.preventDefault();
    if (!passphrase || busy) return;
    busy = true;
    error = "";
    try {
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
    <input
      type="password"
      placeholder="Passphrase"
      bind:value={passphrase}
      autofocus
    />
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
    width: 340px;
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
</style>
