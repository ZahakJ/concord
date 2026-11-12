// Perceived-speed budgets, measured in the running UI against a real core:
// a channel switch shows its rows quickly, at full strength on the first frame
// they appear, and a revisit is painted from memory; a keystroke reaches the
// screen within a frame or two with a long history loaded; a long feed keeps
// its DOM bounded however far back you scroll; nothing animates or polls at
// idle; a dialog, the palette and a context menu arrive in 100 ms or less; and
// a busy guild in the background costs the page next to nothing per message.
//
// The budgets are generous for a loaded machine (and scale with
// E2E_TIMEOUT_SCALE) but each is set to fail on the regression it is named
// after: a fade or a skeleton wait between click and rows, a keystroke that
// re-renders the feed, an unbounded window, a loop left running, a slow
// entrance, the guild list rebuilt per message.
import { test, SCALE, sleep } from "../harness.mjs";

const pct = (a, p) => {
  const s = [...a].sort((x, y) => x - y);
  return s[Math.min(s.length - 1, Math.floor((p / 100) * s.length))];
};

// One frame-polled probe, installed once per page: ms from t0 to the first
// frame on which pred() holds.
async function installProbe(page) {
  await page.evaluate(() => {
    const raf = window.requestAnimationFrame.bind(window);
    window.__raf = raf;
    window.__waitFrame = (t0, pred, timeout = 20000) =>
      new Promise((res) => {
        const tick = () => {
          let ok = false;
          try {
            ok = pred();
          } catch {
            /* not yet */
          }
          const now = performance.now();
          if (ok) return res(now - t0);
          if (now - t0 > timeout) return res(-1);
          raf(tick);
        };
        raf(tick);
      });
    window.__rows = () => {
      const f = document.querySelector(".feed");
      if (!f) return [];
      const fr = f.getBoundingClientRect();
      return [...f.querySelectorAll("[data-msg-id]")]
        .filter((el) => {
          const r = el.getBoundingClientRect();
          return r.height > 0 && r.bottom > fr.top && r.top < fr.bottom;
        })
        .map((el) => el.dataset.msgId);
    };
  });
}

// Clicks a channel row and returns { content, opacity }: ms to the first frame
// with three rows that were not on screen before, and the feed's opacity on
// that frame.
async function timedSwitch(page, name) {
  const re = new RegExp(`^\\s*#?\\s*${name}(\\s|$)`);
  const row = await page.locator(".channel").filter({ hasText: re }).first().elementHandle();
  return page.evaluate(async (el) => {
    const before = new Set(window.__rows());
    let opacity = null;
    const t0 = performance.now();
    el.click();
    const content = await window.__waitFrame(t0, () => {
      const rows = window.__rows();
      const ok = rows.length >= 3 && rows.filter((x) => !before.has(x)).length >= 3;
      if (ok) {
        let o = 1;
        for (let n = document.querySelector(".feed"); n && n !== document.body; n = n.parentElement)
          o *= Number(getComputedStyle(n).opacity);
        opacity = o;
      }
      return ok;
    });
    return { content, opacity };
  }, row);
}

async function seedLong(peer, channelId, n) {
  for (let i = 1; i <= n; i += 8) {
    const batch = [];
    for (let k = i; k < Math.min(n + 1, i + 8); k++)
      batch.push(peer.sendRaw(channelId, `long ${String(k).padStart(5, "0")} the quick brown fox jumps over the lazy dog`));
    await Promise.all(batch);
  }
}

test(
  "switching, typing and scrolling stay inside their budgets on a 5k-message channel",
  async ({ world, assert, step, log, waitFor }) => {
    const [alice] = await world.peers(1);
    const g = await world.guild({ name: "Budget", members: [alice], channels: ["long", "short", "other"] });

    step("Seed 5000 messages into #long and a few dozen into the others");
    await seedLong(alice, g.channels.long, 5000);
    for (const c of ["short", "other"]) {
      for (let i = 0; i < 40; i++) await alice.sendRaw(g.channels[c], `${c} message ${i} with a little text`);
    }
    await waitFor(async () => (await alice.messages(g.channels.long)).some((m) => m.content.startsWith("long 05000")), {
      timeout: 120000,
      message: "the newest seeded message to be stored",
    });

    const a = await alice.open();
    await a.openChannel("other", "Budget");
    await installProbe(a.page);

    step("Channel switches: rows at full strength on their first frame, quickly; revisits from memory");
    const times = [];
    const revisits = [];
    const seq = ["long", "short", "other", "long", "short", "other", "long", "short", "other", "long", "short", "other", "long", "short", "other"];
    for (const [i, name] of seq.entries()) {
      const r = await timedSwitch(a.page, name);
      assert.ok(r.content > 0, `#${name} showed its rows`);
      assert.ok(r.opacity > 0.99, `#${name}'s rows were at full strength when they appeared (opacity ${r.opacity})`);
      times.push(r.content);
      if (i >= 3) revisits.push(r.content);
      await sleep(300);
    }
    log(`switch to content p50 ${pct(times, 50).toFixed(0)} ms, p95 ${pct(times, 95).toFixed(0)} ms; revisits p50 ${pct(revisits, 50).toFixed(0)} ms`);
    assert.ok(pct(times, 95) < 400 * SCALE, `channel switch p95 ${pct(times, 95).toFixed(0)} ms is over 400 ms`);
    assert.ok(pct(revisits, 50) < 250 * SCALE, `a revisit p50 ${pct(revisits, 50).toFixed(0)} ms is over 250 ms`);

    step("A long history stays a bounded DOM however far back you scroll");
    await a.openChannel("long");
    await sleep(800);
    for (let i = 0; i < 6; i++) {
      await a.page.evaluate(() => {
        const f = document.querySelector(".feed");
        f.scrollTop = 0;
        f.dispatchEvent(new Event("scroll"));
      });
      await sleep(900);
    }
    const dom = await a.page.evaluate(() => ({
      rows: document.querySelectorAll(".feed [data-msg-id]").length,
      nodes: document.getElementsByTagName("*").length,
    }));
    log(`after paging back: ${dom.rows} rows, ${dom.nodes} elements`);
    assert.ok(dom.rows <= 170, `${dom.rows} message rows mounted (the window caps at 160)`);
    assert.ok(dom.nodes < 9000, `${dom.nodes} elements in the document`);
    await a.page.evaluate(() => {
      const f = document.querySelector(".feed");
      f.scrollTop = f.scrollHeight;
    });
    await sleep(800);

    step("Typing with that history loaded: keydown to the next frame");
    await a.composer().click();
    await a.page.evaluate(() => {
      window.__keys = [];
      document.querySelector("textarea.draft").addEventListener(
        "keydown",
        (e) => {
          if (e.key.length !== 1) return;
          const t0 = e.timeStamp;
          window.__raf(() => {
            const mc = new MessageChannel();
            mc.port1.onmessage = () => window.__keys.push(performance.now() - t0);
            mc.port2.postMessage(0);
          });
        },
        true,
      );
    });
    for (const ch of "the quick brown fox jumps over the lazy dog") {
      await a.page.keyboard.type(ch);
      await sleep(50);
    }
    const keys = await a.page.evaluate(() => window.__keys);
    log(`keydown to paint p50 ${pct(keys, 50).toFixed(1)} ms, p95 ${pct(keys, 95).toFixed(1)} ms`);
    assert.ok(keys.length >= 30, "keystrokes were measured");
    assert.ok(pct(keys, 95) < 50 * SCALE, `typing p95 ${pct(keys, 95).toFixed(1)} ms is over 50 ms`);
    await a.page.keyboard.press("Control+a");
    await a.page.keyboard.press("Backspace");
  },
  { timeout: 15 * 60 * 1000 },
);

test("at idle nothing animates or polls, and overlays arrive within 100 ms", async ({ world, assert, step, log }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Quiet", members: [alice, bashir], channels: ["busy"] });
  const general = g.allChannels.find((c) => c.name !== "busy");
  for (let i = 0; i < 30; i++) await alice.sendRaw(general.id, `quiet message ${i}`);
  // A sidebar's worth of other guilds, so anything that rebuilds the guild
  // list per message has something to rebuild.
  for (let i = 0; i < 12; i++) {
    const og = await alice.rpc("CreateGuild", `Other ${i}`);
    for (let k = 0; k < 8; k++) await alice.rpc("CreateChannel", og.id, `room-${k}`, "text", "");
  }

  const a = await alice.open();
  await a.openChannel(general.name, "Quiet");
  await a.waitForMessage("quiet message 29");
  await sleep(3000);

  step("Five idle seconds: no animation-frame loop, few timers, no endless animation");
  const idle = await a.page.evaluate(async () => {
    const c = { raf: 0, to: 0, iv: 0 };
    const raf = window.requestAnimationFrame;
    const st = window.setTimeout;
    const si = window.setInterval;
    window.requestAnimationFrame = (cb) => raf((t) => (c.raf++, cb(t)));
    window.setTimeout = (f, ...r) => st(typeof f === "function" ? (...x) => (c.to++, f(...x)) : f, ...r);
    window.setInterval = (f, ...r) => si(typeof f === "function" ? (...x) => (c.iv++, f(...x)) : f, ...r);
    await new Promise((r) => st(r, 5000));
    window.requestAnimationFrame = raf;
    window.setTimeout = st;
    window.setInterval = si;
    const running = document
      .getAnimations()
      .filter((x) => x.playState === "running" && x.effect?.getTiming?.().iterations === Infinity)
      .map((x) => x.animationName || "?");
    return { ...c, running };
  });
  log(`idle 5 s: ${idle.raf} rAF callbacks, ${idle.to} timeouts, ${idle.iv} interval ticks; endless: ${idle.running.join(",") || "none"}`);
  assert.ok(idle.raf <= 10, `${idle.raf} animation-frame callbacks in 5 idle seconds: something is looping`);
  assert.ok(idle.to + idle.iv <= 15, `${idle.to + idle.iv} timer callbacks in 5 idle seconds`);
  assert.deepEqual(idle.running, [], "no endless animation runs at idle");

  step("The palette, settings and a context menu arrive in 100 ms or less");
  const entrance = async (open, sel) => {
    await open();
    await a.page.locator(sel).first().waitFor();
    const longest = await a.page.evaluate((sel) => {
      const root = document.querySelector(sel);
      const all = [root, ...root.querySelectorAll("*")];
      let ov = root.closest(".overlay") || root.parentElement;
      if (ov) all.push(ov);
      let max = 0;
      for (const el of all)
        for (const an of el.getAnimations?.() || []) {
          const t = an.effect?.getTiming?.();
          if (t && t.iterations !== Infinity) max = Math.max(max, Number(t.duration) + Number(t.delay || 0));
        }
      return max;
    }, sel);
    await a.page.keyboard.press("Escape");
    await sleep(400);
    await a.page.keyboard.press("Escape");
    await sleep(400);
    return longest;
  };
  const pal = await entrance(() => a.page.keyboard.press("Control+k"), '[role="dialog"][aria-label="Command palette"]');
  const set = await entrance(() => a.page.keyboard.press("Control+,"), '[role="dialog"]');
  const menu = await entrance(async () => {
    await a.message("quiet message 29").click({ button: "right" });
  }, '[role="menu"]');
  log(`entrances: palette ${pal} ms, settings ${set} ms, context menu ${menu} ms`);
  for (const [what, ms] of [["palette", pal], ["settings", set], ["context menu", menu]])
    assert.ok(ms <= 100, `the ${what}'s entrance runs ${ms} ms`);

});

test("a busy guild in the background costs the open page next to nothing per message", async ({ world, assert, step, log }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Quiet", members: [alice, bashir], channels: ["busy"] });
  const general = g.allChannels.find((c) => c.name !== "busy");
  for (let i = 0; i < 30; i++) await alice.sendRaw(general.id, `quiet message ${i}`);
  // A sidebar's worth of other guilds, so anything that rebuilds the guild
  // list or re-tallies every guild per message has something to rebuild.
  for (let i = 0; i < 12; i++) {
    const og = await alice.rpc("CreateGuild", `Other ${i}`);
    for (let k = 0; k < 8; k++) await alice.rpc("CreateChannel", og.id, `room-${k}`, "text", "");
  }
  const a = await alice.open();
  await a.openChannel(general.name, "Quiet");
  await a.waitForMessage("quiet message 29");
  await sleep(3000);
  step("A busy guild in the background costs the open page next to nothing per message");
  const cdp = await a.ctx.newCDPSession(a.page);
  await cdp.send("Performance.enable");
  const task = async () => (await cdp.send("Performance.getMetrics")).metrics.find((m) => m.name === "TaskDuration").value;
  const busy = g.channels.busy;
  // The page's own idle cost over the same span is subtracted, so this is
  // what the messages cost and nothing else.
  let t = performance.now();
  let before = await task();
  await sleep(4000);
  const idlePerMs = ((await task()) - before) / (performance.now() - t);
  t = performance.now();
  before = await task();
  const n = 40;
  for (let i = 0; i < n; i++) {
    await bashir.sendRaw(busy, `background ${i}`);
    await sleep(100);
  }
  await alice.waitForMessage(busy, `background ${n - 1}`);
  await sleep(1500);
  const spent = (await task()) - before - idlePerMs * (performance.now() - t);
  const perMsg = (spent * 1000) / n;
  log(`main-thread time per background message: ${perMsg.toFixed(2)} ms (idle ${(idlePerMs * 1e6).toFixed(1)} ms/s subtracted)`);
  assert.ok(perMsg < 7 * SCALE, `each background message cost the page ${perMsg.toFixed(2)} ms of main thread`);
});

// The desktop shell may composite in software, where every frame of an
// entrance, a hover ease or a backdrop blur is a repaint. Under its mark the
// strip's hover lands in one frame, overlays arrive at once and nothing blurs.
test("under the desktop shell's mark, hovers and entrances are instant and nothing blurs", async ({ world, assert }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Shell", members: [alice] });
  const a = await alice.open();
  await a.openChannel(g.allChannels[0].name, "Shell");
  await a.page.evaluate(() => (document.documentElement.dataset.shell = "desktop"));
  const tile = a.page.locator('nav[aria-label="Guilds"] .tile').nth(1);
  await tile.hover();
  const strip = await tile.evaluate((el) => {
    const durs = (x) => getComputedStyle(x).transitionDuration.split(",").map((d) => parseFloat(d));
    return { tile: durs(el), wrap: durs(el.parentElement), filter: getComputedStyle(el).filter };
  });
  assert.ok([...strip.tile, ...strip.wrap].every((d) => d === 0), `strip transitions: ${JSON.stringify(strip)}`);
  assert.equal(strip.filter, "none");
  await a.page.keyboard.press("Control+k");
  const pal = a.page.locator('[role="dialog"][aria-label="Command palette"]');
  await pal.waitFor();
  const longest = await pal.evaluate((el) =>
    Math.max(0, ...[el, el.parentElement, ...el.querySelectorAll("*")].flatMap((n) => n.getAnimations().map((x) => Number(x.effect.getTiming().duration)))),
  );
  assert.equal(longest, 0, "the palette arrives at once");
  const blurred = await a.page.evaluate(() =>
    [...document.querySelectorAll("*")].filter((n) => !["none", ""].includes(getComputedStyle(n).backdropFilter)).length,
  );
  assert.equal(blurred, 0, "no backdrop blur under the shell's mark");
});

// Landing on unread history reveals the NEW divider. That reveal used to mount
// the window's whole ceiling (160 rows) around it, which made the first frame
// of every switch into an unread channel cost several times a normal one, and
// the divider was scrolled into view a frame after the rows appeared.
test("landing on unread history mounts a screenful around the divider, already in view", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Unread", members: [alice, bashir], channels: ["busy", "quiet"] });
  for (let i = 0; i < 150; i++) await alice.sendRaw(g.channels.busy, `read ${i} a line that alice has already seen`);
  const a = await alice.open();
  await a.openChannel("busy", "Unread");
  await a.waitForMessage("read 149");
  await a.openChannel("quiet");
  step("Bashir writes 40 messages while Alice is elsewhere");
  for (let i = 0; i < 40; i++) await bashir.sendRaw(g.channels.busy, `unread ${i} from bashir`);
  await alice.waitForMessage(g.channels.busy, "unread 39 from bashir");
  await installProbe(a.page);
  const re = /^\s*#?\s*busy(\s|$)/;
  const row = await a.page.locator(".channel").filter({ hasText: re }).first().elementHandle();
  const first = await a.page.evaluate(async (el) => {
    const t0 = performance.now();
    el.click();
    let snap = null;
    await window.__waitFrame(t0, () => {
      const rows = window.__rows();
      if (rows.length < 3) return false;
      const f = document.querySelector(".feed").getBoundingClientRect();
      const d = document.querySelector(".new-divider")?.getBoundingClientRect();
      snap = { mounted: document.querySelectorAll(".feed [data-msg-id]").length, divider: !!d && d.bottom > f.top && d.top < f.bottom };
      return true;
    });
    return snap;
  }, row);
  assert.ok(first, "the channel showed rows");
  assert.ok(first.mounted <= 60, `${first.mounted} rows mounted on landing`);
  assert.ok(first.divider, "the NEW divider was in view on the first frame with rows");
});
