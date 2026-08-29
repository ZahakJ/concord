<script>
  // Everything that makes noise or lights up. Split out of Settings so the
  // top level can stay a short list of places to go.
  import SettingsShell from "./SettingsShell.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import Icon from "../Icon.svelte";
  import Select from "../Select.svelte";
  import { S, setPref, flash, patchProfile, setAlertWords } from "../lib/state.svelte.js";
  import { tooltip } from "../lib/tooltip.js";
  import { plural } from "../lib/plural.js";
  import { addWord, removeWord, rejectReason, MAX_WORDS, MAX_LEN } from "../lib/alertwords.js";
  import { asksLazily, notificationStatus, requestPermission, openSystemSettings } from "../lib/notify.js";
  import { rangefill } from "../lib/rangefill.js";
  import {
    soundsEnabled,
    setSoundsEnabled,
    soundboardEnabled,
    setSoundboardEnabled,
    RINGTONE_OPTIONS,
    getRingtone,
    setRingtone,
    previewRingtone,
    soundVolume,
    setSoundVolume,
    playDone,
    probeAudioOutput,
    audioTrouble,
  } from "../lib/sounds.js";

  let { onClose } = $props();

  // ---- one control for "how loud", including off -----------------------------
  //
  // This used to be a switch called Sounds AND a slider called Volume, and
  // nothing reconciled them: an install could sit with the switch ON and the
  // slider at 0, which is silence with a control saying otherwise. That is
  // exactly the state a person then reports as "no sound on joining a call",
  // and no amount of looking at the switch explains it.
  //
  // So there is one control. Zero is off — the same stored on/off flag as
  // before, written from the same gesture, so every reader of soundsEnabled()
  // (nine synthesizers, the soundboard, the arrival blip) is untouched.
  let sounds = $state(soundsEnabled());
  let vol = $state(soundsEnabled() ? Math.round(soundVolume() * 100) : 0);
  function setVol(e) {
    vol = Number(e.target.value);
    const on = vol > 0;
    if (on !== sounds) {
      sounds = on;
      setSoundsEnabled(on);
    }
    if (on) setSoundVolume(vol / 100);
  }

  // The soundboard gets its own switch. Sound effects are the one noise a
  // person most often wants gone without also losing the mention ping and the
  // ring of an incoming call, and until now the only lever was all of them.
  let board = $state(soundboardEnabled());
  function toggleBoard() {
    board = !board;
    setSoundboardEnabled(board);
  }

  // Do Not Disturb is a presence, set from the status popover — but it's also
  // the master mute, so it belongs on this page too rather than only next to
  // your name. Turning it off returns you to Online, which is the only sensible
  // opposite (the popover is where you'd pick Idle or Invisible instead).
  const dnd = $derived(S.identity.presence === "dnd");
  async function toggleDnd() {
    try {
      await patchProfile({ presence: dnd ? "online" : "dnd" });
    } catch (err) {
      flash(err);
    }
  }

  // ---- can this machine play a sound at all? ---------------------------------
  //
  // On the Linux desktop, sometimes not: WebKitGTK renders Web Audio through
  // GStreamer, and a box without the audio plugins gets a context that reports
  // itself healthy and never makes a sound. Every switch on this page then does
  // nothing, silently, and the page says nothing about it — which is the whole
  // reason "no chime when I join a call" was reported as a Concord bug.
  //
  // Probing on open is honest and costs a third of a second of a clock reading;
  // it plays nothing. The Test button below is the same probe with a chime on
  // the end, for the case where the machine is fine and the speakers are not.
  let trouble = $state(null);
  let testing = $state(false);
  async function probe(withChime) {
    testing = true;
    await probeAudioOutput();
    trouble = audioTrouble();
    if (withChime && !trouble && sounds) playDone();
    testing = false;
  }
  probe(false);

  let ringtone = $state(getRingtone());
  function pickRingtone(id) {
    ringtone = id;
    setRingtone(id);
    previewRingtone(id); // audition the choice immediately
  }

  // The OS grant. Concord no longer asks for it at login on a phone (see
  // asksLazily in lib/notify.js), so this row is the deliberate route: the only
  // place someone who never saw the rationale bar, or who dismissed it, can
  // still turn tray notifications on. Without it the deferral would be a
  // one-way door.
  //
  // Three states, because Android has three: granted; not yet asked (we can
  // still open the dialog); and hard-denied, where the dialog will never appear
  // again and system Settings is the only way back.
  let osNotif = $state({ enabled: false, canRequest: false });
  let osKnown = $state(false);
  async function refreshOsNotif() {
    try {
      osNotif = await notificationStatus();
    } catch {
      osNotif = { enabled: false, canRequest: false };
    }
    osKnown = true;
  }
  refreshOsNotif();

  // Coming back into view is the other signal something may have changed — the
  // system settings route leaves the app entirely and cannot call back.
  $effect(() => {
    const onVisible = () => {
      if (!document.hidden) refreshOsNotif();
    };
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("focus", onVisible);
    return () => {
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("focus", onVisible);
    };
  });

  async function fixOsNotif() {
    if (osNotif.enabled) {
      // Already on — Settings is where it gets turned off, not here.
      await openSystemSettings();
      return;
    }
    if (osNotif.canRequest) {
      osNotif = await requestPermission(); // resolves once the dialog is answered
      osKnown = true;
      return;
    }
    if (!(await openSystemSettings())) {
      flash("Open your system settings to allow Concord notifications.");
    }
  }

  // ---- alert words ----
  //
  // The list lives in this device's own storage and is handed to the inbox query
  // as an argument; nothing here writes it anywhere else. The copy above says
  // so, so this code has to keep saying so.
  let draft = $state("");
  const reason = $derived(rejectReason(S.alertWords, draft));
  function addAlert(e) {
    e.preventDefault();
    const next = addWord(S.alertWords, draft);
    // addWord returns the SAME array when it refuses, which is how "added" is
    // told from "rejected" without a second validation that could disagree.
    if (next === S.alertWords) {
      if (reason) flash(reason);
      return;
    }
    setAlertWords(next);
    draft = "";
  }

  // "Stay connected" defaults on; toggling flips the pref and the Android
  // foreground service that keeps the P2P node alive in the background.
  let stayConnected = $state(S.prefs.stayConnected !== false);
  function toggleStayConnected() {
    stayConnected = !stayConnected;
    setPref("stayConnected", stayConnected);
    const core = window.Capacitor?.Plugins?.ConcordCore;
    if (stayConnected) core?.startBackground?.().catch(() => {});
    else core?.stopBackground?.().catch(() => {});
  }
</script>

<SettingsShell title="Notifications &amp; sounds" here="notifications" {onClose}>
  <!-- The panel is written so that NOTHING here changes its own height when you
       change a setting. Every state-dependent line is one line in every state,
       and the alert-word validation line is always in the layout. A panel that
       grows a row under the control you just touched moves everything you were
       about to touch next. -->
  <SettingGroup
    label="Pings"
    info="Every guild and channel has its own level — all messages, only @mentions, or nothing — on its {S.isMobile
      ? 'long-press'
      : 'right-click'} menu. This page is about the whole app."
  >
    <SettingRow
      icon="bell"
      title="Do Not Disturb"
      sub="Nothing pings you, mentions included. Badges still count; you stay online."
      checked={dnd}
      onclick={toggleDnd}
    />
  </SettingGroup>

  <SettingGroup label="Sound">
    {#if trouble}
      <!-- Named, not endured. This is the one place a person can act on it. -->
      <div class="trouble" role="status">
        <span class="t-ico"><Icon name="alert" size={15} /></span>
        <p>{trouble}</p>
      </div>
    {/if}
    <SettingRow icon="speaker" title="Volume" sub="Everything Concord plays, from a mention ping to the ringtone.">
      <span class="vol">
        <input
          type="range"
          min="0"
          max="100"
          step="5"
          value={vol}
          aria-label="Sound volume, 0 for silent"
          oninput={setVol}
          onchange={() => vol > 0 && playDone()}
          use:rangefill={vol}
        />
        <span class="volnum" class:off={vol === 0}>{vol === 0 ? "Off" : `${vol}%`}</span>
      </span>
    </SettingRow>
    <SettingRow
      icon="megaphone"
      title="Sound effects"
      sub="Soundboard presses in a voice room, and sound chips people send you."
      checked={board}
      onclick={toggleBoard}
    />
    <SettingRow
      icon="bubble"
      title="Blip on every message"
      sub="A soft note when a message lands in the channel you are already reading."
      checked={S.prefs.soundOnArrive}
      onclick={() => setPref("soundOnArrive", !S.prefs.soundOnArrive)}
    />
    <SettingRow icon="phone" title="Call ringtone" sub="What plays on this device when somebody calls you.">
      <Select
        label="Call ringtone"
        value={ringtone}
        onPick={pickRingtone}
        options={RINGTONE_OPTIONS.map((o) => ({ value: o.id, label: o.label }))}
      />
    </SettingRow>
    <SettingRow icon="play" title="Test the speakers" sub="Plays one chime and checks that it actually reached them.">
      <button class="ghost small-btn" onclick={() => probe(true)} disabled={testing}>
        {testing ? "Listening…" : "Test"}
      </button>
    </SettingRow>
  </SettingGroup>

  <SettingGroup
    label="Alert words"
    info="Concord matches these here, on this machine, against messages that are already on it. Any app with a server has to do this matching on the server, which means telling it what you are watching for. There is no server here to tell."
  >
    <div class="words">
      <p class="say">
        Words that ping you the way your own name does. They are stored on this
        device only — never sent anywhere, never shared with your other devices,
        and never part of any guild.
      </p>
      <form class="add" onsubmit={addAlert}>
        <input
          bind:value={draft}
          placeholder="A word or a short phrase"
          aria-label="Add an alert word"
          maxlength={MAX_LEN}
        />
        <button type="submit" disabled={!draft.trim() || !!reason}>Add</button>
      </form>
      <!-- Always here, empty until there is something to say: a validation line
           that appears pushes the chip list and the counter down the page while
           somebody is typing into the box above it. -->
      <p class="why" aria-live="polite">{reason || ""}</p>
      {#if S.alertWords.length}
        <ul class="chips">
          {#each S.alertWords as w (w)}
            <li>
              <span>{w}</span>
              <button
                aria-label="Remove the alert word {w}"
                use:tooltip={"Remove"}
                onclick={() => setAlertWords(removeWord(S.alertWords, w))}
              >
                <Icon name="close" size={9} />
              </button>
            </li>
          {/each}
        </ul>
        <p class="count">{plural(S.alertWords.length, "word")} of {MAX_WORDS}</p>
      {:else}
        <p class="count">No alert words yet. A project name, a release, a nickname.</p>
      {/if}
    </div>
  </SettingGroup>

  {#if asksLazily() && osKnown}
    <SettingGroup label="System">
      <SettingRow
        icon="bell"
        title="Show notifications"
        sub="Your device's own permission to put Concord in the notification tray."
        info="Concord asks for this the first time you miss a message rather than at startup, because your phone only offers the choice twice before deciding for you."
        onclick={fixOsNotif}
      >
        <span class="perm" class:on={osNotif.enabled}>
          {osNotif.enabled ? "Allowed" : osNotif.canRequest ? "Allow…" : "Blocked"}
        </span>
      </SettingRow>
    </SettingGroup>
  {/if}

  {#if S.isMobile}
    <SettingGroup label="Background">
      <SettingRow
        icon="bell"
        title="Stay connected"
        sub="Keep receiving while Concord is closed."
        info="Without this, messages only arrive while Concord is open — there's no server holding them for you, so the app has to be running to receive."
        checked={stayConnected}
        onclick={toggleStayConnected}
      />
    </SettingGroup>
  {/if}
</SettingsShell>

<style>
  .vol {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .vol input {
    width: 130px;
  }
  .volnum {
    /* Wide enough for "100%" AND for "Off", so the slider does not shift
       sideways as the number crosses a digit or reaches zero. */
    min-width: 4.5ch;
    text-align: right;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  .volnum.off {
    color: var(--text-faint);
    font-weight: 650;
  }
  .small-btn {
    font-size: var(--fs-compact);
    padding: 5px 14px;
    white-space: nowrap;
  }
  /* The OS permission's state, as a word rather than as a sentence that changes
     length. Three states, one line, one width class. */
  .perm {
    font-size: var(--fs-compact);
    font-weight: 650;
    color: var(--text-muted);
    white-space: nowrap;
  }
  .perm.on {
    color: var(--ok-text);
  }

  /* Something is wrong with this machine's audio and no switch on this page can
     fix it. Warn-tinted rather than danger: nothing is broken or lost, there is
     just a package to install. */
  .trouble {
    display: flex;
    align-items: flex-start;
    gap: var(--sp-2);
    padding: 11px 13px;
    background: color-mix(in srgb, var(--warn) 12%, transparent);
    border-bottom: 1px solid var(--hairline);
  }
  .t-ico {
    flex: none;
    display: grid;
    place-items: center;
    width: 22px;
    height: 22px;
    color: var(--warn-text);
  }
  .trouble p {
    margin: 0;
    font-size: var(--fs-compact);
    line-height: 1.5;
    color: var(--text);
  }

  .words {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    padding: var(--sp-3);
  }
  .say {
    margin: 0;
    font-size: var(--fs-compact);
    line-height: 1.55;
    color: var(--text-muted);
  }
  .add {
    display: flex;
    gap: var(--sp-1);
  }
  .add input {
    flex: 1;
    min-width: 0;
  }
  .add button {
    flex: none;
    padding: 6px 14px;
    min-width: 0;
  }
  .why {
    margin: 0;
    /* Reserved, always. See the note at the markup. */
    min-height: 1.4em;
    font-size: var(--fs-tiny);
    line-height: 1.4;
    color: var(--warn-text);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .chips li {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 4px 6px 4px 10px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent-hover);
    font-size: var(--fs-compact);
    unicode-bidi: plaintext;
  }
  .chips button {
    display: grid;
    place-items: center;
    width: 16px;
    height: 16px;
    min-width: 0;
    padding: 0;
    border-radius: 50%;
    background: transparent;
    color: inherit;
  }
  .chips button:hover {
    background: color-mix(in srgb, var(--accent) 30%, transparent);
  }
  .count {
    margin: 0;
    font-size: var(--fs-tiny);
    color: var(--text-faint);
  }
</style>
