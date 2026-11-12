// One rule for the owner's mark: a small filled crown beside the name of
// everything you own — a guild, an instant meeting's guild, a group you
// started — in the switcher, the place header, the message list, the
// conversation header, the Rail layout's tiles and by the owner in member
// lists. Never on the 28px strip tiles and never a ring: a mark there reads
// as a notification. A one-to-one conversation has no owner. The owner's mark
// never shares a class or a spot with the unread dot or the mention count.
import { test } from "../harness.mjs";

const MARK = ".owner-mark";
const STATE_MARKS = ".dot, .badge, .t-dot, .t-badge, .p-dot, .p-badge";

// Two boxes overlap when they share any area.
const overlaps = (a, b) => a && b && a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;

test("everything you own wears the crown beside its name, and nothing else does", async ({ world, assert, step, waitFor, shot }) => {
  const [alice, bashir, chen] = await world.peers(3);
  const mine = await world.guild({ name: "Workshop", members: [alice, bashir, chen] });
  const theirs = await world.guild({ name: "Bench", members: [bashir, alice] });
  const meeting = (await alice.rpc("StartMeeting")).guild;
  for (const [p, q] of [[alice, bashir], [alice, chen], [bashir, alice], [chen, alice]]) await p.rpc("VerifyFingerprint", q.fingerprint);
  await world.dm(alice, bashir);
  const group = await alice.rpc("CreateGroupDM", [bashir.fingerprint, chen.fingerprint]);
  await alice.rpc("RenameDM", group.id, "Kiln crew");
  await waitFor(async () => (await alice.guilds()).some((x) => x.id === group.id && x.name === "Kiln crew"), { message: "the group to be named" });

  const a = await alice.open({ viewport: "desktop" });
  await a.openGuild(meeting.name);
  // Something unread in each guild, so the state marks are on screen beside
  // the owner's.
  await bashir.send(mine.channel("general"), "anyone in the workshop?");
  await bashir.send(theirs.channel("general"), "bench is open");
  const strip = a.page.locator('nav[aria-label="Guilds"]');
  const unreadOn = (name) => strip.locator(".tile-wrap", { has: a.page.locator(`button.tile[aria-label="${name}"]`) }).locator(".t-dot, .t-badge");
  await waitFor(async () => (await unreadOn("Workshop").count()) && (await unreadOn("Bench").count()), { timeout: 60000, message: "both guilds to show unread" });

  step("the strip: no owner mark on any tile, no ring; the description still says so");
  assert.equal(await strip.locator(MARK).count(), 0, "no crown on the strip");
  assert.equal(await strip.locator(".owned").count(), 0, "no owned class on the strip");
  const ringOf = (name) =>
    strip.getByRole("button", { name, exact: true }).evaluate((el) => getComputedStyle(el).boxShadow);
  assert.equal(await ringOf("Workshop"), await ringOf("Bench"), "an owned tile is drawn like a member's");
  assert.match(await a.page.locator(`#strip-d-${mine.id}`).innerText(), /you own this guild/);
  assert.doesNotMatch(await a.page.locator(`#strip-d-${theirs.id}`).innerText(), /you own/);
  await shot(a, "strip without owner marks");

  step("the switcher: a crown right after the name of Workshop and the meeting, none on Bench");
  await strip.getByRole("button", { name: /^All guilds/ }).click();
  const row = (name) => a.page.locator("[role='option']", { hasText: name }).first();
  await row("Bench").waitFor();
  for (const name of ["Workshop", meeting.name]) {
    const r = row(name);
    assert.equal(await r.locator(MARK).count(), 1, `${name} has the crown`);
    assert.equal(await r.locator(MARK).getAttribute("aria-label"), "You own this guild");
    assert.equal(await r.locator(`.trail ${MARK}`).count(), 0, `${name}'s crown is not among the unread marks`);
    const crown = await r.locator(MARK).boundingBox();
    const label = await r.locator(".name").boundingBox();
    const gap = crown.x - (label.x + label.width);
    assert.ok(gap >= 0 && gap <= 12, `${name}'s crown sits right after the name (gap ${gap})`);
    for (const s of await r.locator(STATE_MARKS).all()) assert.ok(!overlaps(crown, await s.boundingBox()), `${name}'s crown is clear of its unread mark`);
  }
  assert.equal(await row("Bench").locator(MARK).count(), 0, "a member's guild has none");
  assert.ok((await row("Bench").locator(STATE_MARKS).count()) > 0, "Bench is unread, so the comparison means something");
  assert.equal(await a.page.locator(`${MARK}:is(${STATE_MARKS})`).count(), 0, "the crown shares no class with a state mark");
  await shot(a, "switcher crowns");
  await a.page.keyboard.press("Escape");

  step("the place header names the owner; not in a guild you only belong to");
  const head = a.page.locator("[data-place-head]");
  assert.equal(await head.locator(MARK).count(), 1, "the meeting's header has the crown");
  assert.equal(await head.locator("button.ph-main").getAttribute("aria-description"), "you own this guild");
  await a.openGuild("Workshop");
  await head.getByText("Workshop").waitFor();
  assert.equal(await head.locator(MARK).count(), 1, "Workshop's header has the crown");
  await a.openGuild("Bench");
  await head.getByText("Bench").waitFor();
  assert.equal(await head.locator(MARK).count(), 0, "Bench's header does not");

  step("the message list and the conversation header: the group you started, not the one-to-one");
  await a.openDM("Kiln crew");
  const groupRow = a.page.locator(".dm-item", { hasText: "Kiln crew" });
  assert.equal(await groupRow.locator(MARK).getAttribute("aria-label"), "You started this group");
  assert.equal(await a.page.locator(".dm-item", { hasText: "Bashir" }).first().locator(MARK).count(), 0, "a one-to-one has no owner");
  assert.equal(await a.page.locator(".row.title").locator(MARK).count(), 1, "the group's header has the crown");
  await shot(a, "group crown");

  step("the Rail layout: a crown in the tile's corner, clear of the unread dot");
  await a.page.evaluate(() => {
    const p = JSON.parse(localStorage.getItem("concord.prefs") || "{}");
    p.navModel = "rail";
    localStorage.setItem("concord.prefs", JSON.stringify(p));
  });
  await a.reload();
  await a.openGuild(meeting.name);
  await bashir.send(mine.channel("general"), "still here?");
  const rail = a.page.locator("nav.rail[aria-label='Guilds']");
  const wrap = (name) => rail.locator(".bubble-wrap", { has: a.page.locator(`button.pill[aria-label="${name}"]`) });
  await wrap("Bench").waitFor();
  for (const name of ["Workshop", meeting.name]) {
    assert.equal(await wrap(name).locator(MARK).count(), 1, `${name}'s rail tile has the crown`);
    assert.equal(await wrap(name).locator("button.pill").getAttribute("aria-description"), "you own this guild");
  }
  assert.equal(await wrap("Bench").locator(MARK).count(), 0, "Bench's rail tile has none");
  await waitFor(async () => (await wrap("Workshop").locator(STATE_MARKS).count()) > 0, { message: "Workshop's unread mark on the rail" });
  const crown = await wrap("Workshop").locator(MARK).boundingBox();
  for (const s of await wrap("Workshop").locator(STATE_MARKS).all()) {
    assert.ok(!overlaps(crown, await s.boundingBox()), "the crown and the unread mark do not share a spot");
  }
  await shot(a, "rail crowns");

  step("the member list: the crown beside the owner, as a member sees it");
  const b = await bashir.open({ viewport: "desktop" });
  await b.openChannel("general", "Workshop");
  const owner = b.page.locator(".member", { hasText: "Alice" }).locator(MARK);
  await owner.waitFor();
  assert.equal(await owner.getAttribute("aria-label"), "Guild owner");
  assert.equal(await b.page.locator(".member", { hasText: "Bashir" }).locator(MARK).count(), 0);
  await shot(b, "owner crown in members");
});
