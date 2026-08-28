//go:build wails && linux

package main

import "os"

// tuneRenderer decides, before the first GTK call, how WebKitGTK is allowed to
// get a finished frame from the web process onto the screen. It exists because
// the obvious answer — leave it alone — costs the whole GPU on the one setup
// where it matters most.
//
// Two separate things wear the name "DMA-BUF renderer" and conflating them is
// what made the first fix expensive:
//
//   - the RENDERER: the web process rasterises layers on the GPU;
//   - the TRANSPORT: those layers reach the UI process as dma-buf handles and
//     are handed to the compositor through zwp_linux_dmabuf.
//
// The Wayland protocol error is the TRANSPORT's. WEBKIT_DISABLE_DMABUF_RENDERER
// turns off both, which is why the app went from crashing to merely slow.
// WEBKIT_DMABUF_RENDERER_FORCE_SHM keeps the renderer and moves the finished
// buffer over wl_shm instead, which is one copy per frame and no dma-buf on the
// wire at all — so the protocol error cannot be raised by construction.
//
// Measured on the reporting machine (Hyprland + proprietary NVIDIA,
// webkit2gtk-4.1 2.52.5), same binary, same seeded 300-message channel, the
// same scripted boot → idle → scroll → animated-pack workload:
//
//	session   renderer            result          web-process GPU   CPU-seconds
//	wayland   disabled (before)   survives        none              45.6
//	wayland   enabled             Gdk Error 71    84 MiB (briefly)  —
//	wayland   enabled, no GBM     Gdk Error 71    84 MiB            —
//	x11       enabled             survives        20 MiB            —
//	wayland   enabled, FORCE_SHM  survives        222 MiB           7.0
//
// Six and a half times the CPU for the identical work, and the animated theme
// packs were the part that showed it: 40.8 fps with a 25 ms median frame under
// software rasterisation, a flat 60 with the renderer back.
//
// The rule, then:
//
//	the user set either variable  → do nothing, they have chosen
//	wayland + proprietary NVIDIA  → keep the renderer, force the SHM transport
//	wayland + anything else       → unchanged: disable it, as before
//	x11 / no session type         → unchanged: touch nothing
//
// Non-NVIDIA Wayland keeps the old blanket workaround deliberately. FORCE_SHM
// would very probably be right there too — it is the most conservative
// transport there is — but no such machine was available to prove it on, and a
// renderer default is not the place to guess.
func tuneRenderer() {
	// An explicit choice wins outright, including the empty-but-set case: a
	// user who exported either of these is asking for a specific path.
	if _, set := os.LookupEnv("WEBKIT_DISABLE_DMABUF_RENDERER"); set {
		return
	}
	if _, set := os.LookupEnv("WEBKIT_DMABUF_RENDERER_FORCE_SHM"); set {
		return
	}
	if os.Getenv("XDG_SESSION_TYPE") != "wayland" {
		return
	}
	if nvidiaProprietary() {
		os.Setenv("WEBKIT_DMABUF_RENDERER_FORCE_SHM", "1")
		return
	}
	os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
}

// nvidiaProprietary reports whether the proprietary NVIDIA kernel module is the
// one driving this session. /proc/driver/nvidia/version is created by that
// module and by nothing else — nouveau does not have it, and neither does a
// machine with an NVIDIA card whose proprietary driver failed to load, which is
// exactly the distinction that matters here. Reading a procfs file costs
// nothing and needs no GL context, so this runs before GTK is initialised
// rather than probing the renderer for a vendor string after the fact.
func nvidiaProprietary() bool {
	if _, err := os.Stat("/proc/driver/nvidia/version"); err == nil {
		return true
	}
	// Fallback for a kernel built without CONFIG_PROC_FS entries for it.
	_, err := os.Stat("/sys/module/nvidia_drm")
	return err == nil
}
