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
      size={32}
    />
    <span class="who-text">
      <span class="who-name">{S.displayName || S.identity.displayName || "Your account"}</span>
      <span class="who-sub">Your account</span>
    </span>
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

  .grp {
    display: flex;
    flex-direction: column;
    gap: 1px;
    flex: none;
  }
  .glabel {
    padding: 14px var(--sp-2) var(--sp-1);
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
  @media (max-width: 920px) {
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
