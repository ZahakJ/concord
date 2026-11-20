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

async function sinkMissing(app) {
  await app.ctx.route("**/rpc", async (route) => {
    let method = "";
    try {
      method = JSON.parse(route.request().postData() || "{}").method;
    } catch {}
    if (method !== "MediaSupport") return route.continue();
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ result: { inline: false, missing: ["autoaudiosink"] } }),
    });
  });
  await app.reload();
}

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
  await shot(a, "media as files");
});
