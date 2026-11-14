// A copy of Concord built with a rendezvous starts from it: a device restored
// from the phrase reaches its guild-mate and comes back as itself with nothing
// typed but the phrase and a passphrase, and Settings says where the address
// came from.
import { test } from "../harness.mjs";
import { execFileSync } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const AVATAR =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAIAAABLbSncAAAAEklEQVR4nGP4z8CAFWEXHbQSACj/P8Fu7N9hAAAAAElFTkSuQmCC";

test("a build with a built-in rendezvous restores with zero typing", async ({ world, assert, waitFor, step, shot }) => {
  step("build a copy with the test rendezvous built in");
  const bin = path.join(world.dir, "concord-web-builtin");
  execFileSync("go", ["build", "-o", bin, "-ldflags", `-X github.com/ZahakJ/concord/internal/version.Rendezvous=${world.bootstrap}`, "."], {
    cwd: ROOT,
    stdio: "inherit",
  });

  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Built In", members: [alice, bashir] });
  await alice.rpc("SetProfile", "Alice", "here", "🦉", "#3366ff", AVATAR, "", "", "Built-in bio.");
  await waitFor(async () => (await bashir.rpc("Members", g.id)).some((m) => m.fingerprint === alice.fingerprint && m.avatar === AVATAR), {
    timeout: 60000,
    message: "Bashir to learn Alice's profile",
  });
  const phrase = await alice.rpc("RevealMnemonic", alice.passphrase);
  await alice.stop();

  step("a fresh install of the built-in copy, with no rendezvous in its environment");
  const [dev] = await world.peers(1, { names: ["BuiltIn"], start: false });
  dev.passphrase = "e2e-pass-builtin";
  dev.bin = bin;
  dev.env.CONCORD_BOOTSTRAP = "";
  await dev.start();
  const app = await dev.open({ unlock: false });
  const { page } = app;
  await page.goto(`${dev.url}/?t=${dev.token}`, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "I lost my devices — restore from my recovery phrase" }).click();
  const rdv = page.getByRole("textbox", { name: "Rendezvous address or an invite code" });
  assert.equal((await rdv.inputValue()).trim(), world.bootstrap, "the restore screen starts from the built-in rendezvous");
  await page.getByText("Filled in with this app's default").waitFor();
  await page.getByRole("textbox", { name: "Recovery phrase" }).fill(phrase);
  await page.getByPlaceholder("New passphrase").fill(dev.passphrase);
  await page.getByPlaceholder("Confirm passphrase").fill(dev.passphrase);
  await page.getByRole("button", { name: "Restore account" }).click();
  await page.getByRole("heading", { name: "Welcome back, Alice" }).waitFor({ timeout: 90000 });
  await shot(app, "builtin-found");
  await page.getByRole("button", { name: "Continue" }).click();
  await page.locator('nav[aria-label="Guilds"]').waitFor({ state: "attached", timeout: 45000 });
  const me = await dev.rpc("Identity");
  assert.equal(me.displayName, "Alice");
  assert.equal(me.avatar, AVATAR);

  step("Settings names where the address came from");
  await app.openSettings("Connection");
  const note = page.getByText("Built in to this copy of Concord");
  await note.waitFor();
  await note.scrollIntoViewIfNeeded();
  await page.locator('[role="dialog"]').last().screenshot({ path: path.join(world.dir, "builtin-settings.png") });
});
