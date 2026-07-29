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
  import { parseEmbed, stripEmbedToken } from "./lib/richembed.js";
  import { ephemeralExpiry, stripEphemeral } from "./lib/ephemeral.svelte.js";
  import YouTubeEmbed from "./YouTubeEmbed.svelte";
  import LinkPreview from "./LinkPreview.svelte";
  import { untrack } from "svelte";
  import { renderMarkdown, emojiOnly, animatedEmojiSrc, twemojiCode } from "./lib/markdown.js";
  import { animateInView, animateOnHover } from "./lib/anemoji.js";
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
    mentionRefs,
  } from "./lib/state.svelte.js";
  import { api } from "./lib/api.js";
  import { addReminder } from "./lib/scheduled.svelte.js";
  import { longpress } from "./lib/touch.js";
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
  let { m, compact = false, replyRef = null, entering = false } = $props();

  // Moderators (Manage Messages) can delete anyone's message.
  const canDeleteOthers = $derived(has(activeGuild()?.myPerms || 0, PERM.MANAGE_MESSAGES));

  // Touch device? Drives which gesture owns the context menu (see the .msg div).
  const coarse = window.matchMedia?.("(pointer: coarse)")?.matches ?? false;
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
  // Keep the hover bar minimal, Discord-style: three quick reactions (your
  // recents first) — the smile button opens the full picker for everything else.
  const DEFAULT_QUICK = ["👍", "❤️", "😂"];
  const quickEmojis = [...new Set([...recentEmoji(), ...DEFAULT_QUICK])].slice(0, 3);

  // The emoji the user just tapped bounces briefly (quick bar + pills share
  // this, keyed by emoji char).
  let bounced = $state(null);
  let bounceTimer;
  function reactWithBounce(emoji) {
    clearTimeout(bounceTimer);
    bounced = null; // restart the CSS animation on rapid re-clicks
    requestAnimationFrame(() => {
      bounced = emoji;
      bounceTimer = setTimeout(() => (bounced = null), 500);
    });
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
  // this channel belongs to — not a remote server, so the local guild is it.
  const announceGuild = $derived(announce ? activeGuild() : null);
  const richEmbed = $derived(m.deleted ? null : parseEmbed(m.content));
  const atts = $derived(m.deleted ? [] : parseAttachTokens(m.content));
  const files = $derived(m.deleted ? [] : parseFileTokens(m.content));
  const bodyText = $derived.by(() => {
    let c = atts.length || files.length ? stripAttachTokens(m.content) : m.content;
    if (richEmbed) c = stripEmbedToken(c);
    return stripEphemeral(c);
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

  function messageMenu(e) {
    if (m.deleted) return;
    // Right-clicking an INLINE image (markdown data-URI) gets the image menu —
    // "Copy Text" on a picture just copies the word "image", which helps nobody.
    // (Encrypted attachments render via Attachment.svelte, which has its own.)
    const img = e.target?.closest?.("img.attachment");
    if (img) {
      openContextMenu(e, [
        {
          label: "Copy Image",
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
        { label: "Save Image", icon: "download", onClick: () => saveImageSrc(img.src) },
        { sep: true },
        // Straight from "that picture is funny" to the editor with it already
        // loaded — the shortest path there is, and the reason the editor takes
        // a src rather than only offering its own templates.
        {
          label: "Make a Meme",
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
      { label: "Reply", icon: "reply", onClick: () => (S.replyingTo = m) },
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
      { label: "Add Reaction", icon: "smile", onClick: () => (S.pickerTarget = m) },
      { sep: true },
      { label: "Copy Text", icon: "edit", onClick: () => copy(stripAttachTokens(m.content).trim() || previewText(m.content), "Copied text") },
      {
        label: "Copy Message Link",
        icon: "forward",
        onClick: () => copy(`concord://msg/${m.channelId}/${m.id}`, "Copied message link"),
      },
      { label: m.pinned ? "Unpin" : "Pin", icon: "pin", onClick: () => api.pinMessage(m.channelId, m.id) },
      activeChannel()?.type === "announcement" && (isOwn || canDeleteOthers) && {
        label: "Publish",
        icon: "megaphone",
        onClick: () => (S.modal = { kind: "publish", message: m, channel: activeChannel() }),
      },
      { label: "Forward", icon: "forward", onClick: () => (S.modal = { kind: "forward", message: m }) },
      { label: "Mark Unread", icon: "bell", onClick: () => markUnread(m.channelId, m) },
      {
        label: "Remind Me",
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
      (isOwn || canDeleteOthers) && { sep: true },
      (isOwn || canDeleteOthers) && {
        label: "Delete",
        icon: "trash",
        danger: true,
        onClick: () => deleteMsg(m),
      },
    ], {
      // Mobile action sheet only: tap-to-react row on top, recents first
      // (desktop's anchored popover ignores these extras).
      quick: { emojis: quickEmojis, onPick: reactWithBounce },
    });
  }
</script>

<!-- Touch: only the longpress action opens the menu (Android's WebView also
     synthesizes contextmenu on long-press — letting both run opens the sheet
     twice: double haptic + re-keyed rows). Mouse right-click keeps contextmenu. -->
<div
  class="msg"
  class:compact
  class:enter={entering}
  data-msg-id={m.id}
  oncontextmenu={coarse ? (e) => e.preventDefault() : messageMenu}
  use:longpress={{ handler: messageMenu }}
>
  {#if compact}
    <span class="gutter-time muted" title={new Date(m.sent).toLocaleString()}>{fmtTime(m.sent)}</span>
  {:else}
    {#if guest}
      <span class="av-btn guest-av" title="A guest in this meeting">
        <Avatar name={guestName} emoji="👤" color="#5b6270" size={38} />
      </span>
    {:else if announce}
      <!-- A published announcement is the server talking, so it wears the
           server's face rather than the face of whoever pressed Publish. -->
      <span class="av-btn" title={announceGuild?.name || "Announcement"}>
        <Avatar
          name={announceGuild?.name || "Server"}
          image={announceGuild?.icon || ""}
          size={38}
        />
      </span>
    {:else}
      <button
        class="av-btn"
        title="View profile"
        onclick={(e) => openProfilePopover(m.sender, e.currentTarget)}
      >
        <Avatar
          name={nameFor(m.sender, m.senderName)}
          emoji={mem?.emoji}
          color={mem?.color}
          image={mem?.avatar}
          size={38}
        />
      </button>
    {/if}
  {/if}

  <div class="msg-main">
    {#if m.replyTo && !compact}
      <button class="reply-ref" title="Jump to original message" onclick={jumpToReply}>
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
          <span class="guest-badge" title="Joined from a meeting link — no account, unverified"
            >guest</span
          >
        {:else if announce}
          <span class="sender server-name">{announceGuild?.name || "Server"}</span>
          <span class="ann-badge" title={announce.from ? `Published from #${announce.from}` : "Announcement"}>
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
            <span class="verify-check" title="Identity verified"><Icon name="check" size={11} /></span>
          {/if}
        {/if}
        <span class="muted time" title={new Date(m.sent).toLocaleString()}>{fmtTime(m.sent)}</span>
        {#if m.pinned}<span class="pin-mark" title="Pinned"><Icon name="pin" size={11} /></span>{/if}
      </div>
    {:else if m.pinned}
      <span class="pin-mark inline" title="Pinned"><Icon name="pin" size={11} /></span>
    {/if}

    {#if m.deleted}
      <!-- svelte-ignore a11y_no_static_element_interactions, a11y_mouse_events_have_key_events -->
      <div
        class="body deleted"
        class:revealable={canRevealDeleted && revealed === null}
        onmouseenter={hoverReveal}
      >
        {#if revealed !== null}
          <span class="revealed-tag" title="Deleted — shown to you as a moderator">
            <Icon name="lock" size={10} /> deleted · original
          </span>
          <span class="revealed-text">{revealDisplay || revealed}</span>
        {:else if m.expired}
          <em class="disappeared"><Icon name="clock" size={11} /> message disappeared</em>
        {:else}
          <em>deleted</em>
          {#if canRevealDeleted}
            <button class="reveal-btn" onclick={revealOriginal} disabled={revealing}>
              {revealing ? "…" : "hover or click to reveal"}
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
            // Focus moving WITHIN the edit UI (the emoji button/picker) must
            // not commit-and-close — that's what made inserting emoji into an
            // edit impossible.
            if (!e.relatedTarget?.closest?.(".edit-wrap")) commitEdit();
          }}
        ></textarea>
        <button
          type="button"
          class="edit-emoji"
          title="Insert emoji"
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
      <div class="edit-actions">
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
        <div class="body" class:jumbo={jumbo} use:animateInView={jumbo} onclick={onBodyClick} onkeydown={onBodyKeydown} onmouseover={onBodyOver} onmouseout={onBodyOut} onfocusin={onBodyOver}>
          {@html renderMarkdown(bodyText, mentionNames, cemoji, refs)}{#if m.edited}<span
              class="edited-tag">(edited)</span
            >{/if}
        </div>
      {/if}
      {#each atts as tok (tok.blobId)}
        <Attachment channelId={m.channelId} {tok} messageId={m.id} own={isOwn} />
      {/each}
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
        <span class="eph-hint" title="Disappears {new Date(ephExp).toLocaleString()}">
          <Icon name="clock" size={10} /> disappearing
        </span>
      {/if}
    {/if}

    {#if !poll && m.reactions && Object.keys(m.reactions).length}
      <div class="reactions" use:animateOnHover>
        {#each Object.entries(m.reactions) as [emoji, fprs] (emoji)}
          {@const cimg = /^:([a-z0-9_]{2,32}):$/.test(emoji) ? cemoji[emoji.slice(1, -1)] : null}
          <span class="react-wrap">
            <button
              class="reaction"
              class:mine={fprs.includes(S.identity.fingerprint)}
              onclick={() => reactWithBounce(emoji)}
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
  </div>

  {#if !m.deleted && S.editing?.id !== m.id}
    <div class="msg-actions" role="toolbar" aria-label="Message actions">
      <div class="grp">
        {#each quickEmojis as e (e)}
          <button class="emoji-btn" class:bounce={bounced === e} title="React {e}" aria-label="React {e}" onclick={() => reactWithBounce(e)}>{e}</button>
        {/each}
        <button class="add-react" title="More reactions" aria-label="More reactions" onclick={() => (S.pickerTarget = m)}>
          <Icon name="smile" size={15} />
          <span class="plus" aria-hidden="true">+</span>
        </button>
      </div>
      <span class="sep"></span>
      <div class="grp">
        <button title="Reply" aria-label="Reply" onclick={() => (S.replyingTo = m)}>
          <Icon name="reply" size={15} />
        </button>
        <button title="Forward" aria-label="Forward" onclick={() => (S.modal = { kind: "forward", message: m })}>
          <Icon name="forward" size={15} />
        </button>
        <button
          class:on={m.pinned}
          title={m.pinned ? "Unpin" : "Pin"}
          aria-label={m.pinned ? "Unpin" : "Pin"}
          onclick={() => api.pinMessage(m.channelId, m.id)}
        >
          <Icon name="pin" size={15} />
        </button>
      </div>
      {#if m.sender === S.identity.fingerprint}
        <span class="sep"></span>
        <div class="grp">
          <button title="Edit" aria-label="Edit" onclick={startEdit}><Icon name="edit" size={15} /></button>
          <button class="danger" title="Delete" aria-label="Delete" onclick={() => deleteMsg(m)}>
            <Icon name="trash" size={15} />
          </button>
        </div>
      {:else if canDeleteOthers}
        <span class="sep"></span>
        <div class="grp">
          <button class="danger" title="Delete (moderator)" aria-label="Delete message" onclick={() => deleteMsg(m)}>
            <Icon name="trash" size={15} />
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  /* Row rhythm comes from the density vars in app.css (Appearance setting):
     cozy is today's spacing, compact tightens padding + group pull together. */
  .msg {
    display: flex;
    gap: 12px;
    position: relative;
    padding: var(--msg-pad-y, 2px) 0;
    border-radius: var(--radius-sm);
  }
  .msg:hover {
    background: color-mix(in srgb, var(--bg-3) 40%, transparent);
  }
  .msg.compact {
    margin-top: var(--msg-group-pull, -10px);
  }
  /* Newest appended message only (see MessageList): quick fade + slide-up.
     The global reduced-motion override in app.css zeroes the duration. */
  .msg.enter {
    animation: msg-in 0.26s cubic-bezier(0.2, 0.8, 0.2, 1) backwards;
  }
  @keyframes msg-in {
    from {
      opacity: 0;
      transform: translateY(8px);
    }
  }
  .gutter-time {
    width: 38px;
    font-size: 10px;
    text-align: right;
    opacity: 0;
    flex-shrink: 0;
    padding-top: 4px;
  }
  .msg.compact:hover .gutter-time {
    opacity: 1;
  }
  .msg-main {
    min-width: 0;
    flex: 1;
  }
  .reply-ref {
    display: flex;
    align-items: center;
    gap: 5px;
    font-size: 12px;
    color: var(--text-muted);
    border-left: 2px solid var(--border);
    padding: 1px 8px 1px 8px;
    margin-bottom: 2px;
    background: transparent;
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
    max-width: 100%;
    min-width: 0;
    transition:
      background 0.12s ease,
      border-color 0.12s ease,
      color 0.12s ease;
  }
  .reply-ref:hover {
    background: color-mix(in srgb, var(--bg-3) 65%, transparent);
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
  .reply-ref:hover .reply-snippet:not(.faded) {
    color: var(--text);
  }
  .msg-head {
    display: flex;
    gap: 8px;
    align-items: baseline;
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
      box-shadow 0.12s ease,
      transform 0.12s ease;
  }
  .av-btn:hover :global(.avatar) {
    box-shadow: 0 0 0 2px var(--accent);
  }
  .sender {
    background: transparent;
    border: none;
    padding: 0;
    font: inherit;
    font-weight: 600;
    color: var(--accent-hover);
    cursor: pointer;
  }
  .sender:hover {
    background: transparent;
    text-decoration: underline;
  }
  /* Marks the row as the server speaking rather than a person — quiet, since
     the guild's name and icon already carry the point. */
  .server-name {
    font-weight: 700;
    color: var(--text);
    cursor: default;
  }
  .ann-badge {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 1px 7px;
    border-radius: 999px;
    background: var(--accent-soft);
    color: var(--accent-hover);
    font-size: 10px;
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
    font-size: 11px;
  }
  .pin-mark {
    color: var(--warn);
    animation: pin-in 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
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
    font-size: 10px;
    font-weight: 600;
    line-height: 16px;
    border-radius: 4px;
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
    /* Comfortable reading measure for multi-line messages — matches Discord's
       roomier line-height without stretching single-line rows noticeably. */
    line-height: 1.45;
  }
  .reveal-btn {
    margin-left: 8px;
    padding: 1px 8px;
    font-size: 11px;
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
    font-size: 10px;
    font-style: normal;
    border-radius: 4px;
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
    transition: color 0.15s ease;
  }
  .body.deleted.revealable:hover em {
    color: var(--accent-hover);
  }
  /* Expired = gone by a timer, on purpose. A faint accent tint sets it apart
     from a plain "deleted" tombstone. */
  .disappeared {
    display: inline-flex;
    align-items: center;
    gap: 4px;
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
    font-size: 10px;
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
    font-size: 10px;
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
     message sits too close to the top of the window to fit it above. */
  .edit-wrap :global(.picker) {
    bottom: calc(100% + 6px);
    top: auto;
    right: 0;
  }
  .edit-wrap.pick-below :global(.picker) {
    top: calc(100% + 6px);
    bottom: auto;
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
      color 0.12s ease,
      background 0.12s ease;
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
    font-size: 12px;
    font-weight: 600;
    border-radius: var(--radius-sm);
    background: var(--bg-3);
    color: var(--text-muted);
    border: 1px solid transparent;
    transition:
      background 0.12s,
      color 0.12s;
  }
  .edit-btn:hover {
    background: color-mix(in srgb, var(--text) 14%, var(--bg-3));
    color: var(--text);
  }
  .edit-btn.save {
    background: var(--accent);
    color: var(--accent-fg, #fff);
  }
  .edit-btn.save:hover {
    background: var(--accent-hover);
  }
  .edit-btn:focus-visible {
    border-color: var(--accent);
  }
  .edit-hint {
    font-size: 11px;
    margin-top: 0;
  }
  /* The shortcut hint is the redundant half once there are buttons — keep it
     where there is room, drop it on a phone where the row would wrap. */
  @media (max-width: 600px) {
    .edit-hint {
      display: none;
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
    padding: 4px;
    display: flex;
    flex-direction: column;
    z-index: 30;
  }
  .es-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 5px 8px;
    background: transparent;
    border: none;
    border-radius: var(--radius-sm);
    color: var(--text);
    font-size: 13px;
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
    font-size: 10px;
    color: var(--text-faint);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 0 4px;
  }
  .reactions {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    margin-top: 4px;
  }
  .reaction {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: var(--bg-3);
    border: 1px solid var(--border);
    color: var(--text);
    padding: 3px 9px;
    font-size: 13px;
    border-radius: 9px;
    /* springy pop when a new pill appears (overshoot bezier) */
    animation: pill-in 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
    transition:
      transform 0.1s ease,
      border-color 0.12s ease,
      background 0.12s ease,
      box-shadow 0.12s ease;
  }
  .reaction:hover {
    transform: translateY(-1px);
    border-color: var(--text-faint);
    box-shadow: 0 2px 6px rgb(0 0 0 / 0.18);
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
  .reaction.mine:hover {
    border-color: var(--accent);
    box-shadow:
      0 0 0 1px var(--accent),
      0 2px 6px rgb(0 0 0 / 0.18);
  }
  .reaction.mine .rcount {
    color: var(--accent-hover);
    font-weight: 600;
  }
  /* Discord sizes the emoji noticeably larger than the count — the glyph is
     the thing you read at a glance. Overrides the pill's 13px font. */
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
    font-size: 13px;
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
    font-size: 12px;
    white-space: nowrap;
    /* Hover-intent: opacity/visibility (not display) so it can fade IN after a
       short delay and fade OUT smoothly. The delay stops the popover strobing as
       the pointer sweeps across a row of reaction pills. */
    opacity: 0;
    visibility: hidden;
    transform: translateY(4px);
    transition:
      opacity 0.15s ease,
      transform 0.15s ease,
      visibility 0s linear 0.15s;
  }
  .react-wrap:hover .react-who {
    opacity: 1;
    visibility: visible;
    transform: translateY(0);
    transition:
      opacity 0.18s ease 0.26s,
      transform 0.18s ease 0.26s,
      visibility 0s;
  }
  .react-who strong {
    font-size: 11px;
    color: var(--text-muted);
    margin-bottom: 2px;
  }
  .rw-more {
    color: var(--text-faint);
    font-size: 11px;
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
    border-radius: 10px;
    padding: 3px;
    box-shadow: var(--shadow-pop);
    z-index: 5;
    transition:
      opacity 0.12s ease,
      transform 0.14s cubic-bezier(0.2, 0.9, 0.3, 1);
  }
  .msg:hover .msg-actions,
  .msg:focus-within .msg-actions {
    opacity: 1;
    transform: none;
  }
  /* Touch devices have no hover — a tap would emulate it and pop this bar up
     right under the finger, causing accidental reacts/replies. Long-press
     (action sheet) is the mobile entry point instead. */
  @media (pointer: coarse) {
    .msg-actions {
      display: none;
    }
    /* Long-press opens the action sheet — don't let the WebView start a text
       selection under it ("Copy Text" in the sheet covers copying). */
    .msg {
      -webkit-user-select: none;
      user-select: none;
    }
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
    border-radius: 7px;
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
    font-size: 10px;
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
    border-radius: 4px;
    font-family: ui-monospace, monospace;
    font-size: 12px;
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
    border-radius: 4px;
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
    border-radius: 4px;
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
    font-size: 10px;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--text-faint);
  }
  .body :global(img.attachment) {
    max-width: 380px;
    max-height: 280px;
    border-radius: var(--radius-sm);
    display: block;
    margin-top: 4px;
    animation: att-in 0.25s ease;
  }
  @keyframes att-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  /* Inline unicode emoji: bundled Twemoji images at Discord's exact sizing —
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
  /* Emoji-only messages render jumbo, like Discord. */
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
</style>
