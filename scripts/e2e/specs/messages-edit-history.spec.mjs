// Where an edited message's history opens. On desktop the "(edited)" mark is a
// button that opens the card. On a phone the mark is plain text, too small to
// tap, and "Edit history" in the message's long-press menu opens the card:
// for a link-only card message too, and on a folded long message, where the
// card must not be clipped by the fold. Opened from the menu, the card is a
// layer, so Escape (and the phone's back) closes it and not the conversation.
import { readFileSync } from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

const ROOT = path.resolve(path.dirname(new URL(import.meta.url).pathname), "../../..");
const b64url = (s) => Buffer.from(s, "utf8").toString("base64url");

// A link card needs a sealed picture; this is the link-cards spec's way of
// making one (an ordinary attachment in a scratch channel).
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

// On a phone the conversation slides in after the feed is already in the DOM;
// act only once the page is at rest.
async function settled(a, waitFor) {
  await waitFor(
    () =>
      a.page.evaluate(() => {
        const p = document.querySelector(".mshell .page");
        return !!p && !p.classList.contains("moving") && Math.round(p.getBoundingClientRect().x) === 0;
      }),
    { message: "the conversation page to come to rest" },
  );
}

// The card is on screen and nothing covers or clips its middle.
const cardShown = (a) =>
  card(a).evaluate((el) => {
    const r = el.getBoundingClientRect();
    const x = r.left + r.width / 2;
    const y = r.top + Math.min(r.height / 2, 40);
    if (r.width < 1 || r.height < 1 || x < 0 || y < 0 || x > innerWidth || y > innerHeight) return false;
    return el.contains(document.elementFromPoint(x, y));
  });

async function editedMessage(world) {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "History", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const first = await bashir.send(ch.id, "said it one way");
  await bashir.rpc("EditMessage", ch.id, first.id, "said it better", "");
  await alice.waitForMessage(ch.id, (m) => m.id === first.id && m.edited);
  return { alice, ch };
}

const card = (a) => a.page.locator('.eh-card[role="dialog"][aria-label="Edit history"]');

test("desktop: the (edited) mark opens the edit history", async ({ world, assert, step }) => {
  const { alice, ch } = await editedMessage(world);
  const a = await alice.open();
  await a.openChannel(ch.name, "History");
  await a.waitForMessage("said it better");
  const mark = a.messageRow("said it better").locator(".eh button.eh-btn");
  assert.equal(await mark.count(), 1, "no (edited) button on desktop");

  step("clicking the mark opens the card with both versions");
  await mark.click();
  await card(a).waitFor({ state: "visible" });
  await card(a).getByText("said it one way").waitFor();
  assert.equal(await mark.getAttribute("aria-expanded"), "true");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("desktop: opening the card never moves the feed sideways", async ({ world, assert, step, sleep }) => {
  // Lines of growing length put the mark at many places along the line; on
  // some the card (about 355px wide) reaches past the feed's right edge. The
  // feed hides its horizontal overflow, so a sideways scroll there could never
  // be undone by the reader.
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "History", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const texts = [];
  for (let n = 8; n <= 30; n++) {
    const text = `m${n} ${"wordy ".repeat(n)}`;
    const m = await bashir.send(ch.id, text);
    await bashir.rpc("EditMessage", ch.id, m.id, `${text}x`, "");
    texts.push(`m${n} `);
  }
  await alice.waitForMessage(ch.id, (m) => (m.content || "").startsWith("m30 ") && m.edited);
  const a = await alice.open({ viewport: { width: 1280, height: 800 } });
  await a.openChannel(ch.name, "History");
  await a.waitForMessage("m30 ");

  step("open each card in turn; the feed's scrollLeft stays 0");
  let crossed = 0;
  for (const t of texts.reverse()) {
    const row = a.messageRow(t);
    await row.evaluate((el) => el.scrollIntoView({ block: "center", inline: "nearest" }));
    await a.page.evaluate(() => (document.querySelector(".feed").scrollLeft = 0));
    await row.locator("button.eh-btn").click();
    await card(a).waitFor({ state: "visible" });
    await card(a).locator(".eh-v").first().waitFor();
    await sleep(150);
    const at = await a.page.evaluate(() => {
      const f = document.querySelector(".feed");
      const c = document.querySelector(".eh-card").getBoundingClientRect();
      return { left: f.scrollLeft, over: c.right > f.getBoundingClientRect().right - 1 };
    });
    if (at.over) crossed++;
    assert.equal(at.left, 0, `opening ${t.trim()}'s history moved the feed sideways`);
    await a.page.keyboard.press("Escape");
    await card(a).waitFor({ state: "detached" });
  }
  // Otherwise the test proved nothing about the edge.
  assert.ok(crossed > 0, "no card reached the feed's right edge");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("phone: (edited) is a plain mark and the long-press menu opens the history", async ({ world, assert, step, waitFor }) => {
  const { alice, ch } = await editedMessage(world);
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "History");
  await a.waitForMessage("said it better");
  await settled(a, waitFor);
  const row = a.messageRow("said it better");
  assert.equal(await row.locator(".eh button").count(), 0, "the (edited) mark is still a button on a phone");
  assert.equal((await row.locator(".eh .eh-mark").textContent()).trim(), "(edited)");

  step("tapping the mark opens nothing");
  await row.locator(".eh .eh-mark").tap();
  await a.page.waitForTimeout(400);
  assert.equal(await card(a).count(), 0, "a tap on the plain mark opened the card");

  step("long-press menu: Edit history");
  await a.longPress(a.message("said it better"));
  await a.page.getByRole("menuitem", { name: "Edit history", exact: true }).tap();
  await card(a).waitFor({ state: "visible" });
  await card(a).getByText("said it one way").waitFor();

  step("Escape closes the card, not the conversation");
  await a.page.keyboard.press("Escape");
  await card(a).waitFor({ state: "detached", timeout: 5000 });
  assert.ok(await a.composer().isVisible(), "Escape left the conversation");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("phone: a link-only card message's (edited) is a plain mark and the menu opens its history", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "History", members: [alice, bashir], channels: ["scratch"] });
  const ch = g.allChannels.find((c) => c.name !== "scratch");
  const scratch = g.allChannels.find((c) => c.name === "scratch");
  const b = await bashir.open();
  const thumb = await sealThumb(bashir, b, scratch.id, "docs/media/social-preview.png");
  await b.close();
  const cardMsg = (title) => `[${title}](https://concord.example.org/) · example.org\n${thumbLine(thumb, title)}`;
  const first = await bashir.send(ch.id, cardMsg("The first title"));
  await bashir.rpc("EditMessage", ch.id, first.id, cardMsg("The second title"), "");
  await alice.waitForMessage(ch.id, (m) => m.id === first.id && m.edited);

  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "History");
  const row = a.page.locator(`.msg[data-msg-id="${first.id}"]`);
  await row.locator(".lc img").waitFor();
  await settled(a, waitFor);
  assert.equal(await row.locator(".body").count(), 0, "not a link-only card row");
  assert.equal(await row.locator(".eh button").count(), 0, "the (edited) mark is still a button on a phone");
  assert.equal((await row.locator(".eh .eh-mark").textContent()).trim(), "(edited)");

  step("long-press the card: Edit history opens the card with both versions");
  await a.longPress(row.locator(".lc").first());
  await a.page.getByRole("menuitem", { name: "Edit history", exact: true }).tap();
  await card(a).waitFor({ state: "visible", timeout: 5000 });
  await card(a).getByText(/The first title/).waitFor();
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("phone: Edit history on a folded long message shows the card", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "History", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const long = Array.from({ length: 40 }, (_, i) => `line ${i + 1} of a long note`).join("\n");
  const first = await bashir.send(ch.id, long);
  await bashir.rpc("EditMessage", ch.id, first.id, `${long}\nand a last line added`, "");
  await alice.waitForMessage(ch.id, (m) => m.id === first.id && m.edited);

  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "History");
  const row = a.page.locator(`.msg[data-msg-id="${first.id}"]`);
  await row.locator(".body").waitFor();
  await settled(a, waitFor);
  assert.equal(await row.locator(".body.folded").count(), 1, "the long message is not folded");

  step("long-press the first line: Edit history");
  await row.locator(".body").evaluate((el) => el.scrollIntoView({ block: "start" }));
  await a.longPress(row.locator(".body"), { at: { x: 24, y: 12 } });
  await a.page.getByRole("menuitem", { name: "Edit history", exact: true }).tap();
  await card(a).waitFor({ state: "attached", timeout: 5000 });
  await card(a).getByText("and a last line added").waitFor({ state: "attached" });
  await waitFor(() => cardShown(a), { timeout: 5000, message: "the history card to be on screen and not clipped" });
  // Unfolded, not scrolled inside the fold: the card belongs to the whole message.
  assert.equal(await row.locator(".body.folded").count(), 0, "the message stayed folded under its history card");
  assert.equal(await row.locator(".body").evaluate((el) => el.scrollTop), 0, "the folded body was scrolled to show the card");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
