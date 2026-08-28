<script>
  // App.svelte is the shell: login gate, the four-column layout, voice
  // lifecycle, global shortcuts, and modal routing. All shared state and the
  // backend event wiring live in lib/state.svelte.js.
  import { onMount } from "svelte";
  import { precacheCosmetics } from "./lib/cosmetics.svelte.js";
  import { api, leaveVoiceOnUnload } from "./lib/api.js";
  import { createVisibilityReporter } from "./lib/visibility.js";
  import { VoiceMesh } from "./lib/voice.js";
  import { requestPermission, asksLazily } from "./lib/notify.js";
  import { installShortcuts } from "./lib/shortcuts.js";
  import { playVoiceJoin, playVoiceLeave, playRing } from "./lib/sounds.js";
  import { startCallClock, stopCallClock } from "./lib/calltimer.svelte.js";
  import {
    S,
    activeGuild,
    activeChannel,
    guildUnread,
    accentForeground,
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
    selectChannel,
    flashChannel,
    isCallLocked,
    nameFor,
    forgetLock,
    clearCallState,
    publishVoiceState,
    toggleMicMute,
    toggleDeafen,
    setPref,
    dismissNotifyAsk,
  } from "./lib/state.svelte.js";

  import { guildAccent } from "./lib/guildaccent.js";
  import { bioEnrolled, unlockWithBiometric } from "./lib/biometric.js";
  import { initDeepLinks, consumePendingChannel } from "./lib/deeplink.js";
  import { closeSearch } from "./lib/search.js";
  import { popLayer, navDepth, syncLayer } from "./lib/navstack.svelte.js";
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
  import FxOverlay from "./FxOverlay.svelte";
  import { validFx } from "./lib/themefx.js";
  import JoinVeil from "./JoinVeil.svelte";
  import EventNudges from "./EventNudges.svelte";

  // ---- the dialogs ----
  //
  // Fifty of them, and every one used to be compiled into the first chunk the
  // app downloads — a fifth of it — so that a session could open the one it
  // wanted without waiting. They are all opened by a click, and a click is an
  // eternity next to reading a file that is already on the device, so they are
  // fetched then instead.
  const MODAL_LOADERS = {
    create: () => import("./modals/ModalCreate.svelte"),
    channel: () => import("./modals/ModalCreateChannel.svelte"),
    category: () => import("./modals/ModalCreate.svelte"),
    emoji: () => import("./modals/ModalEmoji.svelte"),
    gifs: () => import("./modals/ModalGifs.svelte"),
    meme: () => import("./modals/ModalMeme.svelte"),
    forward: () => import("./modals/ModalForward.svelte"),
    report: () => import("./modals/ModalReport.svelte"),
    bans: () => import("./modals/ModalBans.svelte"),
    roles: () => import("./modals/ModalRoles.svelte"),
    modLog: () => import("./modals/ModalModerationLog.svelte"),
    guildHub: () => import("./modals/ModalGuildHub.svelte"),
    guildSettings: () => import("./modals/ModalGuildSettings.svelte"),
    shortcuts: () => import("./modals/ModalShortcuts.svelte"),
    whatsNew: () => import("./modals/ModalWhatsNew.svelte"),
    saved: () => import("./modals/ModalSaved.svelte"),
    when: () => import("./modals/ModalWhen.svelte"),
    scheduled: () => import("./modals/ModalScheduled.svelte"),
    poll: () => import("./modals/ModalPoll.svelte"),
    doodle: () => import("./modals/ModalDoodle.svelte"),
    soundboard: () => import("./modals/ModalSoundboard.svelte"),
    game: () => import("./modals/ModalGame.svelte"),
    compose: () => import("./modals/ModalCompose.svelte"),
    disappear: () => import("./modals/ModalDisappear.svelte"),
    backup: () => import("./modals/ModalBackup.svelte"),
    retention: () => import("./modals/ModalRetention.svelte"),
    stats: () => import("./modals/ModalStats.svelte"),
    chronicle: () => import("./modals/ModalChronicle.svelte"),
    chronicleImport: () => import("./modals/ModalChronicleImport.svelte"),
    blocked: () => import("./modals/ModalBlocked.svelte"),
    requests: () => import("./modals/ModalRequests.svelte"),
    events: () => import("./modals/ModalEvents.svelte"),
    myCalendar: () => import("./modals/ModalMyCalendar.svelte"),
    storyCompose: () => import("./modals/ModalStoryCompose.svelte"),
    storyViewer: () => import("./StoryViewer.svelte"),
    newDM: () => import("./modals/ModalNewDM.svelte"),
    renameGroup: () => import("./modals/ModalRenameGroup.svelte"),
    renameChannel: () => import("./modals/ModalRenameChannel.svelte"),
    channelTopic: () => import("./modals/ModalChannelTopic.svelte"),
    channelLinks: () => import("./modals/ModalChannelLinks.svelte"),
    publish: () => import("./modals/ModalPublish.svelte"),
    meeting: () => import("./modals/ModalMeeting.svelte"),
    newPost: () => import("./modals/ModalNewPost.svelte"),
    forumSettings: () => import("./modals/ModalForumSettings.svelte"),
    rename: () => import("./modals/ModalCreate.svelte"),
    profile: () => import("./modals/ModalProfile.svelte"),
    settings: () => import("./modals/ModalSettings.svelte"),
    notifications: () => import("./modals/ModalNotifications.svelte"),
    inbox: () => import("./modals/ModalInbox.svelte"),
    privacy: () => import("./modals/ModalPrivacy.svelte"),
    bookings: () => import("./modals/ModalBookings.svelte"),
    connection: () => import("./modals/ModalConnection.svelte"),
    reach: () => import("./modals/ModalReach.svelte"),
    linkDevice: () => import("./modals/ModalLinkDevice.svelte"),
    appearance: () => import("./modals/ModalAppearance.svelte"),
    devices: () => import("./modals/ModalDevices.svelte"),
    join: () => import("./modals/ModalJoin.svelte"),
    guildInvite: () => import("./modals/ModalGuildInvite.svelte"),
    invite: () => import("./modals/ModalInvite.svelte"),
    confirm: () => import("./modals/ConfirmDialog.svelte"),
  };

  // The component for whatever S.modal names, or null while it is arriving.
  let ModalView = $state(null);
  let modalSlow = $state(false);
  let modalLoadedKind = "";
  $effect(() => {
    const kind = S.modal?.kind || "";
    if (kind === modalLoadedKind) return;
    modalLoadedKind = "";
    ModalView = null;
    modalSlow = false;
    const load = MODAL_LOADERS[kind];
    if (!load) return;
    const slow = setTimeout(() => (modalSlow = true), 150);
    load().then(
      (m) => {
        clearTimeout(slow);
        modalSlow = false;
        // Opened and shut again, or swapped for another dialog, while the chunk
        // was in the air.
        if (S.modal?.kind !== kind) return;
        modalLoadedKind = kind;
        ModalView = m.default;
      },
      (err) => {
        clearTimeout(slow);
        modalSlow = false;
        S.modal = null;
        flash(err);
      },
    );
    return () => clearTimeout(slow);
  });
  import { startScheduler } from "./lib/scheduled.svelte.js";
  import { startEventRadar, markCalendarSeen, markAllCalendarsSeen } from "./lib/radar.svelte.js";
  import { startEphemeralSweep } from "./lib/ephemeral.svelte.js";

  let composer = $state(null);

  // DMs (incl. the self-DM "Notes") drop the member panel for a roomier view.
  const isDM = $derived(activeGuild()?.kind === "dm");
  // No open channel → show the welcome screen instead of an empty chat.
  const hasChannel = $derived(!!S.activeChannelId && !!activeGuild());
  // Forum channels swap the chat feed for the post board.
  const activeChannelObj = $derived(
    activeGuild()?.channels.find((c) => c.id === S.activeChannelId) || null,
  );

  // Resizable side columns. The persisted pref only overrides the stylesheet
  // when it differs from the wide-desktop default: an untouched pref leaves the
  // CSS var unset, so the sub-900px media query can still pick its narrower
  // base widths. Once you drag, your width wins at every desktop size (clamped).
  const COL_MIN = 160;
  const COL_MAX = 360;
  const COL_DEFAULTS = { colChannels: 220, colMembers: 260 };
  // Live width during a drag, px (0 = not dragging). Persisting happens once,
  // on pointerup, so a drag doesn't hammer localStorage on every pointermove.
  let liveCols = $state({ colChannels: 0, colMembers: 0 });

  function clampCol(w) {
    return Math.min(COL_MAX, Math.max(COL_MIN, Math.round(w)));
  }
  function colVar(key) {
    if (liveCols[key]) return liveCols[key];
    const v = Number(S.prefs[key]);
    // Untouched (or garbage) pref → no override; stylesheet defaults decide.
    if (!v || v === COL_DEFAULTS[key]) return 0;
    return clampCol(v);
  }
  // Per-guild accent: the active guild's banner hue becomes --accent for the
  // whole view, and every derived token (hover/soft/glow are color-mix'd from
  // it) follows — each guild reads as a PLACE. Precedence: a user's explicit
  // accent preset always wins, then the guild, then pack/profile (the root
  // values this stamp overrides). "Use guild colors" in Appearance opts out.
  const guildAccentVars = $derived.by(() => {
    if (S.prefs.accent || S.prefs.guildAccents === false) return "";
    const g = activeGuild();
    if (!g || g.kind === "dm") return "";
    const c = guildAccent(g.banner);
    return c ? `--accent:${c};--accent-fg:${accentForeground(c)}` : "";
  });

  const gridStyle = $derived(
    [
      colVar("colChannels") ? `--col-channels:${colVar("colChannels")}px` : "",
      colVar("colMembers") ? `--col-members:${colVar("colMembers")}px` : "",
      guildAccentVars,
    ]
      .filter(Boolean)
      .join(";"),
  );

  // dir: +1 when dragging right widens the column (channel list, handle on its
  // right edge), -1 when dragging left widens it (member panel, handle on its
  // left edge). Start width is measured from the rendered column — the handle's
  // previous sibling in the grid — so the clamp applies on top of whichever
  // base (220/260, or 190/200 under 900px) is currently in effect.
  function startColDrag(e, key, dir) {
    if (e.button !== 0) return;
    e.preventDefault(); // don't start a text selection under the drag
    const handle = e.currentTarget;
    const startW =
      handle.previousElementSibling?.getBoundingClientRect().width ?? COL_DEFAULTS[key];
    const startX = e.clientX;
    handle.setPointerCapture(e.pointerId);
    const move = (ev) => {
      liveCols[key] = clampCol(startW + dir * (ev.clientX - startX));
    };
    const up = () => {
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", up);
      handle.removeEventListener("pointercancel", up);
      if (liveCols[key]) setPref(key, liveCols[key]);
      liveCols[key] = 0; // pref carries the width from here on
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", up);
    handle.addEventListener("pointercancel", up);
  }

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

  // The call view is up from the CLICK, not from the connection. Joining opens
  // the microphone and then waits on the node, and until the panel appeared
  // early that whole stretch — 130ms on a good day, 31 seconds in the worst
  // soak measured — changed nothing on screen while the mic was already hot.
  const callHere = $derived(
    (S.voice && S.voice.channelId === S.activeChannelId) || S.joiningVoice === S.activeChannelId,
  );
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

  // One-time "What's new" splash after an update. Polish used to ship
  // invisibly — v0.54's headline features arrived unannounced. A fresh install
  // has nothing to announce, so it only records the version and stays quiet.
  let whatsNewChecked = false;
  $effect(() => {
    if (!S.ready || whatsNewChecked) return;
    whatsNewChecked = true;
    api
      .appVersion()
      .then((v) => {
        if (!v) return;
        const seen = localStorage.getItem("concord.seenVersion") || "";
        if (seen === v) return;
        localStorage.setItem("concord.seenVersion", v);
        if (seen && !S.modal) S.modal = { kind: "whatsNew", version: v };
      })
      .catch(() => {});
  });

  // Window title carries the unread signal to the taskbar/dock: "(3) #general
  // — Concord" while minimized or behind another window. Mentions only — raw
  // unread counts would make the title flicker on every chatty channel.
  $effect(() => {
    const ch = activeChannel();
    let mentions = 0;
    for (const g of S.guilds) mentions += guildUnread(g).mentions;
    const where = ch?.name ? `${isDMChannel(ch.id) ? "" : "#"}${ch.name} — ` : "";
    document.title = `${mentions ? `(${mentions}) ` : ""}${where}Concord`;
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

  // Calendar watermarks for the event radar: standing in a calendar IS seeing
  // its events. While ModalEvents (one guild/DM) or ModalMyCalendar (all of
  // them) is open, keep that scope's "seen" mark current — the effect re-runs
  // on every event-cache change, so an event landing under the user's nose is
  // seen too — and advance it once more at close. Clears the rail/pill badges.
  let openCal = null;
  $effect(() => {
    const m = S.modal;
    const cur =
      m?.kind === "myCalendar"
        ? { all: true }
        : m?.kind === "events"
          ? { gid: m.guildId || S.activeGuildId } // same freeze rule ModalEvents uses
          : null;
    if (cur) {
      if (cur.all) markAllCalendarsSeen();
      else markCalendarSeen(cur.gid);
    } else if (openCal) {
      if (openCal.all) markAllCalendarsSeen();
      else markCalendarSeen(openCal.gid);
    }
    openCal = cur;
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
  // The boot mark from index.html. Svelte's mount() appends to #app rather than
  // clearing it, so it has to go by hand — but NOT before we know what replaces
  // it. Removing it up front is what made a refresh flash the login card on its
  // way back to the app: the session probe is a round trip, and until it
  // answers, `!S.ready` means "login". Now the splash covers that gap and
  // crossfades into whichever screen won.
  function clearBootMark() {
    const el = document.getElementById("boot");
    if (!el) return;
    el.classList.add("gone");
    setTimeout(() => el.remove(), 240);
  }

  onMount(async () => {
    // Skip the self-update check on mobile — the app stores own updates there.
    if (!window.Capacitor) checkForUpdate(); // fire-and-forget; works on the login screen too
    initDeepLinks(); // concord:// links (QR scanned with the OS camera)
    // Hardware back and the resume/pause hooks must exist from the FIRST frame:
    // the login and device-linking screens are where a stray back press used to
    // drop a half-set-up user on the launcher.
    wireMobileLifecycle();
    // Escape is the desktop half of the same navigation stack, so it has to be
    // live from the first frame for the same reason: the login screen raises
    // dialogs and a full-screen QR scanner, and until Escape moved into the
    // keymap each dialog answered it with a listener of its own.
    installShortcuts();
    // Hand the launch splash over now that there is something to look at. Held
    // by MainActivity across the WebView load AND the Go core boot, which is a
    // blank window otherwise.
    window.Capacitor?.Plugins?.ConcordCore?.appReady?.().catch(() => {});
    watchSystemBars();
    watchVisibility();
    // Fetch the cosmetic tables once there is something on screen. They are
    // lazy so that a boot never waits for a quarter of a megabyte of path data
    // (lib/cosmetics.svelte.js), but a guild that uses decorations wants them
    // by the time its member list is scrolled — so ask for them in the first
    // idle moment rather than on the first avatar that needs one. On a phone
    // that means requestIdleCallback; a desktop window has the headroom to just
    // do it after the frame.
    if (window.Capacitor && "requestIdleCallback" in window)
      requestIdleCallback(precacheCosmetics, { timeout: 4000 });
    else requestAnimationFrame(() => setTimeout(precacheCosmetics, 0));
    // Closing or reloading the tab while in a call: tell the node to leave, so
    // it stops announcing us to a room we're no longer in. "pagehide" is the
    // one that also fires when a mobile browser backgrounds the page.
    addEventListener("pagehide", () => leaveVoiceOnUnload(S.voice?.channelId || ""));
    // Settle exactly once, whatever happens: session restored, no session, or
    // a core that never answers. The deadline is the safety net — a wedged
    // probe must not trap someone behind a splash forever, and the login
    // screen is the honest thing to show when we can't tell.
    let settled = false;
    const settle = () => {
      if (settled) return;
      settled = true;
      booting = false;
      clearBootMark();
    };
    const deadline = setTimeout(settle, 8000);
    try {
      if (await api.session()) await start();
    } catch {
      /* not unlocked yet — show the login screen */
    } finally {
      clearTimeout(deadline);
      settle();
    }
  });

  // Concorde fly-in: a short takeoff moment between unlocking and the app.
  // Only on a real login (not session-restore refreshes), and never under
  // prefers-reduced-motion.
  // True until the session probe answers — see clearBootMark below.
  let booting = $state(true);
  let flyIn = $state(false);
  function playFlyIn() {
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    flyIn = true;
    setTimeout(() => (flyIn = false), 1500);
  }

  async function start(fromLogin = false) {
    if (fromLogin) playFlyIn();
    await onLogin();
    // Desktop and the browser ask now, as they always have — a dismissed
    // prompt is recoverable there. A phone does not: see asksLazily() in
    // lib/notify.js. On mobile the ask waits for a message the user missed,
    // which offerNotifications() turns into the rationale bar below.
    if (!asksLazily()) requestPermission();
    startScheduler();
    startEventRadar(); // live-meeting + new-event radar (lib/radar.svelte.js)
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
    // Likewise a share that arrived before login: the composer exists (or is
    // about to — insertShare retries while it mounts) only from here on.
    if (pendingShareText) {
      const t = pendingShareText;
      pendingShareText = "";
      shareRetries = 0;
      insertShare(t);
    }
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

  // ---- is anyone looking? ----
  // The Go core drops every periodic loop to one slow shared beat when the app
  // is off screen. Android drives that natively from the Activity lifecycle;
  // until now nothing drove it anywhere else, so a minimised desktop window and
  // a browser tab buried behind forty others kept walking the DHT every fifteen
  // seconds for a UI nobody had looked at in hours.
  //
  // This reports only for THIS page. The backend takes a vote across every
  // attached client and settles only when all of them are hidden, so a second
  // tab (or the phone) does not put the window you are typing in to sleep. On
  // Android the native signal still has the final say — see
  // internal/bridge/visibility.go — because a WebView considers itself visible
  // in situations where the OS has stopped drawing it.
  function watchVisibility() {
    const reporter = createVisibilityReporter({
      send: (visible) => api.setClientVisible(visible).catch(() => {}),
    });
    const onVis = () => reporter.update(!document.hidden);
    const onLeave = () => reporter.leave();
    document.addEventListener("visibilitychange", onVis);
    // pageshow/pagehide cover the back-forward cache, where a restored page
    // never fires visibilitychange — and pagehide is the only one a mobile
    // browser reliably fires on the way out.
    addEventListener("pageshow", onVis);
    addEventListener("pagehide", onLeave);
    onVis(); // the state at mount, not just the first change
    return () => {
      reporter.stop();
      document.removeEventListener("visibilitychange", onVis);
      removeEventListener("pageshow", onVis);
      removeEventListener("pagehide", onLeave);
    };
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

  // Whether this build can register for push at all.
  //
  // It has to be asked, not assumed: PushNotifications.register() reaches
  // FirebaseMessaging, which throws a NATIVE exception on a device with no
  // google-services.json — on a background handler thread, where a JS
  // try/catch cannot stop it, so the app dies. This used to be gated on a
  // window.__CONCORD_PUSH global somebody had to remember to set at build
  // time, in a second place, in step with dropping the file in. The Android
  // shell now reports whether Firebase is actually configured, so the gate
  // follows the build instead of a promise about it.
  //
  // Every failure path answers "no", which is exactly today's behaviour: no
  // registration, no crash, and delivery still working over live sockets and
  // mailbox-drain-on-open. __CONCORD_PUSH is still honoured as an explicit
  // override for iOS, whose APNs entitlement has no equivalent to probe.
  // See docs/PUSH.md.
  async function pushConfigured() {
    if (window.__CONCORD_PUSH) return true;
    const core = window.Capacitor?.Plugins?.ConcordCore;
    if (!core?.pushAvailable) return false;
    try {
      return (await core.pushAvailable())?.available === true;
    } catch {
      return false;
    }
  }

  // Acquire the platform push token (FCM on Android, APNs on iOS) and register
  // it with the rendezvous mailbox, so deposits that land while the app is
  // backgrounded trigger a contentless wake.
  async function registerPushToken() {
    if (!(await pushConfigured())) return; // do NOT call register() — see above
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
  //
  // Back has exactly two jobs, in this order: take away whatever is covering
  // the screen, and then walk out of where you are. The first is the layer
  // stack in lib/navstack.svelte.js, which back shares with desktop Escape. The
  // second is the ladder below, and it is deliberately short:
  //
  //   a post inside a forum board  →  the board
  //   a conversation               →  the channel list (the drawer)
  //   the channel list             →  press again to exit
  //
  // What used to be here was thirteen if/else rungs with a latch in the middle,
  // and it could not leave the app at all. Two of the rungs were "close the
  // drawer" and "open the drawer", so back oscillated between them: press,
  // press, press and you were exactly where you started, while a toast promised
  // an exit that never came. The latch (`backRevealedDrawer`) stopped the loop
  // being infinite and made it a four-press cycle instead.
  //
  // The drawer is the fix. It is not an overlay to be popped, it is the phone's
  // channel list — the only screen that shows one — so leaving a conversation
  // reveals it and leaving IT leaves the app. There is no rung that closes it,
  // which is why the sequence can no longer double back on itself: every press
  // moves strictly outwards and the toast's promise is kept.

  // Layers with no component of their own to register from. Each is something
  // that visibly covers or claims the screen and that back should take away
  // before it starts leaving places.
  syncLayer("reply", () => !!S.replyingTo, () => (S.replyingTo = null));
  syncLayer("call-invite", () => !!S.callInvite, () => (S.callInvite = null));
  syncLayer("knock", () => !!S.knocking, () => (S.knocking = ""));

  let exitArmed = $state(0);
  let exitTimer;
  // How many presses this app still has an answer for: one per open layer, one
  // per place left to walk out of, and one for the "press back again" ask. The
  // drawer standing open IS the outermost place, however it was opened.
  const backSteps = $derived(
    navDepth() +
      (activeChannelObj?.parent ? 1 : 0) +
      (S.isMobile && S.activeChannelId && !S.drawerOpen ? 1 : 0) +
      1,
  );

  function confirmExit(App) {
    // Double-tap to leave. A single press dropping the user on the launcher
    // mid-conversation is the thing that made back feel dangerous.
    if (Date.now() < exitArmed) {
      App.exitApp();
      return;
    }
    exitArmed = Date.now() + 2000;
    // Arming hands the NEXT press to the OS (see reportBackDepth): on Android
    // 13+ that is what lets the system draw its own predictive back-to-home
    // animation for the press that actually leaves.
    clearTimeout(exitTimer);
    exitTimer = setTimeout(() => (exitArmed = 0), 2000);
    flash("Press back again to exit");
  }

  function handleBack(App, canGoBack) {
    // Something is covering the screen: take the top one away.
    if (popLayer()) return;
    // A forum post is a channel nested under its board; going "back" from one
    // means the board, exactly as ChatHeader's breadcrumb does on desktop.
    const parent = activeChannelObj?.parent;
    if (parent) {
      selectChannel(parent);
      return;
    }
    // Out of a conversation and into the list it came from — the single most
    // used back action in every messenger.
    if (S.isMobile && S.activeChannelId && !S.drawerOpen) {
      S.drawerOpen = true;
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
    const core = cap?.Plugins?.ConcordCore;
    // Predictive back (Android 13+): the native side owns an
    // OnBackInvokedCallback whose registration follows reportBackDepth below,
    // and hands each intercepted press back through this event. A shell too old
    // to have it falls back to Capacitor's legacy backButton event, which
    // cannot drive a predictive preview but does still call the same handler.
    if (core?.setBackDepth) core.addListener?.("backPressed", () => handleBack(App, false));
    else App.addListener("backButton", ({ canGoBack }) => handleBack(App, canGoBack));
    App.addListener("resume", () => {
      nudge();
      if (appLocked) tryBioGate();
    });
    // Lock the moment the app leaves the foreground, so the next open (and
    // the OS app-switcher, once the WebView repaints) meets the gate.
    App.addListener("pause", () => {
      if (S.ready && shouldAppLock()) appLocked = true;
    });
    // The ongoing-call notification's "Hang up" action, relayed by the native
    // call service. Same teardown as any in-app leave.
    cap?.Plugins?.ConcordCore?.addListener?.("hangup", () => leaveVoice());
    // Text shared from another app (the OS share sheet). The native side
    // retains the event across a cold start, so this listener catches both
    // warm and cold shares once it's attached in onMount.
    cap?.Plugins?.ConcordCore?.addListener?.("shareIn", (ev) => handleShareIn(ev?.text));
  }

  // ---- how much back the web side wants ----
  //
  // The number is not the layer count: it is "how many more presses does this
  // app have an answer for", and the only distinction the OS cares about is
  // zero versus not. At zero the native callback unregisters and Android owns
  // the gesture — which is what draws the predictive back-to-home preview and
  // performs a real task exit, neither of which a WebView can fake.
  //
  // That is also why arming the exit toast counts as zero. The press that
  // actually leaves is the system's, animation and all; ours is only the one
  // that asks first.
  $effect(() => {
    const core = window.Capacitor?.Plugins?.ConcordCore;
    if (!core?.setBackDepth) return;
    const armed = Date.now() < exitArmed;
    core.setBackDepth({ depth: armed ? 0 : backSteps }).catch(() => {});
  });

  // ---- share-sheet intake (Android) ----
  // v1 is text/links into the ACTIVE conversation's composer draft. A proper
  // conversation picker, and image/video streams, are follow-ups — streams
  // need a route into the composer's attachment flow, which only Composer owns.
  let pendingShareText = "";
  let shareRetries = 0;
  function handleShareIn(text) {
    const t = (text || "").trim();
    if (!t) return;
    // A cold share arrives before login/guilds; start() drains this stash.
    if (!S.ready || !S.activeChannelId) {
      pendingShareText = t;
      return;
    }
    shareRetries = 0;
    insertShare(t);
  }
  function insertShare(text) {
    // The composer owns its draft privately (per-channel, localStorage-backed
    // only on switch), so reach it the way a paste does — through the textarea,
    // whose input event feeds bind:value, the autosize AND the draft save.
    const el = document.querySelector("textarea.draft");
    if (!el) {
      // Mounts a beat after login / channel selection — or never, on a forum
      // board. Try briefly, then tell the user instead of losing their share.
      if (shareRetries++ < 12) {
        setTimeout(() => insertShare(text), 250);
      } else {
        flash("Open a conversation to share into it", "error");
      }
      return;
    }
    el.value = el.value ? `${el.value.replace(/\s+$/, "")}\n${text}` : text;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.focus();
    const g = activeGuild();
    const where = g?.kind === "dm" ? g.name || "the conversation" : `#${activeChannel()?.name || "channel"}`;
    flash(`Shared into ${where}`, "success");
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
      // No flash here: the knock-wait pill says the same thing persistently,
      // and the toast used to land right on top of it.
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
      // How each connection is really doing, per peer. The stage renders it;
      // without it a deadlocked call and a working one look identical.
      onPeerStatus: (peerId, st) => (S.voicePeerStatus = { ...S.voicePeerStatus, [peerId]: st }),
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
    startCallClock();
    // Android: a microphone-type foreground service for the call's duration —
    // without it, Android 14+ cuts the mic the moment the app leaves the
    // screen, and the room hears you silently drop.
    window.Capacitor?.Plugins?.ConcordCore?.startCallService?.().catch(() => {});
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
    // The call is over — release the microphone foreground service first so
    // the ongoing-call notification never outlives the call it announces.
    window.Capacitor?.Plugins?.ConcordCore?.stopCallService?.().catch(() => {});
    // Hand the audio route back to the OS: MODE_IN_COMMUNICATION (and the
    // earpiece proximity lock) outliving the call would mute media playback
    // and blank the screen in-pocket. Every leave path funnels through here,
    // so this is the single place the route is cleared. The setRoute/getRoute
    // half of the contract lives in lib/devices.js; reset is call-teardown
    // only, hence the direct runtime-global reach (no-op off Android).
    window.Capacitor?.Plugins?.CallAudio?.reset?.().catch(() => {});
    // If we locked the call, unlock it as we leave, and clear knock bookkeeping.
    if (isCallLocked(ch)) api.signalCall(ch, "unlock").catch(() => {});
    clearCallState(ch);
    S.voice.mesh.stop();
    S.voice = null;
    stopCallClock();
    // Suppress the incoming-call ring for this channel: the others may still be
    // in the room, and without this you'd immediately re-ring yourself for the
    // call you just left. (Auto-cleared when the room's roster empties.)
    if (ch && !S.dismissedCalls.includes(ch)) S.dismissedCalls = [...S.dismissedCalls, ch];
    S.voiceParticipants = [];
    S.voiceStates = {};
    S.voiceSpeaking = [];
    S.voicePeerFpr = {};
    S.voicePeerStatus = {};
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
    // Creating a channel produced no acknowledgement at all: the dialog shut and
    // the new row appeared somewhere in a list of thirty, below the fold as
    // often as not. Say so, and go there.
    const made = activeGuild()?.channels.find((c) => c.name === name.trim());
    if (made) {
      selectChannel(made.id);
      // …and point at it. A toast in the corner says it worked; it does not
      // answer "where did it go?", and the answer is a row somewhere in a list
      // of thirty, below the fold as often as not.
      flashChannel(made.id);
    }
    flash(`#${name.trim()} created`, "success");
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

  // "Later" on the recovery-phrase nudge. Session-only, deliberately: the
  // sticky "don't ask again" that the notification bar gets would be the wrong
  // shape here, because the thing being asked for has not been done and the
  // consequence of never doing it is losing the account.
  let backupNudgeHidden = $state(false);

  // Every fenced code block the markdown renderer emits carries a Copy button.
  // The blocks arrive as {@html}, in the feed, the archive, forum posts and the
  // preview pane alike, so no component owns them — one delegated listener at
  // the root serves all of them and cannot go stale when a body re-renders.
  // Copying code out of chat was a select-and-drag through a horizontally
  // scrolling box, which is how people ended up with half a line.
  function onRootClick(e) {
    const btn = e.target?.closest?.("[data-code-copy]");
    if (!btn) return;
    e.preventDefault();
    e.stopPropagation();
    const code = btn.parentElement?.querySelector("pre code");
    if (!code) return;
    navigator.clipboard?.writeText(code.textContent || "");
    haptic("light");
    btn.classList.add("copied");
    setTimeout(() => btn.classList.remove("copied"), 1400);
  }
</script>

<svelte:window onclick={onRootClick} />

<!-- The core stopped answering. Everything on screen is now a photograph: the
     presence dots, the member list and the peer count all render the last thing
     the backend said, and they will keep saying it forever. This bar is the only
     thing that knows, so it is not dismissible and it outranks the other two —
     an update offer from a process that is no longer there is noise. -->
{#if S.offline && S.ready}
  <!-- A full-width bar ABOVE the app, not a pill floating over it. As a
       centered pill it landed squarely on the channel header and took search,
       pinned, events and the call controls with it — so the one moment you most
       want to reach the call you are sitting in was the moment its buttons were
       covered. The bar reserves its own height instead (see .app below).
       The wording is for the person reading it: nobody has a "core". -->
  <div class="offline-bar" role="status" aria-live="polite">
    <span class="ob-spin" aria-hidden="true"></span>
    <span class="ob-text">
      <strong>Reconnecting…</strong> You're offline. Messages will send when you're back.
      {#if S.voice}<span class="ob-call">The call is trying to reconnect too.</span>{/if}
    </span>
  </div>
{/if}

<!-- Notification rationale. Raised by offerNotifications() the first time a
     message arrives that the OS grant would have surfaced, so the sentence can
     point at something that just happened rather than at a hypothetical. The
     Enable button is what opens the system dialog — on Android there are only
     ever two of those, and this is how one gets spent on somebody who wants it. -->
{#if S.notifyAsk && !S.update && !ringingChannel && !S.offline}
  <div class="update-banner notif-ask">
    <span class="ub-text">
      <strong>You just missed a message.</strong> Turn on notifications and Concord can tell you next time.
    </span>
    <button
      class="ub-dl"
      onclick={() => {
        requestPermission();
        dismissNotifyAsk(true);
      }}
    >
      Enable
    </button>
    <button class="ub-close" onclick={() => dismissNotifyAsk(true)} aria-label="Not now">×</button>
  </div>
{/if}

<!-- The signup screen holds the door and asks for the recovery phrase back, but
     a page reload walks straight past it — and an account created on one device
     with its phrase written down nowhere is one dead disk away from being gone
     for good. There is no server holding a copy and no reset link, so nobody
     else can put this right later. It keeps coming back until the words have
     been verified, or simply looked at again in Settings; "Later" quiets it for
     this session only, which is the whole point of it. -->
{#if S.ready && S.prefs.backupPending && !backupNudgeHidden && !S.update && !S.notifyAsk && !ringingChannel && !S.offline}
  <div class="update-banner backup-nudge">
    <span class="ub-text">
      <strong>Write down your recovery phrase.</strong> It is the only way back into this account, and
      right now it exists on this device and nowhere else.
    </span>
    <button class="ub-dl" onclick={() => (S.modal = { kind: "settings" })}>Show me</button>
    <button class="ub-close" onclick={() => (backupNudgeHidden = true)} aria-label="Later">×</button>
  </div>
{/if}

{#if S.update && !ringingChannel && !S.offline}
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
  <!-- Stackable effect, over the app rather than behind it, so it composes
       with the opaque packs too. On a phone the particle field gets a smaller
       `scale`, which is the engine's own signal to cut the particle count
       (lib/fx.js); the gradient effects drop a layer in CSS. -->
  <FxOverlay fx={validFx(S.prefs.themeFx)} scale={S.isMobile ? 0.55 : 1} />
{/if}

{#if booting}
  <!-- Nothing: index.html's boot splash still owns the screen until the
       session probe says whether this is a login or a restored session. -->
{:else if !S.ready}
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
  <div
    class="app"
    class:no-panel={isDM || !hasChannel}
    class:offline-shift={S.offline}
    style={gridStyle}
  >
    <GuildRail />
    <ChannelList
      onJoinVoice={joinVoice}
      onLeaveVoice={leaveVoice}
      onToggleMute={toggleMicMute}
      onToggleShare={toggleScreenShare}
      onToggleCamera={toggleCamera}
    />
    <!-- Overlaps the channel list's right edge (explicit grid placement, so it
         takes no track of its own). Must come right after ChannelList: the drag
         measures its previous sibling. Double-click resets to the default. -->
    <div
      class="col-rz rz-channels"
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize channel list"
      onpointerdown={(e) => startColDrag(e, "colChannels", 1)}
      ondblclick={() => setPref("colChannels", COL_DEFAULTS.colChannels)}
    ></div>

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
        <!-- Search results TAKE the pane, for every channel type. They used to
             be mounted inside MessageList, which a forum channel never renders —
             so searching from a forum board filled S.searchResults and displayed
             them nowhere.
             They also used to be a 380px band in the flow, with the live channel
             carrying on at full brightness underneath and the join landing
             wherever it landed: the boundary cut a message row through the
             middle, which reads as a rendering fault rather than as a panel.
             Covering the column instead means nothing is ever half-drawn, and
             the feed keeps its scroll position because it is still mounted and
             still laid out — merely behind. -->
        <div class="pane-body">
          {#if activeChannelObj?.type === "forum"}
            <ForumView forum={activeChannelObj} />
          {:else}
            <MessageList onDropFiles={(files) => files.forEach((f) => composer?.attachFile(f))} />
            <Composer bind:this={composer} />
          {/if}
          <SearchPanel />
        </div>
      {:else}
        <Welcome />
      {/if}
    </main>

    {#if !isDM && hasChannel && S.prefs.memberPanel}
      <MemberPanel />
      <!-- Inner (left) edge of the member panel; dragging left widens it. -->
      <div
        class="col-rz rz-members"
        role="separator"
        aria-orientation="vertical"
        aria-label="Resize member panel"
        onpointerdown={(e) => startColDrag(e, "colMembers", -1)}
        ondblclick={() => setPref("colMembers", COL_DEFAULTS.colMembers)}
      ></div>
    {/if}
  </div>{/if}
{#if S.ready}

  {#if S.quickSwitcher}
    <QuickSwitcher />
  {/if}

  <ProfilePopover />
  <ContextMenu />

  <!-- Knocking on a locked call: waiting to be admitted. The door icon ripples
       on the same knock-knock rhythm the host sees on our avatar — both ends of
       the door share one heartbeat. -->
  {#if S.knocking}
    <div class="knock-wait" role="status">
      <span class="kw-door" aria-hidden="true">
        <span class="kw-ring"></span>
        <Icon name="door" size={16} />
      </span>
      <span class="kw-copy">
        <span class="kw-line">Knocking…</span>
        <span class="kw-sub">waiting for someone inside to let you in</span>
      </span>
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

  <!-- The event radar's banners: "live now — Join" and "new event — View".
       Top-center, apart from the toast pile — these carry a verb. -->
  <EventNudges onJoinVoice={joinVoice} />

  <!-- The join threshold — covers everything while an event room is entered. -->
  <JoinVeil />

  <!-- Concorde fly-in: the mark spools up on the runway, cracks off with a
       shockwave ring, then BANKS into a climb to the upper right, dragging a
       contrail and a sweep of speed through the curtain it pulls off the app.
       Pure theater, 1.5s, once per unlock. pointer-events:none — never blocks
       input, and playFlyIn() bails entirely under reduced motion. -->
  {#if flyIn}
    <div class="flyin" aria-hidden="true">
      <span class="boom"></span>
      {#each [0, 1, 2, 3, 4] as i (i)}
        <span class="streak s{i}"></span>
      {/each}
      <span class="sweep"></span>
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

  <!-- Modals. One component variable for all of them: ModalView is whichever
       dialog the current S.modal.kind has been loaded for (see MODAL_LOADERS),
       which is what lets fifty dialogs stay off the boot bundle without any of
       them losing the props they are called with. -->
  {#if ModalView}
    {#if S.modal?.kind === "create"}
      <ModalView onSubmit={createGuild} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "channel"}
      <ModalView onSubmit={createChannel} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "category"}
      <ModalView
        onSubmit={createCategory}
        onClose={() => (S.modal = null)}
        title="Create a category"
        hint="Groups channels in the sidebar."
        placeholder="Category name"
      />
    {:else if S.modal?.kind === "emoji"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "gifs"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "doodle"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "game"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "soundboard"}
      <!-- onPick is set when the studio is opened from a voice room, where the
           outcome is a sound played for everyone rather than a message sent. -->
      <ModalView onPick={S.modal.onPick || null} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "meme"}
      <!-- `edit` reopens a meme already in the channel; `src` starts a new one
           from a picture. They are mutually exclusive — see ModalMeme. -->
      <ModalView src={S.modal.src || ""} edit={S.modal.edit || null} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "forward"}
      <ModalView message={S.modal.message} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "report"}
      <ModalView message={S.modal.message} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "bans"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "roles"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "modLog"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "guildHub"}
      <!-- The hub is the front door; guildSettings below is now its Overview
           panel (opened via openPanel, so Back returns to the hub). -->
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "guildSettings"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "shortcuts"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "whatsNew"}
      <ModalView version={S.modal.version} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "saved"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "when"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "scheduled"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "poll"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "compose"}
      <ModalView
        initial={S.modal.initial || ""}
        editId={S.modal.editId || ""}
        onSent={S.modal.onSent}
        onClose={() => (S.modal = null)}
      />
    {:else if S.modal?.kind === "disappear"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "backup"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "retention"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "stats"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "chronicle"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "chronicleImport"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "blocked"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "requests"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "events"}
      <!-- onJoinVoice: a voice-channel-located event's Join enters the call
           through the same lifecycle a sidebar click uses (knock included). -->
      <ModalView onClose={() => (S.modal = null)} onJoinVoice={joinVoice} />
    {:else if S.modal?.kind === "myCalendar"}
      <ModalView onClose={() => (S.modal = null)} onJoinVoice={joinVoice} />
    {:else if S.modal?.kind === "storyCompose"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "storyViewer"}
      <!-- Not a Modal: a fullscreen overlay (the studios' tier). It still
           routes through S.modal so the tray can open it from anywhere, and it
           registers its own overlay closer for Esc / the hardware back button. -->
      <ModalView
        stories={S.modal.stories || []}
        start={S.modal.start || 0}
        onClose={() => (S.modal = null)}
      />
    {:else if S.modal?.kind === "newDM"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "renameGroup"}
      <ModalView
        guildId={S.modal.guildId}
        current={S.modal.current}
        onClose={() => (S.modal = null)}
      />
    {:else if S.modal?.kind === "renameChannel"}
      <ModalView
        guildId={S.modal.guildId}
        channelId={S.modal.channelId}
        current={S.modal.current}
        onClose={() => (S.modal = null)}
      />
    {:else if S.modal?.kind === "channelTopic"}
      <ModalView
        channel={S.modal.channel}
        onSubmit={(t) => {
          setChannelTopic(S.modal.channel, t.trim());
          S.modal = null;
        }}
        onClose={() => (S.modal = null)}
      />
    {:else if S.modal?.kind === "channelLinks"}
      <ModalView channel={S.modal.channel} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "publish"}
      <ModalView message={S.modal.message} channel={S.modal.channel} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "meeting"}
      <ModalView
        code={S.modal.code}
        guestLink={S.modal.guestLink || ""}
        guildId={S.modal.guildId || ""}
        expires={S.modal.expires || 0}
        onClose={() => (S.modal = null)}
      />
    {:else if S.modal?.kind === "newPost"}
      <ModalView forum={S.modal.forum} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "forumSettings"}
      <ModalView forum={S.modal.forum} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "rename"}
      <ModalView
        onSubmit={renameGuild}
        onClose={() => (S.modal = null)}
        title="Rename guild"
        hint="Renames the guild for everyone."
        placeholder={activeGuild()?.name || "New name"}
      />
    {:else if S.modal?.kind === "profile"}
      <ModalView identity={S.identity} onSubmit={saveProfile} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "settings"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "notifications"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "inbox"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "privacy"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "bookings"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "connection"}
      <ModalView
        onClose={() => (S.modal = null)}
        onSaved={() => flash("Rendezvous saved", "success")}
      />
    {:else if S.modal?.kind === "reach"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "linkDevice"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "appearance"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "devices"}
      <ModalView onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "join"}
      <ModalView error={S.modal.error} onSubmit={joinGuild} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "guildInvite"}
      <ModalView invite={S.modal.invite} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "invite"}
      <ModalView code={S.modal.code} onCopy={copy} onClose={() => (S.modal = null)} />
    {:else if S.modal?.kind === "confirm"}
      <ModalView
        title={S.modal.title}
        body={S.modal.body}
        confirmLabel={S.modal.confirmLabel}
        onConfirm={S.modal.onConfirm}
        onClose={() => (S.modal = null)}
      />
    {/if}
  {:else if S.modal && modalSlow}
    <!-- The dialog's code is still on its way. Only after 150ms, so opening one
         from a chunk already in memory — which is every time after the first —
         shows nothing at all rather than a flicker. -->
    <div class="modal-wait" role="status" aria-label="Opening…">
      <span class="mw-spin"></span>
    </div>
  {/if}
{/if}

<style>
  /* Shown only when a dialog's code takes longer than a frame or two to arrive
     — a cold first open, or a slow disk. Deliberately not a full backdrop: the
     dialog is about to draw its own, and two of them fading over each other
     reads as a flicker. */
  .modal-wait {
    position: fixed;
    inset: 0;
    z-index: 200;
    display: grid;
    place-items: center;
    pointer-events: none;
  }
  .mw-spin {
    width: 22px;
    height: 22px;
    border: 2px solid color-mix(in srgb, var(--border) 60%, transparent);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: mw-turn 0.7s linear infinite;
  }
  @keyframes mw-turn {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .mw-spin {
      animation: none;
    }
  }

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
  /* The mark is a head-on view with the nose UP, so a climb to the upper right
     means banking the sprite ~40° first — the nose has to point where it's
     going or it reads as a jet sliding sideways. It spools on the spot, then
     the same keyframe rotates AND translates, which is what sells the bank.
     Everything here is transform/opacity; the glow is a static drop-shadow. */
  .flyin-jet {
    position: absolute;
    left: 46%;
    top: 74%;
    color: var(--text);
    filter: drop-shadow(0 0 16px color-mix(in srgb, var(--accent) 60%, transparent));
    animation: flyin-jet 1.4s cubic-bezier(0.5, 0, 0.25, 1) forwards;
  }
  /* Trail hangs from the tail in the jet's OWN rotated frame, so it lines up
     with the flight path for free once the sprite banks — brightest at the
     nozzle, gone by the far end. */
  .contrail {
    position: absolute;
    top: 88%;
    left: 50%;
    transform: translateX(-50%);
    width: 3px;
    height: 74vh;
    border-radius: 2px;
    background: linear-gradient(
      180deg,
      color-mix(in srgb, var(--accent) 80%, white),
      color-mix(in srgb, var(--accent) 35%, transparent) 45%,
      transparent
    );
    opacity: 0.85;
  }
  @keyframes flyin-jet {
    /* On the runway: nose still up, engines lighting. */
    0% {
      transform: translate(-50%, 6vh) rotate(0deg) scale(0.8);
      opacity: 0;
    }
    14% {
      transform: translate(-50%, 2vh) rotate(0deg) scale(1);
      opacity: 1;
    }
    /* The crack: banks into the climb and goes. */
    30% {
      transform: translate(-50%, -4vh) rotate(38deg) scale(1.06);
      opacity: 1;
    }
    100% {
      transform: translate(78vw, -104vh) rotate(42deg) scale(1.3);
      opacity: 1;
    }
  }
  /* Shockwave at the moment of acceleration — one ring, gone in 700ms. */
  .boom {
    position: absolute;
    left: 46%;
    top: 74%;
    width: 60px;
    height: 60px;
    margin: -30px 0 0 -30px;
    border-radius: 50%;
    border: 2px solid color-mix(in srgb, var(--accent) 70%, white);
    opacity: 0;
    animation: flyin-boom 0.75s ease-out 0.26s forwards;
  }
  @keyframes flyin-boom {
    0% {
      transform: scale(0.3);
      opacity: 0.85;
    }
    100% {
      transform: scale(7);
      opacity: 0;
    }
  }
  /* Speed lines racing along the flight axis — the cheap trick that turns
     "something moved" into "something moved FAST". */
  .streak {
    position: absolute;
    left: 40%;
    top: 70%;
    width: 2px;
    height: 130px;
    border-radius: 2px;
    background: linear-gradient(180deg, transparent, color-mix(in srgb, var(--accent) 60%, white), transparent);
    opacity: 0;
    transform: rotate(42deg);
    animation: flyin-streak 0.6s ease-out forwards;
  }
  .streak.s0 {
    left: 26%;
    top: 84%;
    animation-delay: 0.3s;
  }
  .streak.s1 {
    left: 58%;
    top: 88%;
    animation-delay: 0.36s;
  }
  .streak.s2 {
    left: 38%;
    top: 60%;
    height: 90px;
    animation-delay: 0.42s;
  }
  .streak.s3 {
    left: 70%;
    top: 66%;
    animation-delay: 0.48s;
  }
  .streak.s4 {
    left: 16%;
    top: 62%;
    height: 80px;
    animation-delay: 0.54s;
  }
  @keyframes flyin-streak {
    0% {
      transform: rotate(42deg) translateY(40px);
      opacity: 0;
    }
    35% {
      opacity: 0.7;
    }
    100% {
      transform: rotate(42deg) translateY(-260px);
      opacity: 0;
    }
  }
  /* The curtain being pulled: a wide skewed band of light crossing along the
     flight path. Reads as a directional wipe, but it's one translate — no
     mask or clip-path animation on a full-screen element. */
  .sweep {
    position: absolute;
    top: -60%;
    left: -70%;
    width: 60%;
    height: 220%;
    transform: rotate(42deg) translateX(-60vw);
    background: linear-gradient(
      90deg,
      transparent,
      color-mix(in srgb, var(--accent) 22%, transparent) 45%,
      color-mix(in srgb, var(--accent) 8%, transparent) 60%,
      transparent
    );
    opacity: 0;
    animation: flyin-sweep 0.95s cubic-bezier(0.4, 0, 0.2, 1) 0.24s forwards;
  }
  @keyframes flyin-sweep {
    0% {
      transform: rotate(42deg) translateX(-60vw);
      opacity: 0;
    }
    25% {
      opacity: 1;
    }
    100% {
      transform: rotate(42deg) translateX(190vw);
      opacity: 0;
    }
  }
  @keyframes flyin-fade {
    0%,
    38% {
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
    gap: var(--sp-2);
    text-align: center;
  }
  .rc-inner h2 {
    margin: 4px 0 0;
    font-size: 18px;
  }
  .rc-inner p {
    margin: 0;
    font-size: var(--fs-ui);
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
    padding: var(--sp-5);
  }
  .lock-badge {
    display: grid;
    place-items: center;
    width: 64px;
    height: 64px;
    border-radius: 50%;
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent-hover);
    margin-bottom: var(--sp-1);
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
    font-size: var(--fs-ui);
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
    color: var(--danger-fg);
  }
  .app {
    display: grid;
    /* Side-column widths come from the vars only once the user has dragged a
       resize handle (script sets them from S.prefs); the --cw/--mw fallbacks
       here are the untouched defaults, and the 900px tier below narrows them.
       The intermediate vars exist so the resize HANDLES (absolute, below) can
       track the same widths without repeating the fallback logic. */
    --cw: var(--col-channels, 220px);
    --mw: var(--col-members, 260px);
    grid-template-columns: 64px var(--cw) 1fr var(--mw);
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
    grid-template-columns: 64px var(--cw) 1fr;
  }
  /* Room for the offline bar. Shifting the whole shell rather than overlaying
     it is the entire point: the header underneath stays usable. */
  .app.offline-shift {
    --ob-total: calc(var(--offline-bar-h) + max(var(--safe-top), var(--sa-top, 0px)));
    margin-top: var(--ob-total);
    height: calc(100% - var(--ob-total));
  }
  /* Column resize handles: thin strips overlapping each side column's inner
     edge. Absolutely positioned OUT of the grid flow on purpose — as grid
     children they perturbed auto-placement and rehomed every later sibling
     (the chat column collapsed into a 32px strip; caught by the live smoke
     test). Desktop-only — coarse pointers can't hit a 5px strip, and phones
     get MobileShell anyway. */
  .col-rz {
    display: none;
    position: absolute;
    top: 0;
    bottom: 0;
    width: 5px;
    z-index: 5;
    cursor: col-resize;
    touch-action: none; /* a drag resizes; it must never scroll */
    background: var(--accent);
    opacity: 0; /* invisible until hovered; the cursor is the affordance */
    transition: opacity var(--dur-quick) ease;
  }
  .rz-channels {
    left: calc(64px + var(--cw) - 5px);
  }
  .rz-members {
    right: var(--mw);
  }
  @media (pointer: fine) {
    .col-rz {
      display: block;
    }
    .col-rz:hover {
      opacity: 0.5;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .col-rz {
      transition: none;
    }
  }
  /* Narrow desktop (a window parked beside something else). Not a phone tier —
     below 768px S.isMobile is true and MobileShell renders instead of .app —
     just a density step for the columns.
     The member panel used to be display:none'd here while ChatHeader still
     offered its toggle, so the control silently did nothing for a whole band of
     window widths. It keeps its column, narrower; the toggle decides. */
  @media (max-width: 900px) {
    .app {
      /* Only the fallbacks narrow — a user-dragged width still wins, and the
         handles keep tracking via the same intermediate vars. */
      --cw: var(--col-channels, 190px);
      --mw: var(--col-members, 200px);
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
  /* Everything below the header and the call panel: the feed and the composer,
     with the search results able to cover exactly that and no more. The header
     stays out of it deliberately — the search box you are typing into lives
     there. */
  .pane-body {
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }
  /* Update-available banner: a floating top-center pill (doesn't cover the rail). */
  .update-banner {
    position: fixed;
    top: calc(12px + max(var(--safe-top), var(--sa-top, 0px)));
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    max-width: calc(100vw - 24px);
    padding: 8px 10px 8px 16px;
    background: var(--bg-1);
    border: 1px solid var(--accent);
    border-radius: 22px;
    box-shadow: var(--shadow-pop);
    z-index: 205;
    font-size: var(--fs-ui);
  }
  /* Amber rather than accent: this is the one banner that is a warning about
     something the reader has to go and do, not an offer. Same colour the feed
     uses for a mention, for the same reason — attention without alarm. */
  .backup-nudge {
    border-color: var(--warn);
  }
  .ub-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  /* Trouble, not news: the warning colour, and no dismiss button, because the
     condition doesn't end when you stop looking at it. A BAR rather than one of
     the floating pills above, because it can be up for minutes and nothing
     underneath it should be unreachable for that whole time. */
  .offline-bar {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 205;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--sp-3);
    height: var(--offline-bar-h);
    padding: 0 var(--sp-4);
    padding-top: max(var(--safe-top), var(--sa-top, 0px));
    box-sizing: content-box;
    background: var(--bg-1);
    border-bottom: 1px solid var(--warn);
    color: var(--warn-text);
    font-size: var(--fs-ui);
  }
  .ob-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .ob-call {
    opacity: 0.85;
  }
  .ob-spin {
    flex-shrink: 0;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    border: 2px solid color-mix(in srgb, var(--warn) 30%, transparent);
    border-top-color: var(--warn);
    animation: ob-spin 0.8s linear infinite;
  }
  @keyframes ob-spin {
    to {
      transform: rotate(360deg);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .ob-spin {
      animation: none;
    }
  }
  .ub-dl {
    flex-shrink: 0;
    padding: 5px 14px;
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: var(--radius-lg);
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
  /* Waiting-at-the-door pill. */
  .knock-wait {
    position: fixed;
    top: calc(16px + max(var(--safe-top), var(--sa-top, 0px)));
    max-width: calc(100vw - 24px);
    left: 50%;
    /* Centering lives on `translate`, not `transform`, so the entrance keyframe
       can animate transform without snapping the pill off-center. */
    translate: -50% 0;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: 8px 10px 8px 8px;
    /* Same doorway light as the host's knock card: accent spilling from the
       icon's corner, so the two ends of this interaction rhyme. */
    background: linear-gradient(
      115deg,
      color-mix(in srgb, var(--accent) 14%, var(--bg-elevated, var(--bg-1))),
      var(--bg-elevated, var(--bg-1)) 70%
    );
    border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
    border-radius: 999px;
    box-shadow: var(--shadow-pop);
    z-index: 215;
    font-size: var(--fs-compact);
    animation: kw-in 240ms var(--ease-calm);
  }
  @keyframes kw-in {
    from {
      opacity: 0;
      transform: translateY(-8px);
    }
  }
  .kw-door {
    flex-shrink: 0;
    position: relative;
    width: 34px;
    height: 34px;
    display: grid;
    place-items: center;
    border-radius: 50%;
    background: color-mix(in srgb, var(--accent) 20%, transparent);
    color: var(--accent-hover, var(--accent));
  }
  .kw-ring {
    position: absolute;
    inset: 0;
    border-radius: 50%;
    border: 2px solid var(--accent);
    opacity: 0;
    pointer-events: none;
    /* 2.4s to match the host-side knock rings — one knock, one heartbeat. */
    animation: kw-ping 2.4s ease-out infinite;
  }
  @keyframes kw-ping {
    0% {
      transform: scale(0.8);
      opacity: 0.6;
    }
    45%,
    100% {
      transform: scale(1.45);
      opacity: 0;
    }
  }
  .kw-copy {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .kw-line {
    font-weight: 650;
    line-height: 1.2;
  }
  .kw-sub {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .kw-cancel {
    flex-shrink: 0;
    padding: 6px 14px;
    background: var(--bg-3);
    color: var(--text);
    border-radius: 999px;
    font-size: var(--fs-tiny);
    font-weight: 600;
  }
  .kw-cancel:hover {
    background: var(--bg-input);
  }
  @media (prefers-reduced-motion: reduce) {
    .knock-wait {
      animation: none;
    }
    .kw-ring {
      /* Still-waiting must read without motion: one static soft halo. */
      animation: none;
      opacity: 0.3;
      transform: none;
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
    gap: var(--sp-2);
    flex-shrink: 0;
  }
  .ring-btn {
    padding: 7px 16px;
    border-radius: var(--radius-md);
    font-weight: 600;
    font-size: var(--fs-ui);
    white-space: nowrap;
    transition: background var(--dur-quick) ease, transform 0.08s ease, border-color var(--dur-quick) ease, color var(--dur-quick) ease;
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
      translate: none;
      max-width: none;
      font-size: var(--fs-ui);
    }
    .kw-cancel {
      min-height: var(--tap-min);
      padding: 0 var(--sp-4);
      font-size: var(--fs-compact);
    }
    /* The copy column yields; Cancel keeps its full tap size. The subline
       wraps instead of ellipsizing mid-word — there's vertical room to spare
       on a phone, and "let yo…" read like a bug. */
    .kw-copy {
      flex: 1;
    }
    .kw-sub {
      white-space: normal;
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
