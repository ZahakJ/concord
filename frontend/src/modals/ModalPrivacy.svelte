<script>
  // Privacy & safety: the switches whose defaults are deliberate, each with the
  // reason it's set that way. These used to sit in the main Settings list with
  // their paragraphs attached, which is what made that list a wall.
  import Modal from "./Modal.svelte";
  import ConfirmDialog from "./ConfirmDialog.svelte";
  import SettingGroup from "./SettingGroup.svelte";
  import SettingRow from "./SettingRow.svelte";
  import { api } from "../lib/api.js";
  import { bioEnrolled } from "../lib/biometric.js";
  import { S, setPref, flash } from "../lib/state.svelte.js";

  let { onClose } = $props();

  // Empty trash: irreversibly scrub retained bodies of deleted messages so a
  // moderator can no longer reveal any of them on this device.
  let purging = $state(false);
  // Rendered locally, NOT pushed through S.modal. App.svelte renders one modal
  // from a single {#if} chain, so setting S.modal here REPLACES this panel —
  // and ConfirmDialog's own onClose then clears S.modalStack, so cancelling a
  // confirmation reached via Settings → Privacy dropped you all the way back to
  // the chat pane. Layering the component keeps the panel and its trail intact.
  let confirmPurge = $state(false);
  async function emptyTrash() {
    confirmPurge = false;
    purging = true;
    try {
      const n = await api.emptyTrash("");
      flash(`Erased ${n} deleted message${n === 1 ? "" : "s"}`, "success");
    } catch (err) {
      flash(err);
    } finally {
      purging = false;
    }
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
  // Typing indicators are reciprocal by design (see internal/app/typing.go):
  // off means you neither send nor see them. There is no server to enforce a
  // one-way deal, and a client that took without giving would just be lying to
  // its friends.
  let typing = $state(true);
  async function toggleTyping() {
    typing = !typing;
    try {
      await api.setTypingEnabled(typing);
    } catch (err) {
      typing = !typing; // revert on failure
      flash(err);
    }
  }

  (async () => {
    try {
      richPresence = await api.richPresenceEnabled();
    } catch {
      /* older backend: the switch just starts off */
    }
    try {
      typing = await api.typingEnabled();
    } catch {
      /* older backend: indicators are on, as they always were */
    }
  })();
</script>

<Modal title="Privacy &amp; safety" {onClose} wide>
  <SettingGroup
    label="What leaves this device"
    info="Read receipts need no switch here: your read state only ever travels to your own devices, so nobody else was ever told."
  >
    <SettingRow
      icon="screen"
      title="Link previews"
      sub="Fetch a preview for links in messages"
      info="Off by default. Loading a preview reveals your IP and the moment you were online to whoever hosts the link, so a message containing a link to an attacker's server would deanonymize you with no click at all."
      checked={S.prefs.linkPreviews}
      onclick={() => setPref("linkPreviews", !S.prefs.linkPreviews)}
    />
    <SettingRow
      icon="spark"
      title="Game box art"
      sub="Load real cover images for game collections"
      info="Off by default. Covers come from Valve's CDN and are fetched by the app itself, so opening a profile that lists games would tell Valve your IP and the moment you were online — without you clicking anything. Off, collections show generated covers instead and nothing leaves the device. Searching for a game to add always contacts Steam, whatever this says."
      checked={S.prefs.gameCovers}
      onclick={() => setPref("gameCovers", !S.prefs.gameCovers)}
    />
    <SettingRow
      icon="lock"
      title="Hide my IP on calls"
      sub="Relay call media instead of connecting directly"
      info="Costs a little latency and hides your IP from the people you're talking to. Meetings with browser guests always relay, whatever this says."
      checked={S.prefs.hideCallIp}
      onclick={() => setPref("hideCallIp", !S.prefs.hideCallIp)}
    />
    <SettingRow
      icon="edit"
      title="Typing indicators"
      sub="Show others you're typing — and see when they are"
      info="They go both ways. Switch them off and you stop sending them and stop seeing them — there's no server to enforce a one-way deal, and a client that took without giving would just be lying to its friends."
      checked={typing}
      onclick={toggleTyping}
    />
    <SettingRow
      icon="spark"
      title="Rich presence"
      info="Reads what your music player is playing (over MPRIS) and shows it as your status to people who can already see you. Nothing is sent anywhere else."
      sub={richPresenceSupported
        ? "Show what you're listening to as your status"
        : "Not supported on this platform yet"}
      checked={richPresence}
      disabled={!richPresenceSupported}
      onclick={toggleRichPresence}
    />
  </SettingGroup>

  <SettingGroup label="Deleted messages">
    <SettingRow
      icon="trash"
      title="Show deleted messages"
      sub="Leave a faint marker where one used to be"
      info="Off, a deleted message simply disappears. On, a faint marker stays where it was, and a moderator can reveal the original text."
      checked={S.prefs.showDeleted}
      onclick={() => setPref("showDeleted", !S.prefs.showDeleted)}
    />
    <SettingRow
      icon="trash"
      title={purging ? "Emptying…" : "Empty trash"}
      sub="Erase every deleted message's retained text"
      info="Erases the retained text for good, so 'Show original' has nothing left to reveal on this device. This can't be undone."
      danger
      disabled={purging}
      onclick={() => (confirmPurge = true)}
    />
  </SettingGroup>

  <SettingGroup label="People">
    <SettingRow
      icon="members"
      title="Message requests"
      info="A DM from someone you don't share a server with, haven't verified and never messaged first waits here. Concord holds their invitation without opening it, so until you accept they can't see your profile, your presence, or that you're even there."
      sub={S.requests.length
        ? `${S.requests.length} waiting`
        : "DMs from people you don't know yet"}
      to="requests"
      from="privacy"
    />
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

{#if confirmPurge}
  <ConfirmDialog
    title="Empty deleted-message trash?"
    body="Every deleted message's retained text is permanently erased on this device. This can't be undone, and 'Show original' will have nothing left to reveal."
    confirmLabel="Empty trash"
    onConfirm={emptyTrash}
    onClose={() => (confirmPurge = false)}
  />
{/if}
