// Handing the user a file, on platforms that disagree about what that means.
//
// On the desktop window and in a browser, an `<a download>` is the whole story.
// In the Android WebView it is a SILENT no-op: nothing registers a
// DownloadListener, so a click on a blob: URL with a download attribute
// produces no file, no error and no clue. An export that quietly does nothing
// is worse than one that admits it can't — and better than either is one that
// works, which is what the native side does now:
//
//   a document → the system "save as" sheet (ACTION_CREATE_DOCUMENT). No
//                permission, and the user picks where it lands.
//   an image   → straight into the shared Pictures collection, where a picture
//                saved out of a chat belongs, and where the gallery finds it.
//
// See ConcordCorePlugin.saveFile / saveImage. Everything returns one of
// "file", "gallery", "clipboard" or "" — the caller has to say which happened,
// because "Saved", "Saved to your gallery" and "Copied" are different
// sentences, and "" (the user backed out, or nothing worked) is not a claim to
// make at all.

function nativeCore() {
  const p = window.Capacitor?.getPlatform?.();
  // iOS has no plugin methods for this yet; its WebView does honour a
  // download, so it keeps the web path.
  return p === "android" ? window.Capacitor?.Plugins?.ConcordCore : null;
}

// The base64 half of a data: URL, or of a plain string of bytes.
function toBase64(text) {
  // btoa refuses anything outside latin-1, which a message export is full of.
  const bytes = new TextEncoder().encode(text);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin);
}

async function viaAnchor(href, filename, revoke) {
  try {
    const a = document.createElement("a");
    a.href = href;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    return "file";
  } finally {
    if (revoke) URL.revokeObjectURL(href);
  }
}

export async function saveText(filename, text, mime = "application/json") {
  const core = nativeCore();
  if (core) {
    try {
      const r = await core.saveFile({ filename, mime, data: toBase64(text) });
      if (r?.saved) return "file";
      return ""; // the picker was dismissed — say nothing happened
    } catch {
      /* fall through to the clipboard, which does work there */
    }
    try {
      await navigator.clipboard.writeText(text);
      return "clipboard";
    } catch {
      return "";
    }
  }
  try {
    return await viaAnchor(URL.createObjectURL(new Blob([text], { type: mime })), filename, true);
  } catch {
    /* fall through */
  }
  try {
    await navigator.clipboard.writeText(text);
    return "clipboard";
  } catch {
    return "";
  }
}

// saveBlob is saveText's counterpart for bytes that are already a Blob — a
// decrypted attachment, an encrypted backup archive. Same contract.
export async function saveBlob(filename, blob) {
  const core = nativeCore();
  if (!core) {
    try {
      return await viaAnchor(URL.createObjectURL(blob), filename, true);
    } catch {
      return "";
    }
  }
  try {
    const data = await new Promise((resolve, reject) => {
      const fr = new FileReader();
      fr.onload = () => resolve(String(fr.result));
      fr.onerror = () => reject(fr.error);
      fr.readAsDataURL(blob);
    });
    const r = await core.saveFile({
      filename,
      mime: blob.type || "application/octet-stream",
      data,
    });
    return r?.saved ? "file" : "";
  } catch {
    return "";
  }
}

// saveImage hands over a picture from any src the page can already show it
// from: a data: URL, a blob: URL, or an http(s) one.
export async function saveImage(src, filename = "concord-image.png") {
  const core = nativeCore();
  if (!core) return viaAnchor(src, filename, false);
  try {
    const res = await fetch(src);
    const blob = await res.blob();
    const data = await new Promise((resolve, reject) => {
      const fr = new FileReader();
      fr.onload = () => resolve(String(fr.result));
      fr.onerror = () => reject(fr.error);
      fr.readAsDataURL(blob);
    });
    const r = await core.saveImage({
      filename,
      mime: blob.type || "image/png",
      data, // a whole data: URL; the plugin takes the part after the comma
    });
    return r?.saved ? "gallery" : "";
  } catch {
    return "";
  }
}
