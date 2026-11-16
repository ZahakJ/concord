// An edit is sent on the edited message's own channel.
//
// Receivers apply a message action only when the row it names sits in the
// channel the action names. The advanced composer used to save an edit on the
// channel that was active when Save was pressed, so switching channels while
// the dialog was open sent the edit somewhere the row is not, and only the
// sender's copy changed. This reads the source: every call that sends an edit
// names the row's channel, and every way of opening the composer on an
// existing message hands it that channel.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function walk(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) walk(p, out);
    else if (/\.(svelte|js)$/.test(e.name) && !e.name.includes(".test.")) out.push(p);
  }
  return out;
}

let failed = 0;
const fail = (msg) => {
  failed++;
  console.error("FAIL", msg);
};

const sends = [];
const opens = [];
for (const p of walk(SRC)) {
  const text = fs.readFileSync(p, "utf8");
  const rel = path.relative(SRC, p);
  for (const m of text.matchAll(/api\.editMessage\(\s*([^,]+),/g)) sends.push([rel, m[1].trim()]);
  for (const m of text.matchAll(/\{[^{}]*\beditId:[^{}]*\}/g)) opens.push([rel, m[0]]);
  for (const m of text.matchAll(/<ModalView\b[\s\S]*?\/>/g)) {
    if (/\beditId=/.test(m[0]) && !/\beditChannelId=/.test(m[0])) fail(`${rel}: the composer is opened for an edit without the row's channel`);
  }
}

if (sends.length === 0) fail("no api.editMessage call found");
for (const [rel, arg] of sends) {
  const ok = /^m\.channelId$/.test(arg) || /^editChannelId\b/.test(arg);
  if (!ok) fail(`${rel}: an edit is sent on ${arg}, not on the row's own channel`);
}
if (opens.length === 0) fail("no place that opens the composer on an existing message");
for (const [rel, lit] of opens) {
  if (!/\beditChannelId:/.test(lit)) fail(`${rel}: the composer is opened on a message without its channel: ${lit}`);
}

if (failed) process.exit(1);
console.log(`ok - ${sends.length} edit sends and ${opens.length} composer openings name the row's channel`);
