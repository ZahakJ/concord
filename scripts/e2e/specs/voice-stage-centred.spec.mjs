// A voice room is one centred column in every state: before you join (the
// headline, Join and who is in there), once you are in it, and with the call
// stage open, alone or with someone else. The presence line used to be centred
// across the whole action row while Join started at the leading edge, so the
// two sat tens of pixels apart under a left-aligned headline.
import { test } from "../harness.mjs";

// Horizontal centres of the room's parts, each minus the centre of its
// column's content box: the feed before the stage opens, the stage after.
// Text is measured by its ink (a Range over its contents), not its box.
function centres() {
  const cbox = (el) => {
    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    const left = r.left + el.clientLeft + parseFloat(cs.paddingLeft);
    return left + (el.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight)) / 2;
  };
  const shown = (el) => el && el.getClientRects().length > 0 && getComputedStyle(el).visibility !== "hidden";
  const box = (el) => (shown(el) ? el.getBoundingClientRect() : null);
  const ink = (el) => {
    if (!shown(el)) return null;
    const range = document.createRange();
    range.selectNodeContents(el);
    const rs = [...range.getClientRects()].filter((r) => r.width > 0);
    return rs.length ? { left: Math.min(...rs.map((r) => r.left)), right: Math.max(...rs.map((r) => r.right)) } : null;
  };
  const union = (els) => {
    const rs = els.filter(shown).map((e) => e.getBoundingClientRect()).filter((r) => r.width > 0);
    return rs.length ? { left: Math.min(...rs.map((r) => r.left)), right: Math.max(...rs.map((r) => r.right)) } : null;
  };
  const stage = document.querySelector(".voice-panel.on-stage");
  const col = stage || document.querySelector(".feed");
  const mid = cbox(col);
  const out = {};
  const put = (k, r) => {
    if (r) out[k] = Math.round(((r.left + r.right) / 2 - mid) * 10) / 10;
  };
  if (stage) {
    put("waiting", box(stage.querySelector(".ringing")));
    put("tiles", union([...stage.querySelectorAll(".stage .tile")]));
    put("controls", union([...stage.querySelectorAll(".controls > *")]));
  } else {
    put("headline", ink(col.querySelector(".empty h3")));
    put("join", box(col.querySelector(".join-call")));
    put("presence", ink(col.querySelector(".who-in")));
  }
  return { column: stage ? "stage" : "feed", ...out };
}

for (const viewport of ["desktop", "tablet", "phone"]) {
  test(`${viewport}: the voice room's parts share one centre before and after joining`, async ({ world, assert, step, waitFor, shot }) => {
    const [alice, bashir] = await world.peers(2);
    const g = await world.guild({ name: "Studio", members: [alice, bashir], channels: [{ name: "Lounge", type: "voice" }] });
    const lounge = g.channel("Lounge");
    const a = await alice.open({ viewport });
    const phone = viewport === "phone";
    const tol = phone ? 2 : 1;
    const check = async (state, want) => {
      await a.page.waitForTimeout(400);
      const m = await a.page.evaluate(centres);
      step(`${state}: ${JSON.stringify(m)}`);
      for (const k of want) {
        assert.ok(typeof m[k] === "number", `${state}: ${k} is on screen`);
        assert.ok(Math.abs(m[k]) <= tol, `${state}: ${k} is ${m[k]}px off the column's centre`);
      }
    };

    step("Open chat on Lounge: the room before joining");
    await a.openGuild("Studio");
    const row = a.page.locator(".channel", { hasText: "Lounge" }).first();
    if (phone) {
      await row.waitFor();
      const b = await row.boundingBox();
      const cdp = await a.page.context().newCDPSession(a.page);
      const pt = { x: b.x + Math.min(120, b.width / 2), y: b.y + b.height / 2 };
      await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [pt] });
      await a.page.waitForTimeout(650);
      await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
      await cdp.detach().catch(() => {});
      await a.page.getByText("Open chat", { exact: true }).first().click();
    } else {
      await row.click({ button: "right" });
      await a.page.getByRole("menuitem", { name: "Open chat" }).click();
    }
    const join = a.page.locator("button.join-call");
    await join.waitFor();
    await a.page.locator(".who-in", { hasText: "Nobody's in there yet" }).waitFor();
    await check("pre-join", ["headline", "join", "presence"]);
    await shot(a, "pre-join");

    step("Bashir is in there");
    await bashir.rpc("JoinVoice", lounge, "e2e-bashir");
    await a.page.locator(".who-in", { hasText: "Bashir is in there" }).waitFor({ timeout: 45000 });
    await check("pre-join with one", ["headline", "join", "presence"]);
    await bashir.rpc("LeaveVoice", lounge, "");
    await a.page.locator(".who-in", { hasText: "Nobody's in there yet" }).waitFor({ timeout: 45000 });

    step("Alice joins alone");
    await join.click();
    await a.page.locator(".who-in", { hasText: "Alice is in there" }).waitFor({ timeout: 45000 });
    await check("joined", ["headline", "join", "presence"]);
    await join.click(); // Open the call
    await a.page.locator(".voice-panel.on-stage .ringing").waitFor({ timeout: 45000 });
    await check("stage, alone", ["waiting", "tiles", "controls"]);
    await shot(a, "stage alone");

    step("two people on the stage");
    await bashir.rpc("JoinVoice", lounge, "e2e-bashir");
    await waitFor(async () => (await a.page.locator(".voice-panel.on-stage .stage .tile").count()) === 2, {
      timeout: 45000,
      message: "two tiles",
    });
    await check("stage, two", ["tiles", "controls"]);
    await shot(a, "stage two");
    await a.page.getByRole("button", { name: "Minimize the call" }).click();
    await a.page.locator(".who-in", { hasText: "2 people are in there" }).waitFor({ timeout: 45000 });
    await check("in the room, two", ["headline", "join", "presence"]);
    await bashir.rpc("LeaveVoice", lounge, "");
  });
}
