package bridge

// MediaMissing is set by the Linux desktop shell to a check that names the
// system media elements the webview needs and cannot find. It stays nil in
// every other build, where the engine plays media on its own.
//
// It exists for one element. WebKitGTK plays every <audio> and <video> through
// GStreamer's autoaudiosink, and from WebKitGTK 2.54 a system without it
// (gst-plugins-good not installed) does not fail the element: the web process
// dereferences the sink it could not build and dies. A video scrolling into
// view, a voice message pressed, a call's audio arriving: each one crashed the
// window, and WebKit then reloaded the page into the same channel to crash it
// again. The page cannot ask the engine without making the element that kills
// it, so the shell asks GStreamer before the page loads and tells it.
var MediaMissing func() []string

// MediaSupportView tells the page whether it may make <audio>/<video>
// elements. Inline is false only when the shell found an element missing;
// Missing names them so the page can say what to install.
type MediaSupportView struct {
	Inline  bool     `json:"inline"`
	Missing []string `json:"missing,omitempty"`
}

// MediaSupport answers once per call; the check is cheap after the first one
// (the shell caches it).
func (b *Bridge) MediaSupport() MediaSupportView {
	if MediaMissing == nil {
		return MediaSupportView{Inline: true}
	}
	missing := MediaMissing()
	return MediaSupportView{Inline: len(missing) == 0, Missing: missing}
}
