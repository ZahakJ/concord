//go:build wails && !linux

package main

// tuneRenderer is a Linux-only concern: Windows uses WebView2 and macOS uses
// WKWebView, neither of which has a DMA-BUF path to choose between.
func tuneRenderer() {}
