<script>
  // The call stage: one big square per participant (avatar, or their live
  // camera), animated speaking rings, plus wide tiles for any screen shares.
  // Discord-style — dynamic and animated, works the same in a DM call or a
  // guild voice room.
  import { scale } from "svelte/transition";
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
    admitKnocker,
    denyKnocker,
    togglePeerMute,
  } from "./lib/state.svelte.js";
  import { bindLabel } from "./lib/keybind.js";

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

  // Soft lock is only meaningful in guild voice (DMs are already private to
  // their two members). The lock button + knock prompts show only there.
  const chId = $derived(S.voice?.channelId || "");
  const isGuildCall = $derived(activeGuild()?.kind !== "dm" && activeGuild()?.kind !== "meeting");
  const locked = $derived(isCallLocked(chId));
  const knockers = $derived(S.callKnocks[chId] || []);
  const isDM = $derived(activeGuild()?.kind === "dm");

  // Ring for ~30s while alone in a DM call, then quietly settle into "just you
  // in the call" if they never pick up (Discord-style).
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
  const ringing = $derived(solo && isDM && !ringTimedOut);
  const waiting = $derived(solo && !isDM); // empty guild voice channel

  function participant(peerId) {
    const fpr = S.voicePeerFpr[peerId];
    const mem = fpr ? memberByFpr(fpr) : null;
    return {
      // nameFor is the one place names resolve — it knows about browser guests
      // ("Zaza (guest)"), who have no member record to look up.
      name: fpr ? nameFor(fpr) : peerId.slice(0, 8),
      emoji: mem?.emoji || "",
      color: mem?.color || "",
      image: mem?.avatar || "",
    };
  }

  // The camera tile for a roster entry ("self" or a peerId), if they have one
  // live. Remote keys embed a random stream id, so match on peerId/kind, not the
  // key string.
  const camTile = (pid) =>
    S.videoTiles.find((t) => t.kind === "camera" && (pid === "self" ? t.self : t.peerId === pid));

  // Everyone in the room, self first.
  const roster = $derived(["self", ...S.voiceParticipants]);
  // Screen shares get their own wide tiles — a share isn't a person.
  const screens = $derived(S.videoTiles.filter((t) => t.kind === "screen"));

  // Discord-style focus/theater: one thing fills the stage (a screen share OR a
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

  // Fullscreen the focused view (the "zoom" affordance).
  let stageEl = $state(null);
  function toggleFullscreen() {
    if (document.fullscreenElement) document.exitFullscreen?.();
    else stageEl?.requestFullscreen?.();
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
    };
  }

  function screenLabel(tile) {
    return tile.self ? "You" : participant(tile.peerId).name;
  }

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

<div class="voice-panel" class:theater={inTheater}>
  {#if ringing || waiting}
    <div class="ringing">
      <span class="dots"><span></span><span></span><span></span></span>
      {ringing ? "Ringing…" : "Waiting for others to join…"}
    </div>
  {/if}

  {#if inTheater}
    <!-- Theater mode: one big view (a share or a participant), everyone else in
         a clickable strip. -->
    <div
      class="focus-main"
      class:speaking={focusedPid ? tileInfo(focusedPid).speaking : false}
      bind:this={stageEl}
    >
      {#if focusedScreen}
        <!-- svelte-ignore a11y_media_has_caption -->
        <video use:srcObject={focusedScreen.key} autoplay playsinline muted></video>
        <span class="screen-label"><Icon name="screen" size={12} /> {screenLabel(focusedScreen)}'s screen</span>
      {:else}
        {@const t = tileInfo(focusedPid)}
        {@const cam = camTile(focusedPid)}
        {#if cam}
          <!-- svelte-ignore a11y_media_has_caption -->
          <video use:srcObject={cam.key} autoplay playsinline muted class:mirror={t.self}></video>
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
          <span class="mute-badge" title="Deafened — can't hear the call" aria-label="Deafened">
            <Icon name="deafened" size={11} />
          </span>
        {:else if t.muted}
          <span class="mute-badge" title="Muted" aria-label="Muted"><Icon name="micOff" size={11} /></span>
        {/if}
      {/if}
      <div class="focus-actions">
        <button class="fbtn" title="Fullscreen" aria-label="Fullscreen" onclick={toggleFullscreen}>
          <Icon name="screen" size={14} />
        </button>
        <button class="fbtn" title="Exit full view" aria-label="Exit full view" onclick={() => (focusedKey = null)}>
          <Icon name="close" size={14} />
        </button>
      </div>
    </div>
    <div class="strip">
      {#each screens as tile (tile.key)}
        {#if tile.key !== focusedKey}
          <button class="thumb" title="{screenLabel(tile)}'s screen" onclick={() => toggleFocus(tile.key)}>
            <!-- svelte-ignore a11y_media_has_caption -->
            <video use:srcObject={tile.key} autoplay playsinline muted></video>
            <span class="thumb-badge"><Icon name="screen" size={10} /></span>
          </button>
        {/if}
      {/each}
      {#each roster as pid (pid)}
        {#if pid !== focusedKey}
          {@const t = tileInfo(pid)}
          <button
            class="bubble"
            class:speaking={t.speaking}
            transition:scale={pop}
            title={t.self ? `${t.name} (you)` : t.name}
            onclick={() => toggleFocus(pid)}
          >
            <Avatar name={t.name} emoji={t.emoji} color={t.color} image={t.image} size={34} />
          </button>
        {/if}
      {/each}
    </div>
  {:else}
    <div class="stage" class:solo={roster.length === 1}>
      {#each roster as pid (pid)}
        {@const t = tileInfo(pid)}
        {@const cam = camTile(pid)}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div
          class="tile"
          class:speaking={t.speaking}
          transition:scale={pop}
          onclick={() => toggleFocus(pid)}
          title="Click to focus {t.self ? 'yourself' : t.name}"
        >
          {#if cam}
            <!-- svelte-ignore a11y_media_has_caption -->
            <video
              use:srcObject={cam.key}
              autoplay
              playsinline
              muted
              class:mirror={t.self}
            ></video>
          {:else}
            <div class="face" style={t.color ? `--tint:${t.color}` : ""}>
              <Avatar name={t.name} emoji={t.emoji} color={t.color} image={t.image} size={64} />
            </div>
          {/if}
          <span class="ring" aria-hidden="true"></span>
          {#if t.speaking}
            <span class="eq" aria-hidden="true"><span></span><span></span><span></span></span>
          {/if}
          {#if t.deafened}
            <span class="mute-badge" title="Deafened — can't hear the call" aria-label="Deafened">
              <Icon name="deafened" size={11} />
            </span>
          {:else if t.muted}
            <span class="mute-badge" title="Muted" aria-label="Muted"><Icon name="micOff" size={11} /></span>
          {/if}
          {#if !t.self}
            <!-- Silence this participant for YOU only (local). Stops the tile-focus
                 click from also firing. -->
            <button
              class="local-mute"
              class:on={t.localMuted}
              title={t.localMuted ? `Unmute ${t.name} (for you)` : `Mute ${t.name} (for you)`}
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
          <span class="name">{t.self ? `${t.name} (you)` : t.name}</span>
        </div>
      {/each}
    </div>

    {#if screens.length}
      <div class="screens">
        {#each screens as tile (tile.key)}
          <button class="screen-tile" title="Click to zoom" onclick={() => toggleFocus(tile.key)}>
            <!-- svelte-ignore a11y_media_has_caption -->
            <video use:srcObject={tile.key} autoplay playsinline muted></video>
            <span class="screen-label">
              <Icon name="screen" size={12} />
              {screenLabel(tile)}'s screen · click to zoom
            </span>
          </button>
        {/each}
      </div>
    {/if}
  {/if}

  <div class="controls">
    <button
      class="ctl"
      class:danger={S.muted}
      class:keyed={pttOn && !S.muted}
      class:talking={S.talking && !S.muted}
      title={S.muted ? "Unmute" : pttOn ? `Hold ${pttKey || "your push-to-talk key"} to talk` : "Mute"}
      aria-label={S.muted ? "Unmute" : "Mute"}
      aria-pressed={S.muted}
      onclick={onToggleMute}
    >
      <Icon name={S.muted ? "micOff" : "mic"} size={18} />
    </button>
    <button
      class="ctl"
      class:danger={S.deafened}
      title={S.deafened ? "Undeafen" : "Deafen"}
      aria-label={S.deafened ? "Undeafen" : "Deafen"}
      aria-pressed={S.deafened}
      onclick={onToggleDeafen}
    >
      <Icon name={S.deafened ? "deafened" : "speaker"} size={18} />
    </button>
    <button
      class="ctl"
      class:active={S.cameraOn}
      title={S.cameraOn ? "Turn off camera" : "Turn on camera"}
      aria-label={S.cameraOn ? "Turn off camera" : "Turn on camera"}
      onclick={onToggleCamera}
    >
      <Icon name={S.cameraOn ? "cameraOff" : "camera"} size={18} />
    </button>
    <button
      class="ctl"
      class:active={S.sharing}
      title={S.sharing ? "Stop sharing" : "Share screen"}
      aria-label={S.sharing ? "Stop sharing" : "Share screen"}
      onclick={onToggleShare}
    >
      <Icon name={S.sharing ? "screenOff" : "screen"} size={18} />
    </button>
    {#if isGuildCall}
      <button
        class="ctl"
        class:active={locked}
        title={locked ? "Unlock call (anyone can join)" : "Lock call (people must knock)"}
        aria-label={locked ? "Unlock call" : "Lock call"}
        onclick={toggleCallLock}
      >
        <Icon name="lock" size={18} />
      </button>
    {/if}
    <button
      class="ctl"
      title="Audio & video settings"
      aria-label="Audio and video settings"
      onclick={() => (S.modal = { kind: "devices" })}
    >
      <Icon name="gear" size={18} />
    </button>
    <button class="ctl hangup" title="Leave call" aria-label="Leave call" onclick={onLeaveVoice}>
      <Icon name="door" size={18} />
    </button>
  </div>

  {#if knockers.length}
    <div class="knocks">
      {#each knockers as fpr (fpr)}
        <div class="knock">
          <span class="knock-who">{nameFor(fpr)} wants to join</span>
          <button class="knock-admit" onclick={() => admitKnocker(chId, fpr)}>Admit</button>
          <button class="knock-deny" onclick={() => denyKnocker(chId, fpr)} aria-label="Ignore">✕</button>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .voice-panel {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: 14px 16px;
    background: radial-gradient(120% 140% at 50% 0%, var(--bg-2), var(--bg-0));
    border-bottom: 1px solid var(--border);
    max-height: 46vh;
    overflow-y: auto;
  }
  .stage {
    display: grid;
    /* Reflowing tiles, but CAPPED so a wide window doesn't blow them up into a
       scrolling monster: each tile is at most 240px wide, centered. */
    grid-template-columns: repeat(auto-fit, minmax(130px, 200px));
    justify-content: center;
    gap: 12px;
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
      transform 0.15s ease,
      border-color 0.15s ease,
      box-shadow 0.15s ease;
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
  .strip .bubble {
    background: transparent;
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
    transition: box-shadow 0.12s ease;
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
    transition: opacity 0.12s ease, background 0.12s ease;
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
  @media (pointer: coarse) {
    .local-mute {
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
    font-size: 12px;
    font-weight: 600;
    color: #fff;
    background: rgba(0, 0, 0, 0.55);
    border-radius: var(--radius-sm);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
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
    max-height: 62vh;
  }
  .focus-main {
    position: relative;
    background: #000;
    border-radius: var(--radius-md);
    overflow: hidden;
    aspect-ratio: 16 / 9;
    max-height: 46vh;
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
    gap: 8px;
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
    border-radius: 50%;
    padding: 2px;
    border: 2px solid transparent;
    transition:
      transform 0.12s ease,
      border-color 0.12s ease;
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
    gap: 4px;
    padding: 2px 8px;
    font-size: 12px;
    color: #fff;
    background: rgba(0, 0, 0, 0.55);
    border-radius: var(--radius-sm);
  }
  /* Ringing / waiting: a soft pill that breathes in, dots doing the talking. */
  .ringing {
    align-self: center;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 6px 16px;
    font-size: 13px;
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
  /* Call controls, on the call box itself (Discord-style). Sticky to the bottom
     of the (scrollable) panel so mute/leave are always reachable, never scrolled
     off when there are many tiles or a big screen share. */
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
  .knocks {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 4px 8px 8px;
  }
  .knock {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 7px 10px;
    background: color-mix(in srgb, var(--accent) 12%, var(--bg-1));
    border: 1px solid color-mix(in srgb, var(--accent) 35%, transparent);
    border-radius: var(--radius-md);
    font-size: 13px;
  }
  .knock-who {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .knock-admit {
    flex-shrink: 0;
    padding: 5px 12px;
    background: var(--accent);
    color: #fff;
    border-radius: var(--radius-sm);
    font-size: 12px;
    font-weight: 600;
  }
  .knock-deny {
    flex-shrink: 0;
    width: 26px;
    height: 26px;
    padding: 0; /* else the global button padding pushes the ✕ off-center */
    display: grid;
    place-items: center;
    border-radius: 50%;
    color: var(--text-muted);
  }
  .knock-deny:hover {
    background: var(--bg-3);
    color: var(--text);
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
      background 0.12s ease,
      color 0.12s ease,
      transform 0.12s ease,
      box-shadow 0.15s ease;
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
  /* Muted mic reads as a clear "off/alert" state (Discord-style red). */
  .ctl.danger {
    background: color-mix(in srgb, var(--danger) 20%, transparent);
    color: var(--danger-text);
    border-color: color-mix(in srgb, var(--danger) 45%, transparent);
  }
  .ctl.danger:hover {
    background: color-mix(in srgb, var(--danger) 30%, transparent);
  }
  /* Leave is a separate kind of action from the toggles — a little breathing
     room sets it apart so it's never fat-fingered mid-call. */
  .ctl.hangup {
    background: var(--danger);
    color: #fff;
    border-color: transparent;
    margin-left: 6px;
  }
  .ctl.hangup:hover {
    background: color-mix(in srgb, var(--danger) 85%, #000);
  }
  /* Circular controls keep a circular focus ring — the global :focus-visible
     rule otherwise squares their corners to --radius-sm. */
  .ctl:focus-visible,
  .fbtn:focus-visible,
  .bubble:focus-visible {
    border-radius: 50%;
  }
  .screen-tile:focus-visible {
    border-radius: var(--radius-md);
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
  @media (pointer: coarse) {
    /* On a phone the call IS the screen — 46vh is a desktop-derived slice that
       left two rows of tiles scrolling under an empty chat placeholder. */
    .voice-panel {
      max-height: 56vh;
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
    /* .local-mute is always opaque on touch and owns this corner, so the
       speaking equalizer would sit permanently underneath it. */
    .tile .eq {
      right: auto;
      left: 8px;
    }
    /* A knock is the entire point of locking a call, and as the panel's last
       child it rendered below the sticky controls — off-screen, with nothing
       to hint it was there. Float it to the top of the panel. */
    .knocks {
      order: -1;
      padding: 0;
    }
    .controls {
      gap: 8px;
      /* 7 controls never fit one phone row at finger size (44px each + gaps
         needs 362px against 358px of content box at 390). Wrapping is the only
         thing that holds at 320 — without it flex shrinks the WIDTH only and
         border-radius:50% on a 38x52 box draws an ellipse. At 48px the toggles
         keep one row and Leave drops to its own, which is the arrangement we
         actually want. */
      flex-wrap: wrap;
      /* Full-bleed and opaque. The gradient was transparent for its top 45%, so
         tiles and name badges rendered between the buttons, and the band
         stopped 16px short of the panel edges. */
      margin: 0 -16px;
      padding: 10px 16px calc(14px + env(safe-area-inset-bottom));
      background: var(--bg-0);
      border-top: 1px solid var(--border);
    }
    .ctl {
      width: 48px;
      height: 48px;
      flex-shrink: 0;
    }
    /* Leave lands on the wrapped row of its own; the separating margin only
       knocks that row off-centre. Its red fill sets it apart anyway. */
    .ctl.hangup {
      margin-left: 0;
    }
    /* A 16:9 share on a phone is height-limited by its width, and the only
       width left to give it is our own 16px gutters. */
    .focus-main {
      width: auto;
      margin-inline: -16px;
      border-radius: 0;
    }
    .fbtn {
      width: 40px;
      height: 40px;
    }
    .strip .thumb {
      width: 96px;
      height: 56px;
    }
    .strip {
      gap: 10px;
    }
  }

  /* Phone-width stage. auto-fit counts repetitions off the 200px MAX, not the
     130px min, so every portrait phone resolved to ONE column: two people
     stacked, a third below the fold. Two explicit columns fit 173px tiles at
     390 and still 138px ones at 320. Landscape keeps the auto-fit rule — two
     columns of 400px there would be worse than what we started with. */
  @media (pointer: coarse) and (max-width: 560px) {
    .stage {
      grid-template-columns: repeat(2, minmax(0, 1fr));
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
    }
    .tile {
      aspect-ratio: 16 / 9;
    }
    .ctl {
      width: 44px;
      height: 44px;
    }
  }
</style>
