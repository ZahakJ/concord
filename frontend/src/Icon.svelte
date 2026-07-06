<script>
  // Inline SVG icon set (16px grid, stroked) — replaces the old emoji-glyph
  // buttons so icons render identically on every platform.
  let { name, size = 16 } = $props();

  const P = {
    hash: "M5 2 4 14 M12 2 11 14 M2.5 5.5h11 M2 10.5h11",
    pin: "M6 2h4 M8 2v5 M4.5 7h7l-1 4h-5z M8 11v3.5",
    reply: "M6.5 3.5 3 7l3.5 3.5 M3 7h6a4 4 0 0 1 4 4v1.5",
    edit: "M9.5 3.5l3 3L6 13H3v-3z M8.5 4.5l3 3",
    trash: "M3 4.5h10 M6.5 4V2.5h3V4 M4.5 4.5l.7 9h5.6l.7-9 M6.7 7v4 M9.3 7v4",
    smile: "M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12z M5.7 6.4h.01 M10.3 6.4h.01 M5.5 9.2a3.2 3.2 0 0 0 5 0",
    plus: "M8 3v10 M3 8h10",
    attach: "M13 7.5 8.2 12.3a3.4 3.4 0 0 1-4.8-4.8L8.6 2.3a2.3 2.3 0 0 1 3.2 3.2L7 10.3a1.2 1.2 0 0 1-1.7-1.7l4.6-4.6",
    gear: "M8 10.2a2.2 2.2 0 1 0 0-4.4 2.2 2.2 0 0 0 0 4.4z M8 1.8v1.7 M8 12.5v1.7 M1.8 8h1.7 M12.5 8h1.7 M3.6 3.6l1.2 1.2 M11.2 11.2l1.2 1.2 M12.4 3.6l-1.2 1.2 M4.8 11.2l-1.2 1.2",
    mic: "M8 2.5a2 2 0 0 1 2 2v3a2 2 0 1 1-4 0v-3a2 2 0 0 1 2-2z M3.8 7.5a4.2 4.2 0 0 0 8.4 0 M8 11.7v2",
    micOff: "M8 2.5a2 2 0 0 1 2 2v3 M6 5v2.5a2 2 0 0 0 3.4 1.4 M3.8 7.5a4.2 4.2 0 0 0 6.9 3.2 M12.2 7.5c0 .6-.13 1.2-.36 1.7 M8 11.7v2 M2.5 2.5l11 11",
    speaker: "M3 6v4h2.5L9 13V3L5.5 6z M11 5.5a3.5 3.5 0 0 1 0 5 M12.6 3.8a5.8 5.8 0 0 1 0 8.4",
    search: "M7 11.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9z M10.3 10.3 14 14",
    close: "M4 4l8 8 M12 4l-8 8",
    download: "M8 2.5v7.5 M4.5 7 8 10.5 11.5 7 M3 13h10",
    door: "M6 2.5h7v11H6 M6 2.5v11 M2.5 8h6 M6.8 6.2 8.5 8l-1.7 1.8",
    bell: "M8 2a3.6 3.6 0 0 0-3.6 3.6c0 3-1.4 4.4-1.4 4.4h10s-1.4-1.4-1.4-4.4A3.6 3.6 0 0 0 8 2z M6.8 12.5a1.3 1.3 0 0 0 2.4 0",
    bellOff: "M5.2 3.2A3.6 3.6 0 0 1 11.6 5.6c0 3 1.4 4.4 1.4 4.4H6 M4.4 5.6c0 3-1.4 4.4-1.4 4.4h1.6 M6.8 12.5a1.3 1.3 0 0 0 2.4 0 M2.5 2.5l11 11",
    check: "M3 8.5 6.5 12 13 4.5",
    chevron: "M5.5 3 10.5 8l-5 5",
    spark: "M8 1.5 9.6 6l4.4 1.6L9.6 9.2 8 13.7 6.4 9.2 2 7.6 6.4 6z",
    diamond: "M8 1.5 14.5 8 8 14.5 1.5 8z",
  };
</script>

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
