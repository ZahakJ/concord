// The line a 1:1 shows when the contact's account gains a device. Unverified,
// it offers Verify. Verified, it says "Verified" for the person and still marks
// the device "New device, not checked": verifying compares the account, which
// a linked device does not change, so nobody has checked the device.
//
// The core writes this line from its own scan of group members, on a cadence
// a test cannot hurry, so the row is a fixture: the page's Messages call for
// the DM gets one "device" row added, copied from the real greeting so it has
// the shape the core sends. Everything drawn from it is the real component.
import { test } from "../harness.mjs";

test("new-device line: Verify, then Verified with the device still not checked", async ({ world, assert, step, waitFor }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Devices", members: [alice, bashir] });
  const dm = await world.dm(bashir, alice);
  const stored = `${bashir.name} added a new device (Pixel, 11-14). If this wasn't ${bashir.name}, verify.`;

  const a = await alice.open();
  await a.page.route("**/rpc", async (route) => {
    let req = null;
    try {
      req = JSON.parse(route.request().postData() || "");
    } catch {
      /* not ours */
    }
    if (req?.method !== "Messages" || req.args?.[0] !== dm.channelId) return route.continue();
    const res = await route.fetch();
    const body = await res.json();
    const rows = body.result || [];
    const greeting = rows.find((m) => m.content === dm.greeting);
    if (greeting && !rows.some((m) => m.kind === "device")) {
      rows.push({ ...greeting, id: `${greeting.id}-device`, kind: "device", content: stored });
    }
    await route.fulfill({ response: res, json: { ...body, result: rows } });
  });

  const line = a.page.locator(".device-note");
  const openLine = async () => {
    await a.openDM(bashir.name);
    await line.waitFor({ state: "visible" });
  };

  step("not verified: the line as written, and Verify");
  await openLine();
  assert.equal((await line.locator(".dev-verify").textContent()).trim(), "Verify");
  assert.equal(await line.locator(".dev-tag, .dev-verified").count(), 0);
  assert.ok((await line.textContent()).includes(stored), await line.textContent());

  step("Verify opens the QR check for that person");
  await line.locator(".dev-verify").click();
  await a.dialog().waitFor({ state: "visible" });
  await a.closeModals();

  step("verified: Verified for the person, the device still new and not checked");
  await alice.rpc("VerifyFingerprint", bashir.fingerprint);
  await a.reload();
  await openLine();
  await waitFor(async () => (await line.locator(".dev-verified").count()) === 1, {
    message: "the line to show the contact as verified",
  });
  assert.equal(await line.locator(".dev-verify").count(), 0, "Verify still offered for a verified contact");
  assert.equal((await line.locator(".dev-tag").textContent()).trim(), "New device, not checked");
  const mark = line.locator(".dev-verified");
  assert.match((await mark.textContent()).trim(), /^Verified\b/);
  assert.equal(
    await mark.getAttribute("title"),
    `You verified ${bashir.name}. That checked their account, not this device.`,
  );
  const text = await line.textContent();
  assert.ok(text.includes(`${bashir.name} added a new device (Pixel, 11-14).`), text);
  assert.ok(!/verify\./.test(text), `the line still asks for a check: ${text}`);
  // The tag about the device comes before the mark about the person.
  const order = await line.evaluate((el) => {
    const tag = el.querySelector(".dev-tag");
    const ok = el.querySelector(".dev-verified");
    return !!(tag.compareDocumentPosition(ok) & Node.DOCUMENT_POSITION_FOLLOWING);
  });
  assert.ok(order, "the not-checked tag should come before the Verified mark");
  assert.deepEqual(a.errors, [], a.errors.join("\n"));
});
