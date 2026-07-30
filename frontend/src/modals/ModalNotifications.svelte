<script>
  // Everything that makes noise or lights up. Split out of Settings so the
  // top level can stay a short list of places to go.
  import Modal from "./Modal.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import { S, setPref, flash, patchProfile } from "../lib/state.svelte.js";
  import {
    soundsEnabled,
    setSoundsEnabled,
    RINGTONE_OPTIONS,
    getRingtone,
    setRingtone,
    previewRingtone,
  } from "../lib/sounds.js";

  let { onClose } = $props();

  let sounds = $state(soundsEnabled());
  function toggleSounds() {
    sounds = !sounds;
    setSoundsEnabled(sounds);
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

  let ringtone = $state(getRingtone());
  function pickRingtone(id) {
    ringtone = id;
    setRingtone(id);
    previewRingtone(id); // audition the choice immediately
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
    info="Every server and channel has its own level — all messages, only @mentions, or nothing — on its {S.isMobile
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
    <SettingRow icon="phone" title="Call ringtone" sub="Plays when a friend calls you">
      <select class="pick" value={ringtone} onchange={(e) => pickRingtone(e.target.value)} aria-label="Call ringtone">
        {#each RINGTONE_OPTIONS as o (o.id)}
          <option value={o.id}>{o.label}</option>
        {/each}
      </select>
    </SettingRow>
  </SettingGroup>

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
