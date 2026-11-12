<script>
  // The unread mark, in its two forms, and nothing else.
  //
  // A NUMBER means "this needs you": a mention, a DM, a reply aimed at you
  // (lib/readstate.js decides which). It wears the user's accent — never red,
  // because red is for errors and three people talking to you is not an error
  // (tokens.test.mjs rule 13). Plain unread is not a number at all: it is a
  // quiet 6px dot in the ink colour.
  //
  //   <Badge count={3} label="3 mentions" />   accent count, "99+" past max
  //   <Badge dot label="Unread" />             the quiet dot
  //   <Badge count={2} muted />                a muted place with a mention:
  //                                            outlined, in muted ink
  //
  // `ring` draws a 2px cut-out in the colour of the surface it sits on (pass
  // the surface token, e.g. "var(--bg-1)"), for a badge laid over a tile or an
  // icon. `label` is the accessible name; without one the mark is decoration
  // and hidden from assistive tech (say the count in the row's own name).
  let { count = 0, dot = false, muted = false, max = 99, ring = "", label = "" } = $props();
  const text = $derived(count > max ? `${max}+` : String(count));
</script>

{#if dot}
  <span
    class="dot"
    style={ring ? `--ring:${ring}` : undefined}
    class:ringed={!!ring}
    role={label ? "img" : undefined}
    aria-label={label || undefined}
    aria-hidden={label ? undefined : "true"}
  ></span>
{:else if count > 0}
  <span
    class="badge"
    class:muted
    class:ringed={!!ring}
    style={ring ? `--ring:${ring}` : undefined}
    role={label ? "img" : undefined}
    aria-label={label || undefined}
    aria-hidden={label ? undefined : "true"}>{text}</span
  >
{/if}

<style>
  .badge {
    display: inline-block;
    flex: none;
    min-width: 16px;
    height: 16px;
    padding: 0 4px;
    border-radius: 999px;
    /* A pack whose accent sits in the alarm range (themepacks.css) swaps the
       fill for a neutral chip with an edge, so a count never reads as red. */
    background: var(--badge-bg, var(--accent));
    color: var(--badge-fg, var(--accent-fg));
    box-shadow: var(--badge-edge, none);
    font-size: var(--fs-micro);
    font-weight: 600;
    line-height: 16px;
    text-align: center;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    animation: badge-in var(--dur-standard) var(--ease-lift);
  }
  .badge.muted {
    background: transparent;
    color: var(--text-muted);
    box-shadow: inset 0 0 0 1px var(--text-muted);
  }
  .dot {
    display: inline-block;
    flex: none;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--text);
    animation: badge-in var(--dur-standard) var(--ease-lift);
  }
  /* A mark arrives once — it scales in when it appears — and then holds still.
     A changing number does not replay it (the element stays), and nothing
     here ever loops: a count that pulsed would be shouting. */
  @keyframes badge-in {
    from {
      scale: 0.5;
      opacity: 0;
    }
  }
  .badge.ringed {
    box-shadow:
      var(--badge-edge, 0 0 0 0 transparent),
      0 0 0 2px var(--ring);
  }
  .badge.muted.ringed {
    box-shadow:
      inset 0 0 0 1px var(--text-muted),
      0 0 0 2px var(--ring);
  }
  .dot.ringed {
    box-shadow: 0 0 0 2px var(--ring);
  }
</style>
