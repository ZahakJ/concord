// A recovery phrase brings back the account key and nothing else. A device
// restored from it used to ask "What should people call you?" and save the
// answer as the whole profile — and the account's other devices adopted that,
// wiping the avatar and bio everywhere. Now the restored device gets the profile
// the account signed back, from its other device or from a guild-mate, and the
// restore screen shows it without anyone typing.
import { test } from "../harness.mjs";

// 8x8 PNG.
const AVATAR =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAIAAABLbSncAAAAEklEQVR4nGP4z8CAFWEXHbQSACj/P8Fu7N9hAAAAAElFTkSuQmCC";

async function setLook(peer) {
  // SetProfile(name, status, emoji, color, avatar, banner, presence, bio)
  await peer.rpc("SetProfile", peer.name, "reading the canon", "🦉", "#3366ff", AVATAR, "", "", "Physician and philosopher.");
}

// Restores phrase on a fresh peer through the real restore screen and returns
// the open page, sitting on whatever the screen shows after "Restore account".
async function restoreThroughUI(world, phrase) {
  const [dev] = await world.peers(1, { names: ["Restored"], start: false });
  dev.passphrase = "e2e-pass-restored";
  await dev.start();
  const app = await dev.open({ unlock: false });
  const { page } = app;
  await page.goto(`${dev.url}/?t=${dev.token}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Restore from a recovery phrase" }).click();
  await page.getByRole("textbox", { name: "Recovery phrase" }).fill(phrase);
  await page.getByPlaceholder("New passphrase").fill(dev.passphrase);
  await page.getByPlaceholder("Confirm passphrase").fill(dev.passphrase);
  await page.getByRole("button", { name: "Restore account" }).click();
  return { dev, app, page };
}

async function expectProfileBack({ dev, app, page }, name, { assert, step, shot }) {
  step("the restore screen fills the profile in by itself");
  await page.getByRole("heading", { name: `Welcome back, ${name}` }).waitFor({ timeout: 90000 });
  const av = page.locator(".found img.found-av");
  assert.equal(await av.getAttribute("src"), AVATAR, "the avatar that came back is shown");
  await shot(app, "restore-found");
  assert.equal(await page.getByLabel("Your name").count(), 0, "nothing asks for a name once the profile is back");
  await page.getByRole("button", { name: "Continue" }).click();
  await page.locator('nav[aria-label="Guilds"]').waitFor({ state: "attached", timeout: 45000 });
  const me = await dev.rpc("Identity");
  assert.equal(me.displayName, name);
  assert.equal(me.avatar, AVATAR);
  assert.equal(me.bio, "Physician and philosopher.");
  assert.equal(me.status, "reading the canon");
}

test("guild-mate hands a phrase-restored device its profile back", async ({ world, assert, waitFor, step, shot }) => {
  const [alice, bashir] = await world.peers(2);
  await world.guild({ name: "Keepers", members: [alice, bashir] });
  await setLook(alice);
  await waitFor(
    async () => {
      const ms = await bashir.rpc("Members", (await bashir.guilds()).find((g) => g.name === "Keepers").id);
      return ms.some((m) => m.fingerprint === alice.fingerprint && m.avatar === AVATAR);
    },
    { timeout: 60000, message: "Bashir to learn Alice's profile" },
  );
  const phrase = await alice.rpc("RevealMnemonic", alice.passphrase);
  step("Alice's only device is gone; she restores from her phrase");
  await alice.stop();
  const r = await restoreThroughUI(world, phrase);
  await expectProfileBack(r, "Alice", { assert, step, shot });
});

test("linked device hands a phrase-restored device its profile back, and keeps its own", async ({ world, assert, waitFor, step, shot }) => {
  const [alice] = await world.peers(1);
  const phone = await world.linkDevice(alice, { name: "AlicePhone" });
  await setLook(alice);
  await waitFor(async () => (await phone.rpc("Identity")).avatar === AVATAR, {
    timeout: 60000,
    message: "the phone to have Alice's profile",
  });
  const phrase = await alice.rpc("RevealMnemonic", alice.passphrase);
  step("the desktop is gone; the phone is on; Alice restores from her phrase");
  await alice.stop();
  const r = await restoreThroughUI(world, phrase);
  await expectProfileBack(r, "Alice", { assert, step, shot });
  step("and the phone's copy is untouched");
  const p = await phone.rpc("Identity");
  assert.equal(p.avatar, AVATAR);
  assert.equal(p.bio, "Physician and philosopher.");
});
