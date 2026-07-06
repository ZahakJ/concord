<script>
  import { onMount } from "svelte";
  import { api } from "./lib/api.js";
  import Icon from "./Icon.svelte";

  let { onLogin } = $props();
  let passphrase = $state("");
  let confirmPass = $state("");
  let error = $state("");
  let busy = $state(false);
  let hasIdentity = $state(true); // assume until checked, then correct
  let checked = $state(false);
  let confirmingReset = $state(false);
  let restoring = $state(false);
  let restorePhrase = $state("");

  async function doRestore(e) {
    e?.preventDefault();
    if (busy) return;
    if (!restorePhrase.trim()) {
      error = "Enter your recovery phrase";
      return;
    }
    if (passphrase !== confirmPass) {
      error = "Passphrases don't match";
      return;
    }
    busy = true;
    error = "";
    try {
      await api.restoreFromMnemonic(restorePhrase.trim(), passphrase);
      await api.login(passphrase);
      onLogin();
    } catch (err) {
      error = String(err?.message || err).replace(/^.*: /, "");
    } finally {
      busy = false;
    }
  }

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
    if (!hasIdentity && passphrase !== confirmPass) {
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

  async function doReset() {
    busy = true;
    error = "";
    try {
      await api.resetIdentity();
      hasIdentity = false;
      confirmingReset = false;
      passphrase = "";
      confirmPass = "";
    } catch (err) {
      error = String(err?.message || err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="login">
  <form class="card" onsubmit={submit}>
    <div class="logo"><Icon name="concorde" size={44} /></div>
    <h1>Concord</h1>

    {#if !checked}
      <p class="muted">Loading…</p>
    {:else if confirmingReset}
      <p class="muted">
        This permanently deletes your identity and all data on <strong>this device</strong>
        so you can create a new passphrase. Servers you own will be lost. This
        cannot be undone.
      </p>
      {#if error}<div class="error">{error}</div>{/if}
      <button type="button" class="danger-btn" disabled={busy} onclick={doReset}>
        {busy ? "Resetting…" : "Yes, delete and start over"}
      </button>
      <button type="button" class="link" onclick={() => (confirmingReset = false)}>Cancel</button>
    {:else if hasIdentity}
      <p class="muted">Welcome back — enter your passphrase to unlock.</p>
      <input type="password" placeholder="Passphrase" bind:value={passphrase} autofocus />
      {#if error}<div class="error">{error}</div>{/if}
      <button type="submit" disabled={!passphrase || busy}>
        {busy ? "Unlocking…" : "Unlock"}
      </button>
      <button type="button" class="link" onclick={() => ((confirmingReset = true), (error = ""))}>
        Forgot passphrase? Start over
      </button>
    {:else if restoring}
      <p class="muted">
        Enter your 24-word recovery phrase and a new passphrase for this device.
        Your identity, servers, and history come back as you sync.
      </p>
      <textarea
        class="phrase-in"
        rows="3"
        placeholder="word1 word2 word3 …"
        bind:value={restorePhrase}
      ></textarea>
      <input type="password" placeholder="New passphrase" bind:value={passphrase} />
      <input type="password" placeholder="Confirm passphrase" bind:value={confirmPass} />
      {#if error}<div class="error">{error}</div>{/if}
      <button type="button" disabled={busy} onclick={doRestore}>
        {busy ? "Restoring…" : "Restore account"}
      </button>
      <button type="button" class="link" onclick={() => ((restoring = false), (error = ""))}>
        Back
      </button>
    {:else}
      <p class="muted">
        Create a passphrase to protect your identity. Save the recovery phrase
        afterwards (Settings → Recovery phrase) so you can restore it later.
      </p>
      <input type="password" placeholder="Choose a passphrase" bind:value={passphrase} autofocus />
      <input type="password" placeholder="Confirm passphrase" bind:value={confirmPass} />
      {#if error}<div class="error">{error}</div>{/if}
      <button type="submit" disabled={!passphrase || !confirmPass || busy}>
        {busy ? "Creating…" : "Create identity"}
      </button>
      <button type="button" class="link" onclick={() => ((restoring = true), (error = ""))}>
        Restore from a recovery phrase
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
    color: var(--accent);
    display: grid;
    place-items: center;
    width: 72px;
    height: 72px;
    border-radius: 50%;
    background: var(--accent-soft);
    animation: takeoff 0.6s ease both;
  }
  @keyframes takeoff {
    from {
      transform: translateY(6px) rotate(-8deg);
      opacity: 0;
    }
    to {
      transform: none;
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .logo {
      animation: none;
    }
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
  .phrase-in {
    font-family: ui-monospace, monospace;
    font-size: 13px;
    resize: vertical;
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
  .danger-btn {
    background: var(--danger);
  }
  .tiny {
    font-size: 11px;
    opacity: 0.7;
    margin-top: 4px;
  }
</style>
