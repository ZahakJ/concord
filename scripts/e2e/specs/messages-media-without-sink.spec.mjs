// On a Linux desktop whose GStreamer has no audio sink (gst-plugins-good not
// installed), WebKitGTK 2.54 crashes the whole page the moment an <audio> or
// <video> element loads anything, and then reloads it into the same crash. The
// desktop shell checks for the sink at startup and the page asks it through
// MediaSupport. Told the sink is missing, the page makes no media element at
// all: a clip and a voice message arrive as files to save, with one line
// saying why. Told nothing (every browser, and a healthy desktop), it behaves
// as before.
//
// The web build has no shell to ask, so this spec answers MediaSupport itself
// by intercepting that one RPC; everything else reaches the real peer.
import { test } from "../harness.mjs";

// Bytes are enough: what is under test is which element the page makes, not
// whether the clip decodes.
const clip = `data:video/mp4;base64,${Buffer.from("\0\0\0\x18ftypisom\0\0\x02\0isomiso2" + "V".repeat(64), "latin1").toString("base64")}`;
const voice = `data:audio/ogg;base64,${Buffer.from("OggS" + "\0".repeat(60), "latin1").toString("base64")}`;

// Counts every media element the page makes, in the DOM or not: `new Audio()`
// never enters the DOM, and it crashes the engine all the same.
function countMediaElements() {
  window.__mediaMade = 0;
  const make = Document.prototype.createElement;
  Document.prototype.createElement = function (tag, ...rest) {
    if (/^(audio|video)$/i.test(String(tag))) window.__mediaMade++;
    return make.call(this, tag, ...rest);
  };
  const A = window.Audio;
  window.Audio = function (...args) {
    window.__mediaMade++;
    return new A(...args);
  };
  window.Audio.prototype = A.prototype;
}

// `extra(method, req, route)` may answer another RPC; it returns true if it did.
async function sinkMissing(app, extra = async () => false) {
  await app.ctx.addInitScript(countMediaElements);
  await app.ctx.route("**/rpc", async (route) => {
    let req = {};
    try {
      req = JSON.parse(route.request().postData() || "{}");
    } catch {}
    if (req.method === "MediaSupport")
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ result: { inline: false, missing: ["autoaudiosink"] } }),
      });
    if (await extra(req, route)) return;
    return route.continue();
  });
  await app.reload();
}

const made = (app) => app.page.evaluate(() => window.__mediaMade);

test("without an audio sink, clips and voice messages arrive as files, not players", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Clips", members: [alice, bashir] });
  const general = g.channels.general;
  await bashir.rpc("SendFileAsIs", general, clip, "ridge.mp4", "", "", "");
  await bashir.rpc("SendFileAsIs", general, voice, "hello.ogg", "", "", "");
  await waitFor(async () => (await alice.messages(general)).filter((m) => m.content.includes("concord://file/")).length === 2, {
    message: "both files to reach alice",
  });

  step("a page on a healthy engine plays them inline, as before");
  const ok = await alice.open();
  await ok.openChannel("general", "Clips");
  await ok.page.locator(".vid-embed").first().waitFor();
  await waitFor(async () => (await ok.page.locator(".feed video").count()) === 1, { message: "the clip's player" });
  assert.equal(await ok.page.getByText("Plays outside Concord").count(), 0, "no note on a healthy engine");
  await ok.close();

  step("told the sink is missing, the page makes no <video> or <audio>");
  const a = await alice.open();
  await sinkMissing(a);
  await a.openChannel("general", "Clips");
  const cards = a.page.locator(".feed .file-card");
  await waitFor(async () => (await cards.count()) === 2, { message: "both files shown as file cards" });
  await cards.filter({ hasText: "ridge.mp4" }).waitFor();
  await cards.filter({ hasText: "hello.ogg" }).waitFor();
  const notes = a.page.locator(".feed").getByText("gst-plugins-good");
  assert.equal(await notes.count(), 2, "each card says why it is not played here");
  // Long enough for the clip's IntersectionObserver and any fetch to have run.
  await a.page.waitForTimeout(1500);
  assert.equal(await a.page.locator("video, audio").count(), 0, "no media element in the page");
  assert.equal(await a.page.locator(".vid-embed, .vm").count(), 0, "no inline player");
  assert.equal(await made(a), 0, "no media element made at all");
  await shot(a, "media as files");
});

test("without an audio sink, the camera test, the QR scanner and a recording's playback make no media element", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Devices", members: [alice, bashir] });
  const dm = await world.dm(bashir, alice);
  const a = await alice.open();
  // A new-device line in the DM is the way to a QR scanner from a logged-in
  // page (dm-new-device-line.spec.mjs puts it there the same way).
  await sinkMissing(a, async (req, route) => {
    if (req.method !== "Messages" || req.args?.[0] !== dm.channelId) return false;
    const res = await route.fetch();
    const body = await res.json();
    const rows = body.result || [];
    const greeting = rows.find((m) => m.content === dm.greeting);
    if (greeting && !rows.some((m) => m.kind === "device"))
      rows.push({ ...greeting, id: `${greeting.id}-device`, kind: "device", content: `${bashir.name} added a new device (Pixel, 11-14). If this wasn't ${bashir.name}, verify.` });
    await route.fulfill({ response: res, json: { ...body, result: rows } });
    return true;
  });

  step("the camera test shows the reason, not a preview");
  const dlg = await a.openSettings("Voice & video");
  const cam = dlg.locator("section.dev").filter({ hasText: "Camera" });
  await cam.locator("button.test").click();
  await cam.getByText("The camera can't be shown here").waitFor();
  assert.ok((await cam.textContent()).includes("then restart Concord"), "the note says a restart is needed");
  assert.equal(await a.page.locator("video").count(), 0, "no camera preview");
  await shot(a, "camera test without sink");
  await a.closeModals();

  step("the QR scanner says why, and pasting stays offered");
  await a.openDM(bashir.name);
  const line = a.page.locator(".device-note");
  await line.waitFor({ state: "visible" });
  await line.locator(".dev-verify").click();
  await a.dialog().getByRole("button", { name: "Scan theirs" }).click();
  const scanner = a.page.getByRole("dialog", { name: "Scan QR code" });
  await scanner.getByText("The camera can't be shown here").waitFor();
  await scanner.getByText("paste the code instead").waitFor();
  assert.equal(await scanner.locator("video").count(), 0, "no camera element in the scanner");
  await shot(a, "scanner without sink");
  await scanner.getByRole("button", { name: "Close scanner" }).click();
  await a.closeModals();

  step("a recorded voice message is not played back here, and says why");
  const rec = a.page.getByRole("button", { name: "Record a voice message" });
  await rec.click();
  const stopBtn = a.page.getByRole("button", { name: "Stop recording" });
  await stopBtn.waitFor();
  await a.page.waitForTimeout(1200);
  await stopBtn.click();
  await a.page.getByRole("button", { name: "Play back your recording" }).click();
  await a.page.getByText("Plays outside Concord").first().waitFor();
  assert.equal(await made(a), 0, "no media element made at all");
});
