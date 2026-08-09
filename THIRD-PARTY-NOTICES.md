# Third-party notices

Concord itself is AGPL-3.0-or-later (see `LICENSE`). It also redistributes
third-party code and artwork under their own licences, listed here.

Each entry names the upstream project, the copyright holder, the licence, and
what Concord changed. Where an upstream licence text has to travel with the
files, a copy sits next to them; those files are pointed at from each entry.

Ordinary Go and npm dependencies are resolved at build time rather than
committed, so they are not enumerated one by one here — `go.mod`/`go.sum` and
`frontend/package-lock.json` are the exact manifest, pinned by hash. The two npm
packages that end up compiled into the shipped web bundle are noted at the end.

That is the right answer for the *source*, and an incomplete one for the
*binaries*. Concord links its Go dependencies statically, so a downloaded
release contains code under MIT, BSD-2-Clause, BSD-3-Clause and Apache-2.0, and
each of those licences asks that its copyright notice travel with the copy. The
dependency tree is machine-readable, so the notice can be too:

```sh
go install github.com/google/go-licenses@latest
go-licenses report ./... > NOTICES-go.txt     # every module, licence and holder
```

Anyone redistributing a built Concord binary should generate that and ship it
alongside. The maintainers' own releases are covered by the source being
published under AGPL-3.0-or-later at the URL in the binary, but "you could go
and look" is a weaker position than handing someone the list.

---

## Twemoji (emoji SVGs)

- **Upstream:** [jdecked/twemoji](https://github.com/jdecked/twemoji), the
  community continuation of Twitter's Twemoji. Obtained through the npm package
  [`@twemoji/svg`](https://github.com/boywithkeyboard/twemoji_svg) v15.0.0,
  which republishes the optimized SVGs.
- **Copyright (graphics):** © 2014–2021 Twitter, Inc; © 2022–present Jason
  Sofonia & Justine De Caires, and other contributors.
- **Licence (graphics):** CC BY 4.0 —
  https://creativecommons.org/licenses/by/4.0/
- **Copyright/licence (packaging):** the `@twemoji/svg` package declares MIT
  (© 2023 Samuel Kopp). That covers the packaging around the images. The
  images are the CC BY 4.0 work and stay under it regardless of how they are
  packaged, which is why this entry lists both.
- **Where:** `frontend/public/twemoji/`. Not committed — the `prep-twemoji`
  script in `frontend/package.json` copies the SVGs out of `node_modules` on
  every `npm run build` and `npm run dev`, so they exist in build output and in
  release artifacts but not in the source tree.
- **Concord's changes:** none. The SVGs are copied byte-for-byte.

jdecked's Twemoji accepts attribution in a project README, an About section, or
a footer. This file, shipped at the root of the source distribution, is that
attribution; the app has no About screen linking to it yet.

## Noto Animated Emoji

- **Upstream:** [Noto Animated
  Emoji](https://googlefonts.github.io/noto-emoji-animation/), fetched from
  `fonts.gstatic.com/s/e/notoemoji/latest/<codepoints>/512.webp`.
- **Copyright:** © Google Inc.
- **Licence:** CC BY 4.0 — https://creativecommons.org/licenses/by/4.0/
  (upstream's own FAQ: "Animated Noto Emoji is licensed under CC BY 4.0").
- **Where:** `frontend/public/anemoji/` (committed), licence notice in
  `frontend/public/anemoji/LICENSE`. Built by
  `frontend/scripts/prep-anemoji.mjs`.
- **Concord's changes:** each animation is downscaled from 512x512 to 80x80;
  every second frame is dropped and the per-frame delay doubled so the timing
  is unchanged at half the frame rate; re-encoded as WebP at quality 38. Files
  are renamed to the codepoint spelling the app's emoji renderer looks up
  rather than Noto's. Only a curated subset of roughly 150 emoji is included.

## Bundled typefaces

Eight families in `frontend/public/fonts/`, all **SIL Open Font License 1.1**
(https://openfontlicense.org). Full licence text: `frontend/public/fonts/OFL.txt`.
Per-family detail: `frontend/public/fonts/README.md`.

| Family | Upstream | Copyright | Reserved Font Name |
|--------|----------|-----------|--------------------|
| Inter | https://github.com/rsms/inter | Copyright 2020 The Inter Project Authors | none |
| Space Grotesk | https://github.com/floriankarsten/space-grotesk | Copyright 2020 The Space Grotesk Project Authors | none |
| Source Serif 4 | https://github.com/adobe-fonts/source-serif | Copyright 2014 The Source Serif 4 Project Authors | none |
| JetBrains Mono | https://github.com/JetBrains/JetBrainsMono | Copyright 2020 The JetBrains Mono Project Authors | none |
| Atkinson Hyperlegible | https://www.brailleinstitute.org/freefont/ | Copyright 2020 Braille Institute of America, Inc. | none |
| Nunito | https://github.com/googlefonts/nunito | Copyright 2014 The Nunito Project Authors | none |
| Chakra Petch | https://github.com/m4rc1e/Chakra-Petch | Copyright 2018 The Chakra Petch Project Authors | none |
| Comic Neue | https://github.com/crozynski/comicneue | Copyright 2014 The Comic Neue Project Authors | none |

Copyright lines are quoted from each family's `OFL.txt` in the Google Fonts
repository (`google/fonts/ofl/<family>/OFL.txt`), the distribution these files
come from. No family in the set declares a Reserved Font Name.

- **Concord's changes:** `frontend/scripts/prep-fonts.mjs` keeps only the
  `latin` and `latin-ext` subsets of each Google Fonts `.woff2` and renames the
  files to the app's internal font ids. Glyphs, metrics, and font naming are
  untouched. Subsetting makes these Modified Versions under the OFL, hence the
  licence text travelling alongside them.

## mls-go

- **Upstream:** https://github.com/thomas-vilte/mls-go, an RFC 9420 (MLS)
  implementation in Go. Vendored at **v1.5.0**.
- **Copyright:** © 2026 Thomas Vilte.
- **Licence:** MIT — https://opensource.org/licenses/MIT. Text preserved at
  `third_party/mls-go/LICENSE`.
- **Where:** `third_party/mls-go/`, wired in by a `replace` directive in the
  root `go.mod`.
- **Concord's changes** (everything else is upstream v1.5.0 verbatim):
  - Added `client_concord_ed25519.go` — a `WithEd25519SignatureKey` client
    option. Upstream generates a random signature key per client and never
    reloads it, so after a restart a member signs with a key its leaf is not
    bound to and peers reject its messages. Intended to go upstream.
  - `client.go` — the `clientConfig` field and `NewClient` branch that the
    option above needs.
  - `storage/file/file.go` — `syncDir` skips the directory `fsync` on Windows,
    which rejects it with "Access is denied"; the temp file is already synced
    and atomically renamed, so nothing is lost.
  - Removed `interop/server` and `interop/testrunner` (upstream's cross-library
    interop harness, which Concord does not build).

## modernc.org/libc overlay

- **Upstream:** `modernc.org/libc` — https://gitlab.com/cznic/libc, the pure-Go
  C runtime `modernc.org/sqlite` compiles against. Forked at **v1.74.1**.
- **Copyright:** © 2017 The Libc Authors. All rights reserved.
- **Licence:** BSD-3-Clause. Text at `third_party/_libc-overlay/LICENSE`,
  copied verbatim from the upstream module.
- **Where:** `third_party/_libc-overlay/syscall_musl.go` — one patched file,
  applied over a scratch copy of the module during the `android-core` Makefile
  target only. Desktop and server builds use the unmodified upstream module.
- **Concord's changes:** adds `androidRemap`, which rewrites the legacy x86_64
  path syscalls (`open`, `stat`, `mkdir`, …) that musl-derived code issues into
  their modern `*at` equivalents, because Android's seccomp filter denies the
  legacy numbers and kills the process with SIGSYS as soon as sqlite touches a
  file. Guards are added at the top of `___syscall_cp` and `X__syscall1`
  through `X__syscall6`. The remap compiles away on every architecture but
  amd64. Details in `third_party/_libc-overlay/README.md`.

## Meme templates

The meme editor takes an optional pack of 101 image-macro templates sourced from
imgflip's public catalogue and re-encoded to WebP. They are third-party
photographs and artwork, several with identifiable commercial rightsholders, and
Concord claims no rights in them and holds no licence to them.

For that reason **the images are not in this repository and not in any release**.
`frontend/scripts/prep-memes.mjs` builds the pack into
`frontend/public/memes/` on the machine that runs it; only the README describing
the position is tracked. The pack is optional either way — deleting the
directory costs nothing and breaks nothing. See
`frontend/public/memes/README.md` for the full position.

## npm packages compiled into the web bundle

Not committed to this repository, but present in built artifacts:

- **jsQR** v1.4.0 — https://github.com/cozmo/jsQR — Apache-2.0
- **node-qrcode** v1.5.4 — https://github.com/soldair/node-qrcode — MIT

Both are used unmodified.
