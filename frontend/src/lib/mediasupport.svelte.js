// Whether this engine may be handed an <audio> or <video> element at all.
//
// On every browser and on most desktops the answer is yes and nobody asks. The
// exception is the Linux desktop build on a system whose GStreamer has no
// audio sink (gst-plugins-good not installed): WebKitGTK 2.54 builds every
// media element on that sink and, finding none, crashes the web process — and
// then reloads the page into the same channel to crash it again. The page
// cannot find this out by trying, so the desktop shell asks GStreamer at
// startup and the page asks the shell (internal/bridge/mediasupport.go).
//
// `media.inline` is null until the answer is in, then true or false. Code that
// is about to make a media element awaits mediaReady() first; markup reads
// media.inline. An error (a core too old to know the question) counts as yes,
// so a healthy engine never loses its players to a failed lookup.
import { api } from "./api.js";

export const media = $state({ inline: null, missing: [] });

let ready = null;

export function mediaReady() {
  if (!ready) {
    ready = api.mediaSupport().then(
      (v) => {
        media.inline = v?.inline !== false;
        media.missing = Array.isArray(v?.missing) ? v.missing : [];
        return media.inline;
      },
      () => {
        media.inline = true;
        return true;
      },
    );
  }
  return ready;
}

// The one line shown where a player would have been.
export const MEDIA_OUTSIDE_NOTE =
  "Plays outside Concord: this system is missing GStreamer's audio output (gst-plugins-good), and playing it here would crash the window.";
