# Interface

How Concord's interface is put together: the principles it follows, the tokens every component reads, the shared components, motion, density and the rules for words on screen. It is the reference for anyone changing the Svelte frontend. The values live in `frontend/src/app.css`; `frontend/src/lib/tokens.test.mjs` holds them in place.

The interface is partway through a redesign. Everything below describes what the code does today, except where a paragraph is marked **Planned**: those describe the layout and screens the redesign is building towards, so a change made now can head the same way.

## Principles

Concord should read like a well-set page. The conversation is the reason the window is open, so it gets the width and the attention, and everything around it is quiet.

1. **Content first.** Chrome never takes room the content needs. If a panel and the conversation compete for width, the panel gives way.
2. **One accent, for state.** The accent colour marks focus, selection, the primary action, mentions of you, your own reactions, links and the "New" divider. It is never decoration: not a heading colour, not an icon at rest, not a glow.
3. **A number means "this needs you".** Counts appear for mentions, direct messages and replies to you. Anything else that is unread is shown by weight or a dot.
4. **Type does the hierarchy.** Size and weight first, then the two ink tiers, borders last. Labels are sentence case. Nothing is set in capitals for emphasis.
5. **Three surfaces.** The sidebar, the canvas, and things that float. Hover, selection and fields take one more step. That is all the depth there is.
6. **Only floating things have shadows.** A menu, a dialog, a sheet or a toast casts one shadow. Nothing laid out in the page does.
7. **Interaction never moves what you are reading.** Toolbars and indicators float or use space reserved for them.
8. **One of each.** One dialog frame, one empty state, one nudge on screen at a time, one call surface at a time, one way to show an error.
9. **Say it like a person.** No raw error from the core ever reaches the screen.
10. **The keyboard reaches everything.** Menus take focus, Escape closes the top layer, and focus returns to whatever opened it.

Light and dark are designed separately — paper and graphite — rather than one being the inverse of the other. Every word that carries information clears 4.5:1 against the surface it sits on, in both; a disabled control is exempt and says why it is disabled.

## Tokens

Components read tokens and never repeat their values. A literal that is byte-identical to a token fails `npm test`.

### Surfaces and lines

| Token | Dark | Light | Used for |
|---|---|---|---|
| `--bg-0` | `#0d0f10` | `#e4e6e1` | wells: code blocks, slider grooves |
| `--bg-1` | `#121516` | `#eaece6` | sidebar, member panel, tab bar |
| `--bg-2` | `#171a1c` | `#fcfcfb` | the canvas: conversations and pages |
| `--bg-3` | `#212528` | `#dcdfd9` | hover, selected rows, quiet buttons |
| `--bg-input` | `--bg-3` | `#ffffff` | text fields |
| `--bg-elevated` | `#1d2124` | `#ffffff` | dialogs, menus, popovers, sheets, toasts; always opaque |
| `--border` | `#30363a` | `#d3d7d1` | the edges of floating surfaces, outlined chips |
| `--border-strong` | `#6a7277` | `#7a817d` | edges that identify a control: fields, the switch's off track, the selected segment (3:1 or better) |
| `--hairline` | `--border` at 62% | same | dividers between regions and rows |
| `--scrim` | `rgba(4,6,7,.62)` | `rgba(22,25,26,.32)` | behind dialogs; `--scrim-sheet` (`.40` in light) behind phone sheets |

### Ink

| Token | Dark | Light | Lowest contrast where it is allowed |
|---|---|---|---|
| `--text` | `#eceeef` | `#16191a` | 12.0:1 / 13.1:1 |
| `--text-muted` | `#a4abb0` | `#474f54` | 6.0:1 / 6.2:1 |
| `--text-faint` | `#8f979d` | `#576064` | 4.7:1 / 4.8:1 |
| `--accent-hover` (the accent as ink) | teal mixed toward white | teal mixed toward black | 5.9:1 / 5.5:1 |

`--text-faint` is a text tier: timestamps, metadata and hints use it. The token test measures it on every surface, the hover fill included, in both themes and in every theme pack. Placeholders use `--text-muted`.

### Accent and status

`--accent` is the user's own accent: the brand teal, `#14a394` in dark and `#0e7c70` in light (where it carries white text), or a preset they chose. It does not follow the profile colour unless the user picks "Match my profile colour", and then a hue in the red–orange band is desaturated so a "you" signal never reads as an alert. Badges and counts wear it and are never red: red is for danger and errors, and the token test fails a count filled with `--danger`.

`--danger`, `--ok` and `--warn` are fills with their own `-fg` inks; `--danger-text`, `--ok-text` and `--warn-text` are the same colours mixed toward `--text` for use as ink. `--mention-wash` is the tint for a message that mentions you, and is never drawn without an accent bar beside it.

A guild's own colour is `--guild-accent` (`lib/guildaccent.js`), set only on that guild's own marks — its strip tile, its switcher row, its place-header icon — so it never changes how your own mentions, badges and focus look. Your accent is held legible: `applyAccent` walks a weak or greyish choice toward white (dark) or black (light), keeping its hue, until the accent as ink clears 4.5:1 on the canvas and the badge fill clears 3:1 on the sidebar.

### Shape, elevation and focus

Radii: `--radius-sm` 6px, `--radius-md` 10px, `--radius-lg` 14px, and `--radius-sheet` 18px; the Shape setting in Appearance moves all of them. `--shadow-pop` is the one shadow for floating surfaces. There are no glows: `--accent-glow` resolves to `none` and `--float-shadow` to `--shadow-pop`, and the token test fails any component that paints with either. `--focus-ring` is `2px solid var(--accent-hover)`. It is drawn only once the person has moved through the app with the keyboard (Tab, F6, Escape, arrows outside a text field, or a shortcut; `lib/inputmode.js` marks `<html data-kbd>`), and a pointer press turns rings off again, so a dialog opened with the mouse moves focus without painting one. Typing in a field does not count.

### Type

The default face is Inter, served from the app's own `public/fonts`; no font is ever fetched from anywhere else, and "System" remains a choice in Appearance. Code uses JetBrains Mono. `--display-font` is the bundled Source Serif 4, for headlines that name a place (the empty states use it today). Its stack ends in a sans-serif generic on purpose: a serif generic breaks keycap emoji on Android. Choosing a typeface in Appearance, or a theme pack, replaces the display face with the interface face. Arabic falls through to the bundled Noto faces.

| Token | Desktop | Phone | Use |
|---|---|---|---|
| `--fs-micro` | 11px | 11px | badges, counters, key chips |
| `--fs-tiny` | 11.5px | 12px | timestamps, metadata |
| `--fs-small` | 12px | 12.5px | section labels, secondary lines |
| `--fs-compact` | 12.5px | 13px | descriptions, toasts, nudges |
| `--fs-ui` | 13px | 14px | default interface text |
| `--fs-body` | 15.5px | 16px | message and post content |
| `--fs-title` | 17px | 19px | pane titles |
| `--fs-display` | 22px | 24px | empty-state headlines |

Nothing is set below 11px. Times, counts and tables use tabular figures.

### Space, layout and layers

Spacing is the 4px rhythm `--sp-1` to `--sp-6` (4 to 32px), with `--sp-edge` 16px on desktop and 14px on a phone. `--header-h` (52px), `--sidebar-w` (280px) and `--inspector-w` (320px) are the layout defaults the redesigned shell is built on. Message text is capped at `--measure`, 76 characters. Stacking uses the layer scale in `lib/layers.js` (`--z-sticky` to `--z-tooltip`); a menu opened from inside a dialog takes the dialog's layer plus one.

### Breakpoints

The layout is chosen by width alone. `detectMobile()` in `lib/state.svelte.js` is `(max-width: 768px)`, and every phone rule in the stylesheets uses exactly that query, so the script and the CSS never disagree. A coarse pointer — a tablet, a touchscreen laptop — at a wider window keeps the desktop layout: `(pointer: coarse)` may enlarge targets (`--tap-min` is 44px there, 48px on a phone) and show controls that would otherwise wait for hover, and the token test fails a coarse query that changes `display`, `grid-template`, `flex-direction` or `width`. The other tiers are `(max-width: 1150px)` for the squeezed desktop column and `(max-width: 400px)` for the narrowest phones.

### Theme packs

A theme pack may set surfaces, lines (including `--border-strong`), ink, accent and status colours, the three font tokens, radii, the avatar shape, tracking, the floating shadow, the scrim, the message personality tokens (`--msg-surface`, `--msg-surface-hover`, `--msg-pad-x`, `--msg-rule`, `--msg-rule-pad`), the feed rhythm and its own backdrop tokens. It may not set the type scale, motion, the layer scale, layout sizes, `--mention-wash` or the focus ring. Every pack keeps `--bg-elevated` opaque and both lighter ink tiers at 4.5:1 on all of its own surfaces. `frontend/scripts/pack-retune.mjs` measures that; run with `--write`, it walks a short tier toward `--text` (keeping its hue) until it clears, and gives a pack without one its own `--border-strong`.

## Components

**Buttons** come in six kinds, as classes on a `<button>`: `primary` (accent fill, one per surface), `quiet` (a `--bg-3` fill), `ghost` (an outline, for Cancel; `ghost danger` outlines it in the danger ink), `danger` (for a destructive action that has been confirmed), `link` (accent text inside a sentence) and `iconbtn` (a lone glyph, 32px; 40px under a coarse pointer, 44px on a phone). `sm` and `lg` make a kind 28 or 40px tall. A bare `<button>` has no fill and takes its ink from where it sits; give it a kind. Disabled buttons are at 45% and should say why, next to them or in a tooltip.

**Fields** sit on `--bg-input` with a `--border-strong` edge. Focus turns the edge to the accent with a soft halo of it; `aria-invalid="true"` turns the edge red. Put the reason under the field and tie it with `aria-describedby`. `Select.svelte` is the same field with a chevron, opening a menu.

**Menus** — `Menu.svelte`, `Select.svelte` and `ContextMenu.svelte` — share one look: an opaque raised surface, 32px rows with a 16px muted icon and the label in ink, hairline separators, destructive rows in the danger ink. A menu has exactly one active row: the pointer's row and the keyboard's row are the same highlight, and a menu driven from the keyboard also rings its active row in the accent. A context-menu item may carry a `kbd` hint, drawn with `Kbd`.

**Switches** (`Switch.svelte`) are 36×20: off is a recessed track with a `--border-strong` edge and a muted knob, on is the accent with a white knob. A settings row is a label for its switch, never a button that contains other buttons.

**Segmented controls** (`Segmented.svelte`) choose one of two to four options, all visible. The chosen segment is lighter than the track in both themes, ringed in `--border-strong` and set in full ink at 600, so it never relies on colour alone. It is a radio group: one tab stop, and the arrow keys move and choose.

```svelte
<Segmented label="Message density" value={density} onChange={setDensity}
  options={[{ value: "compact", label: "Compact" }, { value: "cozy", label: "Cosy" }]} />
```

**Tabs** (`Tabs.svelte`) are words over a hairline, with a 2px ink underline under the selected one, which is also set in full ink. An optional count after a label is quiet. One tab stop; the arrow keys move between tabs.

```svelte
<Tabs label="Sounds" value={tab} onChange={(id) => (tab = id)}
  tabs={[{ id: "shelf", label: "Shelf" }, { id: "make", label: "Make one" }]} />
```

**Badges** (`Badge.svelte`): a count is the accent with `--accent-fg` ink, 16px high, "99+" past its maximum; a quiet unread mark is a 6px dot in `--text`; a muted place with a mention gets an outlined count. `ring` cuts the mark out of the surface beneath it.

```svelte
<Badge count={mentions} ring="var(--bg-1)" />   <Badge dot />   <Badge count={2} muted />
```

**Keys** (`Kbd.svelte`) are small outlined chips: faint at the end of a menu row, full ink (`strong`) in the shortcut sheet, monospaced with their spaces kept (`typed`) for things you type rather than press.

**Avatars** are circles unless a theme pack says otherwise. Presence is told by shape as well as colour, cut into the corner in the colour of the surface beneath (`--avatar-cut`, the sidebar's by default): a filled disc for online, a crescent for away, a barred disc for do not disturb, and a hollow ring for offline — which is also how an invisible member looks to everyone else.

**Sheets** (`BottomSheet.svelte`) rise from the bottom of a phone with a handle and grouped rows, over their own lighter scrim, and settle without bouncing.

**Toasts** sit at the bottom right on a desktop, clear of the composer, and under the top bar on a phone, at most three at once. Every kind uses the same raised surface; the kind is carried by the icon's ink alone.

**Nudges** (`Nudge.svelte`) ask for one thing: an icon, one sentence, one action and a dismiss. One is shown at a time. In a list it is a 40px row on `--bg-3`; `floating` gives it the raised surface for a caller that has to lay it over the app. The sidebar's nudge row, at the top of its list, holds the one on screen, chosen by priority: the recovery phrase, then a guild owner's setup steps, then tips (notifications, an update). Losing the connection is not a nudge: it is the one full-width bar above the app.

**Empty states** (`EmptyState.svelte`) are one pattern: an outlined mark, a headline that names the place, a sentence or two about what goes here, up to two actions and an optional meta line. They are left-aligned; where they sit is the parent's choice. `compact` is the size for panels and small dialogs.

**The conversation.** A message row is avatar, then the author line (name in text ink, time in the faint tier), then a reply's quote under the author line, then the body capped at `--measure`. Names take their role's colour only when Appearance's "Colour names by role" is on, mixed toward the ink until they clear 4.5:1. A message that mentions you gets the `--mention-wash` and a 2px accent bar on its start edge. Dividers are left-aligned labels with a hairline: days in the display face (with the year for any day outside this one), and "New" in your accent. The hover toolbar floats over the row's corner and moves nothing: three reactions, Add reaction, Reply, Start a thread and ⋯; holding Shift adds Edit, Pin and Delete. A focused row answers R, T, E, P, S and Delete. Messages longer than thirty lines fold to twenty behind "Show more".

**The composer** is one surface: + (everything you can add: a file, a GIF, a voice message, a poll, a doodle, a sound, a game, a thread, scheduling, sealing, the full formatting bar, the advanced composer), the text, emoji and Send. There is no permanent formatting bar; selecting text raises a small toolbar for bold, italic, strike, code, spoiler and link, and the format chords work everywhere. The reply bar and the attachment tray sit inside the surface. A 24px strip above it holds "Juno is typing…" and is there, empty, when nobody is. Editing an older message turns its body into a plain box.

**The header** has four controls: search (a field in place once used), the call button, the inspector toggle (people glyph and member count, Ctrl+U) and ⋯ for everything else. **The inspector** has Members, Pins, Files and Threads tabs; docked it is a flat column on `--bg-1`, as an overlay it is a lifted panel below the header. **The palette** (Ctrl+K) shows Unread, Recent and four actions when empty, and one ranked list with the matched characters in bold once you type.

**Dialogs** are one frame, `modals/Modal.svelte`, in four sizes: S 420px (a question, a rename — the default), M 560px (most forms), L 720px (lists and tables) and XL 900px (a workspace, and every dialog with a side rail, such as Settings and the guild hub, whose rail is 224px). The header is 60px: the title in the display face on the leading side, × on the trailing side, ‹ before the title when there is a pane to go back to. The body scrolls under the header; a form's footer (`.actions`) is pinned to the bottom with its buttons on the trailing side — Cancel, then the one primary, named after what it does — and gains a hairline only while the body scrolls. Settings-type panes save as you change them and have no footer. `ConfirmDialog` is the S dialog for a question: Cancel is focused, the verb is `danger` when it destroys. On a phone every dialog is a bottom sheet with a grip, and a railed dialog is a full page. Settings rows (`SettingRow`) are 58px lines separated by hairlines — label and description on the leading side, the control on the trailing side, dropping below the label on a phone — grouped under a sentence-case label with no card around them.

## The shell

One sidebar (`ChannelList.svelte`, 280px by default, 264 between 769 and 1150px, dragged within 240–360) holds, top to bottom: the **place header** (the guild's icon and name in the display face — a click opens the guild switcher, a right-click the guild's menu — then "N reachable", how many members this device can reach right now, and Invite); the **shortcut strip** (`GuildRail.svelte`): Messages, as many guild tiles as fit and an overflow tile; the jump field (it opens the palette) and the Inbox; the nudge row; the place's list; the **unread messages** block when a direct message waits; the call dock in a call; and you.

The strip is a shortcut, not the inventory. Which guilds earn a tile is `stripTiles` in `lib/rail.js`: the ones you pinned, in your order; then guilds with a mention; then unread ones; then the most recently opened — each class laid out by when its guilds entered it, so a tile never moves because a number changed, and nothing moves while the pointer is over the strip or the switcher is open. The guild you are in is always there. The overflow tile carries the hidden guilds' mentions as one number, or a dot — never how many are hidden. The **guild switcher** (`GuildSwitcher.svelte`) is the inventory: every guild, searchable, pinned ones first, then your order with folders opening in place; a row menu (pin, guild settings, mute, mark read, leave, rearrange), drag or Alt+↑/↓ to reorder, and Create and Join in its footer.

`navModel` (a device-local pref, `"sidebar"` by default) can put the classic slim guild rail beside the list instead of the strip. The inspector docks beside the conversation only when the conversation keeps 640px with it there, measured on the app's own box so the app's zoom counts; otherwise it overlays the conversation below its header. `.app` carries `data-inspector="docked"` or `"overlay"`. On a phone (≤ 768px) the sidebar is the Guilds page, the conversation and the inspector are pages pushed over it, and the tab bar reads Guilds · Messages · Inbox · You.

## Motion

Motion says that something arrived or left, and is over before you think about it. Nothing in the interface overshoots or bounces.

| Token | Value | For |
|---|---|---|
| `--dur-quick` | 120ms | hover, tints, a menu appearing |
| `--dur-standard` | 150ms | panels, reveals, slides |
| `--dur-calm` | 200ms | the events surface |
| `--ease-out` | `cubic-bezier(0.2, 0.9, 0.3, 1)` | state changes |
| `--ease-spring` | `cubic-bezier(0.16, 1, 0.3, 1)` | arrivals; it decelerates and does not overshoot |

`--ease-overshoot` keeps the old springy curve for celebrations — reaction bursts and the effects messages can carry — and the token test allows it only in those files. A menu fades in while rising 4px; a toast rises 8px; a sheet settles without bouncing. With reduced motion, the global stand-down in `app.css` zeroes both durations and delays.

## Density

Appearance offers Compact, Cosy (the default) and Comfortable. One attribute on `<html>` moves the whole feed rhythm together. Groups are made in the markup: the row that starts a group carries `--msg-gap` above it and a continuation row carries nothing, so a group reads as one block. Compact draws no avatars; Cosy's are 36px and Comfortable's 40px.

| | Compact | Cosy | Comfortable |
|---|---|---|---|
| `--fs-body` | 14.5px | 15.5px | 16.5px |
| `--msg-gap` | 6px | 16px | 22px |
| `--msg-pad-y` | 1px | 2px | 3px |
| `--feed-pad` | 10px | 16px | 22px |

Density changes the feed; it never moves the header, the sidebar's width or the composer. UI scale (Ctrl+= and Ctrl+−) is the control for making everything bigger.

## Words on screen

1. Write for the person looking at the screen: what happened, and what they can do about it. Two sentences at most.
2. Never show a raw error from the core — no package prefixes, addresses, status codes or paths. `humanError()` in `lib/errors.js` is where they are turned into words: it maps the errors people actually meet to a sentence each, passes the core's own sentences that were written for a person, and shows anything else that still looks like plumbing as "Something went wrong on this device. Try again — if it keeps happening, restart Concord.", logging the raw text to the console.
3. Be specific: name the person, the guild and the channel.
4. Don't alarm. Never say something was lost unless it was.
5. Buttons say what they will do: "Leave guild", "Copy invite", "Retry". Never "OK".
6. Sentence case everywhere. No exclamation marks or emoji in interface copy.
7. A guild is a guild. A voice channel is not written with `#`.
8. Times read "just now", "2 min ago", "yesterday at 18:02", with the exact time on hover.

## Checking your work

`npm --prefix frontend test` runs the token rules: font sizes, radii, spacing and durations must use tokens; the phone and narrow breakpoints must be on their contracts, and a coarse pointer may change sizes but never the layout; floating surfaces must be opaque; a button that paints its own ground must say its ink; every ink tier must reach 4.5:1 on the surfaces it is allowed on, in both themes and every theme pack; nothing glows, and only celebrations overshoot; counts are never red. The end-to-end suite in `scripts/e2e` must stay green. Then look at the change at 1440×900, 820×1180 and 390×844, in dark and light, before calling it done.
