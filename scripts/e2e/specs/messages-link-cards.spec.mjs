// Link cards: a link's title and picture are fetched by the SENDER, once, when
// the message is sent, and travel inside it. These tests prove that:
//
//   - the reader's page reaches no host but its own core and asks it to fetch
//     nothing;
//   - a link-only message shows only its card, and a link in a sentence keeps
//     the sentence with the URL printed as its domain;
//   - the card, a YouTube box and the card's whole row offer one "Copy link"
//     and "Open link" with the real domain, on right-click and long-press, and
//     "Copy message link" separately;
//   - a hand-typed [paypal.com](https://evil.example) cannot wear the name;
//   - a page posing as YouTube is drawn as a page with its domain in full on
//     a phone;
//   - with the default ("ask"), the first link goes out bare and raises one
//     question that says what Yes does; Yes sends cards from then on, Not now
//     keeps links bare;
//   - Settings › Privacy holds the same choice: its line says undecided, on or
//     off, and setting it there answers the question for good, both ways;
//   - the composer fetches nothing while a link is typed, and once at send;
//   - a message sent after a link is never overtaken and never waits;
//   - a send the core never answers does not hold the channel;
//   - a reload while a link waits for its card loses nothing and sends
//     nothing twice;
//   - editing a card message shows the link, not the card's lines.
//
// No test fetches from the internet: the core's LinkCard answer is stubbed at
// the page's /rpc. LINKCARD_SHOTS=<dir> saves the captures the review asked for.
import { readFileSync } from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

const ROOT = path.resolve(path.dirname(new URL(import.meta.url).pathname), "../../..");
const SHOTS = process.env.LINKCARD_SHOTS || "";
const YT = "https://www.youtube.com/watch?v=dQw4w9WgXcQ";
const EVIL = "never-gonna-give-you-up-official-video-remastered.example";
const DESKTOP = { width: 1280, height: 900 };
const b64url = (s) => Buffer.from(s, "utf8").toString("base64url");

async function shot(page, name) {
  if (!SHOTS) return;
  await page.waitForTimeout(250);
  await page.screenshot({ path: path.join(SHOTS, `${name}.png`) });
}

// Seals a real picture as a 480x270 JPEG through the ordinary attachment path
// (in a scratch channel) and returns the [blobId, keys] a card token needs.
async function sealThumb(peer, app, scratchId, file) {
  const png = readFileSync(path.join(ROOT, file)).toString("base64");
  const dataUrl = await app.page.evaluate(async (b64) => {
    const img = new Image();
    img.src = `data:image/png;base64,${b64}`;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = 480;
    c.height = 270;
    const s = Math.max(480 / img.width, 270 / img.height);
    c.getContext("2d").drawImage(img, (480 - img.width * s) / 2, (270 - img.height * s) / 2, img.width * s, img.height * s);
    return c.toDataURL("image/jpeg", 0.82);
  }, png);
  const blobId = await peer.rpc("SendAttachment", scratchId, dataUrl, 480, 270, "", false, "", "");
  const msg = (await peer.messages(scratchId)).find((m) => (m.content || "").includes(blobId));
  const m = /concord:\/\/attach\/v1\/([0-9a-f]{64})\/([A-Za-z0-9_-]{75})\//.exec(msg.content);
  return [m[1], m[2]];
}
const thumbLine = ([blob, keys], desc) => `![image](concord://attach/v2/${blob}/${keys}/jpeg/480x270/2//${b64url(desc)})`;

// Every request the page makes, and every core method it calls.
function watchTraffic(page) {
  const hosts = new Set();
  const methods = [];
  page.on("request", (r) => {
    const u = new URL(r.url());
    if (u.protocol.startsWith("http")) hosts.add(u.hostname);
    if (u.pathname === "/rpc") {
      try {
        methods.push(JSON.parse(r.postData() || "{}").method);
      } catch {
        /* not JSON */
      }
    }
  });
  return { hosts, methods };
}
const FETCHES = /^(LinkCard|LinkPreview|PrepareLinkPreviews)$/;

async function seed(world) {
  const [alice, bashir] = await world.peers(2, { names: ["Alice", "Bashir"] });
  const g = await world.guild({ name: "Cards", members: [alice, bashir], channels: ["scratch"] });
  const general = g.allChannels.find((c) => c.name !== "scratch");
  const scratch = g.allChannels.find((c) => c.name === "scratch");
  const a = await alice.open();
  const ytThumb = await sealThumb(alice, a, scratch.id, "docs/media/feature-graphic.png");
  const pageThumb = await sealThumb(alice, a, scratch.id, "docs/media/social-preview.png");
  await a.close();
  // What the sender's core writes for a YouTube link: title, channel, no length
  // (YouTube's oEmbed has none), and the domain after the title for v0.60.
  const ytCard = `[Never Gonna Give You Up \\(Official Video\\)](${YT}) · youtube.com\n${thumbLine(ytThumb, "Rick Astley")}`;
  return { alice, bashir, g, general, scratch, ytThumb, pageThumb, ytCard };
}

async function seedFeed(world) {
  const s = await seed(world);
  const { alice, general, ytCard, pageThumb } = s;
  await alice.send(general.id, ytCard);
  await alice.send(general.id, `this one is a classic ${YT} honestly\n${ytCard}`);
  await alice.send(general.id, `[Concord: chat that stays yours](https://concord.example.org/) · example.org\n${thumbLine(pageThumb, "Concord")}`);
  await alice.send(general.id, "log in here: [paypal.com](https://evil.example/login)");
  await alice.send(general.id, "the docs: [Read the manual](https://evil.example/manual)");
  await alice.send(general.id, "an older client sent this https://youtu.be/dQw4w9WgXcQ");
  // What a hostile page (or a hand-made token) can say about itself: a long
  // channel name with youtube.com in it, a length, and a picture of a video.
  // Its domain is long, as a phisher's often is: wider than a phone's card.
  await alice.send(
    general.id,
    `[Never Gonna Give You Up \\(Official Video\\)](https://watch.${EVIL}/v?v=dQw4w9WgXcQ) · ${EVIL}\n` +
      thumbLine(s.ytThumb, "Rick Astley - Topic, official channel · youtube.com · 3:33"),
  );
  return s;
}

// Answers the page's LinkCard calls in place of the core (which would fetch
// from the internet). `answer(url)` -> { block, delay }.
async function stubLinkCard(app, answer, sleep) {
  const asked = [];
  await app.page.route("**/rpc", async (route) => {
    let body;
    try {
      body = JSON.parse(route.request().postData() || "{}");
    } catch {
      return route.continue();
    }
    if (body.method !== "LinkCard") return route.continue();
    asked.push(body.args);
    const { block = "", delay = 0 } = answer(body.args[0]) || {};
    if (delay) await sleep(delay);
    return route
      .fulfill({
        contentType: "application/json",
        body: JSON.stringify({ result: { url: body.args[0], title: "", site: "", duration: "", block } }),
      })
      .catch(() => {});
  });
  return asked;
}

async function waitChecked(box, want) {
  for (let i = 0; i < 50; i++) {
    if ((await box.isChecked()) === want) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`the switch did not turn ${want ? "on" : "off"}`);
}

async function cardsOn(app) {
  await app.page.evaluate(() => {
    const p = JSON.parse(localStorage.getItem("concord.prefs") || "{}");
    localStorage.setItem("concord.prefs", JSON.stringify({ ...p, sendLinkPreviews: true }));
  });
  await app.reload();
}

// The question, word for word: it says in plain words what Yes does.
const QUESTION =
  "Show cards for links you send? Concord will look up the title and picture of each link you send, from your device, so others see a card. The site sees a visit from you, as if you had opened the link.";
const ROW_TITLE = "Preview cards for links you send";
const NOTE = {
  undecided: "Not chosen yet: you will be asked at the first link you send",
  on: "On: Concord looks up a link's title and picture, from your device, when you send it",
  off: "Off: links you send go out as you typed them",
};
const storedChoice = (app) =>
  app.page.evaluate(() => JSON.parse(localStorage.getItem("concord.prefs") || "{}").sendLinkPreviews ?? null);

// The Privacy section's link card row: its switch and the line under it.
// Opened with the gear (desktop) or the You tab (phone) directly: the
// question above the composer is itself role=dialog, which openSettings would
// take for an open settings window.
async function privacyCardRow(app) {
  const btn = app.isPhone
    ? app.page.getByRole("button", { name: "You — profile and settings" })
    : app.page.locator('button[aria-label="Settings"]').last();
  await btn.click();
  await app.settingsSection("Privacy");
  const row = app.page.locator("label.row", { hasText: ROW_TITLE }).first();
  await row.waitFor({ state: "visible", timeout: 15000 });
  return {
    row,
    box: row.getByRole("switch", { name: ROW_TITLE }),
    // The track, where a thumb goes (the row's centre can be the ⓘ, which
    // opens the reasons instead).
    flip: () => row.locator(".switch").click(),
    note: async () => (await row.locator(".sub").innerText()).trim(),
  };
}

test("a card arrives whole, and reading it contacts nobody", async ({ world, assert, step, waitFor }) => {
  const { bashir, general } = await seedFeed(world);
  const b = await bashir.open({ viewport: DESKTOP });
  const traffic = watchTraffic(b.page);
  await b.openChannel(general.name, "Cards");
  const p = b.page;
  await p.locator(".lc", { hasText: "Never Gonna Give You Up" }).first().waitFor();
  await waitFor(async () => (await p.locator(".lc img").count()) >= 3, { message: "card pictures to arrive from Alice" });

  step("a link-only message is only its card");
  const onlyRow = p.locator(".msg[data-msg-id]", { has: p.locator(".lc") }).first();
  assert.equal(await onlyRow.locator(".body").count(), 0, "the link-only card message still drew a body");
  assert.ok(!(await onlyRow.innerText()).includes("watch?v="), "the URL is printed on a link-only card row");
  const card = p.locator(".lc").first();
  assert.match(await card.innerText(), /Rick Astley/);
  assert.equal((await card.locator(".dom").innerText()).trim(), "youtube.com", "the domain line");
  assert.equal(await card.evaluate((el) => el.classList.contains("video")), true, "a real YouTube card is a video card");
  const ratio = await card.locator(".media").evaluate((el) => el.getBoundingClientRect().width / el.getBoundingClientRect().height);
  assert.ok(Math.abs(ratio - 16 / 9) < 0.03, `the picture is not 16:9 (${ratio})`);
  assert.equal(await card.locator(".play").count(), 1, "a real YouTube card has its play button");
  assert.match(await card.locator(".title").innerText(), /\(Official Video\)/, "the escaped title reads cleanly");
  await card.scrollIntoViewIfNeeded();
  await shot(p, "desktop1280-youtube-card");

  step("a link in a sentence keeps the sentence, the URL printed as its domain");
  await b.waitForMessage("this one is a classic");
  const sentence = b.messageRow("this one is a classic");
  assert.equal(await sentence.locator(".lc").count(), 1);
  assert.equal(await sentence.locator(".body a.link-short").innerText(), "youtube.com");
  assert.ok(!(await sentence.locator(".body").innerText()).includes("watch?v="), "the raw URL still shows above its card");
  await sentence.scrollIntoViewIfNeeded();
  await shot(p, "desktop1280-sentence");

  step("a disguised link cannot wear another site's name");
  const phish = b.message("log in here");
  assert.equal(await phish.locator("a", { hasText: /^paypal\.com$/ }).count(), 0, "paypal.com is still the link's face");
  assert.equal(await phish.locator('a[href="https://evil.example/login"]').innerText(), "https://evil.example/login");
  const masked = b.message("the docs:");
  assert.equal((await masked.locator(".link-dom").innerText()).trim(), "evil.example", "a masked link's real domain chip");

  step("a card-less YouTube link waits for a tap");
  await b.messageRow("an older client sent this").locator(".placeholder").waitFor();
  await b.messageRow("an older client sent this").scrollIntoViewIfNeeded();
  await shot(p, "desktop1280-masked-and-cardless");
  await p.locator(".lc", { has: p.locator(".dom", { hasText: EVIL }) }).scrollIntoViewIfNeeded();
  await shot(p, "desktop1280-page-card-and-spoof");

  step("right-click: one Copy link, its real domain, and the message link named apart");
  await card.scrollIntoViewIfNeeded();
  await card.locator(".title").click({ button: "right" });
  const open = p.getByRole("menuitem", { name: /^Open link/ });
  await open.waitFor();
  assert.match(await open.innerText(), /youtube\.com/);
  assert.equal(await p.getByRole("menuitem", { name: /^Copy link/ }).count(), 1, "two items are still called Copy link");
  assert.match(await p.getByRole("menuitem", { name: /^Copy link/ }).innerText(), /youtube\.com/);
  assert.equal(await p.getByRole("menuitem", { name: /^Copy message link/ }).count(), 1);
  await shot(p, "desktop1280-card-menu");
  await p.keyboard.press("Escape");

  step("nothing reached past this device's own core");
  const outside = [...traffic.hosts].filter((h) => h !== "127.0.0.1" && h !== "localhost");
  assert.deepEqual(outside, [], `the reader's page contacted ${outside.join(", ")}`);
  assert.deepEqual(traffic.methods.filter((m) => FETCHES.test(m)), [], "the reader's page asked its core to fetch a link");
});

test("phone: the feed, and a page posing as YouTube is a page with its real domain in full", async ({ world, assert, step }) => {
  const { bashir, general } = await seedFeed(world);
  const b = await bashir.open({ viewport: "phone" });
  await b.openChannel(general.name, "Cards");
  const p = b.page;

  step("phone captures of the feed");
  const yt = p.locator(".lc", { has: p.locator(".dom", { hasText: /^youtube\.com$/ }) }).first();
  await yt.locator("img").waitFor();
  await yt.scrollIntoViewIfNeeded();
  await shot(p, "phone390-youtube-card");
  await b.messageRow("this one is a classic").scrollIntoViewIfNeeded();
  await shot(p, "phone390-sentence");
  await p.locator(".lc", { hasText: "Concord: chat that stays yours" }).scrollIntoViewIfNeeded();
  await shot(p, "phone390-page-card");
  await b.messageRow("the docs:").scrollIntoViewIfNeeded();
  await shot(p, "phone390-masked-links");

  const card = p.locator(".lc", { has: p.locator(".dom", { hasText: EVIL }) }).first();
  await card.waitFor();
  await card.scrollIntoViewIfNeeded();
  await shot(p, "phone390-spoof");

  step("drawn as a page: no video layout, no play button, no length badge");
  assert.equal(await card.evaluate((el) => el.classList.contains("page")), true, "the spoof got the video layout");
  assert.equal(await card.locator(".play").count(), 0);
  assert.equal(await card.locator(".dur").count(), 0);

  step("the site line has lost the address it borrowed");
  const site = await card.locator(".site").innerText();
  assert.ok(!/youtube/i.test(site), `the site still says ${site}`);

  step("the real domain is whole, inside the card, on its own line");
  const dom = card.locator(".dom");
  assert.equal((await dom.innerText()).replace(/\s+/g, ""), EVIL);
  const geo = await dom.evaluate((el) => {
    const d = el.getBoundingClientRect();
    const c = el.closest(".lc").getBoundingClientRect();
    const by = el.parentElement.querySelector(".by")?.getBoundingClientRect();
    return { clipped: el.scrollWidth > el.clientWidth + 1, inside: d.left >= c.left && d.right <= c.right + 0.5, below: !by || d.top >= by.bottom - 1 };
  });
  assert.deepEqual(geo, { clipped: false, inside: true, below: true }, "the domain is cut, outside the card, or shares a line");
});

test("phone: holding a card, or its row, offers the link and its domain", async ({ world, assert, step }) => {
  const { alice, bashir, general, ytCard } = await seed(world);
  await alice.send(general.id, `look at this one ${YT}\n${ytCard}`);
  const b = await bashir.open({ viewport: "phone" });
  const traffic = watchTraffic(b.page);
  await b.openChannel(general.name, "Cards");
  const p = b.page;
  const card = p.locator(".lc", { hasText: "Never Gonna Give You Up" }).first();
  await card.waitFor();
  await card.locator("img").waitFor();

  const sheetOffersTheLink = async (what) => {
    const open = p.getByRole("menuitem", { name: /^Open link/ });
    await open.waitFor();
    assert.match(await open.innerText(), /youtube\.com/, `${what}: Open link names the domain`);
    assert.equal(await p.getByRole("menuitem", { name: /^Copy link/ }).count(), 1, `${what}: one Copy link`);
    assert.equal(await p.getByRole("menuitem", { name: /^Copy message link/ }).count(), 1, `${what}: Copy message link`);
  };

  step("long-press the picture");
  await card.scrollIntoViewIfNeeded();
  await b.longPress(card.locator(".media"));
  await sheetOffersTheLink("the picture");
  await shot(p, "phone390-card-hold");
  await p.keyboard.press("Escape");
  await p.getByRole("menuitem", { name: /^Open link/ }).waitFor({ state: "detached" }).catch(() => {});

  step("long-press the words of the row: the card's link is still offered");
  await b.longPress(b.message("look at this one"));
  await sheetOffersTheLink("the row's words");
  await p.keyboard.press("Escape");

  const outside = [...traffic.hosts].filter((h) => h !== "127.0.0.1" && h !== "localhost");
  assert.deepEqual(outside, [], `the reader's phone page contacted ${outside.join(", ")}`);
});

test("the composer fetches nothing while a link is typed, and the card once at send", async ({ world, assert, step, sleep }) => {
  const { alice, bashir, general, ytThumb } = await seed(world);
  const a = await alice.open({ viewport: DESKTOP });
  const block = `[Never Gonna Give You Up](${YT}) · youtube.com\n${thumbLine(ytThumb, "Rick Astley")}`;
  const traffic = watchTraffic(a.page);

  step("with the default (ask), words without a link raise no question");
  await a.openChannel(general.name, "Cards");
  const question = a.page.locator(".card-ask");
  await a.composer().fill("no link here");
  await a.composer().press("Enter");
  await bashir.waitForMessage(general.id, "no link here", { timeout: 30000 });
  assert.equal(await question.count(), 0, "a message without a link asked about cards");

  step("the first link goes out as typed, nothing is fetched, and the question is asked");
  const asked = await stubLinkCard(a, () => ({ block }), sleep);
  await a.composer().fill(`default ${YT}`);
  await a.composer().press("Enter");
  await bashir.waitForMessage(general.id, `default ${YT}`, { timeout: 30000 });
  await question.waitFor({ timeout: 5000 });
  assert.equal((await question.locator("span").first().innerText()).trim(), QUESTION, "the question's words");
  assert.deepEqual(traffic.methods.filter((m) => FETCHES.test(m)), [], "a card was fetched before the question was answered");
  await shot(a.page, "desktop1280-first-link-question");

  step("Yes: the question goes, and the answer is kept");
  await question.locator(".ca-yes").click();
  await question.waitFor({ state: "detached", timeout: 5000 });
  const pref = await a.page.evaluate(() => JSON.parse(localStorage.getItem("concord.prefs") || "{}").sendLinkPreviews);
  assert.equal(pref, true, `the answer was stored as ${JSON.stringify(pref)}`);

  step("cards on: typing and pausing fetches nothing (ZL-10)");
  const typedFetches = () => traffic.methods.filter((m) => FETCHES.test(m)).length;
  const before = typedFetches();
  await a.composer().pressSequentially("https://www.youtube.com/watch?v=dQw4", { delay: 30 });
  await sleep(1500);
  await a.composer().pressSequentially("w9WgXcQ", { delay: 30 });
  await sleep(1500);
  assert.equal(typedFetches(), before, "a link was fetched while it was being typed");
  assert.deepEqual(asked, []);

  step("Enter fetches the card once, and the link-only message goes out as only the card");
  await a.composer().press("Enter");
  await bashir.waitForMessage(general.id, (m) => m.content === block, { timeout: 30000 });
  assert.deepEqual(asked, [[YT, true]]);

  step("a link in a sentence keeps the sentence");
  await a.composer().fill(`look ${YT} ok`);
  await a.composer().press("Enter");
  await bashir.waitForMessage(general.id, (m) => m.content === `look ${YT} ok\n${block}`, { timeout: 30000 });

  assert.equal(await question.count(), 0, "asked again after Yes");

  step("editing the card message shows the link, not the card's lines (ZL-07)");
  const row = a.page.locator(".msg[data-msg-id]", { has: a.page.locator(".lc") }).first();
  await row.waitFor();
  await a.composer().press("ArrowUp");
  const edit = a.page.locator("textarea.edit-input, .edit textarea").first();
  await edit.waitFor();
  const v = await edit.inputValue();
  assert.equal(v, `look ${YT} ok`, `the edit box holds ${JSON.stringify(v)}`);
  await edit.press("Escape");
});

// The owner's default is "ask": no card until the sender has answered once.
// "Not now" is an answer: links go out bare, nothing is fetched, and the
// question is not asked again, after a reload too.
test("phone: Not now keeps links bare and is not asked again", async ({ world, assert, step, sleep }) => {
  const { alice, bashir, general, ytThumb } = await seed(world);
  const a = await alice.open({ viewport: "phone" });
  const block = `[Never Gonna Give You Up](${YT}) · youtube.com\n${thumbLine(ytThumb, "Rick Astley")}`;
  const traffic = watchTraffic(a.page);
  await a.openChannel(general.name, "Cards");
  const question = a.page.locator(".card-ask");
  // On a phone Enter is a newline; the Send button sends.
  const send = async (text) => {
    await a.composer().fill(text);
    await a.page.getByRole("button", { name: "Send", exact: true }).click();
  };

  step("the first link asks");
  await stubLinkCard(a, () => ({ block }), sleep);
  await send(`first ${YT}`);
  await bashir.waitForMessage(general.id, `first ${YT}`, { timeout: 30000 });
  await question.waitFor({ timeout: 5000 });
  assert.equal((await question.locator("span").first().innerText()).trim(), QUESTION, "the question's words");
  await shot(a.page, "phone390-first-link-question");

  step("Not now: the question goes and the next link is sent bare, without a fetch");
  await question.locator(".ca-no").click();
  await question.waitFor({ state: "detached", timeout: 5000 });
  await send(`second ${YT}`);
  await bashir.waitForMessage(general.id, `second ${YT}`, { timeout: 30000 });
  await sleep(1000);
  assert.equal(await question.count(), 0, "asked again after Not now");

  step("after a reload, still not asked and still nothing fetched");
  await a.reload();
  // The phone comes back in the channel it was in.
  await a.composer().waitFor({ timeout: 15000 });
  await send(`third ${YT}`);
  await bashir.waitForMessage(general.id, `third ${YT}`, { timeout: 30000 });
  await sleep(1000);
  assert.equal(await question.count(), 0, "asked again after a reload");
  assert.deepEqual(traffic.methods.filter((m) => FETCHES.test(m)), [], "a card was fetched after Not now");
});

// Settings › Privacy is the same choice as the question. Its line says which
// of the three states you are in; setting it there, either way, is the answer:
// a question already up goes, it is never asked again, and links go out as
// the switch says.
test("Settings: the card switch is the question's choice, on and then off", async ({ world, assert, step, sleep }) => {
  const { alice, bashir, general, ytThumb } = await seed(world);
  const a = await alice.open({ viewport: DESKTOP });
  const block = `[Never Gonna Give You Up](${YT}) · youtube.com\n${thumbLine(ytThumb, "Rick Astley")}`;
  const traffic = watchTraffic(a.page);
  const fetches = () => traffic.methods.filter((m) => FETCHES.test(m)).length;
  const asked = await stubLinkCard(a, () => ({ block }), sleep);
  const question = a.page.locator(".card-ask");
  await a.openChannel(general.name, "Cards");

  step("the first link asks");
  await a.composer().fill(`first ${YT}`);
  await a.composer().press("Enter");
  await bashir.waitForMessage(general.id, `first ${YT}`, { timeout: 30000 });
  await question.waitFor({ timeout: 5000 });

  step("Settings: the switch is off and its line says the question is still to come");
  let s = await privacyCardRow(a);
  assert.equal(await s.box.isChecked(), false, "undecided shows as on");
  assert.equal(await s.note(), NOTE.undecided);
  assert.equal(await storedChoice(a), null);
  await shot(a.page, "desktop1280-settings-undecided");

  step("switching it on stores Yes and says so");
  await s.flip();
  await waitChecked(s.box, true);
  assert.equal(await s.note(), NOTE.on);
  assert.equal(await storedChoice(a), true);
  await shot(a.page, "desktop1280-settings-on");

  step("that answered the question: it is gone");
  await a.closeModals();
  await a.composer().waitFor();
  assert.equal(await question.count(), 0, "the question stayed after choosing in Settings");

  step("the next link gets its card, and no question");
  await a.composer().fill(YT);
  await a.composer().press("Enter");
  await bashir.waitForMessage(general.id, (m) => m.content === block, { timeout: 30000 });
  assert.deepEqual(asked, [[YT, true]]);
  await sleep(1000);
  assert.equal(await question.count(), 0, "asked after Yes in Settings");

  step("switching it off stores No and says so");
  s = await privacyCardRow(a);
  await s.flip();
  await waitChecked(s.box, false);
  assert.equal(await s.note(), NOTE.off);
  assert.equal(await storedChoice(a), false);
  await shot(a.page, "desktop1280-settings-off");

  step("the next link goes out bare, nothing is fetched, and no question");
  await a.closeModals();
  const before = fetches();
  await a.composer().fill(`bare ${YT}`);
  await a.composer().press("Enter");
  await bashir.waitForMessage(general.id, `bare ${YT}`, { timeout: 30000 });
  await sleep(1000);
  assert.equal(fetches(), before, "a card was fetched with the switch off");
  assert.equal(await question.count(), 0, "asked after No in Settings");

  step("after a reload the switch still says off");
  await a.reload();
  s = await privacyCardRow(a);
  assert.equal(await s.box.isChecked(), false);
  assert.equal(await s.note(), NOTE.off);
});

// A phone that sets the switch before ever sending a link is never asked:
// off first (links bare, nothing fetched), then on (cards).
test("phone: choosing in Settings first means the question never comes, off and then on", async ({ world, assert, step, sleep }) => {
  const { alice, bashir, general, ytThumb } = await seed(world);
  const a = await alice.open({ viewport: "phone" });
  const block = `[Never Gonna Give You Up](${YT}) · youtube.com\n${thumbLine(ytThumb, "Rick Astley")}`;
  const traffic = watchTraffic(a.page);
  const asked = await stubLinkCard(a, () => ({ block }), sleep);
  const question = a.page.locator(".card-ask");
  const send = async (text) => {
    await a.composer().fill(text);
    await a.page.getByRole("button", { name: "Send", exact: true }).click();
  };
  // Settings is reached from the tabs, which a phone shows outside a
  // conversation: Back (history) leaves the channel first.
  const toTabs = async () => {
    await a.page.evaluate(() => history.back());
    await a.page.getByRole("button", { name: "You — profile and settings" }).waitFor({ timeout: 15000 });
  };

  step("Settings before any link: undecided");
  let s = await privacyCardRow(a);
  assert.equal(await s.box.isChecked(), false);
  assert.equal(await s.note(), NOTE.undecided);
  await shot(a.page, "phone390-settings-undecided");

  step("off: undecided already shows off, so on and back off stores No");
  await s.flip();
  await waitChecked(s.box, true);
  await s.flip();
  await waitChecked(s.box, false);
  assert.equal(await s.note(), NOTE.off);
  assert.equal(await storedChoice(a), false);
  await shot(a.page, "phone390-settings-off");
  await a.closeModals();

  step("a link goes out bare: no fetch, no question");
  await a.openChannel(general.name, "Cards");
  await send(`bare ${YT}`);
  await bashir.waitForMessage(general.id, `bare ${YT}`, { timeout: 30000 });
  await sleep(1000);
  assert.equal(await question.count(), 0, "asked after No in Settings");
  assert.deepEqual(traffic.methods.filter((m) => FETCHES.test(m)), [], "a card was fetched after No in Settings");

  step("on in Settings: the next link gets its card, still no question");
  await toTabs();
  s = await privacyCardRow(a);
  await s.flip();
  await waitChecked(s.box, true);
  assert.equal(await s.note(), NOTE.on);
  assert.equal(await storedChoice(a), true);
  await shot(a.page, "phone390-settings-on");
  await a.closeModals();
  await a.openChannel(general.name);
  await send(YT);
  await bashir.waitForMessage(general.id, (m) => m.content === block, { timeout: 30000 });
  assert.deepEqual(asked, [[YT, true]]);
  await sleep(1000);
  assert.equal(await question.count(), 0, "asked after Yes in Settings");
});

test("a message sent after a link is never overtaken and never waits", async ({ world, assert, step, sleep }) => {
  const { alice, bashir, general, ytThumb } = await seed(world);
  const a = await alice.open();
  await cardsOn(a);
  await a.openChannel(general.name, "Cards");
  const block = `[Never Gonna Give You Up](${YT}) · youtube.com\n${thumbLine(ytThumb, "Rick Astley")}`;
  let delay = 1200;
  await stubLinkCard(a, () => ({ block, delay }), sleep);
  const stored = async (first, second) => {
    const msgs = await bashir.messages(general.id);
    return [msgs.findIndex((m) => m.content.startsWith(first)), msgs.findIndex((m) => m.content === second)];
  };

  step("while the card is fetched, the link's row holds the card's place");
  await a.composer().fill(`solo ${YT}`);
  await a.composer().press("Enter");
  await a.page.locator(".pmsg .pcard", { hasText: "youtube.com" }).waitFor({ timeout: 1500 });
  await bashir.waitForMessage(general.id, `solo ${YT}\n${block}`, { timeout: 30000 });

  step("a slow card and a reply straight after: the link goes first, at once, without its card");
  const t0 = Date.now();
  await a.composer().fill(`first ${YT}`);
  await a.composer().press("Enter");
  await a.composer().fill("second, typed after the link");
  await a.composer().press("Enter");
  await bashir.waitForMessage(general.id, "second, typed after the link", { timeout: 30000 });
  const took = Date.now() - t0;
  let [i, j] = await stored(`first ${YT}`, "second, typed after the link");
  assert.ok(i >= 0 && j > i, `stored order: first at ${i}, second at ${j}`);
  assert.equal((await bashir.messages(general.id))[i].content, `first ${YT}`, "the link waited for its card behind the reply");
  assert.ok(took < 1200 + 3000, `the reply waited ${took} ms`);

  step("a card that never comes: out without it, within the short wait");
  delay = 60000;
  const t1 = Date.now();
  await a.composer().fill(`third ${YT}`);
  await a.composer().press("Enter");
  await bashir.waitForMessage(general.id, `third ${YT}`, { timeout: 30000 });
  assert.ok(Date.now() - t1 < 2000 + 3000, `the hung card held the link for ${Date.now() - t1} ms`);
  [i, j] = await stored(`first ${YT}`, `third ${YT}`);
  assert.ok(j > i, "order");
});

// The queue the first ordering fix used held every later message behind the
// one before it, and a send the core never answered then held the channel for
// the rest of the session, Retry included (link-cards review R2-01).
test("a send the core never answers does not hold the channel", async ({ world, assert, step }) => {
  const { alice, bashir, general } = await seed(world);
  const a = await alice.open();
  await a.openChannel(general.name, "Cards");
  await a.page.route("**/rpc", async (route) => {
    let body;
    try {
      body = JSON.parse(route.request().postData() || "{}");
    } catch {
      return route.continue();
    }
    if (body.method === "SendMessage" && String(body.args?.[1]).startsWith("stuck")) return; // never answered
    return route.continue();
  });
  step("one send hangs; the next goes out");
  await a.composer().fill("stuck one");
  await a.composer().press("Enter");
  await a.composer().fill("after the stuck one");
  await a.composer().press("Enter");
  const got = await bashir.waitForMessage(general.id, "after the stuck one", { timeout: 20000 });
  assert.ok(got, "the message after a hung send never arrived");
});

// On the web a reload or a closed tab ends a link's wait and starts its send
// as the page goes, and the browser may cut that request off. The row was
// taken off the disk as the send started, so the message was lost (link-cards-2
// review LC2-03). It now stays written down until the send has finished, and
// the next page checks the channel before sending it again.
test("a reload while a link waits for its card loses nothing and sends nothing twice", async ({ world, assert, step, sleep }) => {
  const { alice, bashir, general } = await seed(world);
  const a = await alice.open();
  await cardsOn(a);
  await a.openChannel(general.name, "Cards");
  // On the context, so the next page gets the same answers. The card never
  // comes in time: the message waits the whole 2 s. A link message's send is
  // what the page going away does to its last request: "cut" never reaches
  // the core; "landed" reaches it, but its answer never reaches the page.
  const seen = { cut: 0, landed: 0 };
  await a.ctx.route("**/rpc", async (route) => {
    let body;
    try {
      body = JSON.parse(route.request().postData() || "{}");
    } catch {
      return route.continue();
    }
    if (body.method === "LinkCard") return; // never answered
    const text = body.method === "SendMessage" ? String(body.args?.[1]) : "";
    if (text.startsWith("cut ") && seen.cut++ === 0) return; // never sent
    if (text.startsWith("landed ") && seen.landed++ === 0) {
      await route.fetch().catch(() => {});
      return; // the core has it; the page never hears so
    }
    return route.continue();
  });
  const copies = async (text) => (await bashir.messages(general.id)).filter((m) => m.content === text).length;
  // The page is told it is going (the pagehide a reload or a closed tab
  // fires), its send is caught as above, and then it is gone: frozen, so
  // nothing more of it runs, and the next page opens beside it.
  const goAway = async (caught) => {
    await a.page.evaluate(() => window.dispatchEvent(new Event("pagehide")));
    for (let i = 0; i < 50 && !caught(); i++) await sleep(100);
    assert.ok(caught(), "the page going away sent the waiting message");
    const cdp = await a.ctx.newCDPSession(a.page);
    await cdp.send("Page.setWebLifecycleState", { state: "frozen" });
    a.page = await a.ctx.newPage();
    await a.unlock();
  };

  step("a send cut off as the page went goes out from the next page");
  await a.composer().fill(`cut ${YT}`);
  await a.composer().press("Enter");
  await a.page.locator(".pmsg").first().waitFor({ timeout: 1500 });
  await goAway(() => seen.cut === 1);
  await bashir.waitForMessage(general.id, `cut ${YT}`, { timeout: 30000 });
  assert.equal(seen.cut, 2, "sent again once, by the next page");
  assert.equal(await copies(`cut ${YT}`), 1, "one copy");

  step("a send that reached the core before the page went is not sent again");
  await a.openChannel(general.name, "Cards");
  await a.composer().fill(`landed ${YT}`);
  await a.composer().press("Enter");
  await a.page.locator(".pmsg").first().waitFor({ timeout: 1500 });
  await goAway(() => seen.landed === 1);
  await bashir.waitForMessage(general.id, `landed ${YT}`, { timeout: 30000 });
  await a.openChannel(general.name, "Cards");
  await a.message("landed").waitFor({ timeout: 15000 });
  await sleep(3000);
  assert.equal(seen.landed, 1, "the next page asked the channel and did not send it again");
  assert.equal(await copies(`landed ${YT}`), 1, "one copy");
  assert.equal(await a.page.locator(".pmsg").count(), 0, "no row left saying it is not sent");
});
