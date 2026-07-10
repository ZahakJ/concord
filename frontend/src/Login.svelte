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
  let forgot = $state(false); // forgot-passphrase menu (recover vs start over)
  let restoring = $state(false);
  let restorePhrase = $state("");

  // After CREATING a fresh identity we hold the door and make the user save
  // their 24-word recovery phrase — it's the only way back into the account.
  let backupPhrase = $state(""); // non-empty ⇒ show the "save this now" step
  let showPhrase = $state(false);
  let revealedOnce = $state(false); // confirm stays disabled until they've looked
  let copiedPhrase = $state(false);
  const backupWords = $derived(backupPhrase.trim().split(/\s+/).filter(Boolean));

  function toggleReveal() {
    showPhrase = !showPhrase;
    if (showPhrase) revealedOnce = true;
  }
  function copyPhrase() {
    navigator.clipboard?.writeText(backupPhrase);
    copiedPhrase = true;
    setTimeout(() => (copiedPhrase = false), 1600);
  }
  function finishBackup() {
    backupPhrase = "";
    onLogin();
  }

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
      // If a (locked) identity is still on this device, this is the
      // forgot-passphrase recovery — validate the phrase, then replace it under
      // the new passphrase. Otherwise it's a fresh-device restore.
      if (hasIdentity) await api.restoreOverExisting(restorePhrase.trim(), passphrase);
      else await api.restoreFromMnemonic(restorePhrase.trim(), passphrase);
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
    if (!passphrase || busy || backupPhrase) return;
    if (!hasIdentity && passphrase !== confirmPass) {
      error = "Passphrases don't match";
      return;
    }
    busy = true;
    error = "";
    try {
      const creating = !hasIdentity;
      await api.login(passphrase);
      if (creating) {
        // Brand-new account: show the recovery phrase as an explicit save
        // step. If reveal fails for any reason, don't trap them at the door.
        try {
          backupPhrase = (await api.revealMnemonic()) || "";
        } catch {
          backupPhrase = "";
        }
        if (backupPhrase) return;
      }
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
  <form class="card" class:wide={!!backupPhrase} onsubmit={submit}>
    <div class="logo"><Icon name="concorde" size={44} /></div>
    <h1>{backupPhrase ? "Save your recovery phrase" : "Concord"}</h1>

    {#if !checked}
      <p class="muted">Loading…</p>
    {:else if backupPhrase}
      <p class="muted">
        These 24 words are the <strong>only</strong> way to get your account back if you lose this
        device or forget your passphrase. Write them down somewhere safe — a password manager or
        paper, not a screenshot.
      </p>
      <div class="words" class:veiled={!showPhrase} aria-label="Recovery phrase">
        {#each backupWords as w, i (i)}
          <span class="word"><span class="wn">{i + 1}</span>{w}</span>
        {/each}
      </div>
      <div class="phrase-actions">
        <button type="button" class="ghost-sm" onclick={toggleReveal}>
          {showPhrase ? "Hide" : "Show phrase"}
        </button>
        <button type="button" class="ghost-sm" disabled={!showPhrase} onclick={copyPhrase}>
          {copiedPhrase ? "Copied ✓" : "Copy"}
        </button>
      </div>
      <p class="muted tiny warn">
        Anyone with these words can become you — never share them. You can view them again later in
        Settings.
      </p>
      <button type="button" disabled={!revealedOnce} onclick={finishBackup}>
        I've saved my recovery phrase
      </button>
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
    {:else if restoring}
      <p class="muted">
        Enter your 24-word recovery phrase and a new passphrase for this device.
        Your identity, servers, and history come back as you sync — nothing is lost.
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
    {:else if forgot}
      <p class="muted">
        Your passphrase can't be looked up — it's never stored anywhere. But if you saved your
        <strong>24-word recovery phrase</strong>, you can get your account back with a new
        passphrase: your identity and servers return and history re-syncs from friends.
      </p>
      {#if error}<div class="error">{error}</div>{/if}
      <button type="button" onclick={() => ((restoring = true), (forgot = false), (error = ""))}>
        I have my recovery phrase
      </button>
      <button type="button" class="link warn-link" onclick={() => ((confirmingReset = true), (forgot = false), (error = ""))}>
        I don't have it — start over (deletes everything)
      </button>
      <button type="button" class="link" onclick={() => (forgot = false)}>Back</button>
    {:else if hasIdentity}
      <p class="muted">Welcome back — enter your passphrase to unlock.</p>
      <input type="password" placeholder="Passphrase" bind:value={passphrase} autofocus />
      {#if error}<div class="error">{error}</div>{/if}
      <button type="submit" disabled={!passphrase || busy}>
        {busy ? "Unlocking…" : "Unlock"}
      </button>
      <button type="button" class="link" onclick={() => ((forgot = true), (error = ""))}>
        Forgot passphrase?
      </button>
    {:else}
      <p class="muted">
        Create a passphrase to protect your identity on this device. Next you'll
        get a 24-word recovery phrase — the key to your account — to save.
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

    {#if !backupPhrase}
      <p class="muted tiny">
        To join a friend, just unlock then paste their invite code — it sets up
        everything for you.
      </p>
    {/if}
  </form>
</div>

<style>
  .login {
    height: 100%;
    display: grid;
    place-items: center;
    /* Subtle vignette in either theme: bg-3 is a step off the page both ways. */
    background: radial-gradient(circle at 50% 30%, color-mix(in srgb, var(--bg-3) 70%, var(--bg)), var(--bg));
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
    /* Neutral, theme-agnostic badge — the login is pre-accent, so it doesn't
       try to match the user's in-app accent (which it can't know yet). */
    color: var(--text);
    display: grid;
    place-items: center;
    width: 72px;
    height: 72px;
    border-radius: 50%;
    background: var(--bg-3);
    border: 1px solid var(--border);
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
  .card.wide {
    width: 430px;
    max-width: calc(100vw - 32px);
  }
  .phrase-in {
    font-family: ui-monospace, monospace;
    font-size: 13px;
    resize: vertical;
  }
  /* The 24-word grid: numbered, monospace, blurred until revealed so nobody
     shoulder-surfs it before the user is ready. */
  .words {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 5px 10px;
    padding: 12px;
    background: var(--bg-input, var(--bg-3));
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    font-family: ui-monospace, monospace;
    font-size: 12.5px;
    text-align: left;
  }
  .word {
    display: flex;
    gap: 6px;
    align-items: baseline;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    transition: filter 0.15s ease;
  }
  .wn {
    color: var(--text-faint);
    font-size: 10px;
    min-width: 14px;
    text-align: right;
  }
  .words.veiled .word {
    filter: blur(6px);
    user-select: none;
  }
  .phrase-actions {
    display: flex;
    gap: 8px;
    justify-content: center;
  }
  .ghost-sm {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
    font-size: 12px;
    padding: 5px 12px;
  }
  .ghost-sm:hover:not(:disabled) {
    color: var(--text);
    border-color: var(--text-muted);
  }
  .ghost-sm:disabled {
    opacity: 0.45;
  }
  .warn {
    color: var(--danger);
    opacity: 0.85;
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
  .warn-link:hover {
    color: var(--danger);
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
