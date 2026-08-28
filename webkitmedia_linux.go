//go:build wails && linux

package main

/*
#cgo webkit2_41 pkg-config: webkit2gtk-4.1 gtk+-3.0
#cgo !webkit2_41 pkg-config: webkit2gtk-4.0 gtk+-3.0
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

// The whole of this lives in C rather than half of it living in Go behind
// //export. cgo forbids definitions in the preamble of a file that exports
// anything, which would mean a companion .c file — and a .c file in package
// main is a compile error for the DEFAULT build, which has no cgo at all. The
// policy being expressed is two lines long; it does not need a trip through Go.

// concord_permission answers WebKit's permission-request signal.
//
// ONLY capture is granted, and only because the page doing the asking is this
// binary's own embedded bundle on its own scheme — a webview with exactly one
// origin, which cannot navigate to anybody else's. Every other class of request
// WebKit can raise (geolocation, notifications, protected media, clipboard
// reads, device info) is denied HERE rather than left to WebKit's default, so
// the answer is written down instead of inherited from whatever a future
// version decides a default should be.
static gboolean concord_permission(WebKitWebView *view, WebKitPermissionRequest *req, gpointer user) {
  (void)view;
  (void)user;
  if (WEBKIT_IS_USER_MEDIA_PERMISSION_REQUEST(req)) {
    webkit_permission_request_allow(req);
  } else {
    webkit_permission_request_deny(req);
  }
  return TRUE; // handled; do not fall through to the default deny
}

static void concord_arm_view(WebKitWebView *view) {
  WebKitSettings *s = webkit_web_view_get_settings(view);
  // Without this, navigator.mediaDevices.getUserMedia does not EXIST —
  // WebKitGTK ships enable-media-stream FALSE — so the page's first question
  // about a microphone throws a TypeError rather than being denied.
  webkit_settings_set_enable_media_stream(s, TRUE);
  webkit_settings_set_enable_mediasource(s, TRUE);
#if WEBKIT_CHECK_VERSION(2, 38, 0)
  webkit_settings_set_enable_webrtc(s, TRUE);
#endif
  g_signal_connect(view, "permission-request", G_CALLBACK(concord_permission), NULL);
}

static WebKitWebView *concord_find(GtkWidget *w);
static void concord_child(GtkWidget *child, gpointer out) {
  WebKitWebView **found = (WebKitWebView **)out;
  if (*found != NULL) return;
  *found = concord_find(child);
}
static WebKitWebView *concord_find(GtkWidget *w) {
  if (w == NULL) return NULL;
  if (WEBKIT_IS_WEB_VIEW(w)) return WEBKIT_WEB_VIEW(w);
  if (!GTK_IS_CONTAINER(w)) return NULL;
  WebKitWebView *found = NULL;
  gtk_container_forall(GTK_CONTAINER(w), concord_child, &found);
  return found;
}

// concord_arm_tick runs on the GTK main loop until it finds the webview.
//
// Wails keeps the WebKitWebView pointer unexported inside its internal/
// package, so there is no way to be handed it; GTK's own toplevel list is a
// stable public API and does not depend on Wails' widget hierarchy keeping the
// shape it has today. The window is built synchronously inside NewFrontend,
// before OnStartup is dispatched, so in practice the first tick finds it — the
// retry is for the case where it does not, and it gives up rather than
// spinning for the life of the process.
static int concord_tries = 0;
static gboolean concord_arm_tick(gpointer data) {
  (void)data;
  GList *tops = gtk_window_list_toplevels();
  WebKitWebView *view = NULL;
  for (GList *l = tops; l != NULL && view == NULL; l = l->next) {
    view = concord_find(GTK_WIDGET(l->data));
  }
  g_list_free(tops);
  if (view != NULL) {
    concord_arm_view(view);
    return FALSE;
  }
  return (++concord_tries < 100) ? TRUE : FALSE; // ~5s, then stop asking
}

static void concord_arm_media(void) {
  // Everything above touches GTK, so it has to happen on the main loop's
  // thread. OnStartup is dispatched from a goroutine by Wails' Linux frontend,
  // which is emphatically not it.
  //
  // A HIGH-priority idle rather than a timeout, so this lands as early in the
  // loop as it can be made to. Wails packs the webview and calls LoadIndex
  // before gtk_main(), so the first idle callback of the loop runs after the
  // view exists and before the page it was pointed at has committed — and a
  // setting that decides which constructors a document gets has to be in place
  // by then. `enable-media-stream` is read when getUserMedia is called and
  // would survive a late arrival; nothing else here would.
  g_idle_add_full(G_PRIORITY_HIGH, concord_arm_tick, NULL, NULL);
}
*/
import "C"

// armWebviewMedia teaches the embedded WebKitWebView that this app may use a
// microphone and a camera.
//
// WebKitGTK requires two opt-ins from an embedder and Wails v2.13 provides
// neither: `WebKitSettings:enable-media-stream` defaults to FALSE, and a
// `permission-request` with no handler connected is auto-denied with no prompt
// and no message. Between them the desktop build could not reach a microphone
// on any Linux machine, however healthy the hardware — which is precisely what
// "the mic is not detected on Linux desktop" was. There is no Wails option for
// either; `linux.Options` carries an icon, a translucency flag, messages, a GPU
// policy and a program name, and nothing else.
//
// MEASURED on the reporting machine (webkit2gtk-4.1 2.52.5, PipeWire, two USB
// capture devices). Before: `enumerateDevices` returned ONE anonymous
// `audioinput` with no label and getUserMedia was refused without a prompt.
// After: the real devices come back named — "K66 Analog Stereo", "Generic USB
// Audio at usb-0000:0a:00.0-6" — and getUserMedia hands back a live track from
// the first of them.
//
// NECESSARY, NOT SUFFICIENT, and the rest is not ours to fix. WebKitGTK's media
// stack is GStreamer, and on that machine `RTCPeerConnection` is still
// undefined with `enable-webrtc` set and read back TRUE (checked, along with
// every WebKitFeature carrying "RTC" in its name — none of them gates it). The
// missing piece is on the system: no `gst-plugins-good` means no
// `autoaudiosink` to play a call through and no `rtpmanager`; no
// `gst-plugins-bad` means no `webrtcbin`, `dtls` or `srtp`, and WebKit does not
// expose a peer connection it has no pipeline for. lib/devices.js reports that
// as its own condition rather than as a microphone problem, because telling
// somebody to check their microphone permissions when the app cannot make a
// call at all is the wrong instruction twice over.
func armWebviewMedia() { C.concord_arm_media() }
