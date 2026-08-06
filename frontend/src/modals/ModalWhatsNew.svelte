<script>
  // The one-time post-update splash. Concord used to ship silently — v0.54's
  // biggest features arrived unannounced and were discovered by accident or
  // not at all. This card runs ONCE per version bump (App.svelte compares the
  // build version against the last one this device saw), says what changed in
  // three or four lines, and gets out of the way forever. The notes are baked
  // into the bundle at build time: no network, per the no-runtime-fetch rule.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import { confettiBurst } from "../lib/burst.js";
  import { onMount } from "svelte";

  let { version = "", onClose } = $props();

  // Update these WITH each release — the splash is the release's voice.
  // 3-4 rows maximum: a changelog is a document, this is a wave hello.
  const HIGHLIGHTS = [
    {
      icon: "spark",
      title: "Moments",
      desc: "Share a story with your guilds — a scene, a caption, your colors. Fades after 24 hours; delete it any time.",
    },
    {
      icon: "diamond",
      title: "A whole new coat of paint",
      desc: "Guilds tint the app with their banner's colors, light mode gets four real themes, and /confetti, /fireworks and /hearts celebrate with everyone.",
    },
    {
      icon: "speaker",
      title: "Voice rooms became hangouts",
      desc: "A synthesized soundboard (airhorn included) — and on Android, calls survive your pocket.",
    },
    {
      icon: "gear",
      title: "And a hundred small things",
      desc: "Threads from any message, saved messages, polls that close and quiz, slow mode, a guild settings hub, swipe to reply…",
    },
  ];

  onMount(() => confettiBurst({ seed: `whatsnew-${version}` }));
</script>

<Modal title="" {onClose}>
  <div class="hero" aria-hidden="true">
    <span class="mark">◆</span>
  </div>
  <h2>What's new{version ? ` in ${version}` : ""}</h2>
  <div class="rows">
    {#each HIGHLIGHTS as h (h.title)}
      <div class="row">
        <span class="ric"><Icon name={h.icon} size={16} /></span>
        <span class="rtext">
          <strong>{h.title}</strong>
          <span class="tiny muted">{h.desc}</span>
        </span>
      </div>
    {/each}
  </div>
  <div class="actions">
    <button class="primary" onclick={onClose}>Nice</button>
  </div>
</Modal>

<style>
  /* Banner-grade gradient header, no image bytes: layered radials in the
     accent family, same trick the guild banner presets use. */
  .hero {
    height: 96px;
    margin: -4px -4px 14px;
    border-radius: var(--radius-md);
    background:
      radial-gradient(70% 120% at 15% 20%, color-mix(in srgb, var(--accent) 55%, transparent), transparent 65%),
      radial-gradient(60% 110% at 85% 30%, color-mix(in srgb, var(--accent) 30%, #7a6ff0), transparent 70%),
      linear-gradient(135deg, color-mix(in srgb, var(--accent) 40%, var(--bg-3)), var(--bg-3));
    display: grid;
    place-items: center;
    overflow: hidden;
  }
  .mark {
    font-size: 34px;
    color: #fff;
    text-shadow: 0 2px 18px rgb(0 0 0 / 0.35);
  }
  h2 {
    margin: 0 0 12px;
    font-size: var(--fs-title, 18px);
    text-align: center;
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .row {
    display: flex;
    gap: 12px;
    align-items: flex-start;
  }
  .ric {
    flex: none;
    width: 32px;
    height: 32px;
    border-radius: var(--radius-md);
    display: grid;
    place-items: center;
    background: var(--accent-soft, color-mix(in srgb, var(--accent) 14%, transparent));
    color: var(--accent);
  }
  .rtext {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .rtext strong {
    font-size: var(--fs-ui);
  }
  .actions {
    margin-top: 16px;
    display: flex;
    justify-content: center;
  }
</style>
