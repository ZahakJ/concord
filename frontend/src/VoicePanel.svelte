<script>
  // The call stage: one big square per participant (avatar, or their live
  // camera), animated speaking rings, plus wide tiles for any screen shares.
  // Dynamic and animated, and the same in a DM call or a guild voice room.
  import { scale } from "svelte/transition";
  import { flip } from "svelte/animate";
  import Avatar from "./Avatar.svelte";
  import Icon from "./Icon.svelte";
  import {
    S,
    memberByFpr,
    nameFor,
    getVideoStream,
    activeGuild,
    isCallLocked,
    toggleCallLock,
    canLockCall,
    callLockedBy,
    admitKnocker,
    denyKnocker,
    isGuestFpr,
    togglePeerMute,
    setPeerVolume,
    setVideoStream,
    openContextMenu,
    channelShort,
    flash,
    retryMic,
    keySurface,
  } from "./lib/state.svelte.js";
  import { callClock } from "./lib/calltimer.svelte.js";
  import { setSelfViewCovered } from "./lib/selfview.svelte.js";
  import { api } from "./lib/api.js";
  import { tooltip } from "./lib/tooltip.js";
  import { bindLabel } from "./lib/keybind.js";
  import { syncLayer } from "./lib/navstack.svelte.js";
  import { bestFit, columnsFor } from "./lib/tilefit.js";
  import { haptic, longpress } from "./lib/touch.js";
  import { SOUNDBOARD, playSfx, playRecipe } from "./lib/sounds.js";
  import { recipeGlyph } from "./lib/sfxrecipe.js";
  import { shelfSounds } from "./lib/soundshelf.svelte.js";

  // Soundboard: play locally on press (instant feedback), gossip a ~30-byte
  // "sfx" trigger on the room's voice topic — every peer synthesizes the same
  // recipe locally (lib/sounds.js). Own presses rate-limit to match the
  // receive-side gate, so what you hear is what the room hears.
  //
  // Two kinds of button, one trigger. A built-in press puts its ID on the wire
  // and every client looks it up; a shelf press puts the whole RECIPE there,
  // because the recipe is only tens of bytes and nobody else has it. The
  // `target` field is an opaque string end to end, so neither needs a backend
  // change — and a peer on a build that predates recipes looks the payload up
  // in its table of six, misses, and plays nothing.
  //
  // The board itself was fourteen unlabelled emoji in one strip, identified by
  // a hover tooltip that deliberately does not exist on a touch screen — so on
  // a phone it was fourteen anonymous pictures, two of them 🥁 and two of them
  // 🎺. It is a grid of named chips now, split under its two headings, and the
  // rate limit — which used to swallow a press in complete silence, so a
  // refused press and a broken button looked identical — draws itself as a
  // sweeping cooldown on the button you just pressed.
  const SFX_COOLDOWN_MS = 1000;
  let sfxOpen = $state(false);
  let sfxLastPress = $state(0);
  let sfxCooling = $state("");
  let coolTimer = null;
  const mySounds = $derived(shelfSounds().slice(0, 8));

  // True while the gate is shut. Read as $state so the buttons can go dim for
  // exactly as long as a press would be refused.
  function beginCooldown(key) {
    sfxLastPress = Date.now();
    sfxCooling = key;
    clearTimeout(coolTimer);
    coolTimer = setTimeout(() => (sfxCooling = ""), SFX_COOLDOWN_MS);
  }
  // aria-disabled, not `disabled`. Both refusals are real — pressSfx and
  // pressMine already gate on the clock, so the attribute was never what
  // enforced the cooldown — but `disabled` on the element that HAS focus makes
  // the browser drop focus to <body>. Every press therefore threw the keyboard
  // user out of the board they had just used, after one sound, silently. The
  // dimming and the refusal stay; the focus does too.
  const cooling = $derived(!!sfxCooling);
  function pressSfx(id) {
    if (Date.now() - sfxLastPress < SFX_COOLDOWN_MS) return;
    beginCooldown(`b:${id}`);
    haptic("light");
    playSfx(id);
    api.signalCall(chId, "sfx", id).catch(() => {});
  }
  function pressMine(s) {
    if (Date.now() - sfxLastPress < SFX_COOLDOWN_MS) return;
    beginCooldown(`m:${s.payload}`);
    haptic("light");
    playRecipe(s.recipe);
    api.signalCall(chId, "sfx", s.payload).catch(() => {});
  }

  // 1-6 fire the six built-ins while the board is open. A soundboard you have
  // to aim at is a soundboard you use once; the whole point of an airhorn is
  // that it arrives on the beat.
  //
  // Opening the board takes focus, and that is what makes the digits usable at
  // all: the composer holds focus for almost the whole life of a channel, and
  // a bare number typed into a message must stay a number. So the keys work
  // from the moment you open the board and stop the moment you click back into
  // the text box — which is the same rule the app's other keyboard policy uses.
  let boardEl = $state(null);
  $effect(() => {
    if (sfxOpen && boardEl) boardEl.querySelector(".sfx")?.focus();
  });
  function sfxKey(e) {
    if (!sfxOpen || e.altKey || e.ctrlKey || e.metaKey) return;
    const t = e.target;
    if (t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName))) return;
    const n = Number(e.key);
    if (!Number.isInteger(n) || n < 1 || n > SOUNDBOARD.length) return;
    e.preventDefault();
    pressSfx(SOUNDBOARD[n - 1].id);
  }
  import {
    canShareScreen,
    listDevices,
    canRouteAudio,
    currentRoute,
    setAudioRoute,
    AUDIO_ROUTES,
  } from "./lib/devices.js";

  // In push-to-talk the mic button stops being a toggle you watch and becomes a
  // readout of a key you're holding, so it says which key and lights up live.
  const pttOn = $derived(!!S.prefs.pushToTalk && !!S.prefs.pttBind);
  const pttKey = $derived(bindLabel(S.prefs.pttBind));

  // Join/leave pop for tiles and strip bubbles; zero-duration under
  // prefers-reduced-motion (Svelte transitions don't read the media query).
  const noMotion =
    typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
  const pop = { duration: noMotion ? 0 : 190, start: 0.82 };

  let { onLeaveVoice, onToggleMute, onToggleDeafen, onToggleShare, onToggleCamera } = $props();

  // Solo in a DM call = still ringing the other person.
  const solo = $derived(S.voiceParticipants.length === 0);

  // Soft lock is meaningful in any call that isn't a DM (a DM is already private
  // to its two members). That includes an instant MEETING: its guest link is
  // public by design, and the lock is exactly what turns "anyone with the link
  // walks in" into office hours where you let people in one at a time.
  // The panel is on screen from the moment the channel is CLICKED, before the
  // mesh exists, so that a join has something to show for itself — see
  // `joining` below and App.svelte's callHere.
  const chId = $derived(S.voice?.channelId || S.joiningVoice || "");
  const joining = $derived(!S.voice && !!S.joiningVoice);
  // The padlock is HIDDEN, not disabled, for anyone who cannot use it. A
  // disabled control still advertises a power you do not have and invites the
  // question of why; and there was no check at all before this, so a member of
  // Warshat al-Layl holding a role with permissions ZERO locked the guild
  // owner's own room from the same eight-button bar he had.
  const canLock = $derived(activeGuild()?.kind !== "dm" && canLockCall(chId));
  const locked = $derived(isCallLocked(chId));
  // Who shut it. The fingerprint comes off the authenticated sender of the lock
  // signal, so this is a claim the app can actually stand behind — the same way
  // a forum post says who locked IT.
  const lockedBy = $derived(callLockedBy(chId));
  const knockers = $derived(S.callKnocks[chId] || []);
  const isDM = $derived(activeGuild()?.kind === "dm");

  // A knocker's face for the door card. Members resolve to their real profile;
  // a browser guest has no member record, so Avatar falls back to initials of
  // the name baked into their guest fingerprint.
  function knockerInfo(fpr) {
    const mem = memberByFpr(fpr);
    return {
      name: nameFor(fpr),
      emoji: mem?.emoji || "",
      color: mem?.color || "",
      image: mem?.avatar || "",
    };
  }

  // Door-card enter/exit: the card fades and rises while its SLOT (height +
  // padding) grows/collapses, so the stack and the panel edge settle smoothly
  // instead of snapping when a knocker arrives or is dealt with.
  function knockSlot(node, { duration = 240 } = {}) {
    const h = node.offsetHeight;
    return {
      duration: noMotion ? 0 : duration,
      easing: (t) => 1 - Math.pow(1 - t, 3), // cubic-out — calm, no bounce
      css: (t, u) => `
        opacity: ${t};
        transform: translateY(${u * -8}px) scale(${0.96 + t * 0.04});
        height: ${t * h}px;
        padding-top: ${t * 10}px;
        padding-bottom: ${t * 10}px;
        overflow: hidden;
      `,
    };
  }

  // Ring for ~30s while alone in a DM call, then quietly settle into "just you
  // in the call" if they never pick up.
  let ringTimedOut = $state(false);
  $effect(() => {
    if (!solo) {
      ringTimedOut = false;
      return;
    }
    ringTimedOut = false;
    const id = setTimeout(() => (ringTimedOut = true), 30000);
    return () => clearTimeout(id);
  });
  // Neither applies until we are actually in the room: "Waiting for others to
  // join" while our own connection is still being made says the wrong thing
  // about the wrong party.
  const ringing = $derived(solo && isDM && !ringTimedOut && !joining);
  const waiting = $derived(solo && !isDM && !joining); // empty guild voice channel

  function participant(peerId) {
    const fpr = S.voicePeerFpr[peerId];
    const mem = fpr ? memberByFpr(fpr) : null;
    return {
      // nameFor is the one place names resolve — it knows about browser guests
      // ("Zaza (guest)"), who have no member record to look up.
      //
      // Until the fingerprint lands this used to fall back to the first eight
      // characters of the libp2p peer id, so for a second or so after somebody
      // joined their tile was labelled "12D3KooW" with an avatar reading "12" —
      // while the sidebar two inches below already said their name. A nameless
      // tile reads as "someone is arriving"; a hex string reads as a crash.
      name: fpr ? nameFor(fpr) : "Joining…",
      pending: !fpr,
      emoji: mem?.emoji || "",
      color: mem?.color || "",
      image: mem?.avatar || "",
    };
  }

  // ---- how each connection is actually doing ----
  //
  // The stage used to render exactly one thing in every state there is:
  // negotiating, connected, half-connected, deadlocked, peer's network gone,
  // peer's process gone — an avatar and a name, identically. So the two ways a
  // call can silently carry no audio at all (see lib/voice.js) looked like a
  // perfect call, and there was nothing on screen to disagree with.
  //
  // VoiceMesh's watchdog supplies the state; this turns it into a word.
  const STATUS_LABEL = {
    connecting: "Connecting…",
    reconnecting: "Reconnecting…",
    failed: "Couldn't connect",
  };
  function connStatus(pid) {
    if (pid === "self") return null;
    const st = S.voicePeerStatus[pid];
    if (!st) return { state: "connecting", label: STATUS_LABEL.connecting };
    if (st.state === "connected") {
      // Connected and silent. The connection has a path; nothing is coming
      // down it. Opus DTX is off, so this really is silence on the wire and
      // not a person who has stopped talking.
      return st.media ? null : { state: "quiet", label: "Can't hear them" };
    }
    return { state: st.state, label: STATUS_LABEL[st.state] || "" };
  }
  // Nobody is through. Said once for the room rather than four times over four
  // tiles, and only when there is somebody to be through TO. It follows the
  // worst tile: while there is still hope it says so, and when there isn't it
  // stops promising.
  const roomLine = $derived.by(() => {
    if (S.offline && S.voice) return "Reconnecting to the call…";
    const others = S.voiceParticipants;
    if (!others.length) return "";
    if (others.some((p) => S.voicePeerStatus[p]?.state === "connected")) return "";
    if (others.every((p) => S.voicePeerStatus[p]?.state === "failed")) {
      return "Nobody can hear each other — these connections couldn't be made.";
    }
    return "Nobody can hear each other yet — still connecting.";
  });

  // The camera tile for a roster entry ("self" or a peerId), if they have one
  // live. Remote keys embed a random stream id, so match on peerId/kind, not the
  // key string.
  const camTile = (pid) =>
    S.videoTiles.find((t) => t.kind === "camera" && (pid === "self" ? t.self : t.peerId === pid));

  // Everyone in the room, self first.
  const roster = $derived(["self", ...S.voiceParticipants]);
  // Screen shares get their own wide tiles — a share isn't a person.
  const screens = $derived(S.videoTiles.filter((t) => t.kind === "screen"));

  // ---- the call takes the stage ----
  //
  // `max-height: 46vh` made a live call a band above an empty chat: on a 900px
  // window two participants got a 230px strip with two 200px tiles floating in
  // it, and the remaining 60% of the column was the "Welcome to Study Hall"
  // empty state of a conversation nobody was having, because everybody was
  // talking. The call was never the main event in its own channel.
  //
  // So when a call is live in the channel you are looking at, the call is the
  // column and the chat collapses to a strip at the bottom — the reverse of
  // what it was. The strip is a real strip, not a hidden pane: the composer and
  // the last message or two stay on screen and stay MOUNTED, which matters
  // because the message list is virtualized and unmounting it would throw away
  // both its window and the reader's place in it.
  //
  // The toggle in the header puts the chat back, because there is a real second
  // mode here — a call you are listening to while reading — and which one you
  // want is a preference, not a guess. Per device, like the dock's position.
  const STAGE_KEY = "concord.callTakesStage";
  function loadStagePref() {
    try {
      return localStorage.getItem(STAGE_KEY) !== "off";
    } catch {
      return true;
    }
  }
  let stageFirst = $state(loadStagePref());
  function toggleStageFirst() {
    stageFirst = !stageFirst;
    haptic("light");
    try {
      localStorage.setItem(STAGE_KEY, stageFirst ? "on" : "off");
    } catch {
      /* private mode — the choice still holds for this session */
    }
  }
  // Tiles grow with the space instead of capping at 200px. The column count is
  // the meeting-grid rule (ceil(sqrt(n)), capped at four), which is what gives
  // two people two big tiles side by side and nine people a 3x3.
  // The arrangement the box can actually afford (lib/tilefit.js) once the stage
  // has been measured; the square rule until then, and for the band that is not
  // the stage at all.
  let fitCols = $state(0);
  const cols = $derived(fitCols || columnsFor(roster.length));
  // While joining there is nothing on the stage yet, and a full-column empty
  // panel above a squashed chat is a worse first second than the old band.
  const onStage = $derived(stageFirst && !joining);

  // ---- the shape of a tile ----
  //
  // The arithmetic — and the reason it is arithmetic — lives in lib/tilefit.js.
  // Here it is only wired up: measure the stage, hand the two lengths back as
  // custom properties, and re-measure on anything that changes the sum.
  let gridEl = $state(null);
  let tileW = $state(0); // 0 = unmeasured; the grid rules stay in charge
  let tileH = $state(0);
  let rowW = $state(0);
  function refit() {
    if (!gridEl || !onStage) {
      tileW = 0;
      fitCols = 0;
      return;
    }
    const gap = parseFloat(getComputedStyle(gridEl).rowGap) || 12;
    // The WIDTH has to come from the stage's container, not the stage: the fit
    // pins the stage to one row's width, so measuring the stage would feed its
    // own last answer back in and the tiles would ratchet smaller every time
    // somebody joined. Its height is set by the flex column and is safe to read
    // off the stage itself.
    const host = gridEl.parentElement;
    const hcs = host && getComputedStyle(host);
    const availW = host
      ? host.clientWidth - parseFloat(hcs.paddingLeft || 0) - parseFloat(hcs.paddingRight || 0)
      : gridEl.clientWidth;
    const fit = bestFit({
      w: availW,
      h: gridEl.clientHeight,
      n: roster.length,
      gap,
    });
    if (!fit) {
      tileW = 0;
      return;
    }
    fitCols = fit.cols;
    tileW = fit.tileW;
    tileH = fit.tileH;
    rowW = fit.rowW;
  }
  $effect(() => {
    // Who is here, how many columns that implies, and the size of the box they
    // are in — the three inputs, and the observer for the third.
    void roster.length;
    void onStage;
    if (!gridEl) return;
    refit();
    const ro = new ResizeObserver(refit);
    ro.observe(gridEl);
    if (gridEl.parentElement) ro.observe(gridEl.parentElement);
    return () => ro.disconnect();
  });

  const clock = $derived(callClock());
  const roomName = $derived(chId ? channelShort(chId) : "");

  // Is your own camera drawn here, big enough to be a preview? The grid always
  // shows your own tile; theater shows it only when you are the focused thing.
  // SelfView reads the answer and fills the gap. Cleared on unmount, so
  // navigating away from the call's channel brings the preview with you.
  $effect(() => {
    const mine = !!camTile("self");
    setSelfViewCovered(mine && (!inTheater || focusedPid === "self"));
    return () => setSelfViewCovered(false);
  });

  // Focus/theater mode: one thing fills the stage (a screen share OR a
  // participant), everyone else drops to a small strip. A shared screen
  // auto-focuses so it isn't a same-size tile you have to scroll to. Click a
  // strip item to switch focus; click the big view (or shrink) to exit.
  let focusedKey = $state(null);
  const focusedScreen = $derived(screens.find((t) => t.key === focusedKey) || null);
  const focusedPid = $derived(roster.includes(focusedKey) ? focusedKey : null);
  const inTheater = $derived(!!focusedScreen || !!focusedPid);

  // Auto-focus the first screen share when it appears (once), and clear focus if
  // the focused thing goes away.
  let autoFocused = $state(false);
  $effect(() => {
    if (screens.length && !focusedKey && !autoFocused) {
      focusedKey = screens[0].key;
      autoFocused = true;
    }
    if (!screens.length) autoFocused = false;
    if (focusedKey && !screens.some((t) => t.key === focusedKey) && !roster.includes(focusedKey)) {
      focusedKey = null;
    }
  });
  function toggleFocus(key) {
    focusedKey = focusedKey === key ? null : key;
  }

  // Fullscreen the focused view. On a phone that alone is worth little: a 16:9
  // share letterboxed into a portrait screen still only fills a third of the
  // glass, so we ask for landscape at the same time — which is also the only
  // moment Android permits the lock (it refuses outside fullscreen), and which
  // iOS/WKWebView simply ignores.
  let stageEl = $state(null);
  // Theater and fullscreen are two layers, in the order you entered them: back
  // leaves the fullscreen first and the big view second. Neither was on the
  // ladder before, so back out of a full-screen share exited the app.
  // Tracked from the event, not from a flag we set ourselves: Escape and the
  // system's own fullscreen affordances leave it without asking us.
  let isFullscreen = $state(false);
  $effect(() => {
    const sync = () => (isFullscreen = !!document.fullscreenElement);
    document.addEventListener("fullscreenchange", sync);
    sync();
    return () => document.removeEventListener("fullscreenchange", sync);
  });
  syncLayer("theater", () => inTheater, () => (focusedKey = null));
  syncLayer("fullscreen", () => isFullscreen, () => document.exitFullscreen?.());
  async function toggleFullscreen() {
    try {
      if (document.fullscreenElement) {
        await document.exitFullscreen?.();
        screen.orientation?.unlock?.();
        return;
      }
      await stageEl?.requestFullscreen?.();
      if (S.isMobile) await screen.orientation?.lock?.("landscape");
    } catch {
      /* denied, unsupported, or the lock refused — the fullscreen still stands */
    }
  }

  // Pinch/pan on the focused view.
  //
  // Without this a shared 1920x1080 desktop arrives on a 393px phone at roughly
  // 20% scale: `object-fit: contain` fits the WHOLE screen into a 16:9 box, so
  // body text in an editor or a terminal is sub-pixel and there is nothing to
  // scroll to — the picture is all there, just unreadable. Fullscreen buys ~1.9x
  // against the ~5x this needs.
  //
  // Pointer events rather than touch events so a trackpad/mouse drag pans too,
  // and `touch-action: none` because the shell sets `pan-y` on the pane around
  // us and would otherwise claim every vertical finger movement.
  const ZOOM_MAX = 6;
  function zoomable(node, key) {
    const pts = new Map();
    let scale = 1;
    let tx = 0;
    let ty = 0;
    let pinch = null; // { dist, mx, my, scale, tx, ty }
    let pan = null; // { x, y, tx, ty }
    let moved = false;

    node.style.touchAction = "none";
    node.style.transformOrigin = "0 0";

    // The FRAME the video sits in, measured off the parent: node's own
    // getBoundingClientRect reports the box after our transform, so using it
    // would feed the scale back into the next clamp and run away.
    const box = () => (node.parentElement || node).getBoundingClientRect();
    function apply() {
      // Clamp so the scaled picture always covers the box: at scale 1 that
      // pins it back to the origin, which doubles as the reset.
      const b = box();
      tx = Math.min(0, Math.max(-(scale - 1) * b.width, tx));
      ty = Math.min(0, Math.max(-(scale - 1) * b.height, ty));
      node.style.transform = scale === 1 ? "" : `translate(${tx}px, ${ty}px) scale(${scale})`;
      node.style.cursor = scale === 1 ? "" : "grab";
    }
    function reset() {
      scale = 1;
      tx = 0;
      ty = 0;
      apply();
    }
    const local = (e) => {
      const b = box();
      return { x: e.clientX - b.left, y: e.clientY - b.top };
    };

    function onDown(e) {
      if (e.target.closest?.(".focus-actions")) return; // the buttons are not the canvas
      node.setPointerCapture?.(e.pointerId);
      pts.set(e.pointerId, local(e));
      moved = false;
      if (pts.size === 2) {
        const [a, b] = [...pts.values()];
        pinch = {
          dist: Math.hypot(a.x - b.x, a.y - b.y) || 1,
          mx: (a.x + b.x) / 2,
          my: (a.y + b.y) / 2,
          scale,
          tx,
          ty,
        };
        pan = null;
      } else if (pts.size === 1 && scale > 1) {
        const p = local(e);
        pan = { x: p.x, y: p.y, tx, ty };
      }
    }
    function onMove(e) {
      if (!pts.has(e.pointerId)) return;
      pts.set(e.pointerId, local(e));
      if (pinch && pts.size >= 2) {
        const [a, b] = [...pts.values()];
        const d = Math.hypot(a.x - b.x, a.y - b.y) || 1;
        const next = Math.min(ZOOM_MAX, Math.max(1, (pinch.scale * d) / pinch.dist));
        // Hold the point under the pinch midpoint still while the scale changes.
        const px = (pinch.mx - pinch.tx) / pinch.scale;
        const py = (pinch.my - pinch.ty) / pinch.scale;
        scale = next;
        tx = pinch.mx - px * next;
        ty = pinch.my - py * next;
        moved = true;
        apply();
        return;
      }
      if (pan) {
        const p = local(e);
        if (Math.hypot(p.x - pan.x, p.y - pan.y) > 6) moved = true;
        tx = pan.tx + (p.x - pan.x);
        ty = pan.ty + (p.y - pan.y);
        apply();
      }
    }
    function onUp(e) {
      pts.delete(e.pointerId);
      node.releasePointerCapture?.(e.pointerId);
      if (pts.size < 2) pinch = null;
      if (pts.size === 0) pan = null;
      // Lifting one finger out of a pinch leaves the other one panning. Re-seed
      // from where it is now, or the picture jumps by the whole pinch offset.
      if (pts.size === 1 && scale > 1) {
        const [p] = [...pts.values()];
        pan = { x: p.x, y: p.y, tx, ty };
      }
    }
    // A clean tap on the picture leaves theater mode (the click handler on
    // .focus-main). While zoomed in it must not: there it means "back to fit",
    // and a gesture that merely ended over the video is not a tap at all.
    function onClick(e) {
      if (moved || scale > 1) {
        e.stopPropagation();
        e.preventDefault();
        if (!moved && scale > 1) reset();
      }
      moved = false;
    }

    node.addEventListener("pointerdown", onDown);
    node.addEventListener("pointermove", onMove);
    node.addEventListener("pointerup", onUp);
    node.addEventListener("pointercancel", onUp);
    node.addEventListener("click", onClick, { capture: true });
    return {
      update(next) {
        if (next !== key) {
          key = next;
          reset(); // a different share is a different picture
        }
      },
      destroy() {
        node.removeEventListener("pointerdown", onDown);
        node.removeEventListener("pointermove", onMove);
        node.removeEventListener("pointerup", onUp);
        node.removeEventListener("pointercancel", onUp);
        node.removeEventListener("click", onClick, { capture: true });
      },
    };
  }

  // Tapping the big view exits it — which the comment above has claimed since
  // this was written, while no handler existed anywhere. The zoom action eats
  // this click when it was a gesture or while zoomed in.
  function leaveFocus(e) {
    if (e.target.closest(".focus-actions")) return;
    focusedKey = null;
  }

  function tileInfo(pid) {
    if (pid === "self") {
      return {
        name: S.displayName || "You",
        emoji: S.identity.emoji,
        color: S.identity.color,
        image: S.identity.avatar,
        speaking: S.voiceSpeaking.includes("self"),
        muted: S.muted,
        deafened: S.deafened,
        self: true,
        // Your own tile exists the instant you click, and says so until the
        // mesh is up. Before this, a slow join (the worst one measured took
        // 31 seconds) changed nothing on screen at all while the microphone
        // was already open and live.
        status: joining ? { state: "connecting", label: "Connecting…" } : null,
      };
    }
    const p = participant(pid);
    const fpr = S.voicePeerFpr[pid];
    const st = (fpr && S.voiceStates[fpr]) || {};
    return {
      ...p,
      // Your own account on a second device: a real participant, but naming it
      // twice with no explanation reads as a glitch.
      name: fpr && fpr === S.identity.fingerprint ? `${p.name} (other device)` : p.name,
      speaking: S.voiceSpeaking.includes(pid),
      // Their own mute/deafen, as announced by their client — deafened implies
      // muted, so show the stronger of the two rather than both badges.
      muted: !!st.muted,
      deafened: !!st.deafened,
      self: false,
      localMuted: S.peerVolumes[pid] === 0,
      // A browser guest, identified by the peer id the gateway gives them. They
      // are the only participant a host removes from here, and the only one who
      // needs it: a member is handled with roles and bans, while a guest holds
      // nothing but an open socket and a link.
      guest: isGuestFpr(pid),
      status: connStatus(pid),
    };
  }

  // Remove a guest from the meeting. Refusing at the door covers office hours;
  // this is for the one you let in and then want gone. It was unreachable until
  // now — the only disconnect control lived on the channel list's voice roster,
  // which renders for channels of type "voice", and a meeting's single channel
  // has no type at all. So in the one room guests can reach, the button that
  // removes them did not exist.
  async function evictGuest(pid, name) {
    if (!S.activeChannelId) return;
    try {
      await api.signalCall(S.activeChannelId, "disconnect", pid, "");
      flash(`Removed ${name}`, "success");
    } catch (err) {
      flash(err);
    }
  }

  // Everything a tile can do, as a long-press action sheet (right-click on a
  // desktop). On a phone this is the ONLY way to reach these: the corner buttons
  // are 22-26px controls inset into a tile whose whole body is itself a tap
  // target, one of which removes a person from the meeting over the wire —
  // hitting the intended one with a thumb was luck.
  function tileMenu(e, pid, t) {
    openContextMenu(e, [
      { label: t.self ? `${t.name} (you)` : t.name, header: true },
      {
        label: focusedKey === pid ? "Exit full view" : "Full view",
        icon: "screen",
        onClick: () => toggleFocus(pid),
      },
      !t.self && {
        label: t.localMuted ? `Unmute ${t.name} for me` : `Mute ${t.name} for me`,
        icon: t.localMuted ? "speaker" : "deafened",
        onClick: () => togglePeerMute(pid),
      },
      // Per-person volume, for you only. The mesh has applied a real per-peer
      // gain under the master level since it was written, and the only thing
      // that ever called it passed 0 or 1 — so the one participant who is too
      // loud could be silenced or endured and nothing in between.
      !t.self && {
        range: true,
        label: `${t.name}'s volume, for you`,
        value: S.peerVolumes[pid] ?? 1,
        onInput: (v) => setPeerVolume(pid, v),
      },
      t.guest && { sep: true },
      t.guest && {
        label: "Remove from call",
        icon: "door",
        danger: true,
        onClick: () => evictGuest(pid, t.name),
      },
    ]);
  }

  // The caption on a shared screen, whole. It used to be a name interpolated
  // into `{screenLabel(tile)}'s screen`, and the name for yourself was "You" —
  // so your own share, its tooltip and its aria-label all read "You's screen",
  // which is the single most-read string in a shared call.
  function screenTitle(tile) {
    return tile.self ? "Your screen" : `${participant(tile.peerId).name}'s screen`;
  }

  // Give up on the connection we have and build a new one. Offered only after
  // the watchdog has spent its own retries, so by here another offer down the
  // same pipe is not the answer — a fresh RTCPeerConnection is.
  function retryPeer(pid) {
    haptic("light");
    S.voice?.mesh.reconnectPeer(pid);
  }

  // ---- phone-only call controls ----

  // Screen sharing simply does not exist on Android's WebView (MediaProjection
  // is never handed to the web layer) or WKWebView. The button used to render
  // there regardless and swallow its own TypeError, so a prominent control did
  // nothing at all, silently, however many times you pressed it.
  const shareable = canShareScreen;

  // Front/back camera. Showing someone what you are looking at is the main
  // reason a phone camera goes on in a call, and until now the only route to it
  // was a settings sheet listing "Camera 1 / Camera 2".
  let cameras = $state(0);
  let facing = $state("");
  $effect(() => {
    if (!S.cameraOn) return;
    listDevices().then((d) => (cameras = d.camera.length));
  });
  async function flipCamera() {
    const mesh = S.voice?.mesh;
    if (!mesh?.flipCamera) return;
    haptic("light");
    const stream = await mesh.flipCamera();
    facing = mesh.facing;
    setVideoStream("self:camera", stream, { self: true, kind: "camera" });
    if (!stream) flash("Couldn't switch camera", "error");
  }

  // Earpiece / speaker / Bluetooth. The web platform cannot move a call between
  // them at all (see lib/devices.js), so this control appears only where the
  // native half exists — not as a button that pretends.
  const canRoute = canRouteAudio();
  let route = $state("earpiece");
  let routes = $state(["earpiece", "speaker"]);
  $effect(() => {
    if (!canRoute) return;
    currentRoute().then((r) => {
      if (!r) return;
      route = r.route;
      routes = r.available;
    });
  });
  const routeInfo = $derived(AUDIO_ROUTES.find((r) => r.id === route) || AUDIO_ROUTES[0]);
  async function cycleRoute() {
    const order = AUDIO_ROUTES.filter((r) => routes.includes(r.id));
    if (!order.length) return;
    const at = order.findIndex((r) => r.id === route);
    haptic("light");
    // The OS gets the last word (a wired headset overrides everything), so show
    // what actually happened rather than what was asked for.
    const got = await setAudioRoute(order[(at + 1) % order.length].id);
    if (got) route = got;
  }

  // FLAG_KEEP_SCREEN_ON while any camera or screen share is live on the stage.
  // App.svelte's navigator.wakeLock covers voice-only calls, but the WebView
  // drops that lock silently on visibility flips and the re-acquire can fail;
  // for video — where a sleeping screen ends the show mid-frame — the native
  // window flag has no such failure mode. No-op outside the Android app.
  $effect(() => {
    const core = window.Capacitor?.Plugins?.ConcordCore;
    if (!core?.setKeepAwake || !S.videoTiles.length) return;
    core.setKeepAwake({ on: true }).catch(() => {});
    return () => {
      core.setKeepAwake({ on: false }).catch(() => {});
    };
  });

  // The phone's "⋯" — everything the four-button bar doesn't carry, as the
  // same action sheet every other secondary action in this app opens into.
  function moreControls(e) {
    openContextMenu(e, [
      { label: "Call controls", header: true },
      {
        label: sfxOpen ? "Hide the soundboard" : "Soundboard",
        icon: "megaphone",
        active: sfxOpen,
        onClick: () => (sfxOpen = !sfxOpen),
      },
      canRoute && {
        label: `Audio out: ${routeInfo.label}`,
        icon: routeInfo.icon,
        onClick: cycleRoute,
      },
      S.cameraOn &&
        cameras > 1 && {
          label: `Switch to the ${facing === "environment" ? "front" : "back"} camera`,
          icon: "forward",
          onClick: flipCamera,
        },
      shareable && {
        label: S.sharing ? "Stop sharing" : "Share screen",
        icon: S.sharing ? "screenOff" : "screen",
        active: S.sharing,
        onClick: onToggleShare,
      },
      canLock && {
        label: locked ? "Unlock call" : "Lock call (people must knock)",
        icon: "lock",
        active: locked,
        onClick: toggleCallLock,
      },
      { sep: true },
      {
        label: "Audio & video settings",
        icon: "gear",
        onClick: () => (S.modal = { kind: "devices" }),
      },
    ]);
  }

  // Muting is the one control you need to know landed without looking at it,
  // and the phone said nothing — the :active scale is under your fingertip at
  // exactly the moment it plays.
  const withHaptic =
    (fn, style = "light") =>
    () => {
      haptic(style);
      fn?.();
    };

  // Landscape tiles are 54-74px tall (see the landscape block below), and a
  // fixed 64px avatar in one is sliced top and bottom with the name badge lying
  // across what survives.
  let compactTiles = $state(false);
  $effect(() => {
    if (typeof matchMedia !== "function") return;
    const mq = matchMedia("(pointer: coarse) and (max-height: 480px)");
    const sync = () => (compactTiles = mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  });

  // Every tile below is `muted`: a shared screen can carry its own sound, and
  // all call audio plays through the mesh's own audio elements — the only path
  // that honors deafen, per-person volume, the master level and the chosen
  // speaker. Unmuted tiles would double it up (a stream can be on screen twice)
  // and slip past all four.
  //
  // Svelte action: bind a MediaStream to a <video>'s srcObject (not a plain attr).
  function srcObject(node, key) {
    const attach = (k) => {
      node.srcObject = getVideoStream(k);
    };
    attach(key);
    return {
      update: attach,
      destroy: () => (node.srcObject = null),
    };
  }
</script>

<svelte:window onkeydown={sfxKey} />

<div class="voice-panel" class:theater={inTheater} class:on-stage={onStage} style="--cols:{cols}">
  <!-- The stage's own header: which room, how many people, how long, and
       whether the door is locked. None of it existed — a call had no elapsed
       time anywhere in the app, and a locked call (which makes everyone else
       knock) announced itself only by one of eight identical circular buttons
       picking up a subtle `active` class. -->
  <div class="stage-head">
    <span class="sh-live" aria-hidden="true"></span>
    <span class="sh-room">{roomName || "Call"}</span>
    <span class="sh-count">{roster.length} in call</span>
    {#if locked}
      <span class="sh-lock">
        <Icon name="lock" size={11} />
        {lockedBy ? `Locked by ${lockedBy} — people must knock` : "Locked — people must knock"}
      </span>
    {/if}
    <span class="sh-spacer"></span>
    {#if clock}<span class="sh-clock" aria-label="Call duration">{clock}</span>{/if}
    <button
      class="sh-swap"
      use:tooltip={{ text: onStage ? "Show the chat" : "Give the call the whole column" }}
      aria-label={onStage ? "Show the chat" : "Give the call the whole column"}
      aria-pressed={onStage}
      onclick={toggleStageFirst}
    >
      <Icon name={onStage ? "hash" : "screen"} size={14} />
    </button>
  </div>

  {#if ringing || waiting}
    <div class="ringing">
      <span class="dots"><span></span><span></span><span></span></span>
      {ringing ? "Ringing…" : "Waiting for others to join…"}
    </div>
  {/if}

  {#if inTheater}
    <!-- Theater mode: one big view (a share or a participant), everyone else in
         a clickable strip. -->
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="focus-main"
      class:speaking={focusedPid ? tileInfo(focusedPid).speaking : false}
      bind:this={stageEl}
      onclick={leaveFocus}
    >
      {#if focusedScreen}
        <!-- svelte-ignore a11y_media_has_caption -->
        <video use:srcObject={focusedScreen.key} use:zoomable={focusedScreen.key} autoplay playsinline muted></video>
        <span class="screen-label"><Icon name="screen" size={12} /> {screenTitle(focusedScreen)}</span>
      {:else}
        {@const t = tileInfo(focusedPid)}
        {@const cam = camTile(focusedPid)}
        {#if cam}
          <!-- svelte-ignore a11y_media_has_caption -->
          <video use:srcObject={cam.key} autoplay playsinline muted class:mirror={t.self && facing !== "environment"}></video>
        {:else}
          <div class="focus-face" style={t.color ? `--tint:${t.color}` : ""}>
            <Avatar name={t.name} emoji={t.emoji} color={t.color} image={t.image} size={96} />
          </div>
        {/if}
        <span class="screen-label">{t.self ? `${t.name} (you)` : t.name}</span>
        {#if t.speaking}
          <span class="eq" aria-hidden="true"><span></span><span></span><span></span></span>
        {/if}
        {#if t.deafened}
          <span class="mute-badge" use:tooltip={"Deafened — can't hear the call"} aria-label="Deafened">
            <Icon name="deafened" size={11} />
          </span>
        {:else if t.muted}
          <span class="mute-badge" use:tooltip aria-label="Muted"><Icon name="micOff" size={11} /></span>
        {/if}
      {/if}
      <div class="focus-actions">
        <button class="fbtn" use:tooltip aria-label="Fullscreen" onclick={toggleFullscreen}>
          <Icon name="screen" size={14} />
        </button>
        <button class="fbtn" use:tooltip aria-label="Exit full view" onclick={() => (focusedKey = null)}>
          <Icon name="close" size={14} />
        </button>
      </div>
    </div>
    <div class="strip">
      {#each screens as tile (tile.key)}
        {#if tile.key !== focusedKey}
          <button
            class="thumb"
            use:tooltip={{ text: screenTitle(tile) }}
            aria-label={screenTitle(tile)}
            onclick={() => toggleFocus(tile.key)}
          >
            <!-- svelte-ignore a11y_media_has_caption -->
            <video use:srcObject={tile.key} autoplay playsinline muted></video>
            <span class="thumb-badge"><Icon name="screen" size={10} /></span>
          </button>
        {/if}
      {/each}
      {#each roster as pid (pid)}
        {#if pid !== focusedKey}
          {@const t = tileInfo(pid)}
          <!-- Name chips, not bare circles. The strip carried its names in a
               tooltip, which does not exist on a touch screen and is a hover
               away on a desktop — so in a six-person call the row under the
               share was six unreadable faces. -->
          <button
            class="bubble"
            class:speaking={t.speaking}
            transition:scale={pop}
            aria-label={t.self ? `${t.name} (you)` : t.name}
            onclick={() => toggleFocus(pid)}
          >
            <Avatar name={t.name} emoji={t.emoji} color={t.color} image={t.image} size={34} />
            <span class="bub-name">{t.self ? "You" : t.name}</span>
            {#if t.deafened}
              <span class="bub-mark" aria-hidden="true"><Icon name="deafened" size={9} /></span>
            {:else if t.muted}
              <span class="bub-mark" aria-hidden="true"><Icon name="micOff" size={9} /></span>
            {/if}
          </button>
        {/if}
      {/each}
    </div>
  {:else}
    <div
      class="stage"
      class:solo={roster.length === 1}
      class:fitted={onStage && tileW > 0}
      bind:this={gridEl}
      style={tileW ? `--tile-w:${tileW}px;--tile-h:${tileH}px;--row-w:${rowW}px` : ""}
    >
      {#each roster as pid (pid)}
        {@const t = tileInfo(pid)}
        {@const cam = camTile(pid)}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div
          class="tile"
          class:speaking={t.speaking}
          class:pending={t.pending}
          class:unsettled={!!t.status}
          class:st-connecting={t.status?.state === "connecting"}
          class:st-reconnecting={t.status?.state === "reconnecting"}
          class:st-failed={t.status?.state === "failed"}
          class:st-quiet={t.status?.state === "quiet"}
          transition:scale={pop}
          onclick={() => toggleFocus(pid)}
          oncontextmenu={(e) => tileMenu(e, pid, t)}
          use:longpress={{ handler: (e) => tileMenu(e, pid, t) }}
          use:tooltip={{ text: `Click to focus ${t.self ? "yourself" : t.name}` }}
          aria-label="Click to focus {t.self ? 'yourself' : t.name}"
        >
          {#if cam}
            <!-- svelte-ignore a11y_media_has_caption -->
            <video
              use:srcObject={cam.key}
              autoplay
              playsinline
              muted
              class:mirror={t.self && facing !== "environment"}
            ></video>
          {:else}
            <div class="face" style={t.color ? `--tint:${t.color}` : ""}>
              <Avatar
                name={t.name}
                emoji={t.emoji}
                color={t.color}
                image={t.image}
                size={compactTiles ? 40 : 64}
              />
            </div>
          {/if}
          <span class="ring" aria-hidden="true"></span>
          {#if t.speaking}
            <span class="eq" aria-hidden="true"><span></span><span></span><span></span></span>
          {/if}
          {#if t.deafened}
            <span class="mute-badge" use:tooltip={"Deafened — can't hear the call"} aria-label="Deafened">
              <Icon name="deafened" size={11} />
            </span>
          {:else if t.muted}
            <span class="mute-badge" use:tooltip aria-label="Muted"><Icon name="micOff" size={11} /></span>
          {/if}
          {#if t.guest}
            <!-- Removing a guest acts on the WIRE, unlike the local mute beside
                 it which only affects this device. -->
            <button
              class="evict"
              use:tooltip
              aria-label="Remove {t.name} from the meeting"
              onclick={(e) => {
                e.stopPropagation();
                evictGuest(pid, t.name);
              }}
            >
              <Icon name="close" size={12} />
            </button>
          {/if}
          {#if !t.self}
            <!-- Silence this participant for YOU only (local). Stops the tile-focus
                 click from also firing. -->
            <button
              class="local-mute"
              class:on={t.localMuted}
              use:tooltip={{ text: t.localMuted ? `Unmute ${t.name} (for you)` : `Mute ${t.name} (for you)` }}
              aria-label={t.localMuted ? `Unmute ${t.name} for yourself` : `Mute ${t.name} for yourself`}
              aria-pressed={t.localMuted}
              onclick={(e) => {
                e.stopPropagation();
                togglePeerMute(pid);
              }}
            >
              <Icon name={t.localMuted ? "deafened" : "speaker"} size={12} />
            </button>
          {/if}
          {#if t.status}
            <!-- What this connection is doing. A tile that says nothing is a
                 tile that is working; anything else says so in words. -->
            <span class="conn" class:bad={t.status.state === "failed" || t.status.state === "quiet"}>
              {t.status.label}
              {#if t.status.state === "failed"}
                <button
                  class="conn-retry"
                  onclick={(e) => {
                    e.stopPropagation();
                    retryPeer(pid);
                  }}
                >
                  Retry
                </button>
              {/if}
            </span>
          {/if}
          <span class="name">{t.self ? `${t.name} (you)` : t.name}</span>
        </div>
      {/each}
    </div>
    {#if roomLine}
      <!-- Everybody is on the stage and nobody is through. Said once, for the
           room: four tiles each saying "Connecting…" is four copies of one
           fact, and the fact is about the call. -->
      <p class="room-stuck" role="status">{roomLine}</p>
    {/if}

    {#if screens.length}
      <div class="screens">
        {#each screens as tile (tile.key)}
          <!-- The touch wording is dropped deliberately: use:tooltip bails out on
               coarse pointers, so a "Tap to zoom" branch could never render. -->
          <button
            class="screen-tile"
            use:tooltip={"Click to zoom"}
            onclick={() => toggleFocus(tile.key)}
          >
            <!-- svelte-ignore a11y_media_has_caption -->
            <video use:srcObject={tile.key} autoplay playsinline muted></video>
            <span class="screen-label">
              <Icon name="screen" size={12} />
              {screenTitle(tile)} · {S.isMobile ? "tap" : "click"} to zoom
            </span>
          </button>
        {/each}
      </div>
    {/if}
  {/if}

  {#if sfxOpen}
    <!-- keySurface: while the board is open it OWNS the digits. Without that
         the composer's type-to-focus handler ran after this one — preventDefault
         does not stop propagation — took the caret on the very first press, and
         from the second digit on the board's own guard ("not while a text box
         has focus") bailed correctly on a text box nobody had clicked into. One
         airhorn, then "234" in your message, with the board still on screen
         still saying "press 1–6". -->
    <div class="sfx-board" role="group" aria-label="Soundboard" bind:this={boardEl} use:keySurface>
      <div class="sfx-cap">
        Concord
        <span class="sfx-hint">press 1–{SOUNDBOARD.length}</span>
      </div>
      <div class="sfx-grid">
        {#each SOUNDBOARD as s, i (s.id)}
          <button
            class="sfx"
            class:cool={sfxCooling === `b:${s.id}`}
            aria-disabled={cooling}
            aria-label={s.name}
            aria-keyshortcuts={String(i + 1)}
            onclick={() => pressSfx(s.id)}
          >
            <span class="sfx-glyph">{s.emoji}</span>
            <span class="sfx-name">{s.name}</span>
            <span class="sfx-key" aria-hidden="true">{i + 1}</span>
          </button>
        {/each}
      </div>
      <div class="sfx-cap">Yours</div>
      <div class="sfx-grid">
        {#each mySounds as s (s.payload)}
          <button
            class="sfx mine"
            class:cool={sfxCooling === `m:${s.payload}`}
            aria-disabled={cooling}
            aria-label={s.recipe.name}
            onclick={() => pressMine(s)}
          >
            <span class="sfx-glyph">{recipeGlyph(s.recipe)}</span>
            <span class="sfx-name">{s.recipe.name}</span>
          </button>
        {/each}
        <button
          class="sfx add"
          aria-label="Sounds you made"
          onclick={() => (S.modal = { kind: "soundboard", onPick: (payload, recipe) => pressMine({ payload, recipe }) })}
        >
          <span class="sfx-glyph"><Icon name="plus" size={16} /></span>
          <span class="sfx-name">Make one</span>
        </button>
      </div>
    </div>
  {/if}
  <!-- No controls until there is a call to control. The panel is up from the
       click so the join has something to show for itself, but mute and hang-up
       reach through S.voice, which does not exist yet — eight live-looking
       buttons that all quietly do nothing is worse than eight that aren't there
       for the moment it takes. -->
  <!-- Quiet, not a toast. The toast said it once at join and scrolled away; a
       call you are sitting in with no voice has to keep saying so, because
       nothing else on this panel would tell you that the room cannot hear
       you. -->
  {#if S.micError && !joining}
    <div class="nomic-note" role="status">
      <Icon name="micOff" size={13} />
      <span>Listening only — no microphone.</span>
      <button class="nomic-retry" disabled={S.micRetrying} onclick={retryMic}>
        {S.micRetrying ? "Trying…" : "Try again"}
      </button>
    </div>
  {/if}
  <div class="controls" class:hidden={joining} class:phone={S.isMobile} inert={joining}>
    <!-- Three states, not two. A mute you chose and a microphone that never
         opened look identical on a two-state toggle, so the button that meant
         "unmute" would have meant "nothing happens" for the whole of a
         listen-only call. The unavailable state says so, and its click is the
         retry rather than a toggle of something that isn't there. -->
    <button
      class="ctl"
      class:danger={S.muted && !S.micError}
      class:nomic={!!S.micError}
      class:busy={S.micRetrying}
      class:keyed={pttOn && !S.muted && !S.micError}
      class:talking={S.talking && !S.muted && !S.micError}
      use:tooltip={{
        text: S.micError
          ? S.micRetrying
            ? "Asking for the microphone…"
            : "No microphone — click to try again"
          : S.muted
            ? "Unmute"
            : pttOn
              ? `Hold ${pttKey || "your push-to-talk key"} to talk`
              : "Mute",
      }}
      aria-label={S.micError ? "Microphone unavailable — try again" : S.muted ? "Unmute" : "Mute"}
      aria-pressed={S.micError ? undefined : S.muted}
      disabled={S.micRetrying}
      onclick={withHaptic(S.micError ? retryMic : onToggleMute)}
    >
      <Icon name={S.micError || S.muted ? "micOff" : "mic"} size={18} />
    </button>
    <button
      class="ctl"
      class:danger={S.deafened}
      use:tooltip
      aria-label={S.deafened ? "Undeafen" : "Deafen"}
      aria-pressed={S.deafened}
      onclick={withHaptic(onToggleDeafen)}
    >
      <Icon name={S.deafened ? "deafened" : "speaker"} size={18} />
    </button>
    <button
      class="ctl"
      class:active={S.cameraOn}
      use:tooltip
      aria-label={S.cameraOn ? "Turn off camera" : "Turn on camera"}
      aria-pressed={S.cameraOn}
      onclick={withHaptic(onToggleCamera)}
    >
      <Icon name={S.cameraOn ? "cameraOff" : "camera"} size={18} />
    </button>
    {#if S.isMobile}
      <!-- Eight 48px controls do not fit a 390px phone, and wrapping them left
           the red hang-up button orphaned in the middle of a second, centred
           row — the one control that must never move, moving. So the phone gets
           the four you reach for mid-call, hang-up pinned to its own end of the
           bar, and everything else in the action sheet this app already uses
           for every other set of secondary actions. -->
      <button class="ctl" use:tooltip aria-label="More call controls" onclick={moreControls}>
        <Icon name="dots" size={18} />
      </button>
    {:else}
      <button
        class="ctl"
        class:active={sfxOpen}
        use:tooltip
        aria-label="Soundboard"
        aria-expanded={sfxOpen}
        onclick={() => (sfxOpen = !sfxOpen)}
      >
        <Icon name="megaphone" size={18} />
      </button>
      {#if canRoute}
        <button
          class="ctl"
          class:active={route !== "earpiece"}
          use:tooltip={{ text: `Audio out: ${routeInfo.label} — tap to change` }}
          aria-label="Audio output: {routeInfo.label}"
          onclick={cycleRoute}
        >
          <Icon name={routeInfo.icon} size={18} />
        </button>
      {/if}
      {#if S.cameraOn && cameras > 1}
        <button
          class="ctl"
          use:tooltip={"Switch camera"}
          aria-label="Switch to the {facing === 'environment' ? 'front' : 'back'} camera"
          onclick={flipCamera}
        >
          <Icon name="forward" size={18} />
        </button>
      {/if}
      {#if shareable}
        <button
          class="ctl"
          class:active={S.sharing}
          use:tooltip
          aria-label={S.sharing ? "Stop sharing" : "Share screen"}
          aria-pressed={S.sharing}
          onclick={withHaptic(onToggleShare)}
        >
          <Icon name={S.sharing ? "screenOff" : "screen"} size={18} />
        </button>
      {/if}
      {#if canLock}
        <button
          class="ctl"
          class:active={locked}
          use:tooltip={{ text: locked ? "Unlock call (anyone can join)" : "Lock call (people must knock)" }}
          aria-label={locked ? "Unlock call" : "Lock call"}
          aria-pressed={locked}
          onclick={withHaptic(toggleCallLock)}
        >
          <Icon name="lock" size={18} />
        </button>
      {/if}
      <button
        class="ctl"
        use:tooltip={"Audio & video settings"}
        aria-label="Audio and video settings"
        onclick={() => (S.modal = { kind: "devices" })}
      >
        <Icon name="gear" size={18} />
      </button>
    {/if}
    <button
      class="ctl hangup"
      use:tooltip
      aria-label="Leave call"
      onclick={withHaptic(onLeaveVoice, "heavy")}
    >
      <Icon name="door" size={18} />
    </button>
  </div>

  {#if knockers.length}
    <div class="knocks">
      <!-- The caption survives stacking: one knocker or five, the section reads
           as ONE doorway, not a pile of unrelated alerts. -->
      <div class="knocks-cap" transition:scale={pop}>
        <Icon name="door" size={12} />
        <span>At the door{knockers.length > 1 ? ` · ${knockers.length}` : ""}</span>
      </div>
      {#each knockers as fpr (fpr)}
        {@const who = knockerInfo(fpr)}
        <div
          class="knock"
          in:knockSlot
          out:knockSlot
          animate:flip={{ duration: noMotion ? 0 : 220 }}
        >
          <!-- The face raps and the rings ripple on the same 2.4s clock: a
               literal knock-knock, so "pending" reads without any copy. -->
          <span class="knock-ava" aria-hidden="true">
            <span class="knock-ring"></span>
            <span class="knock-ring r2"></span>
            <span class="knock-rap">
              <Avatar name={who.name} emoji={who.emoji} color={who.color} image={who.image} size={36} />
            </span>
          </span>
          <span class="knock-id">
            <span class="knock-who">{who.name}</span>
            <span class="knock-sub">
              {isGuestFpr(fpr) ? "knocking with an invite link" : "knocking to join"}
            </span>
          </span>
          <!-- The two answers live in their own row on a phone: this is the
               decision that lets a stranger holding a public link into the call,
               and they used to be a 26px pair a finger-width apart. -->
          <div class="knock-acts">
            <button class="knock-admit" onclick={withHaptic(() => admitKnocker(chId, fpr), "medium")}>
              <Icon name="door" size={15} />
              Admit
            </button>
            <!-- A guest is told they were turned away (they're sitting on an open
                 socket); a member's knock is simply ignored. -->
            <button
              class="knock-deny"
              onclick={withHaptic(() => denyKnocker(chId, fpr), "medium")}
              use:tooltip
              aria-label={isGuestFpr(fpr) ? "Refuse" : "Ignore"}
            >
              <Icon name="close" size={14} />
            </button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .voice-panel {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: 14px 16px;
    background: radial-gradient(120% 140% at 50% 0%, var(--bg-2), var(--bg-0));
    border-bottom: 1px solid var(--border);
    max-height: calc(46 * var(--vh));
    overflow-y: auto;
    /* Flicking past the end of the tile grid used to hand the leftover momentum
       to the message feed underneath — the chat jumped while you were reaching
       for a participant. */
    overscroll-behavior: contain;
  }
  /* ---- the call takes the stage ----
     `.on-stage` grows the panel into the column and the sibling chat pane
     shrinks to a strip. Reaching sideways out of a scoped stylesheet is worth
     one comment: `.pane-body` is this panel's next sibling in both shells, and
     the alternative — a class on the parent, set from two different call
     sites — would put half of one layout decision in a file that knows nothing
     about calls. */
  .voice-panel.on-stage {
    flex: 1 1 auto;
    min-height: 0;
    max-height: none;
  }
  .voice-panel.on-stage + :global(.pane-body) {
    flex: 0 0 var(--chat-strip, 168px);
    min-height: 0;
  }
  .voice-panel.on-stage .stage {
    flex: 1;
    min-height: 0;
    align-content: center;
  }
  /* Header of the stage. */
  .stage-head {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-wrap: wrap;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    /* Above the door cards: the header names the room those knocks are at. */
    order: -2;
  }
  .sh-live {
    width: 8px;
    height: 8px;
    flex: none;
    border-radius: 50%;
    background: var(--ok);
    animation: blink 1.4s ease-in-out infinite;
  }
  @keyframes blink {
    50% {
      opacity: 0.3;
    }
  }
  .sh-room {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 650;
    color: var(--text);
  }
  .sh-count {
    flex: none;
  }
  /* A persistent pill, not a toast that expires: locking is a STATE, and while
     it holds everyone outside has to knock. Said in words on the stage, where
     the person who locked it is looking. */
  .sh-lock {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    flex: none;
    padding: 2px 8px;
    border-radius: 999px;
    font-weight: 600;
    color: var(--warn-text);
    background: color-mix(in srgb, var(--warn) 16%, transparent);
    border: 1px solid color-mix(in srgb, var(--warn) 38%, transparent);
  }
  .sh-spacer {
    flex: 1;
  }
  /* On a phone the lock sentence is 203px of a 380px row, so the head wrapped
     and the call timer and the show-the-chat toggle dropped onto a second line
     HARD LEFT, with the flex spacer stranded at the end of the first one — the
     two controls that are always in the same corner moved the moment the door
     was locked. Give the pill a line of its own, last, and the head keeps the
     exact layout it has when the call is open; the sentence simply sits under
     it. Measured at 412px on the device: three items, then the pill. */
  @media (pointer: coarse), (max-width: 768px) {
    .sh-lock {
      order: 2;
      flex-basis: 100%;
      justify-content: center;
    }
  }
  .sh-clock {
    flex: none;
    font-variant-numeric: tabular-nums;
    font-weight: 600;
    color: var(--text);
  }
  .sh-swap {
    display: grid;
    place-items: center;
    flex: none;
    width: 26px;
    height: 26px;
    padding: 0;
    border-radius: var(--radius-sm);
    color: var(--text-muted);
    background: transparent;
  }
  .sh-swap:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  @media (prefers-reduced-motion: reduce) {
    .sh-live {
      animation: none;
    }
  }
  .stage {
    display: grid;
    /* Reflowing tiles, but CAPPED so a wide window doesn't blow them up into a
       scrolling monster: each tile is at most 240px wide, centered. */
    grid-template-columns: repeat(auto-fit, minmax(130px, 200px));
    justify-content: center;
    gap: var(--sp-3);
  }
  /* On the stage the grid is a meeting grid: `--cols` columns of equal width,
     rows that fill, and tiles shaped by the space rather than by a 4:3 rule
     written for a 230px band. Capped at 1400px so a 2560px monitor doesn't
     draw two enormous rectangles. */
  .voice-panel.on-stage .stage,
  .voice-panel.on-stage .stage.solo {
    grid-template-columns: repeat(var(--cols, 2), minmax(0, 1fr));
    grid-auto-rows: minmax(120px, 1fr);
    width: 100%;
    /* Grows with the room, not with the monitor: one person gets a tile, not a
       wall. 1400px is where a 16:9 tile stops gaining anything from more pixels
       and starts just being far away. */
    max-width: min(1400px, calc(var(--cols, 2) * 620px));
    margin-inline: auto;
  }
  .voice-panel.on-stage .tile {
    aspect-ratio: auto;
  }
  /* The measured fit (lib/tilefit.js). Wrapping flex rather than grid tracks, so
     a last row with fewer tiles than the others is centred under them instead
     of hanging off the left edge. The row is pinned to the width `c` tiles
     actually need, which is what makes the wrap land where the fit assumed. */
  .voice-panel.on-stage .stage.fitted,
  .voice-panel.on-stage .stage.fitted.solo {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    align-content: center;
    gap: var(--sp-3);
    width: var(--row-w);
    max-width: 100%;
    margin-inline: auto;
  }
  .voice-panel.on-stage .stage.fitted .tile {
    flex: 0 0 var(--tile-w);
    width: var(--tile-w);
    height: var(--tile-h);
  }
  /* On a phone the app is one pane at a time, so "the call takes the stage"
     means it takes the pane: the chat goes away entirely and comes back with
     the header's toggle. A strip cannot be the answer here — the composer's
     send button would sit a finger-width under a red hang-up button, which is
     precisely the adjacency the phone control bar exists to prevent. */
  @media (pointer: coarse), (max-width: 768px) {
    .voice-panel.on-stage + :global(.pane-body) {
      display: none;
    }
  }
  /* Theater sets its own (taller) cap; on the stage there is no cap at all, and
     the two rules have equal specificity, so say which wins rather than relying
     on where they sit in the file. */
  .voice-panel.on-stage.theater {
    max-height: none;
  }
  .voice-panel.on-stage .focus-main {
    flex: 1;
    min-height: 0;
    max-height: none;
    aspect-ratio: auto;
  }
  /* A share on the stage is the thing you are looking at; give it the height
     the tiles would have had. */
  .voice-panel.on-stage .screens {
    flex: 1;
    min-height: 0;
    grid-auto-rows: minmax(0, 1fr);
  }
  .voice-panel.on-stage .screen-tile {
    aspect-ratio: auto;
  }
  .stage.solo {
    grid-template-columns: minmax(180px, 260px);
    justify-content: center;
  }
  .tile {
    position: relative;
    /* 4:3 rather than 1:1 — shorter, so rows fit the panel without scrolling. */
    aspect-ratio: 4 / 3;
    border-radius: var(--radius-lg);
    overflow: hidden;
    background: var(--bg-1);
    border: 1px solid var(--border);
    display: grid;
    place-items: center;
    cursor: pointer;
    transition:
      transform var(--dur-standard) ease,
      border-color var(--dur-standard) ease,
      box-shadow var(--dur-standard) ease;
  }
  /* Hover lift — a gentle rise + shadow so tiles feel tactile. */
  .tile:hover {
    border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
    transform: translateY(-2px);
    box-shadow: 0 8px 20px -8px rgba(0, 0, 0, 0.45);
  }
  .tile.speaking {
    border-color: color-mix(in srgb, var(--ok) 55%, transparent);
  }
  .tile video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .tile video.mirror {
    transform: scaleX(-1);
  }
  /* Idle tile face: soft member-color gradient — a top glow melting into a
     faint tinted wash, so every tile carries its owner's color. */
  .face {
    width: 100%;
    height: 100%;
    display: grid;
    place-items: center;
    background:
      radial-gradient(
        120% 95% at 50% 0%,
        color-mix(in srgb, var(--tint, var(--accent)) 30%, transparent),
        transparent 62%
      ),
      linear-gradient(180deg, color-mix(in srgb, var(--tint, var(--accent)) 10%, var(--bg-1)), var(--bg-1) 80%);
  }
  /* Animated speaking ring — layered: a crisp inner ring, an inner wash of
     color, and a breathing outer halo. */
  .ring {
    position: absolute;
    inset: 0;
    border-radius: inherit;
    pointer-events: none;
    box-shadow: inset 0 0 0 0 transparent;
    transition: box-shadow var(--dur-quick) ease;
  }
  .tile.speaking .ring {
    animation: pulse 1.6s ease-in-out infinite;
  }
  @keyframes pulse {
    0%,
    100% {
      box-shadow:
        inset 0 0 0 2px var(--ok),
        inset 0 0 22px -6px color-mix(in srgb, var(--ok) 40%, transparent),
        0 0 0 1px color-mix(in srgb, var(--ok) 35%, transparent),
        0 0 6px 0 color-mix(in srgb, var(--ok) 25%, transparent);
    }
    50% {
      box-shadow:
        inset 0 0 0 2px var(--ok),
        inset 0 0 30px -4px color-mix(in srgb, var(--ok) 52%, transparent),
        0 0 0 3px color-mix(in srgb, var(--ok) 45%, transparent),
        0 0 18px 4px color-mix(in srgb, var(--ok) 40%, transparent);
    }
  }
  /* Tiny equalizer badge — three bars bouncing while someone speaks. */
  .eq {
    position: absolute;
    top: 8px;
    right: 8px;
    display: inline-flex;
    align-items: flex-end;
    gap: 2px;
    height: 12px;
    padding: 4px 5px;
    background: rgba(0, 0, 0, 0.55);
    border-radius: var(--radius-sm);
    pointer-events: none;
  }
  .eq span {
    width: 3px;
    height: 35%;
    border-radius: 2px;
    background: var(--ok);
    animation: eq-bounce 0.9s ease-in-out infinite;
  }
  .eq span:nth-child(2) {
    animation-delay: 0.18s;
  }
  .eq span:nth-child(3) {
    animation-delay: 0.36s;
  }
  @keyframes eq-bounce {
    0%,
    100% {
      height: 35%;
    }
    50% {
      height: 100%;
    }
  }
  /* Muted-mic badge (shown for yourself — the only mute state we truly know). */
  .mute-badge {
    position: absolute;
    right: 8px;
    bottom: 8px;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: #fff;
    background: color-mix(in srgb, var(--danger) 82%, #000);
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.4);
    pointer-events: none;
  }
  /* Remove-guest sits top-LEFT, opposite the local mute, so a destructive
     control is never where a harmless one was a moment ago. */
  .evict {
    position: absolute;
    top: 8px;
    left: 8px;
    width: 22px;
    height: 22px;
    display: grid;
    place-items: center;
    border: none;
    border-radius: 50%;
    background: rgba(0, 0, 0, 0.55);
    color: #fff;
    opacity: 0;
    transition:
      opacity var(--dur-quick),
      background var(--dur-quick);
  }
  .tile:hover .evict,
  .evict:focus-visible {
    opacity: 1;
  }
  .evict:hover {
    background: var(--danger, #d9534f);
  }
  /* No hover to reveal it with, and no room to grow it either: a 22px button
     that disconnects someone, inset into a tile whose whole body focuses that
     person, is a landmine under a thumb. It moves to the long-press sheet
     (tileMenu), which is where every other destructive action on this app's
     phone UI already lives. */
  @media (pointer: coarse), (max-width: 768px) {
    .evict {
      display: none;
    }
  }
  /* Per-participant LOCAL mute: a small control top-right, revealed on tile
     hover (always shown once engaged so you can undo it). */
  .local-mute {
    position: absolute;
    top: 8px;
    right: 8px;
    width: 26px;
    height: 26px;
    padding: 0; /* else the global button padding squishes the icon off-center */
    border-radius: 50%;
    display: grid;
    place-items: center;
    color: #fff;
    background: rgba(0, 0, 0, 0.5);
    opacity: 0;
    transition: opacity var(--dur-quick) ease, background var(--dur-quick) ease;
  }
  .tile:hover .local-mute,
  .local-mute:focus-visible {
    opacity: 1;
  }
  .local-mute:hover {
    background: rgba(0, 0, 0, 0.72);
  }
  .local-mute.on {
    opacity: 1;
    background: color-mix(in srgb, var(--danger) 82%, #000);
  }
  /* Same story as .evict — except muting someone for yourself is worth SEEING,
     so on touch the control stops being a button and becomes the badge for that
     state: shown only while engaged, and not hit-testable, so it can't steal the
     tap meant for the tile. Both directions live in the long-press sheet. */
  @media (pointer: coarse), (max-width: 768px) {
    .local-mute {
      opacity: 0;
      pointer-events: none;
    }
    .local-mute.on {
      opacity: 1;
    }
  }
  .name {
    position: absolute;
    left: 8px;
    bottom: 8px;
    /* Block, not inline-flex: text-overflow is ignored on a flex container, so
       a long name was hard-clipped mid-glyph instead of ellipsised. */
    display: block;
    max-width: calc(100% - 16px);
    padding: 2px 8px;
    font-size: var(--fs-compact);
    font-weight: 600;
    color: #fff;
    background: rgba(0, 0, 0, 0.55);
    border-radius: var(--radius-sm);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  /* ---- connection state ----
     A tile that is fine says nothing. Everything below is a tile that isn't. */

  /* Not through yet: the face recedes so the words on top of it are what you
     read, and a slow pulse says the app is still trying. */
  .tile.unsettled .face,
  .tile.unsettled video {
    opacity: 0.45;
  }
  .tile.st-connecting,
  .tile.st-reconnecting {
    animation: tile-pulse 1.6s ease-in-out infinite;
  }
  .tile.st-reconnecting {
    border-color: var(--warn);
  }
  .tile.st-failed,
  .tile.st-quiet {
    border-color: var(--danger);
    animation: none;
  }
  @keyframes tile-pulse {
    50% {
      border-color: var(--accent);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .tile.st-connecting,
    .tile.st-reconnecting {
      animation: none;
      border-color: var(--accent);
    }
  }
  /* The word itself, centered over the tile — where the eye already is. */
  .conn {
    position: absolute;
    left: 6px;
    right: 6px;
    top: 50%;
    transform: translateY(-50%);
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    align-items: center;
    gap: 6px;
    padding: 3px 6px;
    font-size: var(--fs-compact);
    font-weight: 600;
    text-align: center;
    color: #fff;
    background: rgba(0, 0, 0, 0.6);
    border-radius: var(--radius-sm);
  }
  .conn.bad {
    color: color-mix(in srgb, var(--warn) 60%, #fff);
  }
  .conn-retry {
    padding: 2px 8px;
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: var(--radius-sm);
    font-size: var(--fs-tiny);
    font-weight: 600;
  }
  .conn-retry:hover {
    background: var(--accent-hover);
  }
  /* An identity that hasn't resolved: no name to show, so show none — and let
     the tile shimmer rather than sit there looking broken. */
  .tile.pending .name {
    color: var(--text-muted);
    background: rgba(0, 0, 0, 0.35);
  }
  .room-stuck {
    margin: 0;
    text-align: center;
    font-size: var(--fs-compact);
    color: var(--warn-text);
  }
  .screens {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 320px));
    justify-content: center;
    gap: 10px;
  }
  .screen-tile {
    position: relative;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: #000;
    aspect-ratio: 16 / 9;
    padding: 0;
    border: 1px solid var(--border);
    cursor: pointer;
  }
  .screen-tile:hover {
    border-color: var(--accent);
  }
  /* Theater / focus mode: the panel gets a bit taller for a usable big view. */
  .voice-panel.theater {
    max-height: calc(62 * var(--vh));
  }
  .focus-main {
    position: relative;
    background: #000;
    border-radius: var(--radius-md);
    overflow: hidden;
    aspect-ratio: 16 / 9;
    max-height: calc(46 * var(--vh));
    margin: 0 auto;
    width: 100%;
  }
  /* Speaking glow on the big focused view (participant focus only). */
  .focus-main.speaking {
    animation: pulse 1.6s ease-in-out infinite;
  }
  /* The eq badge would collide with the fullscreen/close buttons top-right, so
     it sits top-left in the big view. */
  .focus-main .eq {
    right: auto;
    left: 8px;
  }
  .focus-main .mute-badge {
    right: 8px;
    bottom: 8px;
  }
  .focus-main video {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }
  .focus-face {
    width: 100%;
    height: 100%;
    display: grid;
    place-items: center;
    background:
      radial-gradient(
        90% 80% at 50% 20%,
        color-mix(in srgb, var(--tint, var(--accent)) 28%, transparent),
        transparent 65%
      ),
      linear-gradient(180deg, color-mix(in srgb, var(--tint, var(--accent)) 10%, var(--bg-1)), var(--bg-1) 80%);
  }
  .focus-actions {
    position: absolute;
    top: 8px;
    right: 8px;
    display: flex;
    gap: 6px;
  }
  .fbtn {
    width: 30px;
    height: 30px;
    padding: 0;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: rgba(0, 0, 0, 0.55);
    color: #fff;
    border: none;
  }
  .fbtn:hover {
    background: rgba(0, 0, 0, 0.8);
  }
  .strip {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    align-items: center;
    gap: var(--sp-2);
  }
  .strip .thumb {
    position: relative;
    width: 84px;
    height: 48px;
    padding: 0;
    border-radius: var(--radius-sm);
    overflow: hidden;
    background: #000;
    border: 1px solid var(--border);
    cursor: pointer;
    flex-shrink: 0;
  }
  .strip .thumb:hover {
    border-color: var(--accent);
  }
  .strip .thumb video {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .thumb-badge {
    position: absolute;
    left: 3px;
    bottom: 3px;
    color: #fff;
    background: rgba(0, 0, 0, 0.55);
    border-radius: 3px;
    padding: 1px 3px;
    display: grid;
    place-items: center;
  }
  .bubble {
    position: relative;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 2px 10px 2px 2px;
    border-radius: 999px;
    border: 2px solid transparent;
    background: color-mix(in srgb, var(--bg-1) 82%, transparent);
    /* Same trap, same fix — the strip's name chips were the fifth surface. */
    color: var(--text);
    transition:
      transform var(--dur-quick) ease,
      border-color var(--dur-quick) ease;
  }
  .bub-name {
    max-width: 110px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-compact);
    font-weight: 600;
  }
  .bub-mark {
    display: grid;
    place-items: center;
    flex: none;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    color: #fff;
    background: color-mix(in srgb, var(--danger) 82%, #000);
  }
  .bubble:hover {
    transform: translateY(-1px);
  }
  .bubble.speaking {
    border-color: var(--ok);
    animation: bubble-glow 1.6s ease-in-out infinite;
  }
  @keyframes bubble-glow {
    0%,
    100% {
      box-shadow: 0 0 0 0 color-mix(in srgb, var(--ok) 40%, transparent);
    }
    50% {
      box-shadow: 0 0 10px 3px color-mix(in srgb, var(--ok) 45%, transparent);
    }
  }
  .screen-tile video {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }
  .screen-label {
    position: absolute;
    left: 8px;
    bottom: 8px;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 2px 8px;
    font-size: var(--fs-compact);
    color: #fff;
    background: rgba(0, 0, 0, 0.55);
    border-radius: var(--radius-sm);
  }
  /* Ringing / waiting: a soft pill that breathes in, dots doing the talking. */
  .ringing {
    align-self: center;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 6px var(--sp-4);
    font-size: var(--fs-ui);
    color: var(--text-muted);
    background: color-mix(in srgb, var(--bg-1) 80%, transparent);
    border: 1px solid var(--border);
    border-radius: 999px;
    animation: ring-in 0.35s ease both;
  }
  @keyframes ring-in {
    from {
      opacity: 0;
      transform: translateY(-4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  .dots {
    display: inline-flex;
    gap: 3px;
  }
  .dots span {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--ok);
    animation: dot 1.2s ease-in-out infinite;
  }
  .dots span:nth-child(2) {
    animation-delay: 0.2s;
  }
  .dots span:nth-child(3) {
    animation-delay: 0.4s;
  }
  @keyframes dot {
    0%,
    100% {
      opacity: 0.25;
      transform: translateY(0);
    }
    50% {
      opacity: 1;
      transform: translateY(-3px);
    }
  }
  /* Call controls, on the call box itself. Sticky to the bottom
     of the (scrollable) panel so mute/leave are always reachable, never scrolled
     off when there are many tiles or a big screen share. */
  .sfx-board {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    width: 100%;
    max-width: 640px;
    margin: 0 auto var(--sp-2);
  }
  /* "Concord" and "Yours" as real headings rather than a hairline on one
     button: a strip of fourteen anonymous faces reads as one undifferentiated
     pile, and which six everybody in the room also has is worth knowing. */
  .sfx-cap {
    display: flex;
    align-items: baseline;
    gap: var(--sp-2);
    padding: 0 2px;
    font-size: var(--fs-micro);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .sfx-hint {
    margin-left: auto;
    letter-spacing: 0;
    text-transform: none;
    font-weight: 500;
  }
  .sfx-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(104px, 1fr));
    gap: 6px;
  }
  .sfx {
    position: relative;
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 36px;
    padding: 5px 8px;
    overflow: hidden;
    text-align: left;
    border-radius: var(--radius-md);
    background: var(--bg-2);
    /* Both halves, always. A button that repaints its ground and says nothing
       about its ink gets --accent-fg from app.css — near-black, which on this
       charcoal chip is 1.12:1. Fourteen sound names were invisible in the dark
       theme, and the only one that could be read ("Make one") was the one chip
       that had happened to declare a colour. tokens.test.mjs rule 10 now
       measures this pair on every button in the app. */
    color: var(--text);
    border: 1px solid var(--border);
    transition: transform 0.1s ease, opacity var(--dur-quick) ease;
  }
  .sfx-glyph {
    display: grid;
    place-items: center;
    flex: none;
    width: 22px;
    font-size: var(--fs-title);
    line-height: 1;
  }
  .sfx-name {
    min-width: 0;
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-compact);
  }
  .sfx-key {
    flex: none;
    padding: 0 4px;
    font-size: var(--fs-micro);
    font-variant-numeric: tabular-nums;
    color: var(--text-faint);
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  @media (pointer: fine) {
    .sfx:not([aria-disabled="true"]):hover {
      background: color-mix(in srgb, var(--accent) 14%, transparent);
      border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
    }
  }
  .sfx:active {
    transform: scale(0.97);
  }
  /* Your own sounds carry an accent hairline as well as their own heading. */
  .sfx.mine {
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  }
  .sfx.add {
    color: var(--text-muted);
  }
  /* The rate limit, drawn. A press inside the window is refused, and it used to
     be refused in complete silence — indistinguishable from a button that does
     not work. Now the one you pressed sweeps its own second back. */
  .sfx[aria-disabled="true"] {
    opacity: 0.45;
    cursor: default;
  }
  .sfx.cool {
    opacity: 1;
    border-color: color-mix(in srgb, var(--accent) 55%, var(--border));
  }
  .sfx.cool::after {
    content: "";
    position: absolute;
    inset: 0;
    background: color-mix(in srgb, var(--accent) 22%, transparent);
    transform-origin: left;
    animation: sfx-cool 1s linear forwards;
    pointer-events: none;
  }
  @keyframes sfx-cool {
    from {
      transform: scaleX(1);
    }
    to {
      transform: scaleX(0);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .sfx {
      transition: none;
    }
    /* Still says "wait" — it just holds the wash instead of sweeping it. */
    .sfx.cool::after {
      animation: none;
    }
  }
  .controls {
    position: sticky;
    bottom: 0;
    display: flex;
    justify-content: center;
    gap: 10px;
    padding: 8px 0 2px;
    background: linear-gradient(to top, var(--bg-0) 55%, transparent);
    z-index: 1;
  }
  /* Held open rather than removed, so the panel doesn't jump when the call
     lands a moment later. */
  .controls.hidden {
    visibility: hidden;
    pointer-events: none;
  }
  .knocks {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: var(--sp-1) var(--sp-2) var(--sp-2);
    /* First thing in the panel, every size. As the panel's last DOM child it
       rendered below the sticky controls — a scrollable 46vh panel hid the one
       decision the lock exists for under the fold. */
    order: -1;
  }
  .knocks-cap {
    display: flex;
    align-items: center;
    gap: 6px;
    /* Caption and cards share the stage's centered column — a 1280px window
       shouldn't stretch one name and two buttons across the whole panel. */
    width: 100%;
    max-width: 640px;
    margin-inline: auto;
    padding: 0 2px;
    font-size: var(--fs-micro);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .knock {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    max-width: 640px;
    margin-inline: auto;
    /* padding-block 10px is load-bearing: knockSlot animates it in JS, so the
       two values must agree or the card jumps at the end of the transition. */
    padding: 10px 12px;
    /* Accent-lit from the corner the avatar sits in, fading to the panel's own
       surface — a doorway with light spilling through, not a solid alert bar. */
    background: linear-gradient(
      115deg,
      color-mix(in srgb, var(--accent) 17%, var(--bg-1)),
      var(--bg-1) 70%
    );
    border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
    border-radius: var(--radius-lg);
    box-shadow: 0 8px 22px -12px color-mix(in srgb, var(--accent) 55%, transparent);
    font-size: var(--fs-ui);
  }
  .knock-ava {
    position: relative;
    flex-shrink: 0;
    display: grid;
    place-items: center;
  }
  /* Sonar rings: two quick ripples then a rest — the rhythm of a real knock,
     not a metronome. Ring 2 trails ring 1 by a beat. */
  .knock-ring {
    position: absolute;
    inset: -3px;
    border-radius: 50%;
    border: 2px solid var(--accent);
    opacity: 0;
    pointer-events: none;
    animation: knock-ping 2.4s ease-out infinite;
  }
  .knock-ring.r2 {
    animation-delay: 0.3s;
  }
  @keyframes knock-ping {
    0% {
      transform: scale(0.75);
      opacity: 0.65;
    }
    45%,
    100% {
      transform: scale(1.45);
      opacity: 0;
    }
  }
  .knock-rap {
    display: grid;
    animation: knock-rap 2.4s var(--ease-calm) infinite;
  }
  @keyframes knock-rap {
    0%,
    16%,
    100% {
      transform: none;
    }
    5% {
      transform: rotate(-7deg) translateX(-1px);
    }
    11% {
      transform: rotate(5deg) translateX(1px);
    }
  }
  .knock-id {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .knock-who {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 650;
  }
  .knock-sub {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  .knock-acts {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-shrink: 0;
  }
  .knock-admit {
    flex-shrink: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 7px 14px;
    background: var(--accent);
    color: var(--accent-fg);
    border-radius: 999px;
    font-size: var(--fs-compact);
    font-weight: 700;
    transition:
      transform var(--dur-quick) ease,
      box-shadow var(--dur-standard) ease,
      background var(--dur-quick) ease;
  }
  .knock-admit:hover {
    background: var(--accent-hover);
    transform: translateY(-1px);
    box-shadow: var(--accent-glow);
  }
  .knock-admit:active {
    transform: scale(0.95);
  }
  .knock-deny {
    flex-shrink: 0;
    width: 30px;
    height: 30px;
    padding: 0; /* else the global button padding pushes the ✕ off-center */
    display: grid;
    place-items: center;
    border-radius: 50%;
    color: var(--text-muted);
    transition:
      background var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  .knock-deny:hover {
    background: color-mix(in srgb, var(--danger) 16%, transparent);
    color: var(--danger-text);
  }
  @media (prefers-reduced-motion: reduce) {
    .knock-ring {
      /* Pending still needs to read: one static soft halo instead of ripples. */
      animation: none;
      opacity: 0.3;
      transform: none;
    }
    .knock-ring.r2 {
      display: none;
    }
    .knock-rap {
      animation: none;
    }
  }
  .ctl {
    width: 44px;
    height: 44px;
    padding: 0;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: var(--bg-3);
    color: var(--text);
    border: 1px solid var(--border);
    transition:
      background var(--dur-quick) ease,
      color var(--dur-quick) ease,
      transform var(--dur-quick) ease,
      box-shadow var(--dur-standard) ease;
  }
  .ctl:hover {
    background: var(--bg-1);
    transform: translateY(-2px);
  }
  .ctl:active {
    transform: scale(0.9);
  }
  .ctl.hangup:hover {
    box-shadow: 0 4px 14px color-mix(in srgb, var(--danger) 45%, transparent);
  }
  .ctl.active {
    background: var(--accent-soft);
    color: var(--accent-hover);
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  }
  /* Toggles keep their tinted state on hover (a deeper wash) instead of falling
     back to the neutral hover grey — so an on/muted control still reads on/off. */
  .ctl.active:hover {
    background: color-mix(in srgb, var(--accent) 26%, transparent);
  }
  /* Push-to-talk: dimmed while the key is up (that IS the state — the mic is
     shut), accent-lit the moment it goes down. */
  .ctl.keyed {
    opacity: 0.65;
  }
  .ctl.keyed.talking {
    opacity: 1;
    background: var(--accent-soft);
    color: var(--text);
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
  }
  /* Muted or deafened is a FILLED danger state, the same one the dock, the
     sidebar bar and the header pill use. It was a 20% wash here, which on a
     44px circle beside four neutral ones is a tint you have to look for — and
     which meant "I am muted" was drawn four different ways in four places you
     can see at once. Camera and share stay accent-lit: they mean something is
     going out, which is not the same alarm. */
  .ctl.danger {
    background: var(--danger);
    color: var(--danger-fg);
    border-color: transparent;
  }
  .ctl.danger:hover {
    background: color-mix(in srgb, var(--danger) 85%, #000);
  }
  /* Not danger, and deliberately so: a missing microphone is a condition, not
     an alarm you set off. It reads as "off" — outlined, muted ink — which is
     also what tells it apart from the filled red of a mute you chose. */
  .ctl.nomic {
    background: transparent;
    color: var(--text-faint);
    border-style: dashed;
  }
  .ctl.nomic:hover {
    color: var(--text);
    border-style: solid;
  }
  .ctl.busy {
    opacity: 0.6;
  }
  .nomic-note {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    margin: 0 auto 8px;
    padding: 6px 10px;
    max-width: max-content;
    font-size: var(--fs-sm);
    color: var(--text-muted);
    background: var(--bg-2);
    border: 1px solid var(--border);
    border-radius: 999px;
  }
  .nomic-retry {
    padding: 2px 8px;
    font-size: var(--fs-sm);
    background: transparent;
    color: var(--accent);
    border: none;
    border-radius: var(--radius-sm);
  }
  .nomic-retry:hover:not(:disabled) {
    background: var(--bg-3);
  }
  /* Leave is a separate kind of action from the toggles — a little breathing
     room sets it apart so it's never fat-fingered mid-call. */
  .ctl.hangup {
    background: var(--danger);
    color: var(--danger-fg);
    border-color: transparent;
    margin-left: 6px;
  }
  .ctl.hangup:hover {
    background: color-mix(in srgb, var(--danger) 85%, #000);
  }
  @media (prefers-reduced-motion: reduce) {
    .tile.speaking .ring,
    .focus-main.speaking,
    .bubble.speaking,
    .eq span,
    .dots span,
    .ringing {
      animation: none;
    }
    /* Speaking still reads without motion: hold the ring's base frame. */
    .tile.speaking .ring {
      box-shadow: inset 0 0 0 2px var(--ok);
    }
    .ctl:hover,
    .tile:hover,
    .bubble:hover {
      transform: none;
    }
  }

  /* ---- touch adjustments: call controls you can't fat-finger. ---- */
  @media (pointer: coarse), (max-width: 768px) {
    /* On a phone the call IS the screen — 46vh is a desktop-derived slice that
       left two rows of tiles scrolling under an empty chat placeholder. */
    .voice-panel {
      max-height: calc(56 * var(--vh));
      /* The sticky control band supplies the bottom padding instead, so it can
         reach the panel's real bottom edge (sticky is caged by its containing
         block, which stops at the padding edge). */
      padding-bottom: 0;
    }
    /* The stage and the big view are the panel's reason to exist: in a squeezed
       panel (landscape, where the composer leaves it ~180px) they'd otherwise
       be flex-shrunk to a sliver rather than letting the panel scroll. */
    .stage,
    .focus-main {
      flex-shrink: 0;
    }
    /* The local-mute badge owns this corner whenever it's engaged, so the
       speaking equalizer would sit underneath it. */
    .tile .eq {
      right: auto;
      left: 8px;
    }
    /* A long press is how tile actions are reached here (tileMenu). Without
       this Android answers it with a text-selection handle over the name badge
       instead, and the sheet opens behind a selection the user then has to
       dismiss. */
    .tile {
      -webkit-user-select: none;
      user-select: none;
      -webkit-touch-callout: none;
    }
    /* order:-1 comes from the base tier; only the gutter changes here. */
    .knocks {
      padding: 0;
    }
    .controls {
      gap: var(--sp-2);
      /* Five 48px targets is 240px of button in 358px of content box at 390 —
         one row, with room, and no wrap for the hang-up button to be orphaned
         onto. (The overflow is in the ⋯ sheet; see moreControls.) */
      flex-wrap: nowrap;
      justify-content: space-between;
      /* Full-bleed and opaque. The gradient was transparent for its top 45%, so
         tiles and name badges rendered between the buttons, and the band
         stopped 16px short of the panel edges. */
      margin: 0 -16px;
      padding: 10px 16px calc(14px + var(--safe-bottom));
      background: var(--bg-0);
      border-top: 1px solid var(--border);
    }
    .ctl {
      width: 48px;
      height: 48px;
      flex-shrink: 0;
    }
    /* Hang-up is pinned to the end of the bar and separated from the toggles by
       whatever width is going spare, because it is the one control here that
       cannot be undone and it must be in the same place every single time. */
    .ctl.hangup {
      margin-left: auto;
    }
    /* A 16:9 share on a phone is height-limited by its width, and the only
       width left to give it is our own 16px gutters. */
    .focus-main {
      width: auto;
      margin-inline: -16px;
      border-radius: 0;
    }
    /* Exit-full-view and Fullscreen sit 6px apart in the same corner over a
       video, and one of them throws away the share you were reading. Both to
       the 44px floor, and pulled apart. */
    .fbtn {
      width: 44px;
      height: 44px;
    }
    .focus-actions {
      gap: var(--sp-3);
    }
    /* Width is the scarce resource for a shared desktop, and .screens was
       spending 40px of it on gutters either side of a 320px-capped tile — the
       same mistake .focus-main above was already fixed for. */
    .screens {
      grid-template-columns: 1fr;
      margin-inline: -16px;
      gap: var(--sp-2);
    }
    .screen-tile {
      border-radius: 0;
    }
    .strip .thumb {
      width: 96px;
      height: 56px;
    }
    .strip {
      gap: 10px;
    }
    /* Admit/Deny get their own full-width row at finger size. This is the
       moment that decides whether a stranger holding a public meeting link is
       in your call, and it was two sub-30px targets 8px apart at the very top
       of the panel, where a thumb has least control. */
    .knock {
      flex-wrap: wrap;
    }
    .knock-acts {
      flex: 1 0 100%;
      gap: var(--sp-3);
    }
    .knock-admit {
      flex: 1;
      min-height: var(--tap-min);
      font-size: var(--fs-ui);
    }
    .knock-deny {
      width: var(--tap-min);
      height: var(--tap-min);
      /* A bare glyph reads as decoration next to a full-width accent pill; the
         hairline makes it a peer button, not an afterthought. */
      border: 1px solid var(--border);
    }
  }

  /* Phone stage. auto-fit counts repetitions off the 200px MAX, not the 130px
     min, so every portrait phone resolved to ONE column: two people stacked, a
     third below the fold. Two explicit columns fit 173px tiles at 390 and still
     138px ones at 320. The 560px cap keeps the wide edge of this tier (a 768px
     window, or a tablet) from drawing two enormous tiles instead of a grid;
     landscape overrides the whole thing below. */
  @media (pointer: coarse), (max-width: 768px) {
    .stage {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      max-width: 560px;
      width: 100%;
      margin-inline: auto;
    }
    .stage.solo {
      grid-template-columns: minmax(0, 260px);
    }
    /* An odd tile out centres across both columns instead of hugging the left
       edge with a hole beside it. */
    .stage:not(.solo) > :last-child:nth-child(odd) {
      grid-column: 1 / -1;
      justify-self: center;
      width: calc(50% - 6px);
    }
  }

  /* Landscape phone: header + composer leave the panel ~175px, so 4:3 tiles
     only fit by scrolling — and the sticky controls then sit across their name
     badges. Compact, wide tiles instead: everyone at once beats a stage you
     have to drag. */
  @media (pointer: coarse) and (max-height: 480px) {
    .stage,
    .stage.solo {
      grid-template-columns: repeat(auto-fit, minmax(96px, 132px));
      max-width: none;
    }
    .tile {
      aspect-ratio: 16 / 9;
    }
    /* A 13px badge across a 54px tile is most of the face. (The avatar shrinks
       with it — see compactTiles in the script; CSS can't reach the size prop
       Avatar renders its emoji and initials at.) */
    .name {
      font-size: var(--fs-tiny);
      padding: 1px 6px;
      left: 6px;
      bottom: 6px;
    }
    .ctl {
      width: 44px;
      height: 44px;
    }
  }
</style>
