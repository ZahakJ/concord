<script>
  // Voice room: a strip of participant avatars (speaking rings) plus a grid of
  // live screen-share tiles when anyone (including you) is sharing.
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

  // Label a video tile: "self" is our own preview, otherwise the sharer's name.
  function videoLabel(key) {
    if (key === "self") return "You";
    return participant(key).name;
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

<div class="voice-panel">
  <div class="avatars">
    <div class="voice-tile" class:speaking={S.voiceSpeaking.includes("self")}>
      <Avatar
        name={S.displayName || "You"}
        emoji={S.identity.emoji}
        color={S.identity.color}
        image={S.identity.avatar}
        size={34}
      />
      <span>You{S.muted ? " (muted)" : ""}{S.sharing ? " · sharing" : ""}</span>
    </div>
    {#each S.voiceParticipants as pid (pid)}
      {@const p = participant(pid)}
      <div class="voice-tile" class:speaking={S.voiceSpeaking.includes(pid)}>
        <Avatar name={p.name} emoji={p.emoji} color={p.color} image={p.image} size={34} />
        <span>{p.name}</span>
      </div>
    {/each}
  </div>

  {#if S.videoPeers.length}
    <div class="video-grid">
      {#each S.videoPeers as key (key)}
        <div class="video-tile">
          <!-- svelte-ignore a11y_media_has_caption -->
          <video use:srcObject={key} autoplay playsinline muted={key === "self"}></video>
          <span class="video-label"><Icon name="screen" size={12} /> {videoLabel(key)}</span>
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
    padding: 12px 16px;
    background: var(--bg-1);
    border-bottom: 1px solid var(--border);
  }
  .avatars {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }
  .voice-tile {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }
  .voice-tile :global(.avatar) {
    border: 2px solid transparent;
    transition: border-color 0.1s ease;
  }
  .voice-tile.speaking :global(.avatar) {
    border-color: var(--ok);
    box-shadow: 0 0 0 2px var(--ok-soft);
  }
  .video-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
    gap: 10px;
  }
  .video-tile {
    position: relative;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: #000;
    aspect-ratio: 16 / 9;
  }
  .video-tile video {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }
  .video-label {
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
