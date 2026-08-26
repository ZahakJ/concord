// Handing the user a file, on platforms that disagree about what that means.
//
// On the desktop window and in a browser, an <a download> is the whole story
// and every export in the app already does it inline. In the Android WebView it
// is a silent no-op: nothing registers a DownloadListener, so a click on a
// blob: URL with a download attribute produces no file, no error and no clue.
// An export that quietly does nothing is worse than one that admits it can't,
// so on a phone this falls back to the clipboard — which does work there, is
// where a phone user is going to paste it anyway, and can be reported honestly.
//
// Returns "file", "clipboard", or "" — the caller has to say which happened,
// because "Saved" and "Copied" are different sentences.

function nativeShell() {
  const p = window.Capacitor?.getPlatform?.();
  return p === "android" || p === "ios";
}

export async function saveText(filename, text, mime = "application/json") {
  if (!nativeShell()) {
    try {
      const a = document.createElement("a");
      a.href = URL.createObjectURL(new Blob([text], { type: mime }));
      a.download = filename;
      a.click();
      URL.revokeObjectURL(a.href);
      return "file";
    } catch {
      /* fall through to the clipboard */
    }
  }
  try {
    await navigator.clipboard.writeText(text);
    return "clipboard";
  } catch {
    return "";
  }
}
