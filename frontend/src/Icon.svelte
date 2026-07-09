<script>
  // Inline SVG icon set (16px grid, stroked) — replaces the old emoji-glyph
  // buttons so icons render identically on every platform.
  let { name, size = 16 } = $props();

  const P = {
    hash: "M5 2 4 14 M12 2 11 14 M2.5 5.5h11 M2 10.5h11",
    pin: "M6 2h4 M8 2v5 M4.5 7h7l-1 4h-5z M8 11v3.5",
    reply: "M6.5 3.5 3 7l3.5 3.5 M3 7h6a4 4 0 0 1 4 4v1.5",
    forward: "M9.5 3.5 13 7l-3.5 3.5 M13 7H7a4 4 0 0 0-4 4v1.5",
    edit: "M9.5 3.5l3 3L6 13H3v-3z M8.5 4.5l3 3",
    trash: "M3 4.5h10 M6.5 4V2.5h3V4 M4.5 4.5l.7 9h5.6l.7-9 M6.7 7v4 M9.3 7v4",
    smile: "M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12z M5.7 6.4h.01 M10.3 6.4h.01 M5.5 9.2a3.2 3.2 0 0 0 5 0",
    plus: "M8 3v10 M3 8h10",
    attach: "M13 7.5 8.2 12.3a3.4 3.4 0 0 1-4.8-4.8L8.6 2.3a2.3 2.3 0 0 1 3.2 3.2L7 10.3a1.2 1.2 0 0 1-1.7-1.7l4.6-4.6",
    gear: "M8 5.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8z M12.8 9.4l1.2.9-1.2 2-1.4-.5a4.9 4.9 0 0 1-1.2.7L10 14H6l-.2-1.5a4.9 4.9 0 0 1-1.2-.7l-1.4.5-1.2-2 1.2-.9a5 5 0 0 1 0-1.4l-1.2-.9 1.2-2 1.4.5a4.9 4.9 0 0 1 1.2-.7L6 2h4l.2 1.5a4.9 4.9 0 0 1 1.2.7l1.4-.5 1.2 2-1.2.9a5 5 0 0 1 0 1.4z",
    mic: "M8 2.5a2 2 0 0 1 2 2v3a2 2 0 1 1-4 0v-3a2 2 0 0 1 2-2z M3.8 7.5a4.2 4.2 0 0 0 8.4 0 M8 11.7v2",
    micOff: "M8 2.5a2 2 0 0 1 2 2v3 M6 5v2.5a2 2 0 0 0 3.4 1.4 M3.8 7.5a4.2 4.2 0 0 0 6.9 3.2 M12.2 7.5c0 .6-.13 1.2-.36 1.7 M8 11.7v2 M2.5 2.5l11 11",
    speaker: "M3 6v4h2.5L9 13V3L5.5 6z M11 5.5a3.5 3.5 0 0 1 0 5 M12.6 3.8a5.8 5.8 0 0 1 0 8.4",
    megaphone: "M3 7v2l7 3V4L3 7z M3 7H2v2h1 M5 9.2v2.3a1 1 0 0 0 1.8.6 M10 4a4 4 0 0 1 0 8",
    search: "M7 11.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9z M10.3 10.3 14 14",
    close: "M4 4l8 8 M12 4l-8 8",
    download: "M8 2.5v7.5 M4.5 7 8 10.5 11.5 7 M3 13h10",
    door: "M6 2.5h7v11H6 M6 2.5v11 M2.5 8h6 M6.8 6.2 8.5 8l-1.7 1.8",
    bell: "M8 2a3.6 3.6 0 0 0-3.6 3.6c0 3-1.4 4.4-1.4 4.4h10s-1.4-1.4-1.4-4.4A3.6 3.6 0 0 0 8 2z M6.8 12.5a1.3 1.3 0 0 0 2.4 0",
    bellOff: "M5.2 3.2A3.6 3.6 0 0 1 11.6 5.6c0 3 1.4 4.4 1.4 4.4H6 M4.4 5.6c0 3-1.4 4.4-1.4 4.4h1.6 M6.8 12.5a1.3 1.3 0 0 0 2.4 0 M2.5 2.5l11 11",
    check: "M3 8.5 6.5 12 13 4.5",
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
  };

  // The Concorde logo is a filled silhouette (not stroked like the icon set),
  // so it gets its own render branch below.
  const CONCORDE =
    "M14.8 8.4c-.2.3-.6.5-1.2.5l-3.2.2-3 3.2c-.2.2-.5.2-.6 0l-.1-.3 1-2.9-3 .1-1.5 1.3c-.1.1-.3 0-.3-.1l.7-1.7-.7-1.7c0-.2.2-.3.3-.1l1.5 1.3 3 .1-1-2.9.1-.3c.1-.2.4-.2.6 0l3 3.2 3.2.2c.6 0 1 .2 1.2.5z";
</script>

{#if name === "concorde"}
  <svg width={size} height={size} viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
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
    <path d={P[name] || P.diamond} />
  </svg>
{/if}
