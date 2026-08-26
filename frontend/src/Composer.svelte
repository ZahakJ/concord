<script>
  // The message composer: draft, :emoji: / @mention / @role / #channel /
  // slash-command
  // autocomplete, emoji picker, image attach (button/paste/drop via parent),
  // typing signals, reply banner, and ArrowUp-in-empty-composer to edit your
  // last message.
  import Icon from "./Icon.svelte";
  import { pushLayer } from "./lib/navstack.svelte.js";
  import EmojiPicker from "./EmojiPicker.svelte";
  import BottomSheet from "./BottomSheet.svelte";
  import Menu from "./Menu.svelte";
  import { untrack } from "svelte";
  import { replaceShortcodes, activeShortcode, searchEmoji } from "./lib/emoji.js";
  import { haptic } from "./lib/touch.js";
  import { S, activeChannel, activeGuild, sendMessage, react, flash, nameColorFor, mentionRefs } from "./lib/state.svelte.js";

  import { PERM, has } from "./lib/perms.js";
  import { api } from "./lib/api.js";
  import { tooltip } from "./lib/tooltip.js";
  import { scheduleMessage } from "./lib/scheduled.svelte.js";
  import { stampEphemeral, channelTTL, ttlLabel } from "./lib/ephemeral.svelte.js";
  import { stampTimestamp } from "./lib/timestamp.js";
  import { playSend } from "./lib/sounds.js";
  import { encodeFx, FX_EFFECTS } from "./lib/fxtoken.js";
  import { stagedImage } from "./lib/attachopts.js";

  let draft = $state("");
  // The draft's BASE DIRECTION, when the writer has asked for one: "" (decide
  // per line, the default and almost always right), "rtl", or "ltr".
  //
  // It exists because the per-line rule cannot be argued with. `unicode-bidi:
  // plaintext` reads the first strong character of each line and fixes that
  // line's direction from it, so a line begun in English is left-to-right for
  // good — and an Arabic phrase added afterwards lands to the RIGHT of the
  // English, where a writer of Arabic does not want it. There is no key that
  // moves it: the only way to change a line's base direction today is to
  // delete back to its start and type it the other way round.
  //
  // What this does NOT fix, and cannot: which arrow key extends a selection
  // which way. Chrome moves the caret VISUALLY for a bare arrow key and
  // LOGICALLY for a word-wise Ctrl+arrow, so in a right-to-left line
  // Ctrl+Shift+Left selects the run sitting on the right. That is the
  // browser's text-editing layer, below anything a page can reach; the only
  // way past it is to give up <textarea> for a contenteditable with
  // hand-written caret handling, which costs IME, mobile keyboards,
  // spellcheck, native undo and accessibility. Not a trade worth making.
  let dirMode = $state("");
  let uploading = $state(0); // files being read into `pending` (brief)
  // Staged attachments: pasting/dropping/picking a file adds a PREVIEW to the
  // composer (Discord-style), sent together with the text on submit.
  // Each: { id, dataUrl, w, h, name, isImage }
  let pending = $state([]);
  let composerEl = $state(null);
  let fileInput = $state(null);
  let cameraInput = $state(null);
  let suggest = $state(null); // { kind:"emoji"|"mention"|"channel"|"slash", start, items, sel }
  let lastTypingSent = 0;

  // A composer placeholder that reads like the conversation you're in — never
  // the internal "#dm" channel name.
  const composerPlaceholder = $derived.by(() => {
    if (!ch) return "Select a channel";
    const g = activeGuild();
    if (g?.dmNotes) return "Write a note to yourself…";
    if (g?.kind === "dm") return `Message ${g.name || "your friend"}…`;
    return `Message #${ch.name}…`;
  });

  const ch = $derived(activeChannel());
  // Touch layout: hide the desktop formatting toolbar (it can't hover-reveal on
  // touch and just eats a row), send with an explicit button instead of Enter
  // (Enter is a newline on a phone keyboard), and roomier tap targets.
  // `mobile` drives LAYOUT (S.isMobile also matches a narrow desktop window);
  // Enter behavior keys off actual pointer coarseness so a physical keyboard
  // in a narrow window keeps Enter-to-send.
  const mobile = $derived(S.isMobile);
  // Between the mobile breakpoint and ~1150px the desktop layout survives but
  // the chat column does not: nine 34px buttons plus their gaps claim ~300px of
  // a 314px well, and the textarea is left with the twelve pixels nobody
  // measured. The five occasional controls fold into an overflow menu there,
  // which is the same trade the phone row already makes — just later.
  const tight = $derived(!mobile && S.narrow);
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;
  const canSend = $derived((!!draft.trim() || pending.length > 0) && !!ch && slowLeft <= 0);

  // Slow mode countdown. The backend is the contract (a too-soon send errors);
  // this keeps honest fingers off the wall and says how long. Managers are
  // exempt on both sides. Channel switches clear the count — each channel's
  // interval is its own.
  const slowSecs = $derived(Number(ch?.slowMode) || 0);
  let slowLeft = $state(0);
  let slowTimer = null;
  $effect(() => {
    S.activeChannelId;
    slowLeft = 0;
    clearInterval(slowTimer);
  });
  function armSlowMode() {
    if (!slowSecs || canModerate) return;
    slowLeft = slowSecs;
    clearInterval(slowTimer);
    slowTimer = setInterval(() => {
      slowLeft -= 1;
      if (slowLeft <= 0) clearInterval(slowTimer);
    }, 1000);
  }
  // Disappearing-messages timer for this channel (0 = off). channelTTL reads the
  // reactive per-channel store, so this updates the moment you change it.
  const ephTTL = $derived(ch ? channelTTL(S.activeChannelId) : 0);
  // Mobile markdown lives behind a toggle rather than an always-on toolbar row.
  let showFmt = $state(false);
  const showFmtBar = $derived(!mobile || showFmt);
  // The phone composer is ONE row — "+", the text, emoji, and mic-or-send. The
  // other five controls (GIF, poll, formatting, advanced compose, schedule) live
  // in this sheet. Eight 44px targets came to 352px, which wrapped to a second
  // row at 393px and a third at 360px, and the row's height then changed on the
  // first keystroke as the mic dropped out.
  let moreOpen = $state(false);
  // Height of the mobile emoji panel while it's open, so the composer can sit ON
  // TOP of it like a keyboard accessory instead of behind it.
  let pickerH = $state(0);

  // Publish the composer's height as --composer-h, so the toast stack can sit
  // ABOVE it instead of on top of it. Toasts were pinned to the bottom-right
  // corner, which on every desktop window is precisely where the composer's
  // icon row is — "Joined voice" and "Failed to fetch" both landed on the
  // buttons and clipped the overflow chevron through the middle of its glyph.
  // A ResizeObserver rather than a measured constant: the composer grows with
  // the draft, with a reply banner, with attachments, and with the emoji panel.
  let wrapEl = $state(null);
  $effect(() => {
    const el = wrapEl;
    if (!el || typeof ResizeObserver === "undefined") return;
    const root = document.documentElement;
    const ro = new ResizeObserver(() => {
      root.style.setProperty("--composer-h", `${Math.round(el.offsetHeight)}px`);
    });
    ro.observe(el);
    return () => {
      ro.disconnect();
      // The composer unmounts on a forum board and in the empty state; leaving
      // a stale height behind would park every toast in mid-air.
      root.style.removeProperty("--composer-h");
    };
  });

  export function focus() {
    composerEl?.focus();
  }

  // ---- per-channel drafts (survive reloads + channel switches) ----
  const draftKey = (id) => `concord.draft.${id}`;
  function saveDraft(id, text) {
    if (!id) return;
    try {
      if (text) localStorage.setItem(draftKey(id), text);
      else localStorage.removeItem(draftKey(id));
    } catch {
      /* storage blocked */
    }
  }
  function loadDraft(id) {
    try {
      return (id && localStorage.getItem(draftKey(id))) || "";
    } catch {
      return "";
    }
  }

  let prevChannel = S.activeChannelId;
  draft = loadDraft(prevChannel);
  // On channel switch: stash the outgoing draft, restore the incoming one.
  $effect(() => {
    const cur = S.activeChannelId;
    untrack(() => {
      if (cur === prevChannel) return;
      saveDraft(prevChannel, draft);
      draft = loadDraft(cur);
      prevChannel = cur;
      queueAutosize();
    });
  });

  // Size to a restored draft once the textarea mounts.
  $effect(() => {
    if (composerEl) queueAutosize();
  });

  // ---- auto-growing textarea ----
  // How tall the draft may grow. A flat 200px is right on a desktop and wrong on
  // a phone: 200px of text plus the composer chrome is ~260px, and a 640px
  // handset with the IME open has about 320px of app left — writing a paragraph
  // erased the conversation you were replying to. On touch it's a share of what
  // the keyboard actually leaves, which is why this is read (not a constant) on
  // every autosize. The CSS max-height is only the pre-mount default.
  const DRAFT_MAX_H = 200;
  function draftCap() {
    if (!mobile) return DRAFT_MAX_H;
    const vh = window.visualViewport?.height || window.innerHeight;
    return Math.max(76, Math.min(DRAFT_MAX_H, Math.round(vh * 0.32)));
  }

  // Where the browser will do it for us. `field-sizing: content` makes the
  // textarea size itself to its value, which is the whole of what autosize()
  // was for — and it does it inside the same layout pass that the keystroke
  // already costs, instead of the write/read/write straddle below, which forces
  // a synchronous re-layout of the entire document on every character typed.
  // Chrome 123+ / WebView 123+; everything older keeps the measuring path.
  const AUTOFIT = typeof CSS !== "undefined" && !!CSS.supports?.("field-sizing", "content");
  let lastCap = -1;

  function autosize() {
    if (!composerEl) return;
    const cap = draftCap();
    if (AUTOFIT) {
      // Only the cap is ours, and it moves with the viewport (keyboard up,
      // rotation) — not with the text. Writing it unconditionally would put a
      // style invalidation back on the keystroke path for no reason.
      if (cap !== lastCap) {
        lastCap = cap;
        composerEl.style.maxHeight = cap + "px";
      }
      return;
    }
    composerEl.style.maxHeight = cap + "px";
    composerEl.style.height = "auto";
    const full = composerEl.scrollHeight;
    composerEl.style.height = Math.min(full, cap) + "px";
    // The scrollbar appears only once the draft genuinely outgrows the cap.
    // Left on `overflow-y: auto`, the height we just assigned can land a
    // fraction of a pixel under the scrollHeight it came from — enough for the
    // browser to render a permanent, tiny scrollbar in a composer with nothing
    // typed in it at all. (Under field-sizing the browser picks the height, so
    // there is no rounding of ours to trip over and the CSS says `auto`.)
    composerEl.style.overflowY = full > cap ? "auto" : "hidden";
  }
  // Coalescing: several of these can be queued by one gesture (input, then a
  // suggestion accepted, then a paste), and they would each have re-measured.
  let autosizeQueued = false;
  const queueAutosize = () => {
    if (autosizeQueued) return;
    autosizeQueued = true;
    requestAnimationFrame(() => {
      autosizeQueued = false;
      autosize();
    });
  };

  // ---- the software keyboard ----
  // Android (adjustResize) and iOS/WKWebView disagree about what an open IME
  // does: one shrinks the layout viewport, so the composer rides up by itself;
  // the other leaves the page exactly as it was and simply draws the keyboard
  // over the bottom of it. Nothing in this app read visualViewport, so on the
  // second — which is also what Android 15's enforced edge-to-edge produces on
  // several OEM builds — the composer sat behind the keyboard with no fallback.
  //
  // --kb-inset is how many CSS px of the LAYOUT viewport the keyboard covers,
  // and it is 0 whenever the platform already resized for us. The composer
  // reserves exactly that much beneath itself (see .composer-wrap.mobile), which
  // both guarantees clearance and drops the gesture-bar padding the IME has
  // already covered.
  function syncKbInset() {
    const vv = window.visualViewport;
    if (!vv) return;
    // If the native bridge is reporting the IME, IT is the source of truth and
    // the shell already reserves that space (.mshell pads by --kb). Measuring
    // the same keyboard again here and adding our own margin reserved it TWICE
    // — on a phone the IME is nearly half the screen, so the feed collapsed to a
    // sliver above a huge grey band. Yield instead.
    const nativeKb =
      parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--kb")) || 0;
    if (nativeKb > 0) {
      document.documentElement.style.setProperty("--kb-inset", "0px");
      queueAutosize();
      return;
    }
    const covered = window.innerHeight - vv.height - vv.offsetTop;
    // A collapsing browser toolbar moves this by ~50px and is not a keyboard;
    // no IME is under ~150px tall. Anything below the threshold is chrome.
    document.documentElement.style.setProperty("--kb-inset", (covered > 90 ? Math.round(covered) : 0) + "px");
    queueAutosize(); // the cap above is a share of the remaining height
  }
  $effect(() => {
    const vv = window.visualViewport;
    // One writer only: a second Composer (or a future shell-level tracker) would
    // fight this one over the same variable.
    if (!vv || window.__concordKbInset) return;
    window.__concordKbInset = true;
    syncKbInset();
    vv.addEventListener("resize", syncKbInset);
    vv.addEventListener("scroll", syncKbInset);
    return () => {
      window.__concordKbInset = false;
      vv.removeEventListener("resize", syncKbInset);
      vv.removeEventListener("scroll", syncKbInset);
      document.documentElement.style.removeProperty("--kb-inset");
    };
  });

  // ---- slash commands (client-side text expansion) ----
  // One registry drives both the "/" autocomplete menu and applySlash(), so
  // the menu can never drift out of sync with what actually expands. `args`
  // controls whether accepting a command leaves the caret after "/cmd ".
  const kaomoji = (face) => (rest) => (rest ? rest + " " : "") + face;
  const fxExpand = (name) => (rest) => `${rest || FX_EFFECTS[name].body} ${encodeFx(name)}`;
  // A sealed timestamp is per-message intent: you arm it, you send, it disarms.
  // Leaving it latched would silently stamp every later message, and a marker
  // that appears when you did not ask for it is worse than no marker.
  let sealNext = $state(false);
  $effect(() => {
    S.activeChannelId; // re-run on channel switch
    sealNext = false;
  });

  const SLASH_COMMANDS = [
    { name: "shrug", usage: "/shrug [message]", desc: "Appends ¯\\_(ツ)_/¯", args: true, expand: kaomoji("¯\\_(ツ)_/¯") },
    { name: "tableflip", usage: "/tableflip [message]", desc: "Appends (╯°□°)╯︵ ┻━┻", args: true, expand: kaomoji("(╯°□°)╯︵ ┻━┻") },
    { name: "unflip", usage: "/unflip [message]", desc: "Appends ┬─┬ ノ( ゜-゜ノ)", args: true, expand: kaomoji("┬─┬ ノ( ゜-゜ノ)") },
    // House joke. Mirror-symmetric on purpose: the right half is the standard
    // flip, the left half is that same flip reflected, and "fa me" sits exactly
    // between them — two people flipping tables away from the phrase.
    { name: "fa", usage: "/fa [message]", desc: "┻━┻ ︵╰(°□°╰) fa me (╯°□°)╯︵ ┻━┻", args: true, expand: kaomoji("┻━┻ ︵╰(°□°╰) fa me (╯°□°)╯︵ ┻━┻") },
    { name: "me", usage: "/me <action>", desc: "Italicized action text", args: true, expand: (rest, text) => (rest ? `*${rest}*` : text) },
    { name: "spoiler", usage: "/spoiler <text>", desc: "Hides text until clicked", args: true, expand: (rest, text) => (rest ? `||${rest}||` : text) },
    // Arms the seal and leaves the text alone, so "/timestamp on my way" sends
    // "on my way" with the mark rather than the command word.
    {
      name: "timestamp",
      usage: "/timestamp [message]",
      desc: "Seal the exact send time onto this message",
      args: true,
      expand: (rest, text) => {
        sealNext = true;
        return rest || text.replace(/^\/timestamp\s*/i, "");
      },
    },
    // Send effects: the token rides the message itself (lib/fxtoken.js), so
    // every peer's client plays the burst when the row first scrolls into
    // view. Sent bare, the effect's emoji stands in as the body — a message
    // that is pure fireworks still needs a row to live in.
    { name: "confetti", usage: "/confetti [message]", desc: "Send with a confetti burst", args: true, expand: fxExpand("confetti") },
    { name: "fireworks", usage: "/fireworks [message]", desc: "Send with fireworks", args: true, expand: fxExpand("fireworks") },
    { name: "hearts", usage: "/hearts [message]", desc: "Send with floating hearts", args: true, expand: fxExpand("hearts") },
    // ACTIONS, not text expansions: these run instead of sending (see runAction).
    { name: "meme", usage: "/meme", desc: "Open the meme editor", expand: (_, text) => text },
    { name: "gif", usage: "/gif", desc: "This guild's GIF pack", expand: (_, text) => text },
    { name: "clear", usage: "/clear <n>", desc: "Delete the last n messages (moderators)", args: true, mod: true, expand: (_, text) => text },
  ];

  const canModerate = $derived(has(activeGuild()?.myPerms || 0, PERM.MANAGE_MESSAGES));
  const slashCommands = $derived(SLASH_COMMANDS.filter((c) => !c.mod || canModerate));

  // Action commands do something instead of sending text. Returns true if the
  // draft was consumed.
  async function runAction(text) {
    if (/^\/meme\s*$/i.test(text)) {
      S.modal = { kind: "meme" };
      return true;
    }
    if (/^\/gif\s*$/i.test(text)) {
      S.modal = { kind: "gifs" };
      return true;
    }
    const m = text.match(/^\/clear(?:\s+(\d+))?\s*$/i);
    if (!m) return false;
    if (!canModerate) {
      flash("You need the Manage messages permission to clear messages");
      return true;
    }
    const n = parseInt(m[1] || "", 10);
    if (!n) {
      flash("How many? e.g. /clear 10");
      return true;
    }
    try {
      const cleared = await api.purgeMessages(S.activeChannelId, n);
      flash(`Cleared ${cleared} message${cleared === 1 ? "" : "s"}`, "success");
    } catch (err) {
      flash(err);
    }
    return true;
  }

  function applySlash(text) {
    const m = text.match(/^\/(\w+)(?:\s+([\s\S]*))?$/);
    const cmd = m && SLASH_COMMANDS.find((c) => c.name === m[1]);
    return cmd ? cmd.expand((m[2] || "").trim(), text) : text;
  }

  // ---- autocomplete (emoji + mentions + slash commands share one popover) ----

  // Only while the caret is still inside the first "/word" token — once a
  // space is typed the command is committed and the menu stays out of the way.
  function activeSlash(text, caret) {
    const m = text.match(/^\/(\w*)/);
    if (!m || caret < 1 || caret > m[0].length) return null;
    return { query: text.slice(1, caret).toLowerCase() };
  }

  function activeMention(text, caret) {
    const upto = text.slice(0, caret);
    const at = upto.lastIndexOf("@");
    if (at < 0 || (at > 0 && /[\w@]/.test(upto[at - 1]))) return null;
    const query = upto.slice(at + 1);
    if (/\s[\s]/.test(query) || query.length > 24) return null;
    return { start: at, query };
  }

  // The role and #channel tables, shared with the renderer so what you pick
  // here is exactly what the sent message will resolve to.
  const refs = $derived(mentionRefs());

  const SUGGEST_HEADS = {
    slash: "Commands",
    emoji: "Emoji",
    mention: "Members & roles",
    channel: "Channels",
  };

  // Roles carry a colour; the same strict hex check the renderer uses, so the
  // list can't be the one place a bad value reaches a style attribute.
  const roleTint = (item) =>
    item.kind === "role" && /^#[0-9a-fA-F]{3,6}$/.test(item.color || "")
      ? `color:${item.color}`
      : "";

  // #channel. Same shape as a mention, with one extra job: a markdown header
  // also starts with "#", so any whitespace in the query closes the list — by
  // the time you've typed "# " you're writing a heading, not naming a channel.
  function activeChannelRef(text, caret) {
    const upto = text.slice(0, caret);
    const at = upto.lastIndexOf("#");
    if (at < 0 || (at > 0 && /[\w#]/.test(upto[at - 1]))) return null;
    const query = upto.slice(at + 1);
    if (/\s/.test(query) || query.length > 32) return null;
    return { start: at, query };
  }

  function updateSuggest() {
    const caret = composerEl?.selectionStart ?? draft.length;
    const slash = activeSlash(draft, caret);
    if (slash) {
      const items = slashCommands.filter((c) => c.name.startsWith(slash.query));
      suggest = items.length ? { kind: "slash", start: 0, items, sel: 0 } : null;
      return;
    }
    const emoji = activeShortcode(draft, caret);
    if (emoji) {
      const items = searchEmoji(emoji.query, 8);
      suggest = items.length
        ? { kind: "emoji", start: emoji.start, items, sel: 0 }
        : null;
      return;
    }
    const mention = activeMention(draft, caret);
    if (mention) {
      const q = mention.query.toLowerCase();
      const hit = (name) => name && name.toLowerCase().includes(q);
      // People first, then roles. Same order the renderer resolves them in, so
      // what the list offers is what the message will actually mean.
      const items = [
        ...S.members
          .filter((m) => !m.isSelf && hit(m.name))
          .map((m) => ({ key: `m:${m.fingerprint}`, name: m.name, kind: "member" })),
        ...refs.roles
          .filter((r) => hit(r.name))
          .map((r) => ({ key: `r:${r.name}`, name: r.name, kind: "role", color: r.color })),
      ].slice(0, 6);
      // @everyone is synthetic — no member or role backs it — and appended
      // AFTER the slice so a full list can't hide it, and last so a broadcast
      // is never the accidental default selection when someone was reaching
      // for a member. Offered only while the query is still a prefix of
      // "everyone" (covers the bare "@" too).
      if ("everyone".startsWith(q)) {
        items.push({ key: "e:everyone", name: "everyone", kind: "everyone" });
      }
      suggest = items.length ? { kind: "mention", start: mention.start, items, sel: 0 } : null;
      return;
    }
    const chref = activeChannelRef(draft, caret);
    if (chref) {
      const q = chref.query.toLowerCase();
      const items = refs.channels
        .filter((c) => c.name.toLowerCase().includes(q))
        .map((c) => ({ key: `c:${c.id}`, name: c.name }))
        .slice(0, 6);
      suggest = items.length ? { kind: "channel", start: chref.start, items, sel: 0 } : null;
      return;
    }
    suggest = null;
  }

  function accept(idx = null) {
    if (!suggest) return;
    const caret = composerEl?.selectionStart ?? draft.length;
    const item = suggest.items[idx ?? suggest.sel];
    const insert =
      suggest.kind === "emoji"
        ? item[1] + " "
        : suggest.kind === "mention"
          ? `@${item.name} `
          : suggest.kind === "channel"
            ? `#${item.name} `
            : `/${item.name}` + (item.args ? " " : "");
    const pos = suggest.start + insert.length;
    const before = draft;
    draft = draft.slice(0, suggest.start) + insert + draft.slice(caret);
    suggest = null;
    // Accepting a suggestion that changes NOTHING means the command was
    // already fully typed — "/gif" + Enter used to merely close the popover
    // and demand a second, unguessable Enter ("/gif doesn't work"). When the
    // accept is a no-op on an argless slash command, that Enter meant SEND.
    if (draft === before && before.trim().startsWith("/") && !item?.args) {
      send();
      return;
    }
    composerEl?.focus();
    queueAutosize();
    // Land the caret right after what we inserted (e.g. after "/spoiler ") so
    // the user can keep typing the args — bind:value alone parks it at the end.
    requestAnimationFrame(() => composerEl?.setSelectionRange(pos, pos));
  }

  // ---- markdown formatting (toolbar buttons + Ctrl/Cmd shortcuts) ----
  // Wraps the current selection with markers, or inserts a selected
  // placeholder when nothing is selected. Mirrors accept(): mutate `draft`,
  // refocus, then place the selection on the next frame because bind:value
  // alone would park the caret at the end.

  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent || "");
  const MOD_LABEL = isMac ? "⌘" : "Ctrl+";
  const WRAPS = {
    bold: { pre: "**", post: "**", ph: "bold" },
    italic: { pre: "*", post: "*", ph: "italic" },
    strike: { pre: "~~", post: "~~", ph: "strikethrough" },
    spoiler: { pre: "||", post: "||", ph: "spoiler" },
    code: { pre: "`", post: "`", ph: "code" },
  };
  // Groups render with a thin separator between them. `keys` is only the
  // tooltip hint — the actual bindings live in onKeydown.
  const FMT_GROUPS = [
    [
      { kind: "bold", label: "Bold", keys: "B" },
      { kind: "italic", label: "Italic", keys: "I" },
      { kind: "strike", label: "Strikethrough" },
      { kind: "spoiler", label: "Spoiler", keys: "Shift+X" },
    ],
    [
      { kind: "code", label: "Inline code", keys: "E" },
      { kind: "codeblock", label: "Code block" },
    ],
    [
      { kind: "quote", label: "Quote", keys: "Shift+." },
      { kind: "link", label: "Link", keys: "K" },
    ],
  ];
  const fmtTitle = (b) => (b.keys ? `${b.label} (${MOD_LABEL}${b.keys})` : b.label);

  function applyFormat(kind) {
    if (!composerEl || !ch) return;
    const start = composerEl.selectionStart ?? draft.length;
    const end = composerEl.selectionEnd ?? start;
    const sel = draft.slice(start, end);
    let selStart, selEnd;

    if (kind === "quote") {
      // Toggle "> " on every line the selection touches.
      const lineStart = draft.lastIndexOf("\n", start - 1) + 1;
      let lineEnd = draft.indexOf("\n", end);
      if (lineEnd < 0) lineEnd = draft.length;
      const lines = draft.slice(lineStart, lineEnd).split("\n");
      const quoted = lines.every((l) => l.startsWith("> "));
      const block = lines.map((l) => (quoted ? l.slice(2) : "> " + l)).join("\n");
      draft = draft.slice(0, lineStart) + block + draft.slice(lineEnd);
      if (start === end) {
        const p = Math.max(lineStart, start + (quoted ? -2 : 2));
        selStart = selEnd = Math.min(p, lineStart + block.length);
      } else {
        selStart = lineStart;
        selEnd = lineStart + block.length;
      }
    } else if (kind === "link") {
      // A selection becomes the label with "url" selected to type over; with
      // nothing selected, insert a full placeholder and select the label.
      const label = sel || "text";
      draft = draft.slice(0, start) + `[${label}](url)` + draft.slice(end);
      if (sel) {
        selStart = start + label.length + 3; // just past "[label]("
        selEnd = selStart + 3;
      } else {
        selStart = start + 1;
        selEnd = selStart + label.length;
      }
    } else if (kind === "codeblock") {
      // Fences want their own lines — only add the newlines that are missing.
      const text = sel || "code";
      const pre = (start > 0 && draft[start - 1] !== "\n" ? "\n" : "") + "```\n";
      const post = "\n```" + (end < draft.length && draft[end] !== "\n" ? "\n" : "");
      draft = draft.slice(0, start) + pre + text + post + draft.slice(end);
      selStart = start + pre.length;
      selEnd = selStart + text.length;
    } else {
      const { pre, post, ph } = WRAPS[kind];
      // Already wrapped (markers just outside, or included in the selection)?
      // Then this press toggles the formatting back off. The italic guard
      // keeps a lone "*" check from eating half of a surrounding "**".
      const outside =
        sel &&
        draft.slice(start - pre.length, start) === pre &&
        draft.slice(end, end + post.length) === post &&
        !(kind === "italic" && draft.slice(start - 2, start) === "**" && draft.slice(end, end + 2) === "**");
      const inside = sel.length >= pre.length + post.length && sel.startsWith(pre) && sel.endsWith(post);
      if (outside) {
        draft = draft.slice(0, start - pre.length) + sel + draft.slice(end + post.length);
        selStart = start - pre.length;
        selEnd = selStart + sel.length;
      } else if (inside) {
        const inner = sel.slice(pre.length, sel.length - post.length);
        draft = draft.slice(0, start) + inner + draft.slice(end);
        selStart = start;
        selEnd = start + inner.length;
      } else {
        const text = sel || ph;
        draft = draft.slice(0, start) + pre + text + post + draft.slice(end);
        selStart = start + pre.length;
        selEnd = selStart + text.length;
      }
    }

    saveDraft(S.activeChannelId, draft);
    suggest = null;
    composerEl.focus();
    queueAutosize();
    requestAnimationFrame(() => composerEl?.setSelectionRange(selStart, selEnd));
  }

  function editLastOwnMessage() {
    const own = [...S.messages].reverse().find(
      (m) => m.sender === S.identity.fingerprint && !m.deleted && m.kind === "",
    );
    if (own) S.editing = own;
  }

  function onKeydown(e) {
    // Formatting shortcuts first — they all carry Ctrl/Cmd, so they can't
    // collide with autocomplete nav, Enter-send, or ArrowUp-edit below.
    if ((e.ctrlKey || e.metaKey) && !e.altKey) {
      const k = e.key.toLowerCase();
      const kind = !e.shiftKey
        ? k === "b"
          ? "bold"
          : k === "i"
            ? "italic"
            : k === "e"
              ? "code"
              : k === "k"
                ? "link"
                : null
        : k === "x"
          ? "spoiler"
          : e.code === "Period" || k === "." || k === ">"
            ? "quote"
            : null;
      if (kind) {
        e.preventDefault();
        applyFormat(kind);
        return;
      }
      // Ctrl/Cmd+Shift+D cycles the draft's base direction: per-line → RTL →
      // LTR → per-line. A cycle rather than two bindings because the third
      // state is the one people want back, and a single key that returns to
      // the default is easier to trust than remembering which of two undoes
      // the other. `e.code` alongside `k` so a non-Latin keyboard layout —
      // exactly the layout someone writing Arabic is on — still matches.
      if (e.shiftKey && (k === "d" || e.code === "KeyD")) {
        e.preventDefault();
        dirMode = dirMode === "" ? "rtl" : dirMode === "rtl" ? "ltr" : "";
        composerEl?.focus();
        return;
      }
    }
    if (suggest) {
      // Tab cycles for emoji/mentions (long-standing behavior) but accepts for
      // slash commands, where there's a single obvious completion.
      const tabAccepts = suggest.kind === "slash";
      if (e.key === "ArrowDown" || (e.key === "Tab" && !e.shiftKey && !tabAccepts)) {
        e.preventDefault();
        suggest = { ...suggest, sel: (suggest.sel + 1) % suggest.items.length };
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        suggest = { ...suggest, sel: (suggest.sel - 1 + suggest.items.length) % suggest.items.length };
      } else if (e.key === "Enter" || (e.key === "Tab" && !e.shiftKey && tabAccepts)) {
        e.preventDefault();
        accept();
      } else if (e.key === "Escape") {
        // Consumed here, so it never reaches the keymap's window listener and
        // pops a layer as well — dismissing the completion list and cancelling
        // the reply you were writing on one keypress.
        e.stopPropagation();
        suggest = null;
      }
      return;
    }
    // Physical keyboard: Enter sends, Shift+Enter is a newline. Touch: Enter is
    // always a newline (you send with the button) — matches every phone
    // messenger and avoids firing off half-typed messages on the on-screen
    // keyboard. Keyed on pointer coarseness, not layout: a narrow desktop
    // window keeps Enter-to-send. Ignore Enter mid-IME-composition so CJK
    // candidate selection doesn't send.
    if (!coarse && e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      send();
    } else if (e.key === "ArrowUp" && !draft) {
      e.preventDefault();
      editLastOwnMessage();
    }
    // Escape while replying is not handled here any more: the reply is a layer
    // like everything else, and the keymap pops whichever layer is on top —
    // which may well be a picker this composer opened.
  }

  function onInput() {
    const now = Date.now();
    if (now - lastTypingSent > 2000 && S.activeChannelId) {
      lastTypingSent = now;
      api.sendTyping(S.activeChannelId).catch(() => {});
    }
    saveDraft(S.activeChannelId, draft);
    queueAutosize();
    updateSuggest();
  }

  // Send-button launch: the paper plane flies off and glides back in. State
  // toggles the CSS animation; cleared on a timer so rapid sends replay it.
  let launching = $state(false);
  let launchTimer;
  function playLaunch() {
    clearTimeout(launchTimer);
    launching = false;
    requestAnimationFrame(() => {
      launching = true;
      launchTimer = setTimeout(() => (launching = false), 450);
    });
  }

  async function send(e) {
    e?.preventDefault();
    const raw = draft.trim();
    if (raw.startsWith("/") && S.activeChannelId && (await runAction(raw))) {
      draft = "";
      saveDraft(S.activeChannelId, "");
      suggest = null;
      queueAutosize();
      return;
    }
    const text = replaceShortcodes(applySlash(raw).trim());
    const atts = pending;
    if ((!text && atts.length === 0) || !S.activeChannelId) return;
    // Plane animation + a two-note tick, every platform. Deliberately NO
    // vibration: sending is the single most frequent action in the app and it
    // is already confirmed twice over — a buzz on every send does not read as
    // feedback, it reads as the phone twitching in your hand. Haptics are for
    // what you cannot see or cannot undo. (The tick rides the global sound
    // mute like every other chime.)
    playLaunch();
    playSend();
    const chId = S.activeChannelId;
    const prevDraft = draft;
    const prevReply = S.replyingTo;
    draft = "";
    pending = [];
    saveDraft(chId, "");
    suggest = null;
    queueAutosize();
    // The reply attaches to the FIRST message we send; the rest are plain.
    let rt = S.replyingTo?.id || "";
    const nextReply = () => {
      const r = rt;
      rt = "";
      return r;
    };
    S.replyingTo = null;
    let sent = 0; // attachments successfully sent so far
    try {
      // Attachments first, then the caption — so a pasted image sits above its
      // text in the feed, the way Discord shows an image with a caption below.
      for (const a of atts) {
        if (a.isImage)
          await api.sendAttachment(chId, a.dataUrl, a.w, a.h, nextReply(), !!a.spoiler, a.name || "", a.desc || "");
        else await api.sendFile(chId, a.dataUrl, a.name, nextReply());
        sent++;
      }
      if (text) {
        // Seal before the ephemeral stamp so both tokens sit at the front in a
        // stable order; each strips independently at render.
        const body = sealNext ? stampTimestamp(text) : text;
        // The direction travels with the message. Laid out one way as it was
        // written and the other way as it is read is not the message anyone
        // wrote — and the reader's client has no way to recover the intent,
        // because the per-line rule is exactly what the writer overrode.
        await sendMessage(stampEphemeral(chId, body), nextReply(), dirMode);
        sealNext = false;
        if (chId === S.activeChannelId) armSlowMode();
      }
    } catch (err) {
      // Restore only what did NOT go out, so a retry can't double-post. The text
      // is the last step, so on any failure it's unsent — put the draft back. The
      // reply rides the first send; only restore it if nothing was sent yet
      // (otherwise it was already consumed and would re-attach to a stray retry).
      draft = prevDraft;
      pending = atts.slice(sent);
      saveDraft(chId, prevDraft);
      if (sent === 0) S.replyingTo = prevReply;
      queueAutosize();
      flash(err);
    }
  }

  // ---- attachments ----
  // Images go out as encrypted blobs (see lib/attachments.js): up to 5 MB,
  // sent as-is when already png/jpeg/gif/webp (keeps GIF animation), anything
  // else (HEIC/AVIF/SVG/...) is canvas-normalized to JPEG — which is also what
  // fixes "my image arrived as a wall of text" for exotic formats.

  const MAX_IMAGE_BYTES = 5 * 1024 * 1024;
  const NATIVE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];

  function readAsDataURL(blob) {
    return new Promise((res, rej) => {
      const r = new FileReader();
      r.onload = () => res(r.result);
      r.onerror = rej;
      r.readAsDataURL(blob);
    });
  }

  async function decodeBitmap(file) {
    try {
      return await createImageBitmap(file);
    } catch {
      // Fallback for formats createImageBitmap rejects but <img> can decode.
      const url = URL.createObjectURL(file);
      try {
        return await new Promise((res, rej) => {
          const img = new Image();
          img.onload = () => res(img);
          img.onerror = rej;
          img.src = url;
        });
      } finally {
        URL.revokeObjectURL(url);
      }
    }
  }

  async function normalizeToJpeg(file) {
    const bmp = await decodeBitmap(file);
    const w = bmp.width || bmp.naturalWidth;
    const h = bmp.height || bmp.naturalHeight;
    const canvas = document.createElement("canvas");
    canvas.width = w;
    canvas.height = h;
    canvas.getContext("2d").drawImage(bmp, 0, 0);
    for (const q of [0.85, 0.7]) {
      const dataUrl = canvas.toDataURL("image/jpeg", q);
      if (dataUrl.length * 0.75 <= MAX_IMAGE_BYTES) return { dataUrl, w, h };
    }
    throw new Error("still too large after compression");
  }

  async function imageDims(dataUrl) {
    return new Promise((res) => {
      const img = new Image();
      img.onload = () => res({ w: img.naturalWidth, h: img.naturalHeight });
      img.onerror = () => res({ w: 0, h: 0 });
      img.src = dataUrl;
    });
  }

  const MAX_FILE_BYTES = 25 * 1024 * 1024;

  // Per-attachment controls, Discord-style: mark as spoiler, edit the name and
  // description, or drop it. Only one editor is open at a time — `editingAtt`
  // holds its id.
  let editingAtt = $state("");
  function setAtt(id, key, value) {
    pending = pending.map((p) => (p.id === id ? { ...p, [key]: value } : p));
  }
  function toggleSpoiler(id) {
    pending = pending.map((p) => (p.id === id ? { ...p, spoiler: !p.spoiler } : p));
  }

  function removePending(id) {
    haptic("light"); // destructive and undoable by nothing — confirm it by feel
    pending = pending.filter((p) => p.id !== id);
    if (editingAtt === id) editingAtt = "";
  }
  const uid = () =>
    (crypto?.randomUUID?.() ?? String(Date.now()) + Math.random());

  // stageImage: read + normalize an image and hold it as a pending attachment.
  async function stageImage(file) {
    if (!file || !S.activeChannelId) return;
    uploading++;
    try {
      let dataUrl, w, h;
      if (NATIVE_TYPES.includes(file.type) && file.size <= MAX_IMAGE_BYTES) {
        dataUrl = await readAsDataURL(file);
        ({ w, h } = await imageDims(dataUrl));
      } else {
        ({ dataUrl, w, h } = await normalizeToJpeg(file));
      }
      // Defaults come from stagedImage (lib/attachopts.js), which keeps `name`
      // empty so an unedited image still goes out as the v1 token older peers
      // can render. See that file for why prefilling it here is a trap.
      pending = [
        ...pending,
        stagedImage({ id: uid(), dataUrl, w, h, fileName: file.name || "" }),
      ];
    } catch (err) {
      const msg = String(err?.message || err);
      flash(
        msg.includes("too large")
          ? "Image too large (max 5 MB, even after compression)"
          : "Couldn't read that image format",
        "error",
      );
    } finally {
      uploading--;
    }
  }

  // attachFile: the general entry point (paste / drop / file button). Images and
  // other files alike are STAGED; nothing sends until submit.
  export async function attachFile(file) {
    if (!file || !S.activeChannelId) return;
    if (file.type.startsWith("image/")) {
      await stageImage(file);
      return;
    }
    if (file.size > MAX_FILE_BYTES) {
      flash("File too large (max 25 MB)", "error");
      return;
    }
    uploading++;
    try {
      const dataUrl = await readAsDataURL(file);
      const isVideo = file.type.startsWith("video/");
      pending = [...pending, { id: uid(), dataUrl, name: file.name || "file", isImage: false, isVideo }];
    } catch (err) {
      flash(err);
    } finally {
      uploading--;
    }
  }
  // Kept for callers that expect the old name; now stages too.
  export const attachImage = stageImage;

  // ---- voice messages ----
  // Record a clip and ship it through the same encrypted-file path as any
  // attachment (SendFile), tagged audio/* so the feed renders it as a player.
  const canRecord =
    typeof MediaRecorder !== "undefined" && !!navigator.mediaDevices?.getUserMedia;
  let recording = $state(false);
  let recSecs = $state(0);
  let mediaRec = null;
  let recChunks = [];
  let recTimer = null;
  let recStream = null;

  async function startRecording() {
    if (!ch || recording || !canRecord) return;
    try {
      recStream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch {
      flash("Microphone permission denied", "error");
      return;
    }
    recChunks = [];
    const pref = ["audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus"].find(
      (t) => MediaRecorder.isTypeSupported?.(t),
    );
    mediaRec = new MediaRecorder(recStream, pref ? { mimeType: pref } : undefined);
    mediaRec.ondataavailable = (e) => e.data.size && recChunks.push(e.data);
    mediaRec.start();
    recording = true;
    // You hold the phone to your face to record; the tap is the only signal that
    // the mic went live that you don't have to look at the screen for.
    haptic("medium");
    recSecs = 0;
    recTimer = setInterval(() => {
      if (++recSecs >= 300) stopRecording(true); // 5-minute cap
    }, 1000);
  }

  function teardownRec() {
    clearInterval(recTimer);
    recTimer = null;
    recStream?.getTracks().forEach((t) => t.stop());
    recStream = null;
    recording = false;
  }

  function stopRecording(send) {
    haptic("medium"); // …and the one that says it stopped
    const rec = mediaRec;
    mediaRec = null;
    if (!rec) {
      teardownRec();
      return;
    }
    rec.onstop = async () => {
      teardownRec();
      if (!send || !recChunks.length || !S.activeChannelId) return;
      // Clean type (no ;codecs=…) so SendFile's data-URL regex matches.
      const ogg = (rec.mimeType || "").includes("ogg");
      const blob = new Blob(recChunks, { type: ogg ? "audio/ogg" : "audio/webm" });
      if (blob.size > MAX_FILE_BYTES) {
        flash("Voice message too large", "error");
        return;
      }
      const chId = S.activeChannelId;
      uploading++;
      try {
        const dataUrl = await readAsDataURL(blob);
        await api.sendFile(chId, dataUrl, `Voice message.${ogg ? "ogg" : "webm"}`);
      } catch (err) {
        flash(err);
      } finally {
        uploading--;
      }
    };
    rec.stop();
  }

  const recClock = $derived(
    `${Math.floor(recSecs / 60)}:${(recSecs % 60).toString().padStart(2, "0")}`,
  );

  // ---- scheduled send ----
  // With a draft: pick a time and queue it (see lib/scheduled). Empty: open the
  // manager so the clock is also the way to see/cancel what's queued.
  function scheduleSend() {
    if (!S.activeChannelId) return;
    const text = replaceShortcodes(draft.trim());
    if (!text) {
      S.modal = { kind: "scheduled" };
      return;
    }
    const chId = S.activeChannelId;
    const replyTo = S.replyingTo?.id || "";
    S.modal = {
      kind: "when",
      title: "Send later",
      cta: "Schedule",
      onPick: (at) => {
        scheduleMessage(chId, text, replyTo, at);
        draft = "";
        saveDraft(chId, "");
        S.replyingTo = null;
        queueAutosize();
        flash("Message scheduled", "success");
      },
    };
  }

  function onPaste(e) {
    const item = [...(e.clipboardData?.items || [])].find((i) => i.type.startsWith("image/"));
    if (item) {
      e.preventDefault();
      stageImage(item.getAsFile());
    }
  }

  // Typing anywhere focuses the composer — the pressed character lands in the
  // box naturally (focus moves during keydown, before the char is inserted).
  // Skips modifier chords, modals/menus, and anything already editable, so
  // shortcuts and other inputs keep working untouched.
  function typeToFocus(e) {
    if (!ch || S.modal || S.contextMenu || S.editing) return;
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key.length !== 1) return; // printable characters only
    const t = e.target;
    if (t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable)) return;
    composerEl?.focus();
  }

  // Focus follows intent: switching channels or starting a reply puts the
  // caret in the box (desktop only — popping the keyboard on the phone for a
  // mere channel switch would be rude).
  $effect(() => {
    S.activeChannelId;
    if (!mobile && ch) composerEl?.focus();
  });
  $effect(() => {
    if (S.replyingTo && !mobile) composerEl?.focus();
  });

  function pickEmoji(e) {
    if (S.pickerTarget !== "composer") {
      if (S.pickerTarget) react(S.pickerTarget, e);
      S.pickerTarget = null;
      return;
    }
    // Insert at the caret the way accept() does. `draft += e` put the emoji at
    // the END of the message however far back you'd moved to fix a typo.
    const start = composerEl?.selectionStart ?? draft.length;
    const end = composerEl?.selectionEnd ?? start;
    draft = draft.slice(0, start) + e + draft.slice(end);
    const pos = start + e.length;
    saveDraft(S.activeChannelId, draft); // was only persisted on the next keystroke
    queueAutosize(); // …and the box didn't grow when a run of emoji wrapped
    requestAnimationFrame(() => composerEl?.setSelectionRange(pos, pos));
    if (mobile) {
      // The panel stays up for a run of emoji. Closing after every pick was
      // sheet down, keyboard up, sheet up again — four viewport resizes to type
      // two emoji. Deliberately no focus() either: the panel IS the keyboard
      // here, and refocusing would raise the IME underneath it.
      haptic("light");
      return;
    }
    composerEl?.focus();
    S.pickerTarget = null;
  }

  function toggleEmojiPicker() {
    if (S.pickerTarget === "composer") {
      S.pickerTarget = null;
      return;
    }
    // Drop the IME first: on a phone the panel and the keyboard would otherwise
    // stack, and the panel is what was asked for.
    if (mobile) composerEl?.blur();
    S.pickerTarget = "composer";
  }

  // ---- the "+" sheet (phone) ----
  function openMore() {
    haptic("light");
    composerEl?.blur(); // same reason as the emoji panel
    moreOpen = true;
  }
  // Every row does its thing and dismisses — a sheet that stays up over the
  // modal it just opened is a second scrim to get out of.
  function fromSheet(run) {
    moreOpen = false;
    run();
  }
  // Hardware back closes the sheet before it reaches the drawers or exits.
  $effect(() => {
    if (moreOpen) return pushLayer("sheet", () => (moreOpen = false));
  });

  function openAdvanced() {
    S.modal = {
      kind: "compose",
      initial: draft,
      // The modal seeds from the inline draft; once IT sends, the seed must go
      // too — otherwise the same text sits in the one-line box waiting for a
      // stray Enter to post it twice.
      onSent: () => {
        draft = "";
        saveDraft(S.activeChannelId, "");
        queueAutosize();
      },
    };
  }
</script>

<svelte:window onkeydown={typeToFocus} />

{#if S.replyingTo}
  <div class="reply-banner">
    <span class="rb-label">
      <span class="rb-icon"><Icon name="reply" size={12} /></span>
      Replying to <strong>{S.replyingTo.senderName || S.replyingTo.sender.slice(0, 9)}</strong>
    </span>
    <button class="mini" aria-label="Cancel reply" onclick={() => (S.replyingTo = null)}>
      <Icon name="close" size={11} />
    </button>
    {#if mobile}
      <!-- Swipe-to-reply is the gesture people trigger by accident, and Escape —
           the only other way out — isn't on a phone keyboard. The whole banner
           cancels, so the 23x15 pill isn't the sole escape. -->
      <button
        class="rb-cancel-all"
        aria-hidden="true"
        tabindex="-1"
        onclick={() => (S.replyingTo = null)}
      ></button>
    {/if}
  </div>
{/if}
<div class="typing-line muted">
  {#if uploading > 0}
    <span class="up-dot"></span> Adding {uploading > 1 ? `${uploading} attachments` : "attachment"}…
  {:else if S.typingList.length > 0}
    <span class="t-dots" aria-hidden="true"><span></span><span></span><span></span></span>
    {#if S.typingList.length === 1}
      <span
        class="typer"
        style={nameColorFor(S.typingList[0].from) ? `color:${nameColorFor(S.typingList[0].from)}` : ""}
        >{S.typingList[0].label}</span
      > is typing…
    {:else if S.typingList.length === 2}
      <span
        class="typer"
        style={nameColorFor(S.typingList[0].from) ? `color:${nameColorFor(S.typingList[0].from)}` : ""}
        >{S.typingList[0].label}</span
      > and <span
        class="typer"
        style={nameColorFor(S.typingList[1].from) ? `color:${nameColorFor(S.typingList[1].from)}` : ""}
        >{S.typingList[1].label}</span
      > are typing…
    {:else}
      {S.typingList.length} people are typing…
    {/if}
  {/if}
</div>

<!-- One textarea, two layouts. A snippet rather than a copy in each branch: two
     copies mean two `bind:this`, and whichever mounted last silently owned the
     caret-restoring code. -->
{#snippet draftBox()}
  <!-- Direction is handled in CSS, not by a dir attribute here — see the bidi
       section of app.css. dir="auto" was tried first and is subtly wrong for a
       draft box: it resolves ONE direction for the whole control, so the second
       line of a two-language message gets laid out by the first line's script,
       and it caches that answer on the element. Clearing the draft after a send
       assigns the value programmatically, which does not always re-run the
       heuristic, so the box stayed right-to-left with the caret on the wrong
       side of an empty composer. -->
  <!-- dir is set only when the writer asked for one. Left off, the textarea
       keeps `unicode-bidi: plaintext` from app.css and resolves each line on
       its own, which is what every draft did before this existed. The style
       override has to switch unicode-bidi back to `isolate` as well: plaintext
       ignores the element's direction by definition, so setting dir alone
       would do exactly nothing. -->
  <textarea
    bind:this={composerEl}
    class="draft"
    class:autofit={AUTOFIT}
    dir={dirMode || null}
    style={dirMode ? "unicode-bidi:isolate" : null}
    rows="1"
    placeholder={composerPlaceholder}
    aria-label={composerPlaceholder}
    bind:value={draft}
    disabled={!ch}
    oninput={onInput}
    onkeydown={onKeydown}
    onpaste={onPaste}
    onblur={() => setTimeout(() => (suggest = null), 150)}
  ></textarea>
  <!-- Shown only while an override is in force. Off by default it costs the
       people who never write bidirectional text nothing at all, and the ones
       who do need to be able to see which way the next line will start
       WITHOUT typing a character to find out — that guess is the whole
       problem this fixes. Clicking it cycles, so the feature is reachable
       once it has been found, and the tooltip carries the shortcut for getting
       back to it. -->
  {#if dirMode}
    <button
      type="button"
      class="dir-pill"
      use:tooltip={{
        text: `Text direction: ${dirMode === "rtl" ? "right to left" : "left to right"} — Ctrl+Shift+D to change`,
      }}
      aria-label="Text direction: {dirMode === 'rtl' ? 'right to left' : 'left to right'}. Activate to change."
      onclick={() => {
        dirMode = dirMode === "rtl" ? "ltr" : "";
        composerEl?.focus();
      }}
    >
      {dirMode === "rtl" ? "رل" : "LR"}
    </button>
  {/if}
{/snippet}

<div class="composer-wrap" class:mobile bind:this={wrapEl} style="--ep-h:{pickerH}px">
  {#if suggest}
    <div class="suggest-pop">
      <div class="suggest-head">
        {SUGGEST_HEADS[suggest.kind]}
      </div>
      {#each suggest.items as item, i (suggest.kind === "emoji" ? item[0] : suggest.kind === "slash" ? item.name : item.key)}
        <button class="suggest-item" class:sel={i === suggest.sel} onclick={() => accept(i)}>
          {#if suggest.kind === "emoji"}
            <span class="s-emoji">{item[1]}</span> :{item[0]}:
          {:else if suggest.kind === "slash"}
            <span class="s-slash" aria-hidden="true"><Icon name="code" size={13} /></span>
            <span class="s-cmd">{item.usage}</span>
            <span class="s-desc">{item.desc}</span>
          {:else if suggest.kind === "channel"}
            <span class="s-emoji">#</span>{item.name}
          {:else}
            <span class="s-emoji">@</span><span style={roleTint(item)}>{item.name}</span>
            {#if item.kind === "role"}<span class="s-desc">role</span>{/if}
            {#if item.kind === "everyone"}<span class="s-desc">notifies the whole channel</span>{/if}
          {/if}
          <kbd class="s-enter" aria-hidden="true">↵</kbd>
        </button>
      {/each}
    </div>
  {/if}
  {#if S.pickerTarget}
    <!-- onHeight: on a phone the picker is a fixed panel at the bottom edge, so
         it covered the very box it types into. Reporting its height lets the
         composer sit on top of it, the way a keyboard accessory bar does. -->
    <EmojiPicker
      onPick={pickEmoji}
      onClose={() => (S.pickerTarget = null)}
      onHeight={(h) => (pickerH = h)}
    />
  {/if}

  <form class="composer" class:mobile onsubmit={send}>
    <input
      type="file"
      multiple
      bind:this={fileInput}
      style="display:none"
      onchange={(e) => {
        // Every file, not files[0]: the picker allows a multi-select, and the
        // drop path already feeds each one through attachFile — mirror it.
        [...(e.currentTarget.files ?? [])].forEach((f) => attachFile(f));
        // Reset so picking the same file twice still fires change.
        e.currentTarget.value = "";
      }}
    />
    <!-- capture="environment" routes straight to the rear camera on phones.
         Desktop never sees this input: the attach button there clicks the
         plain picker directly, and the camera row lives in the coarse-pointer
         sheet only. -->
    <input
      type="file"
      accept="image/*"
      capture="environment"
      bind:this={cameraInput}
      style="display:none"
      onchange={(e) => {
        [...(e.currentTarget.files ?? [])].forEach((f) => attachFile(f));
        e.currentTarget.value = "";
      }}
    />
    {#if ephTTL > 0}
      <button type="button" class="eph-banner" onclick={() => (S.modal = { kind: "disappear", channelId: S.activeChannelId })}>
        <Icon name="clock" size={13} />
        <span>Messages disappear after <strong>{ttlLabel(ephTTL)}</strong></span>
        <span class="eph-change">change</span>
      </button>
    {/if}
    <div class="input-shell" class:active={!!ch}>
    {#if pending.length || uploading > 0}
      <div class="attach-tray">
        {#each pending as p (p.id)}
          <div class="att-chip" class:file={!p.isImage} class:spoilered={p.spoiler}>
            {#if p.isImage}
              <img src={p.dataUrl} alt="" class:blur={p.spoiler} />
              {#if p.spoiler}<span class="att-tag">SPOILER</span>{/if}
            {:else}
              <span class="att-file"><Icon name={p.isVideo ? "play" : "attach"} size={16} /><span class="att-name">{p.name}</span></span>
            {/if}
            {#if mobile}
              <!-- Touch gets ONE target — the chip itself — instead of three
                   19px buttons 2px apart in the corner of a 66px thumbnail,
                   where aiming at "edit" regularly deleted the attachment. The
                   spoiler and remove controls move into the panel below, at a
                   size a finger can actually hit. -->
              <button
                type="button"
                class="att-open"
                aria-label="Attachment options for {p.name || p.origName || 'image'}"
                aria-expanded={editingAtt === p.id}
                onclick={() => (editingAtt = editingAtt === p.id ? "" : p.id)}
              ></button>
            {/if}
            <div class="att-tools">
              {#if p.isImage}
                <button
                  type="button"
                  class="att-tool"
                  class:on={p.spoiler}
                  aria-label={p.spoiler ? "Not a spoiler" : "Mark as spoiler"}
                  aria-pressed={!!p.spoiler}
                  use:tooltip
                  onclick={() => toggleSpoiler(p.id)}
                >
                  <Icon name="spoiler" size={13} />
                </button>
              {/if}
              <button
                type="button"
                class="att-tool"
                class:on={editingAtt === p.id}
                aria-label="Edit name and description"
                use:tooltip={"Edit"}
                onclick={() => (editingAtt = editingAtt === p.id ? "" : p.id)}
              >
                <Icon name="edit" size={13} />
              </button>
              <button
                type="button"
                class="att-tool danger"
                aria-label="Remove attachment"
                use:tooltip={"Remove"}
                onclick={() => removePending(p.id)}
              >
                <Icon name="trash" size={13} />
              </button>
            </div>
          </div>
        {/each}
        {#if uploading > 0}<div class="att-chip loading"><span class="att-spin"></span></div>{/if}
      </div>
      {#if editingAtt && pending.some((p) => p.id === editingAtt)}
        {@const p = pending.find((x) => x.id === editingAtt)}
        <!-- Inline rather than a dialog: you're mid-message, and a modal over
             the composer to rename a file is a heavier interruption than the
             edit is worth. -->
        <div class="att-edit">
          <label>
            <span>File name</span>
            <!-- Placeholder, not value: an unedited field must stay empty so the
                 send path can still use the v1 token older clients understand. -->
            <input
              value={p.name || ""}
              oninput={(e) => setAtt(p.id, "name", e.currentTarget.value)}
              placeholder={p.origName || "image.png"}
            />
          </label>
          <label>
            <span>Description</span>
            <input
              value={p.desc || ""}
              oninput={(e) => setAtt(p.id, "desc", e.currentTarget.value)}
              maxlength="500"
              placeholder="Describe it for people who can't see it"
            />
          </label>
          {#if mobile}
            <!-- The overlay controls the chip no longer carries. Full-width rows,
                 with the destructive one last and visually apart. -->
            <div class="att-acts">
              {#if p.isImage}
                <button type="button" class="att-act" class:on={p.spoiler} aria-pressed={!!p.spoiler} onclick={() => toggleSpoiler(p.id)}>
                  <Icon name="spoiler" size={16} /> {p.spoiler ? "Not a spoiler" : "Mark as spoiler"}
                </button>
              {/if}
              <button type="button" class="att-act danger" onclick={() => removePending(p.id)}>
                <Icon name="trash" size={16} /> Remove
              </button>
            </div>
          {/if}
          <button type="button" class="att-done" onclick={() => (editingAtt = "")}>Done</button>
        </div>
      {/if}
    {/if}
    {#if showFmtBar}
    <div class="fmt-bar" role="toolbar" aria-label="Text formatting">
      {#each FMT_GROUPS as group, gi (gi)}
        {#if gi > 0}<span class="fmt-sep" aria-hidden="true"></span>{/if}
        {#each group as b (b.kind)}
          <button
            type="button"
            class="fmtbtn"
            use:tooltip={{ text: fmtTitle(b) }}
            aria-label={b.label}
            disabled={!ch}
            onmousedown={(e) => e.preventDefault()}
            onclick={() => applyFormat(b.kind)}
          >
            <Icon name={b.kind} size={15} />
          </button>
        {/each}
      {/each}
    </div>
    {/if}
    <div class="input-box" class:focused={ch} class:recording>
      {#if recording}
        <!-- Recording a voice message: the whole row becomes the transport. -->
        <span class="rec-dot" aria-hidden="true"></span>
        <span class="rec-label">Recording… <span class="rec-clock">{recClock}</span></span>
        <button
          type="button"
          class="iconbtn rec-cancel"
          use:tooltip={"Discard"}
          aria-label="Discard recording"
          onclick={() => stopRecording(false)}
        >
          <Icon name="trash" size={18} />
        </button>
        <button
          type="button"
          class="sendbtn"
          use:tooltip
          aria-label="Send voice message"
          onclick={() => stopRecording(true)}
        >
          <Icon name="send" size={17} />
        </button>
      {:else if mobile}
        <!-- The phone row: "+", the text, emoji, and mic-or-send. Four targets,
             one line, at any width from 360px up — and the fourth slot is always
             occupied, so the composer no longer changed height on the first
             keystroke as the mic dropped out from under the text. -->
        <button
          type="button"
          class="iconbtn morebtn"
          use:tooltip={"More"}
          aria-label="Attach a file, GIF, poll, or more"
          aria-expanded={moreOpen}
          disabled={!ch}
          onclick={openMore}
        >
          <Icon name="plus" size={22} />
        </button>
        {@render draftBox()}
        <button
          type="button"
          class="iconbtn"
          use:tooltip={"Emoji"}
          aria-label="Emoji picker"
          disabled={!ch}
          onclick={toggleEmojiPicker}
        >
          <Icon name="smile" size={22} />
        </button>
        {#if canRecord && !canSend}
          <!-- Mic and send share one slot, the way every phone messenger does
               it: nothing to send means the thing you'd want is the mic. -->
          <button
            type="button"
            class="iconbtn"
            use:tooltip
            aria-label="Record a voice message"
            disabled={!ch}
            onclick={startRecording}
          >
            <Icon name="mic" size={22} />
          </button>
        {:else}
          {#if slowLeft > 0}
            <!-- role=img so the label is actually exposed: aria-label on a bare
                 span is prohibited and ignored, and the visible text here is a
                 bare countdown that names nothing on its own. -->
            <span
              class="slow-chip"
              role="img"
              use:tooltip={{ text: `Slow mode — one message per ${slowSecs}s` }}
              aria-label="Slow mode — one message per {slowSecs}s"
            >
              <Icon name="clock" size={11} /> {slowLeft}s
            </span>
          {/if}
          <button type="submit" class="sendbtn" class:launch={launching} aria-label="Send" disabled={!canSend}>
            <Icon name="send" size={17} />
          </button>
        {/if}
      {:else}
        <button
          type="button"
          class="iconbtn"
          use:tooltip={"Attach a file or image (or paste / drop one)"}
          aria-label="Attach a file"
          disabled={!ch || uploading > 0}
          onclick={() => fileInput.click()}
        >
          <Icon name="attach" size={20} />
        </button>
        {@render draftBox()}
        {#if canRecord && !draft.trim() && pending.length === 0}
          <!-- Mic replaces nothing; it appears when there's no text/attachment to
               send, the way messengers surface record-vs-send. -->
          <button
            type="button"
            class="iconbtn"
            use:tooltip
            aria-label="Record a voice message"
            disabled={!ch}
            onclick={startRecording}
          >
            <Icon name="mic" size={20} />
          </button>
        {/if}
        {#if tight}
          <!-- Squeezed column: one door for the five occasional controls. Words
               rather than glyphs, since a menu has the room for them. -->
          <Menu label="More" icon="dots" up>
            <button class="menu-item" disabled={!ch} onclick={() => (S.modal = { kind: "gifs" })}>
              <Icon name="play" size={14} /> GIF
            </button>
            <button class="menu-item" disabled={!ch} onclick={() => (S.modal = { kind: "poll" })}>
              <Icon name="poll" size={14} /> Poll
            </button>
            <button class="menu-item" disabled={!ch} onclick={openAdvanced}>
              <Icon name="heading" size={14} /> Advanced composer
            </button>
            <button
              class="menu-item"
              disabled={!ch}
              onclick={() => { sealNext = !sealNext; if (sealNext) haptic("light"); composerEl?.focus(); }}
            >
              <Icon name="diamond" size={14} />
              {sealNext ? "Don't seal the send time" : "Seal the send time"}
            </button>
            <button class="menu-item" disabled={!ch} onclick={scheduleSend}>
              <Icon name="clock" size={14} />
              {draft.trim() ? "Schedule this message" : "Scheduled messages"}
            </button>
          </Menu>
        {:else}
          <!-- The guild's own GIF pack. A word, not a glyph: there is no icon for
               "GIF" that anyone reads correctly, and this is the label every
               other client uses.

               No vendor named in the tooltip: which GIF service the Search tab
               reaches is the rendezvous operator's choice, and the picker itself
               reports it. A hardcoded "Tenor" here went stale the day Google shut
               that API down (30 June 2026). -->
          <button
            type="button"
            class="iconbtn gifbtn"
            use:tooltip={"GIFs — this guild's pack, or a web search via your rendezvous"}
            aria-label="Open the GIF picker"
            disabled={!ch}
            onclick={() => (S.modal = { kind: "gifs" })}
          >GIF</button>
          <button
            type="button"
            class="iconbtn"
            use:tooltip
            aria-label="Create a poll"
            disabled={!ch}
            onclick={() => (S.modal = { kind: "poll" })}
          >
            <Icon name="poll" size={20} />
          </button>
          <button
            type="button"
            class="iconbtn"
            use:tooltip={"Advanced composer (colours, rich embeds, preview)"}
            aria-label="Advanced composer"
            disabled={!ch}
            onclick={openAdvanced}
          >
            <Icon name="heading" size={19} />
          </button>
          <!-- Seal: arms a permanent timestamp on the next message. Distinct from
               the clock beside it, which schedules a message for LATER — this one
               records exactly when it went out. -->
          <button
            type="button"
            class="iconbtn sealbtn"
            class:armed={sealNext}
            use:tooltip={{
              text: sealNext
                ? "Sealing the send time onto this message — click to cancel"
                : "Seal the exact send time onto this message",
            }}
            aria-label="Seal timestamp"
            aria-pressed={sealNext}
            disabled={!ch}
            onclick={() => { sealNext = !sealNext; if (sealNext) haptic("light"); composerEl?.focus(); }}
          >
            <Icon name="diamond" size={18} />
          </button>
          <button
            type="button"
            class="iconbtn"
            use:tooltip={{ text: draft.trim() ? "Schedule this message" : "Scheduled messages & reminders" }}
            aria-label="Schedule message"
            disabled={!ch}
            onclick={scheduleSend}
          >
            <Icon name="clock" size={20} />
          </button>
        {/if}
        <button
          type="button"
          class="iconbtn"
          use:tooltip={"Emoji"}
          aria-label="Emoji picker"
          disabled={!ch}
          onclick={toggleEmojiPicker}
        >
          <Icon name="smile" size={20} />
        </button>
      {/if}
    </div>
    </div>
  </form>
</div>

{#if moreOpen}
  <!-- A sheet, not a popover: this is the phone's "everything else" drawer, so
       it gets labels. Half these actions were unlabelled 44px glyphs before,
       and "which one was the heading icon" is not a question a composer should
       ask. -->
  <BottomSheet title="Add to your message" onClose={() => (moreOpen = false)} maxHeight="76vh">
    <div class="sheet-list">
      <button type="button" class="sheet-row" onclick={() => fromSheet(() => fileInput.click())}>
        <span class="sr-icon"><Icon name="attach" size={20} /></span>
        <span class="sr-text">
          <span class="sr-label">Photo or file</span>
          <span class="sr-sub">Up to 5 MB images, 25 MB files</span>
        </span>
      </button>
      {#if coarse}
        <!-- coarse, not mobile: the sheet also shows in a narrowed desktop
             window, where "take a photo" would open a file picker and shrug. -->
        <button type="button" class="sheet-row" onclick={() => fromSheet(() => cameraInput.click())}>
          <span class="sr-icon"><Icon name="camera" size={20} /></span>
          <span class="sr-text">
            <span class="sr-label">Take a photo</span>
            <span class="sr-sub">Straight from the camera</span>
          </span>
        </button>
      {/if}
      <button type="button" class="sheet-row" onclick={() => fromSheet(() => (S.modal = { kind: "gifs" }))}>
        <span class="sr-icon sr-gif">GIF</span>
        <span class="sr-text">
          <span class="sr-label">GIF</span>
          <!-- No vendor named: which service the Search tab reaches is the
               rendezvous operator's choice, and the picker reports it. -->
          <span class="sr-sub">This guild's pack, or a search via your rendezvous</span>
        </span>
      </button>
      <button type="button" class="sheet-row" onclick={() => fromSheet(() => (S.modal = { kind: "poll" }))}>
        <span class="sr-icon"><Icon name="poll" size={20} /></span>
        <span class="sr-text"><span class="sr-label">Poll</span></span>
      </button>
      <button type="button" class="sheet-row" onclick={() => fromSheet(() => (showFmt = !showFmt))}>
        <span class="sr-icon sr-aa">Aa</span>
        <span class="sr-text">
          <span class="sr-label">Formatting</span>
          <span class="sr-sub">Bold, italics, code, quotes, links</span>
        </span>
        <span class="sr-state">{showFmt ? "On" : "Off"}</span>
      </button>
      <button type="button" class="sheet-row" onclick={() => fromSheet(openAdvanced)}>
        <span class="sr-icon"><Icon name="heading" size={20} /></span>
        <span class="sr-text">
          <span class="sr-label">Advanced composer</span>
          <span class="sr-sub">Colours, rich embeds, preview</span>
        </span>
      </button>
      <button type="button" class="sheet-row" onclick={() => fromSheet(scheduleSend)}>
        <span class="sr-icon"><Icon name="clock" size={20} /></span>
        <span class="sr-text">
          <span class="sr-label">{draft.trim() ? "Send later" : "Scheduled & reminders"}</span>
          <span class="sr-sub">
            {draft.trim() ? "Pick a time for this message" : "See what's queued"}
          </span>
        </span>
      </button>
    </div>
  </BottomSheet>
{/if}

<style>
  .eph-banner {
    display: flex;
    align-items: center;
    gap: 7px;
    width: 100%;
    padding: 6px 12px;
    margin-bottom: var(--sp-1);
    font-size: var(--fs-compact);
    color: var(--text-muted);
    background: color-mix(in srgb, var(--accent) 8%, var(--bg-input));
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
    border-radius: var(--radius-md);
    text-align: left;
  }
  .eph-banner :global(svg) {
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  .eph-banner strong {
    color: var(--text);
    font-weight: 600;
  }
  .eph-change {
    margin-left: auto;
    color: var(--accent-hover);
    font-weight: 600;
  }
  .eph-banner:hover .eph-change {
    text-decoration: underline;
  }
  /* Touch: the banner is the only route to the disappearing-timer settings, and
     its "change" affordance announced itself with a hover underline no finger
     can produce — so it read as static text. Underline it always, and give the
     row a real target. */
  @media (pointer: coarse) {
    .eph-banner {
      min-height: var(--tap-min);
      font-size: var(--fs-ui);
    }
    .eph-change {
      text-decoration: underline;
    }
  }
  .reply-banner {
    position: relative; /* anchors the phone-wide cancel overlay */
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: var(--sp-2);
    padding: 6px 16px;
    font-size: var(--fs-ui);
    border-top: 1px solid var(--border);
    /* Faint accent wash ties the banner to the reply you're composing. */
    background: color-mix(in srgb, var(--accent) 7%, transparent);
    animation: rb-in var(--dur-standard) var(--ease-out);
  }
  @keyframes rb-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  .rb-label {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rb-label strong {
    color: var(--text);
    font-weight: 600;
  }
  .rb-icon {
    display: inline-flex;
    flex-shrink: 0;
    color: var(--accent-hover);
  }
  /* Covers the banner's dead space so the whole strip cancels; the pill sits
     above it (position:relative below) and keeps its own focus ring. */
  .rb-cancel-all {
    position: absolute;
    inset: 0;
    background: transparent;
    border: none;
    border-radius: 0;
    padding: 0;
  }
  .rb-label,
  .reply-banner .mini {
    /* Both are earlier siblings than the overlay, so without a lift the overlay
       paints over them and the pill's own focus/hover would never fire. */
    position: relative;
    z-index: 1;
  }
  /* Fixed height, not min-height: the strip is always in the DOM, so anything
     that grows with its content shifts the whole composer the moment someone
     starts typing. The phone value is the larger --fs-compact plus its padding. */
  .typing-line {
    height: 20px;
    font-size: var(--fs-compact);
    font-style: italic;
    padding: 3px 16px 1px;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .typing-line {
      height: 24px;
      padding-left: var(--sp-edge);
      padding-right: var(--sp-edge);
    }
  }
  .typing-line .typer {
    font-weight: 600;
    font-style: normal;
    /* Truncate the NAME, not the sentence. The line already ellipsed as a
       whole, which on a long display name ate "is typing…" and left a name
       hanging there with no verb after it. */
    display: inline-block;
    max-width: 15ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: bottom;
  }
  /* Three staggered bouncing dots ahead of "X is typing…". */
  .t-dots {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-right: 5px;
    vertical-align: middle;
  }
  .t-dots span {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
    animation: t-bounce 1.2s ease-in-out infinite;
  }
  .t-dots span:nth-child(2) {
    animation-delay: var(--dur-standard);
  }
  .t-dots span:nth-child(3) {
    animation-delay: 0.3s;
  }
  @keyframes t-bounce {
    0%,
    55%,
    100% {
      transform: none;
      opacity: 0.35;
    }
    28% {
      transform: translateY(-3px);
      opacity: 1;
    }
  }
  /* The global reduced-motion rule only shortens durations — an infinite loop
     would still churn, so stop the dots entirely and hold a steady frame. */
  @media (prefers-reduced-motion: reduce) {
    .t-dots span,
    .up-dot {
      animation: none;
      opacity: 0.7;
    }
  }
  .up-dot {
    display: inline-block;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    animation: up-pulse 0.9s ease-in-out infinite;
    vertical-align: middle;
  }
  @keyframes up-pulse {
    50% {
      opacity: 0.3;
    }
  }
  .composer-wrap {
    position: relative;
  }
  .suggest-pop {
    position: absolute;
    /* Anchor to the wrap's top edge, not a fixed offset — the mobile input is
       taller than desktop's, and a hardcoded bottom overlapped it. */
    bottom: calc(100% + 6px);
    left: 60px;
    /* Glassy floating panel over the feed, matching the command palette. */
    background: color-mix(in srgb, var(--bg-1) 90%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: var(--sp-1);
    display: flex;
    flex-direction: column;
    min-width: 240px;
    box-shadow: var(--shadow-pop);
    z-index: 50;
    transform-origin: bottom left;
    animation: sg-pop var(--dur-quick) var(--ease-out);
  }
  @keyframes sg-pop {
    from {
      opacity: 0;
      transform: translateY(4px) scale(0.98);
    }
  }
  /* On a phone the popover docks to both edges instead of floating: left:60px
     was measured against the desktop composer's attach button, which the phone
     layout no longer has anywhere near there, and min-width:240px with no
     max-width ran a long member name off the right edge of a 360px screen.
     Rows get the touch floor too — they sit directly above the keyboard, where
     a mis-tap inserts the wrong mention into the message. */
  .composer-wrap.mobile .suggest-pop {
    left: var(--sp-2);
    right: var(--sp-2);
    min-width: 0;
  }
  .composer-wrap.mobile .suggest-item {
    min-height: var(--tap-min);
    padding: 10px 12px;
  }
  .suggest-item {
    display: flex;
    align-items: center;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 6px 10px;
    border-radius: var(--radius-sm);
    font-size: var(--fs-ui);
    font-family: ui-monospace, monospace;
    /* A long display name used to push the panel past the right edge — nothing
       here wrapped or truncated, and .suggest-pop has a min-width. */
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    transition:
      background 0.1s ease,
      transform var(--dur-quick) ease;
  }
  .suggest-item.sel,
  .suggest-item:hover {
    background: var(--bg-3);
    transform: translateX(2px);
  }
  .suggest-item.sel {
    background: color-mix(in srgb, var(--accent) 12%, var(--bg-3));
    box-shadow: inset 2px 0 0 var(--accent);
  }
  /* ↵ affordance on the selected row only (matches the command palette). */
  .s-enter {
    margin-left: auto;
    padding-left: var(--sp-3);
    font-family: inherit;
    font-size: var(--fs-small);
    color: var(--accent-hover);
    opacity: 0;
    transition: opacity var(--dur-quick) ease;
  }
  .suggest-item.sel .s-enter {
    opacity: 0.9;
  }
  .s-emoji {
    font-size: 16px;
    margin-right: 6px;
  }
  .s-slash {
    display: inline-flex;
    color: var(--accent-hover);
    margin-right: 7px;
    opacity: 0.9;
  }
  .suggest-head {
    font-size: var(--fs-micro);
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--text-muted);
    padding: 4px 10px 3px;
  }
  .s-cmd {
    font-weight: 600;
  }
  /* Description trails the command in the UI font (the command stays mono). */
  .s-desc {
    flex: 1;
    min-width: 0;
    margin-left: 14px;
    font-family:
      system-ui,
      -apple-system,
      sans-serif;
    font-size: var(--fs-compact);
    color: var(--text-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .composer {
    padding: 0 var(--sp-4) var(--sp-4);
  }
  /* Formatting toolbar: a whisper-quiet row above the input that comes up to
     full strength while the composer is hovered or focused. No transforms, so
     the global reduced-motion duration-zeroing covers it. */
  /* One seamless input shell: toolbar, attach, text, and emoji all live
     INSIDE a single rounded well — no borders between the pieces. */
  .input-shell {
    background: var(--bg-input);
    border: 1px solid transparent;
    border-radius: var(--radius-lg);
    transition:
      border-color var(--dur-standard) ease,
      background var(--dur-standard) ease,
      box-shadow var(--dur-standard) ease;
  }
  .input-shell.active:focus-within {
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 12%, transparent);
    background: color-mix(in srgb, var(--accent) 3%, var(--bg-input));
  }
  .attach-tray {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
    padding: 10px 10px 6px;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
    /* Several staged files make this row scroll; without containment the flick
       that runs out of tray carries on into the conversation behind it. */
    overscroll-behavior: contain;
  }
  .att-chip {
    position: relative;
    border-radius: var(--radius-md);
    overflow: hidden;
    background: color-mix(in srgb, var(--border) 30%, var(--bg-input));
    border: 1px solid color-mix(in srgb, var(--border) 55%, transparent);
  }
  /* Fixed height, free width: a 64x64 cover-crop showed the middle of every
     staged picture, which is the one thing that cannot tell you whether you
     picked the right file. The row still lines up — every chip is the same
     height — and each preview is now recognisably its own image. */
  .att-chip img {
    display: block;
    height: 64px;
    width: auto;
    max-width: 148px;
    min-width: 40px;
    object-fit: contain;
    transition: filter 0.18s ease;
  }
  /* The staged preview blurs too, so "spoiler" is a state you can see before
     you send rather than a promise about what the other side will get. */
  .att-chip img.blur {
    filter: blur(8px);
  }
  .att-tag {
    position: absolute;
    left: 3px;
    bottom: 3px;
    padding: 0 4px;
    border-radius: 3px;
    background: rgba(0, 0, 0, 0.72);
    color: #fff;
    font-size: var(--fs-micro);
    font-weight: 700;
    letter-spacing: 0.4px;
    pointer-events: none;
  }
  /* The three controls, revealed on hover like Discord. A mouse can land on a
     19px button; a fingertip covers all three, and the middle one is a delete
     with no undo. On touch the chip is the target instead (.att-open) and these
     move into the edit panel — see .att-acts. */
  .att-tools {
    position: absolute;
    top: 3px;
    right: 3px;
    display: flex;
    gap: 2px;
    transition: opacity var(--dur-quick) ease;
  }
  /* Spoiler and Edit stay hover-only — they are refinements, and three 19px
     buttons permanently over a 64px thumbnail is more chrome than picture.
     Remove does not: "how do I take this one back off?" is the question a
     staged attachment actually raises, and answering it only on hover means a
     keyboard user tabs through the tray looking for a control that is drawn at
     zero opacity. */
  .att-tool:not(.danger) {
    opacity: 0;
  }
  .att-chip:hover .att-tool,
  .att-chip:focus-within .att-tool {
    opacity: 1;
  }
  .att-tools .att-tool.danger {
    opacity: 0.75;
  }
  .att-chip:hover .att-tool.danger,
  .att-tool.danger:focus-visible {
    opacity: 1;
  }
  /* Scoped to the layout class, not the media query: whether .att-open exists at
     all is a JS decision, and the two must never disagree — a hidden overlay
     with nothing under it is a chip with no controls. */
  .composer.mobile .att-tools {
    display: none;
  }
  .att-open {
    position: absolute;
    inset: 0;
    background: transparent;
    border: none;
    border-radius: 0;
    padding: 0;
  }
  .att-open[aria-expanded="true"] {
    box-shadow: inset 0 0 0 2px var(--accent);
  }
  /* The chip IS the control on touch, so it has to be a target — and a 64px
     square is the floor with nothing spare for a thumb landing off-centre. */
  .composer.mobile .att-chip img,
  .composer.mobile .att-chip.loading {
    width: 84px;
    height: 84px;
  }
  /* The 34px right padding held the hover tools clear of the filename; with the
     tools gone on touch it's just a truncated name for no reason. */
  .composer.mobile .att-file {
    padding-right: var(--sp-3);
    min-height: var(--tap-min);
  }
  /* Full-width rows in the edit panel — spoiler and, last and tinted, remove. */
  .att-acts {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-2);
    width: 100%;
  }
  .att-act {
    flex: 1 1 140px;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
    min-height: var(--tap-min);
    padding: 0 12px;
    font-size: var(--fs-ui);
    font-weight: 600;
    color: var(--text);
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }
  .att-act.on {
    background: var(--accent);
    color: var(--accent-fg);
    border-color: transparent;
  }
  .att-act.danger {
    color: var(--danger-text);
    border-color: color-mix(in srgb, var(--danger) 45%, var(--border));
  }
  .att-tool {
    width: 19px;
    height: 19px;
    display: grid;
    place-items: center;
    padding: 0;
    border: none;
    border-radius: var(--radius-sm);
    background: rgba(0, 0, 0, 0.68);
    color: #fff;
  }
  .att-tool:hover {
    background: rgba(0, 0, 0, 0.86);
  }
  .att-tool.on {
    background: var(--accent);
  }
  .att-tool.danger:hover {
    background: var(--danger, #d9534f);
  }
  .att-edit {
    display: flex;
    align-items: flex-end;
    gap: var(--sp-2);
    flex-wrap: wrap;
    margin: 6px 0 2px;
    padding: var(--sp-2);
    border-radius: var(--radius-md);
    background: var(--bg-3);
  }
  .att-edit label {
    display: flex;
    flex-direction: column;
    gap: 3px;
    flex: 1;
    min-width: 130px;
  }
  .att-edit label span {
    font-size: var(--fs-tiny);
    font-weight: 700;
    letter-spacing: 0.3px;
    text-transform: uppercase;
    color: var(--text-muted);
  }
  .att-edit input {
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    font-size: var(--fs-compact);
    padding: 5px 7px;
    min-width: 0;
  }
  .att-done {
    padding: 6px 12px;
    border-radius: var(--radius-sm);
    background: var(--accent);
    color: var(--accent-fg);
    font-size: var(--fs-compact);
    font-weight: 600;
  }
  /* The attachment editor is deliberately inline in the composer rather than a
     dialog, so Modal.svelte's 16px/44px touch floor never reached it — and the
     accessibility description is the last field that should be a 27px control
     that makes iOS zoom the page in and never back out. */
  @media (pointer: coarse) {
    .att-edit input {
      font-size: 16px;
      min-height: var(--tap-min);
      padding: 8px 10px;
    }
    .att-done {
      min-height: var(--tap-min);
      padding: 0 20px;
      font-size: var(--fs-ui);
    }
  }
  .att-chip.file {
    display: flex;
    align-items: center;
    min-height: 40px;
  }
  .att-file {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 34px 8px 10px;
    max-width: 220px;
    color: var(--text-muted);
  }
  .att-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--fs-ui);
  }
  .att-chip.loading {
    width: 64px;
    height: 64px;
    display: grid;
    place-items: center;
  }
  .att-spin {
    width: 20px;
    height: 20px;
    border: 2px solid color-mix(in srgb, var(--border) 60%, transparent);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: att-spin 0.7s linear infinite;
  }
  @keyframes att-spin {
    to {
      transform: rotate(360deg);
    }
  }
  .fmt-bar {
    display: flex;
    align-items: center;
    gap: 1px;
    padding: 5px 8px 4px;
    /* Quiet until the composer is hovered or focused, but not invisible: at
       0.45 these icons sat at 1.9:1 against the input they float over. */
    opacity: 0.8;
    transition: opacity var(--dur-standard) ease;
    border-bottom: 1px solid color-mix(in srgb, var(--border) 45%, transparent);
  }
  .composer:hover .fmt-bar,
  .composer:focus-within .fmt-bar {
    opacity: 1;
  }
  .fmtbtn {
    display: grid;
    place-items: center;
    width: 26px;
    height: 22px;
    padding: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    transition:
      background var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  .fmtbtn:hover:not(:disabled) {
    background: var(--bg-3);
    color: var(--text);
  }
  .fmtbtn:active:not(:disabled) {
    background: var(--bg-3);
  }
  .fmtbtn:disabled {
    opacity: 0.35;
  }
  .fmt-sep {
    width: 1px;
    height: 14px;
    background: var(--border);
    margin: 0 5px;
    flex: none;
  }
  /* One unified rounded bar — icons live inside it, Discord-style. */
  .input-box {
    display: flex;
    align-items: flex-end;
    gap: 3px;
    background: transparent;
    border: none;
    border-radius: 0;
    padding: 3px 8px;
  }
  .input-box.recording {
    align-items: center;
    padding: 8px 10px;
  }
  .rec-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--danger);
    flex-shrink: 0;
    animation: rec-pulse 1.1s ease-in-out infinite;
  }
  .rec-label {
    flex: 1;
    min-width: 0;
    font-size: var(--fs-ui);
    color: var(--text-muted);
    /* Long-pressing a text label raises Android's selection handles and the
       Copy/Share bar over the composer, mid-recording. */
    -webkit-user-select: none;
    user-select: none;
  }
  .rec-clock {
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }
  .rec-cancel:hover :global(svg) {
    color: var(--danger-text);
  }
  @keyframes rec-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .rec-dot {
      animation: none;
    }
  }
  /* (Focus ring moved up to .input-shell — the whole well glows as one.) */
  /* The base-direction indicator. Deliberately small and quiet: it is a mode
     marker, not an action, and it only exists at all while a mode is on.
     `align-self: end` keeps it on the last line of a draft that has grown to
     several, next to where the next character will actually land. */
  .dir-pill {
    flex: none;
    align-self: end;
    margin-bottom: var(--sp-1);
    padding: 1px 5px;
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    background: var(--bg-3, transparent);
    color: var(--text-dim);
    font-size: var(--fs-tiny);
    line-height: 1.5;
    font-weight: 600;
    letter-spacing: 0.04em;
    cursor: pointer;
  }
  .dir-pill:hover {
    color: var(--text);
    border-color: var(--text-dim);
  }

  .draft {
    flex: 1;
    min-width: 0;
    resize: none;
    /* hidden, not auto: autosize() switches this on only when the draft really
       exceeds max-height. See the note there. */
    overflow-y: hidden;
    /* Pre-mount default only — autosize() sets the real cap, which on a phone is
       a share of what the keyboard leaves. */
    max-height: 200px;
    height: auto;
    /* Once the draft scrolls, running out of textarea used to hand the flick to
       the message feed: you lose your place in the draft AND in the conversation.
       html/body's overscroll-behavior in app.css doesn't stop inner chaining. */
    overscroll-behavior: contain;
    /* Naked inside the shell: the global textarea "recessed well" styling
       would draw a SECOND border/inset inside the composer. */
    background: transparent !important;
    border: none !important;
    box-shadow: none !important;
    outline: none !important; /* the SHELL carries the focus ring */
    border-radius: 0;
    /* Vertical padding tuned so a single-line draft sits at the same height as
       the 34px icon buttons beside it — no more text baseline floating above the
       controls on an empty/one-line composer. */
    padding: 7px 6px;
    font-family: inherit;
    line-height: 1.4;
    box-sizing: border-box;
    width: auto;
  }
  /* Where the engine supports it, the box grows to fit its own value and JS is
     left holding only the cap — see AUTOFIT in autosize(). `auto` rather than
     the `hidden` above because the height is no longer a rounded number of ours
     that the content can sit a fraction of a pixel outside of. */
  .draft.autofit {
    field-sizing: content;
    overflow-y: auto;
  }
  .draft:focus {
    border: none;
  }
  /* Bare icon buttons: muted glyphs that brighten on hover, no box. Fixed square
     so every tray control occupies the same footprint regardless of its glyph's
     intrinsic size — the row reads as an even set, not a jumble of sizes. */
  /* The seal reads as OFF by default and unmistakably ON when armed: the icon
     takes the accent, the button gets a soft ring, and it breathes slowly so a
     glance at the composer tells you the next message will be marked. Motion is
     opacity/transform only, and prefers-reduced-motion drops it. */
  .sealbtn {
    position: relative; /* anchors the armed ring below */
  }
  .sealbtn.armed {
    color: var(--accent);
    background: var(--accent-soft);
  }
  .sealbtn.armed::after {
    content: "";
    position: absolute;
    inset: -2px;
    border-radius: inherit;
    border: 1px solid color-mix(in srgb, var(--accent) 55%, transparent);
    animation: seal-breathe 2.4s ease-in-out infinite;
    pointer-events: none;
  }
  @keyframes seal-breathe {
    0%, 100% { opacity: 0.45; transform: scale(1); }
    50% { opacity: 1; transform: scale(1.06); }
  }
  @media (prefers-reduced-motion: reduce) {
    .sealbtn.armed::after { animation: none; opacity: 0.8; }
  }

  /* The overflow trigger wears the composer's own icon-button clothes rather
     than Menu's default ghost chrome — inside the input well a bordered button
     reads as a separate control sitting on top of the field. */
  .input-box :global(.menu-root) {
    align-self: flex-end;
  }
  .input-box :global(.menu-root > .trigger) {
    width: 34px;
    height: 34px;
    padding: 0;
    background: transparent;
    border: none;
    box-shadow: none;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
  }
  .input-box :global(.menu-root > .trigger:hover) {
    background: transparent;
    color: var(--text);
  }
  .iconbtn {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    padding: 0;
    flex-shrink: 0;
    background: transparent;
    color: var(--text-muted);
    border-radius: var(--radius-sm);
    align-self: flex-end;
    transition:
      color var(--dur-quick) ease,
      transform var(--dur-quick) ease;
  }
  .iconbtn:hover:not(:disabled) {
    background: transparent;
    color: var(--text);
    transform: scale(1.1);
  }
  .iconbtn:active:not(:disabled) {
    transform: scale(0.92);
  }
  .iconbtn:disabled {
    opacity: 0.4;
  }
  /* app.css already zeroes transition durations under reduced motion; also
     drop the scale so hover doesn't snap the glyph around. */
  @media (prefers-reduced-motion: reduce) {
    .iconbtn:hover:not(:disabled),
    .iconbtn:active:not(:disabled) {
      transform: none;
    }
  }
  .mini {
    padding: 2px 6px;
    background: transparent;
    color: var(--text-muted);
    display: grid;
    place-items: center;
  }
  .mini:hover {
    background: var(--bg-3);
    color: var(--text);
  }

  /* ---- mobile composer ------------------------------------------------
     ONE row: "+", the text, emoji, and mic-or-send. The old layout put eight
     44px controls in a wrapping tray under the text — 352px of targets in the
     322px a 360px handset leaves, so the emoji button orphaned onto a third
     row, and typing one character dropped the mic and snapped the whole
     composer 44px shorter. Four targets is 132px whatever the width, and the
     fourth slot always holds something (mic OR send), so the height is fixed.
     Everything the row lost is in the "+" sheet, with labels. */
  .composer.mobile {
    /* The gesture-bar inset is only worth reserving when nothing is already
       covering it — subtract whatever the keyboard or the emoji panel occupies
       rather than stacking a dead strip on top of them. */
    /* NO safe-area inset here. The composer sits inside .mshell, which already
       pads its own bottom by the system inset (and by the keyboard), so adding
       it again applied the gesture bar's height TWICE — a dead strip under the
       composer that read as "the bottom is padded weirdly and wastes space".
       One owner per edge: the outermost positioned container. */
    padding: 0 var(--sp-3) var(--sp-2);
  }
  /* Reserve what the software keyboard covers (0 when the platform already
     resized the layout viewport for us) and, while the emoji panel is up, its
     height too — so the composer rides ON TOP of the panel like a keyboard
     accessory bar instead of behind it. Margin rather than transform: this is
     the last child of the chat column, so the feed above it shrinks by the same
     amount and the newest message stays visible. */
  .composer-wrap.mobile {
    margin-bottom: calc(var(--kb-inset, 0px) + var(--ep-h, 0px));
    transition: margin-bottom var(--dur-quick) ease;
  }
  .composer.mobile .input-box {
    flex-wrap: nowrap;
    column-gap: 0;
    /* Tighter than desktop's 3px 8px: every px here comes off the textarea. */
    padding: 2px 4px;
  }
  .composer.mobile .draft {
    flex: 1 1 auto;
    min-width: 0;
    font-size: var(--fs-body);
    padding: 10px 6px;
  }
  /* On touch the fmt bar can't hover-reveal — when toggled on from the sheet,
     show it at full strength and give the buttons real finger targets. */
  .composer.mobile .fmt-bar {
    opacity: 1;
    flex-wrap: wrap;
    padding-bottom: 6px;
    gap: var(--sp-1);
  }
  /* Two rows of four rather than eight abreast: eight shared 322px gave each
     button 33px, side by side with its neighbours, in a row where hitting the
     wrong one silently inserts the wrong markers into the message. */
  .composer.mobile .fmtbtn {
    flex: 1 1 21%;
    width: auto;
    height: var(--tap-min);
  }
  /* The separators are a desktop grouping cue; in a wrapped grid they'd land
     mid-row and eat 22px that the buttons need. */
  .composer.mobile .fmt-sep {
    display: none;
  }
  /* Finger-sized targets; glyphs stay grid-centered so only the tap area grows. */
  .composer.mobile .iconbtn {
    min-width: var(--tap-min);
    min-height: var(--tap-min);
  }
  .composer.mobile .sendbtn {
    width: var(--tap-min);
    height: var(--tap-min);
    margin: 2px 0;
  }
  /* The one control that opens something rather than doing something — tinted
     so the row reads as [more] [text] [emoji] [send] rather than four glyphs. */
  .composer.mobile .morebtn {
    color: var(--accent-hover);
  }
  /* The recording transport is a different child set, and the row rules above
     were written for the composing one: with align-items:flex-end the 44px send
     button and the 10px dot sat on different baselines. */
  .composer.mobile .input-box.recording {
    align-items: center;
    padding: 6px 8px;
  }
  /* The phone banner's cancel pill: keeps its 11px glyph, gains a real target. */
  @media (pointer: coarse) {
    .mini {
      min-width: var(--tap-min);
      min-height: var(--tap-min);
    }
  }
  /* Text where its neighbours are icons: sized down so "GIF" occupies the same
     optical weight as a 20px glyph instead of shouting. Not tokenised — this is
     an optical match to a specific glyph size, not a step on the type scale. */
  .gifbtn {
    font-size: 11px;
    font-weight: 800;
    font-family: inherit;
    letter-spacing: 0.02em;
    line-height: 1;
  }
  /* A button whose label is a WORD is a word Android will happily offer to
     select: a long press (or a tap misread as one) raises the blue handles and
     the Copy/Share bar over the composer. */
  .gifbtn,
  .sr-gif,
  .sr-aa,
  .s-enter {
    -webkit-user-select: none;
    user-select: none;
    -webkit-touch-callout: none;
  }
  .slow-chip {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    align-self: center;
    padding: 3px 8px;
    border-radius: 999px;
    background: var(--bg-3);
    color: var(--text-muted);
    font-size: var(--fs-tiny);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .sendbtn {
    display: grid;
    place-items: center;
    width: 38px;
    height: 38px;
    padding: 0; /* beat the global button padding — it un-centers the icon */
    flex-shrink: 0;
    align-self: flex-end;
    margin: 2px 0 2px 2px;
    border-radius: 50%;
    background: var(--accent);
    color: var(--accent-fg);
    transition: opacity var(--dur-quick) ease, transform var(--dur-quick) ease;
  }
  .sendbtn:active:not(:disabled) {
    transform: scale(0.9);
  }
  .sendbtn:disabled {
    opacity: 0.35;
  }
  /* On send the plane takes off up-right, then glides back in from the left —
     one quick whoosh, clipped to the round button. */
  .sendbtn {
    overflow: hidden;
  }
  .sendbtn.launch :global(svg) {
    animation: send-launch 0.45s cubic-bezier(0.4, 0, 0.6, 1);
  }
  @keyframes send-launch {
    45% {
      transform: translate(26px, -26px);
      opacity: 0;
    }
    46% {
      transform: translate(-20px, 20px);
      opacity: 0;
    }
    100% {
      transform: none;
      opacity: 1;
    }
  }

  /* ---- the "+" sheet ---- */
  .sheet-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 4px 0 2px;
  }
  .sheet-row {
    display: flex;
    align-items: center;
    gap: 14px;
    width: 100%;
    min-height: 56px; /* comfortably past the floor: these are the primary rows */
    padding: 8px 10px;
    text-align: left;
    background: transparent;
    color: var(--text);
    border: none;
    border-radius: var(--radius-md);
  }
  .sheet-row:active {
    background: var(--bg-3);
  }
  .sr-icon {
    display: grid;
    place-items: center;
    flex: none;
    width: 40px;
    height: 40px;
    border-radius: var(--radius-md);
    background: var(--bg-3);
    color: var(--accent-hover);
  }
  /* Word, not glyph: no icon for "GIF" reads correctly, and every other client
     spells it out. Sized to sit as one optical weight with the 20px glyphs. */
  .sr-gif {
    font-size: var(--fs-small);
    font-weight: 800;
    letter-spacing: 0.02em;
  }
  .sr-aa {
    font-size: var(--fs-title);
    font-weight: 700;
  }
  .sr-text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1;
  }
  .sr-label {
    font-size: var(--fs-body);
    font-weight: 600;
  }
  .sr-sub {
    font-size: var(--fs-compact);
    color: var(--text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .sr-state {
    flex: none;
    font-size: var(--fs-compact);
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--accent-hover);
  }
</style>
