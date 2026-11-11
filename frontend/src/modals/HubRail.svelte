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
  import { guildGroupsFor, guildField } from "../lib/guildnav.js";
  import { S, activeGuild, switchPanel } from "../lib/state.svelte.js";
  import { guildInitials } from "../lib/rail.js";
  import { tooltip } from "../lib/tooltip.js";

  let { here = "" } = $props();

  const g = $derived(activeGuild());
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
    const mq = window.matchMedia("(max-width: 768px)");
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
  <div class="who">
    {#if g?.icon}
      <img class="who-icon" src={g.icon} alt="" />
    {:else}
      <span class="who-mark">{guildInitials(g?.name || "")}</span>
    {/if}
    <span class="who-text">
      <span class="who-name">{g?.name || "This guild"}</span>
      <span class="who-sub">Guild settings</span>
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
  /* The rail of a railed dialog: 224px, recessed from the pane (--bg-1 mixed
     into the raised ground) behind a hairline. Who it is about at the top,
     then groups in sentence case and 32px rows. */
  .rail {
    display: flex;
    flex-direction: column;
    padding: var(--sp-4) 10px var(--sp-4);
    background: color-mix(in srgb, var(--bg-1) 55%, var(--bg-elevated));
    border-right: 1px solid var(--hairline);
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .who {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: var(--sp-1) var(--sp-2) 6px;
    min-width: 0;
    flex: none;
  }
  .who-text {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .who-name {
    font-size: var(--fs-ui);
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .who-sub {
    font-size: var(--fs-small);
    color: var(--text-muted);
  }
  .who-icon,
  .who-mark {
    width: 32px;
    height: 32px;
    flex: none;
    border-radius: var(--radius-sm);
    object-fit: cover;
  }
  .who-mark {
    display: grid;
    place-items: center;
    font-size: var(--fs-small);
    font-weight: 600;
    background: var(--bg-3);
    color: var(--text);
  }
  /* The one destructive group: its rows in the danger ink, quietly. */
  .grp.danger .r-item {
    color: var(--danger-text);
  }
  .grp.danger .mark {
    background: var(--danger-text);
  }

  /* Fifteen rows in five groups have to fit the 700px frame without a
     scroll, or the last group — the Danger zone — is the one cut off. The
     rows keep the 32px target; the air between groups is what gives. */
  .grp {
    display: flex;
    flex-direction: column;
    flex: none;
  }
  .glabel {
    padding: var(--sp-2) var(--sp-2) 2px;
    font-size: var(--fs-small);
    font-weight: 600;
    color: var(--text-muted);
  }
  .r-item {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    min-height: 32px;
    padding: 0 var(--sp-2);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    text-align: start;
    font-size: var(--fs-ui);
    font-weight: 400;
  }
  .r-title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  @media (pointer: fine) {
    .r-item:hover {
      background: color-mix(in srgb, var(--bg-3) 60%, transparent);
      color: var(--text);
    }
  }
  /* Where you are: the hover fill made solid, full ink, and a 2px ink bar on
     the start edge (the fill alone is 1.13:1 on paper). Not the accent: being
     on a page is not something that needs you. */
  .r-item.here {
    background: var(--bg-3);
    color: var(--text);
    font-weight: 500;
  }
  .mark {
    position: absolute;
    left: 0;
    top: 8px;
    bottom: 8px;
    width: 2px;
    border-radius: 0 2px 2px 0;
    background: var(--text);
    opacity: 0;
  }
  .r-item.here .mark {
    opacity: 1;
  }

  /* Narrow windows: the rail keeps its icons and drops its words, and the
     tooltip is what identifies a glyph then. The group hairlines stay — they
     cost 1px and they are the whole information architecture. */
  @media (max-width: 768px) {
    .rail {
      align-items: center;
      padding: var(--sp-3) 7px var(--sp-4);
    }
    .who-name,
    .who-sub,
    .glabel,
    .r-title {
      display: none;
    }
    .who {
      padding: var(--sp-1) 0 var(--sp-2);
      justify-content: center;
    }
    .grp {
      gap: 3px;
      width: 100%;
    }
    .grp + .grp {
      border-top: 1px solid var(--hairline);
      margin-top: var(--sp-2);
      padding-top: var(--sp-2);
    }
    .r-item {
      justify-content: center;
      padding: 0;
      min-height: 36px;
    }
  }
</style>
