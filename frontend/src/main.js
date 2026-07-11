import { mount } from "svelte";
import "./app.css";
import App from "./App.svelte";
import { configureTransport } from "./lib/api.js";

// On mobile (Capacitor), the Go core runs in-process behind a loopback port
// with a bearer token; the native ConcordCore plugin boots it and hands both
// over. Desktop/browser mounts immediately — the page origin IS the API.
async function boot() {
  const cap = typeof window !== "undefined" ? window.Capacitor : null;
  if (cap?.Plugins?.ConcordCore) {
    const { port, token } = await cap.Plugins.ConcordCore.start();
    configureTransport({ baseURL: `http://127.0.0.1:${port}`, authToken: token });
  }
  return mount(App, { target: document.getElementById("app") });
}

const app = boot();

export default app;
