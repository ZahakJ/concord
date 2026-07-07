<script>
  // The call stage: one big square per participant (avatar, or their live
  // camera), animated speaking rings, plus wide tiles for any screen shares.
  // Discord-style — dynamic and animated, works the same in a DM call or a
  // guild voice room.
  import Avatar from "./Avatar.svelte";
  import Icon from "./Icon.svelte";
  import { S, memberByFpr, getVideoStream } from "./lib/state.svelte.js";

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

<div class="voice-panel" style="--n:{roster.length}">
  <div class="stage" class:solo={roster.length === 1}>
    {#each roster as pid (pid)}
      {@const t = tileInfo(pid)}
      {@const cam = camTile(pid)}
      <div class="tile" class:speaking={t.speaking}>
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
            <Avatar name={t.name} emoji={t.emoji} color={t.color} image={t.image} size={72} />
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
        <div class="screen-tile">
          <!-- svelte-ignore a11y_media_has_caption -->
          <video use:srcObject={tile.key} autoplay playsinline muted={tile.self}></video>
          <span class="screen-label">
            <Icon name="screen" size={12} />
            {screenLabel(tile)}'s screen
          </span>
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
    /* Big squares that reflow: fewer people → bigger tiles. */
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: 12px;
  }
  .stage.solo {
    grid-template-columns: minmax(180px, 320px);
    justify-content: center;
  }
  .tile {
    position: relative;
    aspect-ratio: 1 / 1;
    border-radius: var(--radius-lg);
    overflow: hidden;
    background: var(--bg-1);
    border: 1px solid var(--border);
    display: grid;
    place-items: center;
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
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 10px;
  }
  .screen-tile {
    position: relative;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: #000;
    aspect-ratio: 16 / 9;
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
</style>
