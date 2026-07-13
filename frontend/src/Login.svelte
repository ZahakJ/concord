<script>
  import { onMount } from "svelte";
  import { api } from "./lib/api.js";
  import { S } from "./lib/state.svelte.js";
  import { linkCodeFrom } from "./lib/deeplink.js";
  import { bioAvailable, bioEnrolled, enableBiometric, unlockWithBiometric } from "./lib/biometric.js";
  import Icon from "./Icon.svelte";
  import QRScanner from "./QRScanner.svelte";

  let { onLogin } = $props();
  let passphrase = $state("");
  let confirmPass = $state("");
  let displayName = $state(""); // asked once, when CREATING a fresh account
  let error = $state("");
  let busy = $state(false);
  let hasIdentity = $state(true); // assume until checked, then correct
  let checked = $state(false);

  // Biometric unlock (mobile). bioCanEnroll: hardware present; bioOn: the user
  // has already stored their passphrase behind the biometric on this device.
  let bioCanEnroll = $state(false);
  let bioOn = $state(false);
  // After a successful password unlock we offer to enable biometric — holds the
  // just-verified passphrase so an opt-in can store it.
  let offerBio = $state(false);
  let pendingPass = $state("");

  async function tryBiometricUnlock() {
    if (busy) return;
    // NEVER let biometric create an identity — login() creates one when no
    // keystore exists, which silently spawned a throwaway account. Biometric
    // must only UNLOCK an account that already exists on this device.
    if (!hasIdentity) {
      try {
        if (!(await api.hasIdentity())) return;
      } catch {
        return;
      }
    }
    busy = true;
    error = "";
    try {
      const pass = await unlockWithBiometric();
      if (!pass) return; // cancelled or failed — fall back to the passphrase field
      await api.login(pass);
      onLogin();
    } catch (err) {
      error = String(err?.message || err).replace(/^.*: /, "");
    } finally {
      busy = false;
    }
  }

  async function confirmEnableBio() {
    await enableBiometric(pendingPass);
    pendingPass = "";
    offerBio = false;
    onLogin();
  }
  function skipEnableBio() {
    pendingPass = "";
    offerBio = false;
    onLogin();
  }
  let confirmingReset = $state(false);
  let forgot = $state(false); // forgot-passphrase menu (recover vs start over)
  let restoring = $state(false);
  let restorePhrase = $state("");
  let linking = $state(false); // "link this device to an existing account" flow
  let linkCode = $state("");
  let scanning = $state(false); // in-app QR scanner overlay
  // Scan is offered where a camera makes sense: touch devices with getUserMedia.
  const canScan =
    typeof navigator !== "undefined" &&
    !!navigator.mediaDevices?.getUserMedia &&
    matchMedia("(pointer: coarse)").matches;

  // A concord://link deep link (OS camera scanned the QR) drops the code here —
  // jump straight into the linking step with it filled in.
  $effect(() => {
    if (S.pendingLinkCode) {
      linkCode = S.pendingLinkCode;
      S.pendingLinkCode = "";
      linking = true;
      forgot = false;
      error = "";
    }
  });

  function onScanned(text) {
    scanning = false;
    const code = linkCodeFrom(text);
    if (code) {
      linkCode = code;
      error = "";
    } else {
      error = "That QR code isn't a Concord link code";
    }
  }

  async function doLink(e) {
    e?.preventDefault();
    if (busy) return;
    // Accept the raw code or the concord://link?c=… URL form (scans/pastes).
    const code = linkCodeFrom(linkCode);
    if (!code) {
      error = "Paste the code shown on your other device";
      return;
    }
    if (!passphrase || passphrase !== confirmPass) {
      error = "Passphrases don't match";
      return;
    }
    busy = true;
    error = "";
    try {
      // Dials the other device, adopts the account, logs in linked, and joins
      // your existing servers — then we're in.
      await api.redeemLinkCode(code, passphrase);
      onLogin();
    } catch (err) {
      error = String(err?.message || err).replace(/^.*: /, "");
    } finally {
      busy = false;
    }
  }

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
    bioCanEnroll = await bioAvailable();
    bioOn = bioCanEnroll && bioEnrolled();
    // If biometric unlock is set up, offer it straight away on the lock screen.
    if (hasIdentity && bioOn) tryBiometricUnlock();
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
        // Give the fresh account the name they chose, so they never appear as a
        // fingerprint stub. Best-effort — a failure here shouldn't block entry.
        const nm = displayName.trim();
        if (nm) {
          try {
            await api.setProfile(nm, "", "", "", "");
          } catch {}
        }
        // Brand-new account: show the recovery phrase as an explicit save
        // step. If reveal fails for any reason, don't trap them at the door.
        try {
          backupPhrase = (await api.revealMnemonic()) || "";
        } catch {
          backupPhrase = "";
        }
        if (backupPhrase) return;
      }
      // Offer biometric unlock after a successful password unlock on a device
      // that supports it and hasn't enrolled yet.
      if (bioCanEnroll && !bioOn) {
        pendingPass = passphrase;
        offerBio = true;
        return;
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
    {:else if offerBio}
      <p class="muted">
        Unlock Concord with your fingerprint or face next time, instead of typing
        your passphrase. It's stored in this device's secure hardware — your
        passphrase never leaves the device.
      </p>
      <button type="button" onclick={confirmEnableBio}>Enable biometric unlock</button>
      <button type="button" class="link" onclick={skipEnableBio}>Not now</button>
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
    {:else if linking}
      <p class="muted">
        On your other device, open <strong>Settings → Link a device</strong>.
        {#if canScan}Scan the QR code, or paste the code{:else}Copy the code and paste it here{/if}
        with a passphrase for this device (it becomes this device's passphrase,
        replacing any old one). Both devices need to be online.
      </p>
      {#if canScan}
        <button type="button" class="scan-btn" onclick={() => (scanning = true)}>
          <Icon name="camera" size={17} /> Scan QR code
        </button>
        <div class="or"><span>or paste it</span></div>
      {/if}
      <textarea
        class="phrase-in"
        rows="3"
        placeholder="Paste the link code…"
        bind:value={linkCode}
      ></textarea>
      <input type="password" placeholder="Passphrase for this device" bind:value={passphrase} />
      <input type="password" placeholder="Confirm passphrase" bind:value={confirmPass} />
      {#if error}<div class="error">{error}</div>{/if}
      <button type="button" disabled={busy} onclick={doLink}>
        {busy ? "Linking…" : "Link this device"}
      </button>
      <button type="button" class="link" onclick={() => ((linking = false), (error = ""))}>
        Back
      </button>
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
        Your passphrase can't be looked up — it's never stored anywhere. If this
        account is also on <strong>another device</strong>, re-linking from it is
        the best fix: everything comes back — profile, servers, and history. The
        <strong>24-word recovery phrase</strong> restores your identity, but
        servers and history only return by re-linking or re-invites.
      </p>
      {#if error}<div class="error">{error}</div>{/if}
      <button type="button" onclick={() => ((linking = true), (forgot = false), (error = ""))}>
        Re-link from my other device
      </button>
      <button type="button" class="ghost" onclick={() => ((restoring = true), (forgot = false), (error = ""))}>
        Use my recovery phrase
      </button>
      <button type="button" class="link warn-link" onclick={() => ((confirmingReset = true), (forgot = false), (error = ""))}>
        I have neither — start over (deletes everything)
      </button>
      <button type="button" class="link" onclick={() => (forgot = false)}>Back</button>
    {:else if hasIdentity}
      <p class="muted">Welcome back — enter your passphrase to unlock.</p>
      <input type="password" placeholder="Passphrase" bind:value={passphrase} autofocus />
      {#if error}<div class="error">{error}</div>{/if}
      <button type="submit" disabled={!passphrase || busy}>
        {busy ? "Unlocking…" : "Unlock"}
      </button>
      {#if bioOn}
        <button type="button" class="ghost-sm" disabled={busy} onclick={tryBiometricUnlock}>
          Unlock with biometrics
        </button>
      {/if}
      <button type="button" class="link" onclick={() => ((forgot = true), (error = ""))}>
        Forgot passphrase?
      </button>
      <!-- Re-link works over an existing keystore too (the device key is kept,
           only the account material is re-adopted) — handy for refreshing a
           device from the desktop without digging through recovery flows. -->
      <button type="button" class="link" onclick={() => ((linking = true), (error = ""))}>
        Re-link from another device
      </button>
    {:else}
      <p class="muted">
        Pick a name and a passphrase to protect your identity on this device.
        Next you'll get a 24-word recovery phrase — the key to your account — to
        save.
      </p>
      <input
        type="text"
        placeholder="Your name (what people see)"
        maxlength="32"
        autocomplete="off"
        bind:value={displayName}
        autofocus
      />
      <input type="password" placeholder="Choose a passphrase" bind:value={passphrase} />
      <input type="password" placeholder="Confirm passphrase" bind:value={confirmPass} />
      {#if error}<div class="error">{error}</div>{/if}
      <button type="submit" disabled={!displayName.trim() || !passphrase || !confirmPass || busy}>
        {busy ? "Creating…" : "Create identity"}
      </button>
      <button type="button" class="link" onclick={() => ((restoring = true), (error = ""))}>
        Restore from a recovery phrase
      </button>
      <button type="button" class="link" onclick={() => ((linking = true), (error = ""))}>
        Link to an existing account
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

{#if scanning}
  <QRScanner onScan={onScanned} onClose={() => (scanning = false)} />
{/if}

<style>
  .login {
    /* The door is deliberately UNBRANDED: a neutral silver stands in for the
       accent here, because whatever color the user later picks (profile or
       preset) shouldn't be presumed — or clashed with — before they're even
       in. The derived accent vars are re-declared since :root computed them
       from ITS --accent; a scoped override alone wouldn't reach them. */
    --accent: #97a1b2;
    --accent-hover: #b9c1cd;
    --accent-soft: rgba(151, 161, 178, 0.16);
    --accent-glow: 0 0 24px rgba(151, 161, 178, 0.3);
    height: 100%;
    display: grid;
    place-items: center;
    /* Subtle vignette, same silver whisper. */
    background:
      radial-gradient(circle at 50% 18%, color-mix(in srgb, var(--accent) 7%, transparent), transparent 55%),
      radial-gradient(circle at 50% 30%, color-mix(in srgb, var(--bg-3) 70%, var(--bg)), var(--bg));
  }
  .card {
    width: 340px;
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 32px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    text-align: center;
    box-shadow: var(--shadow-pop);
  }
  .logo {
    /* Hero badge: a soft conic accent ring around the jet, breathing a slow
       ambient glow (the app's one continuously-running animation). */
    position: relative;
    color: var(--text);
    display: grid;
    place-items: center;
    width: 72px;
    height: 72px;
    border-radius: 50%;
    border: 2px solid transparent;
    background:
      linear-gradient(var(--bg-3), var(--bg-3)) padding-box,
      conic-gradient(
          from 210deg,
          var(--accent),
          color-mix(in srgb, var(--accent) 25%, transparent) 40%,
          color-mix(in srgb, var(--accent) 55%, transparent) 75%,
          var(--accent)
        )
        border-box;
    animation:
      takeoff 0.6s ease both,
      logo-breathe 4.5s ease-in-out 0.6s infinite;
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
  @keyframes logo-breathe {
    0%,
    100% {
      box-shadow: 0 0 16px color-mix(in srgb, var(--accent) 16%, transparent);
    }
    50% {
      box-shadow: 0 0 34px color-mix(in srgb, var(--accent) 36%, transparent);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .logo {
      animation: none;
    }
  }
  /* Primary CTAs: gradient fill + a soft lift on hover. (Quiet buttons — the
     .link/.ghost-sm/.danger-btn variants — keep their own styling.) */
  .card button:not(.link):not(.ghost-sm):not(.danger-btn) {
    background: linear-gradient(135deg, var(--accent), color-mix(in srgb, var(--accent) 72%, var(--accent-hover)));
    font-weight: 600;
    box-shadow: 0 2px 10px color-mix(in srgb, var(--accent) 28%, transparent);
    transition:
      transform 0.15s ease,
      box-shadow 0.15s ease,
      filter 0.15s ease;
  }
  .card button:not(.link):not(.ghost-sm):not(.danger-btn):hover:not(:disabled) {
    transform: translateY(-1px);
    filter: brightness(1.07);
    box-shadow: 0 4px 16px color-mix(in srgb, var(--accent) 38%, transparent);
  }
  .card button:not(.link):not(.ghost-sm):not(.danger-btn):active:not(:disabled) {
    transform: none;
    filter: brightness(0.97);
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
  .scan-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }
  /* "or paste it" divider between the scan CTA and the manual box. */
  .or {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text-faint);
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }
  .or::before,
  .or::after {
    content: "";
    flex: 1;
    height: 1px;
    background: var(--border);
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

  /* ---- touch adjustments ---- */
  @media (pointer: coarse) {
    /* flex + margin:auto centers like place-items but still scrolls when the
       card outgrows the screen (recovery-phrase step with the keyboard up). */
    .login {
      display: flex;
      overflow-y: auto;
      padding: calc(12px + env(safe-area-inset-top)) 16px
        calc(12px + env(safe-area-inset-bottom));
    }
    .card {
      margin: auto;
      width: min(400px, 100%);
      padding: 28px 22px;
      gap: 16px;
    }
    .card.wide {
      width: min(440px, 100%);
    }
    /* 16px inputs: readable at arm's length, and iOS won't auto-zoom. */
    input,
    .phrase-in {
      font-size: 16px;
      padding: 12px;
    }
    p {
      font-size: 14px;
    }
    button {
      min-height: 48px;
    }
    .phrase-actions .ghost-sm {
      min-height: 42px;
      font-size: 13px;
    }
    .link {
      min-height: 44px;
      font-size: 13px;
    }
  }
</style>
