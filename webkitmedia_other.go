//go:build wails && !linux

package main

// armWebviewMedia is a WebKitGTK concern. WebView2 and WKWebView both route
// capture permission through the OS, which has already asked.
func armWebviewMedia() {}
