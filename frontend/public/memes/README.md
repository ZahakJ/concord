# Bundled meme templates

> **In a fresh clone this README is the only file in this directory.** The
> images are gitignored for the licensing reason set out below, so they are in
> neither the repository nor any release. `node frontend/scripts/prep-memes.mjs`
> builds them here. Everything below describes the pack that script produces.

These images are the starter pack for the meme editor (`/meme`): 101 templates,
2.1 MB, re-encoded as WebP at 600px. They are third-party photographs and
artwork, widely circulated as meme templates and sourced from imgflip's public
catalogue by `frontend/scripts/prep-memes.mjs`.

That script MERGES rather than overwrites. An entry already in the manifest keeps
its hand-placed caption boxes and hand-written tags, because those were checked
by rendering the template with sample text and looking at the result; a
regeneration that silently reset them would undo the only check that catches a
caption box sitting half off its panel. New templates arrive with no caption
preset at all — the editor's default top-and-bottom is right for an ordinary
image macro, and inventing panel coordinates for a template nobody has looked at
would place boxes wrongly, with confidence.

They are **not** original work and Concord claims no rights in them. Several
have identifiable commercial rightsholders — "Distracted Boyfriend" is a
licensed stock photograph, and the film and television stills belong to their
respective studios. Redistributing them inside a binary is ordinary practice for
meme tooling but is not the same thing as having a licence.

To drop the pack, delete this directory. The editor reads
`memes/manifest.json` at runtime and falls back to the bring-your-own card when
it isn't there, so removing it costs nothing and breaks nothing — bring-your-own
image, paste, and "Make a meme" on any picture in the conversation all keep
working.

## manifest.json

One entry per template:

```json
{
  "file": "30b1gx.webp",
  "label": "Drake",
  "tags": ["yes", "no", "prefer"],
  "captions": [{ "x": 0.73, "y": 0.25, "w": 0.48, "size": 0.055, "style": "caption" }]
}
```

`captions` is optional; without it a template opens with the usual top and
bottom boxes. Every value is a fraction of the image (x/y are the centre of the
box, `size` is a fraction of image *height*) so a template works at any
resolution. `style` is a key from `STYLES` in `src/lib/meme.js` — `impact` for
white-on-photo, `caption` for dark text on a white panel.

`tags` is what the gallery's search box matches on besides the label, and it is
the difference between finding Drake and not: almost nobody knows the template
with the two panels is *called* that, but everybody types "yes no". Every term
in a query has to match somewhere in the label or the tags.

Placements were checked by rendering every template through the real engine with
sample text and looking at the result, which is the only way to catch a caption
box that sits half off its panel.
