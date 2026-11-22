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
  import { strandedChoice, lockNotice, erasedAfterUnlink, eraseUnfinished, quitApp, quitClosed } from "./lib/strandeddata.js";
  import { foundName, keepTypedArgs, restoreStep } from "./lib/restoreprofile.js";
  import { eraseWordMatches, replaceEraseLabel, replaceEraseReady } from "./lib/eraseword.js";
  import { pushLayer } from "./lib/navstack.svelte.js";
  import EraseConfirm from "./EraseConfirm.svelte";
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
      const notice = await api.login(pass);
      if (notice) flash(notice, "info"); // e.g. the keystore's label had been altered
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
  // Erasing from here takes a fixed word typed out (EraseConfirm), the same
  // strength as the in-app wipe, and a ticket the core issues when the
  // confirmation opens, bound to what the device holds then. The core checks
  // both (ResetIdentity refuses without them); the word is never taken from a
  // name, which could be untypeable.
  let resetTyped = $state("");
  let resetTicket = $state("");

  async function openReset() {
    error = "";
    try {
      const v = await api.beginStartOver();
      resetTicket = v.ticket;
      resetTyped = "";
      confirmingReset = true;
      fromForgot = forgot;
      forgot = false;
    } catch (err) {
      fail(err, "");
    }
  }
  // Cancel goes back to where the erase was asked from: the ways back, or the
  // first screen's choice, which stays set under the confirmation.
  function leaveReset() {
    confirmingReset = false;
    resetTyped = "";
    resetTicket = "";
    error = "";
    if (fromForgot) forgot = true;
    fromForgot = false;
  }
  // dataNoKeystore: the core refused to create because this device holds an
  // account's data without its keystore; the choices are worded for that.
  let dataNoKeystore = $state(false);
  // noKeystoreKeepable: the core says this data could be kept by restoring or
  // re-linking its own account (never linked, and messages to check against;
  // app.NoKeystoreKeepable). Otherwise both erase it, after asking, and no
  // sentence here may promise otherwise. noKeystoreLinked says why.
  let noKeystoreKeepable = $state(false);
  let noKeystoreLinked = $state(false);
  // stranded: the first screen's choice for data this device can no longer
  // open (strandedChoice), asked before anything else; null when there is
  // none. startNotice: what the core said about how this device was erased.
  let stranded = $state(null);
  let startNotice = $state("");
  // keptFrom: the choice "Keep it and restore" was picked on, so Back from
  // the ways back returns to it. quitAsked: Quit was picked; the choice stays
  // on screen without its buttons, whatever the shell did with the quit.
  let keptFrom = null;
  let quitAsked = $state(false);

  function keepOrQuit() {
    if (stranded?.keepable) {
      keptFrom = stranded;
      stranded = null;
      forgot = true; // the recovery-phrase restore keeps this data
      return;
    }
    quitAsked = true;
    quitApp(window);
  }
  // leaveForgot is Back from the ways back: to the choice when it came from
  // there, else to the screen before. Nothing opened after this came from the
  // ways back, so fromForgot goes too (a stale erase leaves it set).
  function leaveForgot() {
    forgot = false;
    fromForgot = false;
    if (keptFrom) {
      stranded = keptFrom;
      keptFrom = null;
    }
  }
  let forgot = $state(false); // forgot-passphrase menu (recover vs start over)
  let restoring = $state(false);
  let restorePhrase = $state("");
  let linking = $state(false); // "link this device to an existing account" flow
  // fromForgot: Restore, Link or the erase confirmation was opened from the
  // ways back, so its Back (or Cancel) returns there. It used to drop to the screen under the menu, which over
  // data without a keystore is a create form that can only be refused.
  let fromForgot = false;
  function leaveRestoreOrLink() {
    restoring = false;
    linking = false;
    scanned = false;
    error = "";
    if (fromForgot) forgot = true;
    fromForgot = false;
  }
  let linkCode = $state("");
  // While the other device asks its owner: {sas, deviceName, seconds}. The
  // six digits appear only once that device has taken the code for this one.
  let linkWait = $state(null);
  // The digits arrive under the form, below the fold on a phone: bring them
  // into view, since they are what the person must compare.
  function revealOnMount(node) {
    node.scrollIntoView?.({ block: "center" });
  }
  let scanning = $state(false); // in-app QR scanner overlay
  // Restore, Link and Forgot are screens stepped into from the first one, each
  // with its own Back link. The phone's back did not know about them: it fell
  // through to "Press back again to exit", and a second press closed the app
  // from the middle of onboarding. Each is a layer while it is up, so back does
  // what the Back link does.
  $effect(() => {
    if (!restoring) return;
    return pushLayer("onboarding", leaveRestoreOrLink);
  });
  $effect(() => {
    if (!linking) return;
    return pushLayer("onboarding", leaveRestoreOrLink);
  });
  $effect(() => {
    if (!forgot) return;
    return pushLayer("onboarding", leaveForgot);
  });
  // The erase confirmation too: back does what its Cancel does, and like
  // Cancel it does nothing while the erase is running.
  $effect(() => {
    if (!confirmingReset) return;
    return pushLayer("onboarding", leaveReset, { blocking: busy });
  });
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
      // Over the ways back (or a screen opened from them), Back from the link
      // returns there, not to whatever lies under them.
      fromForgot = fromForgot || forgot;
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
    // Over the replace question, only its yes goes ahead: the word typed.
    if (replaceAsk && !eraseWordMatches(replaceTyped)) return;
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
    linkWait = null;
    // The link call blocks until the other device's owner answers; the digits
    // to compare come from a poll beside it.
    const waitPoll = setInterval(async () => {
      try {
        const w = await api.linkWaiting();
        if (busy) linkWait = w;
      } catch {
        /* an older core: no digits to show */
      }
    }, 400);
    try {
      // Dials the other device, adopts the account, logs in linked, and joins
      // your existing guilds — then we're in.
      // A non-empty return is a WARNING, not a failure: the device is linked
      // and logged in either way. The usual case is guilds the other device
      // belongs to but doesn't administer, which it has no authority to hand
      // over — better said out loud than discovered as missing guilds later.
      // confirm: set only once the user has agreed, in the replace step below,
      // that THIS account may replace the one on the device.
      const yes = replaceAsk?.kind === "link";
      const warning = await api.redeemLinkCode(code, passphrase, yes ? replaceAsk.confirm : "", yes ? replaceTyped : "");
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
      clearInterval(waitPoll);
      linkWait = null;
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
  // replaceTyped: the erase word for that yes. Restoring or linking over an
  // account erases it as surely as starting over, so it takes the same word,
  // and it is cleared whenever the question is asked again or left.
  let replaceTyped = $state("");
  function leaveReplace() {
    replaceAsk = null;
    replaceTyped = "";
    error = "";
  }
  function askReplace(kind, d) {
    replaceTyped = "";
    // confirm: the core's token for this account replacing the keystore shown
    // here; sent back as the yes, so a device changed meanwhile is asked again.
    replaceAsk = { kind, match: d.match, incoming: d.incoming, incomingName: d.incomingName || "", confirm: d.confirm || "" };
    if (d.device) device = d.device;
  }

  // naming: a restore left this device's account without a profile (a
  // recovery phrase carries the key and nothing else). The core is already
  // unlocked and asking the account's other devices and its guild-mates for
  // the profile the account signed (internal/app/profilerecord.go); this step
  // says so, shows it when it comes back, and lets the user go in without it.
  //
  // It used to ask for a name and save it as a full profile edit — name set,
  // everything else blank, stamped newest — and the next device of the account
  // it met adopted that, so a restore wiped the avatar, bio and status
  // everywhere. A name typed here is now provisional (SetProvisionalName): it
  // names this device until the real profile turns up, and loses to it.
  let naming = $state(false);
  let foundId = $state(null); // the identity once a profile has come back
  let provisional = $state(""); // the name this step saved for now
  let waitedOut = $state(false); // looked for a while and found nothing yet
  let watchTimer = 0;
  const found = $derived(foundName(foundId));
  const findStep = $derived(restoreStep({ found, typed: displayName, provisional }));

  // Polls the unlocked core for the profile. One cheap call a second, only
  // while this step is up; the step is left by Continue or Skip, never by the
  // profile arriving, so nothing the user is in the middle of typing moves.
  function watchForProfile() {
    clearInterval(watchTimer);
    const began = Date.now();
    watchTimer = setInterval(async () => {
      if (!naming) return clearInterval(watchTimer);
      try {
        const me = await api.identity();
        if (foundName(me) && foundName(me) !== provisional) foundId = me;
      } catch {}
      // Bounded: on the rig the profile arrived within 4 to 9 s whenever a
      // device or guild-mate was online, so a quarter of a minute of silence
      // means nobody is, and the screen should say that rather than spin.
      if (Date.now() - began > 25000) waitedOut = true;
    }, 1000);
  }
  $effect(() => () => clearInterval(watchTimer));

  async function leaveNaming() {
    naming = false;
    clearInterval(watchTimer);
    await enter();
  }

  // The rendezvous field on the restore screen. A phrase carries no settings,
  // so a restored device is a fresh install that knows no node to meet its
  // other devices or guild-mates through. Prefilled with what this device has
  // saved, else the build's default; an invite code works too (its rendezvous
  // list is taken, the guild is not joined).
  let rdvInput = $state("");
  let rdvBuiltIn = $state(false);

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
    // Over the replace question, only its yes goes ahead: the word typed.
    if (replaceAsk && !eraseWordMatches(replaceTyped)) return;
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
      try {
        await api.setRendezvousInput(rdvInput);
      } catch (err) {
        fail(err, "rdv");
        return;
      }
      if (hasIdentity) {
        // The forgot-passphrase recovery, over an account that is still here.
        // Ask the core what this phrase would do BEFORE doing it: the same
        // account on this device keeps everything; anything else would erase
        // what is here, and that needs a yes for exactly this phrase's account.
        let confirm = "";
        let typed = "";
        if (replaceAsk?.kind === "restore") {
          confirm = replaceAsk.confirm;
          typed = replaceTyped;
        } else {
          const chk = await api.checkRecoveryPhrase(phrase);
          // "none": the keystore has gone since this page looked. The core
          // decides there (keeps the phrase's own data, asks otherwise), and
          // its answer says which; asking here would name the wrong case.
          if (!chk.keepsData && chk.match !== "none") {
            askReplace("restore", chk);
            return;
          }
        }
        try {
          await api.restoreOverExisting(phrase, passphrase, confirm, typed);
        } catch (err) {
          if (err?.code === "replace-refused") {
            askReplace("restore", err.detail);
            return;
          }
          throw err;
        }
        replaceAsk = null;
      } else {
        // No keystore here, but there may be data (a lost keystore, an erase
        // that did not finish): the core keeps it if it is this phrase's
        // account's, and otherwise asks, exactly as above.
        try {
          const yes = replaceAsk?.kind === "restore";
          await api.restoreFromMnemonic(phrase, passphrase, yes ? replaceAsk.confirm : "", yes ? replaceTyped : "");
        } catch (err) {
          if (err?.code === "replace-refused") {
            askReplace("restore", err.detail);
            return;
          }
          throw err;
        }
        replaceAsk = null;
      }
      const notice = await api.login(passphrase);
      if (notice) flash(notice, "info"); // e.g. the keystore's label had been altered
      if (await isNameless()) {
        naming = true;
        restoring = false;
        displayName = "";
        watchForProfile();
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

  // saveName is Continue on the restore step. Its meaning follows what has
  // come back: nothing yet → the typed name is provisional; the profile is
  // back → go in as it; a different name typed → only "Keep" (keepTyped) makes
  // that an edit, and then with everything else that came back kept.
  async function saveName(e) {
    e?.preventDefault();
    if (busy) return;
    if (findStep === "found") return leaveNaming();
    if (findStep === "conflict") return;
    const nm = displayName.trim();
    if (!hasVisible(nm)) return;
    busy = true;
    try {
      await api.setProvisionalName(nm);
      provisional = nm;
      await leaveNaming();
    } catch (err) {
      fail(err, "");
    } finally {
      busy = false;
    }
  }

  async function keepTyped() {
    if (busy) return;
    busy = true;
    try {
      const me = await api.identity();
      await api.setProfile(...keepTypedArgs(me, displayName.trim()));
      await leaveNaming();
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
    } else {
      try {
        const v = await api.strandedData();
        if (v?.stranded) {
          stranded = strandedChoice(v);
          dataNoKeystore = true;
          noKeystoreKeepable = !!v.keepable;
          noKeystoreLinked = !!v.linked;
        }
      } catch {}
    }
    try {
      startNotice = lockNotice((await api.sessionState())?.reason);
    } catch {}
    try {
      const r = await api.getRestoreRendezvous();
      const saved = r?.saved || [];
      rdvBuiltIn = !saved.length && !!r?.builtIn?.length;
      rdvInput = (saved.length ? saved : r?.builtIn || []).join("\n");
    } catch {}
    checked = true;
    if (resumeBackup) return;
    bioCanEnroll = await bioAvailable();
    bioOn = bioCanEnroll && bioEnrolled();
    // If biometric unlock is set up, offer it straight away on the lock screen.
    if (hasIdentity && bioOn) tryBiometricUnlock();
  });

  async function submit(e) {
    e?.preventDefault();
    // An erase confirmation or the first screen's choice is on screen:
    // nothing here submits the form.
    if (confirmingReset || replaceAsk || stranded) return;
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
      const notice = await api.login(passphrase);
      if (notice) flash(notice, "info"); // e.g. the keystore's label had been altered
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
      // The core can't tell a wrong passphrase from a damaged keystore; for
      // somebody sure of theirs, the ways back in are on the link below.
      if (err?.code === "wrong-passphrase" && hasIdentity) error += " “Forgot passphrase?” below has the ways back in.";
      // An account's data but no keystore: nothing was created. Offer the
      // ways forward (recovery phrase, re-link, start over) right away.
      if (err?.code === "device-unlinked") {
        error = "";
        startNotice = erasedAfterUnlink;
      }
      if (err?.code === "erase-unfinished") error = eraseUnfinished(err.detail?.files);
      if (err?.code === "data-without-keystore") {
        dataNoKeystore = true;
        noKeystoreKeepable = !!err.detail?.keepable;
        noKeystoreLinked = !!err.detail?.linked;
        forgot = true;
      }
    } finally {
      busy = false;
    }
  }

  async function doReset() {
    if (!eraseWordMatches(resetTyped) || busy) return;
    busy = true;
    error = "";
    try {
      try {
        await api.resetIdentity(resetTicket, resetTyped.trim());
      } catch (err) {
        // Erased, with the keystore file itself still to go: not a failure.
        if (err?.code !== "erase-pending") throw err;
        flash(err.message, "info");
      }
      hasIdentity = false;
      confirmingReset = false;
      resetTyped = "";
      resetTicket = "";
      dataNoKeystore = false;
      stranded = null;
      keptFrom = null;
      fromForgot = false;
      passphrase = "";
      confirmPass = "";
    } catch (err) {
      // The ticket was spent or the device changed since the confirmation
      // opened: back to the choices, to look again before erasing.
      if (err?.code === "confirm-stale") {
        confirmingReset = false;
        resetTicket = "";
        resetTyped = "";
        forgot = true;
      }
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
    checked && stranded && !confirmingReset
      ? stranded.title
      : backupPhrase && verifying
      ? "Check your recovery phrase"
      : backupPhrase || resumeBackup
        ? "Save your recovery phrase"
        : !checked
          ? "Concord"
          : naming
            ? findStep === "waiting"
              ? "Finding your profile"
              : findStep === "found"
                ? `Welcome back, ${found}`
                : "Which name?"
            : replaceAsk
              ? "Before anything is erased"
              : offerBio
                ? "Unlock faster next time"
                : confirmingReset
                  ? dataNoKeystore
                    ? "Erase this data and start over?"
                    : "Start over on this device?"
                  : linking
                    ? "Link this device"
                    : restoring
                      ? "Restore your account"
                      : forgot
                        ? dataNoKeystore
                          ? "This device holds an account it can't open"
                          : "Forgot your passphrase?"
                        : hasIdentity
                          ? "Unlock Concord"
                          : "Create your account",
  );
  const step = $derived(
    !checked || resumeBackup || stranded
      ? ""
      : backupPhrase && verifying
        ? "3 of 3"
        : backupPhrase
          ? "2 of 3"
          : !hasIdentity && !linking && !restoring && !forgot && !confirmingReset && !replaceAsk && !naming && !offerBio
            ? "1 of 3"
            : "",
  );
  const stepNo = $derived(step ? Number(step[0]) : 0);
  // Which screen is up, by the same branches the template takes. The panel's
  // contents re-enter on a change of screen only, never on a change inside
  // one (a profile turning up while a name is being typed must not move it).
  const screen = $derived(
    !checked
      ? "loading"
      : stranded && !confirmingReset
        ? "stranded"
        : resumeBackup && !backupPhrase
          ? "resume"
          : naming
            ? "naming"
            : replaceAsk
              ? "replace"
              : offerBio
                ? "bio"
                : backupPhrase && verifying
                  ? "verify"
                  : backupPhrase
                    ? "phrase"
                    : confirmingReset
                      ? "reset"
                      : linking
                        ? "link"
                        : restoring
                          ? "restore"
                          : forgot
                            ? "forgot"
                            : hasIdentity
                              ? "unlock"
                              : "create",
  );
  // The mesh on the stage: your devices and the people you trust, joined
  // directly, the mark at the middle. Positions in the 480×320 viewBox.
  const NODES = [
    [62, 92, 5],
    [128, 252, 4],
    [404, 70, 4.5],
    [432, 214, 5.5],
    [330, 286, 3.5],
    [96, 176, 3],
    [372, 150, 3],
  ];
  const HUB = [240, 160];
  const ARCS = [
    [0, 5],
    [5, 1],
    [2, 6],
    [6, 3],
    [3, 4],
    [1, 4],
    [0, 2],
  ];
  // A gentle curve between two nodes, bowed away from the hub like a route
  // on a map.
  function arc(a, b) {
    const [x1, y1] = a;
    const [x2, y2] = b;
    const mx = (x1 + x2) / 2;
    const my = (y1 + y2) / 2;
    const k = 0.18;
    const cx = mx + (mx - HUB[0]) * k;
    const cy = my + (my - HUB[1]) * k;
    return `M${x1} ${y1}Q${cx.toFixed(1)} ${cy.toFixed(1)} ${x2} ${y2}`;
  }
</script>

{#snippet mark(size)}
  <!-- The Concord mark: the delta in the aurora's light on a dark tile, drawn
       inline so the stage needs no image. -->
  <svg class="mark" width={size} height={size} viewBox="0 0 64 64" aria-hidden="true">
    <defs>
      <linearGradient id="lg-ink" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" class="s1" />
        <stop offset="0.55" class="s2" />
        <stop offset="1" class="s3" />
      </linearGradient>
    </defs>
    <rect class="tile" x="1" y="1" width="62" height="62" rx="17" />
    <rect class="tile-edge" x="1.5" y="1.5" width="61" height="61" rx="16.5" />
    <path
      fill="url(#lg-ink)"
      fill-rule="evenodd"
      transform="translate(6.4 6.6) scale(0.8)"
      d="M32 6C31.1 6 30.6 6.7 30.6 8.2L30.2 17C27.3 17.8 25.2 20.5 25.2 23.8L25.2 27.6L4.6 41.6C3.4 42.4 4.1 44.3 5.5 44L24 40.7C24.7 41.9 25.8 42.8 27.1 43.3L26.2 50.6C26.1 51.6 26.9 52.2 27.7 51.7L30.3 45.9L31 55.2C31 56 31.4 56.4 32 56.4C32.6 56.4 33 56 33 55.2L33.7 45.9L36.3 51.7C37.1 52.2 37.9 51.6 37.8 50.6L36.9 43.3C38.2 42.8 39.3 41.9 40 40.7L58.5 44C59.9 44.3 60.6 42.4 59.4 41.6L38.8 27.6L38.8 23.8C38.8 20.5 36.7 17.8 33.8 17L33.4 8.2C33.4 6.7 32.9 6 32 6ZM28.4 22.3L31 22.3L31 27.2L27.4 27.2L27.4 24.3C27.4 23.3 27.7 22.3 28.4 22.3ZM33 22.3L35.6 22.3C36.3 22.3 36.6 23.3 36.6 24.3L36.6 27.2L33 27.2Z"
    />
  </svg>
{/snippet}

<!-- The lock and first-run stage. The aurora is one layer moved by transform
     behind everything; the hero (mark, mesh, wordmark) is decoration and a
     sentence; the form sits on an opaque panel, so every word in it — the
     warnings above all — keeps the chrome's ink and contrast. -->
<div class="login" class:deep={!["loading", "unlock", "create", "stranded"].includes(screen)}>
  <div class="sky" aria-hidden="true"><div class="aurora"></div></div>
  <div class="scroller" onfocusin={keepFieldInView}>
    <div class="stage">
      <div class="hero">
        <!-- data-audit="bleed": the drawing's box runs under the wordmark by
             design (a negative margin); its marks end above the word. -->
        <div class="emblem" aria-hidden="true" data-audit="bleed">
          <svg class="mesh" viewBox="0 0 480 320" preserveAspectRatio="xMidYMid meet">
            <defs>
              <radialGradient id="lg-node">
                <stop offset="0" class="n0" />
                <stop offset="1" class="n1" />
              </radialGradient>
              <radialGradient id="lg-spark">
                <stop offset="0" class="k0" />
                <stop offset="1" class="k1" />
              </radialGradient>
            </defs>
            <ellipse class="orbit" cx="240" cy="160" rx="150" ry="104" />
            <ellipse class="orbit far" cx="240" cy="160" rx="222" ry="150" />
            {#each ARCS as [a, b], i (i)}
              <path class="route" style:--i={i} d={arc(NODES[a], NODES[b])} />
            {/each}
            {#each NODES as [x, y], i (i)}
              <line class="spoke" style:--i={i} x1={HUB[0]} y1={HUB[1]} x2={x} y2={y} />
            {/each}
            {#each NODES as [x, y, r], i (i)}
              <!-- One spark per stage: the node that is you. -->
              <g class="node" class:spark={i === 3} style:--i={i}>
                <circle class="halo" cx={x} cy={y} r={r * 4.2} fill={i === 3 ? "url(#lg-spark)" : "url(#lg-node)"} />
                <circle class="dot" cx={x} cy={y} r={r} />
              </g>
            {/each}
          </svg>
          <div class="hub">{@render mark(104)}</div>
        </div>
        <p class="wordmark" aria-hidden="true">Concord</p>
        <p class="tagline">Talk with the people you trust: no account server, no message store.</p>
      </div>

  <form class="card" onsubmit={submit}>
    {#key screen}
    <div class="screen">
    {#if step}
      <div class="steps">
        <span class="segs" aria-hidden="true">
          {#each [1, 2, 3] as n (n)}<span class="seg" class:on={n <= stepNo}></span>{/each}
        </span>
        <p class="step">{step}</p>
      </div>
    {/if}
    <h1>{heading}</h1>

    {#if endedReason && !backupPhrase && !resumeBackup}
      <p class="notice" role="status">
        {#if endedReason === "unlinked"}
          {erasedAfterUnlink}
        {:else if endedReason === "unlinked-not-erased"}
          This device was unlinked from its account, but erasing it here has
          not finished, so some of your identity or data may still be on it.
          While Concord is open it tries again for a day, and it tries again
          the next time it starts. Nothing was deleted from anyone else.
        {:else if endedReason === "unlinked-replaced"}
          This device was unlinked from its account, but before it could erase
          itself an account was linked or restored here again, so nothing was
          erased. What is on this device now stays as it is.
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

    {#if startNotice && !stranded}
      <p class="notice" role="status">{startNotice}</p>
    {/if}

    {#if !checked}
      <p class="muted">Loading…</p>
    {:else if stranded && !confirmingReset}
      <p class="muted">{stranded.body}</p>
      {#if quitAsked}
        <p class="notice" role="status">{quitClosed}</p>
      {:else}
        <button class="primary" type="button" onclick={openReset}>{stranded.erase}</button>
        <button type="button" onclick={keepOrQuit}>{stranded.other}</button>
      {/if}
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
      {#if findStep === "waiting"}
        <p class="muted find-status" role="status">
          {#if !waitedOut}
            <span class="ls-spin" aria-hidden="true"></span>
            Your account is back. Looking for your profile on your other devices and guild-mates…
          {:else}
            None of your devices or guild-mates have answered yet. Your profile will appear by
            itself when one of them is online — you don't have to wait here.
          {/if}
        </p>
        {#if waitedOut}
          <p class="muted small">
            {#if !rdvInput.trim()}
              This device has no rendezvous (the meeting point it finds other devices through),
              so it can only look on this network.
            {/if}
            If this account is still open on a device you never linked, the two can't see each
            other: close it there, or link this device from it instead.
          </p>
        {/if}
        <span class="field-lbl" aria-hidden="true">Your name for now (optional)</span>
        <input
          type="text"
          placeholder="Shown until your profile comes back"
          aria-label="Your name"
          maxlength="32"
          autocomplete="off"
          bind:value={displayName}
        />
        <div class="error slot" role="alert">{error}</div>
        <button class="primary" type="submit" disabled={!hasVisible(displayName) || busy}>Continue</button>
        <button type="button" class="link" onclick={leaveNaming}>Skip for now</button>
      {:else if findStep === "found"}
        <div class="found" role="status">
          {#if foundId?.avatar}
            <img class="found-av" src={foundId.avatar} alt="" />
          {:else if foundId?.emoji}
            <span class="found-av emoji" aria-hidden="true">{foundId.emoji}</span>
          {/if}
          <span class="found-txt">
            <strong class="found-name">{found}</strong>
            <span class="muted small">Your profile came back from another of your devices or a guild-mate.</span>
          </span>
        </div>
        <div class="error slot" role="alert">{error}</div>
        <button class="primary" type="submit" disabled={busy}>Continue</button>
      {:else}
        <p class="muted">
          Your profile came back as <strong>{found}</strong>, and you typed
          <strong>{displayName.trim()}</strong>. Which should people see?
        </p>
        <div class="error slot" role="alert">{error}</div>
        <button class="primary" type="button" disabled={busy} onclick={leaveNaming}>Use {found}</button>
        <button type="button" class="ghost" disabled={busy} onclick={keepTyped}>Keep {displayName.trim()}</button>
      {/if}
    {:else if replaceAsk}
      <!-- The core refused to put an account on this device because it would
           erase the one here. This is the only place that can say yes, and it
           says yes for ONE account: the fingerprint it sends back is the
           incoming one, so an edited phrase or a code for yet another account
           is asked about again. -->
      <!-- The whole question is the alertdialog, its choices included, and it
           takes focus when it opens: the control that opened it is gone, and
           focus left on <body> gave a screen reader nothing to announce. -->
      <div
        class="replace-ask"
        role="alertdialog"
        aria-labelledby="replace-h"
        aria-describedby="replace-p"
        tabindex="-1"
        use:focusOnMount
      >
        <div class="replace">
          <h2 id="replace-h">
            {#if replaceAsk.match === "same"}
              {replaceAsk.kind === "link" ? "Linking" : "Restoring"} would erase this device's copy
            {:else if replaceAsk.match === "unknown"}
              This would erase what's on this device
            {:else if replaceAsk.kind === "link"}
              That code is for a different account
            {:else}
              That phrase is for a different account
            {/if}
          </h2>
          <p id="replace-p">
            {#if !hasIdentity}
              <!-- No keystore: the core matched the phrase or code against the
                   data's messages (app.noKeystoreGate), and no account here can be
                   opened or proved by a passphrase. -->
              This device holds data from an account it can't open: its keystore is
              missing{#if device?.name}, and its files say the account is {device.name}{/if}.
              {#if replaceAsk.match === "same"}
                Its messages show it is the account the {replaceAsk.kind === "link" ? "code" : "phrase"}
                is for, but this device was linked to that account, and its data also needed the
                device key that was in the keystore, so it can't be kept.
              {:else if replaceAsk.match === "different"}
                Its messages show it is not the account the {replaceAsk.kind === "link" ? "code" : "phrase"}
                is for.
              {:else}
                It holds no messages, so Concord can't tell whose it is.
              {/if}
              Going ahead erases <strong>that data</strong>{#if replaceAsk.match !== "unknown"}{" "}— its
                guilds, messages and notes —{/if} and puts the account {replaceAsk.kind === "link" ? "the code is for" : "the phrase is for"}
              ({shortFp(replaceAsk.incoming)}) here.
            {:else if replaceAsk.match === "same"}
              The {replaceAsk.kind === "link" ? "code" : "phrase"} is for <strong>{deviceName}</strong>, the
              account on this device — but this device was linked to it from another device, and that
              link can't be recovered without the passphrase. Going ahead erases this device's guilds,
              messages and notes.{#if replaceAsk.kind !== "link"}{" "}If your other device still has the account, re-linking from it brings everything back
                instead.{/if}
            {:else if replaceAsk.match === "unknown"}
              Concord can't confirm which account is on this device{#if device?.fingerprint}{" — "}its
                files say {deviceName} ({shortFp(device.fingerprint)}), but only its
                passphrase can prove that{/if}.
              Going ahead erases <strong>the account here</strong> and replaces it with the account
              {replaceAsk.kind === "link" ? "the code is for" : "the phrase is for"}
              ({shortFp(replaceAsk.incoming)}). Its guilds, messages and notes go too, unless
              Concord then finds they are that account's and can keep them.
            {:else if device?.verified}
              This device holds <strong>{deviceName}</strong>{#if device?.fingerprint}&nbsp;({shortFp(
                  device.fingerprint,
                )}){/if}. The {replaceAsk.kind === "link" ? "code" : "phrase"} belongs to
              <strong>{replaceAsk.incomingName || "another account"}</strong> ({shortFp(replaceAsk.incoming)}).
              Going ahead <strong>erases {deviceName} from this device</strong> — its guilds,
              messages, notes and settings. Nothing can bring this device's copy back unless
              {deviceName} is also on another of your devices.
            {:else}
              This device's files say it holds <strong>{deviceName}</strong>{#if device?.fingerprint}&nbsp;({shortFp(
                  device.fingerprint,
                )}){/if}, but only its passphrase can prove that. The
              {replaceAsk.kind === "link" ? "code" : "phrase"} belongs to
              <strong>{replaceAsk.incomingName || "another account"}</strong> ({shortFp(replaceAsk.incoming)}).
              Going ahead <strong>erases the account on this device</strong> — its guilds,
              messages, notes and settings. Nothing can bring this device's copy back unless
              that account is also on another of your devices.
            {/if}
          </p>
          <p class="muted small nothing-yet">Nothing has been changed yet.</p>
        </div>
        <!-- The safe choices first and full width; erasing comes last, behind the
             typed word, on a button that is neither first nor full width. -->
        <button type="button" class="primary" disabled={busy} onclick={leaveReplace}>Keep everything here</button>
        {#if replaceAsk.match === "same" && replaceAsk.kind !== "link"}
          <button
            type="button"
            class="ghost"
            disabled={busy}
            onclick={() => (leaveReplace(), (restoring = false), (linking = true))}
          >
            Re-link from my other device instead
          </button>
        {/if}
        {#if replaceAsk.kind === "link"}
          <p class="muted small">
            Each link code works once. To erase and link anyway, make a new code on the
            other device and paste it here.
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
        {#if busy && linkWait}
          <div class="link-sas" role="status" aria-live="assertive" use:revealOnMount>
            <span>Your other device is asking whether to add <strong>{linkWait.deviceName}</strong>. Check that it shows</span>
            <span class="sas" aria-label="Code {linkWait.sas}">{linkWait.sas}</span>
            <span>and choose <strong>Yes, link it</strong> there.</span>
          </div>
        {/if}
        <EraseConfirm
          bind:typed={replaceTyped}
          {busy}
          ready={replaceEraseReady({ typed: replaceTyped, kind: replaceAsk.kind, linkCode })}
          label={replaceEraseLabel({
            match: replaceAsk.match,
            kind: replaceAsk.kind,
            verified: !!device?.verified,
            name: device?.name || "",
            hasIdentity,
          })}
          onconfirm={replaceAsk.kind === "link" ? doLink : doRestore}
        />
      </div>
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
      <p class="caution">
        <Icon name="alert" size={16} />
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
      {#if dataNoKeystore}
        <p class="muted">
          This permanently deletes the data on <strong>this device</strong> from the
          account it can't open — its guilds, messages and notes — so you can set up a
          new account here.{#if noKeystoreKeepable}{" "}That account's recovery phrase, or another of its devices, would keep this data
            instead.{/if} This cannot be undone.
        </p>
      {:else}
        <p class="muted">
          This permanently deletes this account and everything else from <strong>this device</strong>,
          so you can set up a new account here. This cannot be undone.
        </p>
      {/if}
      <p class="muted small">
        Guilds carry on for their other members. A guild you own keeps this account as its
        owner: unless the account comes back (from another device or your recovery phrase),
        only an heir you named can take it over.
      </p>
      <button
        type="button"
        class="primary"
        disabled={busy}
        onclick={leaveReset}
      >
        Cancel
      </button>
      <div class="error slot" role="alert">{error}</div>
      <EraseConfirm bind:typed={resetTyped} {busy} label="Yes, delete and start over" onconfirm={doReset} />
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
          {device?.verified ? "This device holds" : "This device's files say it holds"}
          <strong>{deviceName}</strong>{#if device?.fingerprint}&nbsp;({shortFp(
              device.fingerprint,
            )}){/if}. A code from the same account keeps everything here when Concord
          can confirm it is the same; otherwise you'll be asked before anything is
          erased.
        </p>
      {/if}
      <span class="field-lbl" aria-hidden="true">Passphrase for this device</span>
      <PassphraseField placeholder="Passphrase for this device" autocomplete="new-password" meter bind:value={passphrase} invalid={errField === "pass"} />
      <span class="field-lbl" aria-hidden="true">Confirm passphrase</span>
      <PassphraseField placeholder="Confirm passphrase" autocomplete="new-password" bind:value={confirmPass} invalid={errField === "confirm"} />
      <div class="error slot" role="alert">{error}</div>
      {#if busy && linkWait}
        <div class="link-sas" role="status" aria-live="assertive" use:revealOnMount>
          <span>Your other device is asking whether to add <strong>{linkWait.deviceName}</strong>. Check that it shows</span>
          <span class="sas" aria-label="Code {linkWait.sas}">{linkWait.sas}</span>
          <span>and choose <strong>Yes, link it</strong> there.</span>
        </div>
      {:else if busy}
        <div class="linking-status">
          <span class="ls-spin"></span>
          <span>Connecting to your other device… this can take up to a minute over the internet — keep both devices on the link screen.</span>
        </div>
      {/if}
      <button class="primary" type="button" disabled={busy} onclick={doLink}>
        {busy ? "Linking…" : "Link this device"}
      </button>
      <button type="button" class="link" onclick={leaveRestoreOrLink}>
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
          Enter your 24-word recovery phrase and a new passphrase. If Concord can
          confirm the phrase is for <strong>{deviceName}</strong>, the account on this
          device, everything here stays — only the passphrase changes. Otherwise
          you'll be told before anything is erased.
        </p>
      {:else}
        <p class="muted">
          Your 24 words bring back who you are, not your history: this device starts with no
          guilds and no messages.
        </p>
        <p class="muted small use-link">
          Is the account still open on another device? Don't restore — a copy restored beside it
          can't see it or its guilds.
          <button type="button" class="link inline" onclick={() => ((restoring = false), (linking = true), (error = ""))}>
            Add this device to my account instead
          </button>
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
      <span class="field-lbl" aria-hidden="true">Rendezvous address or an invite code</span>
      <textarea
        class="phrase-in rdv-in"
        rows="2"
        placeholder="/dns4/…/p2p/… or an invite code"
        autocapitalize="none"
        autocorrect="off"
        autocomplete="off"
        spellcheck="false"
        aria-label="Rendezvous address or an invite code"
        aria-describedby="rdv-help"
        aria-invalid={errField === "rdv" ? "true" : undefined}
        bind:value={rdvInput}
      ></textarea>
      <p class="muted small" id="rdv-help">
        {#if rdvBuiltIn}
          Filled in with this app's default — how it finds your other devices and guild-mates.
        {:else}
          How this device finds your other devices and guild-mates. Empty looks only on this network.
        {/if}
      </p>
      <span class="field-lbl" aria-hidden="true">New passphrase</span>
      <PassphraseField placeholder="New passphrase" autocomplete="new-password" meter bind:value={passphrase} invalid={errField === "pass"} />
      <span class="field-lbl" aria-hidden="true">Confirm passphrase</span>
      <PassphraseField placeholder="Confirm passphrase" autocomplete="new-password" bind:value={confirmPass} invalid={errField === "confirm"} />
      <div class="error slot" role="alert">{error}</div>
      <button class="primary" type="button" disabled={busy} onclick={doRestore}>
        {busy ? "Restoring…" : "Restore account"}
      </button>
      <button type="button" class="link" onclick={leaveRestoreOrLink}>
        Back
      </button>
    {:else if forgot}
      {#if dataNoKeystore}
        <!-- What restoring or re-linking does to this data is the core's call
             (app.NoKeystoreKeepable), so each sentence says only what it said. -->
        <p class="muted">
          This device holds data from an account it can't open: its keystore is
          missing.
          {#if noKeystoreKeepable}
            Restoring with <strong>that account's recovery phrase</strong>, or re-linking
            from another of its devices, keeps everything here. A different account's
            phrase, or erasing, deletes this data — you'll be asked first.
          {:else if noKeystoreLinked}
            This device was linked to that account, and its data also needs the device key
            that was in the keystore, so it can't be kept: restoring or re-linking erases
            it, and you'll be asked first. Re-linking from another of the account's devices
            brings the account back with its guilds.
          {:else}
            It holds no messages, so Concord can't tell whose data it is: restoring or
            re-linking erases it, and you'll be asked first.
          {/if}
        </p>
      {:else}
        <p class="muted">
          Your passphrase can't be looked up — it's never stored anywhere. If this
          account is also on <strong>another device</strong>, re-linking from it is
          the best fix: everything comes back — profile, guilds, and history. The
          <strong>24-word recovery phrase</strong> restores your identity, but
          guilds and history only return by re-linking or re-invites.
        </p>
      {/if}
      <div class="error slot" role="alert">{error}</div>
      <button class="primary" type="button" onclick={() => ((linking = true), (forgot = false), (fromForgot = true), (error = ""))}>
        Re-link from my other device
      </button>
      <p class="muted small">
        {dataNoKeystore && !noKeystoreKeepable
          ? "If the account is still open on another device, it comes back from there."
          : "If the account is still open on another device, everything comes back."}
      </p>
      <button type="button" class="ghost" onclick={() => ((restoring = true), (forgot = false), (fromForgot = true), (error = ""))}>
        Use my recovery phrase
      </button>
      <p class="muted small">
        {!dataNoKeystore
          ? "If no other device of yours has the account any more."
          : noKeystoreKeepable
            ? "If it is this account's phrase, everything on this device is kept."
            : "This data can't be kept: you'll be asked before it is erased."}
      </p>
      <button type="button" class="link warn-link" onclick={openReset}>
        {dataNoKeystore ? "Erase this data and start over" : "I have neither — start over (deletes everything)"}
      </button>
      <button type="button" class="link" onclick={leaveForgot}>Back</button>
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
      <!-- The two ways in for an account that already exists, linking first.
           People with a working second device reached for the phrase, and a
           phrase restore beside a running original makes a copy that shares
           its network identity and can see neither it nor its guilds. -->
      <div class="other-ways">
        <button type="button" class="link" onclick={() => ((linking = true), (error = ""))}>
          Add this device to my account
        </button>
        <span class="muted small">If your account is open on another device, link this one with a code from it.</span>
        <button type="button" class="link" onclick={() => ((restoring = true), (error = ""))}>
          I lost my devices — restore from my recovery phrase
        </button>
        <span class="muted small">Only when no other device of yours still has the account.</span>
      </div>
    {/if}

    {#if !backupPhrase && !resumeBackup && !naming && !replaceAsk}
      <p class="foot"><Icon name="lock" size={14} /> Your keys never leave this device.</p>
    {/if}
    </div>
    {/key}
  </form>
    </div>
  </div>
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
  .link-sas {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--sp-1);
    padding: 10px 12px;
    margin: 4px 0 2px;
    text-align: center;
    font-size: var(--fs-compact);
    line-height: 1.4;
    border: 1px solid color-mix(in srgb, var(--accent) 55%, transparent);
    border-radius: var(--radius-md);
  }
  .link-sas .sas {
    font-family: var(--font-mono, ui-monospace, monospace);
    font-size: 2rem;
    font-weight: 700;
    letter-spacing: 0.12em;
    font-variant-numeric: tabular-nums;
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
  :global(:root[data-motion="reduced"]) .ls-spin {
    animation: none;
  }
  /* ---- the stage ----
     The lock and first-run screens are a stage (DESIGN-063.md): the aurora on
     the stage ground, the mark at the hub of a mesh of peers, the wordmark in
     the display face, and the form on an opaque panel beside it. On a phone
     the same pieces stack in one column. */
  .login {
    position: relative;
    height: 100%;
    overflow: hidden;
    isolation: isolate;
    background: var(--stage-ground);
    color: var(--text);
  }
  /* One layer moves: the aurora, by transform, slowly. The dot field over it
     is static paint, faded out toward the edges. */
  .sky {
    position: absolute;
    inset: 0;
    z-index: -1;
    overflow: hidden;
    pointer-events: none;
    animation: lg-fade calc(var(--dur-reveal) * 2) var(--ease-spring) both;
  }
  .aurora {
    position: absolute;
    inset: -20%;
    background: var(--aurora);
    will-change: transform;
    animation: lg-drift 28s ease-in-out infinite alternate;
  }
  .sky::after {
    content: "";
    position: absolute;
    inset: 0;
    background-image: radial-gradient(color-mix(in srgb, var(--stage-ink) 16%, transparent) 1px, transparent 1.5px);
    background-size: 26px 26px;
    -webkit-mask-image: radial-gradient(ellipse 60% 70% at 30% 45%, #000 10%, transparent 72%);
    mask-image: radial-gradient(ellipse 60% 70% at 30% 45%, #000 10%, transparent 72%);
  }
  @keyframes lg-drift {
    0% {
      transform: translate3d(-4%, -3%, 0) rotate(-5deg) scale(1.04);
    }
    50% {
      transform: translate3d(3%, 4%, 0) rotate(2deg) scale(1.1);
    }
    100% {
      transform: translate3d(5%, -2%, 0) rotate(6deg) scale(1);
    }
  }
  :global(:root[data-motion="reduced"]) .aurora {
    animation: none;
    will-change: auto;
  }
  .scroller {
    height: 100%;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .stage {
    box-sizing: border-box;
    min-height: 100%;
    max-width: 1240px;
    margin: 0 auto;
    display: grid;
    grid-template-columns: minmax(0, 1fr) 440px;
    align-items: center;
    gap: var(--sp-8);
    padding: var(--sp-7) var(--sp-8);
  }

  /* ---- the hero ---- */
  .hero {
    min-width: 0;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--sp-4);
  }
  .emblem {
    position: relative;
    width: min(100%, 600px);
    aspect-ratio: 3 / 2;
    margin: 0 0 calc(-1 * var(--sp-5)) calc(-1 * var(--sp-6));
    animation: lg-rise var(--dur-reveal) var(--ease-spring) both;
  }
  .mesh {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    overflow: visible;
  }
  .mesh .orbit {
    fill: none;
    stroke: color-mix(in srgb, var(--stage-ink) 14%, transparent);
    stroke-width: 1;
    stroke-dasharray: 1 7;
    stroke-linecap: round;
  }
  .mesh .orbit.far {
    stroke: color-mix(in srgb, var(--stage-ink) 8%, transparent);
  }
  .mesh .route {
    fill: none;
    stroke: color-mix(in srgb, var(--aurora-2) 60%, transparent);
    stroke-width: 1.25;
    stroke-linecap: round;
  }
  .mesh .spoke {
    stroke: color-mix(in srgb, var(--aurora-1) 45%, transparent);
    stroke-width: 1;
    stroke-dasharray: 3 5;
  }
  .mesh .n0 {
    stop-color: var(--aurora-1);
    stop-opacity: 0.5;
  }
  .mesh .n1 {
    stop-color: var(--aurora-1);
    stop-opacity: 0;
  }
  .mesh .k0 {
    stop-color: var(--aurora-spark);
    stop-opacity: 0.6;
  }
  .mesh .k1 {
    stop-color: var(--aurora-spark);
    stop-opacity: 0;
  }
  .mesh .dot {
    fill: var(--stage-ink);
  }
  .mesh .spark .dot {
    fill: var(--aurora-spark);
  }
  .mesh .node {
    animation: lg-fade var(--dur-reveal) var(--ease-spring) both;
    animation-delay: calc(var(--stagger) * (var(--i) + 1));
  }
  /* The hub: the mark on a pool of the brand light (static). */
  .hub {
    position: absolute;
    left: 50%;
    top: 50%;
    display: grid;
    place-items: center;
    width: 240px;
    height: 240px;
    transform: translate(-50%, -50%);
    background: radial-gradient(closest-side, color-mix(in srgb, var(--aurora-1) 34%, transparent), transparent);
  }
  .mark .tile {
    fill: #0f1518;
  }
  .mark .tile-edge {
    fill: none;
    stroke: url(#lg-ink);
    stroke-opacity: 0.55;
  }
  .mark .s1 {
    stop-color: #3fe0cc;
  }
  .mark .s2 {
    stop-color: #5aaef0;
  }
  .mark .s3 {
    stop-color: #a58cff;
  }
  .wordmark {
    margin: 0;
    font-family: var(--display-font);
    font-size: var(--fs-hero);
    font-weight: 700;
    letter-spacing: -0.02em;
    line-height: 1.05;
    /* The aurora's ink, with the stage ink under it for anything that cannot
       clip a background to text. */
    color: var(--stage-ink);
    background: var(--aurora-ink);
    -webkit-background-clip: text;
    background-clip: text;
    -webkit-text-fill-color: transparent;
    animation: lg-rise var(--dur-reveal) var(--ease-spring) both;
    animation-delay: var(--stagger);
  }
  .tagline {
    max-width: 19ch;
    font-family: var(--display-font);
    font-size: var(--fs-headline);
    font-weight: 500;
    letter-spacing: -0.02em;
    line-height: 1.15;
    color: var(--stage-ink);
    animation: lg-rise var(--dur-reveal) var(--ease-spring) both;
    animation-delay: calc(var(--stagger) * 2);
  }

  /* ---- the panel ----
     Opaque, so the chrome's contrast holds whatever the aurora does behind
     it; the light catches its top edge. Present from the first frame: it
     settles in, but its field takes focus and keys at once. */
  .card {
    position: relative;
    box-sizing: border-box;
    width: 100%;
    padding: var(--sp-7);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-xl);
    box-shadow: var(--shadow-stage);
    text-align: start;
    animation: lg-panel var(--dur-stage) var(--ease-spring) both;
  }
  .card::before {
    content: "";
    position: absolute;
    top: -1px;
    left: var(--sp-7);
    right: var(--sp-7);
    height: 1px;
    background: var(--aurora-ink);
    opacity: 0.9;
  }
  .screen {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    animation: lg-step var(--dur-stage) var(--ease-spring) both;
  }
  .steps {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    margin-bottom: var(--sp-1);
  }
  .segs {
    display: flex;
    gap: var(--sp-1);
  }
  .seg {
    width: 28px;
    height: 4px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--text-muted) 28%, transparent);
  }
  .seg.on {
    background: var(--accent);
  }
  @keyframes lg-fade {
    from {
      opacity: 0;
    }
  }
  @keyframes lg-rise {
    from {
      opacity: 0;
      transform: translateY(var(--stage-rise));
    }
  }
  @keyframes lg-panel {
    from {
      opacity: 0;
      transform: translateY(var(--stage-rise)) scale(var(--stage-scale));
    }
  }
  @keyframes lg-step {
    from {
      opacity: 0;
      transform: translateY(calc(var(--stage-rise) / 2));
    }
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
  .step {
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--accent-hover);
  }
  h1 {
    margin: 0 0 var(--sp-1);
    font-family: var(--display-font);
    font-size: var(--fs-headline);
    font-weight: 600;
    letter-spacing: -0.02em;
    line-height: 1.1;
    overflow-wrap: anywhere;
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
  .other-ways {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    margin-top: var(--sp-2);
    text-align: start;
  }
  .other-ways .link + .small {
    margin-bottom: var(--sp-2);
  }
  .link.inline {
    display: inline;
    padding: 0;
    min-height: 0;
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
    margin-top: var(--sp-3);
    padding-top: var(--sp-4);
    border-top: 1px solid var(--hairline);
    font-size: var(--fs-small);
    color: var(--text-muted);
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
  /* The question and its choices, laid out as the card lays out its own. */
  .replace-ask {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .replace-ask:focus {
    outline: none;
  }
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
  .rdv-in {
    font-size: var(--fs-compact);
  }
  #rdv-help {
    margin: -4px 0 0;
  }
  .find-status {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .found {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: 10px 12px;
    border-radius: var(--radius-md);
    background: var(--bg-1);
    border: 1px solid var(--border);
    text-align: start;
  }
  .found-av {
    width: 44px;
    height: 44px;
    flex: none;
    border-radius: 50%;
    object-fit: cover;
  }
  .found-av.emoji {
    display: grid;
    place-items: center;
    font-size: 26px;
    background: var(--bg-2);
  }
  .found-txt {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .found-name {
    overflow-wrap: anywhere;
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
  /* A caution, not an error: the chrome's own ink on a warn-edged panel,
     the alert glyph in the warn colour carrying it. */
  .caution {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: 10px var(--sp-3);
    border-radius: var(--radius-md);
    border: 1px solid color-mix(in srgb, var(--warn) 55%, transparent);
    background: color-mix(in srgb, var(--warn) 10%, transparent);
    color: var(--text);
    font-size: var(--fs-compact);
  }
  .caution :global(svg) {
    flex: none;
    margin-top: 1px;
    color: var(--warn);
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
  /* Too narrow for the hero beside the panel: one column, the hero compact
     and centred above. */
  @media (max-width: 1080px) {
    .stage {
      grid-template-columns: minmax(0, 480px);
      justify-content: center;
      align-content: center;
      gap: var(--sp-5);
      padding: var(--sp-7) var(--sp-5);
    }
    .hero {
      align-items: center;
      text-align: center;
      gap: var(--sp-2);
    }
    .emblem {
      width: min(100%, 300px);
      margin: 0 0 calc(-1 * var(--sp-3));
    }
    .tagline {
      max-width: 26ch;
      font-family: var(--ui-font);
      font-size: var(--fs-body);
      font-weight: 400;
      letter-spacing: 0;
      line-height: 1.45;
      /* Body-sized on the aurora: the full stage ink, which holds AA on the
         aurora's brightest point; the muted ink does not. */
      color: var(--stage-ink);
    }
  }
  @media (max-width: 768px) {
    /* One column 14px from the edges, under the safe area; it scrolls when a
       step and the keyboard need it. */
    .stage {
      gap: var(--sp-4);
      padding: calc(var(--safe-top) + var(--sp-5)) 14px calc(12px + var(--safe-bottom) + var(--kb, 0px));
    }
    .emblem {
      width: min(100%, 240px);
    }
    .hub {
      width: 150px;
      height: 150px;
    }
    .hub :global(svg) {
      width: 64px;
      height: 64px;
    }
    .card {
      padding: var(--sp-5) var(--sp-4);
    }
    /* Past the first screen (the phrase, a restore, a link) the steps are
       long: the hero steps down to the wordmark and gives them the room. */
    .deep .emblem,
    .deep .tagline {
      display: none;
    }
    .deep .wordmark {
      font-size: var(--fs-headline);
    }
    .card::before {
      left: var(--sp-5);
      right: var(--sp-5);
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
