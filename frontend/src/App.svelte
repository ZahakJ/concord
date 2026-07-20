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
    applyAppearance,
    flash,
    setVideoStream,
    clearVideoStreams,
    incomingCall,
    isDMChannel,
    jumpToChannel,
    checkForUpdate,
    dismissUpdate,
    setChannelTopic,
    nudge,
    closeTopOverlay,
    isCallLocked,
    clearCallState,
  } from "./lib/state.svelte.js";

  import { bioEnrolled, unlockWithBiometric } from "./lib/biometric.js";
  import { initDeepLinks } from "./lib/deeplink.js";
  import Icon from "./Icon.svelte";
  import Login from "./Login.svelte";
  import MobileShell from "./MobileShell.svelte";
  import GuildRail from "./GuildRail.svelte";
  import ChannelList from "./ChannelList.svelte";
  import ChatHeader from "./ChatHeader.svelte";
  import VoicePanel from "./VoicePanel.svelte";
  import MessageList from "./MessageList.svelte";
  import Composer from "./Composer.svelte";
  import MemberPanel from "./MemberPanel.svelte";
  import Welcome from "./Welcome.svelte";
  import ForumView from "./ForumView.svelte";
  import QuickSwitcher from "./QuickSwitcher.svelte";
  import ProfilePopover from "./ProfilePopover.svelte";
  import ContextMenu from "./ContextMenu.svelte";
  import FloatingCall from "./FloatingCall.svelte";
  import Toasts from "./Toasts.svelte";
  import ModalCreate from "./modals/ModalCreate.svelte";
  import ModalCreateChannel from "./modals/ModalCreateChannel.svelte";
  import ModalEmoji from "./modals/ModalEmoji.svelte";
  import ModalForward from "./modals/ModalForward.svelte";
  import ModalBans from "./modals/ModalBans.svelte";
  import ModalRoles from "./modals/ModalRoles.svelte";
  import ModalGuildSettings from "./modals/ModalGuildSettings.svelte";
  import ModalChannelTopic from "./modals/ModalChannelTopic.svelte";
  import ModalChannelLinks from "./modals/ModalChannelLinks.svelte";
  import ModalPublish from "./modals/ModalPublish.svelte";
  import ModalNewPost from "./modals/ModalNewPost.svelte";
  import ModalMeeting from "./modals/ModalMeeting.svelte";
  import ModalShortcuts from "./modals/ModalShortcuts.svelte";
  import ModalNewDM from "./modals/ModalNewDM.svelte";
  import ModalRenameGroup from "./modals/ModalRenameGroup.svelte";
  import ModalJoin from "./modals/ModalJoin.svelte";
  import ModalInvite from "./modals/ModalInvite.svelte";
  import ModalAddMembers from "./modals/ModalAddMembers.svelte";
  import ModalGuildInvite from "./modals/ModalGuildInvite.svelte";
  import ModalProfile from "./modals/ModalProfile.svelte";
  import ModalSettings from "./modals/ModalSettings.svelte";
  import ModalLinkDevice from "./modals/ModalLinkDevice.svelte";
  import ModalAppearance from "./modals/ModalAppearance.svelte";
  import ModalWhen from "./modals/ModalWhen.svelte";
  import ModalScheduled from "./modals/ModalScheduled.svelte";
  import ModalPoll from "./modals/ModalPoll.svelte";
  import ModalCompose from "./modals/ModalCompose.svelte";
  import ModalDisappear from "./modals/ModalDisappear.svelte";
  import ModalStats from "./modals/ModalStats.svelte";
  import ModalBlocked from "./modals/ModalBlocked.svelte";
  import { startScheduler } from "./lib/scheduled.svelte.js";
  import { startEphemeralSweep } from "./lib/ephemeral.svelte.js";
  import ConfirmDialog from "./modals/ConfirmDialog.svelte";

  let composer = $state(null);

  // DMs (incl. the self-DM "Notes") drop the member panel for a roomier view.
  const isDM = $derived(activeGuild()?.kind === "dm");
  // No open channel → show the welcome screen instead of an empty chat.
  const hasChannel = $derived(!!S.activeChannelId && !!activeGuild());
  // Forum channels swap the chat feed for the post board.
  const activeChannelObj = $derived(
    activeGuild()?.channels.find((c) => c.id === S.activeChannelId) || null,
  );

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

  // Admitted into a locked call → join for real (admitted=true skips the knock).
  $effect(() => {
    const ch = S.admittedJoin;
    if (!ch) return;
    S.admittedJoin = "";
    flash("You were let in", "success");
    joinVoice(ch, true);
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
    // Skip the self-update check on mobile — the app stores own updates there.
    if (!window.Capacitor) checkForUpdate(); // fire-and-forget; works on the login screen too
    initDeepLinks(); // concord:// links (QR scanned with the OS camera)
    try {
      if (await api.session()) await start();
    } catch {
      /* not unlocked yet — show the login screen */
    }
  });

  // Concorde fly-in: a short takeoff moment between unlocking and the app.
  // Only on a real login (not session-restore refreshes), and never under
  // prefers-reduced-motion.
  let flyIn = $state(false);
  function playFlyIn() {
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    flyIn = true;
    setTimeout(() => (flyIn = false), 1500);
  }

  async function start(fromLogin = false) {
    if (fromLogin) playFlyIn();
    await onLogin();
    requestPermission();
    installShortcuts();
    startScheduler();
    startEphemeralSweep();
    wireMobileLifecycle();
    registerPushToken();
    applyStayConnected();
    // App lock: the Go core (and its unlocked session) stays alive in the
    // background, so reopening the app skips the passphrase. With the pref on,
    // gate re-entry behind the device biometric instead of walking straight in.
    if (shouldAppLock()) {
      appLocked = true;
      tryBioGate();
    }
  }

  // ---- app lock (biometric re-entry gate) ----
  let appLocked = $state(false);
  const shouldAppLock = () => S.isMobile && S.prefs.appLock === true && bioEnrolled();
  async function tryBioGate() {
    const pass = await unlockWithBiometric(); // retrieval is biometric-gated
    if (pass) appLocked = false;
  }

  // "Stay connected" (Android): run a foreground service so the P2P node keeps
  // receiving messages while the app is backgrounded. Default on; a settings
  // toggle (S.prefs.stayConnected) can turn it off. No-op on web/desktop/iOS.
  function applyStayConnected() {
    const core = window.Capacitor?.Plugins?.ConcordCore;
    if (!core?.startBackground) return;
    if (S.prefs.stayConnected === false) core.stopBackground?.().catch(() => {});
    else core.startBackground().catch(() => {});
  }

  // Acquire the platform push token (FCM on Android, APNs on iOS) and register
  // it with the rendezvous mailbox, so deposits that land while the app is
  // backgrounded trigger a contentless wake.
  //
  // DISABLED until push credentials are configured: PushNotifications.register()
  // calls FirebaseMessaging, which throws a NATIVE exception on a real device
  // when there's no google-services.json — and that exception crashes the app
  // (it's on a background handler thread, so a JS try/catch can't stop it).
  // Foreground delivery + mailbox-drain-on-open work fine without this. To
  // enable: add google-services.json (Android) / APNs entitlement (iOS), then
  // set window.__CONCORD_PUSH = true at build time.
  async function registerPushToken() {
    if (!window.__CONCORD_PUSH) return; // push not provisioned — do NOT call register()
    const cap = typeof window !== "undefined" ? window.Capacitor : null;
    const Push = cap?.Plugins?.PushNotifications;
    if (!Push) return;
    try {
      const perm = await Push.requestPermissions();
      if (perm.receive !== "granted") return;
      Push.addListener("registration", (t) => {
        const platform = cap.getPlatform?.() === "ios" ? "apns" : "fcm";
        api.registerPush(platform, t.value).catch(() => {});
      });
      Push.addListener("pushNotificationReceived", () => nudge());
      await Push.register();
    } catch {
      /* no push available — foreground delivery still works */
    }
  }

  // On Capacitor, hook the OS: hardware back closes an open drawer/sheet before
  // leaving the app, and resuming from the background triggers a fast reconnect
  // + resync (the libp2p sockets die while suspended). No-op on web/desktop.
  function wireMobileLifecycle() {
    const cap = typeof window !== "undefined" ? window.Capacitor : null;
    const App = cap?.Plugins?.App;
    if (!App) return;
    App.addListener("backButton", ({ canGoBack }) => {
      // Dismiss overlays innermost-first, one per press: a context sheet or a
      // component overlay (QR scanner, Ring/Banner studio) or the profile card,
      // THEN drawers, THEN a modal, and only then leave the app. Without this,
      // back on a scanner/studio jumped straight to exiting.
      if (closeTopOverlay()) return;
      if (S.drawerOpen || S.membersOpen) {
        S.drawerOpen = false;
        S.membersOpen = false;
      } else if (S.modal) {
        S.modal = null;
      } else if (!canGoBack) {
        App.exitApp();
      }
    });
    App.addListener("resume", () => {
      nudge();
      if (appLocked) tryBioGate();
    });
    // Lock the moment the app leaves the foreground, so the next open (and
    // the OS app-switcher, once the WebView repaints) meets the gate.
    App.addListener("pause", () => {
      if (S.ready && shouldAppLock()) appLocked = true;
    });
  }

  // ---- voice lifecycle (owns the mesh; state lives in S) ----

  let joining = false;
  // Missed-call detection: did anyone else ever show up during this call, and
  // did we enter it by accepting someone's ring (vs. initiating)? The caller's
  // client is the source of truth for the "Missed call" line.
  let voiceHadPeer = false;
  let voiceWasAccept = false;
  async function joinVoice(channelId = S.activeChannelId, admitted = false) {
    if (!channelId || joining) return; // re-entrancy guard: no orphan meshes
    if (S.voice) {
      const inThisRoom = S.voice.channelId === channelId;
      await leaveVoice();
      // Re-clicking the room you're already in toggles you out (no need to hunt
      // for the disconnect button). Clicking a different one switches rooms.
      if (inThisRoom) return;
    }
    // Locked call: don't barge in — knock and wait to be admitted (unless we're
    // arriving BECAUSE we were just admitted). Someone already in the call
    // approves, which flips admittedJoin and re-calls us with admitted=true.
    if (!admitted && isCallLocked(channelId)) {
      S.knocking = channelId;
      api.signalCall(channelId, "knock").catch(() => {});
      flash("Call is locked — knocking to be let in…", "info");
      return;
    }
    joining = true;
    voiceHadPeer = false;
    voiceWasAccept = incomingCall()?.channelId === channelId;
    // Kill the incoming-call ring the instant we commit to joining — otherwise
    // it keeps brringing through the mic prompt + mesh setup (seconds) until
    // S.voice is finally set, which is the "we're both on the call but it's
    // still ringing" cringe. incomingCall() skips S.joiningVoice. (Set AFTER
    // voiceWasAccept, which itself calls incomingCall.)
    S.joiningVoice = channelId;

    // IP privacy. Fetch ICE config (STUN + optional TURN relay) up front. We
    // force-relay — hiding our IP from the call's peers — when either:
    //   • this is a MEETING (guests join meetings from public links; a stranger
    //     must never learn the host's IP, and forcing relay on both ends is what
    //     makes that mutual — the guest page already relays), or
    //   • the user turned on "Hide my IP on calls" globally.
    // If no relay is available we fall back to a normal call (can't hide, but
    // still connects) rather than failing.
    // Resolve the guild that OWNS this channel, not the one that happens to be
    // active — admitting to a locked meeting (the knock→admit path) joins without
    // navigating, so keying off S.activeGuildId could miss the "meeting" kind and
    // skip the forced IP-hiding relay. IP privacy must follow the channel.
    const kind =
      S.guilds.find((g) => g.channels?.some((c) => c.id === channelId))?.kind ??
      S.guilds.find((g) => g.id === S.activeGuildId)?.kind;
    let iceServers;
    let forceRelay = false;
    try {
      const cfg = await api.callIceServers();
      iceServers = cfg?.iceServers;
      const wantRelay = kind === "meeting" || S.prefs.hideCallIp === true;
      forceRelay = wantRelay && cfg?.relayAvailable === true;
      if (wantRelay && !cfg?.relayAvailable) {
        flash("No relay available — this call won't hide your IP.", "info");
      }
    } catch {
      // stay on defaults (plain STUN, no relay)
    }

    const mesh = new VoiceMesh({
      selfPeerId: S.identity.peerId,
      channelId,
      iceServers,
      forceRelay,
      relay: api.relaySignal,
      onRoster: (ids) => {
        S.voiceParticipants = ids;
        if (ids.length > 0) voiceHadPeer = true;
      },
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
      flash("Microphone access denied", "error");
      joining = false;
      S.joiningVoice = "";
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
      S.joiningVoice = "";
      return;
    }
    S.voice = { mesh, channelId };
    S.joiningVoice = "";
    // Rejoining a call clears any prior "declined" suppression for this channel.
    if (S.dismissedCalls.includes(channelId))
      S.dismissedCalls = S.dismissedCalls.filter((c) => c !== channelId);
    playVoiceJoin();
    flash("Joined voice", "success");
    joining = false;
  }

  async function leaveVoice() {
    if (!S.voice) return;
    const ch = S.voice.channelId;
    // If we locked the call, unlock it as we leave, and clear knock bookkeeping.
    if (isCallLocked(ch)) api.signalCall(ch, "unlock").catch(() => {});
    clearCallState(ch);
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
    S.deafened = false;
    S.peerVolumes = {};
    S.sharing = false;
    S.cameraOn = false;
    clearVideoStreams();
    playVoiceLeave();
    await api.leaveVoice(ch);
    // A DM ring we initiated where the other side never showed → leave a quiet
    // "Missed call" line in the conversation (both sides render it; it never
    // pings or counts as unread — any non-"" kind is exempt).
    if (!voiceHadPeer && !voiceWasAccept && isDMChannel(ch)) {
      api.sendCallNotice(ch, "call-missed", "Missed call").catch(() => {});
    }
  }

  function toggleMicMute() {
    // Talking again means you can hear again: unmuting lifts deafen too.
    S.muted = !S.muted;
    if (!S.muted && S.deafened) {
      S.deafened = false;
      S.voice?.mesh.setDeafened(false);
    }
    S.voice?.mesh.setMuted(S.muted);
  }

  function toggleDeafen() {
    S.deafened = !S.deafened;
    S.voice?.mesh.setDeafened(S.deafened);
    // Deafen implies mic-muted; the mesh already muted, mirror it for the UI.
    if (S.deafened) S.muted = true;
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
    try {
      await api.createChannel(S.activeGuildId, name.trim(), type || "", category || "");
    } catch (err) {
      flash(err); // e.g. "you don't have permission" — never fail silently
      return;
    }
    await refreshGuilds();
    S.modal = null;
  }

  async function createCategory(name) {
    if (!name?.trim() || !S.activeGuildId) return;
    try {
      await api.createCategory(S.activeGuildId, name.trim());
    } catch (err) {
      flash(err);
      return;
    }
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
    await api.setProfile(
      p.name, p.status, p.emoji, p.color, p.avatar || "", p.banner || "",
      p.presence || "", p.bio || "", p.color2 || "", p.frame || "", p.effect || "",
      p.style ? JSON.stringify(p.style) : "",
    );
    S.identity = await api.identity();
    S.displayName = S.identity.displayName || "";
    applyAppearance(); // new profile color, unless an accent preset overrides it
    await refreshRightPanel();
    S.modal = null;
    flash("Profile updated", "success");
  }

  function copy(text) {
    navigator.clipboard?.writeText(text);
    flash("Copied to clipboard", "success");
  }
</script>

{#if S.update && !ringingChannel}
  <div class="update-banner">
    <span class="ub-text">
      <strong>Update available</strong> — Concord {S.update.latest} is out (you have {S.update.current}).
    </span>
    <!-- Fully in-app: opens Settings → Software update AND kicks the install
         off immediately. No external links — the app updates itself. -->
    <button class="ub-dl" onclick={() => (S.modal = { kind: "settings", startUpdate: true })}>
      Update now
    </button>
    <button class="ub-close" onclick={dismissUpdate} aria-label="Dismiss">×</button>
  </div>
{/if}

{#if S.ready}
  <!-- Live backdrop for animated theme packs; inert/hidden otherwise (CSS gates
       it on [data-anim-bg]). Sits behind the app; surfaces go translucent. -->
  <div class="theme-bg" aria-hidden="true">
    <span class="tb-a"></span>
    <span class="tb-b"></span>
    <span class="tb-c"></span>
  </div>
{/if}

{#if !S.ready}
  <Login onLogin={() => start(true)} />
{:else if S.isMobile}
  <MobileShell
    bind:composer
    onJoinVoice={joinVoice}
    onLeaveVoice={leaveVoice}
    onToggleMute={toggleMicMute}
    onToggleShare={toggleScreenShare}
    onToggleCamera={toggleCamera}
  />
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
            onToggleDeafen={toggleDeafen}
            onToggleShare={toggleScreenShare}
            onToggleCamera={toggleCamera}
          />
        {/if}
        {#if activeChannelObj?.type === "forum"}
          <ForumView forum={activeChannelObj} />
        {:else}
          <MessageList onDropFiles={(files) => files.forEach((f) => composer?.attachFile(f))} />
          <Composer bind:this={composer} />
        {/if}
      {:else}
        <Welcome />
      {/if}
    </main>

    {#if !isDM && hasChannel && S.prefs.memberPanel}
      <MemberPanel />
    {/if}
  </div>{/if}
{#if S.ready}

  {#if S.quickSwitcher}
    <QuickSwitcher />
  {/if}

  <ProfilePopover />
  <ContextMenu />

  <!-- Knocking on a locked call: waiting to be admitted. -->
  {#if S.knocking}
    <div class="knock-wait" role="status">
      <span class="kw-dot"></span>
      <span>Waiting to be let into the call…</span>
      <button class="kw-cancel" onclick={() => (S.knocking = "")}>Cancel</button>
    </div>
  {/if}

  <!-- Ongoing call you've navigated away from: a draggable pinned window. -->
  {#if callElsewhere}
    <FloatingCall
      label={callLabel}
      onLeave={leaveVoice}
      onToggleMute={toggleMicMute}
      onToggleDeafen={toggleDeafen}
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

  <Toasts />

  <!-- Concorde fly-in: the jet takes off straight up (nose-first) trailing a
       vertical vapour trail while the overlay fades to reveal the app. Pure
       theater, 1.5s, once per unlock. pointer-events:none — never blocks input. -->
  {#if flyIn}
    <div class="flyin" aria-hidden="true">
      <div class="flyin-jet">
        <span class="contrail"></span>
        <Icon name="concorde" size={54} />
      </div>
    </div>
  {/if}

  <!-- Self-update curtain: goes up the moment "Restart now" is clicked, so
       the outgoing version's UI (update banner and all) is never seen while
       the process swaps binaries. The page reloads into the new version. -->
  {#if S.restarting}
    <div class="restart-curtain" role="status" aria-live="polite">
      <div class="rc-inner">
        <span class="rc-jet"><Icon name="concorde" size={44} /></span>
        <h2>Installing update</h2>
        <p class="muted">Concord will be right back…</p>
      </div>
    </div>
  {/if}

  <!-- App lock: fully opaque (privacy in the app switcher), above everything. -->
  {#if appLocked}
    <div class="lock-gate" role="dialog" aria-label="Locked">
      <div class="lock-inner">
        <span class="lock-badge"><Icon name="lock" size={26} /></span>
        <h2>Concord is locked</h2>
        <p class="muted">Unlock with your fingerprint or face.</p>
        <button class="lock-btn" onclick={tryBioGate}>Unlock</button>
        <button
          class="lock-alt"
          onclick={async () => {
            await api.logout();
            location.reload();
          }}
        >
          Use passphrase instead
        </button>
      </div>
    </div>
  {/if}

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
  {:else if S.modal?.kind === "when"}
    <ModalWhen onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "scheduled"}
    <ModalScheduled onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "poll"}
    <ModalPoll onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "compose"}
    <ModalCompose
      initial={S.modal.initial || ""}
      editId={S.modal.editId || ""}
      onSent={S.modal.onSent}
      onClose={() => (S.modal = null)}
    />
  {:else if S.modal?.kind === "disappear"}
    <ModalDisappear onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "stats"}
    <ModalStats onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "blocked"}
    <ModalBlocked onClose={() => (S.modal = null)} />
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
  {:else if S.modal?.kind === "channelLinks"}
    <ModalChannelLinks channel={S.modal.channel} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "publish"}
    <ModalPublish message={S.modal.message} channel={S.modal.channel} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "meeting"}
    <ModalMeeting code={S.modal.code} guestLink={S.modal.guestLink || ""} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "newPost"}
    <ModalNewPost forum={S.modal.forum} onClose={() => (S.modal = null)} />
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
    <ModalSettings onClose={() => (S.modal = null)} onSaved={() => flash("Rendezvous saved", "success")} />
  {:else if S.modal?.kind === "linkDevice"}
    <ModalLinkDevice onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "appearance"}
    <ModalAppearance onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "join"}
    <ModalJoin error={S.modal.error} onSubmit={joinGuild} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "guildInvite"}
    <ModalGuildInvite invite={S.modal.invite} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "addMembers"}
    <ModalAddMembers onClose={() => (S.modal = null)} />
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
  /* Concorde fly-in: overlay fades from the login backdrop to nothing while
     the jet sweeps lower-left → upper-right. Under the app lock (privacy
     wins), above everything else. */
  .flyin {
    position: fixed;
    inset: 0;
    z-index: 490;
    pointer-events: none;
    background:
      radial-gradient(circle at 50% 18%, color-mix(in srgb, var(--accent) 7%, transparent), transparent 55%),
      radial-gradient(circle at 50% 30%, color-mix(in srgb, var(--bg-3) 70%, var(--bg)), var(--bg));
    animation: flyin-fade 1.5s ease forwards;
    overflow: hidden;
  }
  /* The mark is a head-on front view (nose up), so it takes off STRAIGHT UP from
     the centre — nose already pointing where it goes — with a vertical vapour
     trail directly beneath it (no awkward diagonal). */
  .flyin-jet {
    position: absolute;
    left: 50%;
    top: 82%;
    color: var(--text);
    filter: drop-shadow(0 0 14px color-mix(in srgb, var(--accent) 55%, transparent));
    animation: flyin-jet 1.35s cubic-bezier(0.4, 0, 0.2, 1) forwards;
  }
  /* Trail hangs straight down from the jet's tail, brightest at the nozzle and
     fading with distance. It's a child, so it rides the jet's climb. */
  .contrail {
    position: absolute;
    top: 90%;
    left: 50%;
    transform: translateX(-50%);
    width: 3px;
    height: 62vh;
    border-radius: 2px;
    background: linear-gradient(180deg, color-mix(in srgb, var(--accent) 75%, white), transparent);
    opacity: 0.8;
  }
  @keyframes flyin-jet {
    0% {
      transform: translate(-50%, 0) scale(0.85);
      opacity: 0;
    }
    18% {
      opacity: 1;
    }
    100% {
      transform: translate(-50%, -118vh) scale(1.2);
      opacity: 1;
    }
  }
  @keyframes flyin-fade {
    0%,
    45% {
      opacity: 1;
    }
    100% {
      opacity: 0;
    }
  }

  /* App lock overlay: opaque so chat content is never visible behind it. */
  /* Self-update curtain: opaque, above everything but the app lock. */
  .restart-curtain {
    position: fixed;
    inset: 0;
    z-index: 480;
    display: grid;
    place-items: center;
    background: var(--bg-0);
    animation: rc-in 0.2s ease;
  }
  @keyframes rc-in {
    from {
      opacity: 0;
    }
  }
  .rc-inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    text-align: center;
  }
  .rc-inner h2 {
    margin: 4px 0 0;
    font-size: 18px;
  }
  .rc-inner p {
    margin: 0;
    font-size: 13px;
  }
  .rc-jet {
    color: var(--accent);
    animation: rc-bob 1.6s ease-in-out infinite;
  }
  @keyframes rc-bob {
    0%,
    100% {
      transform: translateY(2px) rotate(-4deg);
    }
    50% {
      transform: translateY(-4px) rotate(3deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .restart-curtain,
    .rc-jet {
      animation: none;
    }
  }

  .lock-gate {
    position: fixed;
    inset: 0;
    z-index: 500;
    display: grid;
    place-items: center;
    background: var(--bg-2);
  }
  .lock-inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    text-align: center;
    padding: 24px;
  }
  .lock-badge {
    display: grid;
    place-items: center;
    width: 64px;
    height: 64px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent);
    margin-bottom: 4px;
  }
  .lock-gate h2 {
    margin: 0;
    font-size: 20px;
  }
  .lock-gate p {
    margin: 0 0 8px;
    font-size: 14px;
  }
  .lock-btn {
    min-width: 200px;
    min-height: 48px;
    font-size: 16px;
    font-weight: 600;
    border-radius: var(--radius-md);
  }
  .lock-alt {
    background: transparent;
    color: var(--text-muted);
    font-size: 13px;
    padding: 10px;
  }
  .lock-alt:hover {
    background: transparent;
    color: var(--text);
  }
  .app {
    display: grid;
    grid-template-columns: 64px 220px 1fr 260px;
    height: 100%;
    /* Sit above the animated theme backdrop (.theme-bg, z-index 0). */
    position: relative;
    z-index: 1;
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
  .knock-wait {
    position: fixed;
    top: 16px;
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
    border-radius: 22px;
    box-shadow: var(--shadow-pop);
    z-index: 215;
    font-size: 13px;
  }
  .kw-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--accent);
    animation: kw-pulse 1.2s ease-in-out infinite;
  }
  @keyframes kw-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.3;
    }
  }
  .kw-cancel {
    padding: 4px 12px;
    background: var(--bg-3);
    color: var(--text);
    border-radius: 12px;
    font-size: 12px;
  }
  .kw-cancel:hover {
    background: var(--bg-input);
  }
  @media (prefers-reduced-motion: reduce) {
    .kw-dot {
      animation: none;
    }
  }
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
  @media (prefers-reduced-motion: reduce) {
    .ring-pulse {
      animation: none;
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
    transition: background 0.12s ease, transform 0.08s ease, border-color 0.12s ease, color 0.12s ease;
  }
  /* The most time-critical buttons in the app were visually inert — no hover or
     press feedback. Give them clear states so they feel like live controls. */
  .ring-btn:active {
    transform: scale(0.96);
  }
  .ring-btn.accept {
    background: var(--ok);
    color: #fff;
  }
  .ring-btn.accept:hover {
    background: color-mix(in srgb, var(--ok) 86%, #fff);
  }
  .ring-btn.decline {
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-muted);
  }
  .ring-btn.decline:hover {
    background: var(--danger-soft);
    border-color: var(--danger);
    color: var(--danger);
  }
  @media (prefers-reduced-motion: reduce) {
    .ring-btn:active {
      transform: none;
    }
  }
</style>
