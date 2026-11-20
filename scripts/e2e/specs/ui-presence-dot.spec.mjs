// The presence dot is a mark on the avatar, not a second avatar: at every
// size it appears (member list, conversation list, the self panel, the
// profile card) it stays under two fifths of the avatar's width, keeps a
// coloured disc wide enough to read, and keeps the cut-out ring that parts it
// from the picture. It was a flat 14px everywhere, half of a 28px member row.
import { test } from "../harness.mjs";

const measure = (page) =>
  page.evaluate(() =>
    [...document.querySelectorAll(".avatar .dot")]
      .filter((d) => d.getBoundingClientRect().width > 0)
      .map((d) => {
        // Layout sizes, not painted ones: the profile card scales in.
        const av = d.closest(".avatar").offsetWidth;
        const s = getComputedStyle(d);
        const w = d.offsetWidth || parseFloat(s.width);
        const ring = parseFloat(s.borderTopWidth);
        return { avatar: Math.round(av), dot: w, ring, disc: w - 2 * ring };
      }),
  );

test("the presence dot is in proportion to its avatar at every size", async ({ world, assert, step, shot }) => {
  const [alice, bashir] = await world.peers(2);
  const g = await world.guild({ name: "Workshop", members: [alice, bashir] });
  await world.dm(alice, bashir);
  await bashir.send(g.allChannels[0].id, "hello");
  const a = await alice.open({ viewport: "desktop" });
  await a.openChannel(g.allChannels[0].name || "general", "Workshop");
  await a.waitForMessage("hello");
  const seen = [];

  step("the member list and the self panel");
  seen.push(...(await measure(a.page)));
  step("the profile card");
  await a.page.locator(".mname", { hasText: /Bashir/ }).first().click();
  await a.page.locator(".avatar .dot").nth(3).waitFor();
  seen.push(...(await measure(a.page)));
  await shot(a, "profile card");
  await a.closeModals();
  step("the conversation list");
  await a.openDM("Bashir");
  seen.push(...(await measure(a.page)));
  await shot(a, "conversation list");

  const sizes = new Set(seen.map((x) => x.avatar));
  for (const n of [28, 32, 72]) assert.ok(sizes.has(n), `a dot on a ${n}px avatar was measured (${[...sizes]})`);
  for (const x of seen) {
    const at = JSON.stringify(x);
    assert.ok(x.dot / x.avatar <= 0.4, `the dot is at most two fifths of its avatar: ${at}`);
    assert.ok(x.disc >= 5, `its coloured disc stays legible: ${at}`);
    assert.ok(x.ring >= 1.5, `it keeps its cut-out ring: ${at}`);
  }
});
