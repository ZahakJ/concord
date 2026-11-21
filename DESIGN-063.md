# Concord 0.63 design direction

Concord is a place you talk with people you trust, with no server in the
middle. The 0.63 pass gives it a face that says so: calm chrome around the
conversation, and a few stages where the app is allowed to be cinematic. It
takes its standard of finish from Astrolabe (one token sheet, a stage that
is the hero of a deck, motion that is named and never decorative) but not its
look: Astrolabe is ink and gold leaf on a page; Concord is signal light in a
dark room.

## Two registers

**Chrome** is everything you use for hours: lists, the feed, the composer,
menus, settings. It stays quiet. No glows, no gradients behind words, opaque
floating surfaces, sentence-case labels, the accent kept for "this is about
you". The rules in `frontend/src/lib/tokens.test.mjs` hold, unchanged.

**Stages** are moments, each seen for seconds: unlocking, first run, what's
new, the media viewer, empty conversations. A stage owns the screen, has one
focal point, and may use the aurora, large display type and a choreographed
entrance. A stage never carries a security warning in decorative type: the
warning keeps the chrome's ink, weight and contrast, wherever it sits.

## Palette

The grounds, inks and status colours of 0.62 stay; they are measured by the
contrast tests in both themes and all packs. 0.63 adds the stage palette.

| Token | Dark | Light | Use |
|---|---|---|---|
| `--aurora-1` | `#19b3a3` teal | `#0e7c70` | the brand light, always present |
| `--aurora-2` | `#2f8fe0` signal blue | `#1f63b0` | the cool side of a stage |
| `--aurora-3` | `#8a6cf6` violet | `#5b3fc4` | depth, never alone |
| `--aurora-spark` | `#f3c06b` | `#a8640f` | one highlight per stage at most |
| `--stage-ground` | `#060808` | `#f4f6f3` | the floor a stage is painted on |
| `--stage-ink` | `#f2f5f4` | `#0f1314` | headline ink on a stage (AA on the ground and on the aurora's brightest point behind text) |
| `--stage-veil` | 72% ground | 78% ground | the panel behind stage body text |

The aurora is drawn as two or three large blurred radial fields
(`--aurora` is the composed `background-image`), transformed slowly on the
compositor and never behind body text without `--stage-veil` under the text.
It is the accent's hue family, so a user's accent preset still reads as part
of it; packs leave it alone.

## Type

UI face Inter, display face **Space Grotesk** (both bundled, OFL, no runtime
fetch; the licence is `frontend/public/fonts/OFL.txt`). Arabic falls back to
the bundled Noto faces. The display face replaces the serif for titles that
name a place; the serif stays a Typeface choice.

| Token | Size | Use |
|---|---|---|
| `--fs-hero` | `clamp(34px, 4.2vw + 12px, 60px)` | one per stage: the unlock greeting, the release title |
| `--fs-headline` | `clamp(24px, 1.6vw + 16px, 32px)` | a stage's section titles, feature names |
| `--fs-display` | 22px | dialog and place titles (unchanged) |
| `--fs-title` … `--fs-micro` | 17 → 11px | chrome, unchanged |

Hero and headline set `letter-spacing: -0.02em` and `line-height: 1.05–1.15`;
nothing at or below `--fs-body` is tracked. No uppercase eyebrows: a kicker is
`--fs-small`, 600, in `--accent-hover` ink.

## Space and shape

The 4px scale (`--sp-1` … `--sp-6`) stays; stages add `--sp-7: 48px` and
`--sp-8: 72px`. Radii: `--radius-sm 6 / md 10 / lg 14` unchanged, plus
`--radius-xl` (lg + 8 = 22px) for stage cards, the viewer's info sheet and the
media picker's tiles, and `999px` for pills. Every radius is derived, so the
Shape setting still reaches all of them.

## Elevation

Three steps, each one opaque surface plus a shadow; the order is the stacking
ladder in `lib/layers.js`.

| Step | Token | Use |
|---|---|---|
| 0 | none | the page, the feed |
| 1 | `--shadow-rest` | a card that sits on the page (a media tile on hover, a what's new card) |
| 2 | `--shadow-pop` | menus, popovers, toasts, sheets (unchanged) |
| 3 | `--shadow-stage` | dialogs and stages: a long soft drop that reads as distance |

## Motion

Transform and opacity only; colour and filter fades are allowed; nothing
animates layout. Under `:root[data-motion="reduced"]` every duration and delay
is zero and every travel amount is identity, so a stage appears in its final
frame.

| Token | Value | Use |
|---|---|---|
| `--dur-enter` / `--dur-quick` / `--dur-standard` / `--dur-move` | 90 / 120 / 150 / 180ms | chrome, unchanged |
| `--dur-stage` | 420ms | a stage arriving: the viewer opening from its thumbnail, the release deck |
| `--dur-reveal` | 560ms | a staggered reveal's per-item time on a stage |
| `--stagger` | 55ms | the step between items in a reveal, capped at 8 items |
| `--ease-spring` | `cubic-bezier(0.16, 1, 0.3, 1)` | every stage arrival (decelerates, no overshoot) |
| `--ease-in` | unchanged | leaving: quicker than arriving (about 60% of the time) |
| `--ease-overshoot` | unchanged | celebrations only (bursts, the join veil) |

The media viewer's open is a FLIP from the thumbnail's rect (`transform`
only), its close returns there; a drag to dismiss follows the finger and the
backdrop fades with the distance. A stage holds 60 fps on a mid phone because
it moves at most three composited layers at once, and blur is a static
property of a layer, never animated.

## Iconography

One set: `Icon.svelte`, drawn on a 16px grid with a 1.5px round-capped stroke,
`currentColor` only. Sizes 16 (inline), 18 (toolbar), 20 (composer, sheets),
24 (stage controls). Icons sit in a square hit area of at least `--tap-min`.
No emoji as chrome icons. Stage illustrations (release heroes, empty states)
are inline SVG compositions in the same stroke language, scaled up, filled
with the aurora; they ship in the bundle and are `aria-hidden` with the
meaning carried by text beside them.

## Accessibility bars

WCAG AA in both themes for every text and for control edges (3:1); the
contrast tests measure the chrome tokens and the stage tokens. Focus is the
one ring (`--focus-ring`), on stages too. Every stage is reachable and
dismissable by keyboard (Esc, arrows, Tab order that matches the reading
order), and every gesture has a button.
