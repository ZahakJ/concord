<script>
  // The Media tab's side of the viewer: it fetches the bytes of the item it
  // is given and hands the shared viewer (MediaViewer.svelte) the picture or
  // the clip, with "Go to message", Save, Copy for a photo, and Previous /
  // Next through what the gallery has loaded. It opens out of the item's tile
  // when the tile is on screen.
  import MediaViewer from "./MediaViewer.svelte";
  import { api } from "./lib/api.js";
  import { loadAttachment, copyImageToClipboard, saveImageSrc, unavailableNote } from "./lib/attachments.js";
  import { KIND_VIDEO, itemKey, noteDuration, unsignedItem } from "./lib/gallery.js";
  import { UNSIGNED_LABEL, UNSIGNED_TIP } from "./lib/unsigned.js";
  import { whenLabel } from "./lib/viewerset.js";
  import { S, flash, openContextMenu, nameFor, fmtClock } from "./lib/state.svelte.js";
  import { humanError } from "./lib/errors.js";
  import { saveBlob } from "./lib/savefile.js";
  import { media, mediaReady, MEDIA_OUTSIDE_NOTE } from "./lib/mediasupport.svelte.js";

  let { item, index = 0, count = 1, onClose, onGoto, onStep, onLoaded = () => {} } = $props();

  const video = $derived(item.kind === KIND_VIDEO);
  mediaReady();
  const unsigned = $derived(unsignedItem(item, S.identity.fingerprint));
  let src = $state("");
  let err = $state("");
  // Spoilers stay covered in the viewer until asked, once per opening.
  let revealed = $state(new Set());

  // A different item (Previous / Next) loads its own bytes; a stale answer
  // for the one before is dropped.
  let want = "";
  $effect(() => {
    const key = `${item.messageId}:${item.ord}`;
    if (key === want) return;
    want = key;
    src = "";
    err = "";
    const p = video
      ? api.fetchFile(item.channelId, item.blobId, item.keys, item.mime)
      : loadAttachment(item.channelId, { blobId: item.blobId, keys: item.keys, subtype: item.subtype });
    p.then(
      (s) => {
        if (want !== key) return;
        src = s;
        onLoaded(item);
      },
      (e) => {
        if (want === key) err = String(e?.message || e);
      },
    );
  });

  const fileName = () => item.name || `concord-${item.blobId.slice(0, 8)}${video ? "" : `.${item.subtype || "png"}`}`;
  async function save() {
    if (!src) return;
    if (video) {
      const how = await saveBlob(fileName(), await (await fetch(src)).blob());
      if (!how) flash("Couldn't save that video");
      return;
    }
    const how = await saveImageSrc(src, fileName());
    if (how === "gallery") flash("Saved to your gallery", "success");
    else if (!how) flash("Couldn't save that image");
  }
  async function copy() {
    try {
      await copyImageToClipboard(src);
      flash("Image copied", "success");
    } catch (e) {
      flash(`Couldn't copy image: ${humanError(e)}`, "error");
    }
  }
  function menu(e) {
    openContextMenu(e, [
      { label: "Go to message", icon: "bubble", onClick: onGoto },
      src && { label: "Save", icon: "download", onClick: save },
      src && !video && { label: "Copy image", icon: "copy", onClick: copy },
    ]);
  }

  const key = $derived(itemKey(item));
  const viewItem = $derived({
    key,
    kind: video ? "video" : "image",
    src,
    err: err ? unavailableNote(err) : "",
    note: video && media.inline === false ? MEDIA_OUTSIDE_NOTE : "",
    w: item.w,
    h: item.h,
    name: item.name,
    desc: item.desc,
    size: item.size,
    sender: nameFor(item.sender, item.senderName),
    when: item.sent ? whenLabel(item.sent, fmtClock) : "",
    spoiler: !!item.spoiler && !video && !revealed.has(key),
    unsigned,
  });
  // The tile this item is drawn in, while the grid has it mounted.
  const origin = () => document.querySelector(`[data-testid="gallery-scroll"] .tile[data-k="${CSS.escape(key)}"]`);
  // Clip length for the tile, as the feed's player notes it.
  $effect(() => {
    if (!video || !src) return;
    const v = document.querySelector(".mv video");
    if (!v) return;
    const note = () => noteDuration(item.blobId, v.duration);
    v.addEventListener("loadedmetadata", note, { once: true });
    return () => v.removeEventListener("loadedmetadata", note);
  });
</script>

<MediaViewer
  item={viewItem}
  {index}
  {count}
  {origin}
  onStep={count > 1 ? onStep : null}
  {onClose}
  onGoto={onGoto}
  onSave={save}
  onCopy={video ? null : copy}
  onMenu={menu}
  onReveal={() => (revealed = new Set([...revealed, key]))}
  unsignedLabel={UNSIGNED_LABEL}
  unsignedTip={UNSIGNED_TIP}
/>
