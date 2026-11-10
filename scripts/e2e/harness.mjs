// The end-to-end harness: real peer processes on a private local network, a
// headless Chromium driving their real UI, and the small API specs are written
// against. scripts/e2e/run.mjs owns the process lifetime; specs only ever see
// a World (see README.md for the API and the reasons behind its shape).
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import fs from "node:fs";
import net from "node:net";
import path from "node:path";

// ---------------------------------------------------------------- timing

// The suite runs on shared, loaded machines, where a gossip round that takes
// 300 ms on an idle laptop can take ten seconds. Every wait in the harness is a
// poll with a deadline, never a fixed sleep, and every deadline scales with
// E2E_TIMEOUT_SCALE so a slow box can be given more room without editing specs.
export const SCALE = Number(process.env.E2E_TIMEOUT_SCALE || 1) || 1;
export const scaled = (ms) => Math.round(ms * SCALE);
export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Polls fn until it returns something truthy and returns that value. Errors
// thrown by fn count as "not yet": the last one is quoted in the timeout error,
// which is usually the most useful line in a failure report.
// fatal(err) → true stops polling and rethrows at once.
export async function waitFor(fn, { timeout = 30000, interval = 250, message, fatal } = {}) {
  const deadline = Date.now() + scaled(timeout);
  let last, lastErr;
  for (;;) {
    try {
      last = await fn();
      if (last) return last;
      lastErr = undefined;
    } catch (e) {
      if (fatal?.(e)) throw e;
      lastErr = e;
    }
    if (Date.now() > deadline) break;
    await sleep(interval);
  }
  const why = lastErr ? `last error: ${lastErr.message}` : `last value: ${short(last)}`;
  throw new Error(`timed out after ${scaled(timeout)}ms waiting for ${message || fn.toString().slice(0, 120)} (${why})`);
}

function short(v) {
  let s;
  try {
    s = JSON.stringify(v);
  } catch {
    s = String(v);
  }
  return s === undefined ? String(v) : s.length > 300 ? s.slice(0, 300) + "…" : s;
}

// ---------------------------------------------------------------- registry

const registry = [];
let currentFile = null;

// test("name", async (t) => { ... }, { timeout }) — registers a test in the spec
// file being loaded. A spec file may instead `export default` one function.
export function test(name, fn, opts = {}) {
  registry.push({ file: currentFile, name, fn, timeout: opts.timeout });
}

export async function loadSpec(file) {
  currentFile = file;
  const before = registry.length;
  const mod = await import(new URL(`file://${file}`).href);
  if (registry.length === before && typeof mod.default === "function") {
    registry.push({ file, name: path.basename(file).replace(/\.spec\.mjs$/, ""), fn: mod.default });
  }
  currentFile = null;
  return registry.slice(before);
}

// ---------------------------------------------------------------- ports

export function portFree(port) {
  return new Promise((resolve) => {
    const s = net.createServer();
    s.once("error", () => resolve(false));
    s.listen(port, "127.0.0.1", () => s.close(() => resolve(true)));
  });
}

// ---------------------------------------------------------------- processes

// Every PID the harness starts, so the runner can kill exactly those (and
// nothing else) on success, failure, or Ctrl-C.
export const started = new Set();

export function spawnLogged(bin, args, { env, log, cwd }) {
  const out = fs.openSync(log, "a");
  const child = spawn(bin, args, {
    env: { ...process.env, ...env },
    cwd,
    stdio: ["ignore", out, out],
    // Its own process group, so a Ctrl-C in the terminal reaches the runner
    // alone; the runner then stops what it started, in order.
    detached: true,
  });
  fs.closeSync(out);
  // A missing binary surfaces as an exit with this error in the log, rather
  // than as an unhandled 'error' event that takes the runner down.
  child.on("error", (e) => fs.appendFileSync(log, `spawn failed: ${e.message}\n`));
  if (child.pid) started.add(child.pid);
  child.exited = new Promise((r) => child.once("exit", (code, sig) => (started.delete(child.pid), r({ code, sig }))));
  return child;
}

export async function stopChild(child, graceMs = 10000) {
  if (!child || child.exitCode !== null || child.signalCode !== null) return;
  try {
    process.kill(child.pid, "SIGTERM");
  } catch {
    return;
  }
  const t = setTimeout(() => {
    try {
      process.kill(child.pid, "SIGKILL");
    } catch {}
  }, graceMs);
  await child.exited;
  clearTimeout(t);
}

export function killAllSync() {
  for (const pid of started) {
    try {
      process.kill(pid, "SIGKILL");
    } catch {}
  }
}

// ---------------------------------------------------------------- rendezvous

export async function startRendezvous({ bin, port, dir }) {
  for (const p of [port, port + 1]) {
    if (!(await portFree(p))) throw new Error(`port ${p} is in use: pick another E2E_BASE_PORT`);
  }
  const log = path.join(dir, "rendezvous.log");
  const child = spawnLogged(bin, [], {
    log,
    cwd: dir,
    env: {
      PORT: String(port),
      CONCORD_GUEST_PORT: String(port + 1),
      // A fixed seed gives the rendezvous the same PeerID every run, which
      // keeps logs comparable between runs. Test-only key; it guards nothing.
      CONCORD_RELAY_SEED: "e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0e2e0",
    },
  });
  const id = await waitFor(
    () => {
      if (child.exitCode !== null) throw new Error(`rendezvous exited: ${fs.readFileSync(log, "utf8").slice(-500)}`);
      return /^PeerID: (\S+)/m.exec(fs.readFileSync(log, "utf8"))?.[1];
    },
    { timeout: 30000, message: "the rendezvous to print its PeerID" },
  );
  return { child, port, bootstrap: `/ip4/127.0.0.1/tcp/${port}/p2p/${id}`, log };
}

// ---------------------------------------------------------------- peers

const NAMES = ["Alice", "Bashir", "Chen", "Dana", "Emre"];

export class Peer {
  constructor(world, index, { name, port, passphrase }) {
    this.world = world;
    this.index = index;
    this.name = name;
    this.port = port;
    this.passphrase = passphrase;
    this.token = world.token;
    this.url = `http://127.0.0.1:${port}`;
    this.home = path.join(world.dir, `peer${index}-${name.toLowerCase()}`);
    this.log = `${this.home}.log`;
    this.child = null;
    this.fingerprint = null;
    this.apps = [];
  }

  toString() {
    return `${this.name} (peer ${this.index}, :${this.port})`;
  }

  // Starts the process (data kept from any previous run of this peer) and waits
  // until its HTTP server answers. Does not unlock: see unlock().
  async start() {
    if (this.running) return this;
    if (!(await portFree(this.port))) throw new Error(`port ${this.port} is in use: pick another E2E_BASE_PORT`);
    fs.mkdirSync(this.home, { recursive: true });
    fs.appendFileSync(this.log, `\n==== start ${new Date().toISOString()}\n`);
    this.child = spawnLogged(this.world.bin, [], {
      log: this.log,
      cwd: this.home,
      env: {
        CONCORD_HOME: this.home,
        CONCORD_WEB_ADDR: `127.0.0.1:${this.port}`,
        CONCORD_NO_OPEN: "1",
        CONCORD_DISABLE_MDNS: "1",
        CONCORD_BOOTSTRAP: this.world.bootstrap,
        CONCORD_API_TOKEN: this.token,
      },
    });
    await waitFor(
      async () => {
        if (!this.running) throw new Error(`${this} exited during start; see ${this.log}`);
        const r = await fetch(`${this.url}/`, { signal: AbortSignal.timeout(2000) });
        return r.ok;
      },
      { timeout: 30000, message: `${this} to serve HTTP` },
    );
    return this;
  }

  get running() {
    return !!this.child && this.child.exitCode === null && this.child.signalCode === null;
  }

  // Simulates the person closing the app: SIGTERM, data kept. Open pages on
  // this peer are closed too, since their event stream dies with the process.
  async stop() {
    for (const app of this.apps.splice(0)) await app.close();
    await stopChild(this.child);
    this.child = null;
  }

  async restart() {
    await this.stop();
    await this.start();
    return this.unlock();
  }

  // Login creates the identity on the first call against an empty home and
  // unlocks it on every later one. The display name is set once, on creation.
  async unlock() {
    const fresh = !this.fingerprint;
    await this.rpc("Login", this.passphrase);
    const id = await this.rpc("Identity");
    this.fingerprint = id.fingerprint;
    this.peerId = id.peerId;
    if (fresh && id.displayName !== this.name) await this.rpc("SetProfile", this.name, "", "", "", "");
    return this;
  }

  async rpc(method, ...args) {
    const r = await fetch(`${this.url}/rpc`, {
      method: "POST",
      headers: { Authorization: `Bearer ${this.token}`, "Content-Type": "application/json" },
      body: JSON.stringify({ method, args }),
      signal: AbortSignal.timeout(scaled(60000)),
    });
    if (!r.ok) throw new Error(`${this.name} rpc ${method}: HTTP ${r.status}`);
    const j = await r.json();
    if (j.error) throw new Error(`${this.name} rpc ${method}: ${j.error}`);
    return j.result;
  }

  // ---- messaging conveniences (RPC level)

  async messages(channelId) {
    return (await this.rpc("Messages", channelId)) || [];
  }

  // SendMessage, retried while the core refuses with "proposals pending":
  // right after a join or a new DM the group's key change is still being
  // committed, and the core rejects application messages until it is. That
  // is setup noise for a test about something else; a spec about the window
  // itself should call rpc("SendMessage", …) directly.
  async sendRaw(channelId, text, replyTo = "") {
    return waitFor(
      async () => {
        try {
          await this.rpc("SendMessage", channelId, text, replyTo, "");
          return true;
        } catch (e) {
          if (/proposals pending/.test(e.message)) throw e;
          throw Object.assign(e, { fatal: true });
        }
      },
      { timeout: 30000, interval: 500, message: `${this.name} to send ${JSON.stringify(text)}`, fatal: (e) => e.fatal },
    );
  }

  async send(channelId, text, replyTo = "") {
    await this.sendRaw(channelId, text, replyTo);
    return this.waitForMessage(channelId, text, { timeout: 15000 });
  }

  // Resolves with the stored message whose content is exactly `text` (or for
  // which `text` returns true when it is a function).
  async waitForMessage(channelId, text, { timeout = 45000, deleted } = {}) {
    const match = typeof text === "function" ? text : (m) => m.content === text;
    return waitFor(
      async () => (await this.messages(channelId)).find((m) => match(m) && (deleted === undefined || !!m.deleted === deleted)),
      { timeout, interval: 400, message: `${this.name} to have message ${typeof text === "function" ? "matching predicate" : JSON.stringify(text)}` },
    );
  }

  async guilds() {
    return (await this.rpc("Guilds")) || [];
  }

  // ---- event stream

  // A tap on the peer's SSE stream: { list, waitFor(name, pred?, opts), close() }.
  // Closed automatically at the end of the test.
  events() {
    const ac = new AbortController();
    const list = [];
    const tap = {
      list,
      close: () => ac.abort(),
      waitFor: (name, pred = () => true, opts = {}) =>
        waitFor(() => list.find((e) => e.name === name && pred(e.data)), {
          message: `${this.name} event ${name}`,
          ...opts,
        }),
    };
    const pump = async () => {
      const r = await fetch(`${this.url}/events?token=${this.token}`, { signal: ac.signal });
      const dec = new TextDecoder();
      let buf = "";
      for await (const chunk of r.body) {
        buf += dec.decode(chunk, { stream: true });
        let i;
        while ((i = buf.indexOf("\n\n")) >= 0) {
          const block = buf.slice(0, i);
          buf = buf.slice(i + 2);
          const name = /^event: (.*)$/m.exec(block)?.[1];
          const data = /^data: (.*)$/m.exec(block)?.[1];
          if (!name) continue;
          let parsed = data;
          try {
            parsed = JSON.parse(data);
          } catch {}
          list.push({ name, data: parsed, at: Date.now() });
        }
      }
    };
    tap.ready = pump().catch(() => {});
    this.world.cleanups.push(() => ac.abort());
    return tap;
  }

  // ---- browser

  async open(opts = {}) {
    const app = new App(this, opts);
    await app.launch();
    this.apps.push(app);
    return app;
  }
}

// ---------------------------------------------------------------- browser

// Init script for { watchMedia: true }: runs in the page, so it closes over
// nothing.
function watchPeerConnections() {
  const Orig = window.RTCPeerConnection;
  if (!Orig) return;
  window.__pcs = [];
  window.RTCPeerConnection = class extends Orig {
    constructor(...a) {
      super(...a);
      window.__pcs.push(this);
    }
  };
}

export const VIEWPORTS = {
  desktop: { width: 1440, height: 900 },
  laptop: { width: 1180, height: 720 },
  tablet: { width: 820, height: 1180 },
  phone: { width: 390, height: 844 },
  phoneSmall: { width: 360, height: 740 },
  phoneLandscape: { width: 844, height: 390 },
};

// One open page on one peer. `page` and `ctx` are plain Playwright objects for
// anything the helpers below do not cover.
export class App {
  constructor(peer, opts) {
    this.peer = peer;
    this.opts = opts;
    this.console = []; // every console line, for the failure report
    this.errors = []; // console errors + uncaught page errors
  }

  async launch() {
    const o = this.opts;
    const phone = o.mobile || o.viewport === "phone" || o.viewport === "phoneSmall";
    const vp = typeof o.viewport === "object" ? o.viewport : VIEWPORTS[o.viewport || (phone ? "phone" : "desktop")];
    const browser = await this.peer.world.browser();
    this.ctx = await browser.newContext({
      viewport: vp,
      hasTouch: !!phone,
      isMobile: !!phone,
      reducedMotion: o.reducedMotion ? "reduce" : "no-preference",
      colorScheme: o.colorScheme || "dark",
      permissions: ["microphone", "camera", "notifications", "clipboard-read", "clipboard-write"],
    });
    // Calls: every RTCPeerConnection the page makes is kept on window.__pcs,
    // so a spec can read getStats() and prove media moves, not just that a
    // tile was drawn. Installed before the app's first script runs.
    if (o.watchMedia) await this.ctx.addInitScript(watchPeerConnections);
    this.page = await this.ctx.newPage();
    this.page.setDefaultTimeout(scaled(20000));
    this.page.on("console", (m) => {
      const line = `[${m.type()}] ${m.text()}`;
      this.console.push(line);
      if (m.type() === "error") this.errors.push(line);
    });
    this.page.on("pageerror", (e) => {
      const line = `[pageerror] ${e.stack || e.message}`;
      this.console.push(line);
      this.errors.push(line);
    });
    if (o.unlock !== false) await this.unlock();
    return this;
  }

  // The app asks for the passphrase on every load, even when the peer process
  // is already unlocked, so every navigation goes through here.
  async unlock() {
    const { page, peer } = this;
    await page.goto(`${peer.url}/?t=${peer.token}`, { waitUntil: "domcontentloaded" });
    const pass = page.locator('input[placeholder="Passphrase"]');
    const shell = page.locator('nav[aria-label="Guilds"], nav[aria-label="Sections"]').first();
    await pass.or(shell).first().waitFor({ state: "attached", timeout: scaled(30000) });
    if (await pass.isVisible().catch(() => false)) {
      await pass.fill(peer.passphrase);
      await pass.press("Enter");
    }
    await shell.waitFor({ state: "attached", timeout: scaled(45000) });
    await this.closeModals();
    return this;
  }

  async reload() {
    return this.unlock();
  }

  async close() {
    await this.ctx?.close().catch(() => {});
    this.ctx = null;
  }

  get isPhone() {
    return (this.page.viewportSize()?.width || 1000) < 700;
  }

  // ---- locators

  composer() {
    return this.page.locator("textarea.draft").first();
  }

  // The visible body of the newest message row containing `text`. A `.msg`
  // row's own box is a degenerate sliver, so this points at its `.body`.
  message(text) {
    return this.page.locator(".msg[data-msg-id] .body", { hasText: text }).last();
  }

  // The whole `.msg` row (for data-msg-id, classes, or scoping) — do not use
  // its bounding box.
  messageRow(text) {
    return this.page.locator(".msg[data-msg-id]", { has: this.page.locator(".body", { hasText: text }) }).last();
  }

  dialog() {
    return this.page.locator('[role="dialog"]').last();
  }

  // ---- actions

  // Enter sends on desktop; on a phone Enter is a newline, as on any phone
  // keyboard, and the Send button is how a message goes.
  async send(text) {
    const c = this.composer();
    await c.click();
    await c.fill(text);
    if (this.isPhone) await this.page.getByRole("button", { name: "Send", exact: true }).click();
    else await c.press("Enter");
    await waitFor(async () => (await c.inputValue()) === "", { timeout: 10000, message: "the composer to clear after send" });
    return this.waitForMessage(text);
  }

  async waitForMessage(text, { timeout = 45000 } = {}) {
    await this.message(text).waitFor({ state: "visible", timeout: scaled(timeout) });
    return this.message(text);
  }

  async waitForNoMessage(text, { timeout = 45000 } = {}) {
    await this.message(text).waitFor({ state: "detached", timeout: scaled(timeout) });
  }

  // Hovers the row so its toolbar appears, then presses the toolbar button
  // with this accessible name ("Reply", "Edit", "Delete", "Pin", "More", …).
  // Returns the row pinned by its id: once an edit opens, the row has no
  // `.body` any more, so a text-based locator would stop matching it.
  async messageAction(text, label) {
    const id = await this.messageRow(text).getAttribute("data-msg-id");
    const row = this.page.locator(`.msg[data-msg-id="${id}"]`);
    await row.locator(".msg-main").first().hover();
    // Edit, Pin and Delete join the toolbar only while Shift is held; their
    // everyday home is the toolbar's ⋯ menu, which is where a reader goes.
    const bar = row.getByRole("toolbar", { name: "Message actions" });
    const button = bar.getByRole("button", { name: label, exact: true }).first();
    if (await button.isVisible()) {
      await button.click();
      return row;
    }
    await bar.getByRole("button", { name: "More", exact: true }).click();
    await this.page.getByRole("menuitem", { name: label, exact: true }).first().click();
    return row;
  }

  async editMessage(text, next) {
    const row = await this.messageAction(text, "Edit");
    const box = row.locator(".edit-wrap textarea").first();
    await box.fill(next);
    await box.press("Enter");
    await this.waitForMessage(next);
  }

  async deleteMessage(text) {
    await this.messageAction(text, "Delete");
    await this.dialog().getByRole("button", { name: "Delete", exact: true }).click();
    await this.dialog().waitFor({ state: "detached" }).catch(() => {});
  }

  async openGuild(name) {
    const pill = this.page.locator('nav[aria-label="Guilds"]').getByRole("button", { name, exact: true }).first();
    await pill.click();
    return this;
  }

  async openChannel(name, guild) {
    if (guild) await this.openGuild(guild);
    const re = new RegExp(`^\\s*#?\\s*${name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}(\\s|$)`);
    const row = this.page.locator(".channel").filter({ hasText: re }).first();
    await row.click();
    await waitFor(async () => (await this.composer().getAttribute("placeholder"))?.includes(name), {
      timeout: 15000,
      message: `the composer to target #${name}`,
    });
    return this;
  }

  // Opens settings (the gear on desktop, the "You" tab on a phone) and, when
  // given, switches to a section by its title in the settings rail
  // ("Account", "Your profile", "Appearance", "Notifications & sounds", …).
  async openSettings(section) {
    if (!(await this.page.locator('[role="dialog"]').count())) {
      const btn = this.isPhone
        ? this.page.getByRole("button", { name: "You — profile and settings" })
        : this.page.locator('button[aria-label="Settings"]').last();
      await btn.click();
      await this.dialog().waitFor({ state: "visible" });
    }
    if (section) await this.settingsSection(section);
    return this.dialog();
  }

  // Desktop: a section in the settings rail. Phone: there is no rail; the
  // settings sheet lists the sections as rows, each opening a panel with a
  // Back button, so this goes back to that list first.
  async settingsSection(title) {
    if (this.isPhone) {
      const dlg = this.dialog();
      const back = dlg.getByRole("button", { name: "Back", exact: true });
      for (let i = 0; i < 4 && (await back.isVisible().catch(() => false)); i++) {
        await back.click();
        await sleep(200);
      }
      const row = this.#phoneSectionRows().filter({ hasText: title }).first();
      await row.click();
      await back.waitFor({ state: "visible" });
      return this.dialog();
    }
    const rail = this.page.locator('nav[aria-label="Settings sections"]');
    await rail.waitFor({ state: "visible" });
    const item = rail.locator("button.r-item").filter({ hasText: title }).first();
    await item.click();
    await waitFor(async () => (await item.getAttribute("aria-current")) === "page", {
      timeout: 15000,
      message: `settings section "${title}" to open`,
    });
    return this.dialog();
  }

  // The section rows of the phone settings sheet are its navigable rows (the
  // ones with a chevron) outside the account page's own body (.desk-cols).
  #phoneSectionRows() {
    return this.dialog().locator(".row:has(.chev):not(.desk-cols .row)");
  }

  // The titles of every settings section, in order (the rail on desktop, the
  // section rows on a phone).
  async settingsSections() {
    if (this.isPhone) {
      const rows = this.#phoneSectionRows();
      await rows.first().waitFor({ state: "visible" });
      return rows.evaluateAll((els) => els.map((e) => e.querySelector(".title")?.textContent.trim()));
    }
    const items = this.page.locator('nav[aria-label="Settings sections"] button.r-item');
    await items.first().waitFor({ state: "visible" });
    return items.evaluateAll((els) => els.map((e) => (e.querySelector(".r-title")?.textContent || e.textContent).trim()));
  }

  // Messages → the direct-message list → the conversation with this name (the
  // other person's display name, or "Notes" for your own).
  async openDM(name) {
    await this.page.locator('nav[aria-label="Guilds"]').getByRole("button", { name: /^Messages/ }).click();
    await this.page.locator(".dm-item").filter({ has: this.page.locator(".dm-name", { hasText: name }) }).first().click();
    await waitFor(async () => !(await this.composer().isDisabled()), { timeout: 15000, message: `the DM with ${name} to open` });
    return this;
  }

  // Attaches files through the composer's file input (paths on disk).
  async attach(...files) {
    await this.page.locator('input[type="file"]').first().setInputFiles(files);
    return this;
  }

  // Phone only: from a conversation back to the channel list / DM list.
  async back() {
    await this.page.getByRole("button", { name: /^Back to (channels|messages)$/ }).click();
    return this;
  }

  // Toggles a reaction from the message toolbar's quick row ("👍", …).
  async react(text, emoji) {
    return this.messageAction(text, `React ${emoji}`);
  }

  // ---- calls (open the page with { watchMedia: true })

  // Bytes and frames received so far over every live peer connection.
  async inbound() {
    return this.page.evaluate(async () => {
      const t = { audio: 0, video: 0, frames: 0, connected: 0 };
      for (const pc of window.__pcs || []) {
        if (pc.connectionState === "closed") continue;
        if (pc.connectionState === "connected") t.connected++;
        (await pc.getStats()).forEach((r) => {
          if (r.type !== "inbound-rtp") return;
          if (r.kind === "audio") t.audio += r.bytesReceived || 0;
          else {
            t.video += r.bytesReceived || 0;
            t.frames += r.framesDecoded || 0;
          }
        });
      }
      return t;
    });
  }

  // Waits until `kind` ("audio" | "video") is arriving: received bytes grow
  // across two reads a second apart. Proves media flows, which a tile cannot.
  async waitForMedia(kind = "audio", { timeout = 45000 } = {}) {
    return waitFor(
      async () => {
        const a = await this.inbound();
        await sleep(1000);
        const b = await this.inbound();
        return b[kind] > a[kind] ? b : null;
      },
      { timeout: scaled(timeout), interval: 500, message: `${this.peer.name} to receive ${kind}` },
    );
  }

  // Escape until no dialog is open (bounded: some dialogs ask before closing).
  async closeModals() {
    for (let i = 0; i < 5; i++) {
      if (!(await this.page.locator('[role="dialog"]').count())) return;
      await this.page.keyboard.press("Escape");
      await sleep(150);
    }
  }

  // The guild switcher (the strip's last tile, "All guilds" — or "Create or
  // join a guild" with none yet), then one of its footer actions.
  async switcherAction(label) {
    await this.page
      .locator('nav[aria-label="Guilds"]')
      .getByRole("button", { name: /^(All guilds|Create or join a guild)/ })
      .first()
      .click();
    // Scoped to the switcher: a brand-new account's welcome offers the same words.
    await this.page.locator('[role="dialog"][aria-label="Guilds"]').getByRole("button", { name: label }).click();
    return this;
  }

  // Switcher → "Create a guild". Resolves once the new guild's tile is on the
  // strip (a new guild is opened, and the open guild is always on it).
  async createGuild(name) {
    await this.switcherAction("Create a guild");
    await this.dialog().getByLabel("Guild name").fill(name);
    await this.dialog().getByRole("button", { name: "Create", exact: true }).click();
    await this.page
      .locator('nav[aria-label="Guilds"]')
      .getByRole("button", { name, exact: true })
      .first()
      .waitFor({ state: "attached", timeout: scaled(30000) });
    return this;
  }

  // Switcher → "Join with an invite code", as a person does it.
  async joinGuild(code, guildName) {
    await this.switcherAction("Join with an invite code");
    const box = this.page.locator('textarea[placeholder="Paste invite code here…"]');
    await box.fill(code);
    await this.dialog().getByRole("button", { name: "Join", exact: true }).click();
    await box.waitFor({ state: "detached", timeout: scaled(60000) });
    if (guildName) {
      await this.page
        .locator('nav[aria-label="Guilds"]')
        .getByRole("button", { name: guildName, exact: true })
        .first()
        .waitFor({ state: "attached", timeout: scaled(30000) });
    }
    return this;
  }

  async screenshot(file) {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    await this.page.screenshot({ path: file });
    return file;
  }
}

// ---------------------------------------------------------------- world

// Everything one test owns: its peers, their data, the pages opened on them.
// Built fresh per test and torn down by the runner, so tests cannot leak state
// into each other.
export class World {
  constructor({ dir, bin, bootstrap, token, portBase, getBrowser }) {
    this.dir = dir;
    this.bin = bin;
    this.bootstrap = bootstrap;
    this.token = token;
    this.portBase = portBase;
    this.getBrowser = getBrowser;
    this.all = [];
    this.cleanups = [];
  }

  browser() {
    return this.getBrowser();
  }

  // n started and unlocked peers. opts.names overrides the default names;
  // opts.start === false returns them unstarted.
  async peers(n, opts = {}) {
    if (this.all.length + n > 5) throw new Error("at most 5 peers per test");
    const out = [];
    for (let k = 0; k < n; k++) {
      const index = this.all.length + 1;
      const name = opts.names?.[k] || NAMES[index - 1];
      const p = new Peer(this, index, { name, port: this.portBase + index - 1, passphrase: `e2e-pass-${name.toLowerCase()}` });
      this.all.push(p);
      out.push(p);
    }
    if (opts.start !== false) {
      // Started together so a five-peer test does not pay five boot times in
      // a row; unlocked together for the same reason.
      await Promise.all(out.map((p) => p.start()));
      await Promise.all(out.map((p) => p.unlock()));
    }
    return out;
  }

  async peer(opts = {}) {
    return (await this.peers(1, { ...opts, names: opts.name ? [opts.name] : undefined }))[0];
  }

  // A direct message between two peers who already share a guild (a DM
  // needs the other side's keys). a starts it over RPC and sends one greeting,
  // `"<a.name> says hi"`, since a DM nobody has written in may not surface on
  // the other side; resolves once b has the conversation and the greeting.
  // Returns { id, channelId, greeting }.
  async dm(a, b) {
    const g = await a.rpc("StartDM", b.fingerprint);
    const channelId = g.channels?.[0]?.id;
    if (!channelId) throw new Error(`StartDM returned no channel: ${short(g)}`);
    const greeting = `${a.name} says hi`;
    await a.sendRaw(channelId, greeting);
    await waitFor(async () => (await b.guilds()).some((x) => x.id === g.id), {
      timeout: 60000,
      interval: 500,
      message: `${b.name} to receive the DM from ${a.name}`,
    });
    await b.waitForMessage(channelId, greeting);
    return { id: g.id, channelId, greeting };
  }

  // A second device on `account`'s identity, linked the way a person links a
  // phone: a link offer on the existing device, redeemed on a fresh one. The
  // new device unlocks with the same passphrase. Counts against the five
  // peers a test may start.
  async linkDevice(account, { name = `${account.name}2` } = {}) {
    const [dev] = await this.peers(1, { names: [name], start: false });
    dev.passphrase = account.passphrase;
    dev.linkedTo = account;
    await dev.start();
    // Each retry asks for a fresh offer rather than reusing one a failed
    // attempt may have spent. Retries cover the new device not having found
    // the old one through the rendezvous yet.
    await waitFor(
      async () => {
        const code = await account.rpc("LinkOffer");
        await dev.rpc("RedeemLinkCode", code, dev.passphrase);
        return true;
      },
      { timeout: 90000, interval: 2000, message: `${name} to link to ${account.name}'s account` },
    );
    const id = await dev.rpc("Identity");
    dev.fingerprint = id.fingerprint;
    dev.peerId = id.peerId;
    return dev;
  }

  // Creates a guild on members[0] and joins every other member by invite code
  // over RPC, then waits until every member sees every other one in the member
  // list and every named channel exists on every peer. Returns
  // { id, name, code, channels: {name: id}, channel(name) }.
  async guild({ name = "E2E Guild", members, channels = [] } = {}) {
    if (!members?.length) members = this.all;
    const [owner, ...rest] = members;
    const g = await owner.rpc("CreateGuild", name);
    const ids = {};
    for (const c of channels) {
      const spec = typeof c === "string" ? { name: c } : c;
      const ch = await owner.rpc("CreateChannel", g.id, spec.name, spec.type || "text", "");
      ids[spec.name] = ch.id;
    }
    const code = await owner.rpc("InviteCode", g.id);
    for (const m of rest) await joinWithRetry(m, code);
    const guild = await this.settleGuild({ id: g.id, name, code }, members, Object.keys(ids));
    for (const c of guild.allChannels) ids[c.name] ??= c.id;
    guild.channels = ids;
    return guild;
  }

  // Waits until every member sees the guild, every other member, and the
  // named channels. Also used after a UI join.
  async settleGuild(guild, members, channelNames = []) {
    const fps = members.map((m) => m.fingerprint);
    for (const m of members) {
      await waitFor(
        async () => {
          const view = (await m.guilds()).find((x) => x.id === guild.id);
          if (!view) return false;
          const have = new Set((view.channels || []).map((c) => c.name));
          if (!channelNames.every((n) => have.has(n))) return false;
          const mem = new Set(((await m.rpc("Members", guild.id)) || []).map((x) => x.fingerprint));
          return fps.every((f) => mem.has(f)) && view;
        },
        { timeout: 90000, interval: 500, message: `${m.name} to see guild "${guild.name}" with all ${members.length} members` },
      );
    }
    const view = (await members[0].guilds()).find((x) => x.id === guild.id);
    guild.allChannels = view.channels || [];
    guild.channel = (n) => {
      const c = guild.allChannels.find((x) => x.name === n) || { id: guild.channels?.[n] };
      if (!c.id) throw new Error(`no channel "${n}" in ${guild.name}`);
      return c.id;
    };
    return guild;
  }

  async cleanup() {
    for (const f of this.cleanups.splice(0)) {
      try {
        await f();
      } catch {}
    }
    for (const p of this.all) for (const app of p.apps.splice(0)) await app.close();
    await Promise.all(this.all.map((p) => stopChild(p.child)));
  }
}

// A join needs the owner reachable through the rendezvous. Right after start
// the peers may not have found each other yet; that is a retryable "not yet",
// not the behaviour under test.
async function joinWithRetry(peer, code) {
  return waitFor(
    async () => {
      await peer.rpc("JoinViaInvite", code);
      return true;
    },
    { timeout: 90000, interval: 2000, message: `${peer.name} to join by invite code` },
  );
}

// Sends n numbered messages from peer to channel over RPC and waits until the
// sender has stored all of them. Returns the texts in order.
export async function seedMessages(peer, channelId, n, { prefix = `seed-${peer.name}` } = {}) {
  const texts = [];
  for (let i = 1; i <= n; i++) {
    const t = `${prefix} ${i}`;
    await peer.sendRaw(channelId, t);
    texts.push(t);
  }
  await waitFor(
    async () => {
      const have = new Set((await peer.messages(channelId)).map((m) => m.content));
      return texts.every((t) => have.has(t));
    },
    { timeout: 30000, message: `${peer.name} to store ${n} seeded messages` },
  );
  return texts;
}

export { assert };
