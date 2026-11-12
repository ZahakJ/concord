// A search hit opened from inside a forum post lands in the hit's conversation
// whole: the header, the feed's rows and the composer all belong to it, on
// every frame. Jumping to another guild's conversation (Notes) used to switch
// the guild, then wait on its member list before switching the channel, and
// for as long as that fetch took the header said Notes while the feed still
// held the post's rows and the composer read "Select a channel". A history
// load that answers after the reader has moved on must not land either.
import { test } from "../harness.mjs";

// Every frame from now on: what the header names, which rows the feed holds,
// and what the composer says. Read back with takeFrames.
const startSampling = (page) =>
  page.evaluate(() => {
    window.__frames = [];
    window.__sampling = true;
    const tick = () => {
      if (!window.__sampling) return;
      const names = [...document.querySelectorAll(".chat-head .row.title strong")];
      window.__frames.push({
        header: (names[names.length - 1]?.textContent || "").trim(),
        ids: [...document.querySelectorAll(".feedwrap .msg[data-msg-id]")].map((el) => el.dataset.msgId),
        composer: document.querySelector(".feedwrap textarea.draft")?.getAttribute("placeholder") || "",
      });
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
const takeFrames = (page) =>
  page.evaluate(() => {
    window.__sampling = false;
    return window.__frames;
  });

test("a search jump out of an open forum post never shows the post under the new header", async ({ world, assert, step, waitFor, sleep, shot }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Lanterns", members: [alice], channels: [{ name: "patterns", type: "forum" }] });
  const general = g.channel("general");
  const post = await alice.rpc("CreateThread", g.id, g.channel("patterns"), "Cube lantern", "Four panels and a lantern base.", []);
  for (let i = 1; i <= 4; i++) await alice.send(post.id, `lantern step ${i}: score and fold`);
  await alice.send(general, "the lantern frame is glued");
  const notes = await alice.rpc("NotesDM");
  const notesCh = notes.channels[0].id;
  await alice.send(notesCh, "poster: lantern front");
  const notesName = (await alice.guilds()).find((x) => x.id === notes.id)?.name || "Notes";

  const ids = async (ch) => new Set((await alice.messages(ch)).map((m) => m.id));
  const convo = {
    "Cube lantern": { rows: await ids(post.id) },
    general: { rows: await ids(general), composer: /general/ },
    [notesName]: { rows: await ids(notesCh), composer: /note to yourself/i },
  };
  const targets = [
    { name: notesName, hit: "poster: lantern front" },
    { name: "general", hit: "the lantern frame is glued" },
  ];

  const a = await alice.open();
  const { page } = a;
  const header = () => page.evaluate(() => {
    const names = [...document.querySelectorAll(".chat-head .row.title strong")];
    return (names[names.length - 1]?.textContent || "").trim();
  });

  // Delays one kind of RPC call by `ms` while `on` holds for its arguments.
  let delay = null;
  await page.route(/\/rpc$/, async (route) => {
    let body = null;
    try {
      body = route.request().postDataJSON();
    } catch {
      /* not JSON: not ours */
    }
    const d = delay;
    if (d && body?.method === d.method && d.on(body.args || [])) {
      d.hits = (d.hits || 0) + 1;
      await sleep(d.ms);
    }
    await route.continue().catch(() => {});
  });

  async function openPost({ wait = true } = {}) {
    if ((await header()) === "Cube lantern") return;
    if ((await header()) !== "patterns" && (await header()) !== "general") await a.openGuild("Lanterns");
    await page.locator(".channel").filter({ hasText: /patterns/ }).first().click();
    await page.locator(".card").filter({ hasText: "Cube lantern" }).first().click();
    await waitFor(async () => (await header()) === "Cube lantern" && (await page.locator(".feedwrap.aspanel").count()) > 0, { message: "the post is open as a panel" });
    if (wait) await a.waitForMessage("lantern step 4");
  }

  async function jump(target) {
    const box = page.locator('input[aria-label="Search messages across all conversations"]').first();
    await box.click();
    await box.fill("lantern");
    await page.keyboard.press("Enter");
    const hit = page.locator(".search-panel .sp-hit").filter({ hasText: target.hit }).first();
    await hit.waitFor({ state: "visible" });
    await startSampling(page);
    await hit.click();
    await waitFor(async () => (await header()) === target.name && (await a.message(target.hit).isVisible().catch(() => false)), {
      message: `landed in ${target.name} on "${target.hit}"`,
    });
  }

  function check(frames, target, run) {
    for (const [i, f] of frames.entries()) {
      const c = convo[f.header];
      if (!c) continue; // a header in between (the board, nothing yet)
      const foreign = f.ids.filter((id) => !c.rows.has(id));
      assert.equal(foreign.length, 0, `${run}, frame ${i}: the header says ${f.header} but the feed holds ${foreign.length} rows from elsewhere`);
      assert.notEqual(f.composer, "Select a channel", `${run}, frame ${i}: the header says ${f.header} but the composer has no channel`);
    }
    const last = frames[frames.length - 1];
    assert.equal(last.header, target.name, `${run}: the header ends on the target`);
    if (convo[target.name].composer) assert.match(last.composer, convo[target.name].composer, `${run}: the composer is the target's`);
  }

  step("the target's member list answers slowly: the gap the old switch left open");
  await openPost();
  delay = { method: "Members", on: (args) => args[0] === notes.id, ms: 2500 };
  await jump(targets[0]);
  assert.ok(delay.hits > 0, "the Members fetch for Notes was the one delayed");
  delay = null;
  check(await takeFrames(page), targets[0], "slow member list");
  await shot(a, "landed in notes");

  step("the post's own history answers after the reader has left it");
  delay = { method: "Messages", on: (args) => args[0] === post.id, ms: 3000 };
  await openPost({ wait: false });
  await jump(targets[1]);
  // Past the delayed answer, so it has had its chance to land.
  await waitFor(() => delay.hits > 0, { message: "the post's history fetch was made" });
  await sleep(3500);
  delay = null;
  check(await takeFrames(page), targets[1], "late post history");

  for (let run = 1; run <= 15; run++) {
    const target = targets[run % 2 === 1 ? 0 : 1];
    step(`jump ${run}: from the open post to ${target.name}`);
    await openPost();
    await jump(target);
    await sleep(300);
    check(await takeFrames(page), target, `jump ${run}`);
  }
});
