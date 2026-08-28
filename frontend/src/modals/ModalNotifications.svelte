<script>
  // Everything that makes noise or lights up. Split out of Settings so the
  // top level can stay a short list of places to go.
  import Modal from "./Modal.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import Icon from "../Icon.svelte";
  import { S, setPref, flash, patchProfile, setAlertWords } from "../lib/state.svelte.js";
  import { tooltip } from "../lib/tooltip.js";
  import { plural } from "../lib/plural.js";
  import { addWord, removeWord, rejectReason, MAX_WORDS, MAX_LEN } from "../lib/alertwords.js";
  import { asksLazily, notificationStatus, requestPermission, openSystemSettings } from "../lib/notify.js";
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
  } from "../lib/sounds.js";

  let { onClose } = $props();

  let sounds = $state(soundsEnabled());
  function toggleSounds() {
    sounds = !sounds;
    setSoundsEnabled(sounds);
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

  // The section is headed HOW LOUD and contained nothing that answered it —
  // two on/off switches and a ringtone picker. The slider auditions itself on
  // release rather than on every input event: dragging through forty values
  // must not fire forty chimes.
  let vol = $state(Math.round(soundVolume() * 100));
  function setVol(e) {
    vol = Number(e.target.value);
    setSoundVolume(vol / 100);
  }

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

<Modal title="Notifications &amp; sounds" {onClose} wide>
  <SettingGroup
    label="How loud"
    info="Every guild and channel has its own level — all messages, only @mentions, or nothing — on its {S.isMobile
      ? 'long-press'
      : 'right-click'} menu."
  >
    <SettingRow
      icon="bell"
      title="Do Not Disturb"
      info="Silences every ping and chime, mentions included, without hiding your unread badges and without going offline."
      sub={dnd ? "On — nothing will ping you" : "Silence every ping without going offline"}
      checked={dnd}
      onclick={toggleDnd}
    />
  </SettingGroup>

  <SettingGroup>
    <SettingRow
      icon="speaker"
      title="Sounds"
      sub="Voice join/leave chimes and @mention pings"
      checked={sounds}
      onclick={toggleSounds}
    />
    <SettingRow
      icon="megaphone"
      title="Sound effects"
      sub="The voice-room soundboard and sound chips other people send"
      checked={board}
      onclick={toggleBoard}
    />
    <SettingRow
      icon="bubble"
      title="Sound on every message"
      sub="A soft blip when a message lands in the channel you're looking at"
      checked={S.prefs.soundOnArrive}
      onclick={() => setPref("soundOnArrive", !S.prefs.soundOnArrive)}
    />
    <SettingRow icon="speaker" title="Volume" sub="Everything this app plays, from the mention ping to the ringtone">
      <span class="vol">
        <input
          type="range"
          min="0"
          max="100"
          step="5"
          value={vol}
          disabled={!sounds}
          aria-label="Sound volume"
          oninput={setVol}
          onchange={() => sounds && playDone()}
        />
        <span class="volnum">{vol}%</span>
      </span>
    </SettingRow>
    <SettingRow icon="phone" title="Call ringtone" sub="Plays when a friend calls you">
      <select class="pick" value={ringtone} onchange={(e) => pickRingtone(e.target.value)} aria-label="Call ringtone">
        {#each RINGTONE_OPTIONS as o (o.id)}
          <option value={o.id}>{o.label}</option>
        {/each}
      </select>
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
      {#if reason}<p class="why">{reason}</p>{/if}
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
        sub={osNotif.enabled
          ? "On — messages appear in your notification tray"
          : osNotif.canRequest
            ? "Off — tap to allow Concord to notify you"
            : "Blocked — tap to change it in system settings"}
        info="This is your device's own permission, not a Concord setting. Concord asks for it the first time you miss a message rather than at startup, because your phone only offers the choice twice before deciding for you."
        onclick={fixOsNotif}
      />
    </SettingGroup>
  {/if}

  {#if S.isMobile}
    <SettingGroup label="Background">
      <SettingRow
        icon="bell"
        title="Stay connected"
        sub="Receive messages in the background"
        info="Without this, messages only arrive while Concord is open — there's no server holding them for you, so the app has to be running to receive."
        checked={stayConnected}
        onclick={toggleStayConnected}
      />
    </SettingGroup>
  {/if}
</Modal>

<style>
  .vol {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .vol input {
    width: 120px;
  }
  .volnum {
    min-width: 4ch;
    text-align: right;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
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
    font-size: var(--fs-tiny);
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
  .pick {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--fs-compact);
    padding: 6px 8px;
    max-width: 100%;
  }
</style>
