// Coming back to a page the phone froze. iOS suspends a tab that leaves the
// screen and the socket under its event stream dies with it, usually without
// an error: the page came back still "connected", heard nothing more, and
// showed the chat as it was when you left until a reload. And a page whose
// render was lost while away came back as an all-black screen.
//
// The browser here cannot be frozen the way iOS does it, so the page is put
// into the same state by hand: its event stream is cut off silently (it keeps
// saying OPEN and delivers nothing, which is what a frozen socket looks like
// from inside), and its visibility is driven the way the OS would drive it.
import { test, waitFor, scaled } from "../harness.mjs";

// Runs in the page before the app's first script.
function freezeRig() {
  const Real = window.EventSource;
  window.__streams = [];
  window.EventSource = class extends Real {
    constructor(...a) {
      super(...a);
      this.__dead = false;
      window.__streams.push(this);
    }
    addEventListener(name, fn, o) {
      const self = this;
      super.addEventListener(name, function (e) {
        if (!self.__dead) return fn.call(this, e);
      }, o);
    }
    removeEventListener() {
      // Listeners are wrapped above; a removed one just stays muted with its
      // stream. Nothing here depends on removal.
    }
  };
  window.__killStreams = () => window.__streams.forEach((s) => (s.__dead = true));
  let hidden = false;
  Object.defineProperty(document, "hidden", { configurable: true, get: () => hidden });
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => (hidden ? "hidden" : "visible"),
  });
  window.__setHidden = (h) => {
    hidden = h;
    document.dispatchEvent(new Event("visibilitychange"));
  };
}

async function openFrozenable(peer, opts = {}) {
  const app = await peer.open({ ...opts, unlock: false });
  await app.ctx.addInitScript(freezeRig);
  await app.unlock();
  return app;
}

async function setup(world) {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Resume", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const a = await openFrozenable(alice, { viewport: "phone" });
  await a.openChannel(ch.name, "Resume");
  await a.settled();
  await bashir.send(ch.id, "before you left");
  await a.waitForMessage("before you left");
  return { alice, bashir, ch, a };
}

// Bashir writes while Alice's page is frozen; her core has it, her page not.
async function missWhileAway({ alice, bashir, ch, a }, text) {
  await a.page.evaluate(() => window.__killStreams());
  await bashir.send(ch.id, text);
  await alice.waitForMessage(ch.id, text);
  await a.page.waitForTimeout(scaled(1500));
  if (await a.message(text).count()) throw new Error("the rig did not cut the stream: the page heard it live");
}

test("back from another app, the chat shows what was said while away, and stays live", async ({ world, step, shot }) => {
  const s = await setup(world);
  const { bashir, ch, a } = s;

  step("Alice switches away; her stream dies silently; Bashir writes");
  await a.page.evaluate(() => window.__setHidden(true));
  await missWhileAway(s, "said while you were away");
  await a.page.waitForTimeout(3500); // longer than a flick between tabs

  step("Alice comes back: the message is there without a reload");
  await a.page.evaluate(() => window.__setHidden(false));
  await a.waitForMessage("said while you were away", { timeout: scaled(15000) });
  await a.waitForMessage("before you left");

  step("and the page is live again, not just caught up once");
  await bashir.send(ch.id, "and live again");
  await a.waitForMessage("and live again", { timeout: scaled(15000) });
  await shot(a, "alice after resume");
});

test("restored from the back/forward cache, the chat catches up", async ({ world, step }) => {
  const s = await setup(world);
  const { a } = s;
  await missWhileAway(s, "said before the page was restored");
  step("pageshow with persisted: every socket the page had is gone");
  await a.page.evaluate(() => window.dispatchEvent(new PageTransitionEvent("pageshow", { persisted: true })));
  await a.waitForMessage("said before the page was restored", { timeout: scaled(15000) });
});

test("a page that comes back with nothing rendered reloads instead of staying black", async ({ world, step }) => {
  const s = await setup(world);
  const { a } = s;
  step("the render is lost while away");
  await a.page.evaluate(() => {
    window.__setHidden(true);
    document.getElementById("app").replaceChildren();
  });
  step("back on screen: the app comes back by itself");
  const nav = a.page.waitForEvent("load", { timeout: scaled(15000) });
  await a.page.evaluate(() => window.__setHidden(false));
  await nav;
  const pass = a.page.locator('input[placeholder="Passphrase"]');
  const shell = a.page.locator('nav[aria-label="Guilds"], nav[aria-label="Sections"]').first();
  await waitFor(async () => (await pass.isVisible().catch(() => false)) || (await shell.count()) > 0, {
    timeout: scaled(30000),
    message: "nothing rendered after the reload",
  });
});
