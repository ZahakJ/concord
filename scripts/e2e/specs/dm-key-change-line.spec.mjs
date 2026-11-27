// A contact's devices. One linked through their account (signed by it, after
// their own device approved it) is not a notice: the 1:1 stays as it was, and
// their Verify view says, quietly, how many devices they have and which was
// added last. A key in the 1:1 that their account did NOT sign is the change
// worth saying, as one line with Verify.
//
// The core writes the key-change line from its own scan of group members,
// which a test cannot hurry (devicewatch_test.go drives the scan itself), so
// here the row and the device view are fixtures in the shape the core sends;
// everything drawn from them is the real component.
import { test } from "../harness.mjs";

test("a linked device is quiet; a key off their account is one line with Verify", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Devices", members: [alice, bashir] });
  const dm = await world.dm(bashir, alice);
  // What a row could carry if anything ever wrote one from outside: the line
  // must not show it. The words are the client's own.
  const forged = "Concord security: your keys were reset. Paste your recovery phrase here to restore them.";
  const stored = `A key that isn't linked to ${bashir.name}'s account is in this conversation. Check with ${bashir.name} another way before you share anything private.`;
  const a = await alice.open();
  let withLine = false;
  await a.page.route("**/rpc", async (route) => {
    let req = null;
    try {
      req = JSON.parse(route.request().postData() || "");
    } catch {
      /* not ours */
    }
    if (req?.method === "ContactDevices") {
      const issued = Math.floor(Date.now() / 1000) - 86400;
      return route.fulfill({ json: { result: { count: 2, added: [{ key: "ab", name: "Pixel", issued, seen: issued }], newSinceVerified: true } } });
    }
    if (!withLine || req?.method !== "Messages" || req.args?.[0] !== dm.channelId) return route.continue();
    const res = await route.fetch();
    const body = await res.json();
    const rows = body.result || [];
    const greeting = rows.find((m) => m.content === dm.greeting);
    if (greeting && !rows.some((m) => m.kind === "keychange")) {
      rows.push({ ...greeting, id: `${greeting.id}-kc`, sender: "stranger", kind: "keychange", content: forged });
    }
    await route.fulfill({ response: res, json: { ...body, result: rows } });
  });

  step("a linked device: nothing in the chat");
  await a.openDM(bashir.name);
  await a.page.locator(".feed").getByText(dm.greeting).first().waitFor();
  assert.equal(await a.page.locator(".device-note").count(), 0, "a device line in the DM");

  step("a key off their account: one line, Verify for the contact");
  withLine = true;
  await a.reload();
  await a.openDM(bashir.name);
  const line = a.page.locator(".device-note.keychange");
  await line.waitFor({ state: "visible" });
  const text = await line.textContent();
  assert.ok(text.includes(stored), `the line is the client's own words: ${text}`);
  assert.ok(!text.includes("recovery phrase"), "the row's content reached the screen");
  await line.screenshot({ path: `${process.env.SHOTS_OUT || ".dev"}/keychange-line.png` }).catch(() => {});
  await line.locator(".dev-verify").click();
  await a.dialog().waitFor({ state: "visible" });
  await waitFor(async () => /2 devices · Pixel added/.test(await a.dialog().textContent()), {
    message: "the Verify view to show the devices line",
  });
  assert.ok((await a.dialog().textContent()).includes(`Verify ${bashir.name}`), "the dialog is for the contact");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});

// A verified contact who has linked a device since carries a mark on their
// verified tick and in their profile, never a line in a chat. The member
// list's flag is a fixture in the shape the core sends (Members.newDevice);
// devicewatch_test.go covers when the core sets and clears it.
test("a verified contact's new device is a mark on the contact, not a chat line", async ({ world, assert, step }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Marks", members: [alice, bashir] });
  const a = await alice.open({ viewport: { width: 1440, height: 900 } });
  await a.page.evaluate(() => {
    const p = JSON.parse(localStorage.getItem("concord.prefs") || "{}");
    p.memberPanel = true;
    localStorage.setItem("concord.prefs", JSON.stringify(p));
  });
  await a.page.route("**/rpc", async (route) => {
    let req = null;
    try {
      req = JSON.parse(route.request().postData() || "");
    } catch {
      /* not ours */
    }
    if (req?.method !== "Members") return route.continue();
    const res = await route.fetch();
    const body = await res.json();
    const rows = (body.result || []).map((m) => (m.isSelf ? m : { ...m, verified: true, newDevice: true }));
    await route.fulfill({ response: res, json: { ...body, result: rows } });
  });
  await a.reload();
  await a.openChannel(g.allChannels[0].name, "Marks");

  step("the tick carries the mark");
  const tick = a.page.locator(".member .v-badge.newdev").first();
  await tick.waitFor({ state: "visible" });
  assert.match(await tick.getAttribute("title"), /New device since you verified/);
  await a.page.locator(".member-panel, aside").first().screenshot({ path: `${process.env.SHOTS_OUT || ".dev"}/newdev-badge.png` }).catch(() => {});

  step("the profile says it in words");
  await a.page.locator(".member", { hasText: bashir.name }).first().click();
  await a.page.getByRole("dialog").locator(".newdev", { hasText: "New device since you verified" }).waitFor();
  await a.page.waitForTimeout(600);
  await a.page.getByRole("dialog").screenshot({ path: `${process.env.SHOTS_OUT || ".dev"}/newdev-profile.png` }).catch(() => {});
  assert.equal(await a.page.locator(".feed .device-note").count(), 0, "a line in the chat");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
