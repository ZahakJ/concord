<script>
  // Voice room strip: one tile per participant, speaking ring on the avatar.
  import Avatar from "./Avatar.svelte";
  import { S, memberByFpr } from "./lib/state.svelte.js";

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
</script>

<div class="voice-panel">
  <div class="voice-tile" class:speaking={S.voiceSpeaking.includes("self")}>
    <Avatar
      name={S.displayName || "You"}
      emoji={S.identity.emoji}
      color={S.identity.color}
      image={S.identity.avatar}
      size={34}
    />
    <span>You{S.muted ? " (muted)" : ""}</span>
  </div>
  {#each S.voiceParticipants as pid (pid)}
    {@const p = participant(pid)}
    <div class="voice-tile" class:speaking={S.voiceSpeaking.includes(pid)}>
      <Avatar name={p.name} emoji={p.emoji} color={p.color} image={p.image} size={34} />
      <span>{p.name}</span>
    </div>
  {/each}
</div>

<style>
  .voice-panel {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    padding: 12px 16px;
    background: var(--bg-1);
    border-bottom: 1px solid var(--border);
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
</style>
