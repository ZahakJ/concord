// On a phone, a held finger in a text box belongs to the system: it selects a
// word, raises Cut / Copy / Paste / Select all, and on an empty spot offers
// Paste. The edit box sits inside a message row, whose long-press opened the
// message sheet at 400 ms (before the system's own long-press) and whose
// contextmenu was cancelled, so in edit mode none of that worked. Received
// text was select-none with no way to copy part of it.
//
// Chromium cannot draw Android's toolbar, so this checks what decides whether
// it appears: the field is selectable, the long-press opens no app menu, and
// the browser's contextmenu reaches it uncancelled. The paste of an image is
// driven through a ClipboardEvent, which is what the toolbar's Paste fires.
import { test } from "../harness.mjs";

const PNG_B64 = "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFElEQVR4nGP8z8DwnwEIGBmQAQMAKf4C/k9rnZgAAAAASUVORK5CYII=";

test("phone: the composer, the edit box and Select text leave long-press to the system", async ({
  world,
  assert,
  step,
  waitFor,
  sleep,
}) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Text", members: [alice, bashir] });
  const ch = g.allChannels[0];
  await alice.send(ch.id, "a line of mine to edit");
  await bashir.send(ch.id, "copy just the middle word please");
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "Text");
  await a.settled();
  await a.waitForMessage("copy just the middle word");
  const p = a.page;

  // Every contextmenu the page sees, and whether something cancelled it.
  await p.evaluate(() => {
    window.__cm = [];
    window.addEventListener("contextmenu", (e) => setTimeout(() => window.__cm.push(e.defaultPrevented)), true);
  });
  const contextMenus = () => p.evaluate(() => window.__cm.splice(0));
  const sheetOpen = () => p.locator(".bs-sheet").count();
  const userSelect = (loc) => loc.evaluate((el) => getComputedStyle(el).userSelect);

  step("the composer");
  await a.composer().fill("hello wonderful world");
  assert.notEqual(await userSelect(a.composer()), "none");
  await a.longPress(a.composer(), { at: { x: 60, y: 20 } });
  assert.equal(await sheetOpen(), 0, "a long-press in the composer opened an app menu");
  assert.ok(!(await contextMenus()).includes(true), "the composer's contextmenu was cancelled");

  step("the edit box");
  await a.composer().fill("");
  await a.longPress(a.message("a line of mine"));
  await p.getByRole("menuitem", { name: "Edit", exact: true }).tap();
  const edit = p.locator(".edit-input");
  await edit.waitFor();
  await sleep(300);
  await contextMenus();
  assert.notEqual(await userSelect(edit), "none", "the edit box is select-none");
  await a.longPress(edit, { at: { x: 40, y: 15 } });
  assert.equal(await sheetOpen(), 0, "a long-press in the edit box opened the message sheet");
  assert.ok(!(await contextMenus()).includes(true), "the edit box's contextmenu was cancelled");
  assert.equal(await edit.count(), 1, "the edit box is still open");
  await edit.press("Escape");

  step("part of a received message, through Select text");
  const body = a.message("copy just the middle word");
  assert.equal(await userSelect(body), "none", "rows stay select-none until asked");
  await a.longPress(body);
  await p.getByRole("menuitem", { name: "Select text", exact: true }).tap();
  await waitFor(async () => (await sheetOpen()) === 0, { timeout: 5000, message: "the sheet to close" });
  assert.notEqual(await userSelect(body), "none", "Select text left the row select-none");
  await contextMenus();
  await a.longPress(body, { at: { x: 60, y: 10 } });
  assert.equal(await sheetOpen(), 0, "a long-press on the words reopened the sheet instead of selecting");
  assert.ok(!(await contextMenus()).includes(true), "the words' contextmenu was cancelled");
  // A part of it can be selected and comes out as just that part.
  const part = await body.evaluate((el) => {
    const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    let n;
    while ((n = walk.nextNode())) {
      const i = n.data.indexOf("middle");
      if (i >= 0) {
        const r = document.createRange();
        r.setStart(n, i);
        r.setEnd(n, i + "middle".length);
        const sel = getSelection();
        sel.removeAllRanges();
        sel.addRange(r);
        return sel.toString();
      }
    }
    return null;
  });
  assert.equal(part, "middle");
  // A tap elsewhere ends it.
  await a.composer().tap();
  await waitFor(async () => (await userSelect(body)) === "none", { timeout: 5000, message: "the row to go back to select-none" });
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

test("phone: pasting an image into the composer stages it as an attachment", async ({ world, assert, waitFor }) => {
  const [alice] = await world.peers(1);
  const g = await world.guild({ name: "Paste", members: [alice] });
  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(g.allChannels[0].name, "Paste");
  await a.settled();
  await a.composer().tap();
  const prevented = await a.composer().evaluate(async (el, b64) => {
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const dt = new DataTransfer();
    dt.items.add(new File([bytes], "clip.png", { type: "image/png" }));
    const ev = new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true });
    el.dispatchEvent(ev);
    return ev.defaultPrevented;
  }, PNG_B64);
  assert.ok(prevented, "the composer took the image paste itself");
  await waitFor(async () => (await a.page.locator(".attach-tray .att-chip").count()) === 1, {
    timeout: 10000,
    message: "the pasted image to be staged",
  });
  assert.equal(await a.composer().inputValue(), "", "no text was pasted alongside");

  // The picker's guards hold for a paste too: an empty image says so, and an
  // image that will not decode is staged as a file rather than dropped.
  const paste = (name, b64) =>
    a.composer().evaluate((el, [name, b64]) => {
      const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
      const dt = new DataTransfer();
      dt.items.add(new File([bytes], name, { type: "image/png" }));
      el.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
    }, [name, b64]);
  await paste("empty.png", "");
  await a.page.getByText("empty.png is empty", { exact: false }).first().waitFor({ timeout: 10000 });
  await paste("broken.png", btoa("this is not a picture at all"));
  await waitFor(async () => (await a.page.locator(".attach-tray .att-chip").count()) === 2, {
    timeout: 10000,
    message: "the undecodable image to be staged as a file",
  });
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
