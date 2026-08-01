<script>
  // App.svelte is the shell: login gate, the four-column layout, voice
  // lifecycle, global shortcuts, and modal routing. All shared state and the
  // backend event wiring live in lib/state.svelte.js.
  import { onMount } from "svelte";
  import { api, leaveVoiceOnUnload } from "./lib/api.js";
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
    selectChannel,
    isCallLocked,
    nameFor,
    forgetLock,
    clearCallState,
    publishVoiceState,
  } from "./lib/state.svelte.js";

  import { bioEnrolled, unlockWithBiometric } from "./lib/biometric.js";
  import { initDeepLinks, consumePendingChannel } from "./lib/deeplink.js";
  import { closeSearch } from "./lib/search.js";
  import { haptic, hapticNotify } from "./lib/touch.js";
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
  import SearchPanel from "./SearchPanel.svelte";
  import QuickSwitcher from "./QuickSwitcher.svelte";
  import ProfilePopover from "./ProfilePopover.svelte";
  import ContextMenu from "./ContextMenu.svelte";
  import FloatingCall from "./FloatingCall.svelte";
  import Toasts from "./Toasts.svelte";
  import ModalCreate from "./modals/ModalCreate.svelte";
  import ModalCreateChannel from "./modals/ModalCreateChannel.svelte";
  import ModalEmoji from "./modals/ModalEmoji.svelte";
  import ModalMeme from "./modals/ModalMeme.svelte";
  import ModalGifs from "./modals/ModalGifs.svelte";
  import ModalForward from "./modals/ModalForward.svelte";
  import ModalBans from "./modals/ModalBans.svelte";
  import ModalRoles from "./modals/ModalRoles.svelte";
  import ModalGuildSettings from "./modals/ModalGuildSettings.svelte";
  import ModalChannelTopic from "./modals/ModalChannelTopic.svelte";
  import ModalChannelLinks from "./modals/ModalChannelLinks.svelte";
  import ModalPublish from "./modals/ModalPublish.svelte";
  import ModalNewPost from "./modals/ModalNewPost.svelte";
  import ModalForumSettings from "./modals/ModalForumSettings.svelte";
  import ModalMeeting from "./modals/ModalMeeting.svelte";
  import ModalShortcuts from "./modals/ModalShortcuts.svelte";
  import ModalNewDM from "./modals/ModalNewDM.svelte";
  import ModalRenameGroup from "./modals/ModalRenameGroup.svelte";
  import ModalRenameChannel from "./modals/ModalRenameChannel.svelte";
  import ModalJoin from "./modals/ModalJoin.svelte";
  import ModalInvite from "./modals/ModalInvite.svelte";
  import ModalAddMembers from "./modals/ModalAddMembers.svelte";
  import ModalGuildInvite from "./modals/ModalGuildInvite.svelte";
  import ModalProfile from "./modals/ModalProfile.svelte";
  import ModalSettings from "./modals/ModalSettings.svelte";
  import ModalLinkDevice from "./modals/ModalLinkDevice.svelte";
  import ModalAppearance from "./modals/ModalAppearance.svelte";
  import ModalDevices from "./modals/ModalDevices.svelte";
  import ModalNotifications from "./modals/ModalNotifications.svelte";
  import ModalPrivacy from "./modals/ModalPrivacy.svelte";
  import ModalBookings from "./modals/ModalBookings.svelte";
  import ModalConnection from "./modals/ModalConnection.svelte";
  import ModalWhen from "./modals/ModalWhen.svelte";
  import ModalScheduled from "./modals/ModalScheduled.svelte";
  import ModalPoll from "./modals/ModalPoll.svelte";
  import ModalCompose from "./modals/ModalCompose.svelte";
  import ModalDisappear from "./modals/ModalDisappear.svelte";
  import ModalStats from "./modals/ModalStats.svelte";
  import ModalBlocked from "./modals/ModalBlocked.svelte";
  import ModalRequests from "./modals/ModalRequests.svelte";
  import ModalEvents from "./modals/ModalEvents.svelte";
  import ModalMyCalendar from "./modals/ModalMyCalendar.svelte";
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

  // A moderator moved or disconnected us. The authority check already happened
  // in state.svelte.js against our own copy of the guild's governance — by the
  // time it lands here it's a decision to carry out, and to say out loud: being
  // moved with no explanation is the kind of thing that feels like a bug.
  $effect(() => {
    const m = S.moderatedVoice;
    if (!m) return;
    S.moderatedVoice = null;
    if (m.action === "disconnect") {
      flash(`${m.by} disconnected you from the call`, "info");
      leaveVoice();
    } else {
      flash(`${m.by} moved you to ${m.name}`, "info");
      // admitted=true: being moved BY a moderator shouldn't make us knock at
      // the destination, even if it happens to be locked.
      joinVoice(m.channelId, true);
    }
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
    // The boot mark from index.html. Svelte's mount() appends to #app rather
    // than clearing it, so this has to go by hand or it sits under the app.
    document.getElementById("boot")?.remove();
    // Skip the self-update check on mobile — the app stores own updates there.
    if (!window.Capacitor) checkForUpdate(); // fire-and-forget; works on the login screen too
    initDeepLinks(); // concord:// links (QR scanned with the OS camera)
    // Hardware back and the resume/pause hooks must exist from the FIRST frame:
    // the login and device-linking screens are where a stray back press used to
    // drop a half-set-up user on the launcher.
    wireMobileLifecycle();
    // Hand the launch splash over now that there is something to look at. Held
    // by MainActivity across the WebView load AND the Go core boot, which is a
    // blank window otherwise.
    window.Capacitor?.Plugins?.ConcordCore?.appReady?.().catch(() => {});
    watchSystemBars();
    // Closing or reloading the tab while in a call: tell the node to leave, so
    // it stops announcing us to a room we're no longer in. "pagehide" is the
    // one that also fires when a mobile browser backgrounds the page.
    addEventListener("pagehide", () => leaveVoiceOnUnload(S.voice?.channelId || ""));
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
    registerPushToken();
    applyStayConnected();
    // App lock: the Go core (and its unlocked session) stays alive in the
    // background, so reopening the app skips the passphrase. With the pref on,
    // gate re-entry behind the device biometric instead of walking straight in.
    if (shouldAppLock()) {
      appLocked = true;
      tryBioGate();
    }
    // A notification tapped while the app was cold parked its channel here; the
    // guild list only exists now, so this is the first moment it can be opened.
    const pending = consumePendingChannel();
    if (pending) jumpToChannel(pending).catch(() => {});
  }

  // ---- system bar / theme-color sync ----
  // Concord picks light vs dark IN the app (Appearance → Theme, plus 18 packs),
  // while Android decides status-bar icon colour from the SYSTEM setting. So a
  // phone in light mode running Concord's dark theme drew dark icons on a
  // near-black bar — invisible. Watch the attribute the appearance code stamps
  // on <html> and tell the OS (and the PWA's theme-color) what we actually are.
  function syncBars() {
    const bg = getComputedStyle(document.documentElement).getPropertyValue("--bg-1").trim();
    const meta = document.querySelector('meta[name="theme-color"]');
    if (bg && meta) meta.setAttribute("content", bg);
    // Relative luminance is overkill here: the question is only "are the bar
    // icons going to be readable if they're dark".
    const light = document.documentElement.getAttribute("data-theme") === "light";
    window.Capacitor?.Plugins?.ConcordCore?.setSystemBarStyle?.({ light }).catch(() => {});
  }
  function watchSystemBars() {
    syncBars();
    const mo = new MutationObserver(syncBars);
    mo.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme", "data-theme-pack"],
    });
    return () => mo.disconnect();
  }

  // ---- app lock (biometric re-entry gate) ----
  let appLocked = $state(false);
  const shouldAppLock = () => S.isMobile && S.prefs.appLock === true && bioEnrolled();
  async function tryBioGate() {
    const pass = await unlockWithBiometric(); // retrieval is biometric-gated
    if (pass) appLocked = false;
  }
  // FLAG_SECURE follows the App Lock pref. The gate itself is a WebView repaint,
  // which lands AFTER the OS has already snapshotted the app for the recents
  // carousel — so without this the last open conversation, decrypted, sat in the
  // task switcher of an app whose whole premise is that it doesn't.
  $effect(() => {
    const on = S.prefs.appLock === true;
    window.Capacitor?.Plugins?.ConcordCore?.setSecure?.({ secure: on }).catch(() => {});
  });

  // ---- keep the screen awake during a call ----
  // Without this the display times out (typically 30s) with no touch input,
  // which is precisely what a call is; once the WebView is hidden the voice
  // monitor is throttled and the AudioContext can be suspended, so the call
  // degrades exactly when the user stopped touching the phone.
  $effect(() => {
    if (!S.voice || !navigator.wakeLock) return;
    let lock = null;
    let dead = false;
    const acquire = async () => {
      if (dead || document.hidden) return;
      try {
        lock = await navigator.wakeLock.request("screen");
      } catch {
        /* denied, or no permission policy for it — the call still works */
      }
    };
    // The OS drops a wake lock whenever the page hides; re-take it on return.
    const onVis = () => !document.hidden && acquire();
    document.addEventListener("visibilitychange", onVis);
    acquire();
    return () => {
      dead = true;
      document.removeEventListener("visibilitychange", onVis);
      lock?.release?.().catch(() => {});
    };
  });

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

  // ---- hardware back ----
  // The Android back button is the app's primary navigation control, and it used
  // to be: dismiss an overlay, close the drawers, close a modal, EXIT. The SPA
  // pushes no history entries, so `canGoBack` is always false — which meant back
  // from inside any conversation, or from inside a forum post, quit Concord.
  // What follows is a real stack, unwound one press at a time:
  //
  //   overlay / sheet / popover   (closeTopOverlay)
  //   modal
  //   transient panel             (quick switcher, pins, picker, search results)
  //   knock / call invite
  //   open drawer
  //   forum post  →  its board
  //   open channel →  reveal the channel list
  //   root        →  press again to exit
  //
  // Escape on desktop walks a near-identical ladder in lib/shortcuts.js.

  // Back revealed the drawer, so the NEXT back is at the root and should leave.
  // Without this the two rungs ping-pong: back opens the drawer, back closes it,
  // back opens it again, and there is no way out of the app but the home button.
  let backRevealedDrawer = false;
  $effect(() => {
    if (!S.drawerOpen) backRevealedDrawer = false;
  });

  let exitArmed = 0;
  function confirmExit(App) {
    // Double-tap to leave. A single press dropping the user on the launcher
    // mid-conversation is the thing that made back feel dangerous.
    if (Date.now() < exitArmed) {
      App.exitApp();
      return;
    }
    exitArmed = Date.now() + 2000;
    flash("Press back again to exit");
  }

  function handleBack(App, canGoBack) {
    if (closeTopOverlay()) return;
    if (S.modal) {
      S.modal = null;
      return;
    }
    // Panels the old handler could not see — opening Pinned messages and
    // pressing back exited the app with the panel still on screen.
    if (S.quickSwitcher) {
      S.quickSwitcher = false;
      return;
    }
    if (S.pickerTarget) {
      S.pickerTarget = null;
      return;
    }
    if (S.showPins) {
      S.showPins = false;
      return;
    }
    if (S.searchResults !== null || S.searchLoading) {
      closeSearch();
      return;
    }
    if (S.replyingTo) {
      S.replyingTo = null;
      return;
    }
    if (S.callInvite) {
      S.callInvite = null;
      return;
    }
    if (S.knocking) {
      S.knocking = "";
      return;
    }
    if (S.membersOpen) {
      S.membersOpen = false;
      return;
    }
    if (S.drawerOpen) {
      if (backRevealedDrawer) {
        backRevealedDrawer = false;
        confirmExit(App);
      } else {
        S.drawerOpen = false;
      }
      return;
    }
    // A forum post is a channel nested under its board; going "back" from one
    // means the board, exactly as ChatHeader's breadcrumb does on desktop.
    const parent = activeChannelObj?.parent;
    if (parent) {
      selectChannel(parent);
      return;
    }
    // Back out of a conversation into the list it came from — the single most
    // used back action in every messenger, and the one this app had no step for.
    if (S.isMobile && S.activeChannelId) {
      S.drawerOpen = true;
      backRevealedDrawer = true;
      return;
    }
    if (canGoBack) return; // let the WebView handle a real history entry
    confirmExit(App);
  }

  // On Capacitor, hook the OS: hardware back walks the stack above, and resuming
  // from the background triggers a fast reconnect + resync (the libp2p sockets
  // die while suspended). No-op on web/desktop.
  //
  // Wired from onMount, NOT from start(): before this ran only after login, so
  // during onboarding back fell through to Capacitor's default and quit the app
  // — including with the full-screen QR scanner open, which registers an overlay
  // closer that nothing was listening to.
  function wireMobileLifecycle() {
    const cap = typeof window !== "undefined" ? window.Capacitor : null;
    const App = cap?.Plugins?.App;
    if (!App) return;
    App.addListener("backButton", ({ canGoBack }) => handleBack(App, canGoBack));
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
    // A lock on an EMPTY call can only be stale — the one person who could
    // admit us would have to be inside. Rather than knock at a door with nobody
    // behind it, drop the lock and walk in. (forgetLock also covers the roster
    // going empty; this is the last-line check at the moment it matters.)
    if (!Object.keys(S.voiceRosters[channelId] || {}).length) forgetLock(channelId);
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
      // This device's chosen hardware + audio knobs (Voice & Video settings).
      devices: {
        mic: S.prefs.micId,
        speaker: S.prefs.speakerId,
        camera: S.prefs.cameraId,
        shareAudio: S.prefs.shareAudioId,
      },
      audio: {
        output: S.prefs.outputVolume,
        gain: S.prefs.micGain,
        gate: S.prefs.micGate,
        nr: S.prefs.micNr,
        pushToTalk: S.prefs.pushToTalk,
        bitrate: S.prefs.voiceBitrate,
        echoCancel: S.prefs.echoCancel,
        noiseSuppress: S.prefs.noiseSuppress,
        autoGain: S.prefs.autoGain,
      },
      relay: api.relaySignal,
      onRoster: (ids) => {
        S.voiceParticipants = ids;
        if (ids.length > 0) voiceHadPeer = true;
      },
      onSpeaking: (keys) => (S.voiceSpeaking = keys),
      onVideo: (key, stream, meta) => setVideoStream(key, stream, meta),
      onWatcher: (peerId) => {
        const fpr = S.voicePeerFpr[peerId];
        flash(`${fpr ? nameFor(fpr) : "Someone"} is watching your screen`, "info");
      },
      onVideoState: (kind, on, info) => {
        // If a source stopped via the browser's own chrome (not our button),
        // update the flag and drop our local preview tile too.
        if (kind === "screen") {
          S.sharing = on;
          // Silence on the other end is the kind of thing you only find out
          // about ten minutes in, so say which way it went.
          if (on && info) {
            flash(
              info.audio
                ? "Sharing your screen, with sound"
                : "Sharing your screen — no sound: your system didn't offer it. Settings → Voice & Video can capture it from an input instead.",
              info.audio ? "success" : "info",
            );
          }
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
      // A phone is often already at the ear by now; the error toast is behind it.
      hapticNotify("ERROR");
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
      hapticNotify("ERROR");
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
    publishVoiceState();
    haptic("medium"); // the phone is usually at the ear by now — say it connected
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
    S.voiceStates = {};
    S.voiceSpeaking = [];
    S.voicePeerFpr = {};
    S.muted = false;
    S.deafened = false;
    S.talking = false;
    S.peerVolumes = {};
    S.sharing = false;
    S.cameraOn = false;
    clearVideoStreams();
    playVoiceLeave();
    haptic("medium");
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
    publishVoiceState();
  }

  // Deafening also mutes you — you can't sensibly talk to a room you can't
  // hear, which is what every other client does too. Undeafening then puts your
  // mic back the way it was: if you were talking before you stepped away, you
  // are talking again, rather than silently wondering why nobody replies.
  let mutedBeforeDeafen = false;
  function toggleDeafen() {
    if (!S.deafened) mutedBeforeDeafen = S.muted;
    S.deafened = !S.deafened;
    S.voice?.mesh.setDeafened(S.deafened);
    if (S.deafened) {
      S.muted = true; // the mesh already muted; mirror it for the UI
    } else {
      S.muted = mutedBeforeDeafen;
      S.voice?.mesh.setMuted(S.muted);
    }
    publishVoiceState();
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
    onToggleDeafen={toggleDeafen}
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
        <!-- Search results overlay the pane for EVERY channel type. It used to
             be mounted inside MessageList, which a forum channel never renders —
             so searching from a forum board filled S.searchResults and displayed
             them nowhere. -->
        <SearchPanel />
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

  <!-- Someone in a call asked you to come. Reuses the ring card's shape but is
       a quieter thing: an invitation, not a phone ringing. -->
  {#if S.callInvite && !S.voice}
    <div class="ring-card invite">
      <span class="ring-pulse"></span>
      <div class="ring-info">
        <strong>{S.callInvite.from}</strong>
        <span class="muted">wants you in {S.callInvite.where}</span>
      </div>
      <div class="ring-actions">
        <button class="ring-btn decline" onclick={() => (S.callInvite = null)}>Not now</button>
        <button
          class="ring-btn accept"
          onclick={() => {
            const ch = S.callInvite.channelId;
            S.callInvite = null;
            acceptCall(ch);
          }}
        >
          Join
        </button>
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
        <!-- Honest label: this path signs the device out and reloads. It used to
             read "Use passphrase instead", which sounds like a second door into
             the same room. -->
        <button
          class="lock-alt"
          onclick={async () => {
            await api.logout();
            location.reload();
          }}
        >
          Sign out and use my passphrase
        </button>
      </div>
      <!-- The gate is opaque and above the call dock, so backgrounding the app
           mid-call left no way to mute or hang up until biometrics succeeded.
           These two controls reveal nothing the gate is protecting. -->
      {#if S.voice}
        <div class="lock-call">
          <span class="lc-label">In a call{callLabel ? ` · ${callLabel}` : ""}</span>
          <button class="lc-btn" onclick={toggleMicMute}>{S.muted ? "Unmute" : "Mute"}</button>
          <button class="lc-btn danger" onclick={leaveVoice}>Leave</button>
        </div>
      {/if}
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
  {:else if S.modal?.kind === "gifs"}
    <ModalGifs onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "meme"}
    <!-- `edit` reopens a meme already in the channel; `src` starts a new one
         from a picture. They are mutually exclusive — see ModalMeme. -->
    <ModalMeme src={S.modal.src || ""} edit={S.modal.edit || null} onClose={() => (S.modal = null)} />
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
  {:else if S.modal?.kind === "requests"}
    <ModalRequests onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "events"}
    <ModalEvents onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "myCalendar"}
    <ModalMyCalendar onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "newDM"}
    <ModalNewDM onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "renameGroup"}
    <ModalRenameGroup
      guildId={S.modal.guildId}
      current={S.modal.current}
      onClose={() => (S.modal = null)}
    />
  {:else if S.modal?.kind === "renameChannel"}
    <ModalRenameChannel
      guildId={S.modal.guildId}
      channelId={S.modal.channelId}
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
    <ModalMeeting
      code={S.modal.code}
      guestLink={S.modal.guestLink || ""}
      guildId={S.modal.guildId || ""}
      expires={S.modal.expires || 0}
      onClose={() => (S.modal = null)}
    />
  {:else if S.modal?.kind === "newPost"}
    <ModalNewPost forum={S.modal.forum} onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "forumSettings"}
    <ModalForumSettings forum={S.modal.forum} onClose={() => (S.modal = null)} />
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
    <ModalSettings onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "notifications"}
    <ModalNotifications onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "privacy"}
    <ModalPrivacy onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "bookings"}
    <ModalBookings onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "connection"}
    <ModalConnection
      onClose={() => (S.modal = null)}
      onSaved={() => flash("Rendezvous saved", "success")}
    />
  {:else if S.modal?.kind === "linkDevice"}
    <ModalLinkDevice onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "appearance"}
    <ModalAppearance onClose={() => (S.modal = null)} />
  {:else if S.modal?.kind === "devices"}
    <ModalDevices onClose={() => (S.modal = null)} />
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
    color: var(--accent-hover);
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
    color: var(--accent-hover);
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
  .lock-call {
    position: absolute;
    left: var(--sp-3);
    right: var(--sp-3);
    bottom: calc(var(--sp-3) + max(var(--safe-bottom), var(--sa-bottom, 0px)));
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-2);
    border-radius: var(--radius-lg);
    background: var(--bg-1);
    border: 1px solid var(--border);
  }
  .lc-label {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-compact);
    color: var(--text-muted);
  }
  .lc-btn {
    flex-shrink: 0;
    min-height: var(--tap-min);
    padding: 0 var(--sp-3);
    border-radius: var(--radius-md);
    background: var(--bg-3);
    color: var(--text);
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .lc-btn.danger {
    background: var(--danger);
    color: #fff;
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
  /* Narrow desktop (a window parked beside something else). Not a phone tier —
     below 768px S.isMobile is true and MobileShell renders instead of .app —
     just a density step for the columns.
     The member panel used to be display:none'd here while ChatHeader still
     offered its toggle, so the control silently did nothing for a whole band of
     window widths. It keeps its column, narrower; the toggle decides. */
  @media (max-width: 900px) {
    .app {
      grid-template-columns: 64px 190px 1fr 200px;
    }
    /* Higher specificity, or .app.no-panel keeps the wide 220px column. */
    .app.no-panel {
      grid-template-columns: 64px 190px 1fr;
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
    top: calc(12px + max(var(--safe-top), var(--sa-top, 0px)));
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
    color: var(--accent-fg);
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
    top: calc(16px + max(var(--safe-top), var(--sa-top, 0px)));
    max-width: calc(100vw - 24px);
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
    /* Or the buttons win the space fight and the text column collapses to
       min-content — one word per line. */
    flex: 1;
    min-width: 0;
  }
  .ring-actions {
    display: flex;
    gap: 8px;
    flex-shrink: 0;
  }
  .ring-btn {
    padding: 7px 16px;
    border-radius: var(--radius-md);
    font-weight: 600;
    font-size: 13px;
    white-space: nowrap;
    transition: background 0.12s ease, transform 0.08s ease, border-color 0.12s ease, color 0.12s ease;
  }
  /* The most time-critical buttons in the app were visually inert — no hover or
     press feedback. Give them clear states so they feel like live controls. */
  .ring-btn:active {
    transform: scale(0.96);
  }
  .ring-btn.accept {
    background: var(--ok);
    color: var(--ok-fg);
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
    color: var(--danger-text);
  }
  @media (prefers-reduced-motion: reduce) {
    .ring-btn:active {
      transform: none;
    }
  }
  /* Touch: a full-width banner rather than a centred card that has to squeeze
     name, status line and both buttons into whatever the text doesn't claim. */
  @media (pointer: coarse), (max-width: 768px) {
    /* The update banner's × sat ~6px from "Update now", both about 22px tall —
       so dismissing it regularly started a 200MB download instead. Give them
       real targets and put a gap between them, and let the sentence wrap rather
       than ellipsise to six words. */
    .update-banner {
      left: var(--sp-3);
      right: var(--sp-3);
      transform: none;
      max-width: none;
      flex-wrap: wrap;
      gap: var(--sp-2) var(--sp-3);
      padding: var(--sp-2) var(--sp-3);
      font-size: var(--fs-ui);
    }
    .ub-text {
      flex: 1 0 100%;
      white-space: normal;
    }
    .ub-dl {
      flex: 1;
      min-height: var(--tap-min);
      display: flex;
      align-items: center;
      justify-content: center;
      font-size: var(--fs-ui);
    }
    .ub-close {
      display: flex;
      align-items: center;
      justify-content: center;
      min-width: var(--tap-min);
      min-height: var(--tap-min);
      margin-left: var(--sp-2);
    }
    .knock-wait {
      left: var(--sp-3);
      right: var(--sp-3);
      transform: none;
      max-width: none;
      font-size: var(--fs-ui);
    }
    .kw-cancel {
      min-height: var(--tap-min);
      padding: 0 var(--sp-4);
      font-size: var(--fs-ui);
    }
    .ring-card {
      top: calc(10px + var(--safe-top));
      left: 12px;
      right: 12px;
      transform: none;
      flex-wrap: wrap;
      gap: 10px;
      padding: 12px 14px;
    }
    /* Answering is what the card is for: the buttons get their own row, full
       width, rather than squeezing the caller's name into one word per line. */
    .ring-actions {
      flex: 1 0 100%;
    }
    /* The most time-critical taps in the app — they were 34px tall, and "Not
       now" wrapped inside its own button. */
    .ring-btn {
      flex: 1;
      min-height: 44px;
    }
  }
</style>
