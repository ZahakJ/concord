<script>
  import AvatarRing from "./AvatarRing.svelte";
  import AvatarDecoration from "./AvatarDecoration.svelte";
  import { wornRing } from "./lib/wornrings.js";
  // The one avatar. Renders, in priority order: uploaded image, profile emoji,
  // name/fingerprint initials — tinted by the member's accent color, with an
  // optional presence dot or custom-status emoji. Replaces five copy-pasted
  // implementations.
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
    // Custom-status emoji, as a badge on the corner. A voice tile is where this
    // earns its keep: "I'm gaming" has to read at a glance, and the presence
    // dot yields to it because they share the same corner and the emoji is the
    // more specific thing.
    mood = "",
    moodTitle = "",
  } = $props();

  const glyph = $derived(emoji || (name || "?").slice(0, 2));
  const moodSize = $derived(mood ? Math.max(12, Math.min(28, Math.round(size * 0.4))) : 0);
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

  // Presence, by SHAPE as well as colour, so it reads without colour vision:
  // online is a filled disc, idle a crescent, do-not-disturb a disc with a bar,
  // offline (and invisible, which looks the same to everyone else) a hollow
  // ring. Hidden when `online` is null.
  const state = $derived(
    online === null
      ? null
      : !online || presence === "invisible"
        ? "off"
        : presence === "idle"
          ? "idle"
          : presence === "dnd"
            ? "dnd"
            : "on",
  );
</script>

<span
  class="avatar"
  class:ringed={ringed}
  class:pictured={!!image && !imageFailed}
  style="width:{size}px;height:{size}px;font-size:{Math.max(10, Math.round(size * 0.38))}px;{color &&
  (!image || imageFailed)
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
  {#if mood}
    <span class="mood" style="--mood:{moodSize}px" title={moodTitle || undefined} aria-hidden="true"
      >{mood}</span
    >
  {:else if state}
    <span class="dot {state}" class:small={size <= 24}></span>
  {/if}
</span>

<style>
  .avatar {
    position: relative;
    isolation: isolate; /* the ring sits behind the avatar, not the page */
    overflow: visible;
    border-radius: 50%;
    /* The tint is the backdrop for INITIALS and an emoji — see .pictured below,
       which takes it away again once there is a picture to show. A picture that
       FAILED to load is not one: the initials come back, so the colour has to
       come back with them (an archived author whose manifest avatar 404s was
       drawn on the default accent while everyone beside them wore their own). */
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
  /* 10px (8 on a small avatar) with a 2px cut-out in the colour of the
     surface the avatar sits on. --avatar-cut lets a caller on another ground
     (the feed, a dialog) name it; the sidebar's is the default. */
  .dot {
    position: absolute;
    bottom: -2px;
    right: -2px;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    border: 2px solid var(--avatar-cut, var(--bg-1));
    background: var(--ok);
  }
  .dot.small {
    width: 12px;
    height: 12px;
  }
  .dot.idle {
    background: radial-gradient(circle at 28% 28%, var(--avatar-cut, var(--bg-1)) 0 36%, var(--warn) 38%);
  }
  .dot.dnd {
    background: var(--danger);
  }
  .dot.dnd::after {
    content: "";
    position: absolute;
    left: 20%;
    right: 20%;
    top: 50%;
    height: 2px;
    margin-top: -1px;
    border-radius: 1px;
    background: var(--avatar-cut, var(--bg-1));
  }
  .dot.off {
    background: var(--avatar-cut, var(--bg-1));
    box-shadow: inset 0 0 0 2px var(--text-faint);
  }
  .mood {
    position: absolute;
    bottom: -1px;
    right: -1px;
    width: var(--mood);
    height: var(--mood);
    border-radius: 50%;
    display: grid;
    place-items: center;
    font-size: calc(var(--mood) * 0.72);
    font-weight: 400;
    line-height: 1;
    text-transform: none;
    background: var(--bg-1);
    box-shadow: 0 0 0 2px var(--bg-1);
    z-index: 2;
  }
</style>
