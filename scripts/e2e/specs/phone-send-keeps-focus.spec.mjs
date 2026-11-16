// On a phone, tapping Send leaves the caret in the composer, so the keyboard
// stays up for the next message: after a text send, a picture send (staged
// through the composer's file input, with a caption and without one) and a
// reply, document.activeElement is the composer's textarea once the message
// has landed.
import fs from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

// A 2x2 PNG, small enough to write inline.
const PNG = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAAFElEQVR4nGP8z8DwnwEIGBmQAQMAKf4C/k9rnZgAAAAASUVORK5CYII=",
  "base64",
);

const focused = (page) =>
  page.evaluate(() => {
    const el = document.activeElement;
    return el ? `${el.tagName.toLowerCase()}.${[...el.classList].join(".")}` : "none";
  });

test("phone: the composer keeps focus after a tap on Send", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Keep", members: [alice, bashir] });
  const ch = g.allChannels[0];
  await bashir.send(ch.id, "something to answer");
  const pic = path.join(world.dir, "pic.png");
  fs.writeFileSync(pic, PNG);

  const a = await alice.open({ viewport: "phone" });
  await a.openChannel(ch.name, "Keep");
  await a.waitForMessage("something to answer");
  const p = a.page;
  const box = a.composer();
  const sendBtn = () => p.getByRole("button", { name: "Send", exact: true });
  const sentImages = async () => (await alice.messages(ch.id)).filter((m) => m.content.includes("concord://attach/")).length;
  const assertFocus = async (what) => {
    const f = await focused(p);
    assert.match(f, /^textarea\..*draft/, `after ${what}, focus is on ${f}, not the composer`);
  };

  step("text: type, tap Send");
  await box.tap();
  await box.fill("first message from the phone");
  await sendBtn().tap();
  await alice.waitForMessage(ch.id, "first message from the phone");
  await a.waitForMessage("first message from the phone");
  await assertFocus("a text send");

  step("a follow-up goes straight in, no second tap on the box");
  await p.keyboard.type("second, typed without touching the box");
  assert.equal(await box.inputValue(), "second, typed without touching the box");
  await sendBtn().tap();
  await alice.waitForMessage(ch.id, "second, typed without touching the box");
  await assertFocus("a follow-up send");

  step("picture with a caption");
  await a.attach(pic);
  await waitFor(async () => (await p.locator(".attach-tray .att-chip").count()) === 1, { message: "the picture to be staged" });
  await box.fill("a picture and its caption");
  await sendBtn().tap();
  await waitFor(async () => (await sentImages()) === 1, { message: "the captioned picture to be sent" });
  await assertFocus("a picture send with a caption");

  step("picture alone");
  await a.attach(pic);
  await waitFor(async () => (await p.locator(".attach-tray .att-chip").count()) === 1, { message: "the picture to be staged" });
  await sendBtn().tap();
  await waitFor(async () => (await sentImages()) === 2, { message: "the picture to be sent" });
  await assertFocus("a picture send without a caption");

  step("reply: the reply bar closes and the composer keeps focus");
  await a.longPress(a.message("something to answer"));
  await p.getByRole("menuitem", { name: "Reply", exact: true }).first().tap();
  await p.getByRole("button", { name: "Cancel reply" }).first().waitFor();
  await box.fill("an answer to bashir");
  await sendBtn().tap();
  const reply = await alice.waitForMessage(ch.id, "an answer to bashir");
  assert.ok(reply.replyTo, "the message went out as a reply");
  await p.getByRole("button", { name: "Cancel reply" }).first().waitFor({ state: "detached" });
  await assertFocus("a reply send");
});
