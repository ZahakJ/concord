// A video Concord holds back waits on its row for the person's choice. One
// with subtitles offers "Remove and send" and "Send as it is"; a container
// Concord cannot clean (Matroska here) offers "Send as it is" only. Nothing
// goes out before a button is pressed. A thread started with such a file is
// not created around it: the dialog says to send it from the message box.
import fs from "node:fs";
import path from "node:path";
import { test } from "../harness.mjs";

const u32 = (v) => {
  const b = Buffer.alloc(4);
  b.writeUInt32BE(v >>> 0);
  return b;
};
const box = (typ, ...parts) => {
  const body = Buffer.concat(parts);
  return Buffer.concat([u32(8 + body.length), Buffer.from(typ, "latin1"), body]);
};
const full = (typ, ...parts) => box(typ, Buffer.alloc(4), ...parts);

// An MP4 with a picture track, a subtitle track and a location in its user
// data (internal/app/mediaclean_test.go builds the same file).
function subtitledVideo() {
  const ftyp = box("ftyp", Buffer.from("isom"), u32(512), Buffer.from("isom"));
  const at = ftyp.length + 8;
  const trak = (h, off, n) => {
    const stbl = box("stbl", full("stsd", u32(0)), full("stts", u32(0)),
      full("stsc", u32(1), u32(1), u32(1), u32(1)),
      full("stsz", u32(0), u32(1), u32(n)),
      full("stco", u32(1), u32(off)));
    const minf = box("minf", box("dinf", full("dref", u32(1), box("url ", Buffer.from([0, 0, 0, 1])))), stbl);
    const mdia = box("mdia", full("mdhd", Buffer.alloc(20)), full("hdlr", u32(0), Buffer.from(h), Buffer.alloc(13)), minf);
    return box("trak", full("tkhd", Buffer.alloc(80)), mdia);
  };
  const moov = box("moov", full("mvhd", Buffer.alloc(96)), trak("vide", at, 32), trak("sbtl", at + 32, 24),
    box("udta", box("\xa9xyz", Buffer.from("+48.8584+002.2945/"))));
  return Buffer.concat([ftyp, box("mdat", Buffer.alloc(32, "V"), Buffer.from("SUBTITLE: meet at home..")), moov]);
}

test("a held video waits for the person's choice on its row", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Clips", members: [alice, bashir] });
  const general = g.channels.general;
  const subs = path.join(world.dir, "subs.mp4");
  fs.writeFileSync(subs, subtitledVideo());
  const mkv = path.join(world.dir, "dashcam.mkv");
  fs.writeFileSync(mkv, Buffer.concat([Buffer.from([0x1a, 0x45, 0xdf, 0xa3, 0x9f, 0x42, 0x86, 0x81, 0x01]), Buffer.from("GPS 48.8584")]));

  const a = await alice.open();
  await a.openChannel("general", "Clips");
  const files = async () => (await alice.messages(general)).filter((m) => m.content.includes("concord://file/")).length;
  // Reading a picked file takes a moment, and an Enter pressed before it is
  // staged has nothing to send. Wait for the finished chip, not the spinner
  // that shares its class.
  const stage = async (file) => {
    await a.attach(file);
    await waitFor(async () => (await a.page.locator(".attach-tray .att-chip:not(.loading)").count()) === 1, { message: "the file to be staged" });
  };

  step("a video with subtitles is held with both choices, and nothing is sent");
  await stage(subs);
  await a.composer().press("Enter");
  const row = a.page.locator(".pmsg").filter({ hasText: "subtitles or other text tracks" });
  await row.waitFor();
  await row.getByRole("button", { name: "Remove and send" }).waitFor();
  await row.getByRole("button", { name: "Send as it is" }).waitFor();
  assert.equal(await files(), 0, "nothing may go out before a choice");
  await shot(a, "held for subtitles");

  step("Remove and send sends the cleaned video");
  await row.getByRole("button", { name: "Remove and send" }).click();
  await waitFor(async () => (await files()) === 1, { message: "the cleaned video to be sent" });
  await row.waitFor({ state: "detached" });

  step("Send as it is sends it unchanged");
  await stage(subs);
  await a.composer().press("Enter");
  await row.waitFor();
  await row.getByRole("button", { name: "Send as it is" }).click();
  await waitFor(async () => (await files()) === 2, { message: "the video to be sent as it is" });

  step("a Matroska file is held with Send as it is only");
  await stage(mkv);
  await a.composer().press("Enter");
  const row2 = a.page.locator(".pmsg").filter({ hasText: "couldn't remove the details stored in this video" });
  await row2.waitFor();
  assert.equal(await row2.getByRole("button", { name: "Remove and send" }).count(), 0);
  assert.equal(await files(), 2);
  await shot(a, "held container");
  await row2.getByRole("button", { name: "Send as it is" }).click();
  await waitFor(async () => (await files()) === 3, { message: "the Matroska file to be sent as it is" });

  step("a thread started with a held video is not created");
  const before = (await alice.messages(general)).length;
  await a.page.getByRole("button", { name: "Add to your message" }).click();
  await a.page.getByRole("menuitem", { name: /^Thread/ }).click();
  const dialog = a.page.getByRole("dialog");
  await dialog.getByLabel("Post title").fill("Footage from the trip");
  await dialog.getByPlaceholder("Start the discussion…").fill("The clip from the ridge.");
  await dialog.locator('input[type="file"]').first().setInputFiles(subs);
  await waitFor(async () => (await dialog.getByText("subs.mp4").count()) > 0, { message: "the file to be staged in the dialog" });
  await dialog.locator("button.primary.go").click();
  await a.page.getByText("Attach it in the message box").first().waitFor();
  assert.equal(await dialog.count(), 1, "the dialog stays open");
  assert.equal((await alice.messages(general)).length, before, "no thread opener may be posted");
  await shot(a, "thread refused before it exists");
});
