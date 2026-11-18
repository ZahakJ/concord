// Run: node src/lib/resume.test.mjs
import assert from "node:assert/strict";
import { installResume, AWAY_MS } from "./resume.js";

function fakePage() {
  const listeners = {};
  const target = () => ({
    addEventListener: (n, f) => ((listeners[n] ||= []).push(f)),
    removeEventListener: (n, f) => (listeners[n] = (listeners[n] || []).filter((x) => x !== f)),
  });
  const doc = { hidden: false, ...target() };
  const win = target();
  const fire = (n, e = {}) => (listeners[n] || []).forEach((f) => f(e));
  return { doc, win, fire };
}

function rig({ rendered = true } = {}) {
  const p = fakePage();
  let t = 1000;
  const calls = { reopen: 0, reload: 0 };
  const r = { rendered };
  installResume({
    doc: p.doc,
    win: p.win,
    now: () => t,
    reopen: () => calls.reopen++,
    reload: () => calls.reload++,
    rendered: () => r.rendered,
  });
  const away = (ms) => {
    p.doc.hidden = true;
    p.fire("visibilitychange");
    t += ms;
    p.doc.hidden = false;
    p.fire("visibilitychange");
  };
  return { ...p, calls, away, r };
}

// Away long enough for the phone to have frozen the page: a fresh stream.
{
  const { calls, away } = rig();
  away(AWAY_MS + 1);
  assert.deepEqual(calls, { reopen: 1, reload: 0 });
}

// A flick to another tab and straight back leaves the stream alone.
{
  const { calls, away } = rig();
  away(200);
  assert.deepEqual(calls, { reopen: 0, reload: 0 });
}

// Restored from the back/forward cache: every connection was closed under it.
{
  const { calls, fire } = rig();
  fire("pageshow", { persisted: false });
  assert.equal(calls.reopen, 0);
  fire("pageshow", { persisted: true });
  assert.equal(calls.reopen, 1);
}

// Nothing rendered (the all-black screen): reload, however short the absence.
{
  const { calls, away, r } = rig();
  r.rendered = false;
  away(10);
  assert.deepEqual(calls, { reopen: 0, reload: 1 });
}

console.log("resume ok");
