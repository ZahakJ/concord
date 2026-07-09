<script>
  // App.svelte is the shell: login gate, the four-column layout, voice
  // lifecycle, global shortcuts, and modal routing. All shared state and the
  // backend event wiring live in lib/state.svelte.js.
  import { onMount } from "svelte";
  import { api } from "./lib/api.js";
  import { VoiceMesh } from "./lib/voice.js";
  import { requestPermission } from "./lib/notify.js";
  import { installShortcuts } from "./lib/shortcuts.js";
  import { playVoiceJoin, playVoiceLeave, playRing } from "./lib/sounds.js";
  import {
    S,
    activeGuild,
    onLogin,
    refreshGuilds,
    refreshRightPanel,
    applyAccent,
    flash,
    setVideoStream,
    clearVideoStreams,
    incomingCall,
    jumpToChannel,
    checkForUpdate,
    dismissUpdate,
    setChannelTopic,
  } from "./lib/state.svelte.js";

  import Login from "./Login.svelte";
  import GuildRail from "./GuildRail.svelte";
  import ChannelList from "./ChannelList.svelte";
  import ChatHeader from "./ChatHeader.svelte";
  import VoicePanel from "./VoicePanel.svelte";
  import MessageList from "./MessageList.svelte";
  import Composer from "./Composer.svelte";
  import MemberPanel from "./MemberPanel.svelte";
  import Welcome from "./Welcome.svelte";
  import QuickSwitcher from "./QuickSwitcher.svelte";
  import ProfilePopover from "./ProfilePopover.svelte";
  import ContextMenu from "./ContextMenu.svelte";
  import FloatingCall from "./FloatingCall.svelte";
  import ModalCreate from "./modals/ModalCreate.svelte";
  import ModalCreateChannel from "./modals/ModalCreateChannel.svelte";
  import ModalEmoji from "./modals/ModalEmoji.svelte";
  import ModalForward from "./modals/ModalForward.svelte";
  import ModalBans from "./modals/ModalBans.svelte";
  import ModalRoles from "./modals/ModalRoles.svelte";
  import ModalGuildSettings from "./modals/ModalGuildSettings.svelte";
  import ModalChannelTopic from "./modals/ModalChannelTopic.svelte";
  import ModalShortcuts from "./modals/ModalShortcuts.svelte";
  import ModalNewDM from "./modals/ModalNewDM.svelte";
  import ModalRenameGroup from "./modals/ModalRenameGroup.svelte";
  import ModalJoin from "./modals/ModalJoin.svelte";
  import ModalInvite from "./modals/ModalInvite.svelte";
  import ModalProfile from "./modals/ModalProfile.svelte";
  import ModalSettings from "./modals/ModalSettings.svelte";
  import ConfirmDialog from "./modals/ConfirmDialog.svelte";

  let composer = $state(null);

  // DMs (incl. the self-DM "Notes") drop the member panel for a roomier view.
  const isDM = $derived(activeGuild()?.kind === "dm");
  // No open channel → show the welcome screen instead of an empty chat.
  const hasChannel = $derived(!!S.activeChannelId && !!activeGuild());

  // Voice: the call box shows inline on its own channel; navigate away and it
  // pins to a small draggable floating window instead.
  // Friendly OS name for the update banner's download button (cosmetic — the
  // actual asset is chosen server-side from runtime.GOOS).
  const ua = navigator.userAgent;
  const osLabel = /windows/i.test(ua)
    ? "Windows"
    : /mac/i.test(ua)
      ? "macOS"
      : /linux|x11/i.test(ua)
        ? "Linux"
        : "your OS";

  const callHere = $derived(S.voice && S.voice.channelId === S.activeChannelId);
  const callElsewhere = $derived(S.voice && S.voice.channelId !== S.activeChannelId);
  const call = $derived(incomingCall());
  const ringingChannel = $derived(call?.channelId || "");
  // Friendly label for the floating call window: DM name, or "Guild · #ch".
  const callLabel = $derived.by(() => {
    if (!S.voice) return "";
    for (const gg of S.guilds) {
      const c = gg.channels.find((x) => x.id === S.voice.channelId);
      if (c) return gg.kind === "dm" ? gg.name : `${gg.name} · ${c.name}`;
    }
    return "";
  });

  // Ring while a DM call is incoming; stops when accepted, declined, or ended.
  $effect(() => {
    if (!ringingChannel) return;
    playRing();
    const id = setInterval(playRing, 2400);
    return () => clearInterval(id);
  });

  async function acceptCall(channelId) {
    await jumpToChannel(channelId); // open the DM so the call box is in view
    await joinVoice(channelId);
  }
  function declineCall(channelId) {
    S.dismissedCalls = [...S.dismissedCalls, channelId];
  }

  // Skip the login screen if the backend is already unlocked (e.g. after a
  // browser refresh — the Go process stays running and holds the session).
  onMount(async () => {
    checkForUpdate(); // fire-and-forget; works on the login screen too
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

  let joining = false;
  async function joinVoice(channelId = S.activeChannelId) {
    if (!channelId || joining) return; // re-entrancy guard: no orphan meshes
    if (S.voice) {
      const inThisRoom = S.voice.channelId === channelId;
      await leaveVoice();
      // Re-clicking the room you're already in toggles you out (no need to hunt
      // for the disconnect button). Clicking a different one switches rooms.
      if (inThisRoom) return;
    }
    joining = true;
    const mesh = new VoiceMesh({
      selfPeerId: S.identity.peerId,
      channelId,
      relay: api.relaySignal,
      onRoster: (ids) => (S.voiceParticipants = ids),
      onSpeaking: (keys) => (S.voiceSpeaking = keys),
      onVideo: (key, stream, meta) => setVideoStream(key, stream, meta),
      onVideoState: (kind, on) => {
        // If a source stopped via the browser's own chrome (not our button),
        // update the flag and drop our local preview tile too.
        if (kind === "screen") {
          S.sharing = on;
          if (!on) setVideoStream("self:screen", null);
        } else {
          S.cameraOn = on;
          if (!on) setVideoStream("self:camera", null);
        }
      },
    });
    try {
      await mesh.start();
    } catch {
      flash("Microphone access denied");
      joining = false;
      return;
    }
    try {
      await api.joinVoice(channelId);
    } catch (err) {
      // Presence never broadcast — don't leave a phantom "in call" with a live
      // mesh (open mic, peer connections) that leaveVoice can't fully undo.
      mesh.stop();
      flash(err);
      joining = false;
      return;
    }
    S.voice = { mesh, channelId };
    // Rejoining a call clears any prior "declined" suppression for this channel.
    if (S.dismissedCalls.includes(channelId))
      S.dismissedCalls = S.dismissedCalls.filter((c) => c !== channelId);
    playVoiceJoin();
    flash("Joined voice");
    joining = false;
  }

  async function leaveVoice() {
    if (!S.voice) return;
    const ch = S.voice.channelId;
    S.voice.mesh.stop();
    S.voice = null;
    // Suppress the incoming-call ring for this channel: the others may still be
    // in the room, and without this you'd immediately re-ring yourself for the
    // call you just left. (Auto-cleared when the room's roster empties.)
    if (ch && !S.dismissedCalls.includes(ch)) S.dismissedCalls = [...S.dismissedCalls, ch];
    S.voiceParticipants = [];
    S.voiceSpeaking = [];
    S.voicePeerFpr = {};
    S.muted = false;
    S.sharing = false;
    S.cameraOn = false;
    clearVideoStreams();
    playVoiceLeave();
    await api.leaveVoice(ch);
  }

  function toggleMicMute() {
    S.muted = !S.muted;
    S.voice?.mesh.setMuted(S.muted);
  }

  async function toggleScreenShare() {
    if (!S.voice) return;
    const stream = await S.voice.mesh.toggleVideo("screen");
    setVideoStream("self:screen", stream, { self: true, kind: "screen" });
  }

  async function toggleCamera() {
    if (!S.voice) return;
    const stream = await S.voice.mesh.toggleVideo("camera");
    setVideoStream("self:camera", stream, { self: true, kind: "camera" });
  }

  // ---- modal handlers ----

  async function createGuild(name) {
    if (!name?.trim()) return;
    await api.createGuild(name.trim());
    await refreshGuilds();
    S.modal = null;
  }

  async function createChannel({ name, type, category }) {
    if (!name?.trim() || !S.activeGuildId) return;
    await api.createChannel(S.activeGuildId, name.trim(), type || "", category || "");
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
    await api.setProfile(p.name, p.status, p.emoji, p.color, p.avatar || "", p.banner || "", p.presence || "", p.bio || "");
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

{#if S.update && !ringingChannel}
  <div class="update-banner">
    <span class="ub-text">
      <strong>Update available</strong> — Concord {S.update.latest} is out (you have {S.update.current}).
    </span>
    {#if S.update.download}
      <!-- One-click: the direct asset for this machine's OS. -->
      <a
        class="ub-dl"
        href={S.update.download}
        target="_blank"
        rel="noopener noreferrer"
        title={S.update.asset}
      >
        Download for {osLabel}
      </a>
      <a class="ub-alt" href={S.update.url} target="_blank" rel="noopener noreferrer">All files</a>
    {:else}
      <a class="ub-dl" href={S.update.url} target="_blank" rel="noopener noreferrer">Download</a>
    {/if}
    <button class="ub-close" onclick={dismissUpdate} aria-label="Dismiss">×</button>
  </div>
{/if}

{#if !S.ready}
  <Login onLogin={start} />
{:else}
  <div class="app" class:no-panel={isDM || !hasChannel}>
    <GuildRail />
    <ChannelList
      onJoinVoice={joinVoice}
      onLeaveVoice={leaveVoice}
      onToggleMute={toggleMicMute}
      onToggleShare={toggleScreenShare}
      onToggleCamera={toggleCamera}
    />

    <main class="chat">
      {#if hasChannel}
        <ChatHeader
          onJoinVoice={joinVoice}
          onLeaveVoice={leaveVoice}
          onToggleMute={toggleMicMute}
          onToggleShare={toggleScreenShare}
          onToggleCamera={toggleCamera}
        />
        {#if callHere}
          <VoicePanel
            onLeaveVoice={leaveVoice}
            onToggleMute={toggleMicMute}
            onToggleShare={toggleScreenShare}
            onToggleCamera={toggleCamera}
          />
        {/if}
        <MessageList onDropFiles={(files) => files.forEach((f) => composer?.attachFile(f))} />
        <Composer bind:this={composer} />
      {:else}
        <Welcome />
      {/if}
    </main>

    {#if !isDM && hasChannel}
      <MemberPanel />
    {/if}
  </div>

  {#if S.quickSwitcher}
    <QuickSwitcher />
  {/if}

  <ProfilePopover />
  <ContextMenu />

  <!-- Ongoing call you've navigated away from: a draggable pinned window. -->
  {#if callElsewhere}
    <FloatingCall
      label={callLabel}
      onLeave={leaveVoice}
      onToggleMute={toggleMicMute}
      onReturn={() => jumpToChannel(S.voice.channelId)}
    />
  {/if}

  <!-- Someone is ringing you in a DM. -->
  {#if call}
    <div class="ring-card">
      <span class="ring-pulse"></span>
      <div class="ring-info">
        <strong>{call.name}</strong>
        <span class="muted">is calling you…</span>
      </div>
      <div class="ring-actions">
        <button class="ring-btn decline" onclick={() => declineCall(call.channelId)}>Decline</button>
        <button class="ring-btn accept" onclick={() => acceptCall(call.channelId)}>Join</button>
      </div>
    </div>
  {/if}

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
  {:else if S.modal?.kind === "emoji"}
    <ModalEmoji onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "forward"}
    <ModalForward message={S.modal.message} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "bans"}
    <ModalBans onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "roles"}
    <ModalRoles onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "guildSettings"}
    <ModalGuildSettings onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "shortcuts"}
    <ModalShortcuts onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "newDM"}
    <ModalNewDM onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "renameGroup"}
    <ModalRenameGroup
      guildId={S.modal.guildId}
      current={S.modal.current}
      onClose={() => (S.modal = null)}
    />
  {:else if S.modal?.kind === "channelTopic"}
    <ModalChannelTopic
      channel={S.modal.channel}
      onSubmit={(t) => {
        setChannelTopic(S.modal.channel, t.trim());
        S.modal = null;
      }}
      onClose={() => (S.modal = null)}
    />
  {:else if S.modal?.kind === "rename"}
    <ModalCreate
      onSubmit={renameGuild}
      onClose={() => (S.modal = null)}
      title="Rename guild"
      hint="Renames the guild for everyone."
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
    /* Both selectors so .app.no-panel (higher specificity) doesn't keep the
       wide 220px column in DM/Welcome view below 900px. */
    .app,
    .app.no-panel {
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
  /* Update-available banner: a floating top-center pill (doesn't cover the rail). */
  .update-banner {
    position: fixed;
    top: 12px;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: 12px;
    max-width: calc(100vw - 24px);
    padding: 8px 10px 8px 16px;
    background: var(--bg-1);
    border: 1px solid var(--accent);
    border-radius: 22px;
    box-shadow: var(--shadow-pop);
    z-index: 205;
    font-size: 13px;
  }
  .ub-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ub-dl {
    flex-shrink: 0;
    padding: 5px 14px;
    background: var(--accent);
    color: #fff;
    border-radius: 14px;
    font-weight: 600;
    text-decoration: none;
  }
  .ub-dl:hover {
    background: var(--accent-hover);
  }
  .ub-alt {
    flex-shrink: 0;
    color: var(--text-muted);
    text-decoration: none;
    font-size: 12px;
  }
  .ub-alt:hover {
    color: var(--text);
    text-decoration: underline;
  }
  .ub-close {
    flex-shrink: 0;
    background: transparent;
    color: var(--text-muted);
    font-size: 18px;
    line-height: 1;
    padding: 2px 6px;
    border-radius: 50%;
  }
  .ub-close:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  /* Incoming-call card. */
  .ring-card {
    position: fixed;
    top: 20px;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: 14px;
    padding: 14px 18px;
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--accent);
    border-radius: var(--radius-lg);
    box-shadow: var(--shadow-pop);
    z-index: 210;
  }
  .ring-pulse {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--ok);
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--ok) 60%, transparent);
    animation: ring-pulse 1.3s ease-out infinite;
    flex-shrink: 0;
  }
  @keyframes ring-pulse {
    0% {
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--ok) 60%, transparent);
    }
    100% {
      box-shadow: 0 0 0 12px transparent;
    }
  }
  .ring-info {
    display: flex;
    flex-direction: column;
    line-height: 1.3;
  }
  .ring-actions {
    display: flex;
    gap: 8px;
  }
  .ring-btn {
    padding: 7px 16px;
    border-radius: var(--radius-md);
    font-weight: 600;
    font-size: 13px;
  }
  .ring-btn.accept {
    background: var(--ok);
    color: #fff;
  }
  .ring-btn.decline {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
</style>
