// decorations.js — things you WEAR, as opposed to rings you sit inside.
//
// A ring (lib/rings.js) is built from gradients: a spinning disc, an orbiting
// dot, a halo, a weather layer. That vocabulary can only ever produce rings,
// which is why every option looked like a variation of the same option. A
// decoration is a drawn FIGURE — ears, horns, a crown, a jaw closing over your
// face — and it is what makes an avatar look like a character instead of a
// circle with a gradient behind it.
//
// Drawn, never downloaded. Nothing in Concord is fetched at runtime, so these
// are paths, not images. That constraint turns out to be the advantage: a path
// recolours to the wearer's own palette, stays sharp from a 20px member row to
// a 96px profile card, animates procedurally rather than as a baked loop, and
// costs a couple of hundred bytes.
//
// ── THE AUTHORING CONTRACT ──────────────────────────────────────────────────
//
// Geometry. Every decoration is authored in a `0 0 100 100` viewBox. The avatar
// is the circle centred at (50,50) with r=36 — so its top edge is y=14, its
// bottom y=86, and the corners of the box are free space for things that stick
// out. Anything drawn outside the box is clipped; keep within 2..98.
//
// Layering. Each part declares `z`:
//   "back"  — behind the avatar (wings, a glow, a tail curling out)
//   "front" — over the avatar (ears overlapping the crown of the head, a jaw)
// Parts render in array order within their layer.
//
// Colour. Never hardcode a colour that ought to be the wearer's. Use:
//   "c1" / "c2"  the wearer's two profile colours
//   "ink"        a near-black outline that reads on any background
//   "light"      a soft highlight
// or a literal hex ONLY where the object's identity IS its colour — gold on a
// crown, red on a devil horn. A decoration that recolours is worth more than
// one that does not, because it composes with every theme pack.
//
// Motion. `anim` names a class from a fixed set, applied to parts carrying
// `a: true`. Adding a new one means adding CSS in AvatarDecoration.svelte:
//   twitch  small rotation, ears flicking
//   float   slow vertical drift, haloes
//   flap    wing beat
//   flicker opacity + scale jitter, flame
//   chomp   jaw open/close
//   sway    lazy side-to-side, tails
// Motion must degrade: prefers-reduced-motion stops all of it, and small
// avatars drop it too — forty animated decorations in a member list is forty
// animation timers for something 20 pixels tall.
//
// Reactivity is the thing a baked image cannot do. `reactive: true` marks a
// decoration whose animation is driven by what the wearer is doing rather than
// a timer (see AvatarDecoration's `state` prop). Start with none and earn them.
//
// Ids are on the wire. They are validated as [a-z0-9-]{1,32} (validDecoration,
// service.go) and looked up here; an unknown id renders NOTHING rather than
// failing, so a peer inventing one gets an ordinary avatar. Never make an id
// carry data.

const P = (d, o = {}) => ({ d, z: "front", fill: "ink", ...o });

export const DECORATIONS = [
  {
    id: "cat-ears",
    name: "Cat ears",
    group: "Creature",
    anim: "twitch",
    parts: [
      // Left ear: outer shell, then the inner cup, sitting on the head's curve.
      P("M17 33 L27 3 L45 25 Z", { fill: "c1", a: true, o: "l" }),
      P("M24 28 L29 12 L38 24 Z", { fill: "light", a: true, o: "l" }),
      P("M83 33 L73 3 L55 25 Z", { fill: "c1", a: true, o: "r" }),
      P("M76 28 L71 12 L62 24 Z", { fill: "light", a: true, o: "r" }),
    ],
  },
  {
    id: "fox-ears",
    name: "Fox ears",
    group: "Creature",
    anim: "twitch",
    parts: [
      P("M14 32 L24 1 L44 26 Z", { fill: "#d9762f", a: true, o: "l" }),
      P("M22 27 L26 10 L37 25 Z", { fill: "#fbe3c8", a: true, o: "l" }),
      P("M86 32 L76 1 L56 26 Z", { fill: "#d9762f", a: true, o: "r" }),
      P("M78 27 L74 10 L63 25 Z", { fill: "#fbe3c8", a: true, o: "r" }),
    ],
  },
  {
    id: "bunny-ears",
    name: "Bunny ears",
    group: "Creature",
    anim: "twitch",
    parts: [
      P("M33 26 C24 6 30 -4 39 0 C47 4 43 20 41 27 Z", { fill: "c1", a: true, o: "l" }),
      P("M36 22 C31 9 35 3 38 6 C41 9 40 18 39 23 Z", { fill: "light", a: true, o: "l" }),
      P("M67 26 C76 6 70 -4 61 0 C53 4 57 20 59 27 Z", { fill: "c1", a: true, o: "r" }),
      P("M64 22 C69 9 65 3 62 6 C59 9 60 18 61 23 Z", { fill: "light", a: true, o: "r" }),
    ],
  },
  {
    id: "devil-horns",
    name: "Devil horns",
    group: "Creature",
    parts: [
      P("M24 26 C18 16 20 6 27 3 C25 11 30 16 34 20 Z", { fill: "#c1362f" }),
      P("M76 26 C82 16 80 6 73 3 C75 11 70 16 66 20 Z", { fill: "#c1362f" }),
    ],
  },
  {
    id: "halo",
    name: "Halo",
    group: "Ethereal",
    anim: "float",
    parts: [
      {
        z: "front",
        a: true,
        el: "ellipse",
        attrs: { cx: 50, cy: 9, rx: 21, ry: 5.5 },
        stroke: "#ffd76a",
        width: 3.5,
        glow: 6,
      },
    ],
  },
  {
    id: "crown",
    name: "Crown",
    group: "Regalia",
    parts: [
      P("M22 26 L28 10 L38 20 L50 4 L62 20 L72 10 L78 26 Z", { fill: "#e9c25c" }),
      P("M22 26 L78 26 L76 31 L24 31 Z", { fill: "#c99f3d" }),
      { z: "front", el: "circle", attrs: { cx: 50, cy: 12, r: 3 }, fill: "c2" },
    ],
  },
  {
    id: "laurel",
    name: "Laurel",
    group: "Regalia",
    parts: [
      P("M24 66 C12 50 15 27 32 15 C24 32 24 50 31 64 Z", { fill: "#6d9a52" }),
      P("M22 54 C14 50 12 42 16 36 C21 41 23 47 24 53 Z", { fill: "#87b869" }),
      P("M25 40 C19 34 20 26 25 21 C28 27 28 34 27 40 Z", { fill: "#87b869" }),
      P("M76 66 C88 50 85 27 68 15 C76 32 76 50 69 64 Z", { fill: "#6d9a52" }),
      P("M78 54 C86 50 88 42 84 36 C79 41 77 47 76 53 Z", { fill: "#87b869" }),
      P("M75 40 C81 34 80 26 75 21 C72 27 72 34 73 40 Z", { fill: "#87b869" }),
    ],
  },
  {
    id: "wings",
    name: "Wings",
    group: "Ethereal",
    anim: "flap",
    parts: [
      P("M26 44 C6 22 -6 40 2 62 C6 52 8 74 18 76 C16 62 20 52 28 50 Z", { z: "back", fill: "light", a: true, o: "l" }),
      P("M74 44 C94 22 106 40 98 62 C94 52 92 74 82 76 C84 62 80 52 72 50 Z", { z: "back", fill: "light", a: true, o: "r" }),
    ],
  },
  {
    id: "flame",
    name: "Flame",
    group: "Elemental",
    anim: "flicker",
    parts: [
      P("M50 2 C60 14 66 20 62 28 C58 34 42 34 38 28 C34 20 40 14 50 2 Z", { fill: "#ef7d2a", a: true }),
      P("M50 12 C55 19 57 23 55 27 C52 31 48 31 45 27 C43 23 45 19 50 12 Z", { fill: "#ffd46a", a: true }),
    ],
  },
  {
    id: "shark",
    name: "Shark bite",
    group: "Creature",
    anim: "chomp",
    parts: [
      // A jaw arriving from below, teeth biting up over the chin.
      P("M2 100 C2 78 20 64 50 64 C80 64 98 78 98 100 Z", { fill: "#4a6f88", a: true, o: "jaw" }),
      P("M12 82 C12 72 28 66 50 66 C72 66 88 72 88 82 Z", { fill: "#e8f0f6", a: true, o: "jaw" }),
      P("M16 78 L22 66 L28 78 L34 66 L40 78 L46 66 L52 78 L58 66 L64 78 L70 66 L76 78 Z", {
        fill: "#ffffff",
        a: true,
        o: "jaw",
      }),
    ],
  },
];

export const DECORATION_BY_ID = Object.fromEntries(DECORATIONS.map((d) => [d.id, d]));

export const DECORATION_GROUPS = [...new Set(DECORATIONS.map((d) => d.group))].map((title) => ({
  title,
  ids: DECORATIONS.filter((d) => d.group === title).map((d) => d.id),
}));

// decoration resolves an id to its definition, or null. Fails CLOSED: an id
// this build does not know renders nothing at all, which is what makes it safe
// to take the value straight off a peer's profile.
export function decoration(id) {
  return DECORATION_BY_ID[id] || null;
}
