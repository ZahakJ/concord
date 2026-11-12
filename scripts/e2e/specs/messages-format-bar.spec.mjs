// The composer's formatting bar is one tap away and stays where you put it.
// The redesign took the permanent bar away and left it only as a row of the +
// menu, as session state: two clicks, forgotten on every reload, and nothing
// in the box said it existed. "Aa" beside emoji now shows a single row of
// every mark, + › Formatting and Ctrl/Cmd+Shift+F flip the same per-device
// pref, every button applies its markdown to a selection and to an empty
// caret, and the chords work with the bar hidden. On a phone the + sheet's Aa
// tile raises the same one sideways-scrolling row of finger-sized buttons.
import { test } from "../harness.mjs";

// [aria-label, chord, on "word" selected, on an empty caret]
const MARKS = [
  ["Bold", "Control+b", "**word**", "**bold**"],
  ["Italic", "Control+i", "*word*", "*italic*"],
  ["Underline", "Control+Shift+U", "__word__", "__underline__"],
  ["Strikethrough", "Control+Shift+S", "~~word~~", "~~strikethrough~~"],
  ["Spoiler", "Control+Shift+X", "||word||", "||spoiler||"],
  ["Inline code", "Control+e", "`word`", "`code`"],
  ["Code block", "Control+Shift+E", "```\nword\n```", "```\ncode\n```"],
  ["Quote", "Control+Shift+Period", "> word", "> "],
  ["Bulleted list", "Control+Shift+Digit8", "- word", "- "],
  ["Link", "Control+Shift+K", "[word](url)", "[text](url)"],
];

async function reset(app, text = "") {
  const c = app.composer();
  await c.click();
  await c.fill(text);
  if (text) await c.press("Control+a");
}

test("Aa shows the formatting bar, every button formats, and the choice survives a reload", async ({ world, assert, step, waitFor, shot }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Marks", members: [alice] });
  const ch = g.allChannels[0];
  const a = await alice.open();
  const { page } = a;
  await a.openChannel(ch.name, "Marks");

  const aa = page.getByRole("button", { name: "Formatting bar" });
  const bar = page.locator(".composer .fmt-bar:not(.selection)");

  step("The box starts clean: no bar, Aa not pressed");
  assert.equal(await bar.count(), 0, "a formatting bar before anyone asked for one");
  assert.equal(await aa.getAttribute("aria-pressed"), "false");

  step("Aa shows one row of every mark, each with its chord in the tooltip");
  const before = await page.locator(".composer-wrap").evaluate((e) => e.getBoundingClientRect().height);
  await aa.click();
  await bar.waitFor({ state: "visible" });
  assert.equal(await aa.getAttribute("aria-pressed"), "true");
  const labels = await bar.locator("button").evaluateAll((bs) => bs.map((b) => b.getAttribute("aria-label")));
  for (const [label] of MARKS) assert.ok(labels.includes(label), `the bar has no ${label} button`);
  assert.ok(labels.includes("Advanced composer"), "the bar has no way to the advanced composer");
  const rows = await bar.locator(".fmtbtn").evaluateAll((bs) => new Set(bs.map((b) => Math.round(b.getBoundingClientRect().top))).size);
  assert.equal(rows, 1, `the bar wrapped onto ${rows} rows`);
  const rowH = await bar.evaluate((e) => e.getBoundingClientRect().height);
  const after = await page.locator(".composer-wrap").evaluate((e) => e.getBoundingClientRect().height);
  assert.ok(Math.abs(after - before - rowH) <= 2, `the composer grew ${after - before}px for a ${rowH}px row`);
  await bar.getByRole("button", { name: "Code block" }).hover();
  await waitFor(async () => (await page.locator(".app-tooltip").filter({ hasText: "Code block (Ctrl+Shift+E)" }).count()) > 0, {
    message: "the Code block tooltip naming Ctrl+Shift+E",
  });
  await shot(a, "bar open");

  step("Every button wraps a selection, and fills an empty caret with its placeholder");
  for (const [label, , sel, caret] of MARKS) {
    await reset(a, "word");
    await bar.getByRole("button", { name: label, exact: true }).click();
    assert.equal(await a.composer().inputValue(), sel, `${label} on a selection`);
    await reset(a);
    await bar.getByRole("button", { name: label, exact: true }).click();
    assert.equal(await a.composer().inputValue(), caret, `${label} on an empty caret`);
  }

  step("What the bar wrote is what gets stored");
  await reset(a, "ship it");
  await bar.getByRole("button", { name: "Bold", exact: true }).click();
  await a.composer().press("Enter");
  await alice.waitForMessage(ch.id, "**ship it**");
  await reset(a, "first\nsecond");
  await bar.getByRole("button", { name: "Bulleted list", exact: true }).click();
  await a.composer().press("Enter");
  await alice.waitForMessage(ch.id, "- first\n- second");
  await a.page.locator(".msg .body ul li", { hasText: "second" }).waitFor({ state: "visible" });

  step("+ › Formatting is the same switch, ticked while the bar shows");
  const plus = page.getByRole("button", { name: "Add to your message" });
  await plus.click();
  const item = page.locator(".menu-item", { hasText: "Formatting" });
  assert.equal(await item.getAttribute("aria-pressed"), "true", "+ › Formatting does not know the bar is on");
  await item.click();
  await bar.waitFor({ state: "detached" });
  assert.equal(await aa.getAttribute("aria-pressed"), "false");
  await aa.click();
  await bar.waitFor({ state: "visible" });

  step("The bar is still there after a reload");
  await a.reload();
  await a.openChannel(ch.name, "Marks");
  await bar.waitFor({ state: "visible" });
  assert.equal(await page.getByRole("button", { name: "Formatting bar" }).getAttribute("aria-pressed"), "true");

  step("The bar's last button takes the draft to the advanced composer");
  await reset(a);
  await a.composer().fill("a longer thought");
  await bar.getByRole("button", { name: "Advanced composer" }).click();
  const dlg = a.dialog();
  await dlg.waitFor({ state: "visible" });
  assert.equal(await dlg.locator("textarea").first().inputValue(), "a longer thought");
  await a.closeModals();

  step("Ctrl+Shift+F hides it again, and that sticks too");
  await a.composer().click();
  await a.composer().press("Control+Shift+F");
  await bar.waitFor({ state: "detached" });
  await a.reload();
  await a.openChannel(ch.name, "Marks");
  assert.equal(await bar.count(), 0, "the bar came back after it was hidden");
});

test("the formatting chords work with the bar hidden, and Ctrl+Shift+F toggles it", async ({ world, assert, step }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Chords", members: [alice] });
  const ch = g.allChannels[0];
  const a = await alice.open();
  const { page } = a;
  await a.openChannel(ch.name, "Chords");
  const bar = page.locator(".composer .fmt-bar:not(.selection)");
  assert.equal(await bar.count(), 0);

  step("Each chord wraps a selection and fills an empty caret, bar hidden");
  for (const [label, chord, sel, caret] of MARKS) {
    await reset(a, "word");
    await a.composer().press(chord);
    assert.equal(await a.composer().inputValue(), sel, `${label} chord ${chord} on a selection`);
    await reset(a);
    await a.composer().press(chord);
    assert.equal(await a.composer().inputValue(), caret, `${label} chord ${chord} on an empty caret`);
    assert.equal(await bar.count(), 0, `${chord} raised the bar`);
  }

  step("A chord-formatted message is stored as markdown");
  await reset(a, "run the tests");
  await a.composer().press("Control+Shift+E");
  await a.composer().press("Enter");
  await alice.waitForMessage(ch.id, "```\nrun the tests\n```");

  step("Ctrl+Shift+F shows and hides the bar");
  await a.composer().click();
  await a.composer().press("Control+Shift+F");
  await bar.waitFor({ state: "visible" });
  assert.equal(await page.getByRole("button", { name: "Formatting bar" }).getAttribute("aria-pressed"), "true");
  await a.composer().press("Control+Shift+F");
  await bar.waitFor({ state: "detached" });

  step("The shortcut sheet lists the toggle and every formatting chord");
  await a.composer().press("Control+/");
  const sheet = a.dialog();
  await sheet.waitFor({ state: "visible" });
  const text = await sheet.innerText();
  for (const label of ["Show or hide the formatting bar", "Bold", "Italic", "Underline", "Strikethrough", "Spoiler", "Inline code", "Code block", "Quote", "Bulleted list", "Link"]) {
    assert.ok(text.includes(label), `the shortcut sheet does not list "${label}"`);
  }
});

test("phone: the + sheet's Aa raises one row of finger-sized buttons, remembered", async ({ world, assert, step, shot }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Thumbs", members: [alice] });
  const ch = g.allChannels[0];
  const a = await alice.open({ viewport: "phone" });
  const { page } = a;
  await a.openChannel(ch.name, "Thumbs");
  const bar = page.locator(".composer .fmt-bar");
  assert.equal(await bar.count(), 0, "a formatting bar on a phone before anyone asked");
  // The phone row stays four targets: no Aa of its own.
  assert.equal(await page.getByRole("button", { name: "Formatting bar" }).count(), 0, "the phone row grew a fifth button");

  step("+ › Aa Formatting raises the bar");
  await page.getByRole("button", { name: /^Attach a file/ }).tap();
  await page.locator(".sheet-grid .tile", { hasText: "Formatting" }).tap();
  await bar.waitFor({ state: "visible" });
  const m = await bar.evaluate((el) => {
    const btns = [...el.querySelectorAll(".fmtbtn")].map((b) => b.getBoundingClientRect());
    return {
      rows: new Set(btns.map((b) => Math.round(b.top))).size,
      minW: Math.min(...btns.map((b) => b.width)),
      minH: Math.min(...btns.map((b) => b.height)),
      scrolls: el.scrollWidth > el.clientWidth,
    };
  });
  assert.equal(m.rows, 1, `phone formatting on ${m.rows} rows`);
  assert.ok(m.minW >= 44 && m.minH >= 44, `smallest phone formatting button ${m.minW}x${m.minH}`);
  assert.ok(m.scrolls, "eleven 44px buttons should scroll sideways at 390px");
  await shot(a, "phone bar open");

  step("A tap formats the draft");
  await a.composer().tap();
  await a.composer().fill("");
  await bar.getByRole("button", { name: "Spoiler", exact: true }).tap();
  assert.equal(await a.composer().inputValue(), "||spoiler||");

  step("The bar is still there after a reload, and the tile shows it is on");
  // The phone comes back inside the conversation it left.
  await a.reload();
  await bar.waitFor({ state: "visible" });
  await page.getByRole("button", { name: /^Attach a file/ }).tap();
  const tile = page.locator(".sheet-grid .tile", { hasText: "Formatting" });
  assert.equal(await tile.getAttribute("aria-pressed"), "true");
  await tile.tap();
  await bar.waitFor({ state: "detached" });
});

// The send control is in proportion to the box: the size of emoji beside it,
// an icon rather than a filled bubble while there is nothing to send, and the
// accent only once there is (text or a staged file). On a phone the glyph
// stays small inside a full finger target, and mic still stands in for send
// on an empty draft.
test("the send control matches emoji and takes the accent only when there is something to send", async ({ world, assert, step }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Paper planes", members: [alice] });
  const ch = g.allChannels[0];
  for (const viewport of ["desktop", "tablet", "phone"]) {
    step(`At ${viewport} width`);
    const a = await alice.open({ viewport });
    const { page } = a;
    await a.openChannel(ch.name, "Paper planes");
    const measure = () =>
      page.evaluate(() => {
        const box = (e) => e && { w: Math.round(e.getBoundingClientRect().width), h: Math.round(e.getBoundingClientRect().height) };
        const send = document.querySelector(".composer .sendbtn");
        const emoji = document.querySelector('.composer button[aria-label="Emoji picker"]');
        return {
          send: box(send),
          emoji: box(emoji),
          sendGlyph: box(send?.querySelector("svg")),
          emojiGlyph: box(emoji?.querySelector("svg")),
          ground: send && getComputedStyle(send).backgroundColor,
          mic: !!document.querySelector('.composer button[aria-label="Record a voice message"]'),
        };
      });
    const clear = (bg) => !bg || bg === "transparent" || /^rgba\(0, 0, 0, 0\)$/.test(bg);

    await a.composer().click();
    await a.composer().fill("");
    let m = await measure();
    if (a.isPhone) assert.ok(m.mic && !m.send, "the phone's empty draft shows the mic, not send");
    else {
      assert.deepEqual(m.send, m.emoji, `${viewport}: empty send ${JSON.stringify(m.send)} vs emoji ${JSON.stringify(m.emoji)}`);
      assert.ok(clear(m.ground), `${viewport}: send is filled (${m.ground}) with nothing to send`);
    }

    await a.composer().fill("   ");
    m = await measure();
    if (!a.isPhone) assert.ok(clear(m.ground), `${viewport}: whitespace lit the send control`);

    await a.composer().fill("look at this");
    m = await measure();
    assert.deepEqual(m.sendGlyph, m.emojiGlyph, `${viewport}: send glyph ${JSON.stringify(m.sendGlyph)} vs emoji glyph ${JSON.stringify(m.emojiGlyph)}`);
    assert.deepEqual(m.send, m.emoji, `${viewport}: send ${JSON.stringify(m.send)} vs emoji ${JSON.stringify(m.emoji)}`);
    assert.ok(!clear(m.ground), `${viewport}: send did not take the accent with text to send`);
    if (a.isPhone) assert.ok(m.send.w >= 44 && m.send.h >= 44, `phone send target ${m.send.w}x${m.send.h}`);

    await a.composer().fill("");
    await page.locator(".composer input[type=file]").first().setInputFiles({ name: "notes.txt", mimeType: "text/plain", buffer: Buffer.from("hello") });
    await page.locator(".composer .sendbtn.ready").waitFor({ state: "visible" });
    m = await measure();
    assert.ok(!clear(m.ground), `${viewport}: a staged file did not light the send control`);
    await a.close();
  }
});
