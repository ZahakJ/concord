// The message list renders a window of rows over a long history. These tests
// hold it to what a reader sees: the screen is never blank, scrolling up
// reaches the true first message, the reading position does not move when an
// older page lands above it or a new message arrives below it, a channel with
// a few enormous messages still fills the screen, dragging a window across the
// phone breakpoint does not leave stale row heights behind, and a search hit
// far back in history opens on that message.
import { test, sleep } from "../harness.mjs";

// Seeds n messages "<prefix> 0001".."<prefix> nnnn" from peer, a few at a time
// (the store orders by send time, so the first ten go one by one to keep the
// oldest message predictable), and waits until `reader` has the newest one.
async function seed(peer, reader, channelId, n, { prefix = "hist", every = null, waitFor } = {}) {
  const text = (i) => {
    let t = `${prefix} ${String(i).padStart(4, "0")} lorem ipsum`;
    if (every) t = every(i, t);
    return t;
  };
  for (let i = 1; i <= Math.min(10, n); i++) await peer.sendRaw(channelId, text(i));
  for (let i = 11; i <= n; i += 4) {
    const batch = [];
    for (let k = i; k < Math.min(n + 1, i + 4); k++) batch.push(peer.sendRaw(channelId, text(k)));
    await Promise.all(batch);
  }
  await waitFor(async () => (await reader.messages(channelId)).some((m) => m.content === text(n)), {
    timeout: 120000,
    message: `${reader.name} to receive the newest of ${n} messages`,
  });
  return text;
}

// In-page probes: how much of the feed's viewport has a row under it, which
// row is under a fixed point, and every older-page request with the row that
// was under that point at the moment it was asked for.
async function installProbes(page) {
  await page.evaluate(() => {
    const feed = () => document.querySelector(".feed");
    window.__probe = () => {
      const f = feed();
      const fr = f.getBoundingClientRect();
      const el = document.elementFromPoint(fr.left + 200, fr.top + 150)?.closest("[data-msg-id]");
      return el ? { id: el.dataset.msgId, top: Math.round(el.getBoundingClientRect().top) } : null;
    };
    window.__find = (id) => {
      const el = document.querySelector(`[data-msg-id="${id}"]`);
      return el ? Math.round(el.getBoundingClientRect().top) : null;
    };
    window.__cover = () => {
      const f = feed();
      const r = f.getBoundingClientRect();
      let hit = 0;
      let n = 0;
      for (let y = r.top + 15; y < r.bottom - 10; y += 30) {
        n++;
        const el = document.elementFromPoint(r.left + Math.min(220, r.width / 2), y);
        const row = el?.closest?.(
          "[data-msg-id], .day-divider, .system-msg, .feed-start, .new-divider, .older-loading, .pmsg, .empty, .feed-error",
        );
        if (row && f.contains(row)) hit++;
      }
      return { hit, n, st: Math.round(f.scrollTop), sh: f.scrollHeight, nodes: document.getElementsByTagName("*").length };
    };
    window.__older = [];
    window.__pending = 0;
    const of = window.fetch;
    window.fetch = async (...a) => {
      const older = String(a[1]?.body || "").includes('"MessagesBefore"');
      if (older) {
        window.__pending++;
        window.__older.push(window.__probe());
      }
      try {
        return await of(...a);
      } finally {
        if (older) window.__pending--;
      }
    };
    window.__lt = [];
    new PerformanceObserver((l) => {
      for (const e of l.getEntries()) window.__lt.push(e.duration);
    }).observe({ type: "longtask" });
  });
}

const cover = (page) => page.evaluate(() => window.__cover());

test(
  "a 2,100-message channel scrolls to its first message without a blank screen or a jump",
  async ({ world, assert, step, waitFor, shot, log }) => {
    const [alice, bashir] = await world.peers(2);
    const g = await world.guild({ name: "Long", members: [alice, bashir], channels: ["history"] });
    const ch = g.channels.history;
    step("seed 2,100 messages");
    await seed(alice, bashir, ch, 2100, { waitFor });

    const b = await bashir.open();
    await b.openChannel("history", "Long");
    await b.waitForMessage("hist 2100");
    await installProbes(b.page);
    const box = await b.page.locator(".feed").boundingBox();
    await b.page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);

    step("scroll up gently through three older pages: each lands without moving the reader");
    let blank = 0;
    let maxNodes = 0;
    let seen = 0;
    const checkScreen = async () => {
      const c = await cover(b.page);
      if (c.hit === 0) {
        blank++;
        await shot(b, `blank ${blank}`);
      }
      maxNodes = Math.max(maxNodes, c.nodes);
      return c;
    };
    for (let i = 0; i < 1500 && seen < 3; i++) {
      await b.page.mouse.wheel(0, -120);
      await sleep(40);
      await checkScreen();
      const older = await b.page.evaluate(() => window.__older.length);
      if (older > seen) {
        // The row that was under the probe point when the page was asked for
        // must still be there, to the pixel, once it has landed.
        await waitFor(async () => !(await b.page.evaluate(() => window.__pending)), { timeout: 15000 });
        await sleep(400);
        const at = await b.page.evaluate((k) => window.__older[k], older - 1);
        if (at) {
          const now = await b.page.evaluate((id) => window.__find(id), at.id);
          assert.ok(now !== null && Math.abs(now - at.top) <= 3, `page ${older} moved the reader: ${at.top} -> ${now}`);
        }
        seen = older;
      }
    }
    assert.equal(seen, 3, "three older pages did not load");

    step("fling up to the beginning, checking the screen at every step");
    let reached = false;
    for (let i = 0; i < 2000 && !reached; i++) {
      await b.page.mouse.wheel(0, -600);
      await sleep(40);
      const c = await checkScreen();
      reached = c.st < 5 && (await b.page.locator(".feed-start").count()) > 0;
    }
    seen = await b.page.evaluate(() => window.__older.length);
    const lt = await b.page.evaluate(() => window.__lt);
    log(`older pages: ${seen}, max DOM nodes: ${maxNodes}, long tasks: ${lt.length} (max ${Math.round(Math.max(0, ...lt))} ms)`);
    await shot(b, "the beginning");
    assert.ok(reached, "never reached the beginning of the channel");
    assert.equal(blank, 0, "the viewport was blank");
    assert.ok(seen >= 10, `expected ten older pages, saw ${seen}`);
    assert.ok(maxNodes < 5000, `the window rendered ${maxNodes} DOM nodes`);
    await b.waitForMessage("hist 0001");

    step("a new message arriving while scrolled up leaves the reader where they are");
    for (let i = 0; i < 20; i++) await b.page.mouse.wheel(0, 900);
    await sleep(800);
    const at = await b.page.evaluate(() => window.__probe());
    await alice.send(ch, "arrived while you were reading");
    await bashir.waitForMessage(ch, "arrived while you were reading");
    await sleep(800);
    const now = await b.page.evaluate((id) => window.__find(id), at.id);
    assert.ok(now !== null && Math.abs(now - at.top) <= 3, `an arrival moved the reader: ${at.top} -> ${now}`);
    await b.page.locator(".new-below").waitFor();
  },
  { timeout: 900000 },
);

test("a channel ending in very tall messages still fills the screen", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Tall", members: [alice, bashir], channels: ["tall"] });
  const ch = g.channels.tall;
  for (let i = 1; i <= 10; i++) await alice.sendRaw(ch, `short ${i}`);
  const table =
    "| " + Array.from({ length: 30 }, (_, i) => `c${i}`).join(" | ") + " |\n|" + "---|".repeat(30) + "\n| " +
    Array.from({ length: 30 }, (_, i) => `v${i}`).join(" | ") + " |";
  for (const t of [
    "Four thousand. " + "lorem ipsum dolor sit amet ".repeat(150),
    "Twenty thousand. " + "consectetur adipiscing elit sed do ".repeat(570),
    Array.from({ length: 500 }, (_, i) => `line ${i + 1}`).join("\n"),
    "x".repeat(600),
    "https://example.com/" + "a/b/c/".repeat(40),
    "```js\nconst a = 1;\nconsole.log(a);\n```",
    table,
  ])
    await alice.send(ch, t);
  await bashir.waitForMessage(ch, (m) => m.content === table);

  step("open the channel on the receiving side, and again after a reload");
  const b = await bashir.open();
  for (const pass of ["open", "reload"]) {
    if (pass === "reload") await b.reload();
    await b.openChannel("tall", "Tall");
    await b.waitForMessage("v29");
    await installProbes(b.page);
    const c = await waitFor(async () => {
      const x = await cover(b.page);
      return x.hit >= x.n - 2 ? x : null;
    }, { timeout: 5000, message: `the feed to fill the screen after ${pass}` }).catch(async (e) => {
      await shot(b, `blank after ${pass}`);
      throw e;
    });
    assert.ok(c.hit >= c.n - 2);
    assert.ok((await b.page.locator(".feed .msg[data-msg-id]").count()) > 1, "only one row rendered");
  }
});

test("dragging a window across the phone breakpoint keeps the feed's heights true", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Widths", members: [alice, bashir], channels: ["widths"] });
  const ch = g.channels.widths;
  await seed(alice, bashir, ch, 150, { prefix: "w", every: (i, t) => (i % 9 ? t : `${t} ${"wrapping words ".repeat(30)}`), waitFor });
  const b = await bashir.open({ viewport: { width: 1300, height: 800 } });
  await b.openChannel("widths", "Widths");
  await b.waitForMessage("w 0150");
  await installProbes(b.page);

  step("narrow to 320px, then widen 40px at a time back to 1300px");
  await b.page.setViewportSize({ width: 320, height: 800 });
  await sleep(500);
  for (let w = 360; w <= 1300; w += 40) {
    await b.page.setViewportSize({ width: w, height: 800 });
    await sleep(200);
    const c = await cover(b.page);
    assert.ok(c.hit > 0, `blank feed at ${w}px`);
  }
  await sleep(1500);
  const swept = await cover(b.page);
  await b.page.locator(".feed").evaluate((f) => f.scrollTo(0, f.scrollHeight));
  await sleep(500);

  step("compare with a fresh load at the same width");
  await b.reload();
  await b.openChannel("widths", "Widths");
  await b.waitForMessage("w 0150");
  await installProbes(b.page);
  await sleep(1500);
  const fresh = await cover(b.page);
  // The window only knows the rows it has measured, so the two are never equal;
  // what must not happen is the sweep's leftovers doubling the feed.
  assert.ok(
    swept.sh < fresh.sh * 1.3,
    `after the sweep the feed is ${swept.sh}px tall, a fresh load ${fresh.sh}px — stale heights from the narrow widths`,
  );
});

test("a search hit far back in history opens on that message", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Finds", members: [alice, bashir], channels: ["archive"] });
  const ch = g.channels.archive;
  await seed(alice, bashir, ch, 900, { prefix: "old", every: (i, t) => (i === 20 ? "NEEDLE-EARLY the oldest needle" : t), waitFor });
  const b = await bashir.open();
  await b.openChannel("general", "Finds");

  step("search, and click the hit");
  const q = b.page.getByPlaceholder(/^Search/).first();
  await q.click();
  await q.fill("NEEDLE-EARLY");
  await q.press("Enter");
  await b.page.locator(".sp-hit").first().click();

  const landed = await waitFor(
    () =>
      b.page.evaluate(() => {
        const el = [...document.querySelectorAll(".feed .msg[data-msg-id]")].find((e) => e.innerText.includes("NEEDLE-EARLY"));
        if (!el) return null;
        const f = document.querySelector(".feed").getBoundingClientRect();
        const body = el.querySelector(".body").getBoundingClientRect();
        return body.top >= f.top && body.bottom <= f.bottom ? { flash: el.classList.contains("flash-highlight") } : null;
      }),
    { timeout: 20000, message: "the feed to land on the old message" },
  );
  await shot(b, "landed on the hit");
  assert.ok(landed);
  assert.equal(await b.page.getByText("That message isn't loaded yet").count(), 0);
});
