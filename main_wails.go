//go:build wails

// Wails desktop front end. Built only with the `wails` tag (via `wails build`
// or `go build -tags wails .`) since it links the system WebView via cgo. The
// default build produces the browser-served variant in main_web.go, which needs
// no native dependencies.
//
// The frontend talks to the backend over the SAME HTTP surface as the web build
// (POST /rpc, SSE /events), mounted on the Wails asset server via Middleware.
// We deliberately do NOT rely on Wails' injected JS bindings (window.go.*): its
// binding generator deadlocks on newer Go toolchains, and -skipbindings (needed
// to build at all) means those bindings never appear — so the HTTP transport is
// what makes the desktop app actually work.
package main

import (
	"context"
	"embed"
	"net/http"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	appsvc "github.com/zahak/concord/internal/app"
	"github.com/zahak/concord/internal/bridge"
	"github.com/zahak/concord/internal/httpapi"
)

//go:embed all:frontend/dist
var assets embed.FS

// appIcon is embedded so the window/taskbar shows the Concorde logo even when
// building with a direct `go build -tags wails` (which bypasses the Wails CLI's
// icon injection). Windows takes its exe icon from build/versioninfo.json.
//
//go:embed build/appicon.png
var appIcon []byte

func main() {
	// Windows: behave like a real installed app no matter where the exe was
	// launched from — self-install to %LOCALAPPDATA%\Concord, register
	// shortcuts + Add/Remove, and hand over to the installed copy.
	if ensureInstalled() {
		return
	}
	// Native build: the update check must only ever offer desktop assets.
	bridge.NativeBuild = true
	// Clear the parked binary from a completed self-update, if any.
	bridge.CleanupOldBinary()
	b := bridge.New(context.Background())
	// trusted=true: the webview is native and unreachable from other origins, so
	// the /rpc CSRF guard isn't needed (and would reject the wails:// origin).
	api := httpapi.NewWith(b, true)

	err := wails.Run(&options.App{
		Title:     "Concord",
		Width:     1120,
		Height:    720,
		MinWidth:  860,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
			// Serve /rpc and /events ourselves; everything else falls through to
			// the embedded frontend assets.
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/rpc":
						api.HandleRPC(w, r)
					case "/events":
						api.HandleEvents(w, r)
					default:
						next.ServeHTTP(w, r)
					}
				})
			},
		},
		BackgroundColour: &options.RGBA{R: 30, G: 32, B: 36, A: 1},
		Linux:            &linux.Options{Icon: appIcon},
		OnStartup: func(ctx context.Context) {
			b.SetContext(ctx)
			// Events flow over the native Wails runtime (window.runtime.EventsOn),
			// which is injected even with -skipbindings — unlike the Go method
			// bindings (window.go.*), which aren't, hence RPC over HTTP.
			b.OnMessage = func(m bridge.MessageView) { wruntime.EventsEmit(ctx, "message", m) }
			b.OnPresence = func() { wruntime.EventsEmit(ctx, "presence", nil) }
			b.OnVoicePresence = func(v bridge.VoicePresence) { wruntime.EventsEmit(ctx, "voice-presence", v) }
			b.OnVoiceSignal = func(v bridge.VoiceSignal) { wruntime.EventsEmit(ctx, "voice-signal", v) }
			b.OnTyping = func(t bridge.TypingInfo) { wruntime.EventsEmit(ctx, "typing", t) }
			b.OnGuildUpdate = func() { wruntime.EventsEmit(ctx, "guild-updated", nil) }
			b.OnGuildInvite = func(inv appsvc.GuildInvite) { wruntime.EventsEmit(ctx, "guild-invite", inv) }
			b.OnReadState = func(r bridge.ReadStateView) { wruntime.EventsEmit(ctx, "read-state", r) }
			b.OnStory = func(u bridge.StoryUpdate) { wruntime.EventsEmit(ctx, "story", u) }
		},
		OnShutdown: func(context.Context) { b.Close() },
	})
	if err != nil {
		println("concord: " + err.Error())
	}
}
