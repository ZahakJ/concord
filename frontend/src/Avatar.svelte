<script>
  import AvatarRing from "./AvatarRing.svelte";
  import AvatarDecoration from "./AvatarDecoration.svelte";
  import { drawnFrame } from "./lib/frames.js";
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
    frame = "", // decorative ring id — see lib/rings.js (snow, comet, orbit-cow…)
    style = null, // ring dials: { speed, dir, glow, width, sat } (lib/rings.js)
    color2 = "", // second theme color — the "theme" ring spins between the two
    // A worn figure (ears, crown, wings) — see lib/decorations.js. Independent
    // of `frame`: a decoration composes WITH a ring rather than replacing it,
    // which is the point of splitting them.
    decoration = "",
    // Passed straight through to the decoration painter: let a picker tile
    // animate even at thumbnail size.
    preview = false,
  } = $props();

  const glyph = $derived(emoji || (name || "?").slice(0, 2));

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
  class:ringed={!!frame}
  class:pictured={!!image}
  style="width:{size}px;height:{size}px;font-size:{Math.max(10, Math.round(size * 0.38))}px;{color && !image
    ? `background:${color};`
    : ''}"
>
  <!-- One `frame` value, two libraries: a drawn frame (lib/frames.js) renders
       through the decoration painter, anything else is a gradient ring
       (lib/rings.js). Old ring ids keep working untouched. -->
  {#if frame && drawnFrame(frame)}
    <AvatarDecoration id={frame} kind="frame" {size} {color} {color2} {preview} />
  {:else if frame}
    <AvatarRing ring={frame} {size} {style} {color} {color2} />
  {/if}
  {#if decoration}
    <AvatarDecoration id={decoration} {size} {color} {color2} {preview} />
  {/if}
  {#if image}
    <img src={image} alt="" />
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
