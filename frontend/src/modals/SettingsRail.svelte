<script>
  // The desktop settings rail: every destination visible at once, on the left,
  // permanently.
  //
  // What it replaces was a list of eight chevron rows in a 460px card. To reach
  // the microphone you clicked a row, the whole dialog swapped its contents AND
  // its size (380 → 460 → 1080 wide, depending on which panel you landed on),
  // you read one screen, then you clicked ‹ and it all swapped back. Eight
  // destinations, sixteen full-surface transitions to see them, and the box
  // under the cursor resizing on every one of them. That is the "nauseous".
  //
  // A rail fixes it by not moving: the dialog is one constant size for every
  // destination, the rail is always on screen, and going somewhere changes only
  // the pane on the right. Where you are is a thing you can SEE rather than a
  // thing you have to remember, and going back is looking at the rail.
  import Icon from "../Icon.svelte";
  import Avatar from "../Avatar.svelte";
  import { SETTINGS_GROUPS } from "../lib/settingsnav.js";
  import { S, switchPanel } from "../lib/state.svelte.js";
  import { tooltip } from "../lib/tooltip.js";

  let { here = "" } = $props();
  // Below 920px the words go and nine 16px glyphs are all that is left, so how
  // you learn what one means IS the tooltip. It was the NATIVE tooltip until
  // now — the one lib/tooltip.js opens by explaining is the wrong tool for an
  // icon-only column: the OS sits on it for about a second and paints it in the
  // platform's theme rather than ours. delay 80, side right, the same numbers
  // the guild rail passes, because identifying a glyph should feel instant.
  // The aria-label goes on with it: the label IS the tip, so the two cannot
  // drift, and a rail of nine unnamed buttons had nothing for a screen reader
  // either.
  const railTip = { side: "right", delay: 80 };
  let collapsed = $state(false);
  $effect(() => {
    const mq = window.matchMedia("(max-width: 920px)");
    const sync = () => (collapsed = mq.matches);
    sync();
    mq.addEventListener("change", sync);
    return () => mq.removeEventListener("change", sync);
  });
</script>

<!-- Plain buttons, one tab stop each: this is a list of links, not a toolbar,
     and Tab walking it is exactly what the eight rows it replaces did. -->
<nav class="rail" aria-label="Settings sections" data-nav-rail>
  <!-- Whose settings these are. Not decoration: the account is the thing every
       one of these pages is about, and on a device with several linked it is
       the only place that says which one you are looking at. -->
  <div class="who">
    <Avatar
      name={S.displayName || S.identity.displayName || "You"}
      emoji={S.identity.emoji}
      color={S.identity.color}
      image={S.identity.avatar}
      size={30}
    />
    <span class="who-name">{S.displayName || S.identity.displayName || "Your account"}</span>
  </div>

  {#each SETTINGS_GROUPS as g (g.label)}
    <div class="grp">
      <div class="glabel">{g.label}</div>
      {#each g.items as it (it.kind)}
        <button
          class="r-item"
          class:here={here === it.kind}
          aria-current={here === it.kind ? "page" : undefined}
          aria-label={collapsed ? it.title : undefined}
          use:tooltip={collapsed ? railTip : undefined}
          onclick={() => switchPanel(it.kind)}
        >
          <span class="mark" aria-hidden="true"></span>
          <Icon name={it.icon} size={16} />
          <span class="r-title">{it.title}</span>
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
    padding: var(--sp-3) var(--sp-2) var(--sp-4);
    /* Recessed from the pane, so the pane reads as the page and the rail as
       the furniture around it. --bg-1 rather than --bg-2 because on the light
       theme the elevated ground is nearly white and a 55% --bg-2 mix is a step
       nobody can see. */
    background: color-mix(in srgb, var(--bg-1) 55%, var(--bg-elevated));
    border-right: 1px solid var(--border);
  }
  .who {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: var(--sp-1) var(--sp-2) var(--sp-2);
    min-width: 0;
  }
  .who-name {
    font-size: var(--fs-compact);
    font-weight: 650;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .grp {
    display: flex;
    flex-direction: column;
    gap: 1px;
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
  /* The one piece of motion in the rail: the marker on the current section
     grows out of nothing as the page lands. It is a transform on a 3px bar —
     the cheapest thing the compositor can be asked to do — and it happens once
     per navigation rather than continuously. */
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
  /* Narrow windows: the rail keeps its icons and drops its words. 226px of
     labels out of a 720px dialog is the navigation eating the page, and the
     titles are on the buttons as tooltips either way. Matches the grid-column
     change in Modal.svelte. */
  @media (max-width: 920px) {
    .rail {
      align-items: center;
      padding: var(--sp-3) 7px var(--sp-4);
    }
    .who-name,
    .glabel,
    .r-title {
      display: none;
    }
    .who {
      padding: var(--sp-1) 0 var(--sp-2);
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
