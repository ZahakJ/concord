<script>
  // Inline SVG icon set (16px grid, stroked) — replaces the old emoji-glyph
  // buttons so icons render identically on every platform.
  // `draw` sketches the stroke in on mount (~250ms) — opt-in per call site.
  let { name, size = 16, draw = false } = $props();

  const P = {
    hash: "M5 2 4 14 M12 2 11 14 M2.5 5.5h11 M2 10.5h11",
    menu: "M2.5 4.5h11 M2.5 8h11 M2.5 11.5h11",
    devices: "M1.5 3.5h8v6h-8z M3 11.5h5 M11 6h3v7.5h-3z M12.5 12h.01",
    pin: "M6 2h4 M8 2v5 M4.5 7h7l-1 4h-5z M8 11v3.5",
    reply: "M6.5 3.5 3 7l3.5 3.5 M3 7h6a4 4 0 0 1 4 4v1.5",
    forward: "M9.5 3.5 13 7l-3.5 3.5 M13 7H7a4 4 0 0 0-4 4v1.5",
    edit: "M9.5 3.5l3 3L6 13H3v-3z M8.5 4.5l3 3",
    trash: "M3 4.5h10 M6.5 4V2.5h3V4 M4.5 4.5l.7 9h5.6l.7-9 M6.7 7v4 M9.3 7v4",
    smile: "M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12z M5.7 6.4h.01 M10.3 6.4h.01 M5.5 9.2a3.2 3.2 0 0 0 5 0",
    plus: "M8 3v10 M3 8h10",
    eye: "M1.5 8s2.5-4.5 6.5-4.5S14.5 8 14.5 8s-2.5 4.5-6.5 4.5S1.5 8 1.5 8z M8 10a2 2 0 1 0 0-4 2 2 0 0 0 0 4z",
    eyeOff: "M6.2 6.3A2 2 0 0 0 8 10a2 2 0 0 0 1.8-1.2 M4.3 4.5A9.6 9.6 0 0 0 1.5 8s2.5 4.5 6.5 4.5c1.2 0 2.3-.4 3.2-.9 M6.6 3.7A6.6 6.6 0 0 1 8 3.5c4 0 6.5 4.5 6.5 4.5a12 12 0 0 1-2 2.6 M2 2l12 12",
    folder: "M2.5 4.5h3.4l1.3 1.6h6.3v6.9h-11z M2.5 7.6h11",
    bolt: "M8.8 2 4.5 9h3l-1 5L11.5 7h-3z",
    forum: "M2.5 3.5h8v5.5H5.5l-3 2.5z M13.5 6.5v6l-2.5-2H7.5",
    // One speech bubble, for a direct message. `forum` is two of them and
    // means a board; a conversation with one person is not a board, and the
    // DM list was wearing a PENCIL, which means "edit this".
    bubble: "M8 2.8c-3.4 0-6 2-6 4.6 0 1.4.8 2.7 2 3.5-.1.9-.5 1.8-1.2 2.4 1.4-.2 2.6-.8 3.4-1.5.6.1 1.2.2 1.8.2 3.4 0 6-2 6-4.6S11.4 2.8 8 2.8z",
    attach: "M13 7.5 8.2 12.3a3.4 3.4 0 0 1-4.8-4.8L8.6 2.3a2.3 2.3 0 0 1 3.2 3.2L7 10.3a1.2 1.2 0 0 1-1.7-1.7l4.6-4.6",
    gear: "M8 5.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8z M12.8 9.4l1.2.9-1.2 2-1.4-.5a4.9 4.9 0 0 1-1.2.7L10 14H6l-.2-1.5a4.9 4.9 0 0 1-1.2-.7l-1.4.5-1.2-2 1.2-.9a5 5 0 0 1 0-1.4l-1.2-.9 1.2-2 1.4.5a4.9 4.9 0 0 1 1.2-.7L6 2h4l.2 1.5a4.9 4.9 0 0 1 1.2.7l1.4-.5 1.2 2-1.2.9a5 5 0 0 1 0 1.4z",
    clock: "M8 2.6a5.4 5.4 0 1 0 0 10.8 5.4 5.4 0 0 0 0-10.8z M8 5.4V8l1.9 1.4",
    poll: "M3 13.2h10 M4.5 12.5V8 M8 12.5V4.5 M11.5 12.5V6.5",
    play: "M5.5 3.6v8.8l7-4.4z",
    pause: "M6 3.5v9 M10 3.5v9",
    // A square, not two bars. Two bars are the universal promise that pressing
    // it again resumes; "Stop recording" ends the take and moves you to a
    // preview you cannot append to, which is the opposite promise.
    stop: "M4.6 4.6h6.8v6.8h-6.8z",
    mic: "M8 2.5a2 2 0 0 1 2 2v3a2 2 0 1 1-4 0v-3a2 2 0 0 1 2-2z M3.8 7.5a4.2 4.2 0 0 0 8.4 0 M8 11.7v2",
    micOff: "M8 2.5a2 2 0 0 1 2 2v3 M6 5v2.5a2 2 0 0 0 3.4 1.4 M3.8 7.5a4.2 4.2 0 0 0 6.9 3.2 M12.2 7.5c0 .6-.13 1.2-.36 1.7 M8 11.7v2 M2.5 2.5l11 11",
    speaker: "M3 6v4h2.5L9 13V3L5.5 6z M11 5.5a3.5 3.5 0 0 1 0 5 M12.6 3.8a5.8 5.8 0 0 1 0 8.4",
    imagetext: "M2 3.5h12v9H2z M4.5 6.3h7 M4.5 8.5h7 M4.5 10.7h4.2",
    deafened: "M3 6v4h2.5L9 13V3L5.5 6z M2.5 2.5l11 11",
    underline: "M4 3v5a4 4 0 0 0 8 0V3 M3.5 13.5h9",
    list: "M3 4.5h.01 M6 4.5h7.5 M3 8h.01 M6 8h7.5 M3 11.5h.01 M6 11.5h7.5",
    heading: "M4 3v10 M12 3v10 M4 8h8",
    megaphone: "M3 7v2l7 3V4L3 7z M3 7H2v2h1 M5 9.2v2.3a1 1 0 0 0 1.8.6 M10 4a4 4 0 0 1 0 8",
    search: "M7 11.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9z M10.3 10.3 14 14",
    close: "M4 4l8 8 M12 4l-8 8",
    download: "M8 2.5v7.5 M4.5 7 8 10.5 11.5 7 M3 13h10",
    door: "M6 2.5h7v11H6 M6 2.5v11 M2.5 8h6 M6.8 6.2 8.5 8l-1.7 1.8",
    bell: "M8 2a3.6 3.6 0 0 0-3.6 3.6c0 3-1.4 4.4-1.4 4.4h10s-1.4-1.4-1.4-4.4A3.6 3.6 0 0 0 8 2z M6.8 12.5a1.3 1.3 0 0 0 2.4 0",
    bellOff: "M5.2 3.2A3.6 3.6 0 0 1 11.6 5.6c0 3 1.4 4.4 1.4 4.4H6 M4.4 5.6c0 3-1.4 4.4-1.4 4.4h1.6 M6.8 12.5a1.3 1.3 0 0 0 2.4 0 M2.5 2.5l11 11",
    check: "M3 8.5 6.5 12 13 4.5",
    // The mirror of `chevron`. Three studios rendered `chevron` unrotated
    // inside a button labelled "Back", so the most prominent control on each
    // was a forward arrow — a direction typed from memory. A name cannot be
    // typed the wrong way round.
    back: "M10.5 3 5.5 8l5 5",
    // Text colour: an A, with the coloured bar under it drawn by the caller so
    // the swatch can be the colour and the glyph can stay monochrome.
    textcolor: "M3.4 10.6 8 2.6l4.6 8M5.2 7.9h5.6",
    // The advanced composer. It borrowed `heading` — an H — inside a composer
    // that already has a formatting bar, where an H means "heading". A page
    // with a pen over it says "write this one properly".
    docpen: "M9 2H4.2A1.2 1.2 0 0 0 3 3.2v9.6A1.2 1.2 0 0 0 4.2 14h5.3M5.5 5h3M5.5 7.5h2M14 5.6 9.6 10l-2 .6.6-2L12.6 4.2a1 1 0 0 1 1.4 1.4Z",
    // Seal a send time. It borrowed `diamond`, which is this file's fallback
    // for an unknown name and means nothing in any icon vocabulary. A stamp
    // with a pressed impression under it does.
    stamp: "M6 2.2h4a1.4 1.4 0 0 1 1.4 1.5l-.4 3.1h1.6a1.4 1.4 0 0 1 1.4 1.4v1.3H2v-1.3a1.4 1.4 0 0 1 1.4-1.4H5l-.4-3.1A1.4 1.4 0 0 1 6 2.2ZM3.4 12.4h9.2v1.4H3.4z",
    copy: "M5.5 5.5h8v8h-8z M10.5 5.5v-3h-8v8h3",
    info: "M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12z M8 7.2v3.6 M8 5h.01",
    alert: "M8 2.2 14.3 13H1.7z M8 6.4v3 M8 11.3h.01",
    chevron: "M5.5 3 10.5 8l-5 5",
    spark: "M8 1.5 9.6 6l4.4 1.6L9.6 9.2 8 13.7 6.4 9.2 2 7.6 6.4 6z",
    screen: "M2 3.5h12v8H2z M6 14h4 M8 11.5V14",
    screenOff: "M2 3.5h12v8H2z M6 14h4 M8 11.5V14 M2 2.5l12 11",
    camera: "M2 4.5h8v7H2z M10 7l3.5-2v6L10 9",
    cameraOff: "M2 4.5h8v7H2z M10 7l3.5-2v6L10 9 M2 2.5l12 11",
    diamond: "M8 1.5 14.5 8 8 14.5 1.5 8z",
    lock: "M5 7V5.2a3 3 0 0 1 6 0V7 M3.5 7h9v6.5h-9z M8 9.5v1.7",
    members:
      "M6 7.5a2.2 2.2 0 1 0 0-4.4 2.2 2.2 0 0 0 0 4.4z M1.8 13v-.8a3.7 3.7 0 0 1 7.4 0v.8 M10.6 3.4a2.2 2.2 0 0 1 0 4.2 M11.4 9.1a3.7 3.7 0 0 1 2.8 3.1V13",
    // ONE person, for the settings entry that means you. `members` is a crowd
    // and reads as "the member list"; the row it sits on is your own name.
    user: "M8 8.2a2.7 2.7 0 1 0 0-5.4 2.7 2.7 0 0 0 0 5.4z M2.9 13.5v-.9a5.1 5.1 0 0 1 10.2 0v.9",
    // A painter's palette, for Appearance. The row used to carry a two-tone
    // diagonal block with nothing inside it — the only glyph in Settings that
    // was not an icon, and the one the owner could not name.
    palette:
      "M8 2.2a5.8 5.8 0 0 0 0 11.6c.9 0 1.4-.6 1.4-1.3 0-.8-.6-1.1-.6-1.8 0-.6.5-1.1 1.2-1.1h1.2A3.4 3.4 0 0 0 14 6.2C13.7 3.9 11.1 2.2 8 2.2z M5.4 6.2h.01 M8 4.8h.01 M10.6 6h.01 M5.2 9.2h.01",
    // A shield, for account & security. Distinct from `lock`, which Privacy
    // already wears: a lock is about what is closed, a shield about what is
    // protecting you.
    shield: "M8 2.2 13 4v4.1c0 3-2.1 5-5 5.7-2.9-.7-5-2.7-5-5.7V4z",
    // Markdown formatting toolbar (composer)
    bold: "M5.5 2.5v11 M5.5 2.5H9a2.75 2.75 0 0 1 0 5.5H5.5 M5.5 8h3.9a2.75 2.75 0 0 1 0 5.5H5.5",
    italic: "M6.5 2.5h5 M4.5 13.5h5 M9.5 2.5l-3 11",
    strike:
      "M2.5 8h11 M11.2 4.6c-.4-1.3-1.7-2.1-3.2-2.1-1.9 0-3.3 1-3.3 2.4 0 .4.1.8.3 1.1 M4.8 11.4c.4 1.3 1.7 2.1 3.2 2.1 1.9 0 3.3-1 3.3-2.4 0-.4-.1-.8-.3-1.1",
    spoiler:
      "M2 8s2.2-3.8 6-3.8S14 8 14 8s-2.2 3.8-6 3.8S2 8 2 8z M8 9.7a1.7 1.7 0 1 0 0-3.4 1.7 1.7 0 0 0 0 3.4z M3.2 12.8 12.8 3.2",
    code: "M5.5 4.5 2 8l3.5 3.5 M10.5 4.5 14 8l-3.5 3.5",
    codeblock: "M2.5 3.5h11v9h-11z M6.5 6 4.5 8l2 2 M9.5 6l2 2-2 2",
    quote: "M3 2.5v11 M6.5 4.5H13 M6.5 8h6.5 M6.5 11.5h4.5",
    link: "M6.8 9.2l2.4-2.4 M5.6 6.8 3.9 8.5a2.6 2.6 0 0 0 3.6 3.6l1.7-1.7 M10.4 9.2l1.7-1.7a2.6 2.6 0 0 0-3.6-3.6L6.8 5.6",
    // Mobile composer: paper-plane send button.
    send: "M14.5 1.5 1.5 7.2 6.9 9.1 8.8 14.5z M14.5 1.5 6.9 9.1",
    // Mobile top bar: vertical-ellipsis "more" menu.
    dots: "M8 3.4h.01 M8 8h.01 M8 12.6h.01",
    // Guild calendar (events panel, "Your calendar").
    calendar: "M2.5 4h11v9.5h-11z M2.5 7h11 M5 2.5V5 M11 2.5V5",
    // Ownership transfer (member context menu): a simple three-point crown.
    crown: "M2.5 12.5 2 5l3.5 2.5L8 3.5l2.5 4L14 5l-.5 7.5z",
    // Empty game library (EmptyState badge): a five-pip die.
    die: "M2.5 2.5h11v11h-11z M5.4 5.4h.01 M10.6 5.4h.01 M8 8h.01 M5.4 10.6h.01 M10.6 10.6h.01",
    // Take back the last stroke on the drawing pad: an arrow curving back on
    // itself. Distinct from `reply`, which points into a conversation.
    undo: "M3 7.5h6.5a3.5 3.5 0 0 1 0 7H6 M5.5 4 3 7.5 5.5 11",
    // Call events (missed-call lines in DMs).
    phone:
      "M4.4 2.5c.4 0 .8.2.9.6l.9 2.1a1 1 0 0 1-.2 1.1L4.8 7.5a9.8 9.8 0 0 0 3.7 3.7l1.2-1.2a1 1 0 0 1 1.1-.2l2.1.9c.4.1.6.5.6.9v1.4c0 .6-.5 1.1-1.1 1A11.9 11.9 0 0 1 2.5 3.6c-.1-.6.4-1.1 1-1.1z",
  };

  // The Concorde logo: a front-view delta-wing silhouette (its own 64-unit
  // viewBox, filled not stroked like the icon set — separate branch below).
  // The two cockpit panes are evenodd HOLES, so it themes to any --accent and
  // sits on any background with no baked-in fill showing through the windows.
  const CONCORDE =
    "M32 6C31.1 6 30.6 6.7 30.6 8.2L30.2 17C27.3 17.8 25.2 20.5 25.2 23.8L25.2 27.6L4.6 41.6C3.4 42.4 4.1 44.3 5.5 44L24 40.7C24.7 41.9 25.8 42.8 27.1 43.3L26.2 50.6C26.1 51.6 26.9 52.2 27.7 51.7L30.3 45.9L31 55.2C31 56 31.4 56.4 32 56.4C32.6 56.4 33 56 33 55.2L33.7 45.9L36.3 51.7C37.1 52.2 37.9 51.6 37.8 50.6L36.9 43.3C38.2 42.8 39.3 41.9 40 40.7L58.5 44C59.9 44.3 60.6 42.4 59.4 41.6L38.8 27.6L38.8 23.8C38.8 20.5 36.7 17.8 33.8 17L33.4 8.2C33.4 6.7 32.9 6 32 6ZM28.4 22.3L31 22.3L31 27.2L27.4 27.2L27.4 24.3C27.4 23.3 27.7 22.3 28.4 22.3ZM33 22.3L35.6 22.3C36.3 22.3 36.6 23.3 36.6 24.3L36.6 27.2L33 27.2Z";
</script>

{#if name === "concorde"}
  <svg width={size} height={size} viewBox="0 0 64 64" fill="currentColor" fill-rule="evenodd" aria-hidden="true">
    <path d={CONCORDE} />
  </svg>
{:else}
  <svg
    width={size}
    height={size}
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    stroke-width="1.4"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <!-- pathLength=1 normalises every glyph's length so one dasharray value
         covers the whole set when draw is on. -->
    <path d={P[name] || P.diamond} pathLength={draw ? 1 : undefined} class:draw />
  </svg>
{/if}

<style>
  /* Opt-in draw-in: the stroke sketches itself from nothing on mount. */
  .draw {
    stroke-dasharray: 1;
    animation: icon-draw 0.25s ease-out both;
  }
  @keyframes icon-draw {
    from {
      stroke-dashoffset: 1;
    }
    to {
      stroke-dashoffset: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    /* No sketching; dashoffset stays at its default 0, so with dasharray 1 on
       a pathLength-1 path the glyph simply renders complete. */
    .draw {
      animation: none;
    }
  }
</style>
