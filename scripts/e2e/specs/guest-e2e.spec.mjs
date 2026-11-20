// A browser guest's leg is sealed end to end (docs/dev/guest-e2e.md): a host
// mints a meeting link, a guest in another browser opens it on the real
// gateway, they chat both ways and share a call, and nothing the guest's
// WebSocket carries in either direction contains the chat, the guest's name or
// the link's secret. A clear frame injected after the handshake is ignored.
// Revoking the link ends the guest's session and call at once, and the old
// link then fails. A link in the old format, or a page without WebCrypto,
// opens no socket at all.
import { test } from "../harness.mjs";

async function withGuestLinks(world, peer) {
  const port = Number(/\/tcp\/(\d+)\//.exec(world.bootstrap)[1]);
  peer.env.CONCORD_GUEST_BASE = `http://127.0.0.1:${port + 1}`;
  await peer.restart();
  return `http://127.0.0.1:${port + 1}`;
}

async function throughToMeeting(app, waitFor) {
  const ready = app.page.getByRole("button", { name: "Start the meeting", exact: true });
  const live = app.page.getByRole("dialog", { name: /Your meeting is live/ });
  await waitFor(async () => (await ready.isVisible()) || (await live.isVisible()), { timeout: 30000, message: "a meeting dialog" });
  if (await ready.isVisible()) await ready.click();
  await live.waitFor({ timeout: 30000 });
  return live;
}

// Installed in the guest's browser before the page's own script: every
// socket made, every frame sent and received, and every peer connection.
function guestTap() {
  window.__ws = { made: 0, tx: [], rx: [], last: null };
  const OWS = window.WebSocket;
  window.WebSocket = class extends OWS {
    constructor(...a) {
      super(...a);
      window.__ws.made++;
      window.__ws.last = this;
      this.addEventListener("message", (ev) => {
        if (ev.isTrusted) window.__ws.rx.push(String(ev.data));
      });
    }
    send(d) {
      window.__ws.tx.push(String(d));
      return super.send(d);
    }
  };
  window.__pcs = [];
  const OP = window.RTCPeerConnection;
  if (OP) {
    window.RTCPeerConnection = class extends OP {
      constructor(...a) {
        super(...a);
        window.__pcs.push(this);
      }
    };
  }
}

async function guestPage(world, { insecure = false } = {}) {
  const ctx = await (await world.browser()).newContext({ permissions: ["microphone", "camera"] });
  await ctx.addInitScript(guestTap);
  if (insecure) {
    await ctx.addInitScript(() => {
      Object.defineProperty(window, "isSecureContext", { value: false });
    });
  }
  const page = await ctx.newPage();
  return { ctx, page };
}

test("a guest's chat and call cross the gateway sealed, and revoking the link ends them", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1);
  const gateway = await withGuestLinks(world, alice);
  await world.guild({ name: "Studio", members: [alice] });
  const a = await alice.open({ viewport: "desktop", watchMedia: true });

  step("Alice starts an instant meeting and takes its guest link");
  await a.page.locator('nav[aria-label="Guilds"]').getByRole("button", { name: "Add a guild" }).click();
  await a.page.getByRole("menuitem", { name: /Instant meeting/ }).click();
  const live = await throughToMeeting(a, waitFor);
  const link = (await live.locator("code.link").innerText()).trim();
  assert.match(link, /\/guest#h=[^&]+&k=[A-Za-z0-9_-]{43}$/, "a v2 link with a 32-byte secret");
  assert.ok(link.startsWith(gateway), "the link is on the e2e gateway");
  const secret = /&k=([A-Za-z0-9_-]+)/.exec(link)[1];
  await a.closeModals();

  const meeting = await waitFor(async () => (await alice.guilds()).find((g) => g.kind === "meeting"), { message: "the meeting guild" });
  // A meeting has one channel that is both its chat and its call room.
  const textChannels = (meeting.channels.some((c) => c.type === "text")
    ? meeting.channels.filter((c) => c.type === "text")
    : meeting.channels
  ).map((c) => c.id);
  const guestSaid = (text) =>
    waitFor(async () => {
      for (const ch of textChannels) {
        const m = (await alice.messages(ch)).find((x) => String(x.content || "").includes(text));
        if (m) return m;
      }
      return null;
    }, { timeout: 30000, message: `Alice to receive ${JSON.stringify(text)}` });

  step("a guest opens the link in another browser and walks in");
  const NAME = "Zephyrine";
  const HOST_SAYS = "marker-host-says-quokka-7731";
  const GUEST_SAYS = "marker-guest-says-axolotl-4410";
  const AFTER_INJECT = "marker-guest-after-inject-9902";
  const g = await guestPage(world);
  await g.page.goto(link);
  await g.page.fill("#name", NAME);
  await g.page.click("#go");
  await g.page.locator("#room.on").waitFor({ timeout: 30000 });

  step("chat both ways");
  await alice.send(textChannels[0], HOST_SAYS);
  await g.page.locator("#feed").getByText(HOST_SAYS).waitFor({ timeout: 30000 });
  await g.page.fill("#msg", GUEST_SAYS);
  await g.page.press("#msg", "Enter");
  await guestSaid(GUEST_SAYS);

  step("a clear frame injected after the handshake is ignored");
  await g.page.evaluate(() => {
    const s = window.__ws.last;
    s.dispatchEvent(new MessageEvent("message", { data: '{"type":"msg","from":"Mallory","content":"INJECTED-CLEAR-FRAME"}\n' }));
    s.dispatchEvent(new MessageEvent("message", { data: '{"type":"end","code":"unreachable","reason":"INJECTED-END"}\n' }));
  });
  await g.page.fill("#msg", AFTER_INJECT);
  await g.page.press("#msg", "Enter");
  await guestSaid(AFTER_INJECT);
  assert.equal(await g.page.getByText("INJECTED", { exact: false }).count(), 0, "nothing injected is shown");
  assert.equal(await g.page.locator("#over.on").count(), 0, "the injected end did not end the meeting");

  step("the guest joins the call and Alice's app connects to it");
  await g.page.click("#callBtn");
  await waitFor(async () => (await a.inbound()).connected >= 1, { timeout: 45000, message: "Alice's peer connection to the guest" });
  await waitFor(async () => (await g.page.locator("#callStat").innerText()).includes("connected"), { timeout: 45000, message: "the guest's call to connect" });

  step("nothing on the guest's socket is readable");
  const frames = await g.page.evaluate(() => ({ tx: window.__ws.tx, rx: window.__ws.rx }));
  const all = [...frames.tx, ...frames.rx].join("\n");
  for (const needle of [HOST_SAYS, GUEST_SAYS, AFTER_INJECT, NAME, secret]) {
    assert.ok(!all.includes(needle), `the socket never carries ${JSON.stringify(needle)}`);
  }
  const lines = all.split("\n").filter((l) => l.trim()).map((l) => JSON.parse(l));
  const clear = lines.filter((f) => f.type !== "e").map((f) => f.type);
  assert.deepEqual(clear, ["hello", "chal"], "only hello and chal travel in clear");
  for (const f of lines.filter((x) => x.type === "e")) {
    const n = Buffer.from(f.c, "base64url").length;
    assert.equal((n - 16) % 256, 0, `a record of ${n} bytes is a 256-byte bucket plus the tag`);
  }
  assert.ok(lines.filter((x) => x.type === "e").length >= 6, "chat and signalling went through as records");

  step("Alice revokes the link from Meeting details");
  await a.page.locator(".voice-bar").getByRole("button", { name: "Open the call" }).click();
  await a.page.getByRole("button", { name: "Meeting", exact: true }).click();
  await a.page.getByRole("menuitem", { name: "Meeting details" }).click();
  const details = a.page.getByRole("dialog", { name: "Meeting details" });
  await details.getByRole("button", { name: "Revoke link" }).click();
  await details.getByRole("button", { name: "New link" }).waitFor();
  await shot(a, "link revoked");

  step("the guest is sent out and the call ends on both sides");
  await g.page.locator("#over.on").waitFor({ timeout: 30000 });
  await waitFor(async () => !(await g.page.locator("#stage.on").count()), { message: "the guest's call stage to close" });
  await waitFor(
    () => g.page.evaluate(() => window.__pcs.every((pc) => pc.connectionState === "closed")),
    { message: "the guest's peer connection to close" },
  );
  await waitFor(async () => (await a.inbound()).connected === 0, { timeout: 30000, message: "Alice's peer connection to the guest to close" });

  step("the old link no longer works");
  const again = await guestPage(world);
  await again.page.goto(link);
  await again.page.fill("#name", "Second try");
  await again.page.click("#go");
  await again.page.locator("#err", { hasText: "no longer works" }).waitFor({ timeout: 30000 });

  step("New link mints a different one");
  await details.getByRole("button", { name: "New link" }).click();
  const fresh = (await details.locator("code.link").innerText()).trim();
  assert.match(fresh, /\/guest#h=[^&]+&k=[A-Za-z0-9_-]{43}$/);
  assert.notEqual(fresh, link, "a fresh secret");

  await g.ctx.close();
  await again.ctx.close();
});

test("an old-format link and a page without WebCrypto open no socket", async ({ world, assert, step }) => {
  const port = Number(/\/tcp\/(\d+)\//.exec(world.bootstrap)[1]);
  const gateway = `http://127.0.0.1:${port + 1}`;
  const host = "12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTN";

  step("#t= is refused locally");
  const old = await guestPage(world);
  await old.page.goto(`${gateway}/guest#h=${host}&t=old-bearer-token-5521`);
  await old.page.getByText("This link no longer works").waitFor();
  assert.equal(await old.page.locator("#go").isVisible(), false, "no join button");
  assert.equal(await old.page.evaluate(() => window.__ws.made), 0, "no socket");

  step("an insecure context is refused locally");
  const k = Buffer.alloc(32, 7).toString("base64url");
  const bad = await guestPage(world, { insecure: true });
  await bad.page.goto(`${gateway}/guest#h=${host}&k=${k}`);
  await bad.page.getByText("This page can't be used here").waitFor();
  assert.equal(await bad.page.locator("#go").isVisible(), false, "no join button");
  assert.equal(await bad.page.evaluate(() => window.__ws.made), 0, "no socket");

  await old.ctx.close();
  await bad.ctx.close();
});
