<script>
  // The one-time post-update splash. Concord used to ship silently — v0.54's
  // biggest features arrived unannounced and were discovered by accident or
  // not at all. This card runs ONCE per version bump (App.svelte compares the
  // build version against the last one this device saw), says what changed in
  // three or four lines, and gets out of the way forever. The notes are baked
  // into the bundle at build time: no network, per the no-runtime-fetch rule.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";

  let { version = "", onClose } = $props();

  // Update these WITH each release — the splash is the release's voice.
  // 3-5 rows maximum: a changelog is a document, this is a wave hello.
  const HIGHLIGHTS = [
    {
      icon: "folder",
      title: "Bring a community's history with you",
      desc: "Point the import wizard at a chat export and it says what is inside, and what your choices will cost, before it writes a byte. What comes out sits above your channels — scrolling past the top simply keeps reading.",
    },
    {
      icon: "members",
      title: "A crowd can arrive all at once",
      desc: "A guild used to pay for every joiner separately, so a wave cost more than the wave. A hundred people joining now settle in 47 seconds rather than two and a quarter minutes, and a newcomer is handed everybody's name instead of everybody's photograph.",
    },
    {
      icon: "phone",
      title: "The phone app, rebuilt around your thumb",
      desc: "Only the messages you are looking at are drawn, so a channel opens in 55ms rather than freezing; half as much is fetched before the first one appears; every sheet answers a pull the same way; and back is Android's own gesture again.",
    },
    {
      icon: "bell",
      title: "Find what was aimed at you",
      desc: "An inbox gathers every mention, every reply and every hit on the words you asked to be told about — matched on your own machine, told to nobody. Come back after a night away and a card at the unread line hands you what you missed.",
    },
    {
      icon: "die",
      title: "Draw on a message, play, make a noise",
      desc: "A pad that sends strokes rather than a picture, four in a row played inside a channel with nothing but the messages holding the board, and a soundboard whose sounds are thirty-byte recipes anyone can write.",
    },
  ];


  // Deliberately no confetti here. The splash already arrives unannounced on
  // top of a fresh login; adding a burst to it made opening the app feel like
  // being shouted at. The gradient header is the celebration.
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
    gap: var(--sp-3);
  }
  .row {
    display: flex;
    gap: var(--sp-3);
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
    margin-top: var(--sp-4);
    display: flex;
    justify-content: center;
  }
</style>
