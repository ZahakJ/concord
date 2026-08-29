<script>
  // EventNudges — the event radar's in-app surface (lib/radar.svelte.js), a
  // top-center banner stack deliberately separate from the bottom-right toast
  // pile: a meeting starting NOW is an interruption with a verb on it, not a
  // status line. Two voices:
  //   live       "🔴 {title} is live in {place}" + Join — jumps to the channel
  //              and enters its call through App's one true voice path.
  //   scheduled  "📅 {author} scheduled {title}" + View — opens that calendar.
  // Banners self-dismiss (the radar owns the timers); ✕ works sooner.
  import Icon from "./Icon.svelte";
  import { fly } from "svelte/transition";
  import { S, jumpToChannel } from "./lib/state.svelte.js";
  import { RADAR, dismissNudge } from "./lib/radar.svelte.js";

  // onJoinVoice is App's joinVoice — knock-if-locked, mic capture, call view —
  // the exact path EventCard's Join uses, threaded here so the banner's Join
  // is the same door, not a fork.
  let { onJoinVoice } = $props();

  const reducedMotion =
    typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
  const dur = reducedMotion ? 0 : 180;

  async function join(n) {
    dismissNudge(n.id);
    S.modal = null; // the channel is the destination; whatever was open is done
    await jumpToChannel(n.channelId);
    if (n.voice) onJoinVoice?.(n.channelId);
  }

  function view(n) {
    dismissNudge(n.id);
    S.modal = { kind: "events", guildId: n.guildId };
  }
</script>

{#if RADAR.nudges.length}
  <div class="nudges">
    {#each RADAR.nudges as n (n.id)}
      <div
        class="nudge"
        class:live={n.kind === "live"}
        role="status"
        aria-live="polite"
        in:fly={{ y: -16, duration: dur }}
        out:fly={{ y: -10, duration: dur / 2 }}
      >
        {#if n.kind === "live"}
          <span class="live-dot" aria-hidden="true"></span>
        {:else}
          <span class="cal-ic" aria-hidden="true">📅</span>
        {/if}
        <span class="txt">
          <strong class="t">{n.title}</strong>
          <span class="sub">{n.sub}</span>
        </span>
        {#if n.kind === "live"}
          <button class="act" onclick={() => join(n)}>Join</button>
        {:else}
          <button class="act ghost" onclick={() => view(n)}>View</button>
        {/if}
        <button class="x" onclick={() => dismissNudge(n.id)} aria-label="Dismiss">
          <Icon name="close" size={12} />
        </button>
      </div>
    {/each}
  </div>
{/if}

<style>
  .nudges {
    position: fixed;
    top: calc(10px + var(--safe-top));
    left: 50%;
    transform: translateX(-50%);
    /* Same shelf as the toast stack: above sheets/scrims, under the app lock. */
    z-index: 450;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    width: min(440px, calc(100 * var(--vw) - 20px));
    pointer-events: none; /* the gaps must not eat clicks on the app below */
  }
  .nudge {
    pointer-events: auto;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    border-radius: var(--radius-lg);
    background: var(--bg-2);
    border: 1px solid var(--border);
    box-shadow: var(--shadow-pop);
  }
  /* Live is the loud one: --ok frame + a slow breathing glow, the same
     temperature EventCard gives the one live card in a list. */
  .nudge.live {
    border-color: color-mix(in srgb, var(--ok) 55%, var(--border));
    animation: nudge-breathe 2.8s ease-in-out infinite;
  }
  @keyframes nudge-breathe {
    0%,
    100% {
      box-shadow: var(--shadow-pop);
    }
    50% {
      box-shadow:
        var(--shadow-pop),
        0 0 14px color-mix(in srgb, var(--ok) 28%, transparent);
    }
  }
  .live-dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--ok);
    flex-shrink: 0;
    animation: nudge-pulse 1.4s ease-in-out infinite;
  }
  @keyframes nudge-pulse {
    50% {
      opacity: 0.35;
    }
  }
  .cal-ic {
    flex-shrink: 0;
    font-size: var(--fs-body);
    line-height: 1;
  }
  .txt {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .t {
    font-size: var(--fs-ui);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .nudge.live .sub {
    color: var(--ok-text);
    font-weight: 600;
  }
  .act {
    flex-shrink: 0;
    padding: 6px 16px;
    border-radius: 999px;
    background: var(--ok);
    color: var(--ok-fg);
    font-size: var(--fs-compact);
    font-weight: 700;
  }
  .act:hover {
    filter: brightness(1.1);
  }
  .act.ghost {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .act.ghost:hover {
    background: color-mix(in srgb, var(--accent) 24%, transparent);
    filter: none;
  }
  .x {
    flex-shrink: 0;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border-radius: 50%;
    background: transparent;
    color: var(--text-faint);
  }
  .x:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  @media (prefers-reduced-motion: reduce) {
    .nudge.live,
    .live-dot {
      animation: none;
    }
  }
  /* One-handed at 393px: the actionable pills reach the 44px floor. */
  @media (pointer: coarse), (max-width: 768px) {
    /* And below the phone's top bar, not across it: `10px + safe-top` clears
       the status bar and lands squarely on the shell's own header, which is
       52px tall after that inset. --mchrome is the number MobileShell keeps
       for exactly this, and the toast stack and the banner pills read it too.
       A nudge that hides the way out of the channel it is nudging about is
       worse than no nudge. */
    .nudges {
      top: calc(var(--mchrome) + 10px);
    }
    .act {
      min-height: var(--tap-min);
      padding-inline: var(--sp-4);
    }
    .x {
      width: 36px;
      height: 36px;
    }
  }
</style>
