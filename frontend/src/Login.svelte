<script>
  import { onMount } from "svelte";
  import { api } from "./lib/api.js";
  import { S, flash, humanError, setPref } from "./lib/state.svelte.js";
  import { errorField } from "./lib/errors.js";
  import { samePassphrase } from "./lib/passphrase.js";
  import { hasVisible } from "./lib/profilefields.js";
  import { linkCodeFrom } from "./lib/deeplink.js";
  import { focusOnMount } from "./lib/focus.js";
  import { confettiBurst } from "./lib/burst.js";
  import { bioAvailable, bioEnrolled, enableBiometric, unlockWithBiometric } from "./lib/biometric.js";
  import { finishUnlock } from "./lib/unlock.js";
  import Icon from "./Icon.svelte";
  import PassphraseField from "./PassphraseField.svelte";
  import QRScanner from "./QRScanner.svelte";

  // resumeBackup: the session is open but the account was created in a window
  // that went away before its recovery phrase was saved (see App.svelte).
  let { onLogin, resumeBackup = false } = $props();

  let passphrase = $state("");
  let confirmPass = $state("");
  let displayName = $state(""); // asked once, when CREATING a fresh account
  let scanned = $state(false); // a link code came from the QR scanner
  let error = $state("");
  // Which field the error is about ("pass", "confirm", "phrase", "code", or ""
  // for none). Only that one is painted and announced invalid: every error
  // used to turn both passphrase boxes red, a bad link code included, which
  // sent a screen-reader user back to the one field that was fine.
  let errField = $state("");
  function fail(err, field) {
    error = humanError(err);
    errField = field ?? errorField(err);
  }
  let busy = $state(false);
  let hasIdentity = $state(true); // assume until checked, then correct
  let checked = $state(false);

  // enter() is the ONLY way this screen leaves, and all seven paths out of it
  // go through here.
  //
  // onLogin is the whole post-passphrase init — identity, guilds, the first
  // channel's history, the member panel — and it is asynchronous. Called bare,
  // the way all seven call sites used to call it, its failures landed outside
  // the catch sitting three lines below them: an unhandled rejection, an empty
  // error slot, and a login screen that re-enabled its form as though nothing
  // had been typed. That is the whole of what a real user saw after upgrading
  // — a correct passphrase doing nothing, forever. Awaiting it is the fix;
  // reporting it is what makes the fix visible.
  //
  // It swallows deliberately, because the callers differ: four sit inside a
  // try/finally that owns `busy`, three do not, and a helper that rethrew would
  // have to be handled correctly in seven places to be right in one.
  async function enter() {
    try {
      await finishUnlock(onLogin, humanError);
      return true;
    } catch (err) {
      error = humanError(err);
      errField = "";
      return false;
    }
  }

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
      await enter();
    } catch (err) {
      fail(err);
    } finally {
      busy = false;
    }
  }

  async function confirmEnableBio() {
    // Enrolment is an offer, not a gate. It used to be neither guarded nor
    // awaited safely: a rejected biometric prompt threw here and killed the
    // handler before the door was ever opened, so declining the fingerprint
    // reader left the user on the login screen with no way forward.
    try {
      await enableBiometric(pendingPass);
    } catch (err) {
      flash(err);
    }
    pendingPass = "";
    offerBio = false;
    await enter();
  }
  async function skipEnableBio() {
    pendingPass = "";
    offerBio = false;
    await enter();
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

  // The moment anything is typed, the complaint goes. It used to sit there for
  // the whole retype — still reading "Passphrases don't match" over two fields
  // that now did — because nothing but another submit ever cleared it.
  $effect(() => {
    void [passphrase, confirmPass, displayName, linkCode, restorePhrase];
    error = "";
    errField = "";
  });

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
      scanned = true;
      error = "";
      // Auto-advance: the code is captured, so send focus to the passphrase.
      requestAnimationFrame(() => document.querySelector('.login input[type="password"], .login input[type="text"]')?.focus());
    } else {
      error = "That QR code isn't a Concord link code — make sure it's the one under Settings → Your devices.";
      errField = "code";
    }
  }

  async function doLink(e) {
    e?.preventDefault();
    if (busy) return;
    // Accept the raw code or the concord://link?c=… URL form (scans/pastes).
    const code = linkCodeFrom(linkCode);
    if (!code) {
      error = "Paste the code shown on your other device.";
      errField = "code";
      return;
    }
    if (!passphrase) {
      error = "Choose a passphrase for this device.";
      errField = "pass";
      return;
    }
    if (!samePassphrase(passphrase, confirmPass)) {
      error = "The passphrases don't match.";
      errField = "confirm";
      return;
    }
    busy = true;
    error = "";
    try {
      // Dials the other device, adopts the account, logs in linked, and joins
      // your existing guilds — then we're in.
      // A non-empty return is a WARNING, not a failure: the device is linked
      // and logged in either way. The usual case is guilds the other device
      // belongs to but doesn't administer, which it has no authority to hand
      // over — better said out loud than discovered as missing guilds later.
      // confirm: set only once the user has agreed, in the replace step below,
      // that THIS account may replace the one on the device.
      const confirm = replaceAsk?.kind === "link" ? replaceAsk.incoming : "";
      const warning = await api.redeemLinkCode(code, passphrase, confirm);
      replaceAsk = null;
      if (warning) flash(warning);
      // The link just landed — celebrate HERE, on the transition the user
      // watched happen, not on the mount of whatever screen comes next. The
      // scanned-ok tick above isn't this moment: a scan only captures the
      // code, redeeming it is what links the device.
      confettiBurst({ seed: "device-link" });
      await enter();
    } catch (err) {
      if (err?.code === "replace-refused") {
        // Nothing was written. The code it came with is spent (each works
        // once), so the step asks for a fresh one along with the yes.
        askReplace("link", err.detail);
        linkCode = "";
        scanned = false;
        return;
      }
      fail(err);
    } finally {
      busy = false;
    }
  }

  // Why this screen is showing, when the answer is not "you opened the app".
  // api.js reloads onto it when the core reports the identity locked under a
  // running window, and leaves the core's own answer to "why" (SessionState)
  // here: restarted, signed out elsewhere, wiped, or unlinked. Only the last is
  // an erase. This used to assume it always was, and told people after a plain
  // restart that their identity, guilds and messages were gone. Read once and
  // cleared, so a later reload does not re-explain something that has stopped
  // being true. ("1" is what older builds stored, and it meant unlinked.)
  const endedReason = (() => {
    try {
      const v = sessionStorage.getItem("concord.sessionEnded");
      sessionStorage.removeItem("concord.sessionEnded");
      return v === "1" ? "unlinked" : v || "";
    } catch {
      return "";
    }
  })();

  // The account on this device, named without unlocking it (the clear account
  // key in the keystore, and the stored display name). The flows that replace
  // the account — recovery phrase, re-link — say whose it is before they ask.
  let device = $state(null);
  const deviceName = $derived(device?.name || "the account on this device");
  const shortFp = (fp) => (fp ? String(fp).split(" ")[0] : "");

  // replaceAsk: the core refused to put an account on this device because it
  // would erase the one already here (internal/app/replace.go). Holds what it
  // said — whose account is here, whose would replace it — until the user
  // agrees for exactly that account or goes back. kind: "restore" | "link".
  let replaceAsk = $state(null);
  function askReplace(kind, d) {
    replaceAsk = { kind, match: d.match, incoming: d.incoming, incomingName: d.incomingName || "" };
    if (d.device) device = d.device;
  }

  // naming: a restore left this device's account without a name (a recovery
  // phrase carries none), so ask for one before going in — otherwise everyone
  // it talks to sees a four-character fingerprint stub.
  let naming = $state(false);

  // After CREATING a fresh identity we hold the door and make the user save
  // their 24-word recovery phrase — it's the only way back into the account.
  let backupPhrase = $state(""); // non-empty ⇒ show the "save this now" step
  let showPhrase = $state(false);
  let revealedOnce = $state(false); // confirm stays disabled until they've looked
  let copiedPhrase = $state(false);
  const backupWords = $derived(backupPhrase.trim().split(/\s+/).filter(Boolean));

  // ---- the verify step ----
  //
  // "I've saved my recovery phrase" is a button, and a button is not evidence.
  // Asking for two of the words back is: it cannot be answered by a person who
  // clicked past the screen, and it takes about fifteen seconds for a person
  // who actually wrote them down. Two positions, drawn once per phrase and kept
  // stable so the question does not change under the answer.
  //
  // This is still not a wall — see askAgain: someone who cannot answer is shown
  // the words again rather than locked out of their own new account. The point
  // is to make writing them down the path of least resistance, and to KNOW
  // whether it happened, which is what the nudge banner needs.
  let verifying = $state(false);
  let verifyErr = $state("");
  let answerA = $state("");
  let answerB = $state("");
  const asks = $derived.by(() => {
    const n = backupWords.length;
    if (n < 4) return [];
    // Deterministic in the phrase itself: re-rendering must not reshuffle the
    // question, and two different positions are the whole point.
    let a = 0;
    for (const ch of backupPhrase) a = (a * 31 + ch.charCodeAt(0)) >>> 0;
    const i = a % n;
    const j = (i + 1 + ((a >>> 8) % (n - 1))) % n;
    return [Math.min(i, j), Math.max(i, j)];
  });

  function toggleReveal() {
    showPhrase = !showPhrase;
    if (showPhrase) revealedOnce = true;
  }
  function copyPhrase() {
    navigator.clipboard?.writeText(backupPhrase);
    copiedPhrase = true;
    setTimeout(() => (copiedPhrase = false), 1600);
  }
  async function startVerify() {
    if (!asks.length) {
      // Nothing to ask about (a phrase we could not split): don't invent a quiz.
      await finishBackup();
      return;
    }
    verifying = true;
    showPhrase = false; // otherwise it is a copying exercise, not a check
    verifyErr = "";
    answerA = "";
    answerB = "";
  }
  function askAgain() {
    verifying = false;
    showPhrase = true;
    verifyErr = "";
  }
  const norm = (s) => s.trim().toLowerCase();
  async function submitVerify(e) {
    e?.preventDefault();
    if (norm(answerA) === backupWords[asks[0]] && norm(answerB) === backupWords[asks[1]]) {
      await finishBackup();
      return;
    }
    verifyErr = "That doesn't match. Check the numbers against what you wrote down.";
  }
  async function finishBackup(verified = true) {
    // Cleared only on a real answer. Skipping leaves it set, which is what
    // keeps the banner coming back until the words exist somewhere.
    setPref("backupPending", !verified);
    setPref("backupGate", false);
    backupPhrase = "";
    verifying = false;
    await enter();
  }

  async function doRestore(e) {
    e?.preventDefault();
    if (busy) return;
    if (!restorePhrase.trim()) {
      error = "Enter your recovery phrase.";
      errField = "phrase";
      return;
    }
    if (!passphrase) {
      error = "Choose a new passphrase for this device.";
      errField = "pass";
      return;
    }
    if (!samePassphrase(passphrase, confirmPass)) {
      error = "The passphrases don't match.";
      errField = "confirm";
      return;
    }
    busy = true;
    error = "";
    try {
      const phrase = restorePhrase.trim();
      if (hasIdentity) {
        // The forgot-passphrase recovery, over an account that is still here.
        // Ask the core what this phrase would do BEFORE doing it: the same
        // account on this device keeps everything; anything else would erase
        // what is here, and that needs a yes for exactly this phrase's account.
        let confirm = "";
        if (replaceAsk?.kind === "restore") {
          confirm = replaceAsk.incoming;
        } else {
          const chk = await api.checkRecoveryPhrase(phrase);
          if (!chk.keepsData) {
            askReplace("restore", chk);
            return;
          }
        }
        try {
          await api.restoreOverExisting(phrase, passphrase, confirm);
        } catch (err) {
          if (err?.code === "replace-refused") {
            askReplace("restore", err.detail);
            return;
          }
          throw err;
        }
        replaceAsk = null;
      } else {
        await api.restoreFromMnemonic(phrase, passphrase);
      }
      await api.login(passphrase);
      if (await isNameless()) {
        naming = true;
        restoring = false;
        return;
      }
      await enter();
    } catch (err) {
      fail(err);
    } finally {
      busy = false;
    }
  }

  // isNameless: the unlocked account has no stored name — the core then
  // reports the first block of its fingerprint as the display name.
  async function isNameless() {
    try {
      const me = await api.identity();
      return !me?.displayName || me.displayName === shortFp(me.fingerprint);
    } catch {
      return false;
    }
  }

  async function saveName(e) {
    e?.preventDefault();
    const nm = displayName.trim();
    if (!hasVisible(nm) || busy) return;
    busy = true;
    try {
      await api.setProfile(nm, "", "", "", "");
      naming = false;
      await enter();
    } catch (err) {
      fail(err, "");
    } finally {
      busy = false;
    }
  }

  // The resumed recovery-phrase step: the passphrase again, then the words.
  // The phrase is never shown on an open session alone (see RevealMnemonic).
  async function resumeShow(e) {
    e?.preventDefault();
    if (!passphrase || busy) return;
    busy = true;
    try {
      backupPhrase = (await api.revealMnemonic(passphrase)) || "";
      if (!backupPhrase) await finishBackup(false);
    } catch (err) {
      fail(err);
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
    if (hasIdentity) {
      try {
        device = await api.lockedAccount();
      } catch {
        device = null;
      }
    }
    checked = true;
    if (resumeBackup) return;
    bioCanEnroll = await bioAvailable();
    bioOn = bioCanEnroll && bioEnrolled();
    // If biometric unlock is set up, offer it straight away on the lock screen.
    if (hasIdentity && bioOn) tryBiometricUnlock();
  });

  async function submit(e) {
    e?.preventDefault();
    if (resumeBackup && !backupPhrase) return resumeShow(e);
    if (naming) return saveName(e);
    if (!passphrase || busy || backupPhrase) return;
    if (!hasIdentity && !samePassphrase(passphrase, confirmPass)) {
      error = "The passphrases don't match.";
      errField = "confirm";
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
          backupPhrase = (await api.revealMnemonic(passphrase)) || "";
        } catch {
          backupPhrase = "";
        }
        // From here the phrase exists on exactly one device and nowhere else.
        // The flag stays set until it has been verified or looked at again.
        setPref("backupPending", true);
        // backupGate holds the door across a reload: the core is already
        // unlocked, so without it a reload walked straight past this step into
        // the app, for good. App.svelte brings it back until it is finished.
        if (backupPhrase) setPref("backupGate", true);
        if (backupPhrase) return;
      }
      // Offer biometric unlock after a successful password unlock on a device
      // that supports it and hasn't enrolled yet.
      if (bioCanEnroll && !bioOn) {
        pendingPass = passphrase;
        offerBio = true;
        return;
      }
      await enter();
    } catch (err) {
      fail(err);
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
      fail(err, "");
    } finally {
      busy = false;
    }
  }

  // The keyboard arrives after focus, and an edge-to-edge WebView never
  // shrinks for it, so the browser's own "scroll the field into view" runs
  // against a screen that has not lost anything yet. Once the IME has risen
  // (and .login has grown its --kb padding), bring the field up above it.
  function keepFieldInView(e) {
    const el = e.target;
    if (!S.isMobile || !(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement)) return;
    setTimeout(() => {
      if (document.activeElement === el) el.scrollIntoView({ block: "center", behavior: "smooth" });
    }, 350);
  }

  // One decision per screen, each named by its heading; the three steps of a
  // new account say where you are in them.
  const heading = $derived(
    backupPhrase && verifying
      ? "Check your recovery phrase"
      : backupPhrase || resumeBackup
        ? "Save your recovery phrase"
        : !checked
          ? "Concord"
          : naming
            ? "What should people call you?"
            : replaceAsk
              ? "Before anything is erased"
              : offerBio
                ? "Unlock faster next time"
                : confirmingReset
                  ? "Start over on this device?"
                  : linking
                    ? "Link this device"
                    : restoring
                      ? "Restore your account"
                      : forgot
                        ? "Forgot your passphrase?"
                        : hasIdentity
                          ? "Unlock Concord"
                          : "Create your account",
  );
  const step = $derived(
    !checked || resumeBackup
      ? ""
      : backupPhrase && verifying
        ? "3 of 3"
        : backupPhrase
          ? "2 of 3"
          : !hasIdentity && !linking && !restoring && !forgot && !confirmingReset && !replaceAsk && !naming && !offerBio
            ? "1 of 3"
            : "",
  );
</script>

<div class="login" onfocusin={keepFieldInView}>
  <form class="card" onsubmit={submit}>
    <!-- The app's own mark on a tile of its own, so it reads on the dark
         ground as well as on paper. -->
    <div class="logo"><Icon name="concorde" size={28} /></div>
    {#if step}<p class="step">{step}</p>{/if}
    <h1>{heading}</h1>

    {#if endedReason && !backupPhrase && !resumeBackup}
      <p class="notice" role="status">
        {#if endedReason === "unlinked"}
          This device was unlinked from its account. Its copy of your identity,
          your guilds and your messages has been erased here — nothing was deleted
          from anyone else. Link it again from one of your other devices to get
          them back.
        {:else if endedReason === "wiped"}
          Everything on this device was deleted from another Concord window.
        {:else if endedReason === "signed-out"}
          You signed out in another Concord window. Unlock to carry on — you'll
          land where you were.
        {:else}
          Concord restarted. Nothing was lost — unlock to carry on where you were.
        {/if}
      </p>
    {/if}

    {#if !checked}
      <p class="muted">Loading…</p>
    {:else if resumeBackup && !backupPhrase}
      <p class="muted">
        This account was created here, but its recovery phrase was never saved —
        and it is the only way back in if this device is lost. Enter your
        passphrase to see the 24 words.
      </p>
      <PassphraseField placeholder="Passphrase" bind:value={passphrase} invalid={errField === "pass"} autofocus={!S.isMobile} />
      <div class="error slot" role="alert">{error}</div>
      <button class="primary" type="submit" disabled={!passphrase || busy}>Show my recovery phrase</button>
    {:else if naming}
      <p class="muted">
        Your account is back. What should people call you? A recovery phrase
        carries no name, so this device doesn't have one yet.
      </p>
      <input
        type="text"
        placeholder="Your name (what people see)"
        aria-label="Your name"
        maxlength="32"
        autocomplete="off"
        bind:value={displayName}
        use:focusOnMount={{ skip: S.isMobile }}
      />
      <div class="error slot" role="alert">{error}</div>
      <button class="primary" type="submit" disabled={!hasVisible(displayName) || busy}>Continue</button>
    {:else if replaceAsk}
      <!-- The core refused to put an account on this device because it would
           erase the one here. This is the only place that can say yes, and it
           says yes for ONE account: the fingerprint it sends back is the
           incoming one, so an edited phrase or a code for yet another account
           is asked about again. -->
      <div class="replace" role="alertdialog" aria-labelledby="replace-h" aria-describedby="replace-p">
        <h2 id="replace-h">
          {#if replaceAsk.match === "same"}
            Restoring would erase this device's copy
          {:else if replaceAsk.match === "unknown"}
            This would erase what's on this device
          {:else if replaceAsk.kind === "link"}
            That code is for a different account
          {:else}
            That phrase is for a different account
          {/if}
        </h2>
        <p id="replace-p">
          {#if replaceAsk.match === "same"}
            The phrase is for <strong>{deviceName}</strong>, the account on this device —
            but this device was linked to it from another device, and that link can't be
            recovered without the passphrase. Restoring erases this device's guilds,
            messages and notes. If your other device still has the account, re-linking
            from it brings everything back instead.
          {:else if replaceAsk.match === "unknown"}
            Concord can't tell which account is on this device without its passphrase.
            Going ahead erases <strong>everything on this device</strong> — the account
            here, its guilds, messages and notes — and replaces it with the account
            {replaceAsk.kind === "link" ? "the code is for" : "the phrase is for"}
            ({shortFp(replaceAsk.incoming)}).
          {:else}
            This device holds <strong>{deviceName}</strong>{#if device?.fingerprint}&nbsp;({shortFp(
                device.fingerprint,
              )}){/if}. The {replaceAsk.kind === "link" ? "code" : "phrase"} belongs to
            <strong>{replaceAsk.incomingName || "another account"}</strong> ({shortFp(replaceAsk.incoming)}).
            Going ahead <strong>erases {deviceName} from this device</strong> — its guilds,
            messages, notes and settings. Nothing can bring this device's copy back unless
            {deviceName} is also on another of your devices.
          {/if}
        </p>
        <p class="muted small nothing-yet">Nothing has been changed yet.</p>
      </div>
      {#if replaceAsk.kind === "link"}
        <p class="muted small">
          Each link code works once. To go ahead, make a new code on the other device
          and paste it here.
        </p>
        <textarea
          class="phrase-in"
          rows="3"
          placeholder="Paste the new link code…"
          aria-label="New link code"
          aria-invalid={errField === "code" ? "true" : undefined}
          autocapitalize="none"
          autocorrect="off"
          autocomplete="off"
          spellcheck="false"
          bind:value={linkCode}
        ></textarea>
      {/if}
      <div class="error slot" role="alert">{error}</div>
      <button
        type="button"
        class="danger"
        disabled={busy || (replaceAsk.kind === "link" && !linkCode.trim())}
        onclick={replaceAsk.kind === "link" ? doLink : doRestore}
      >
        {#if busy}
          Erasing…
        {:else if replaceAsk.match === "same"}
          Erase this device's copy and restore
        {:else}
          Erase {device?.name || "this device's account"} and {replaceAsk.kind === "link" ? "link" : "restore"}
        {/if}
      </button>
      {#if replaceAsk.match === "same"}
        <button
          type="button"
          class="ghost"
          onclick={() => ((replaceAsk = null), (restoring = false), (linking = true), (error = ""))}
        >
          Re-link from my other device instead
        </button>
      {/if}
      <button type="button" class="link" onclick={() => ((replaceAsk = null), (error = ""))}>
        Go back
      </button>
    {:else if offerBio}
      <p class="muted">
        Unlock Concord with your fingerprint or face next time, instead of typing
        your passphrase. It's stored in this device's secure hardware — your
        passphrase never leaves the device.
      </p>
      <button class="primary" type="button" onclick={confirmEnableBio}>Enable biometric unlock</button>
      <button type="button" class="link" onclick={skipEnableBio}>Not now</button>
    {:else if backupPhrase && verifying}
      <p class="muted">
        Two of them, to be sure they made it off the screen. Count from the top of what you wrote
        down.
      </p>
      <!-- A div, not a form: this whole card already sits inside the login
           form, and a nested one would submit THAT — straight past the check
           and into the app. Enter is wired to the two fields instead. -->
      <div class="verify">
        <label class="vfield">
          <span class="vnum">Word {asks[0] + 1}</span>
          <input
            bind:value={answerA}
            aria-label="Word {asks[0] + 1} of your recovery phrase"
            autocapitalize="none"
            autocorrect="off"
            spellcheck="false"
            autocomplete="off"
            use:focusOnMount={{ skip: S.isMobile }}
            onkeydown={(e) => e.key === "Enter" && submitVerify(e)}
          />
        </label>
        <label class="vfield">
          <span class="vnum">Word {asks[1] + 1}</span>
          <input
            bind:value={answerB}
            aria-label="Word {asks[1] + 1} of your recovery phrase"
            autocapitalize="none"
            autocorrect="off"
            spellcheck="false"
            autocomplete="off"
            onkeydown={(e) => e.key === "Enter" && submitVerify(e)}
          />
        </label>
      </div>
      <div class="error slot" role="alert">{verifyErr}</div>
      <button class="primary" type="button" disabled={!answerA.trim() || !answerB.trim()} onclick={submitVerify}>
        Confirm
      </button>
      <!-- Never a lock-out: someone who cannot answer needs the words MORE, not
           less, and refusing to let them into an account they just made would
           be the app breaking its own promise about who holds the keys. -->
      <button type="button" class="ghost" onclick={askAgain}>Show me the words again</button>
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
      <!-- One control to show them: the primary. Once they have been seen,
           Hide and Copy appear beside the grid and the primary moves on to the
           next step. (There were two "show" controls, a chip and the button.) -->
      {#if revealedOnce}
        <div class="phrase-actions">
          <button type="button" class="quiet sm" onclick={toggleReveal}>
            {showPhrase ? "Hide" : "Show"}
          </button>
          <button type="button" class="quiet sm" disabled={!showPhrase} onclick={copyPhrase}>
            {copiedPhrase ? "Copied" : "Copy"}
          </button>
        </div>
      {/if}
      <p class="muted tiny note-line">
        <Icon name="alert" size={13} />
        <span>
          Anyone with these words can become you — never share them. You can see them again later in
          Settings.
        </span>
      </p>
      {#if revealedOnce}
        <button class="primary" type="button" onclick={startVerify}>I've written it down</button>
      {:else}
        <button class="primary" type="button" onclick={toggleReveal}>Show the phrase</button>
      {/if}
    {:else if confirmingReset}
      <p class="muted">
        This permanently deletes your identity and all data on <strong>this device</strong>
        so you can create a new passphrase. Guilds you own will be lost. This
        cannot be undone.
      </p>
      <div class="error slot" role="alert">{error}</div>
      <button type="button" class="danger" disabled={busy} onclick={doReset}>
        {busy ? "Resetting…" : "Yes, delete and start over"}
      </button>
      <button type="button" class="link" onclick={() => (confirmingReset = false)}>Cancel</button>
    {:else if linking}
      <p class="muted">
        On your other device, open <strong>Settings → Your devices</strong>.
        {#if canScan}Scan the QR code, or paste the code{:else}Copy the code and paste it here{/if}
        with a passphrase for this device (it becomes this device's passphrase,
        replacing any old one). Both devices need to be online.
      </p>
      {#if scanned}
        <!-- The QR scan worked: confirm it clearly and get out of the way, so
             the scan doesn't feel like it did nothing. Just set a passphrase. -->
        <div class="scanned-ok">
          <span class="tick"><Icon name="check" size={15} /></span>
          <span>Code received from your other device</span>
          <button type="button" class="rescan" onclick={() => { scanned = false; linkCode = ""; scanning = true; }}>
            Rescan
          </button>
        </div>
      {:else}
        {#if canScan}
          <button type="button" class="primary scan-btn" onclick={() => (scanning = true)}>
            <Icon name="camera" size={17} /> Scan QR code
          </button>
          <div class="or"><span>or paste it</span></div>
        {/if}
        <!-- A textarea on Android defaults to sentence capitalisation with
             autocorrect and predictive text on, which mangles a code the
             backend then rejects with an opaque error — on the one screen
             where the user has no other way in. -->
        <span class="field-lbl" aria-hidden="true">Link code</span>
        <textarea
          class="phrase-in"
          rows="3"
          placeholder="Paste the link code…"
          aria-label="Link code"
          aria-invalid={errField === "code" ? "true" : undefined}
          autocapitalize="none"
          autocorrect="off"
          autocomplete="off"
          spellcheck="false"
          bind:value={linkCode}
        ></textarea>
      {/if}
      {#if hasIdentity}
        <p class="muted small">
          This device holds <strong>{deviceName}</strong>{#if device?.fingerprint}&nbsp;({shortFp(
              device.fingerprint,
            )}){/if}. A code from the same account keeps
          everything here; if it's from a different account you'll be asked before
          anything is erased.
        </p>
      {/if}
      <span class="field-lbl" aria-hidden="true">Passphrase for this device</span>
      <PassphraseField placeholder="Passphrase for this device" autocomplete="new-password" meter bind:value={passphrase} invalid={errField === "pass"} />
      <span class="field-lbl" aria-hidden="true">Confirm passphrase</span>
      <PassphraseField placeholder="Confirm passphrase" autocomplete="new-password" bind:value={confirmPass} invalid={errField === "confirm"} />
      <div class="error slot" role="alert">{error}</div>
      {#if busy}
        <div class="linking-status">
          <span class="ls-spin"></span>
          <span>Connecting to your other device… this can take up to a minute over the internet — keep both devices on the link screen.</span>
        </div>
      {/if}
      <button class="primary" type="button" disabled={busy} onclick={doLink}>
        {busy ? "Linking…" : "Link this device"}
      </button>
      <button type="button" class="link" onclick={() => ((linking = false), (scanned = false), (error = ""))}>
        Back
      </button>
    {:else if restoring}
      <!-- This screen is where somebody bets their history on what it says, so
           it says what actually happens: the phrase rebuilds the account KEY and
           nothing else. There is no data in those words. Every other surface in
           the app already told this truth (the forgot menu above, the wipe
           dialog, Privacy, Backup) — this one used to promise "nothing is lost",
           and it is the only one anybody reads while deciding. -->
      {#if hasIdentity}
        <p class="muted">
          Enter your 24-word recovery phrase and a new passphrase. If the phrase is
          for <strong>{deviceName}</strong>, the account on this device, everything
          here stays — only the passphrase changes. If it's for a different account,
          you'll be told before anything is erased.
        </p>
      {:else}
        <p class="muted">
          Your 24 words bring back who you are, not your history: this device starts with no
          guilds and no messages. If another of your devices still has the account, link to it
          instead and everything comes with it.
        </p>
      {/if}
      <!-- Same reason as the link code, and worse here: the IME will happily
           "correct" uncommon BIP-39 words (gauge, nuclear, kernel) to
           dictionary neighbours as you type, and the phrase is the only way
           back into the account. -->
      <span class="field-lbl" aria-hidden="true">Recovery phrase</span>
      <textarea
        class="phrase-in"
        rows="3"
        placeholder="word1 word2 word3 …"
        autocapitalize="none"
        autocorrect="off"
        autocomplete="off"
        spellcheck="false"
        aria-label="Recovery phrase"
        aria-invalid={errField === "phrase" ? "true" : undefined}
        bind:value={restorePhrase}
      ></textarea>
      <span class="field-lbl" aria-hidden="true">New passphrase</span>
      <PassphraseField placeholder="New passphrase" autocomplete="new-password" meter bind:value={passphrase} invalid={errField === "pass"} />
      <span class="field-lbl" aria-hidden="true">Confirm passphrase</span>
      <PassphraseField placeholder="Confirm passphrase" autocomplete="new-password" bind:value={confirmPass} invalid={errField === "confirm"} />
      <div class="error slot" role="alert">{error}</div>
      <button class="primary" type="button" disabled={busy} onclick={doRestore}>
        {busy ? "Restoring…" : "Restore account"}
      </button>
      <button type="button" class="link" onclick={() => ((restoring = false), (error = ""))}>
        Back
      </button>
    {:else if forgot}
      <p class="muted">
        Your passphrase can't be looked up — it's never stored anywhere. If this
        account is also on <strong>another device</strong>, re-linking from it is
        the best fix: everything comes back — profile, guilds, and history. The
        <strong>24-word recovery phrase</strong> restores your identity, but
        guilds and history only return by re-linking or re-invites.
      </p>
      <div class="error slot" role="alert">{error}</div>
      <button class="primary" type="button" onclick={() => ((linking = true), (forgot = false), (error = ""))}>
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
      <p class="muted">Enter the passphrase you chose for this device.</p>
      <span class="field-lbl" aria-hidden="true">Passphrase</span>
      <!-- No autofocus on a phone. onMount raises the OS biometric sheet on the
           same frame; autofocus slid the software keyboard up underneath it,
           and dismissing the biometric prompt left the keyboard covering a card
           the app can't scroll out from under it. Let the biometric prompt own
           the first interaction; a tap on the field summons the keyboard when
           it's actually wanted. -->
      <PassphraseField placeholder="Passphrase" bind:value={passphrase} invalid={errField === "pass"} autofocus={!S.isMobile} />
      <div class="error slot" role="alert">{error}</div>
      <button
        class="primary"
        type="submit"
        disabled={!passphrase || busy}
        title={!passphrase && !busy ? "Enter your passphrase first" : undefined}
      >
        {busy ? "Unlocking…" : "Unlock"}
      </button>
      {#if bioOn}
        <button type="button" class="ghost" disabled={busy} onclick={tryBiometricUnlock}>
          Unlock with biometrics
        </button>
      {/if}
      <div class="links">
        <button type="button" class="link" onclick={() => ((forgot = true), (error = ""))}>
          Forgot passphrase?
        </button>
        <!-- Re-link works over an existing keystore too (the device key is kept,
             only the account material is re-adopted) — handy for refreshing a
             device from the desktop without digging through recovery flows. -->
        <button type="button" class="link" onclick={() => ((linking = true), (error = ""))}>
          Re-link from another device
        </button>
      </div>
    {:else}
      <p class="muted">
        Pick a name and a passphrase to protect your identity on this device.
        Next you'll get a 24-word recovery phrase — the key to your account — to
        save.
      </p>
      <!-- Likewise: a brand-new user's very first frame should be the copy
           explaining what they're about to do, not a keyboard covering it. -->
      <span class="field-lbl" aria-hidden="true">Your name</span>
      <input
        type="text"
        placeholder="What people see"
        aria-label="Your name"
        maxlength="32"
        autocomplete="off"
        bind:value={displayName}
        use:focusOnMount={{ skip: S.isMobile }}
      />
      <!-- The field stops at 32; say so before it starts dropping letters. -->
      {#if [...displayName].length >= 24}
        <span class="count">{[...displayName].length}/32</span>
      {/if}
      <span class="field-lbl" aria-hidden="true">Passphrase</span>
      <PassphraseField placeholder="Choose a passphrase" autocomplete="new-password" meter bind:value={passphrase} invalid={errField === "pass"} />
      <span class="field-lbl" aria-hidden="true">Confirm passphrase</span>
      <PassphraseField placeholder="Confirm passphrase" autocomplete="new-password" bind:value={confirmPass} invalid={errField === "confirm"} />
      <div class="error slot" role="alert">{error}</div>
      <button class="primary" type="submit" disabled={!hasVisible(displayName) || !passphrase || !confirmPass || busy}>
        {busy ? "Creating…" : "Create identity"}
      </button>
      <button type="button" class="link" onclick={() => ((restoring = true), (error = ""))}>
        Restore from a recovery phrase
      </button>
      <button type="button" class="link" onclick={() => ((linking = true), (error = ""))}>
        Link to an existing account
      </button>
    {/if}

    {#if !backupPhrase && !resumeBackup && !naming && !replaceAsk}
      <p class="foot"><Icon name="lock" size={14} /> Your keys never leave this device.</p>
    {/if}
  </form>
</div>

{#if scanning}
  <QRScanner onScan={onScanned} onClose={() => (scanning = false)} />
{/if}

<style>
  .linking-status {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    margin: 4px 0 2px;
    font-size: var(--fs-compact);
    line-height: 1.4;
    color: var(--text-muted);
    background: var(--accent-soft);
    border-radius: var(--radius-md, 10px);
    text-align: left;
  }
  .ls-spin {
    flex-shrink: 0;
    width: 15px;
    height: 15px;
    border: 2px solid rgba(151, 161, 178, 0.35);
    border-top-color: var(--accent-hover);
    border-radius: 50%;
    animation: ls-spin 0.7s linear infinite;
  }
  @keyframes ls-spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .ls-spin {
      animation: none;
    }
  }
  /* The lock and onboarding screens: no card, a 340px column on the canvas,
     everything left-aligned to it — the mark, the heading in the display
     face, one sentence, the field, the full-width primary, the links. */
  .login {
    height: 100%;
    display: grid;
    place-items: center;
    background: var(--bg-2);
    overflow-y: auto;
  }
  .error.slot {
    min-height: 1.3em;
    margin: -4px 0;
    text-align: start;
    font-size: var(--fs-small);
    color: var(--danger-text);
  }
  .error.slot:empty {
    min-height: 0;
    margin: -6px 0 0;
  }
  .card {
    width: 340px;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-6) 0;
    text-align: start;
  }
  /* The existing mark, on a raised tile with a hairline so it holds on the
     graphite canvas as well as on paper. */
  .logo {
    display: grid;
    place-items: center;
    width: 48px;
    height: 48px;
    margin-bottom: var(--sp-3);
    border-radius: var(--radius-lg);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    color: var(--text);
  }
  .step {
    margin: 0 0 -6px;
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  h1 {
    margin: 0;
    font-family: var(--display-font);
    font-size: var(--fs-display);
    font-weight: 600;
    letter-spacing: -0.02em;
    line-height: 1.2;
  }
  p {
    margin: 0;
    font-size: var(--fs-ui);
    line-height: 1.5;
  }
  .muted {
    color: var(--text-muted);
  }
  .field-lbl {
    margin: var(--sp-2) 0 -6px;
    font-size: var(--fs-compact);
    font-weight: 500;
  }
  .card input,
  .card textarea {
    min-height: 40px;
  }
  /* The one primary: 40px, full width, the accent. Disabled is the shared
     45% so an enabled Unlock never looks like a disabled one. */
  .card button.primary,
  .card button.danger {
    min-height: 40px;
    width: 100%;
    font-size: var(--fs-ui);
  }
  /* The secondary is the same lg control as the primary: 40px, --fs-ui,
     600. It was 36px at the inherited 16px beside a 40px primary at 13px,
     so the less important button read as the bigger one. */
  .card button.primary,
  .card button.danger,
  .card button.ghost {
    font-weight: 600;
  }
  .card button.ghost {
    min-height: 40px;
    width: 100%;
    font-size: var(--fs-ui);
  }
  .links {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1) var(--sp-5);
    margin-top: var(--sp-1);
  }
  .link {
    font-size: var(--fs-compact);
    align-self: flex-start;
    text-align: start;
  }
  .warn-link {
    color: var(--danger-text);
  }
  .foot {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin-top: 40px;
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  .card.wide {
    width: 440px;
    max-width: calc(100 * var(--vw) - 28px);
  }
  .phrase-in {
    font-family: var(--mono-font);
    font-size: var(--fs-ui);
    resize: vertical;
  }
  .scan-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
  }
  .or {
    display: flex;
    align-items: center;
    gap: 10px;
    color: var(--text-muted);
    font-size: var(--fs-small);
  }
  .or::before,
  .or::after {
    content: "";
    flex: 1;
    height: 1px;
    background: var(--hairline);
  }
  .scanned-ok {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 10px 12px;
    border-radius: var(--radius-md);
    background: var(--bg-1);
    border: 1px solid var(--border);
    font-size: var(--fs-ui);
  }
  .scanned-ok .tick {
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    flex: none;
    border-radius: 50%;
    background: var(--ok);
    color: var(--ok-fg);
  }
  .scanned-ok .rescan {
    margin-left: auto;
    padding: 0 10px;
    min-height: 28px;
    font-size: var(--fs-compact);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text-muted);
  }
  .notice {
    padding: 10px 12px;
    border-radius: var(--radius-md);
    background: var(--bg-1);
    border: 1px solid var(--border);
    color: var(--text);
    font-size: var(--fs-compact);
    line-height: 1.5;
  }
  /* The erase question keeps its weight: a danger-tinted panel, and the
     verb on a danger button under it. */
  .replace {
    padding: var(--sp-3);
    border-radius: var(--radius-md);
    border: 1px solid color-mix(in srgb, var(--danger) 55%, transparent);
    background: var(--danger-soft);
  }
  .replace h2 {
    margin: 0 0 var(--sp-2);
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .small {
    font-size: var(--fs-compact);
  }
  .count {
    margin: -8px 0 0;
    font-size: var(--fs-small);
    color: var(--text-faint);
    text-align: end;
  }
  .nothing-yet {
    margin-top: var(--sp-2);
  }
  .phrase-in[aria-invalid="true"] {
    border-color: var(--danger);
  }
  /* The 24 words: numbered cells in a 3×8 grid on --bg-1 with hairlines. */
  .words {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 1px;
    background: var(--hairline);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    font-family: var(--mono-font);
    font-size: var(--fs-compact);
  }
  .word {
    display: flex;
    gap: 6px;
    align-items: baseline;
    padding: 6px var(--sp-2);
    background: var(--bg-1);
    overflow-wrap: anywhere;
    transition: filter var(--dur-standard) ease;
  }
  .wn {
    color: var(--text-faint);
    font-size: var(--fs-tiny);
    min-width: 16px;
    flex: none;
    text-align: end;
  }
  .words.veiled .word {
    filter: blur(6px);
    user-select: none;
  }
  .phrase-actions {
    display: flex;
    gap: var(--sp-2);
  }
  /* A caution, not an error: muted, with the alert glyph carrying it. */
  .note-line {
    display: flex;
    align-items: flex-start;
    gap: 6px;
  }
  .note-line :global(svg) {
    flex: none;
    margin-top: 2px;
  }
  .tiny {
    font-size: var(--fs-small);
  }
  .verify {
    display: flex;
    gap: 10px;
  }
  .vfield {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    text-align: left;
  }
  .vnum {
    font-size: var(--fs-small);
    color: var(--text-faint);
  }
  @media (max-width: 768px) {
    /* The same column, 14px from the edges, its top a fifth of the way down
       below the safe area. It scrolls when a step and the keyboard need it. */
    .login {
      display: flex;
      overscroll-behavior: contain;
      border-top: var(--safe-top) solid transparent;
      padding: 0 14px calc(12px + var(--safe-bottom) + var(--kb, 0px));
    }
    .card,
    .card.wide {
      margin: calc(14 * var(--vh)) auto auto;
      width: 100%;
      max-width: 460px;
    }
    input,
    .phrase-in {
      font-size: 16px;
    }
    .card button.primary,
    .card button.danger,
    .card button.ghost {
      min-height: 48px;
    }
    .link {
      min-height: var(--tap-min);
    }
    .words {
      grid-template-columns: repeat(2, 1fr);
      font-size: var(--fs-ui);
    }
    .scanned-ok .rescan {
      position: relative;
    }
    .scanned-ok .rescan::after {
      content: "";
      position: absolute;
      inset: -10px -6px;
    }
  }
</style>
