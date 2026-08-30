<script>
  // The guild hub's rail: every destination for ONE guild visible at once, on
  // the left, permanently.
  //
  // What it replaces was sixteen chevron rows in a scrolling column, drilled
  // into one at a time. Measured at 1440x900: the box was 460x810 on the hub,
  // 460x400 on Overview, 380x220 on Roles and 380x168 on Banned members, so the
  // surface under the cursor swung 80px wide and 642px tall between two clicks
  // and its top-left corner travelled 321px down the screen. Clicking a row at
  // y 500 left the pointer over scrim. And Moderation log, Members, Roles and
  // Bans — the four things one moderation session touches together — could not
  // be reached except one at a time, out through the hub and back in.
  //
  // Same answer as the settings rail, for the same reason: the dialog is one
  // constant size for every destination, the rail is always on screen, and
  // going somewhere changes only the pane on the right.
  import Icon from "../Icon.svelte";
  import Banner from "../Banner.svelte";
  import { guildGroupsFor, guildField } from "../lib/guildnav.js";
  import { S, activeGuild, switchPanel } from "../lib/state.svelte.js";
  import { guildBannerArt } from "../lib/guildbanners.js";
  import { guildInitials } from "../lib/rail.js";
  import { tooltip } from "../lib/tooltip.js";

  let { here = "" } = $props();

  const g = $derived(activeGuild());
  const art = $derived(guildBannerArt(g?.banner));
  const groups = $derived(guildGroupsFor(g));
  // Identifying an icon-only column should feel instant. Native title= is the
  // wrong tool here for the reasons lib/tooltip.js opens with, and below 920px
  // the words are all that is left to identify a glyph by.
  const railTip = { side: "right", delay: 80 };
  // The words identify the row on a full-width rail. Below 920px they go and
  // the tooltip / aria-label become the name; showing both at once is how
  // "Members" appeared twice, once in the row and once as a chip over the pane.
  let collapsed = $state(false);
  $effect(() => {
    const mq = window.matchMedia("(max-width: 920px)");
    const sync = () => (collapsed = mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  });
</script>

<nav class="rail" aria-label="Guild sections" data-nav-rail>
  <!-- WHICH guild these settings are. Not decoration: on a device in six
       guilds it is the only thing on the surface that says which one you are
       editing, and it is the identity every member's sidebar already shows —
       same Banner component, same scrim. -->
  <div class="who" class:ink-dark={art?.ink === "dark"}>
    <Banner banner={art ? g.banner : ""} scrim={art?.ink || "light"} class="who-art" />
    <span class="who-row">
      {#if g?.icon}
        <img class="who-icon" src={g.icon} alt="" />
      {:else}
        <span class="who-mark">{guildInitials(g?.name || "")}</span>
      {/if}
      <span class="who-name">{g?.name || "This guild"}</span>
    </span>
  </div>

  {#each groups as grp (grp.label)}
    <div class="grp" class:danger={grp.danger}>
      <div class="glabel">{grp.label}</div>
      {#each grp.items as it (it.kind)}
        {@const label = guildField(it.title, g, S)}
        <button
          class="r-item"
          class:here={here === it.kind}
          aria-current={here === it.kind ? "page" : undefined}
          aria-label={collapsed ? label : undefined}
          use:tooltip={collapsed ? railTip : undefined}
          onclick={() => switchPanel(it.kind)}
        >
          <span class="mark" aria-hidden="true"></span>
          <Icon name={guildField(it.icon, g, S)} size={16} />
          <span class="r-title">{label}</span>
        </button>
      {/each}
    </div>
  {/each}
</nav>

<style>
  .rail {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: 0 var(--sp-2) var(--sp-4);
    /* Recessed from the pane, so the pane reads as the page and the rail as
       the furniture around it — the settings rail's ground, because these two
       surfaces must read as the same kind of place. */
    background: color-mix(in srgb, var(--bg-1) 55%, var(--bg-elevated));
    border-right: 1px solid var(--border);
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  /* The guild's own art, bled to the rail's top edge the way the channel-list
     header paints it. */
  .who {
    position: relative;
    display: flex;
    align-items: flex-end;
    min-height: 74px;
    margin: 0 calc(-1 * var(--sp-2)) var(--sp-1);
    padding: 10px var(--sp-2);
    color: #fff;
    text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
    flex: none;
  }
  .who.ink-dark {
    color: #12161a;
    text-shadow: 0 1px 2px rgba(255, 255, 255, 0.65);
  }
  .who :global(.who-art) {
    position: absolute;
    inset: 0;
  }
  /* Positioned boxes paint over in-flow ones whatever the DOM order. */
  .who-row {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    min-width: 0;
  }
  .who-icon,
  .who-mark {
    width: 26px;
    height: 26px;
    flex: none;
    border-radius: var(--radius-md);
    object-fit: cover;
  }
  .who-mark {
    display: grid;
    place-items: center;
    font-size: var(--fs-micro);
    font-weight: 800;
    background: rgba(0, 0, 0, 0.28);
  }
  .who-name {
    font-size: var(--fs-compact);
    font-weight: 700;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .grp {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: none;
  }
  .glabel {
    padding: 0 var(--sp-2) 5px;
    font-size: var(--fs-micro);
    font-weight: 700;
    letter-spacing: 0.09em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .r-item {
    position: relative;
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    padding: 8px var(--sp-2);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    text-align: left;
    font-size: var(--fs-compact);
    font-weight: 550;
    transition:
      background var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  .r-title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .r-item:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .r-item.here {
    background: var(--accent-soft);
    color: var(--accent-hover);
    font-weight: 650;
  }
  /* The one destructive group reads as one: quiet at rest, its own colour on
     hover and when you are standing in it. A whole card tinted red beside four
     that are not was the hub's old answer and it shouted at every glance. */
  .grp.danger .r-item {
    color: color-mix(in srgb, var(--danger-text) 72%, var(--text-muted));
  }
  .grp.danger .r-item:hover {
    background: var(--danger-soft);
    color: var(--danger-text);
  }
  .grp.danger .r-item.here {
    background: var(--danger-soft);
    color: var(--danger-text);
  }
  .grp.danger .mark {
    background: var(--danger);
  }
  .mark {
    position: absolute;
    left: -1px;
    top: 50%;
    width: 3px;
    height: 18px;
    margin-top: -9px;
    border-radius: 0 3px 3px 0;
    background: var(--accent);
    transform: scaleY(0);
    transform-origin: center;
  }
  .r-item.here .mark {
    animation: mark-in 0.24s var(--ease-spring) both;
  }
  @keyframes mark-in {
    to {
      transform: scaleY(1);
    }
  }
  /* Narrow windows: the rail keeps its icons and drops its words, and the
     tooltip above is what identifies a glyph then. The group hairlines stay —
     they cost 1px and they are the whole information architecture. */
  @media (max-width: 920px) {
    .rail {
      align-items: center;
      padding: 0 7px var(--sp-4);
    }
    .who {
      margin: 0 -7px var(--sp-1);
      padding: 8px 7px;
      justify-content: center;
    }
    .who-name,
    .glabel,
    .r-title {
      display: none;
    }
    .grp {
      gap: 3px;
      width: 100%;
    }
    .grp + .grp {
      border-top: 1px solid var(--hairline);
      padding-top: var(--sp-2);
    }
    .r-item {
      justify-content: center;
      padding: 9px 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .r-item.here .mark {
      animation: none;
      transform: scaleY(1);
    }
    .r-item {
      transition: none;
    }
  }
</style>
