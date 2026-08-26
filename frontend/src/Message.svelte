<script>
  // One message row. `compact` renders the grouped continuation form (no
  // avatar/header, hover timestamp). The action bar is keyboard-reachable
  // (focus-within) with labelled icon buttons.
  import Icon from "./Icon.svelte";
  import EmojiPicker from "./EmojiPicker.svelte";
  import Avatar from "./Avatar.svelte";
  import Attachment from "./Attachment.svelte";
  import FileAttachment from "./FileAttachment.svelte";
  import VoiceMessage from "./VoiceMessage.svelte";
  import VideoAttachment from "./VideoAttachment.svelte";
  import PollView from "./PollView.svelte";
  import AnnouncementView from "./AnnouncementView.svelte";
  import { parsePoll } from "./lib/polls.js";
  import { parseAnnounce } from "./lib/announce.js";
  import EmbedView from "./EmbedView.svelte";
  import DoodleView from "./DoodleView.svelte";
  import { parseDoodle, stripDoodle } from "./lib/doodle.js";
  import { parseEmbed, stripEmbedToken } from "./lib/richembed.js";
  import { ephemeralExpiry, stripEphemeral } from "./lib/ephemeral.svelte.js";
  import { fxEffect, stripFx, playFxOnce } from "./lib/fxtoken.js";
  import { radialBurst } from "./lib/burst.js";
  import { saved, isSaved, toggleSaved, refreshSaved } from "./lib/saved.svelte.js";
  import { sealedAt, stripTimestamp, sealShort, sealFull, sealAgo } from "./lib/timestamp.js";
  import YouTubeEmbed from "./YouTubeEmbed.svelte";
  import LinkPreview from "./LinkPreview.svelte";
  import { untrack } from "svelte";
  import { renderMarkdown, emojiOnly, animatedEmojiSrc, twemojiCode } from "./lib/markdown.js";
  import { animateInView, animateOnHover } from "./lib/anemoji.js";
  import { highlightCode } from "./lib/highlight.js";
  import {
    parseAttachTokens,
    parseFileTokens,
    stripAttachTokens,
    previewText,
    copyImageToClipboard,
    saveImageSrc,
  } from "./lib/attachments.js";
  import { knownRecipe } from "./lib/memerecipe.js";
  import { extractLinks, youtubeID } from "./lib/embeds.js";
  import {
    S,
    memberByFpr,
    nameColorFor,
    nameFor,
    customEmojiMap,
    react,
    deleteMsg,
    saveEdit,
    scrollToMessage,
    flash,
    openProfilePopover,
    scheduleCloseProfilePopover,
    openContextMenu,
    markUnread,
    activeGuild,
    activeChannel,
    fmtClock,
    jumpToChannel,
    refreshGuilds,
    mentionRefs,
    clockOpts,
    isMentionOfSelf,
    alertWordIn,
  } from "./lib/state.svelte.js";
  import { clampToBytes, TITLE_MAX_BYTES } from "./lib/postdraft.js";
  import { api } from "./lib/api.js";
  import { tooltip } from "./lib/tooltip.js";
  import { addReminder } from "./lib/scheduled.svelte.js";
  import { longpress, haptic } from "./lib/touch.js";
  import { PERM, has } from "./lib/perms.js";
  import {
    recentEmoji,
    pushRecentEmoji,
    replaceShortcodes,
    activeShortcode,
    searchEmoji,
  } from "./lib/emoji.js";

  // `entering` is set by MessageList for the newest appended message only, so
  // it fades/slides in once — history rows never animate.
  //
  // `tabbable` is the feed's single roving tab stop: exactly one row in the list
  // carries it, so Tab enters the thread once instead of walking through every
  // link, avatar and reaction pill of every row. ↑/↓ move it — the handler lives
  // in MessageList, which is the only thing that knows the row order.
  let { m, compact = false, replyRef = null, entering = false, tabbable = false } = $props();

  // Moderators (Manage Messages) can delete anyone's message.
  const canDeleteOthers = $derived(has(activeGuild()?.myPerms || 0, PERM.MANAGE_MESSAGES));

  // Touch device? Drives which gesture owns the context menu (see the .msg div).
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;
  // animateOnHover binds pointerover/pointerout, which under a finger means an
  // animated reaction plays for exactly the length of the tap — and that same
  // tap re-renders the pill, so it is never actually seen. On touch, drive the
  // pills off the viewport instead: they animate once when scrolled to.
  const reactionAnim = coarse ? animateInView : animateOnHover;
  const reduceMotion = window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches ?? false;

  // Long-press a reaction pill: who reacted — the touch counterpart of the
  // hover card. Rows are informational; tapping one just closes the sheet.
  function whoReacted(emoji, fprs) {
    return (e) =>
      openContextMenu(
        e,
        fprs.map((f) => ({ label: memberByFpr(f)?.name || f.slice(0, 9), onClick: () => {} })),
        { title: `${emoji} reactions` },
      );
  }

  // Svelte delegates touchstart at the root, so an ontouchstart attribute
  // would run AFTER the .msg longpress action's native listener — too late to
  // stop it. A native listener on the pill stops the bubble before .msg arms,
  // so a pill long-press opens only the who-reacted sheet, never both.
  function stopTouch(node) {
    const h = (ev) => ev.stopPropagation();
    node.addEventListener("touchstart", h, { passive: true });
    return { destroy: () => node.removeEventListener("touchstart", h) };
  }

  // Quick-reaction bar: the viewer's recently-used emoji padded with a
  // default set, capped at 5. Computed once per row (fresh rows pick up new
  // recents; recents only ever hold unicode chars).
  // Keep the hover bar minimal: three quick reactions (your
  // recents first) — the smile button opens the full picker for everything else.
  const DEFAULT_QUICK = ["👍", "❤️", "😂"];
  const quickEmojis = [...new Set([...recentEmoji(), ...DEFAULT_QUICK])].slice(0, 3);
  // The phone sheet has a full-width row to spend, so it offers twice the
  // choice: recents first, then enough well-worn defaults to fill six slots —
  // plus the "+" that ContextMenu renders for the whole picker.
  const SHEET_QUICK = ["👍", "❤️", "😂", "😮", "😢", "🔥"];
  const sheetEmojis = [...new Set([...recentEmoji(), ...SHEET_QUICK])].slice(0, 6);

  // The emoji the user just tapped bounces briefly (quick bar + pills share
  // this, keyed by emoji char).
  let bounced = $state(null);
  let bounceTimer;
  function reactWithBounce(emoji, e) {
    // Reacting is a broadcast: everyone in the channel sees it. Mid-scroll on a
    // phone the 450ms bounce is easy to miss, and a reader who misses it taps
    // again and silently un-reacts. A tick of vibration confirms it landed.
    haptic("light");
    clearTimeout(bounceTimer);
    bounced = null; // restart the CSS animation on rapid re-clicks
    requestAnimationFrame(() => {
      bounced = emoji;
      bounceTimer = setTimeout(() => (bounced = null), 500);
    });
    // ADDING a reaction earns a little burst of the emoji itself out of the
    // control you tapped; un-reacting is a retraction and stays quiet. Custom
    // emoji (":name:") have no glyph to throw — the bounce carries those.
    const adding = !(m.reactions?.[emoji] || []).includes(S.identity.fingerprint);
    const at = e?.currentTarget?.getBoundingClientRect?.();
    if (adding && at && !emoji.startsWith(":")) {
      radialBurst(at.left + at.width / 2, at.top + at.height / 2, {
        glyphs: [emoji],
        n: 6,
        size: [11, 16],
        seed: m.id + emoji,
      });
    }
    if (!emoji.startsWith(":")) pushRecentEmoji(emoji); // unicode only (custom emoji are guild-scoped)
    react(m, emoji);
  }

  const mem = $derived(memberByFpr(m.sender));
  const cemoji = $derived(customEmojiMap());

  const jumbo = $derived(!!bodyText && emojiOnly(bodyText));
  // "" when there's no animation for this emoji, which is the signal to fall
  // back to the plain character the pill has always shown. The manifest is
  // loaded once at boot (main.js), so this is a plain lookup — deliberately NOT
  // reactive: anything that flips after mount re-renders the body and re-fetches
  // every image in it.
  const animFor = (e) => animatedEmojiSrc(e);
  // Highlight every member's @name; the viewer's own name gets the self style.
  const mentionNames = $derived([
    // @everyone / @here highlight for everyone (self:true so they stand out).
    { name: "everyone", self: true },
    { name: "here", self: true },
    ...S.members.filter((mm) => mm.name).map((mm) => ({ name: mm.name, self: mm.isSelf })),
  ]);
  // @role and #channel, resolved against the guild on screen.
  const refs = $derived(mentionRefs());
  // A message addressed to you gets the whole row, not just the pill inside it.
  // Scrolling past your own name in a busy channel is the failure this fixes,
  // and a highlighted pill three words into a paragraph is easy to miss when
  // the eye is moving. Deliberately the same predicate the notification uses,
  // so what pinged you and what is tinted can never disagree.
  const mentionsMe = $derived(!m.deleted && isMentionOfSelf(m));
  // An alert word gets the same treatment through a different door, and wears a
  // different colour for it. A mention is somebody aiming a message at you; an
  // alert word is you having asked to be told. Both are worth a tinted row and
  // they are not the same event, so the row must not claim they are.
  const alertHit = $derived(m.deleted ? "" : alertWordIn(m));

  // @mentions open a floating profile card — on hover (with intent delay) and
  // immediately on click.
  function mentionMember(target) {
    const el = target.closest?.(".mention");
    if (!el) return null;
    const m = S.members.find((mm) => mm.name === el.dataset.mention);
    return m ? { el, fpr: m.fingerprint } : null;
  }
  // Reveal a focused spoiler with Enter/Space (it's role=button tabindex=0).
  function onBodyKeydown(e) {
    if (e.key !== "Enter" && e.key !== " ") return;
    const spoiler = e.target.closest?.(".spoiler");
    if (spoiler && !spoiler.classList.contains("revealed")) {
      e.preventDefault();
      spoiler.classList.add("revealed");
    }
  }

  function onBodyClick(e) {
    // Reveal a spoiler on click (first click only).
    const spoiler = e.target.closest?.(".spoiler");
    if (spoiler && !spoiler.classList.contains("revealed")) {
      e.preventDefault();
      spoiler.classList.add("revealed");
      return;
    }
    // A #channel goes where it points. It carries the id, not the name, so a
    // renamed channel still resolves; one that's since been deleted just does
    // nothing rather than throwing you somewhere arbitrary.
    const ch = e.target.closest?.(".mention-channel")?.dataset.channel;
    if (ch) {
      e.preventDefault();
      jumpToChannel(ch);
      return;
    }
    const hit = mentionMember(e.target);
    if (hit) {
      e.preventDefault();
      openProfilePopover(hit.fpr, hit.el);
    }
  }
  function onBodyOver(e) {
    const hit = mentionMember(e.target);
    if (hit) openProfilePopover(hit.fpr, hit.el, { delay: 320 });
  }
  function onBodyOut(e) {
    if (mentionMember(e.target)) scheduleCloseProfilePopover();
  }
  const poll = $derived(m.deleted ? null : parsePoll(m.content));
  const announce = $derived(m.deleted ? null : parseAnnounce(m.content));
  // The announcement speaks for the guild it was published in, which is the one
  // this channel belongs to — not a remote guild, so the local guild is it.
  const announceGuild = $derived(announce ? activeGuild() : null);
  const richEmbed = $derived(m.deleted ? null : parseEmbed(m.content));
  const atts = $derived(m.deleted ? [] : parseAttachTokens(m.content));
  const files = $derived(m.deleted ? [] : parseFileTokens(m.content));
  // A doodle: strokes, not an image (lib/doodle.js). null means either "there
  // is no doodle here" or "there is one and this client refuses to draw it" —
  // over a bound, from a version we do not know, or simply corrupt. The two
  // cases render the same, which is the whole point of failing closed.
  const doodle = $derived(m.deleted ? null : parseDoodle(m.content));
  const bodyText = $derived.by(() => {
    let c = atts.length || files.length ? stripAttachTokens(m.content) : m.content;
    if (richEmbed) c = stripEmbedToken(c);
    // Stripped unconditionally, NOT gated on `doodle` being non-null. A doodle
    // this client refused must leave nothing behind: gating the strip on a
    // successful parse is how a rejected token ends up printing several
    // hundred characters of base64 into the message body.
    return stripDoodle(stripFx(stripTimestamp(stripEphemeral(c))));
  });

  // Send effects ([fx](concord://fx/v1/…)): the burst plays once per session
  // when the row first scrolls into view, seeded by the message id so every
  // peer watches the identical field. Deleted rows keep their words private
  // AND their fireworks.
  const fxName = $derived(m.deleted ? "" : fxEffect(m.content));
  function fxOnView(node) {
    if (!fxName) return;
    // Not an IntersectionObserver: a .msg row's own box measures as a
    // degenerate 32px sliver hanging left of the viewport (its children
    // overflow-paint the real content), so intersection never reports true.
    // Only the row's VERTICAL geometry is trustworthy — check that by hand on
    // mount and then on every scroll until the row crosses the screen.
    const visible = () => {
      const r = node.getBoundingClientRect();
      return r.height > 0 && r.bottom > 0 && r.top < window.innerHeight;
    };
    let onScroll = null;
    const raf = requestAnimationFrame(() => {
      if (visible()) return playFxOnce(m.id, fxName, m.sent);
      onScroll = () => {
        if (!visible()) return;
        window.removeEventListener("scroll", onScroll, true);
        onScroll = null;
        playFxOnce(m.id, fxName, m.sent);
      };
      window.addEventListener("scroll", onScroll, true);
    });
    return {
      destroy() {
        cancelAnimationFrame(raf);
        if (onScroll) window.removeEventListener("scroll", onScroll, true);
      },
    };
  }
  // Swipe-to-reply: a short leftward drag on the row slides it out (capped) and
  // arms a reply that commits on release — the messenger-standard shortcut past
  // long-press → sheet → Reply. Touch only. MobileShell's drawer gesture claims
  // mostly-horizontal touches over the whole chat pane; rows advertise
  // data-swipe-reply so its claim step stands down for LEFTWARD drags that
  // start here (see its onTouchMove). If our own threshold is never met the row
  // just snaps back — the shell never gets the drag handed back mid-stream;
  // losing that one drag is far cheaper than a handoff protocol.
  const SWIPE_CAP = 64; // px the row travels, tops
  const SWIPE_COMMIT = 40; // px past which release replies
  function swipeReply(node) {
    if (!coarse) return;
    node.dataset.swipeReply = ""; // the flag MobileShell's claim defers to
    let sx = 0;
    let sy = 0;
    let tracking = false;
    let claimed = false;
    let buzzed = false;
    let dx = 0;

    // Transform-only (no layout work mid-drag), and skipped entirely under
    // reduced motion — the gesture still commits, there is just no slide (and
    // the hint stays clipped off-screen, so the haptic is the feedback there).
    function visual(x) {
      if (reduceMotion) return;
      node.style.transform = x ? `translateX(${x}px)` : "";
      node.style.setProperty("--swipe-p", String(Math.min(1, -x / SWIPE_COMMIT)));
    }
    function settle() {
      if (!reduceMotion && claimed) {
        // Snap back through a brief transition, then drop the class so the
        // NEXT drag tracks the finger raw instead of easing behind it.
        node.classList.add("swipe-settle");
        setTimeout(() => node.classList.remove("swipe-settle"), 200);
      }
      node.classList.remove("swipe-commit");
      visual(0);
      tracking = claimed = buzzed = false;
      dx = 0;
    }
    function onStart(e) {
      if (e.touches.length !== 1) return;
      // Same stand-downs as the shell's drawer: text fields keep their touches
      // (an edit-in-place textarea lives inside the row), and a live selection
      // means the finger is about to adjust handles, not swipe.
      if (e.target.closest?.("textarea, input")) return;
      const sel = window.getSelection?.();
      if (sel && !sel.isCollapsed) return;
      sx = e.touches[0].clientX;
      sy = e.touches[0].clientY;
      dx = 0;
      tracking = true;
      claimed = buzzed = false;
    }
    function onMove(e) {
      if (!tracking) return;
      const t = e.touches[0];
      if (!t || e.touches.length !== 1) {
        tracking = false;
        return;
      }
      dx = t.clientX - sx;
      const dy = t.clientY - sy;
      if (!claimed) {
        // Same claim geometry as the shell so the two gestures carve up the
        // plane identically: mostly-vertical is a scroll, rightward belongs to
        // the drawer, only a clear leftward slant is ours. (By ±10px of any
        // movement the longpress has already cancelled itself — lib/touch.js
        // onMove — so a drag never races the 400ms menu.)
        if (Math.abs(dy) > 14 && Math.abs(dy) > Math.abs(dx)) {
          tracking = false;
          return;
        }
        if (dx > 0) {
          tracking = false;
          return;
        }
        if (Math.abs(dx) < 12 || Math.abs(dx) < Math.abs(dy) * 1.4) return;
        claimed = true;
      }
      visual(Math.max(dx, -SWIPE_CAP));
      const past = dx <= -SWIPE_COMMIT;
      node.classList.toggle("swipe-commit", past);
      if (past && !buzzed) {
        buzzed = true; // exactly once per drag, even if the finger wanders back
        haptic("light");
      }
    }
    function onEnd(e) {
      if (claimed) {
        // A claimed drag ending over a link would still synthesize a click on
        // it — same ghost-click the shell and longpress both eat.
        if (e.cancelable) e.preventDefault();
        if (dx <= -SWIPE_COMMIT) S.replyingTo = m;
      }
      settle();
    }
    function onCancel() {
      settle(); // browser took the touch (scroll/system gesture): no reply
    }
    node.addEventListener("touchstart", onStart, { passive: true });
    node.addEventListener("touchmove", onMove, { passive: true });
    node.addEventListener("touchend", onEnd);
    node.addEventListener("touchcancel", onCancel);
    return {
      destroy() {
        node.removeEventListener("touchstart", onStart);
        node.removeEventListener("touchmove", onMove);
        node.removeEventListener("touchend", onEnd);
        node.removeEventListener("touchcancel", onCancel);
      },
    };
  }
  // clampSealCard keeps the reveal card on screen. The card is anchored to the
  // chip, and a chip can sit anywhere — including the left edge of a narrow
  // phone column, where a right-anchored card walked straight off the screen
  // ("the timestamp box shows out of screen partially LOL" — accurate). CSS
  // alone cannot know where the chip is, so measure once on mount and shift by
  // exactly the overflow; if the card would poke above the top bar, flip it
  // below the chip instead.
  function clampSealCard(node) {
    const pad = 8;
    const r = node.getBoundingClientRect();
    let dx = 0;
    if (r.left < pad) dx = pad - r.left;
    else if (r.right > window.innerWidth - pad) dx = window.innerWidth - pad - r.right;
    if (dx) node.style.transform = `translateX(${dx}px)`;
    // The top bar is ~52px plus the status bar; anything above ~90px is at risk
    // of sliding under chrome. Flip below the chip — there is always room there,
    // the feed scrolls.
    if (r.top < 90) {
      node.style.bottom = "auto";
      node.style.top = "calc(100% + 6px)";
    }
  }

  // A sealed timestamp: the author explicitly marked when this was sent, so it
  // is shown rather than hidden behind a hover the way the ordinary gutter time
  // is. 0 when unsealed.
  const sealMs = $derived(m.deleted ? 0 : sealedAt(m.content));
  let sealOpen = $state(false);
  // "3m ago" has to keep moving or it lies. One ticker per open card only.
  let sealNow = $state(Date.now());
  $effect(() => {
    if (!sealOpen) return;
    const id = setInterval(() => (sealNow = Date.now()), 1000);
    return () => clearInterval(id);
  });

  // Disappearing: expiry epoch (ms) if this message carries one, else 0.
  const ephExp = $derived(m.deleted ? 0 : ephemeralExpiry(m.content));
  // One embed per message: the first YouTube link gets a player; otherwise
  // the first link gets a preview card.
  const embed = $derived.by(() => {
    if (m.deleted || m.kind !== "") return null;
    // Prefer a YouTube player, but keep scanning past plain links to find one —
    // returning the first link as a card immediately meant a YouTube link after
    // any other link never got a player. Fall back to the first link's card.
    let firstCard = null;
    for (const url of extractLinks(m.content)) {
      const yt = youtubeID(url);
      if (yt) return { kind: "yt", id: yt, url };
      if (!firstCard) firstCard = { kind: "card", url };
    }
    return firstCard;
  });
  let editDraft = $state("");
  let editCancelled = false;
  let wasEditing = false;
  let editEl = $state(null);
  let editPicker = $state(false);
  let editPickerBelow = $state(false);
  function toggleEditPicker() {
    if (!editPicker) {
      // Open toward the roomier side: a message near the top of the feed gets
      // the picker BELOW the edit box (above would clip off-screen).
      editPickerBelow = (editEl?.getBoundingClientRect().top ?? 999) < 460;
    }
    editPicker = !editPicker;
  }

  // Seed the edit draft ONCE, when this message becomes the edit target (via the
  // menu or ArrowUp in an empty composer). untrack keeps a later reaction/edit
  // event that swaps `m` from wiping what the user is typing.
  $effect(() => {
    const editing = S.editing?.id === m.id;
    if (editing && !wasEditing) {
      editDraft = untrack(() => m.content);
      editCancelled = false;
    }
    wasEditing = editing;
  });

  function startEdit() {
    // A message with a rich embed can't be edited inline (the raw
    // [embed](concord://…) token would show and could be mangled) — reopen it in
    // the advanced composer, which decodes the embed back into its builder.
    if (richEmbed) {
      S.modal = { kind: "compose", initial: m.content, editId: m.id };
      return;
    }
    S.editing = m;
  }
  function cancelEdit() {
    editCancelled = true; // so the textarea's blur handler doesn't save it
    S.editing = null;
  }
  function commitEdit() {
    // Same shortcode treatment as the composer: :fire: saves as 🔥.
    if (!editCancelled) saveEdit(m, replaceShortcodes(editDraft));
  }

  // The ":colon command" while editing — same autocomplete as the composer:
  // typing :fir pops suggestions; ↑/↓ pick, Enter/Tab insert, Esc dismisses
  // (without cancelling the edit). A fully-typed :name: also converts inline.
  let editSuggest = $state(null); // { items: [[name, emoji]…], sel, start }

  function updateEditSuggest() {
    const el = editEl;
    if (!el) {
      editSuggest = null;
      return;
    }
    const sc = activeShortcode(editDraft, el.selectionStart);
    if (!sc) {
      editSuggest = null;
      return;
    }
    const items = searchEmoji(sc.query, 8);
    editSuggest = items.length ? { items, sel: 0, start: sc.start } : null;
  }

  function acceptEditSuggest(i) {
    const el = editEl;
    const [, emoji] = editSuggest.items[i];
    const end = el.selectionStart;
    editDraft = editDraft.slice(0, editSuggest.start) + emoji + " " + editDraft.slice(end);
    const caret = editSuggest.start + emoji.length + 1;
    editSuggest = null;
    requestAnimationFrame(() => {
      el.focus();
      el.setSelectionRange(caret, caret);
    });
  }

  function onEditInput(e) {
    const el = e.target;
    // Typing the closing colon of a known :name: converts it immediately.
    if (editDraft[el.selectionStart - 1] === ":") {
      const converted = replaceShortcodes(editDraft);
      if (converted !== editDraft) {
        const shift = editDraft.length - converted.length;
        const caret = el.selectionStart - shift;
        editDraft = converted;
        editSuggest = null;
        requestAnimationFrame(() => el.setSelectionRange(caret, caret));
        return;
      }
    }
    updateEditSuggest();
  }

  // Suggest-aware keys, layered over the save/cancel keys the textarea has.
  function onEditKeydown(e) {
    if (editSuggest) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const n = editSuggest.items.length;
        const d = e.key === "ArrowDown" ? 1 : -1;
        editSuggest = { ...editSuggest, sel: (editSuggest.sel + d + n) % n };
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        acceptEditSuggest(editSuggest.sel);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        editSuggest = null; // dismiss the popup, keep editing
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      commitEdit();
    } else if (e.key === "Escape") {
      e.preventDefault();
      cancelEdit();
    }
  }

  const fmtTime = fmtClock; // honors the 12/24h clock pref

  function jumpToReply() {
    if (m.replyTo && !scrollToMessage(m.replyTo)) flash("Original message not loaded");
  }

  // Reply preview: the original message with attachment tokens turned into a
  // readable placeholder, whitespace collapsed, and capped for one line.
  const replySnippet = $derived(
    replyRef && !replyRef.deleted
      ? previewText(replyRef.content).replace(/\s+/g, " ").trim().slice(0, 80) || "(empty message)"
      : "",
  );

  function copy(text, ok) {
    navigator.clipboard?.writeText(text);
    flash(ok, "success");
  }

  const isOwn = $derived(m.sender === S.identity.fingerprint && m.kind !== "guest");

  // Moderator reveal of a deleted GUILD message. The content only exists to
  // reveal in guilds — DM deletes erase it — so this is guild-only and gated on
  // Manage Messages (re-checked on the backend). `revealed` holds the fetched
  // original once shown.
  // An EXPIRED (disappeared) message was truly erased on every device — there's
  // nothing to reveal, so it's never revealable regardless of mod powers.
  const canRevealDeleted = $derived(
    m.deleted && !m.expired && activeGuild()?.kind !== "dm" && canDeleteOthers,
  );
  let revealed = $state(null);
  let revealing = $state(false);
  let revealDisplay = $state(""); // animated "de-crusting" text
  async function revealOriginal() {
    if (revealing || revealed !== null) return;
    revealing = true;
    try {
      revealed = (await api.revealDeleted(m.channelId, m.id)) || "(the original was empty)";
      decrustInto(revealed);
    } catch (err) {
      flash(err);
    } finally {
      revealing = false;
    }
  }
  // Hover-to-reveal: fetch the original and let the deleted tombstone crust
  // away into the real text. Click still works (touch / keyboard).
  function hoverReveal() {
    if (canRevealDeleted && revealed === null) revealOriginal();
  }

  // decrustInto animates target text out of glitchy "crust": each not-yet-settled
  // character flickers through random glyphs, resolving left-to-right — the
  // corruption breaking apart into the original message.
  const CRUST = "▓▒░#@%&$*/\\|=+<>";
  let decrustTimer = null;
  function decrustInto(target) {
    if (reduceMotion) {
      revealDisplay = target;
      return;
    }
    clearInterval(decrustTimer);
    let frame = 0;
    const settleFrames = 3; // frames each char stays scrambled before settling
    decrustTimer = setInterval(() => {
      frame++;
      const settled = Math.floor(frame / settleFrames);
      let out = "";
      for (let i = 0; i < target.length; i++) {
        if (i < settled || target[i] === " ") out += target[i];
        else out += CRUST[(Math.random() * CRUST.length) | 0];
      }
      revealDisplay = out;
      if (settled >= target.length) {
        revealDisplay = target;
        clearInterval(decrustTimer);
      }
    }, 28);
  }
  // A browser guest has no key: their message is relayed under the host's
  // signature. It is NOT the host talking, so it gets its own author row and
  // never inherits the host's name, color, avatar or fingerprint.
  // `kind:"guest"` is only honoured where guests can actually exist: a meeting
  // guild, relayed by that meeting's owner (the host). Anywhere else it's a
  // forgery — a member setting kind/Name themselves to post as an
  // unaccountable "guest" — so we fall back to normal member rendering, which
  // always shows the MLS-authenticated sender's fingerprint. This keeps the
  // guest feature while making the true author impossible to hide.
  const guest = $derived(
    m.kind === "guest" &&
      activeGuild()?.kind === "meeting" &&
      m.sender === activeGuild()?.ownerFingerprint,
  );
  const guestName = $derived(m.senderName || "Guest");

  // What the sheet calls this message. On a phone it is also the only way to
  // read the send time of a GROUPED message: the compact row's gutter clock is
  // hover-revealed, and hover does not exist here.
  const menuTitle = $derived(
    `${guest ? guestName : announce ? announceGuild?.name || "Guild" : nameFor(m.sender, m.senderName)} · ${new Date(m.sent).toLocaleString()}`,
  );

  // Start a thread from this message: a forum post IS a thread channel under
  // its parent, and the same machinery works with a text channel as the parent.
  // Offered only where it can succeed — a guild text channel. Not in DMs, not
  // in announcement/voice chat, and not inside a thread (no threads under
  // threads; the backend refuses them anyway).
  const canThread = $derived(
    activeGuild()?.kind !== "dm" && (activeChannel()?.type || "text") === "text",
  );
  let threadPrompt = $state(false);
  let threadTitle = $state("");
  let threadBusy = $state(false);

  // Same byte-honest clamp as ModalNewPost: the backend caps a title at 64
  // UTF-8 BYTES with a raw slice, and maxlength counts characters.
  function onThreadTitle(e) {
    const v = clampToBytes(e.currentTarget.value, TITLE_MAX_BYTES);
    if (v !== e.currentTarget.value) e.currentTarget.value = v;
    threadTitle = v;
  }

  async function createThreadFromMessage() {
    const title = threadTitle.trim();
    if (!title || threadBusy) return;
    threadBusy = true;
    try {
      // The opening message quotes the origin (capped — a wall of text makes a
      // bad excerpt) and carries a concord://msg link back to it, in exactly
      // the shape "Copy Message Link" produces.
      let src = stripAttachTokens(m.content).trim() || previewText(m.content);
      if (src.length > 280) src = src.slice(0, 280) + "…";
      const quoted = src
        .split("\n")
        .map((l) => `> ${l}`)
        .join("\n");
      const opener = `${quoted}\n\nconcord://msg/${m.channelId}/${m.id}`;
      const ch = await api.createThread(S.activeGuildId, m.channelId, title, opener, []);
      threadPrompt = false;
      threadTitle = "";
      // CreateThread returns the new channel; refresh first so jumpToChannel
      // can find it in the guild snapshot.
      await refreshGuilds();
      await jumpToChannel(ch.id);
      flash("Thread created", "success");
    } catch (err) {
      flash(err);
    } finally {
      threadBusy = false;
    }
  }

  function messageMenu(e) {
    // Bookmark state loads lazily: the first menu of a session may label a
    // saved row "Save Message" for a beat, which merely re-saves it (no-op).
    if (!saved.loaded) refreshSaved();
    if (m.deleted) {
      // A tombstone used to open nothing at all, which left a moderator's only
      // route to the original a 15px hover-labelled button.
      if (canRevealDeleted && revealed === null) {
        openContextMenu(e, [{ label: "Show original", icon: "lock", onClick: revealOriginal }], {
          title: menuTitle,
        });
      }
      return;
    }
    // A long-press on a LINK offers the link, above everything else: the row is
    // user-select:none so the WebView's own "copy link address" never appears,
    // and "Copy Text" copies the whole message — neither gets you one URL out of
    // a paragraph. Same branch improves the desktop right-click.
    const link = e.target?.closest?.("a[href]");
    // Code fences scroll horizontally on desktop; on a phone the chat pane is
    // touch-action:pan-y so they cannot be panned at all (they wrap instead, see
    // the styles) — either way "copy the block" is what a reader actually wants.
    const pre = e.target?.closest?.("pre");
    // Right-clicking an INLINE image (markdown data-URI) gets the image menu —
    // "Copy Text" on a picture just copies the word "image", which helps nobody.
    // (Encrypted attachments render via Attachment.svelte, which has its own.)
    const img = e.target?.closest?.("img.attachment");
    if (img) {
      openContextMenu(e, [
        {
          label: "Copy image",
          icon: "copy",
          onClick: async () => {
            try {
              await copyImageToClipboard(img.src);
              flash("Image copied", "success");
            } catch (err) {
              flash(`Couldn't copy image: ${err?.message || err}`);
            }
          },
        },
        {
          label: "Save image",
          icon: "download",
          onClick: async () => {
            // Only the gallery route is worth a toast: a browser download
            // announces itself, and a dismissed picker is not an event.
            const how = await saveImageSrc(img.src);
            if (how === "gallery") flash("Saved to your gallery", "success");
            else if (!how) flash("Couldn't save that image");
          },
        },
        { sep: true },
        // Straight from "that picture is funny" to the editor with it already
        // loaded — the shortest path there is, and the reason the editor takes
        // a src rather than only offering its own templates.
        {
          label: "Make a meme",
          icon: "spark",
          onClick: () => (S.modal = { kind: "meme", src: img.src }),
        },
      ]);
      return;
    }
    // Reopen a meme you made, in the editor that made it, and save back over
    // the same message. Only your own, only an image, and only while the
    // recipe that describes how it was built is still on THIS device — the
    // recipe is never sent, so on any other machine the entry is simply absent
    // and "Make a Meme" (a new meme from the flattened picture) is what's left.
    // Checked at click time: the recipe index is plain module state.
    const memeTok = isOwn ? atts.find((t) => knownRecipe(t.blobId)) : null;
    openContextMenu(e, [
      link && {
        label: "Open link",
        icon: "forward",
        onClick: () => window.open(link.href, "_blank", "noopener,noreferrer"),
      },
      link && { label: "Copy link", icon: "copy", onClick: () => copy(link.href, "Copied link") },
      pre && {
        label: "Copy code",
        icon: "copy",
        onClick: () => copy(pre.textContent || "", "Copied code"),
      },
      (link || pre) && { sep: true },
      { label: "Reply", icon: "reply", onClick: () => (S.replyingTo = m) },
      canThread && {
        label: "Start thread",
        icon: "forum",
        onClick: () => {
          threadTitle = "";
          threadPrompt = true;
        },
      },
      isOwn && { label: "Edit", icon: "edit", onClick: startEdit },
      memeTok && {
        label: "Edit meme",
        icon: "spark",
        onClick: () =>
          (S.modal = {
            kind: "meme",
            edit: { channelId: m.channelId, messageId: m.id, blobId: memeTok.blobId },
          }),
      },
      { label: "Add reaction", icon: "smile", onClick: () => (S.pickerTarget = m) },
      { sep: true },
      { label: "Copy text", icon: "copy", onClick: () => copy(stripAttachTokens(m.content).trim() || previewText(m.content), "Copied text") },
      {
        label: "Copy message link",
        icon: "copy",
        onClick: () => copy(`concord://msg/${m.channelId}/${m.id}`, "Copied message link"),
      },
      {
        label: m.pinned ? "Unpin" : "Pin",
        icon: "pin",
        onClick: () => {
          haptic("medium"); // pinning changes the channel for everyone; confirm it landed
          api.pinMessage(m.channelId, m.id);
        },
      },
      activeChannel()?.type === "announcement" && (isOwn || canDeleteOthers) && {
        label: "Publish",
        icon: "megaphone",
        onClick: () => (S.modal = { kind: "publish", message: m, channel: activeChannel() }),
      },
      { label: "Forward", icon: "forward", onClick: () => (S.modal = { kind: "forward", message: m }) },
      {
        label: isSaved(m.id) ? "Remove from saved" : "Save message",
        icon: "pin",
        onClick: () => toggleSaved(m),
      },
      { label: "Mark unread", icon: "bell", onClick: () => markUnread(m.channelId, m) },
      {
        label: "Remind me",
        icon: "clock",
        onClick: () =>
          (S.modal = {
            kind: "when",
            title: "Remind me about this",
            cta: "Remind me",
            onPick: (at) => {
              addReminder(m.channelId, m.id, stripAttachTokens(m.content).trim() || previewText(m.content), at);
              flash("Reminder set", "success");
            },
          }),
      },
      // Somebody else's message only: reporting your own is nothing but a
      // slower route to Delete, which is right there. This one entry serves
      // both the phone (long-press sheet) and the desktop (⋯), because they
      // render the same list.
      !isOwn && { sep: true },
      !isOwn && {
        label: "Report message",
        icon: "alert",
        onClick: () => (S.modal = { kind: "report", message: m }),
      },
      (isOwn || canDeleteOthers) && { sep: true },
      (isOwn || canDeleteOthers) && {
        label: "Delete",
        icon: "trash",
        danger: true,
        onClick: () => deleteMsg(m),
      },
    ], {
      // Mobile action sheet only: tap-to-react row on top, recents first, and a
      // title that doubles as the message's timestamp (desktop's anchored
      // popover ignores these extras).
      title: menuTitle,
      quick: { emojis: sheetEmojis, onPick: reactWithBounce, onMore: () => (S.pickerTarget = m) },
    });
  }

  // The hover toolbar carries the common verbs; Remind Me, Mark Unread, Copy
  // Text and friends lived only behind right-click, which plenty of users
  // never do. ⋯ opens the SAME menu (no duplicated item list), anchored under
  // the button via a synthetic point — same shape as EventCard's anchorEvt.
  // No target on the point, so messageMenu's link/pre/img sniffing all miss.
  function moreMenu(e) {
    const r = e.currentTarget.getBoundingClientRect();
    messageMenu({
      clientX: r.left,
      clientY: r.bottom + 4,
      preventDefault() {},
      stopPropagation() {},
    });
  }
</script>

<!-- Touch: only the longpress action opens the menu (Android's WebView also
     synthesizes contextmenu on long-press — letting both run opens the sheet
     twice: double haptic + re-keyed rows). Mouse right-click keeps contextmenu.

     role=article + a nonnegative tabindex on exactly one row is the ARIA feed
     pattern, not an accident: the rows ARE the navigable units, and the roving
     tab stop is what lets a keyboard reach a message's actions without walking
     every link and reaction pill above it. The rule this suppresses is the
     general "don't make static content focusable", which is the opposite of
     what a feed wants. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
<div
  class="msg"
  role="article"
  class:compact
  class:enter={entering}
  class:mentions-me={mentionsMe}
  class:alerts-me={!mentionsMe && !!alertHit}
  data-msg-id={m.id}
  tabindex={tabbable ? 0 : -1}
  oncontextmenu={coarse ? (e) => e.preventDefault() : messageMenu}
  use:longpress={{ handler: messageMenu }}
  use:fxOnView
  use:swipeReply
>
  {#if compact}
    <span
      class="gutter-time muted"
      use:tooltip={{ text: new Date(m.sent).toLocaleString() }}>{fmtTime(m.sent)}</span
    >
  {:else}
    {#if guest}
      <span class="av-btn guest-av" role="img" use:tooltip={"A guest in this meeting"} aria-label="A guest in this meeting">
        <Avatar name={guestName} emoji="👤" color="#5b6270" size={38} />
      </span>
    {:else if announce}
      <!-- A published announcement is the guild talking, so it wears the
           guild's face rather than the face of whoever pressed Publish. -->
      <span
        class="av-btn"
        role="img"
        use:tooltip={{ text: announceGuild?.name || "Announcement" }}
        aria-label={announceGuild?.name || "Announcement"}
      >
        <Avatar
          name={announceGuild?.name || "Guild"}
          image={announceGuild?.icon || ""}
          size={38}
        />
      </span>
    {:else}
      <button
        class="av-btn"
        use:tooltip={"View profile"}
        aria-label="View profile"
        onclick={(e) => openProfilePopover(m.sender, e.currentTarget)}
      >
        <!-- What someone is wearing belongs HERE above anywhere else: the
             message list is where you actually see people, and a decoration
             that only shows in the member panel and on a profile card is one
             almost nobody looks at. Same fields the member panel passes, so
             the two surfaces cannot drift. -->
        <Avatar
          name={nameFor(m.sender, m.senderName)}
          emoji={mem?.emoji}
          color={mem?.color}
          color2={mem?.color2}
          image={mem?.avatar}
          size={38}
          frame={mem?.frame}
          decoration={mem?.style?.dec || ""}
          dc={mem?.style?.dc || ""}
          style={mem?.style}
        />
      </button>
    {/if}
  {/if}

  <div class="msg-main">
    {#if m.replyTo && !compact}
      <button
        class="reply-ref"
        use:tooltip={"Jump to original message"}
        aria-label="Jump to original message"
        onclick={jumpToReply}
      >
        <span class="reply-icon"><Icon name="reply" size={11} /></span>
        {#if replyRef}
          <span
            class="reply-author"
            style={nameColorFor(replyRef.sender) ? `color:${nameColorFor(replyRef.sender)}` : ""}
            >{nameFor(replyRef.sender, replyRef.senderName)}</span
          >
          {#if replyRef.deleted}
            <em class="reply-snippet faded">message deleted</em>
          {:else}
            <span class="reply-snippet">{replySnippet}</span>
          {/if}
        {:else}
          <em class="reply-snippet faded">original message not loaded</em>
        {/if}
      </button>
    {/if}
    {#if !compact}
      <div class="msg-head">
        {#if guest}
          <span class="sender guest-name">{guestName}</span>
          <span
            class="guest-badge"
            use:tooltip={"Joined from a meeting link — no account, unverified"}
            >guest</span
          >
        {:else if announce}
          <span class="sender guild-name">{announceGuild?.name || "Guild"}</span>
          <span
            class="ann-badge"
            use:tooltip={{ text: announce.from ? `Published from #${announce.from}` : "Announcement" }}
          >
            <Icon name="megaphone" size={10} /> announcement
          </span>
        {:else}
          <button
            class="sender"
            style={nameColorFor(m.sender) ? `color:${nameColorFor(m.sender)}` : ""}
            onclick={(e) => openProfilePopover(m.sender, e.currentTarget)}
            >{nameFor(m.sender, m.senderName)}</button
          >
          <!-- The raw fingerprint used to sit on EVERY row (clutter, and
               meaningless on your own messages). Verification lives in the
               profile card now; here we show only a small check for a verified
               sender. -->
          {#if !isOwn && memberByFpr(m.sender)?.verified}
            <span class="verify-check" role="img" use:tooltip={"Identity verified"} aria-label="Identity verified"
              ><Icon name="check" size={11} /></span
            >
          {/if}
        {/if}
        <span
          class="muted time"
          use:tooltip={{ text: new Date(m.sent).toLocaleString() }}>{fmtTime(m.sent)}</span
        >
        {#if m.pinned}<span class="pin-mark" role="img" use:tooltip={"Pinned"} aria-label="Pinned"
            ><Icon name="pin" size={11} /></span
          >{/if}
      </div>
    {:else if m.pinned}
      <span class="pin-mark inline" role="img" use:tooltip={"Pinned"} aria-label="Pinned"
        ><Icon name="pin" size={11} /></span
      >
    {/if}

    {#if m.deleted}
      <!-- svelte-ignore a11y_no_static_element_interactions, a11y_mouse_events_have_key_events -->
      <div
        class="body deleted"
        class:revealable={canRevealDeleted && revealed === null}
        onmouseenter={hoverReveal}
      >
        {#if revealed !== null}
          <span
            class="revealed-tag"
            use:tooltip={"Deleted — shown to you as a moderator"}
          >
            <Icon name="lock" size={10} /> deleted · original
          </span>
          <span class="revealed-text">{revealDisplay || revealed}</span>
        {:else if m.expired}
          <em class="disappeared"><Icon name="clock" size={11} /> message disappeared</em>
        {:else}
          <em>deleted</em>
          {#if canRevealDeleted}
            <!-- The label has to match the device: "hover" is an instruction a
                 touchscreen cannot follow. -->
            <button class="reveal-btn" onclick={revealOriginal} disabled={revealing}>
              {revealing ? "…" : coarse ? "tap to reveal" : "hover or click to reveal"}
            </button>
          {/if}
        {/if}
      </div>
    {:else if S.editing?.id === m.id}
      <div class="edit-wrap" class:pick-below={editPickerBelow}>
        <!-- svelte-ignore a11y_autofocus -->
        <textarea
          class="edit-input"
          rows="1"
          bind:value={editDraft}
          bind:this={editEl}
          oninput={onEditInput}
          autofocus
          onkeydown={onEditKeydown}
          onblur={(e) => {
            // Focus moving WITHIN the edit UI must not commit-and-close. That
            // is the emoji button and its picker (.edit-wrap) — and Cancel and
            // Save, which live in a SIBLING .edit-actions row. A mouse never
            // noticed the gap because those buttons swallow mousedown, but Tab
            // does not, so tabbing to Cancel saved the edit on the way there
            // and then cancelled nothing.
            if (!e.relatedTarget?.closest?.(".edit-wrap, .edit-actions")) commitEdit();
          }}
        ></textarea>
        <button
          type="button"
          class="edit-emoji"
          use:tooltip
          aria-label="Insert emoji"
          onclick={toggleEditPicker}
        >
          <Icon name="smile" size={17} />
        </button>
        {#if editSuggest}
          <div class="edit-suggest" role="listbox" aria-label="Emoji suggestions">
            {#each editSuggest.items as item, i (item[0])}
              <button
                type="button"
                class="es-item"
                class:sel={i === editSuggest.sel}
                role="option"
                aria-selected={i === editSuggest.sel}
                onclick={() => acceptEditSuggest(i)}
              >
                <span class="es-emoji">{item[1]}</span> :{item[0]}:
                {#if i === editSuggest.sel}<kbd class="es-enter" aria-hidden="true">↵</kbd>{/if}
              </button>
            {/each}
          </div>
        {/if}
        {#if editPicker}
          <EmojiPicker
            onPick={(e) => {
              editDraft += e;
              editPicker = false;
              editEl?.focus();
            }}
            onClose={() => {
              editPicker = false;
              editEl?.focus();
            }}
          />
        {/if}
      </div>
      <!-- Buttons as well as the shortcuts. The keyboard path is faster once you
           know it, but a hint line only TELLS you it exists; something you can
           click is what makes it discoverable — and on a touchscreen there is no
           Escape key at all. onmousedown/preventDefault matters: the textarea's
           blur handler commits, so a plain click on Cancel would save the edit
           on the way down and then cancel nothing. -->
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <div
        class="edit-actions"
        onfocusout={(e) => {
          // The textarea hands the edit to these buttons rather than committing;
          // leaving THEM for somewhere else outside the edit is the real "you
          // walked away", and still commits, exactly as before.
          if (!e.relatedTarget?.closest?.(".edit-wrap, .edit-actions")) commitEdit();
        }}
      >
        <button
          type="button"
          class="edit-btn"
          onmousedown={(e) => e.preventDefault()}
          onclick={cancelEdit}
        >
          Cancel
        </button>
        <button
          type="button"
          class="edit-btn save"
          onmousedown={(e) => e.preventDefault()}
          onclick={() => {
            editCancelled = false;
            commitEdit();
            S.editing = null;
          }}
        >
          Save
        </button>
        <span class="edit-hint muted">escape to cancel · enter to save</span>
      </div>
    {:else if announce}
      <AnnouncementView {announce} />
    {:else if poll}
      <PollView {m} {poll} />
    {:else}
      {#if bodyText}
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <!-- dir comes from the AUTHOR when they overrode the per-line rule
             (domain.Message.Dir). Absent — which is every message written
             before the composer could set it, and most written since — the
             `.body` rule in app.css resolves each line on its own exactly as
             before. Where it IS present the inline style has to put
             unicode-bidi back to `isolate`, because `plaintext` ignores the
             element's own direction by definition and dir alone would be
             silently inert. Bounded to two values on the way in
             (domain.ValidDir), so this is never a string a stranger chose. -->
        <div class="body" class:jumbo={jumbo} dir={m.dir || null} style={m.dir ? "unicode-bidi:isolate" : null} use:animateInView={jumbo} use:highlightCode={bodyText} onclick={onBodyClick} onkeydown={onBodyKeydown} onmouseover={onBodyOver} onmouseout={onBodyOut} onfocusin={onBodyOver}>
          {#if sealMs}
            <!-- Tap AND hover, deliberately — but the hover pair must be gated
                 on a device that HAS hover: a tap synthesizes mouseenter first
                 and click second, so on touch the enter opened the card and the
                 click toggled it straight back shut. The card flashed for one
                 frame and the user reasonably reported it "broken". -->
            <button
              type="button"
              class="seal"
              class:open={sealOpen}
              use:tooltip={{ text: sealFull(sealMs, clockOpts()) }}
              aria-label="Sealed at {sealFull(sealMs, clockOpts())}"
              aria-expanded={sealOpen}
              onclick={(e) => { e.stopPropagation(); sealOpen = !sealOpen; if (sealOpen) { sealNow = Date.now(); haptic("light"); } }}
              onmouseenter={() => { if (matchMedia("(hover: hover)").matches) sealOpen = true; }}
              onmouseleave={() => { if (matchMedia("(hover: hover)").matches) sealOpen = false; }}
            >
              <Icon name="diamond" size={11} />
              <span class="seal-t">{sealShort(sealMs, clockOpts())}</span>
              {#if sealOpen}
                <span class="seal-card" role="tooltip" use:clampSealCard>
                  <span class="seal-card-h">Sealed by the sender</span>
                  <span class="seal-card-f">{sealFull(sealMs, clockOpts())}</span>
                  <span class="seal-card-a">{sealAgo(sealMs, sealNow)}</span>
                </span>
              {/if}
            </button>
          {/if}{@html renderMarkdown(bodyText, mentionNames, cemoji, refs)}{#if m.edited}<span
              class="edited-tag">(edited)</span
            >{/if}
        </div>
      {/if}
      {#if doodle}
        <DoodleView strokes={doodle} />
      {/if}
      <!-- One image sizes itself, as it always has. Two or more become a grid:
           stacked at their own sizes they were a ragged column — a portrait
           photo, then a 60px-tall panorama, then a screenshot — and reading
           four of those meant scrolling past three. -->
      {#if atts.length > 1}
        <div class="att-grid">
          {#each atts as tok (tok.blobId)}
            <Attachment channelId={m.channelId} {tok} messageId={m.id} own={isOwn} tile />
          {/each}
        </div>
      {:else}
        {#each atts as tok (tok.blobId)}
          <Attachment channelId={m.channelId} {tok} messageId={m.id} own={isOwn} />
        {/each}
      {/if}
      {#each files as tok (tok.blobId)}
        {#if tok.mime?.startsWith("audio/")}
          <VoiceMessage channelId={m.channelId} {tok} />
        {:else if tok.mime?.startsWith("video/")}
          <VideoAttachment channelId={m.channelId} {tok} />
        {:else}
          <FileAttachment channelId={m.channelId} {tok} />
        {/if}
      {/each}
      {#if m.edited && !bodyText && (atts.length || files.length)}
        <!-- The "(edited)" tag normally rides at the end of the body text, so a
             message whose whole content is an attachment token had nowhere to
             show it — and editing a meme in place changes the PICTURE, which is
             exactly the case where a reader deserves to be told. -->
        <span class="edited-tag att-edited">(edited)</span>
      {/if}
      {#if richEmbed}
        <EmbedView embed={richEmbed} {mentionNames} customEmoji={cemoji} {refs} />
      {/if}
      {#if embed?.kind === "yt"}
        <YouTubeEmbed videoId={embed.id} autoload={S.prefs.linkPreviews !== false} />
      {:else if embed?.kind === "card"}
        {#key embed.url}
          <LinkPreview url={embed.url} />
        {/key}
      {/if}
      {#if ephExp}
        <span
          class="eph-hint"
          use:tooltip={{ text: `Disappears ${new Date(ephExp).toLocaleString()}` }}
        >
          <Icon name="clock" size={10} /> disappearing
        </span>
      {/if}
    {/if}

    {#if !poll && m.reactions && Object.keys(m.reactions).length}
      <div class="reactions" use:reactionAnim>
        {#each Object.entries(m.reactions) as [emoji, fprs] (emoji)}
          {@const cimg = /^:([a-z0-9_]{2,32}):$/.test(emoji) ? cemoji[emoji.slice(1, -1)] : null}
          <span class="react-wrap">
            <button
              class="reaction"
              class:mine={fprs.includes(S.identity.fingerprint)}
              onclick={(ev) => reactWithBounce(emoji, ev)}
              use:stopTouch
              use:longpress={{ handler: whoReacted(emoji, fprs) }}
            >
              <span class="remoji" class:bounce={bounced === emoji}>
                {#if cimg}<img class="cemoji" src={cimg} alt={emoji} />
                {:else if animFor(emoji)}<img
                    class="remoji-img"
                    src="/twemoji/{twemojiCode(emoji)}.svg"
                    data-anim={animFor(emoji)}
                    alt={emoji}
                  />
                {:else}{emoji}{/if}
              </span>
              <!-- keyed so the count re-mounts (and animates) when it changes -->
              {#key fprs.length}<span class="rcount">{fprs.length}</span>{/key}
            </button>
            <!-- who reacted, on hover -->
            <span class="react-who">
              <strong>
                {#if cimg}<img class="cemoji" src={cimg} alt={emoji} />{:else}{emoji}{/if}
                · {fprs.length}
              </strong>
              {#each fprs.slice(0, 12) as f (f)}
                <span class="rw-row">{memberByFpr(f)?.name || f.slice(0, 9)}</span>
              {/each}
              {#if fprs.length > 12}<span class="rw-more">+{fprs.length - 12} more</span>{/if}
            </span>
          </span>
        {/each}
      </div>
    {/if}

    {#if threadPrompt}
      <!-- The whole ceremony a thread needs is one line: a title. A modal would
           also hide the message the thread is about — which is exactly what the
           title is written from. -->
      <div class="thread-prompt">
        <span class="tp-icon"><Icon name="forum" size={14} /></span>
        <!-- svelte-ignore a11y_autofocus -->
        <input
          class="tp-input"
          placeholder="Thread title"
          aria-label="Thread title"
          value={threadTitle}
          oninput={onThreadTitle}
          autofocus={!S.isMobile}
          onkeydown={(e) => {
            if (e.key === "Enter") createThreadFromMessage();
            else if (e.key === "Escape") threadPrompt = false;
          }}
        />
        <button type="button" class="tp-btn" onclick={() => (threadPrompt = false)}>Cancel</button>
        <button
          type="button"
          class="tp-btn go"
          onclick={createThreadFromMessage}
          disabled={threadBusy || !threadTitle.trim()}
        >
          {threadBusy ? "Creating…" : "Create"}
        </button>
      </div>
    {/if}
  </div>

  {#if coarse}
    <!-- Swipe-to-reply affordance. In-flow zero-width flex child, NOT absolute
         against .msg: the row's own box is a degenerate sliver (see fxOnView),
         so only child boxes anchor anything trustworthy. The dot paints just
         past the row's right edge — the feed's overflow-x:hidden clips it until
         the leftward slide carries it on screen. -->
    <span class="swipe-hint" aria-hidden="true">
      <span class="swipe-hint-dot"><Icon name="reply" size={14} /></span>
    </span>
  {/if}

  <!-- Hover toolbar. Built only where hovering is a thing.
       This is ~33 nodes per row and it used to be built on every row of every
       feed and then hidden with display:none on phones — on a 200-row channel
       that is the majority of the DOM, paid for in parse, layout and memory by
       the device least able to afford it. The mobile entry point is the
       long-press sheet (messageMenu), which carries every action this bar has
       and several it never had, so nothing is lost by not drawing it. -->
  {#if !S.isMobile && !m.deleted && S.editing?.id !== m.id}
    <div class="msg-actions" role="toolbar" aria-label="Message actions">
      <div class="grp">
        {#each quickEmojis as e (e)}
          <button class="emoji-btn" class:bounce={bounced === e} use:tooltip aria-label="React {e}" onclick={(ev) => reactWithBounce(e, ev)}>{e}</button>
        {/each}
        <button class="add-react" use:tooltip aria-label="More reactions" onclick={() => (S.pickerTarget = m)}>
          <Icon name="smile" size={15} />
          <span class="plus" aria-hidden="true">+</span>
        </button>
      </div>
      <span class="sep"></span>
      <div class="grp">
        <button use:tooltip aria-label="Reply" onclick={() => (S.replyingTo = m)}>
          <Icon name="reply" size={15} />
        </button>
        <button use:tooltip aria-label="Forward" onclick={() => (S.modal = { kind: "forward", message: m })}>
          <Icon name="forward" size={15} />
        </button>
        <button
          class:on={m.pinned}
          use:tooltip
          aria-label={m.pinned ? "Unpin" : "Pin"}
          onclick={() => api.pinMessage(m.channelId, m.id)}
        >
          <Icon name="pin" size={15} />
        </button>
      </div>
      {#if m.sender === S.identity.fingerprint}
        <span class="sep"></span>
        <div class="grp">
          <button use:tooltip aria-label="Edit" onclick={startEdit}><Icon name="edit" size={15} /></button>
          <button class="danger" use:tooltip aria-label="Delete" onclick={() => deleteMsg(m)}>
            <Icon name="trash" size={15} />
          </button>
        </div>
      {:else if canDeleteOthers}
        <span class="sep"></span>
        <div class="grp">
          <button
            class="danger"
            use:tooltip={"Delete (moderator)"}
            aria-label="Delete message"
            onclick={() => deleteMsg(m)}
          >
            <Icon name="trash" size={15} />
          </button>
        </div>
      {/if}
      <span class="sep"></span>
      <div class="grp">
        <button use:tooltip aria-label="More" onclick={moreMenu}>
          <Icon name="dots" size={15} />
        </button>
      </div>
    </div>
  {/if}
</div>

<style>
  /* Row rhythm comes from the density vars in app.css (Appearance setting):
     cozy is today's spacing, compact tightens padding + group pull together. */
  .msg {
    display: flex;
    gap: var(--sp-3);
    position: relative;
    padding: var(--msg-pad-y, 2px) 0;
    border-radius: var(--radius-sm);
  }
  /* Row highlight is a pointer affordance, and Chromium latches :hover onto the
     last element a finger touched — unguarded, the message you last tapped
     stayed lit forever and read as a selection the app doesn't have. */
  @media (pointer: fine) {
    .msg:hover {
      background: color-mix(in srgb, var(--bg-3) 40%, transparent);
    }
  }
  /* ---- @you ----
     Warm, not red: --danger is the rail's mention colour because a rail badge
     is a dot with no room to say anything, but a whole row painted in the alarm
     colour reads as "this message is broken". Amber carries the same "look
     here" without the alarm, and both --warn and the tint derived from it flip
     with the theme, so this is one declaration for all fifty packs. The bar sits
     in the gutter the same way the unread bar does in the channel list. */
  .msg.mentions-me {
    background: var(--warn-soft);
    /* The margin and the padding cancel, so the tint reaches into the feed's
       gutter without moving a single pixel of the row's content. */
    margin-left: -8px;
    padding-left: var(--sp-2);
    margin-right: -8px;
    padding-right: var(--sp-2);
  }
  @media (pointer: fine) {
    .msg.mentions-me:hover {
      background: color-mix(in srgb, var(--warn) 22%, transparent);
    }
  }
  .msg.mentions-me::before {
    content: "";
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--warn);
  }
  /* ---- a word you asked about ----
     The same shape as @you, in the accent instead of the amber, because it is
     the same "look here" arriving through a different door. Amber says someone
     aimed this at you; the accent says you asked to be told. Giving them one
     colour would be a small lie every time an alert word fired, and giving the
     alert word a THIRD colour would just be another thing to learn. */
  .msg.alerts-me {
    background: var(--accent-soft);
    margin-left: -8px;
    padding-left: var(--sp-2);
    margin-right: -8px;
    padding-right: var(--sp-2);
  }
  @media (pointer: fine) {
    .msg.alerts-me:hover {
      background: color-mix(in srgb, var(--accent) 20%, transparent);
    }
  }
  .msg.alerts-me::before {
    content: "";
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--accent);
  }
  /* The row is the feed's roving tab stop (see the `tabbable` prop). The ring
     is drawn INSIDE it: the row runs the full width of the feed, and the app's
     2px outward offset would land in the column's overflow-x:hidden and be
     sliced off on both sides. */
  .msg:focus-visible {
    outline: var(--focus-ring);
    outline-offset: -2px;
  }
  .msg.compact {
    margin-top: var(--msg-group-pull, -10px);
  }
  /* Newest appended message only (see MessageList): quick fade + slide-up.
     The global reduced-motion override in app.css zeroes the duration. */
  .msg.enter {
    animation: msg-in 0.26s var(--ease-out) backwards;
  }
  @keyframes msg-in {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
  }
  /* Also the spacer that keeps a grouped row's text aligned under the author's,
     which is why it survives on touch even with nothing rendered in it — the
     send time moves to the long-press sheet's title there. */
  .gutter-time {
    /* 38px is the avatar's width, not a guess — it is what keeps a grouped
       row's text under the text of the row that names its author, so it cannot
       simply grow. The 12-hour clock used to wrap its meridiem onto a second
       line inside it and make the row taller on hover; dropping the leading
       zero (see fmtClock) is what makes "7:28 PM" fit, and nowrap is what makes
       any remaining overhang hang into the feed's own gutter instead. */
    width: 38px;
    font-size: var(--fs-micro);
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.02em;
    text-align: right;
    white-space: nowrap;
    opacity: 0;
    flex-shrink: 0;
    padding-top: var(--sp-1);
  }
  @media (pointer: fine) {
    .msg.compact:hover .gutter-time {
      opacity: 1;
    }
  }
  .msg-main {
    min-width: 0;
    flex: 1;
  }
  .reply-ref {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: var(--fs-small);
    color: var(--text-muted);
    border-left: 2px solid var(--border);
    padding: 1px 8px 1px 8px;
    margin-bottom: 2px;
    background: transparent;
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
    max-width: 100%;
    min-width: 0;
    transition:
      background var(--dur-quick) ease,
      border-color var(--dur-quick) ease,
      color var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .reply-ref:hover {
      background: color-mix(in srgb, var(--bg-3) 65%, transparent);
      border-left-color: var(--accent);
      color: var(--text);
    }
  }
  /* Touch's only feedback that the jump registered. */
  .reply-ref:active {
    background: color-mix(in srgb, var(--accent) 16%, transparent);
    border-left-color: var(--accent);
    color: var(--text);
  }
  .reply-icon {
    display: inline-flex;
    flex-shrink: 0;
    opacity: 0.75;
  }
  .reply-author {
    font-weight: 600;
    color: var(--accent-hover);
    flex-shrink: 0;
  }
  .reply-snippet {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .reply-snippet.faded {
    color: var(--text-faint);
  }
  @media (pointer: fine) {
    .reply-ref:hover .reply-snippet:not(.faded) {
      color: var(--text);
    }
  }
  .msg-head {
    display: flex;
    gap: var(--sp-2);
    align-items: baseline;
    min-width: 0;
  }
  .av-btn {
    background: transparent;
    border: none;
    padding: 0;
    border-radius: 50%;
    cursor: pointer;
    flex-shrink: 0;
    align-self: flex-start;
  }
  .av-btn:hover {
    background: transparent;
  }
  .av-btn :global(.avatar) {
    transition:
      box-shadow var(--dur-quick) ease,
      transform var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .av-btn:hover :global(.avatar) {
      box-shadow: 0 0 0 2px var(--accent);
    }
  }
  .sender {
    background: transparent;
    border: none;
    padding: 0;
    font: inherit;
    font-weight: 600;
    color: var(--accent-hover);
    cursor: pointer;
    /* A 32-character display name used to run under the timestamp and push the
       pin mark off the row. Ellipsis, the way the member list has always done
       it — the full name is one hover (and one profile card) away. */
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .msg-head .time,
  .msg-head .guest-badge,
  .msg-head .ann-badge,
  .msg-head .verify-check,
  .msg-head .pin-mark {
    flex-shrink: 0;
  }
  /* The transparent background is not decoration — it cancels the global button
     hover fill, which Chromium would otherwise latch onto a tapped name. Only
     the underline is pointer-only. */
  .sender:hover {
    background: transparent;
  }
  @media (pointer: fine) {
    .sender:hover {
      text-decoration: underline;
    }
  }
  /* Marks the row as the guild speaking rather than a person — quiet, since
     the guild's name and icon already carry the point. */
  .guild-name {
    font-weight: 700;
    color: var(--text);
    cursor: default;
  }
  .ann-badge {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent-hover);
    font-size: var(--fs-micro);
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
  }
  .verify-check {
    display: inline-flex;
    align-items: center;
    color: var(--ok-text);
  }
  .time {
    font-size: var(--fs-tiny);
  }
  .pin-mark {
    color: var(--warn);
    animation: pin-in 0.25s var(--ease-spring);
  }
  @keyframes pin-in {
    from {
      transform: scale(0.4) rotate(-30deg);
      opacity: 0;
    }
  }
  /* A guest is visibly not a member: muted name, an explicit badge, no
     fingerprint (they have no key to show). */
  .guest-name {
    color: var(--text);
    cursor: default;
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    font-weight: 600;
  }
  .guest-badge {
    padding: 0 6px;
    font-size: var(--fs-micro);
    font-weight: 600;
    line-height: 16px;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .guest-av {
    display: inline-flex;
    cursor: default;
  }
  .pin-mark.inline {
    float: right;
  }
  .body {
    margin-top: 2px;
    white-space: pre-wrap;
    word-break: break-word;
    /* Comfortable reading measure for multi-line messages — roomier than the
       default without stretching single-line rows noticeably.
       Arabic and Persian sit taller than Latin at the same size: the diacritics
       climb above the letterform and the descenders drop well below it, so 1.45
       clipped them against the row above. */
    line-height: 1.5;
    /* Direction is resolved per line — see the bidi section of app.css, which
       covers every rendered-prose container rather than just this one. */
  }
  .reveal-btn {
    margin-left: var(--sp-2);
    padding: 1px 8px;
    font-size: var(--fs-small);
    border: 1px solid var(--border);
    border-radius: 999px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    vertical-align: middle;
  }
  .reveal-btn:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .revealed-tag {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-right: 7px;
    padding: 0 6px;
    font-size: var(--fs-micro);
    font-style: normal;
    border-radius: var(--radius-sm);
    background: var(--accent-soft);
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .revealed-text {
    color: var(--text);
    font-style: normal;
    white-space: pre-wrap;
  }
  .body.deleted {
    color: var(--text-muted);
  }
  /* A deleted message a moderator can un-crust: hint it's interactive. */
  .body.deleted.revealable {
    cursor: pointer;
  }
  .body.deleted.revealable em {
    border-bottom: 1px dashed color-mix(in srgb, var(--accent) 45%, transparent);
    transition: color var(--dur-standard) ease;
  }
  .body.deleted.revealable:hover em {
    color: var(--accent-hover);
  }
  /* Expired = gone by a timer, on purpose. A faint accent tint sets it apart
     from a plain "deleted" tombstone. */
  .disappeared {
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    font-style: italic;
    color: color-mix(in srgb, var(--accent) 55%, var(--text-faint));
  }
  .disappeared :global(svg) {
    opacity: 0.8;
  }
  /* Standing on its own under a picture rather than trailing a line of text,
     so it needs to be a block or it sits beside the image's baseline. */
  .att-edited {
    display: block;
    margin-left: 0;
    margin-top: 2px;
  }
  .edited-tag {
    margin-left: 5px;
    font-size: var(--fs-micro);
    color: var(--text-faint);
    user-select: none;
    vertical-align: baseline;
    animation: tag-in 0.3s ease;
  }
  .eph-hint {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-top: 2px;
    font-size: var(--fs-micro);
    color: var(--text-faint);
    user-select: none;
  }
  .eph-hint :global(svg) {
    opacity: 0.8;
  }
  @keyframes tag-in {
    from {
      opacity: 0;
    }
  }
  .edit-wrap {
    position: relative; /* anchors the emoji button + picker */
  }
  /* The shared picker defaults to composer placement (bottom:54px); in the
     edit context anchor it just above the box — or just below when the
     message sits too close to the top of the window to fit it above.
     Mouse-only: the picker is position:absolute inside the feed, which is a
     clipping scroll container, and the 460px flip threshold below is a desktop
     window height. On a phone EmojiPicker has its own fixed bottom-panel
     presentation — these rules matched it at equal specificity and whichever
     came last in the bundle won, which is not a decision CSS order should be
     making. Scoped out, the panel takes over. */
  @media (pointer: fine) {
    .edit-wrap :global(.picker) {
      bottom: calc(100% + 6px);
      top: auto;
      right: 0;
    }
    .edit-wrap.pick-below :global(.picker) {
      top: calc(100% + 6px);
      bottom: auto;
    }
  }
  .edit-input {
    margin-top: 2px;
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    min-height: 38px;
    font-family: inherit;
    line-height: 1.4;
    padding-right: 34px; /* keep text clear of the emoji button */
  }
  .edit-emoji {
    position: absolute;
    top: 8px;
    right: 6px;
    display: grid;
    place-items: center;
    width: 26px;
    height: 26px;
    padding: 0;
    line-height: 0;
    border: none;
    border-radius: 50%;
    background: transparent;
    color: var(--text-faint);
    cursor: pointer;
    transition:
      color var(--dur-quick) ease,
      background var(--dur-quick) ease;
  }
  .edit-emoji:hover {
    color: var(--text);
    background: var(--bg-3);
  }
  .edit-actions {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 5px;
  }
  .edit-btn {
    padding: 3px 10px;
    font-size: var(--fs-small);
    font-weight: 600;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
    border: 1px solid transparent;
    transition:
      background var(--dur-quick),
      color var(--dur-quick);
  }
  .edit-btn:hover {
    background: color-mix(in srgb, var(--text) 14%, var(--bg-3));
    color: var(--text);
  }
  .edit-btn.save {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .edit-btn.save:hover {
    background: var(--accent-hover);
  }
  .edit-btn:focus-visible {
    border-color: var(--accent);
  }
  .edit-hint {
    font-size: var(--fs-small);
    margin-top: 0;
  }
  /* The shortcut hint is the redundant half once there are buttons — keep it
     where there is room, drop it on a phone where the row would wrap. (There is
     no Escape key there either, so the hint would be a lie as well as a wrap.)
     With it gone the two buttons are the ONLY way out of an edit, which is why
     they get the full tap minimum and a real gap: Cancel discards what you
     typed and it used to sit 6px from Save. */
  @media (pointer: coarse), (max-width: 768px) {
    .edit-hint {
      display: none;
    }
    .edit-actions {
      gap: var(--sp-3);
    }
    .edit-btn {
      min-height: var(--tap-min);
      padding: 10px 20px;
      font-size: var(--fs-ui);
    }
    .edit-emoji {
      top: 5px;
      width: 40px;
      height: 40px;
    }
    .edit-input {
      padding-right: 48px;
    }
  }
  /* Start-Thread's one-line prompt: same quiet chrome as the edit box, sized
     to sit under the message it threads off rather than over it. */
  .thread-prompt {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 6px;
    padding: 5px 8px;
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    max-width: 440px;
    transition:
      border-color var(--dur-standard) ease,
      box-shadow var(--dur-standard) ease;
  }
  .thread-prompt:focus-within {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .tp-icon {
    display: inline-flex;
    color: var(--text-faint);
    flex-shrink: 0;
  }
  .thread-prompt .tp-input {
    flex: 1;
    min-width: 0;
    padding: 3px 4px;
    font-family: inherit;
    font-size: var(--fs-ui);
    color: var(--text);
    background: transparent;
    border: none;
    box-shadow: none;
    outline: none;
  }
  .tp-btn {
    padding: 3px 10px;
    font-size: var(--fs-small);
    font-weight: 600;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
    flex-shrink: 0;
  }
  .tp-btn:hover {
    background: color-mix(in srgb, var(--text) 14%, var(--bg-3));
    color: var(--text);
  }
  .tp-btn.go {
    background: var(--accent);
    color: var(--accent-fg);
  }
  .tp-btn.go:hover:not(:disabled) {
    background: var(--accent-hover);
  }
  .tp-btn:disabled {
    opacity: 0.6;
  }
  @media (pointer: coarse), (max-width: 768px) {
    .thread-prompt {
      max-width: none;
      flex-wrap: wrap;
    }
    .tp-btn {
      min-height: var(--tap-min);
      padding: 10px 16px;
      font-size: var(--fs-ui);
    }
  }

  /* :shortcode autocomplete inside the edit box (composer parity). */
  .edit-suggest {
    position: absolute;
    left: 0;
    bottom: calc(100% + 4px);
    min-width: 220px;
    max-width: 320px;
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    padding: var(--sp-1);
    display: flex;
    flex-direction: column;
    z-index: 30;
  }
  .es-item {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    width: 100%;
    padding: 5px 8px;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: var(--fs-ui);
    text-align: left;
    cursor: pointer;
  }
  .es-item:hover,
  .es-item.sel {
    background: var(--accent-soft);
  }
  .es-emoji {
    font-size: 16px;
    width: 20px;
    text-align: center;
  }
  .es-enter {
    margin-left: auto;
    font-size: var(--fs-tiny);
    color: var(--text-faint);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 0 4px;
  }
  .reactions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
    margin-top: var(--sp-1);
  }
  .reaction {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 3px 9px;
    font-size: var(--fs-ui);
    border-radius: var(--radius-md);
    /* springy pop when a new pill appears (overshoot bezier) */
    animation: pill-in 0.25s var(--ease-spring);
    transition:
      transform 0.1s ease,
      border-color var(--dur-quick) ease,
      background var(--dur-quick) ease,
      box-shadow var(--dur-quick) ease;
  }
  @media (pointer: fine) {
    .reaction:hover {
      transform: translateY(-1px);
      border-color: var(--text-faint);
      box-shadow: 0 2px 6px rgb(0 0 0 / 0.18);
    }
  }
  .reaction:active {
    transform: scale(0.93);
  }
  .reaction.mine {
    border-color: var(--accent);
    background: var(--accent-soft);
    box-shadow:
      0 0 0 1px var(--accent),
      0 0 8px color-mix(in srgb, var(--accent) 26%, transparent); /* accent ring + faint charge: you reacted */
  }
  @media (pointer: fine) {
    .reaction.mine:hover {
      border-color: var(--accent);
      box-shadow:
        0 0 0 1px var(--accent),
        0 2px 6px rgb(0 0 0 / 0.18);
    }
  }
  .reaction.mine .rcount {
    color: var(--accent-hover);
    font-weight: 600;
  }
  /* The emoji is noticeably larger than the count — the glyph is the thing you
     read at a glance. Overrides the pill's 13px font. */
  .remoji {
    display: inline-flex;
    line-height: 1;
    font-size: 18px;
  }
  /* Sized to match the character it replaces, so a pill doesn't change shape
     depending on whether that emoji happens to have an animation. */
  .remoji-img {
    width: 18px;
    height: 18px;
    object-fit: contain;
    display: block;
  }
  .rcount {
    display: inline-block;
    min-width: 1ch;
    text-align: center;
    font-size: var(--fs-ui);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    animation: count-in 0.18s ease; /* replays on {#key} re-mount */
  }
  /* Click-bounce for the emoji you just tapped (pill glyph + quick bar). */
  .remoji.bounce,
  .msg-actions .emoji-btn.bounce {
    animation: emoji-bounce 0.45s ease;
  }
  @keyframes pill-in {
    from {
      transform: scale(0.5);
      opacity: 0;
    }
  }
  @keyframes count-in {
    from {
      transform: translateY(-7px);
      opacity: 0;
    }
  }
  @keyframes emoji-bounce {
    30% {
      transform: scale(1.35) rotate(-8deg);
    }
    55% {
      transform: scale(0.92) rotate(6deg);
    }
  }
  .react-wrap {
    position: relative;
    display: inline-flex;
  }
  /* Who-reacted popover, on hover. */
  .react-who {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    z-index: 30;
    min-width: 120px;
    max-width: 220px;
    padding: 7px 10px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    background: var(--bg-elevated, var(--bg-1));
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-pop);
    font-size: var(--fs-small);
    white-space: nowrap;
    /* Hover-intent: opacity/visibility (not display) so it can fade IN after a
       short delay and fade OUT smoothly. The delay stops the popover strobing as
       the pointer sweeps across a row of reaction pills. */
    opacity: 0;
    visibility: hidden;
    transform: translateY(4px);
    transition:
      opacity var(--dur-standard) ease,
      transform var(--dur-standard) ease,
      visibility 0s linear var(--dur-standard);
  }
  @media (pointer: fine) {
    .react-wrap:hover .react-who {
      opacity: 1;
      visibility: visible;
      transform: translateY(0);
      transition:
        opacity 0.18s ease 0.26s,
        transform 0.18s ease 0.26s,
        visibility 0s;
    }
  }
  .react-who strong {
    font-size: var(--fs-small);
    color: var(--text-muted);
    margin-bottom: 2px;
  }
  .rw-more {
    color: var(--text-faint);
    font-size: var(--fs-small);
  }
  .msg-actions {
    position: absolute;
    top: -16px;
    right: 10px;
    display: flex;
    align-items: center;
    gap: 3px;
    opacity: 0;
    transform: translateY(3px) scale(0.97);
    background: var(--bg-1);
    border: 1px solid var(--border);
    border-radius: var(--radius-md);
    padding: 3px;
    box-shadow: var(--shadow-pop);
    z-index: 5;
    transition:
      opacity var(--dur-quick) ease,
      transform var(--dur-quick) var(--ease-out);
  }
  .msg:hover .msg-actions,
  .msg:focus-within .msg-actions {
    opacity: 1;
    transform: none;
  }
  /* ---- phone ------------------------------------------------------------
     Touch devices have no hover — a tap would emulate it and pop the action bar
     up right under the finger, causing accidental reacts/replies. Long-press
     (action sheet) is the mobile entry point instead, and it now carries
     everything the bar does plus the link/code/timestamp cases hover never had.
     The bar is no longer rendered at all here (see the !S.isMobile gate on the
     markup), so there is nothing left for this block to hide. */
  @media (pointer: coarse), (max-width: 768px) {
    .msg {
      /* The row's gap is 12px of a 360px screen next to a 38px avatar; trimming
         it buys back characters per line where they are scarcest. */
      gap: 10px;
    }
    /* A mis-tap here is a broadcast, not a navigation slip: it reacts for
       everyone and you have to notice and undo it. 36px of pill plus an 8px gap
       gives the 44px of slop that stops that, without inflating the pill's
       visual weight the way a 44px box would. */
    .reactions {
      gap: var(--sp-2);
      margin-top: 6px;
    }
    .reaction {
      min-height: 36px;
      padding: 6px 11px;
    }
    /* The only affordance for walking a reply chain, and it was ~17px tall
       directly above the sender name — a miss opened a profile card. */
    .reply-ref {
      min-height: 34px;
      padding: 5px 10px;
    }
    .reveal-btn {
      min-height: 34px;
      padding: 6px 12px;
    }
    /* A code fence cannot be scrolled here at all: the chat pane is
       touch-action:pan-y (MobileShell), so no descendant gets a horizontal
       finger pan, and the feed clips the overflow. Wrapping is the only way the
       text is readable; "Copy Code" in the long-press sheet covers the rest. */
    .body :global(pre) {
      overflow-x: hidden;
    }
    .body :global(pre code) {
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
  }

  /* These two compensate for a touch INPUT MODEL, not a narrow screen, so they
     stay on the bare coarse query rather than the shared phone breakpoint: a
     desktop window under 768px renders the mobile shell but still has a mouse,
     and applying them there would take away text selection and the who-reacted
     card while offering nothing back (there is no long-press without touch). */
  @media (pointer: coarse) {
    /* Long-press opens the action sheet — don't let the WebView start a text
       selection under it, which would fight the gesture and leave selection
       handles over the sheet. Copy Text / Copy Link / Copy Code cover copying. */
    .msg {
      -webkit-user-select: none;
      user-select: none;
    }
    /* Duplicate of the who-reacted sheet that long-press already opens, and a
       harmful one: Chromium latches :hover after a tap, so toggling a reaction
       also left this card hanging over the feed — clipped by the feed's
       overflow-x:hidden for any pill in the right half of the row. */
    .react-who {
      display: none;
    }
  }

  /* ---- swipe-to-reply (touch; see swipeReply in the script) --------------
     The drag itself writes an inline translateX; this class rides only for the
     ~200ms snap-back so tracking stays raw under the finger. Reduced motion
     never gets the transform in the first place (guarded in JS). */
  .msg.swipe-settle {
    transition: transform 0.18s ease;
  }
  .swipe-hint {
    flex: 0 0 0;
    align-self: center;
    position: relative;
  }
  /* Opacity/scale keyed to --swipe-p (0 → commit threshold = 1), set by the
     drag on the row, so the dot fades and grows as the row travels. */
  .swipe-hint-dot {
    position: absolute;
    top: 50%;
    left: 16px;
    width: 30px;
    height: 30px;
    border-radius: 50%;
    background: var(--bg-3);
    color: var(--text-muted);
    display: grid;
    place-items: center;
    opacity: var(--swipe-p, 0);
    transform: translateY(-50%) scale(calc(0.55 + 0.45 * var(--swipe-p, 0)));
  }
  /* Past the commit threshold (the moment the haptic fires): released = reply. */
  .msg.swipe-commit .swipe-hint-dot {
    background: var(--accent);
    color: var(--bg-0);
  }
  .grp {
    display: flex;
    gap: 1px;
  }
  .sep {
    width: 1px;
    align-self: stretch;
    margin: 3px 2px;
    background: var(--border);
  }
  .msg-actions button {
    background: transparent;
    border: none;
    color: var(--text-muted);
    padding: 5px;
    min-width: 28px;
    height: 28px;
    font-size: 14px;
    display: grid;
    place-items: center;
    border-radius: var(--radius-sm);
    transition:
      background 0.1s ease,
      transform 0.08s ease;
  }
  .msg-actions button:hover {
    background: var(--bg-3);
    color: var(--text);
  }
  .msg-actions .emoji-btn:hover {
    transform: scale(1.18);
    background: transparent;
  }
  /* "smiley +" picker opener */
  .msg-actions .add-react {
    position: relative;
  }
  .msg-actions .add-react .plus {
    position: absolute;
    top: 0;
    right: 2px;
    font-size: var(--fs-tiny);
    font-weight: 700;
    line-height: 1;
  }
  .msg-actions button.on {
    color: var(--warn);
  }
  .msg-actions button.danger:hover {
    background: var(--danger-soft);
    color: var(--danger-text);
  }
  .body :global(code) {
    background: var(--bg-3);
    padding: 1px 5px;
    border-radius: var(--radius-sm);
    /* An Arabic string literal or comment inside code has no glyph in a
       monospace face and drops to whatever the system offers. Naming the
       companion keeps it the same Arabic the rest of the app is set in. */
    font-family: ui-monospace, "Noto Sans Arabic", monospace;
    font-size: var(--fs-compact);
  }
  /* Two columns, 4:3 cells, capped so a grid of eight does not take over the
     window. minmax(0,…) is load-bearing: a grid track's automatic minimum is
     its content, so without it a wide image widens its own column and the two
     stop matching — which is the ragged stack again, in a grid. */
  .att-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: var(--sp-1);
    margin-top: var(--sp-1);
    max-width: 380px;
  }
  .att-grid > :global(*) {
    aspect-ratio: 4 / 3;
    min-width: 0;
    border-radius: var(--radius-sm);
    overflow: hidden;
  }
  /* An odd count would leave a half-width orphan on the last row; let it take
     the full width instead, which is also the more useful shape for it. */
  .att-grid > :global(*:last-child:nth-child(odd)) {
    grid-column: 1 / -1;
    aspect-ratio: 16 / 9;
  }
  .body :global(pre) {
    background: var(--bg-0);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 8px 10px;
    overflow-x: auto;
    margin: 4px 0;
  }
  .body :global(pre code) {
    background: transparent;
    padding: 0;
  }
  .body :global(blockquote) {
    margin: 2px 0;
    padding: 0 0 0 8px;
    border-left: 3px solid var(--border);
    color: var(--text-muted);
  }
  .body :global(ul),
  .body :global(ol) {
    margin: 2px 0;
    padding-left: 22px;
  }
  .body :global(a) {
    color: var(--accent-hover);
    text-decoration: none;
  }
  .body :global(a:hover) {
    text-decoration: underline;
    text-underline-offset: 3px;
    text-decoration-color: color-mix(in srgb, var(--accent) 65%, transparent);
  }
  .body :global(.mention) {
    background: color-mix(in srgb, var(--text-muted) 22%, transparent);
    color: var(--text);
    border-radius: var(--radius-sm);
    padding: 0 3px;
    font-weight: 600;
    cursor: pointer;
  }
  .body :global(.mention:hover) {
    background: color-mix(in srgb, var(--text-muted) 34%, transparent);
  }
  .body :global(.mention-self) {
    background: var(--accent-soft);
    color: var(--accent-hover);
  }
  .body :global(.mention-self:hover) {
    background: color-mix(in srgb, var(--accent) 26%, transparent);
  }
  /* A role mention wears its role's colour. The tint is mixed from currentColor,
     which the renderer's inline style has already set to the role's hex — so
     one rule covers coloured and uncoloured roles alike, and no colour value
     needs to reach the stylesheet. Placed after .mention-self so the tint wins
     the background; the ring below is what says it pings you. */
  .body :global(.mention-role) {
    background: color-mix(in srgb, currentColor 16%, transparent);
  }
  .body :global(.mention-role:hover) {
    background: color-mix(in srgb, currentColor 28%, transparent);
  }
  .body :global(.mention-role.mention-self) {
    box-shadow: 0 0 0 1px color-mix(in srgb, currentColor 55%, transparent);
  }
  /* #channel is a destination, so it reads like the links next to it. */
  .body :global(.mention-channel) {
    color: var(--accent-hover);
  }
  /* Spoiler: blacked-out until clicked. */
  /* Blurred rather than blacked out: you can see there are words, roughly how
     many, and that they're waiting for you — which is the fun of a spoiler. A
     solid bar just reads as redaction. The radius is in em so it scales with
     the text and stays unreadable at any size; at 0.35em not even the letter
     shapes survive. */
  .body :global(.spoiler) {
    filter: blur(0.35em);
    background: color-mix(in srgb, var(--text-muted) 16%, transparent);
    border-radius: var(--radius-sm);
    padding: 0 3px;
    cursor: pointer;
    user-select: none;
    transition:
      filter 0.22s ease,
      background 0.22s ease;
  }
  .body :global(.spoiler:hover) {
    background: color-mix(in srgb, var(--text-muted) 26%, transparent);
  }
  .body :global(.spoiler.revealed) {
    filter: none;
    background: color-mix(in srgb, var(--text-muted) 22%, transparent);
    color: inherit;
    cursor: text;
    user-select: text;
  }
  /* Reduced motion still gets the reveal, just without the dissolve. */
  @media (prefers-reduced-motion: reduce) {
    .body :global(.spoiler) {
      transition: none;
    }
  }
  /* Inline headers (chat-sized). */
  .body :global(.md-h) {
    margin: 4px 0 2px;
    font-weight: 700;
    line-height: 1.25;
  }
  .body :global(h3.md-h) {
    font-size: 1.25em;
  }
  .body :global(h4.md-h) {
    font-size: 1.1em;
  }
  .body :global(h5.md-h) {
    font-size: 1em;
  }
  .body :global(u) {
    text-decoration: underline;
  }
  .body :global(s) {
    text-decoration: line-through;
    opacity: 0.85;
  }
  /* Code-fence language label. */
  .body :global(pre[data-lang]) {
    position: relative;
    padding-top: 20px;
  }
  .body :global(pre[data-lang])::before {
    content: attr(data-lang);
    position: absolute;
    top: 3px;
    right: 8px;
    font-size: var(--fs-tiny);
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  /* Inline (data-URI) images have no lightbox, so anything the column clips is
     unreachable by any means — 380px inside a ~300px phone column lost a third
     of the picture. min() keeps the desktop ceiling and adds the floor. */
  .body :global(img.attachment) {
    max-width: min(380px, 100%);
    max-height: 280px;
    height: auto;
    border-radius: var(--radius-sm);
    display: block;
    margin-top: var(--sp-1);
    animation: att-in 0.25s ease;
  }
  @keyframes att-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  /* Inline unicode emoji: bundled Twemoji images —
     uniform 1.375em squares hanging slightly below the baseline, identical on
     every platform (native font glyphs wobble in size and baseline per OS). */
  .body :global(img.emoji) {
    width: 1.375em;
    height: 1.375em;
    vertical-align: -0.3em;
    margin: 0 0.5px;
    object-fit: contain;
  }
  .body :global(img.cemoji) {
    height: 1.375em;
    width: auto;
    vertical-align: -0.2em;
    margin: 0 1px;
    object-fit: contain;
  }
  /* Emoji-only messages render jumbo. */
  .body.jumbo :global(img.emoji) {
    width: 3em;
    height: 3em;
    vertical-align: bottom;
    margin: 0 1.5px;
  }
  .body.jumbo :global(img.cemoji) {
    height: 2.6em;
  }
  .reaction :global(img.cemoji),
  .reaction .cemoji {
    height: 20px;
    width: auto;
    vertical-align: -3px;
  }
  /* ---- sealed timestamp ------------------------------------------------
     A seal is the author saying "the time matters here", so it sits INLINE at
     the head of the message rather than in the gutter with the ordinary clock:
     it should read as part of the sentence, not as chrome. Small, accent-tinted,
     and it states the time outright — the hover card adds the full date, the
     seconds and a live "x ago", but nothing essential hides behind a pointer. */
  .seal {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 3px;
    margin-right: 6px;
    padding: 1px 6px 1px 4px;
    border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent);
    font-size: var(--fs-tiny);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    vertical-align: baseline;
    line-height: 1.5;
    cursor: default;
    transition:
      background var(--dur-standard) ease,
      border-color var(--dur-standard) ease;
  }
  .seal:hover,
  .seal.open {
    background: color-mix(in srgb, var(--accent) 22%, transparent);
    border-color: color-mix(in srgb, var(--accent) 55%, transparent);
  }
  .seal-t {
    letter-spacing: 0.01em;
  }
  .seal-card {
    position: absolute;
    bottom: calc(100% + 6px);
    left: 0;
    z-index: 30;
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 168px;
    padding: 8px 10px;
    border-radius: var(--radius-md);
    background: var(--bg-1);
    border: 1px solid var(--border);
    box-shadow: var(--shadow-pop);
    color: var(--text);
    text-align: left;
    white-space: nowrap;
    animation: seal-in var(--dur-quick) ease-out;
  }
  @keyframes seal-in {
    from { opacity: 0; transform: translateY(3px); }
  }
  @media (prefers-reduced-motion: reduce) {
    .seal-card { animation: none; }
  }
  .seal-card-h {
    font-size: var(--fs-micro);
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .seal-card-f {
    font-size: var(--fs-compact);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }
  .seal-card-a {
    font-size: var(--fs-tiny);
    color: var(--text-muted);
  }
  /* On a phone the card would hang off the left edge of a narrow column, and
     there is no pointer to dismiss it — it closes on the next tap instead. */
  @media (pointer: coarse), (max-width: 768px) {
    .seal {
      padding: 2px 8px 2px 6px;
    }
    .seal-card {
      left: auto;
      right: 0;
    }
  }

</style>
