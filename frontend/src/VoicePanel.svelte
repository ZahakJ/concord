<script>
  // The call stage: one big square per participant (avatar, or their live
  // camera), animated speaking rings, plus wide tiles for any screen shares.
  // Discord-style — dynamic and animated, works the same in a DM call or a
  // guild voice room.
  import Avatar from "./Avatar.svelte";
  import Icon from "./Icon.svelte";
  import { S, memberByFpr, getVideoStream, activeGuild } from "./lib/state.svelte.js";

  let { onLeaveVoice, onToggleMute, onToggleShare, onToggleCamera } = $props();

  // Solo in a DM call = still ringing the other person.
  const solo = $derived(S.voiceParticipants.length === 0);
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
      name: mem?.name || (fpr ? fpr.slice(0, 9) : peerId.slice(0, 8)),
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
        self: true,
      };
    }
    const p = participant(pid);
    return { ...p, speaking: S.voiceSpeaking.includes(pid), muted: false, self: false };
  }

  function screenLabel(tile) {
    return tile.self ? "You" : participant(tile.peerId).name;
  }

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

<div class="voice-panel" class:theater={!!focused}>
  {#if ringing || waiting}
    <div class="ringing">
      <span class="dots"><span></span><span></span><span></span></span>
      {ringing ? "Ringing…" : "Waiting for others to join…"}
    </div>
  {/if}

  {#if inTheater}
    <!-- Theater mode: one big view (a share or a participant), everyone else in
         a clickable strip. -->
    <div class="focus-main" bind:this={stageEl}>
      {#if focusedScreen}
        <!-- svelte-ignore a11y_media_has_caption -->
        <video use:srcObject={focusedScreen.key} autoplay playsinline muted={focusedScreen.self}></video>
        <span class="screen-label"><Icon name="screen" size={12} /> {screenLabel(focusedScreen)}'s screen</span>
      {:else}
        {@const t = tileInfo(focusedPid)}
        {@const cam = camTile(focusedPid)}
        {#if cam}
          <!-- svelte-ignore a11y_media_has_caption -->
          <video use:srcObject={cam.key} autoplay playsinline muted={t.self} class:mirror={t.self}></video>
        {:else}
          <div class="focus-face" style={t.color ? `--tint:${t.color}` : ""}>
            <Avatar name={t.name} emoji={t.emoji} color={t.color} image={t.image} size={96} />
          </div>
        {/if}
        <span class="screen-label">{t.self ? `${t.name} (you)` : t.name}</span>
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
            <video use:srcObject={tile.key} autoplay playsinline muted={tile.self}></video>
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
          onclick={() => toggleFocus(pid)}
          title="Click to focus {t.self ? 'yourself' : t.name}"
        >
          {#if cam}
            <!-- svelte-ignore a11y_media_has_caption -->
            <video
              use:srcObject={cam.key}
              autoplay
              playsinline
              muted={t.self}
              class:mirror={t.self}
            ></video>
          {:else}
            <div class="face" style={t.color ? `--tint:${t.color}` : ""}>
              <Avatar name={t.name} emoji={t.emoji} color={t.color} image={t.image} size={64} />
            </div>
          {/if}
          <span class="ring" aria-hidden="true"></span>
          <span class="name">
            {#if t.muted}<Icon name="micOff" size={12} />{/if}
            {t.self ? `${t.name} (you)` : t.name}
          </span>
        </div>
      {/each}
    </div>

    {#if screens.length}
      <div class="screens">
        {#each screens as tile (tile.key)}
          <button class="screen-tile" title="Click to zoom" onclick={() => toggleFocus(tile.key)}>
            <!-- svelte-ignore a11y_media_has_caption -->
            <video use:srcObject={tile.key} autoplay playsinline muted={tile.self}></video>
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
      title={S.muted ? "Unmute" : "Mute"}
      aria-label={S.muted ? "Unmute" : "Mute"}
      onclick={onToggleMute}
    >
      <Icon name={S.muted ? "micOff" : "mic"} size={18} />
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
    <button class="ctl hangup" title="Leave call" aria-label="Leave call" onclick={onLeaveVoice}>
      <Icon name="door" size={18} />
    </button>
  </div>
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
  }
  .tile:hover {
    border-color: var(--accent);
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
  .face {
    width: 100%;
    height: 100%;
    display: grid;
    place-items: center;
    background:
      radial-gradient(90% 90% at 50% 35%, color-mix(in srgb, var(--tint, var(--accent)) 26%, transparent), transparent),
      var(--bg-1);
  }
  /* Animated speaking ring — a soft pulsing glow around the tile. */
  .ring {
    position: absolute;
    inset: 0;
    border-radius: inherit;
    pointer-events: none;
    box-shadow: inset 0 0 0 0 transparent;
    transition: box-shadow 0.12s ease;
  }
  .tile.speaking .ring {
    box-shadow: inset 0 0 0 3px var(--ok);
    animation: pulse 1.3s ease-in-out infinite;
  }
  @keyframes pulse {
    0%,
    100% {
      box-shadow:
        inset 0 0 0 3px var(--ok),
        0 0 0 0 color-mix(in srgb, var(--ok) 55%, transparent);
    }
    50% {
      box-shadow:
        inset 0 0 0 3px var(--ok),
        0 0 14px 3px color-mix(in srgb, var(--ok) 45%, transparent);
    }
  }
  .name {
    position: absolute;
    left: 8px;
    bottom: 8px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
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
      radial-gradient(70% 70% at 50% 40%, color-mix(in srgb, var(--tint, var(--accent)) 26%, transparent), transparent),
      var(--bg-1);
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
  }
  .bubble.speaking {
    border-color: var(--ok);
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
  .ringing {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    font-size: 13px;
    color: var(--text-muted);
    font-style: italic;
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
      color 0.12s ease;
  }
  .ctl:hover {
    background: var(--bg-1);
  }
  .ctl.active {
    background: var(--accent-soft);
    color: var(--accent-hover);
    border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  }
  /* Muted mic reads as a clear "off/alert" state (Discord-style red). */
  .ctl.danger {
    background: color-mix(in srgb, var(--danger) 20%, transparent);
    color: var(--danger);
    border-color: color-mix(in srgb, var(--danger) 45%, transparent);
  }
  .ctl.hangup {
    background: var(--danger);
    color: #fff;
    border-color: transparent;
  }
  .ctl.hangup:hover {
    background: color-mix(in srgb, var(--danger) 85%, #000);
  }
</style>
