<script>
  // One encrypted image attachment: reserves layout space from the token's
  // dimensions, fetches + decrypts through the backend, and renders with
  // spinner / error+retry states. Clicking opens a lightbox that shows the
  // whole picture fitted to the window: click and wheel zoom toward the cursor
  // in steps, double-click toggles fit and 100%, drag pans, pinch works on
  // touch, Esc / backdrop / ✕ close.
  import Icon from "./Icon.svelte";
  import FileAttachment from "./FileAttachment.svelte";
  import MediaViewer from "./MediaViewer.svelte";
  import {
    loadAttachment,
    copyImageToClipboard,
    saveImageSrc,
    unavailableNote,
    worthRetrying,
    placeholderName,
  } from "./lib/attachments.js";
  import { dataUrlBytes } from "./lib/attachopts.js";
  import { knownRecipe } from "./lib/memerecipe.js";
  import { openContextMenu, flash, S, memberByFpr, nameFor, fmtClock } from "./lib/state.svelte.js";
  import { humanError } from "./lib/errors.js";
  import { longpress } from "./lib/touch.js";
  import { registerFrame, frameEntry, whenLabel } from "./lib/viewerset.js";

  // `messageId`/`own` exist only for "Edit meme": editing a picture in place
  // means editing the message that carries it, and only its author may.
  // `tile` says this image is one cell of a multi-image grid rather than a
  // standalone attachment: it fills the cell its parent sized instead of
  // sizing itself, and the cell is what keeps four photos looking like four
  // photos rather than a ragged stack of four different shapes.
  // `sender` is the account fingerprint of whoever posted the picture. It is
  // only ever used when the picture will NOT load: an attachment lives on the
  // peers that hold its bytes, so "we can't get it" almost always means "the
  // person who sent it is offline", and naming them is the difference between
  // a broken frame and a wait with an end to it.
  // `sent` (the message's time) and `text` (its words) are for the caption and
  // the viewer's details: a description that only repeats the message's own
  // text is not printed twice under the picture.
  let { channelId, tok, messageId = "", own = false, tile = false, sender = "", sent = "", text = "" } = $props();

  // Right-click on the image (thumbnail or lightbox): copy to the system
  // clipboard as a real image (paste it anywhere — in Concord or outside),
  // or save it to disk.
  // Can this picture be reopened in the editor it came out of? Three things
  // have to hold, and the check is done at click time rather than in a
  // $derived because the recipe index is plain module state, not reactive:
  // it must be YOUR message (an edit is a message edit), it must be an image
  // we can address, and the recipe — which never leaves the device that made
  // the meme — must still be here. Fail any of them and only the ordinary
  // "Make a Meme" is offered, which starts a NEW meme from the flattened
  // picture and says so.
  const editable = () => own && !!messageId && knownRecipe(tok.blobId);

  function imageMenu(e) {
    openContextMenu(e, [
      {
        label: "Copy image",
        icon: "copy",
        onClick: async () => {
          try {
            await copyImageToClipboard(src);
            flash("Image copied", "success");
          } catch (err) {
            flash(`Couldn't copy image: ${humanError(err)}`, "error");
          }
        },
      },
      {
        label: "Save image",
        icon: "download",
        // Honour a sender-supplied file name when there is one: it is the only
        // thing the v2 token's name field is good for, and without this the
        // rename control in the composer has no observable effect anywhere.
        onClick: saveThis,
      },
      { sep: true },
      editable() && {
        label: "Edit meme",
        icon: "edit",
        onClick: () =>
          (S.modal = { kind: "meme", edit: { channelId, messageId, blobId: tok.blobId } }),
      },
      // `src` is the already-decrypted data URL, so the editor gets the picture
      // itself and never has to know it came from an encrypted attachment.
      // Still offered next to "Edit meme": one fixes this meme, the other
      // starts a fresh one on top of the flattened result.
      { label: "Make a meme", icon: "spark", onClick: () => (S.modal = { kind: "meme", src }) },
    ]);
  }

  // Touch counterpart of the right-click above. Svelte delegates touchstart at
  // the root, so an attribute handler would run AFTER the message row's own
  // longpress action has armed and both sheets would open; a native listener
  // here stops the bubble first. A covered spoiler lets it through on purpose —
  // there is no picture to act on yet, so the row's menu is the useful one.
  function imageTouch(node) {
    const stop = (ev) => {
      if (!hidden) ev.stopPropagation();
    };
    node.addEventListener("touchstart", stop, { passive: true });
    return { destroy: () => node.removeEventListener("touchstart", stop) };
  }
  const imageLongPress = (e) => {
    if (!hidden) imageMenu(e);
  };

  let state = $state("loading"); // loading | done | error
  // A spoiler stays covered until clicked. Tracked here rather than derived
  // from the token so revealing one doesn't un-hide every other copy of the
  // same image, and so it re-covers if the row is rebuilt.
  let revealed = $state(false);
  const hidden = $derived(!!tok.spoiler && !revealed);
  const reveal = () => (revealed = true);
  let src = $state("");
  let errMsg = $state("");

  // Reserve space: scale token dims into the same box the CSS allows. The width
  // is only the DESKTOP ceiling — the placeholder carries the ratio as an
  // aspect-ratio so `max-width:100%` can shrink it inside a phone's ~300px
  // column without the reserved box ending up the wrong shape (and without the
  // frame visibly jumping when the real image lands).
  const MAXW = 380;
  const MAXH = 280;
  const dims = $derived.by(() => {
    if (!tok.w || !tok.h) return { w: 220, h: 140 };
    const scale = Math.min(MAXW / tok.w, MAXH / tok.h, 1);
    return { w: Math.round(tok.w * scale), h: Math.round(tok.h * scale) };
  });

  async function fetchNow() {
    state = "loading";
    try {
      src = await loadAttachment(channelId, tok);
      state = "done";
    } catch (err) {
      errMsg = String(err?.message || err);
      state = "error";
    }
  }

  // Refetch only when this is genuinely a different blob.
  //
  // `tok` is parsed fresh out of the message content, so every re-render hands
  // us a new object with the same contents — and a new message arriving in the
  // channel re-renders the whole feed. Keying the effect on the object rather
  // than the id meant an unrelated message made every visible image drop back
  // to `state = "loading"`, which tears down the <img> and, with it, the
  // lightbox mounted beside it: open a picture, someone says hello, the picture
  // blinks or closes. The bytes were cached the whole time; only the state flag
  // moved.
  let loadedBlob = "";
  $effect(() => {
    const id = tok.blobId;
    if (id === loadedBlob) return;
    loadedBlob = id;
    fetchNow();
  });

  // Who to blame, and whether waiting will fix it. The roster only covers the
  // guild currently open, so `online` is deliberately null rather than false
  // when there is no row — "I don't know where they are" and "they are away"
  // are different sentences.
  const senderRow = $derived(sender ? memberByFpr(sender) : undefined);
  const who = $derived({
    name: senderRow ? nameFor(sender) : "",
    self: !!sender && sender === S.identity?.fingerprint,
  });
  const note = $derived(unavailableNote(errMsg, who));
  // A picture the core will not hand a renderer (over 50 MP, or no header it
  // can read) is still the sender's file: it is offered to save, not shown.
  const asFile = $derived(state === "error" && /too large to show/.test(errMsg));
  const fileTok = $derived({
    blobId: tok.blobId,
    keys: tok.keys,
    mime: `image/${tok.subtype}`,
    name: placeholderName(tok) || `picture.${tok.subtype === "jpeg" ? "jpg" : tok.subtype}`,
    size: 0,
  });

  // Auto-retry when the roster changes. Nothing about this message changes when
  // the sender comes back — the token was always fine — so without this the
  // frame stays dead until the reader thinks to reload the whole app, which is
  // a strange thing to expect of someone who was only told "nobody has it".
  // S.members is refreshed off presence events, so its reassignment is the
  // in-app signal that the network moved; worthRetrying decides which of those
  // are worth spending a fetch on.
  let seenOnline = null;
  let lastTry = 0;
  $effect(() => {
    const now = new Set(
      (S.members || []).filter((m) => m.online).map((m) => m.fingerprint),
    );
    const prev = seenOnline;
    seenOnline = now;
    if (state !== "error") return;
    if (!worthRetrying(prev, now, Date.now() - lastTry)) return;
    lastTry = Date.now();
    fetchNow();
  });

  async function saveThis() {
    const how = await saveImageSrc(src, tok.name || `concord-${(tok.blobId || "image").slice(0, 8)}.png`);
    if (how === "gallery") flash("Saved to your gallery", "success");
    else if (!how) flash("Couldn't save that image");
  }
  async function copyThis() {
    try {
      await copyImageToClipboard(src);
      flash("Image copied", "success");
    } catch (err) {
      flash(`Couldn't copy image: ${humanError(err)}`, "error");
    }
  }

  // ---- the viewer (MediaViewer.svelte) ----
  // Each picture registers what the viewer needs under its frame. A message
  // with several pictures is one set: the viewer opened on one of them walks
  // the others without closing, found through the message id the grid stamps
  // on every tile.
  let frameEl = $state(null);
  const captionShown = $derived(!!tok.desc && tok.desc.trim() !== String(text || "").trim());
  $effect(() => {
    if (!frameEl || state !== "done") return;
    return registerFrame(frameEl, {
      get src() {
        return src;
      },
      get covered() {
        return hidden;
      },
      tok,
      sender,
      sent,
      reveal,
      menu: imageMenu,
      save: saveThis,
      copy: copyThis,
    });
  });
  function siblings() {
    if (!messageId || !frameEl) return [];
    const row = frameEl.closest("[data-msg-id]");
    return row ? [...row.querySelectorAll(`.frame.done[data-att-msg="${CSS.escape(messageId)}"]`)] : [];
  }
  let lightbox = $state(false);
  let set = $state([]);
  let at = $state(0);
  function openLightbox() {
    const list = tile ? siblings() : [];
    set = list.length ? list : [frameEl];
    at = Math.max(0, set.indexOf(frameEl));
    lightbox = true;
  }
  const cur = $derived(lightbox ? frameEntry(set[at]) : null);
  const viewItem = $derived.by(() => {
    const e = cur;
    if (!e) return null;
    const t = e.tok;
    return {
      key: t.blobId,
      kind: "image",
      src: e.src,
      w: t.w,
      h: t.h,
      name: t.name,
      desc: t.desc,
      size: dataUrlBytes(e.src),
      sender: e.sender ? nameFor(e.sender) : "",
      when: e.sent ? whenLabel(e.sent, fmtClock) : "",
      spoiler: e.covered,
    };
  });
  function closeLightbox() {
    if (!lightbox) return;
    const back = set[at];
    lightbox = false;
    set = [];
    // Focus goes back to the picture the viewer ended on.
    const to = back?.isConnected ? back : frameEl;
    if (to?.isConnected) to.focus({ preventScroll: true });
  }
</script>

{#if state === "done"}
  <div class="att" class:tile>
    <button
      class="frame done"
      class:tile
      class:hidden
      class:fill={tile && !hidden && !!src}
      bind:this={frameEl}
      style:width={!tile && tok.w && tok.h ? `min(${dims.w}px, 100%)` : undefined}
      style:--pic={tile && !hidden && src ? `url("${src}")` : undefined}
      data-att-msg={tile ? messageId : undefined}
      onclick={hidden ? reveal : openLightbox}
      oncontextmenu={hidden ? undefined : (e) => (e.pointerType === "touch" ? e.preventDefault() : imageMenu(e))}
      title={hidden ? "Spoiler — click to reveal" : "Click to enlarge"}
      use:imageTouch
      use:longpress={{ handler: imageLongPress }}
    >
      <!-- min(): 380px is the desktop ceiling, but a phone's message column is
           ~300px and the feed clips its overflow — an unclamped landscape photo
           simply lost its right-hand third with no way to reach it. -->
      <img
        {src}
        alt={tok.desc || "attachment"}
        class:blur={hidden}
        style={tile
          ? ""
          : tok.w && tok.h
            ? `width:100%;height:auto;aspect-ratio:${tok.w} / ${tok.h}`
            : `max-width:min(${MAXW}px, 100%);max-height:${MAXH}px`}
      />
      {#if hidden}<span class="spoiler-tag"><Icon name="spoiler" size={16} /> Spoiler</span>{/if}
      {#if tile && captionShown && !hidden}
        <!-- In a grid the caption rides on its own picture, over a fade, so
             the grid stays a grid. -->
        <span class="tile-cap" aria-hidden="true">{tok.desc}</span>
      {/if}
    </button>
    {#if !tile && captionShown && !hidden}
      <!-- The sender's description, shown as a caption as well as being the alt
           text: it's useful to everyone, not only to a screen reader. -->
      <span class="att-desc" dir="auto">{tok.desc}</span>
    {/if}
  </div>
  {#if lightbox && viewItem}
    <MediaViewer
      item={viewItem}
      index={at}
      count={set.length}
      origin={() => set[at]}
      onStep={set.length > 1 ? (d) => (at = Math.max(0, Math.min(set.length - 1, at + d))) : null}
      onClose={closeLightbox}
      onSave={() => cur?.save()}
      onCopy={() => cur?.copy()}
      onMenu={(e) => cur?.menu(e)}
      onReveal={() => cur?.reveal()}
    />
  {/if}
{:else if asFile}
  <FileAttachment {channelId} tok={fileTok} />
{:else}
  <div
    class="frame placeholder"
    class:tile
    class:gone={state === "error"}
    style={tile
      ? ""
      : state === "error"
        ? `width:${dims.w}px;min-height:${Math.max(dims.h, 150)}px`
        : `width:${dims.w}px;aspect-ratio:${dims.w} / ${dims.h}`}
  >
    {#if state === "loading"}
      <!-- A shimmer in the picture's own shape: the frame is already the size
           the picture will be, so nothing moves when it lands. -->
      <span class="shimmer" aria-hidden="true"></span>
      <span class="ph-ic" aria-hidden="true"><Icon name="image" size={20} /></span>
      <span class="sr-only">Fetching image…</span>
    {:else}
      <Icon name="close" size={18} />
      <!-- Name the picture the reader is missing. A v2 token carries the
           sender's file name and every token carries the dimensions, so even
           with none of the bytes in hand there is something concrete to say
           about what is not showing.

           A spoiler gets the anonymous label instead — see placeholderName. -->
      {#if placeholderName(tok)}<span class="ph-name">{placeholderName(tok)}</span>{/if}
      {#if tok.w && tok.h}<span class="muted small">{tok.w} × {tok.h}</span>{/if}
      <span class="muted small note">{note}</span>
      <button class="ghost retry" onclick={fetchNow}>Retry</button>
    {/if}
  </div>
{/if}

<style>
  .att {
    display: block;
    width: fit-content;
    max-width: 100%;
    margin-top: var(--sp-1);
  }
  .att.tile {
    width: 100%;
    height: 100%;
    margin-top: 0;
  }
  .frame {
    position: relative; /* anchors the spoiler label and the caption */
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
    border-radius: var(--radius-lg);
    overflow: hidden;
  }
  .frame.done {
    background: transparent;
    padding: 0;
    display: block;
    width: fit-content;
    /* The shrink-to-fit box must itself be capped, or the image's own
       max-width:100% has a containing block wider than the column. */
    max-width: 100%;
    cursor: zoom-in;
    /* Its own layer for the clip, so the picture's hover scale stays inside
       the rounded corners in every engine. */
    isolation: isolate;
  }
  .frame.done:hover {
    background: transparent;
  }
  .frame.done:focus-visible {
    outline: var(--focus-ring);
    outline-offset: var(--focus-ring-offset);
  }
  /* A hairline ring inside the edge, so a photo whose own edge is the colour
     of the page still has one. */
  .frame.done::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--text) 9%, transparent);
    pointer-events: none;
  }
  .frame.done img {
    display: block;
    border-radius: inherit;
    transition: transform var(--dur-move) var(--ease-out);
  }
  .frame.done img.blur {
    filter: blur(22px);
    transform: scale(1.12); /* the blur's soft edge stays outside the frame */
  }
  .frame.hidden {
    cursor: pointer;
  }
  /* Leaning in: on a real pointer the picture comes a little closer under the
     cursor, inside its own frame. Not on touch: Chromium latches :hover onto
     the last-tapped element. */
  @media (pointer: fine) {
    .frame.done:not(.hidden):hover img {
      transform: scale(1.025);
    }
  }
  .spoiler-tag {
    position: absolute;
    left: 50%;
    top: 50%;
    display: inline-flex;
    align-items: center;
    gap: var(--sp-1);
    padding: 6px var(--sp-3);
    border-radius: 999px;
    background: rgba(0, 0, 0, 0.55);
    border: 1px solid rgba(255, 255, 255, 0.28);
    color: #fff;
    font-size: var(--fs-compact);
    font-weight: 600;
    transform: translate(-50%, -50%);
    pointer-events: none;
    white-space: nowrap;
  }
  .att-desc {
    display: block;
    margin-top: 6px;
    font-size: var(--fs-compact);
    line-height: 1.45;
    color: var(--text-muted);
    max-width: min(380px, 100%);
  }

  /* One cell of a grid: the cell owns the box, the image fills it. `contain`
     rather than `cover` on purpose — a cropped thumbnail of a screenshot is
     a picture of the middle of a screenshot, and the whole reason to send four
     at once is usually that each one shows something different. */
  .frame.tile {
    width: 100%;
    height: 100%;
    border-radius: inherit;
    background: color-mix(in srgb, var(--bg-0) 70%, black);
    color: var(--text);
  }
  .frame.done.tile img {
    position: relative;
    width: 100%;
    height: 100%;
    object-fit: contain;
  }
  /* The room beside a picture that does not share its cell's shape carries
     the picture itself, blurred and dimmed, as the staged cards do: the set
     reads as one composition, not photos on grey. Never under a spoiler. */
  .frame.tile.fill::before {
    content: "";
    position: absolute;
    inset: -16px;
    background: var(--pic) center / cover;
    filter: blur(16px) saturate(1.2);
    opacity: 0.6;
    pointer-events: none;
  }
  .tile-cap {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    padding: var(--sp-5) var(--sp-3) var(--sp-2);
    background: linear-gradient(to top, rgba(0, 0, 0, 0.72), transparent);
    color: #fff;
    font-size: var(--fs-small);
    line-height: 1.35;
    text-align: start;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    pointer-events: none;
  }

  .placeholder {
    background: var(--bg-2);
    color: var(--text-faint);
    max-width: 100%;
    margin-top: var(--sp-1);
    box-shadow: inset 0 0 0 1px var(--hairline);
  }
  .placeholder.tile {
    margin-top: 0;
  }
  /* A soft band of light that crosses the frame while the bytes arrive:
     transform only, on one layer. */
  .shimmer {
    position: absolute;
    inset: 0;
    overflow: hidden;
  }
  .shimmer::before {
    content: "";
    position: absolute;
    inset: 0;
    width: 60%;
    background: linear-gradient(100deg, transparent, color-mix(in srgb, var(--text) 7%, transparent), transparent);
    animation: att-shimmer 1.3s var(--ease-calm) infinite;
  }
  @keyframes att-shimmer {
    from {
      transform: translateX(-100%);
    }
    to {
      transform: translateX(260%);
    }
  }
  :global(:root[data-motion="reduced"]) .shimmer::before {
    animation: none;
    opacity: 0;
  }
  .ph-ic {
    position: relative;
    color: var(--text-faint);
    opacity: 0.7;
  }
  .small {
    font-size: var(--fs-compact);
  }
  /* The error box says four short things, so it needs room and its own gutter. */
  .placeholder.gone {
    gap: var(--sp-1);
    padding: var(--sp-3) var(--sp-2);
    text-align: center;
  }
  .ph-name {
    font-size: var(--fs-compact);
    font-weight: 600;
    color: var(--text-muted);
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .note {
    max-width: 30ch;
    line-height: 1.4;
  }
  .retry {
    padding: 3px 12px;
    font-size: var(--fs-compact);
  }
</style>
