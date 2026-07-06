<script>
  import { onMount } from "svelte";
  import { api } from "./lib/api.js";

  let { onLogin } = $props();
  let passphrase = $state("");
  let confirm = $state("");
  let error = $state("");
  let busy = $state(false);
  let hasIdentity = $state(true); // assume until checked, then correct
  let checked = $state(false);

  onMount(async () => {
    try {
      hasIdentity = await api.hasIdentity();
    } catch {
      hasIdentity = false;
    }
    checked = true;
  });

  async function submit(e) {
    e?.preventDefault();
    if (!passphrase || busy) return;
    if (!hasIdentity && passphrase !== confirm) {
      error = "Passphrases don't match";
      return;
    }
    busy = true;
    error = "";
    try {
      await api.login(passphrase);
      onLogin();
    } catch (err) {
      error = String(err?.message || err).replace(/^.*: /, "");
    } finally {
      busy = false;
    }
  }

  async function startOver() {
    if (
      !confirm(
        "Start over? This permanently deletes your identity and all data on THIS device so you can create a new passphrase. Servers you own will be lost. Continue?",
      )
    )
      return;
    try {
      await api.resetIdentity();
      hasIdentity = false;
      passphrase = "";
      error = "";
    } catch (err) {
      error = String(err?.message || err);
    }
  }
</script>

<div class="login">
  <form class="card" onsubmit={submit}>
    <div class="logo">◆</div>
    <h1>Concord</h1>

    {#if !checked}
      <p class="muted">Loading…</p>
    {:else if hasIdentity}
      <p class="muted">Welcome back — enter your passphrase to unlock.</p>
      <input type="password" placeholder="Passphrase" bind:value={passphrase} autofocus />
      {#if error}<div class="error">{error}</div>{/if}
      <button type="submit" disabled={!passphrase || busy}>
        {busy ? "Unlocking…" : "Unlock"}
      </button>
      <button type="button" class="link" onclick={startOver}>
        Forgot passphrase? Start over
      </button>
    {:else}
      <p class="muted">
        Create a passphrase to protect your identity. There is no recovery —
        pick something you'll remember.
      </p>
      <input type="password" placeholder="Choose a passphrase" bind:value={passphrase} autofocus />
      <input type="password" placeholder="Confirm passphrase" bind:value={confirm} />
      {#if error}<div class="error">{error}</div>{/if}
      <button type="submit" disabled={!passphrase || !confirm || busy}>
        {busy ? "Creating…" : "Create identity"}
      </button>
    {/if}

    <p class="muted tiny">
      To join a friend, just unlock then paste their invite code — it sets up
      everything for you.
    </p>
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
  .link {
    background: transparent;
    color: var(--text-muted);
    font-size: 12px;
    padding: 2px;
  }
  .link:hover {
    color: var(--text);
    text-decoration: underline;
  }
  .tiny {
    font-size: 11px;
    opacity: 0.7;
    margin-top: 4px;
  }
</style>
