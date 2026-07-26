<script>
  // Privacy & safety: the switches whose defaults are deliberate, each with the
  // reason it's set that way. These used to sit in the main Settings list with
  // their paragraphs attached, which is what made that list a wall.
  import Modal from "./Modal.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import { api } from "../lib/api.js";
  import { bioEnrolled } from "../lib/biometric.js";
  import { S, setPref, flash } from "../lib/state.svelte.js";

  let { onClose } = $props();

  // Empty trash: irreversibly scrub retained bodies of deleted messages so a
  // moderator can no longer reveal any of them on this device.
  let purging = $state(false);
  function emptyTrash() {
    S.modal = {
      kind: "confirm",
      title: "Empty deleted-message trash?",
      body: "Every deleted message's retained text is permanently erased on this device. This can't be undone, and 'Show original' will have nothing left to reveal.",
      confirmLabel: "Empty trash",
      onConfirm: async () => {
        S.modal = null;
        purging = true;
        try {
          const n = await api.emptyTrash("");
          flash(`Erased ${n} deleted message${n === 1 ? "" : "s"}`, "success");
        } catch (err) {
          flash(err);
        } finally {
          purging = false;
        }
      },
    };
  }

  let richPresence = $state(false);
  // Linux (and the Linux web build) can read MPRIS now-playing today.
  const richPresenceSupported = /linux|x11/i.test(navigator.userAgent);
  async function toggleRichPresence() {
    richPresence = !richPresence;
    try {
      await api.setRichPresence(richPresence);
    } catch (err) {
      richPresence = !richPresence; // revert on failure
      flash(err);
    }
  }
  (async () => {
    try {
      richPresence = await api.richPresenceEnabled();
    } catch {
      /* older backend: the switch just starts off */
    }
  })();
</script>

<Modal title="Privacy &amp; safety" {onClose} wide>
  <SettingGroup
    label="What leaves this device"
    note="Link previews are off by default because loading one reveals your IP and
          the moment you were online to whoever hosts the link — a message with a
          link to an attacker's server would otherwise deanonymize you with no
          click at all. Relaying calls costs a little latency and hides your IP
          from the people you're talking to; meetings with browser guests always
          relay."
  >
    <SettingRow
      icon="screen"
      title="Link previews"
      sub="Fetch a preview for links in messages"
      checked={S.prefs.linkPreviews}
      onclick={() => setPref("linkPreviews", !S.prefs.linkPreviews)}
    />
    <SettingRow
      icon="lock"
      title="Hide my IP on calls"
      sub="Relay call media instead of connecting directly"
      checked={S.prefs.hideCallIp}
      onclick={() => setPref("hideCallIp", !S.prefs.hideCallIp)}
    />
    <SettingRow
      icon="spark"
      title="Rich presence"
      sub={richPresenceSupported
        ? "Show what you're listening to as your status"
        : "Not supported on this platform yet"}
      checked={richPresence}
      disabled={!richPresenceSupported}
      onclick={toggleRichPresence}
    />
  </SettingGroup>

  <SettingGroup
    label="Deleted messages"
    note="Deleted messages simply disappear unless you ask for a marker. Emptying
          the trash erases the retained text for good, so 'Show original' has
          nothing left to reveal on this device."
  >
    <SettingRow
      icon="trash"
      title="Show deleted messages"
      sub="Leave a faint marker where one used to be"
      checked={S.prefs.showDeleted}
      onclick={() => setPref("showDeleted", !S.prefs.showDeleted)}
    />
    <SettingRow
      icon="trash"
      title={purging ? "Emptying…" : "Empty trash"}
      sub="Erase every deleted message's retained text"
      danger
      disabled={purging}
      onclick={emptyTrash}
    />
  </SettingGroup>

  <SettingGroup label="People">
    <SettingRow
      icon="lock"
      title="Blocked users"
      sub="People who can't add you to DMs or servers"
      to="blocked"
      from="privacy"
    />
  </SettingGroup>

  {#if S.isMobile}
    <SettingGroup label="This device">
      <SettingRow
        icon="lock"
        title="App lock"
        sub={bioEnrolled()
          ? "Require fingerprint when opening the app"
          : "Enable biometric unlock first"}
        checked={bioEnrolled() && S.prefs.appLock === true}
        disabled={!bioEnrolled()}
        onclick={() => setPref("appLock", !(S.prefs.appLock === true))}
      />
    </SettingGroup>
  {/if}
</Modal>
