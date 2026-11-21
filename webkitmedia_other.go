//go:build wails && !linux

package main

// armWebviewMedia is a WebKitGTK concern. WebView2 and WKWebView both route
// capture permission through the OS, which has already asked.
func armWebviewMedia() {}

// armMediaCheck is a WebKitGTK concern too: only its GStreamer backend crashes
// on a media element it has no audio sink for.
func armMediaCheck() {}
