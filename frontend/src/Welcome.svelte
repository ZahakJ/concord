<script>
  // Shown in the main area when no channel is open (typically a brand-new
  // user with no servers yet). Turns an empty screen into a warm first run.
  import Icon from "./Icon.svelte";
  import { S, selectNotes } from "./lib/state.svelte.js";
</script>

<div class="welcome">
  <span class="ambient" aria-hidden="true"></span>
  <div class="hero">
    <span class="contrail" aria-hidden="true"></span>
    <div class="badge"><Icon name="concorde" size={40} /></div>
  </div>
  <h1>Welcome to <span class="brand">Concord</span></h1>
  <p class="muted lede">
    Your community, on your machines, readable by no one else. Spin up a guild, join a friend's
    with their invite code, or jot something in your private Notes.
  </p>
  <div class="chips">
    <span class="chip"><Icon name="lock" size={12} /> End-to-end encrypted</span>
    <span class="chip"><Icon name="members" size={12} /> Peer-to-peer</span>
    <span class="chip"><Icon name="spark" size={12} /> No central servers</span>
  </div>
  <div class="cards">
    <button class="card" onclick={() => (S.modal = { kind: "create" })}>
      <span class="ic"><Icon name="plus" size={18} /></span>
      <strong>Create a guild</strong>
      <span class="muted sub">Start a space for your friends</span>
    </button>
    <button class="card" onclick={() => (S.modal = { kind: "join", code: "" })}>
      <span class="ic"><Icon name="download" size={18} /></span>
      <strong>Join with an invite</strong>
      <span class="muted sub">Paste a code a friend sent you</span>
    </button>
    <button class="card" onclick={selectNotes}>
      <span class="ic"><Icon name="edit" size={18} /></span>
      <strong>Open your Notes</strong>
      <span class="muted sub">A private, encrypted scratchpad</span>
    </button>
  </div>
</div>

<style>
  .welcome {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    text-align: center;
    padding: 24px;
    gap: 10px;
    overflow-y: auto;
    position: relative;
    overflow-x: hidden;
  }
  /* Ambient aurora: one soft accent blob drifting slowly behind everything —
     the room feels lit, not flat. Single element, GPU-cheap (transform only). */
  .ambient {
    position: absolute;
    top: 8%;
    left: 50%;
    width: 420px;
    height: 420px;
    margin-left: -210px;
    border-radius: 50%;
    background: radial-gradient(circle, color-mix(in srgb, var(--accent) 12%, transparent), transparent 65%);
    pointer-events: none;
    animation: drift 14s ease-in-out infinite;
  }
  @keyframes drift {
    0%,
    100% {
      transform: translate(0, 0) scale(1);
    }
    33% {
      transform: translate(50px, 26px) scale(1.12);
    }
    66% {
      transform: translate(-40px, -14px) scale(0.94);
    }
  }
  /* "Concord" carries the accent — a soft gradient so the wordmark glows. */
  .brand {
    background: linear-gradient(120deg, var(--accent-hover), var(--accent));
    -webkit-background-clip: text;
    background-clip: text;
    color: transparent;
  }
  .hero {
    position: relative;
    display: grid;
    place-items: center;
    padding: 10px 0;
  }
  /* The jet's contrail: a fading dashed streak behind the badge, plus a soft
     accent glow. Pure CSS — no images (strict CSP). */
  .contrail {
    position: absolute;
    top: 50%;
    right: 66px;
    width: 150px;
    height: 2px;
    border-radius: 2px;
    background: repeating-linear-gradient(
      90deg,
      transparent 0 8px,
      color-mix(in srgb, var(--accent) 45%, transparent) 8px 22px
    );
    mask-image: linear-gradient(90deg, transparent, black);
    -webkit-mask-image: linear-gradient(90deg, transparent, black);
  }
  .badge {
    position: relative;
    width: 76px;
    height: 76px;
    border-radius: 20px;
    display: grid;
    place-items: center;
    background: linear-gradient(120deg, var(--accent), var(--accent-hover));
    color: var(--accent-fg);
    box-shadow:
      var(--shadow-pop),
      0 0 44px color-mix(in srgb, var(--accent) 30%, transparent);
    /* Arrive, then float: the jet gently bobs at cruise altitude. */
    animation:
      arrive 0.5s ease both,
      hover-bob 5s ease-in-out 0.5s infinite;
  }
  @keyframes arrive {
    from {
      transform: translateX(-14px);
      opacity: 0;
    }
    to {
      transform: none;
      opacity: 1;
    }
  }
  @keyframes hover-bob {
    0%,
    100% {
      transform: translateY(0) rotate(0deg);
    }
    50% {
      transform: translateY(-5px) rotate(-1.2deg);
    }
  }
  h1 {
    margin: 8px 0 0;
    font-size: var(--fs-display);
  }
  p {
    max-width: 440px;
    line-height: 1.55;
    font-size: 14px;
    margin: 0;
  }
  .chips {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: center;
    margin-top: 4px;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 4px 10px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent-hover);
    font-size: var(--fs-compact);
    font-weight: 600;
    /* Chips land a beat after the headline, left to right. */
    animation: card-in 0.35s ease backwards;
  }
  .chip:nth-child(2) {
    animation-delay: 0.08s;
  }
  .chip:nth-child(3) {
    animation-delay: 0.16s;
  }
  .cards {
    display: flex;
    gap: 12px;
    margin-top: 18px;
    flex-wrap: wrap;
    justify-content: center;
  }
  .card {
    width: 190px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    padding: 16px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-lg);
    color: var(--text);
    text-align: left;
    transition:
      transform 0.15s ease,
      border-color 0.15s ease,
      box-shadow 0.15s ease;
    /* Staggered arrival: each card fades up a beat after the previous. */
    animation: card-in 0.35s ease backwards;
  }
  .cards .card:nth-child(2) {
    animation-delay: 0.07s;
  }
  .cards .card:nth-child(3) {
    animation-delay: 0.14s;
  }
  @keyframes card-in {
    from {
      opacity: 0;
      transform: translateY(10px);
    }
  }
  /* Mouse only: on touch this lift sticks to the last card you tapped, so the
     screen keeps a card raised behind whatever dialog just opened. */
  @media (pointer: fine) {
    .card:hover {
      background: var(--bg-1);
      border-color: var(--accent);
      transform: translateY(-2px);
      box-shadow: 0 6px 20px color-mix(in srgb, var(--accent) 14%, transparent);
    }
    .card:hover .ic {
      transform: scale(1.12) rotate(-5deg);
    }
  }
  .card:active {
    transform: translateY(0);
  }
  .ic {
    width: 36px;
    height: 36px;
    border-radius: var(--radius-md);
    display: grid;
    place-items: center;
    background: linear-gradient(
      135deg,
      var(--accent-soft),
      color-mix(in srgb, var(--accent) 30%, transparent)
    );
    color: var(--accent-hover);
    margin-bottom: 6px;
    transition: transform 0.18s cubic-bezier(0.34, 1.56, 0.64, 1);
  }
  .sub {
    font-size: var(--fs-compact);
  }
  @media (prefers-reduced-motion: reduce) {
    .card,
    .card:hover,
    .chip {
      animation: none;
      transform: none;
    }
    .badge,
    .ambient {
      animation: none;
    }
    .card:hover .ic {
      transform: none;
    }
  }

  /* ---- touch adjustments: cards stack full-width, comfy targets ---- */
  @media (pointer: coarse), (max-width: 768px) {
    /* The first thing a new user sees after the door, held at arm's length —
       the lede has to read as body copy, not as a caption. */
    p {
      font-size: var(--fs-body);
    }
    .cards {
      flex-direction: column;
      width: 100%;
      max-width: 380px;
      gap: 10px;
    }
    .card {
      width: 100%;
      padding: 16px 18px;
    }
    .card:active {
      border-color: var(--accent);
      background: var(--bg-1);
    }
    .chip {
      padding: 6px 12px;
    }
    .welcome {
      padding: 20px 16px calc(20px + env(safe-area-inset-bottom));
    }
  }
</style>
