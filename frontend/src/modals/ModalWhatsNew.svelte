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
      icon: "palette",
      title: "A new look, built around the conversation",
      desc: "One sidebar holds your guilds and channels, the conversation gets the middle of the window, and members, pins, files and threads open beside it when you ask. Prefer the column of guilds down the side? Settings › Appearance › Layout has it.",
    },
    {
      icon: "search",
      title: "Jump anywhere, and find what you meant",
      desc: "Ctrl+K reaches any channel, person, guild or action — # @ ! * narrow it. Search results, pins, replies and the inbox now open the message itself, however far back it is.",
    },
    {
      icon: "phone",
      title: "Calls that say what is happening",
      desc: "A direct call rings, is declined, times out and ends the same way on both sides. Look elsewhere during a call and it follows you in a small window, shared screen included.",
    },
    {
      icon: "bell",
      title: "Badges mean it is for you",
      desc: "A number is a mention or a direct message; plain unread is a quiet dot. A channel counts as read only while you are actually reading it, and Invisible now looks exactly like offline.",
    },
    {
      icon: "shield",
      title: "Safer with your account",
      desc: "Restoring or re-linking can no longer erase a different account without naming it and asking. You can change your passphrase from Settings, and the recovery phrase asks for it before it shows.",
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
