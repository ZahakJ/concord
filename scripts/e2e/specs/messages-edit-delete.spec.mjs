// An edit and a delete made in one window reach the other member's window.
import { test } from "../harness.mjs";

test("edit and delete are seen by the other member", async ({ world, assert, step, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Edits", members: [alice, bashir] });
  const ch = g.allChannels[0];
  const a = await alice.open();
  const b = await bashir.open();
  await a.openChannel(ch.name, "Edits");
  await b.openChannel(ch.name, "Edits");

  await a.send("first draft of a thought");
  await b.waitForMessage("first draft of a thought");

  step("Alice edits; Bashir sees the new text and not the old");
  await a.editMessage("first draft of a thought", "second draft, much better");
  await b.waitForMessage("second draft, much better");
  // The new text and nothing else. An edit box that mounted empty and was
  // filled a moment later joined a quick correction onto the old text and
  // saved "first draft of a thoughtsecond draft, much better".
  for (const app of [a, b]) {
    const [body, html] = await app.message("second draft, much better").evaluate((el) => {
      const c = el.cloneNode(true);
      c.querySelectorAll(".edited-tag").forEach((t) => t.remove());
      return [c.textContent.trim(), el.innerHTML.slice(0, 500)];
    });
    assert.equal(body, "second draft, much better", html);
  }
  await b.waitForNoMessage("first draft of a thought");
  const edited = await bashir.waitForMessage(ch.id, "second draft, much better");
  assert.equal(edited.edited, true);

  step("Alice deletes; the text leaves Bashir's window");
  await a.send("this one is a mistake");
  await b.waitForMessage("this one is a mistake");
  const victim = await alice.waitForMessage(ch.id, "this one is a mistake");
  await a.deleteMessage("this one is a mistake");
  await b.waitForNoMessage("this one is a mistake");
  await bashir.waitForMessage(ch.id, (m) => m.id === victim.id && m.deleted);
  await shot(b, "bashir after edit and delete");
});
