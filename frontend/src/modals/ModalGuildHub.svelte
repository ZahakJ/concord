<script>
  // The guild hub, on a phone.
  //
  // It used to be both surfaces. On a desktop it was sixteen chevron rows in
  // one scrolling column with a hero on top, drilled into one at a time, and
  // the dialog under the cursor resized on every navigation — that is the rail
  // now (lib/guildnav.js, HubRail.svelte), landing straight on Overview,
  // because a list of sixteen doors in front of a rail that already shows all
  // sixteen is a menu in front of a menu.
  //
  // On a 390px sheet there is no room for a rail and no need for one: a sheet
  // you can flick through IS the overview. So the list stays, and it is built
  // from the same table the rail reads, which is what stops the two drifting
  // apart the way the header menu, the phone sheet and this dialog used to.
  import Modal from "./Modal.svelte";
  import Icon from "../Icon.svelte";
  import Banner from "../Banner.svelte";
  import { S, activeGuild, openPanel } from "../lib/state.svelte.js";
  import { guildBannerArt } from "../lib/guildbanners.js";
  import { guildGroupsFor, guildField } from "../lib/guildnav.js";

  let { onClose } = $props();

  const g = $derived(activeGuild());
  const art = $derived(guildBannerArt(g?.banner));
  const groups = $derived(guildGroupsFor(g));
</script>

<Modal title="Guild settings" wide {onClose}>
  {#if g}
    <!-- Hero: the guild's name and icon over its banner, laid out like the
         channel-list header paints it — same Banner component, same scrim, so
         the hub opens on exactly the identity every member's sidebar shows.
         A guild without a banner gets Banner's default accent gradient rather
         than a blank strip; the scrim keeps the name readable over either. -->
    <div class="hero" class:ink-dark={art?.ink === "dark"}>
      <Banner banner={art ? g.banner : ""} scrim={art?.ink || "light"} class="hub-art" />
      <span class="hero-row">
        {#if g.icon}
          <img class="hero-icon" src={g.icon} alt="" />
        {/if}
        <strong>{g.name}</strong>
      </span>
    </div>

    {#each groups as grp (grp.label)}
      <section class="grp">
        <div class="sec-label">{grp.label}</div>
        <div class="card" class:danger-card={grp.danger}>
          {#each grp.items as it (it.kind)}
            <button
              class="row"
              class:danger={grp.danger}
              onclick={() => openPanel(it.kind, "guildHub")}
            >
              <span class="chip" class:danger-chip={grp.danger}>
                <Icon name={guildField(it.icon, g, S)} size={17} />
              </span>
              <span class="row-text">
                <span class="row-title">{guildField(it.title, g, S)}</span>
                <span class="row-sub">{guildField(it.sub, g, S)}</span>
              </span>
              <span class="chev">›</span>
            </button>
          {/each}
        </div>
      </section>
    {/each}
  {/if}
</Modal>

<style>
  /* Hero: bleeds to the dialog's edges so the art reads as a header, not a
     thumbnail. Name pinned to the bottom edge like the channel-list header. */
  .hero {
    position: relative;
    margin: -4px -20px 0;
    min-height: 88px;
    display: flex;
    align-items: flex-end;
    padding: 12px 20px;
    color: #fff;
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  }
  /* The pale templates (Linen Press) ask for dark ink; Banner.svelte flips its
     scrim to match, so the pair stays readable together. */
  .hero.ink-dark {
    color: #12161a;
    text-shadow: 0 1px 2px rgba(255, 255, 255, 0.65);
  }
  .hero :global(.hub-art) {
    position: absolute;
    inset: 0;
  }
  /* The row sits ABOVE the art layer: the art is absolutely positioned, and
     positioned boxes paint over in-flow ones whatever the DOM order. */
  .hero-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .hero-row strong {
    font-size: var(--fs-title);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hero-icon {
    width: 30px;
    height: 30px;
    border-radius: var(--radius-md);
    object-fit: cover;
    flex-shrink: 0;
  }

  /* Sectioned, carded rows — the same structure the phone's settings list uses,
     so the guild hub and the app hub read as siblings. */
  .grp {
    display: flex;
    flex-direction: column;
    gap: 7px;
    text-align: left;
    animation: grp-in 0.3s ease both;
  }
  /* Sections cascade in — a beat apart, settled fast. (The dialog's children
     start at the pinned sheet-top, then the hero, so the first section is
     nth-child(3).) */
  .grp:nth-child(4) {
    animation-delay: 0.04s;
  }
  .grp:nth-child(5) {
    animation-delay: 0.08s;
  }
  .grp:nth-child(6) {
    animation-delay: var(--dur-quick);
  }
  .grp:nth-child(7) {
    animation-delay: var(--dur-standard);
  }
  @keyframes grp-in {
    from {
      opacity: 0;
      transform: translateY(6px);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .grp {
      animation: none;
    }
  }
  .sec-label {
    font-size: var(--fs-small);
    font-weight: 700;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 0 4px;
  }
  .card {
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }
  /* Hairlines between rows, inset past the icon chip. */
  .card > .row + .row {
    border-top: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    width: 100%;
    min-height: 52px;
    padding: 10px 14px;
    background: transparent;
    color: var(--text);
    text-align: left;
    border-radius: 0;
    transition: background var(--dur-quick) ease;
  }
  button.row:hover,
  button.row:active {
    background: var(--bg-3);
  }
  .row-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .row-title {
    font-size: var(--fs-ui);
    font-weight: 600;
    line-height: 1.3;
  }
  .row-sub {
    font-size: var(--fs-small);
    line-height: 1.45;
    color: var(--text-muted);
  }
  .chev {
    flex-shrink: 0;
    font-size: 20px;
    line-height: 1;
    color: var(--text-faint);
    transition:
      transform var(--dur-standard) ease,
      color var(--dur-standard) ease;
  }
  /* Nav chevrons drift toward the destination on hover. */
  .row:hover .chev {
    color: var(--text-muted);
    transform: translateX(2px);
  }
  /* Icon chips: soft accent-tinted circles. */
  .chip {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    flex-shrink: 0;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    color: var(--accent-hover);
  }

  /* Danger zone: warning-tinted card, danger row hover. */
  .danger-card {
    border-color: color-mix(in srgb, var(--danger) 30%, var(--border));
    background: color-mix(in srgb, var(--danger) 4%, var(--bg-0));
  }
  .danger-chip {
    background: color-mix(in srgb, var(--danger) 15%, transparent);
    color: var(--danger-text);
  }
  .row.danger .row-title {
    color: var(--danger-text);
  }
  button.row.danger:hover,
  button.row.danger:active {
    background: var(--danger-soft);
  }

  /* Phone: rows get a touch more height — the finger-sized floor the other
     hubs use (Modal's global --tap-min covers the buttons; this covers the
     row rhythm). */
  @media (pointer: coarse), (max-width: 768px) {
    .row {
      min-height: 56px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .chev {
      transition: none;
    }
    .row:hover .chev {
      transform: none;
    }
  }
</style>
