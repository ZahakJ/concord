<script>
  import AvatarRing from "./AvatarRing.svelte";
  import AvatarDecoration from "./AvatarDecoration.svelte";
  import { wornRing } from "./lib/decorations.js";
  // The one avatar. Renders, in priority order: uploaded image, profile emoji,
  // name/fingerprint initials — tinted by the member's accent color, with an
  // optional presence dot. Replaces five copy-pasted implementations.
  let {
    name = "",
    emoji = "",
    color = "",
    image = "",
    size = 32,
    online = null, // null hides the dot; true/false shows connection state
    presence = "", // "" | online | idle | dnd | invisible — shades the dot when connected
    frame = "", // gradient ring id — see lib/rings.js (snow, comet, orbit-cow…)
    style = null, // ring dials: { speed, dir, glow, width, sat } (lib/rings.js)
    color2 = "", // second theme color — the "theme" ring spins between the two
    // Anything worn on the avatar — ears, a crown, wings, a band of runes.
    // See lib/decorations.js. Independent of `frame`: a decoration composes
    // WITH a gradient ring rather than replacing it.
    decoration = "",
    // The colourway the decoration is painted in — a bounded id out of
    // lib/decorations.js COLORWAYS. Given as a prop for the call sites that
    // hand this component nothing but a member row, and read off `style`
    // otherwise, which is where it travels.
    dc = "",
    // Passed straight through to the decoration painter: let a picker tile
    // animate even at thumbnail size.
    preview = false,
  } = $props();

  const glyph = $derived(emoji || (name || "?").slice(0, 2));
  // Reset per image, so a component reused for a different person (the feed
  // recycles these) does not inherit the last one's failure.
  let failedSrc = $state("");
  const imageFailed = $derived(!!image && failedSrc === image);
  const cw = $derived(dc || style?.dc || "");

  // The drawn RINGS used to be their own library and used to travel in `frame`.
  // They are decorations now, but the ids never changed, so a `frame` naming
  // one is painted through the decoration painter. Anyone already wearing one
  // keeps it, with no migration and no rewrite of what they broadcast.
  //
  // `wornRing` and not a plain lookup, because only those twenty-one were ever
  // reachable from `frame`. Matching the whole decoration table here means a
  // figure that happens to share a name with a gradient ring takes that ring's
  // wearers with it — "comet" is in both libraries, and for a while the
  // gradient one could not be worn at all.
  const legacy = $derived(!!frame && wornRing(frame));

  // A circle around a squircle reads as a rendering bug, so app.css keeps the
  // circular silhouette for `.ringed` avatars only. That has to hold however
  // the ring arrived: as a gradient ring in `frame`, as a legacy drawn ring in
  // `frame`, or — now that the libraries are one — as a decoration in `dec`.
  const ringed = $derived(!!frame || wornRing(decoration));

  // Dot color: hidden when online is null; grey when disconnected or invisible;
  // otherwise the chosen availability (default online = green).
  const dotColor = $derived(
    online === null
      ? null
      : !online || presence === "invisible"
        ? "var(--text-faint)"
        : presence === "idle"
          ? "#f0b232"
          : presence === "dnd"
            ? "#f04747"
            : "var(--ok)",
  );
</script>

<span
  class="avatar"
  class:ringed={ringed}
  class:pictured={!!image && !imageFailed}
  style="width:{size}px;height:{size}px;font-size:{Math.max(10, Math.round(size * 0.38))}px;{color && !image
    ? `background:${color};`
    : ''}"
>
  <!-- A `frame` saved before the two libraries merged may name a drawn ring;
       it paints through the decoration painter. Anything else is a gradient
       ring (lib/rings.js). Both may be on at once — someone who set a drawn
       ring AND a decoration chose to wear two, and wears two. -->
  {#if legacy}
    <AvatarDecoration id={frame} {size} {color} {color2} {cw} {preview} />
  {:else if frame}
    <AvatarRing ring={frame} {size} {style} {color} {color2} />
  {/if}
  {#if decoration}
    <AvatarDecoration id={decoration} {size} {color} {color2} {cw} {preview} />
  {/if}
  {#if image && !imageFailed}
    <!-- A picture that will not decode falls back to the initials underneath
         it rather than to the browser's broken-image glyph. Ordinary profile
         pictures are validated on the way in, but an ARCHIVE carries author
         portraits copied straight out of somebody else's export: the importer
         checks the size and the declared type, never the pixels, so a truncated
         or mislabelled file reaches this img and there is nothing to be done
         about it after the fact — the manifest is signed. -->
    <img src={image} alt="" onerror={() => (failedSrc = image)} />
  {:else}
    {glyph}
  {/if}
  {#if dotColor}
    <span
      class="dot"
      class:live={online && (!presence || presence === "online")}
      style="background:{dotColor}"
    ></span>
  {/if}
</span>

<style>
  .avatar {
    position: relative;
    isolation: isolate; /* the ring sits behind the avatar, not the page */
    border-radius: 50%;
    /* The tint is the backdrop for INITIALS and an emoji — see .pictured below,
       which takes it away again once there is a picture to show. */
    background: var(--accent);
    color: var(--accent-fg);
    display: grid;
    place-items: center;
    font-weight: 600;
    text-transform: uppercase;
    flex-shrink: 0;
    user-select: none;
  }
  /* A picture gets no backplate. object-fit:cover fills the circle, so for an
     opaque image the tint was merely invisible — but a logo or avatar saved as a
     PNG with transparency showed the accent through its gaps, which reads as the
     app colouring in someone's picture. A guild icon on an announcement is the
     obvious case: it should look like a profile picture, not a badge. */
  .avatar.pictured {
    background: transparent;
  }
  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    border-radius: 50%;
    /* Avatars fade in instead of popping — lists feel settled while loading. */
    animation: img-in 0.18s ease;
  }
  @keyframes img-in {
    from {
      opacity: 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    img {
      animation: none;
    }
  }
  .dot {
    position: absolute;
    bottom: -1px;
    right: -1px;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    border: 2px solid var(--bg-1);
  }
  /* Online friends breathe: a slow soft glow on the green dot. The glow lives
     on a pseudo-element whose OPACITY animates — animating box-shadow itself
     re-paints the layer sixty times a second for every visible dot (a real
     cost on phones, where a member list can hold dozens), while an opacity
     fade composites on the GPU for free. */
  .dot.live::after {
    content: "";
    position: absolute;
    inset: -2px;
    border-radius: 50%;
    box-shadow: 0 0 6px 1.5px color-mix(in srgb, var(--ok) 45%, transparent);
    animation: dot-breathe 3.2s ease-in-out infinite;
  }
  @keyframes dot-breathe {
    0%,
    100% {
      opacity: 0;
    }
    50% {
      opacity: 1;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .dot.live::after {
      animation: none;
      opacity: 0;
    }
  }
</style>
