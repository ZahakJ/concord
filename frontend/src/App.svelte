<script>
  // App.svelte is the shell: login gate, the four-column layout, voice
  // lifecycle, global shortcuts, and modal routing. All shared state and the
  // backend event wiring live in lib/state.svelte.js.
  import { onMount } from "svelte";
  import { api } from "./lib/api.js";
  import { VoiceMesh } from "./lib/voice.js";
  import { requestPermission } from "./lib/notify.js";
  import { installShortcuts } from "./lib/shortcuts.js";
  import { playVoiceJoin, playVoiceLeave } from "./lib/sounds.js";
  import {
    S,
    activeGuild,
    onLogin,
    refreshGuilds,
    refreshRightPanel,
    applyAccent,
    flash,
  } from "./lib/state.svelte.js";

  import Login from "./Login.svelte";
  import GuildRail from "./GuildRail.svelte";
  import ChannelList from "./ChannelList.svelte";
  import ChatHeader from "./ChatHeader.svelte";
  import VoicePanel from "./VoicePanel.svelte";
  import MessageList from "./MessageList.svelte";
  import Composer from "./Composer.svelte";
  import MemberPanel from "./MemberPanel.svelte";
  import QuickSwitcher from "./QuickSwitcher.svelte";
  import ProfilePopover from "./ProfilePopover.svelte";
  import ModalCreate from "./modals/ModalCreate.svelte";
  import ModalCreateChannel from "./modals/ModalCreateChannel.svelte";
  import ModalJoin from "./modals/ModalJoin.svelte";
  import ModalInvite from "./modals/ModalInvite.svelte";
  import ModalProfile from "./modals/ModalProfile.svelte";
  import ModalSettings from "./modals/ModalSettings.svelte";
  import ConfirmDialog from "./modals/ConfirmDialog.svelte";

  let composer = $state(null);

  // DMs (incl. the self-DM "Notes") drop the member panel for a roomier view.
  const isDM = $derived(activeGuild()?.kind === "dm");

  // Skip the login screen if the backend is already unlocked (e.g. after a
  // browser refresh — the Go process stays running and holds the session).
  onMount(async () => {
    try {
      if (await api.session()) await start();
    } catch {
      /* not unlocked yet — show the login screen */
    }
  });

  async function start() {
    await onLogin();
    requestPermission();
    installShortcuts();
  }

  // ---- voice lifecycle (owns the mesh; state lives in S) ----

  async function joinVoice(channelId = S.activeChannelId) {
    if (!channelId) return;
    if (S.voice) {
      if (S.voice.channelId === channelId) return; // already in it
      await leaveVoice(); // switch rooms
    }
    const mesh = new VoiceMesh({
      selfPeerId: S.identity.peerId,
      channelId,
      relay: api.relaySignal,
      onRoster: (ids) => (S.voiceParticipants = ids),
      onSpeaking: (keys) => (S.voiceSpeaking = keys),
    });
    try {
      await mesh.start();
    } catch {
      flash("Microphone access denied");
      return;
    }
    S.voice = { mesh, channelId };
    await api.joinVoice(channelId);
    playVoiceJoin();
    flash("Joined voice");
  }

  async function leaveVoice() {
    if (!S.voice) return;
    const ch = S.voice.channelId;
    S.voice.mesh.stop();
    S.voice = null;
    S.voiceParticipants = [];
    S.voiceSpeaking = [];
    S.voicePeerFpr = {};
    S.muted = false;
    playVoiceLeave();
    await api.leaveVoice(ch);
  }

  function toggleMicMute() {
    S.muted = !S.muted;
    S.voice?.mesh.setMuted(S.muted);
  }

  // ---- modal handlers ----

  async function createGuild(name) {
    if (!name?.trim()) return;
    await api.createGuild(name.trim());
    await refreshGuilds();
    S.modal = null;
  }

  async function createChannel({ name, type }) {
    if (!name?.trim() || !S.activeGuildId) return;
    await api.createChannel(S.activeGuildId, name.trim(), type || "");
    await refreshGuilds();
    S.modal = null;
  }

  async function createCategory(name) {
    if (!name?.trim() || !S.activeGuildId) return;
    await api.createCategory(S.activeGuildId, name.trim());
    await refreshGuilds();
    S.modal = null;
  }

  async function renameGuild(name) {
    if (!name?.trim() || !S.activeGuildId) return;
    await api.renameGuild(S.activeGuildId, name.trim());
    await refreshGuilds();
    S.modal = null;
  }

  async function joinGuild(code) {
    if (!code?.trim()) return;
    try {
      await api.joinViaInvite(code.trim());
      await refreshGuilds();
      S.modal = null;
    } catch (err) {
      S.modal = { ...S.modal, error: String(err?.message || err) };
    }
  }

  async function saveProfile(p) {
    await api.setProfile(p.name, p.status, p.emoji, p.color, p.avatar || "");
    S.identity = await api.identity();
    S.displayName = S.identity.displayName || "";
    applyAccent(S.identity.color);
    await refreshRightPanel();
    S.modal = null;
    flash("Profile updated");
  }

  function copy(text) {
    navigator.clipboard?.writeText(text);
    flash("Copied to clipboard");
  }
</script>

{#if !S.ready}
  <Login onLogin={start} />
{:else}
  <div class="app" class:no-panel={isDM}>
    <GuildRail />
    <ChannelList onJoinVoice={joinVoice} />

    <main class="chat">
      <ChatHeader onJoinVoice={joinVoice} onLeaveVoice={leaveVoice} onToggleMute={toggleMicMute} />
      {#if S.voice && S.voice.channelId === S.activeChannelId}
        <VoicePanel />
      {/if}
      <MessageList onDropFiles={(files) => files.forEach((f) => composer?.attachFile(f))} />
      <Composer bind:this={composer} />
    </main>

    {#if !isDM}
      <MemberPanel />
    {/if}
  </div>

  {#if S.quickSwitcher}
    <QuickSwitcher />
  {/if}

  <ProfilePopover />

  {#if S.toast}<div class="toast">{S.toast}</div>{/if}

  <!-- Modals -->
  {#if S.modal?.kind === "create"}
    <ModalCreate onSubmit={createGuild} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "channel"}
    <ModalCreateChannel onSubmit={createChannel} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "category"}
    <ModalCreate
      onSubmit={createCategory}
      onClose={() => (S.modal = null)}
      title="Create a category"
      hint="Groups channels in the sidebar."
      placeholder="Category name"
    />
  {:else if S.modal?.kind === "rename"}
    <ModalCreate
      onSubmit={renameGuild}
      onClose={() => (S.modal = null)}
      title="Rename server"
      hint="Renames the server for everyone."
      placeholder={activeGuild()?.name || "New name"}
    />
  {:else if S.modal?.kind === "profile"}
    <ModalProfile identity={S.identity} onSubmit={saveProfile} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "settings"}
    <ModalSettings onClose={() => (S.modal = null)} onSaved={() => flash("Rendezvous saved")} />
  {:else if S.modal?.kind === "join"}
    <ModalJoin error={S.modal.error} onSubmit={joinGuild} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "invite"}
    <ModalInvite code={S.modal.code} onCopy={copy} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "confirm"}
    <ConfirmDialog
      title={S.modal.title}
      body={S.modal.body}
      confirmLabel={S.modal.confirmLabel}
      onConfirm={S.modal.onConfirm}
      onClose={() => (S.modal = null)}
    />
  {/if}
{/if}

<style>
  .app {
    display: grid;
    grid-template-columns: 64px 220px 1fr 260px;
    height: 100%;
    /* Pin the single row to the viewport so tall columns (the chat feed)
       scroll internally instead of pushing the layout past the screen. */
    grid-template-rows: 100%;
    overflow: hidden;
  }
  .app.no-panel {
    grid-template-columns: 64px 220px 1fr;
  }
  @media (max-width: 900px) {
    .app {
      grid-template-columns: 64px 190px 1fr;
    }
    .app > :global(.panel) {
      display: none;
    }
  }
  .chat {
    display: flex;
    flex-direction: column;
    min-width: 0;
    /* min-height:0 lets the feed (flex:1) shrink and scroll rather than
       growing to fit every message and shoving the composer off-screen. */
    min-height: 0;
    overflow: hidden;
    background: var(--bg-2);
  }
  .toast {
    position: fixed;
    bottom: 20px;
    left: 50%;
    transform: translateX(-50%);
    background: var(--bg-1);
    border: 1px solid var(--border);
    padding: 10px 16px;
    border-radius: var(--radius-md);
    font-size: 13px;
    box-shadow: var(--shadow-pop);
    z-index: 200;
  }
</style>
