//go:build wails

// Wails desktop front end. Built only with the `wails` tag (via `wails build`
// or `go build -tags wails .`) since it links the system WebView via cgo. The
// default build produces the browser-served variant in main_web.go, which needs
// no native dependencies.
package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	b := newBridge(context.Background())

	err := wails.Run(&options.App{
		Title:            "Concord",
		Width:            1120,
		Height:           720,
		MinWidth:         860,
		MinHeight:        560,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 30, G: 32, B: 36, A: 1},
		OnStartup: func(ctx context.Context) {
			b.setContext(ctx)
			b.onMessage = func(m MessageView) { wruntime.EventsEmit(ctx, "message", m) }
			b.onPresence = func() { wruntime.EventsEmit(ctx, "presence", nil) }
			b.onVoicePresence = func(v VoicePresence) { wruntime.EventsEmit(ctx, "voice-presence", v) }
			b.onVoiceSignal = func(v VoiceSignal) { wruntime.EventsEmit(ctx, "voice-signal", v) }
			b.onTyping = func(t TypingInfo) { wruntime.EventsEmit(ctx, "typing", t) }
			b.onGuildUpdate = func() { wruntime.EventsEmit(ctx, "guild-updated", nil) }
		},
		OnShutdown: func(context.Context) { b.close() },
		Bind:       []interface{}{b},
	})
	if err != nil {
		println("concord: " + err.Error())
	}
}
