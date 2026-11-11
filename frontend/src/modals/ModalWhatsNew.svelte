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
  // being shouted at.
</script>

<!-- No build channel in the title: "What's new in dev" named an internal
     word to the person reading it. The version is in Settings › Account. -->
<Modal title="What's new" size="m" {onClose}>
  <div class="rows">
    {#each HIGHLIGHTS as h (h.title)}
      <div class="row">
        <span class="ric"><Icon name={h.icon} size={16} /></span>
        <span class="rtext">
          <strong>{h.title}</strong>
          <span class="desc">{h.desc}</span>
        </span>
      </div>
    {/each}
  </div>
  <div class="actions">
    <button class="primary" onclick={onClose}>Done</button>
  </div>
</Modal>

<style>
  /* A list of changes: each a 16px glyph, a title in ink and one muted
     sentence under it — the title is the larger of the two. */
  .rows {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .row {
    display: flex;
    gap: var(--sp-3);
    align-items: flex-start;
  }
  .ric {
    flex: none;
    display: grid;
    place-items: center;
    height: 20px;
    color: var(--text-muted);
  }
  .rtext {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .rtext strong {
    font-size: var(--fs-ui);
    font-weight: 600;
  }
  .desc {
    font-size: var(--fs-compact);
    line-height: 1.5;
    color: var(--text-muted);
  }
</style>
