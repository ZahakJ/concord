<script>
  // PassNudge — a rare, skippable card asking whether you still know your
  // passphrase, for someone who mostly unlocks with a fingerprint. The
  // passphrase is what a computer, recovery and a change of fingerprints
  // need, and it is easy to forget once nothing asks for it. Never on the
  // lock screen; checking it goes to the core like an unlock does.
  import { api } from "./lib/api.js";
  import { nudgeDue, passphraseUsed, snoozeNudge } from "./lib/passnudge.js";

  let open = $state(nudgeDue());
  let checking = $state(false);
  let pass = $state("");
  let busy = $state(false);
  let note = $state("");

  async function check(e) {
    e.preventDefault();
    if (!pass || busy) return;
    busy = true;
    note = "";
    try {
      await api.login(pass); // with a session open, this only checks it
      passphraseUsed();
      open = false;
    } catch {
      note = "That isn't it. If you've forgotten it, you can set a new one in Settings with your recovery phrase.";
    } finally {
      pass = "";
      busy = false;
    }
  }
  function later() {
    snoozeNudge();
    open = false;
  }
</script>

{#if open}
  <aside class="pass-nudge" aria-label="Your passphrase">
    <p class="pn-title">Do you still know your passphrase?</p>
    <p class="pn-body">
      You unlock with your fingerprint, but a computer, recovery and new fingerprints need the passphrase.
    </p>
    {#if checking}
      <form onsubmit={check}>
        <input
          type="password"
          bind:value={pass}
          autocomplete="current-password"
          aria-label="Passphrase"
          placeholder="Passphrase"
        />
        <div class="pn-actions">
          <button class="primary" type="submit" disabled={!pass || busy}>{busy ? "Checking…" : "Check"}</button>
          <button class="quiet" type="button" onclick={later}>Not now</button>
        </div>
      </form>
      {#if note}<p class="pn-note" role="status">{note}</p>{/if}
    {:else}
      <div class="pn-actions">
        <button class="primary" type="button" onclick={() => (checking = true)}>Check it</button>
        <button class="quiet" type="button" onclick={later}>Not now</button>
      </div>
    {/if}
  </aside>
{/if}

<style>
  .pass-nudge {
    position: fixed;
    left: var(--sp-3);
    right: var(--sp-3);
    bottom: calc(var(--safe-bottom) + var(--sp-6));
    z-index: var(--z-raised);
    max-width: 420px;
    margin: 0 auto;
    padding: var(--sp-4);
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    background: var(--bg-elevated);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
  }
  .pn-title {
    margin: 0;
    font-weight: 600;
  }
  .pn-body,
  .pn-note {
    margin: 0;
    font-size: var(--fs-ui);
    color: var(--text-muted);
  }
  form {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .pn-actions {
    display: flex;
    gap: var(--sp-2);
  }
</style>
