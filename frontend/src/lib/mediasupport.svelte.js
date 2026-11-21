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
// is about to make a media element awaits mediaReady() first and makes none
// on false; markup reads media.inline.
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
      (err) => {
        // A core that does not know the question has no shell check either:
        // play as before. Any other failure (the core briefly unreachable)
        // answers no for now and is asked again next time, because guessing
        // yes on the one engine where the answer matters is a crash.
        if (/unknown method/.test(String(err?.message || err))) {
          media.inline = true;
          return true;
        }
        ready = null;
        return false;
      },
    );
  }
  return ready;
}

// The one line shown where a player would have been. The shell asks once per
// run, so installing the package takes a restart to be noticed.
export const MEDIA_OUTSIDE_NOTE =
  "Plays outside Concord: this system is missing GStreamer's audio output (gst-plugins-good), and playing it here would crash the window. Install it, then restart Concord.";

// The same for a camera preview (QR scanner, device test).
export const MEDIA_CAMERA_NOTE =
  "The camera can't be shown here: this system is missing GStreamer's audio and video output (gst-plugins-good), and showing it would crash the window. Install it, then restart Concord.";
