#!/usr/bin/env node
// End-to-end runner: builds (or takes) a concord-web binary, starts a private
// rendezvous on 127.0.0.1, and runs each spec under scripts/e2e/specs/ against
// fresh isolated peers driven by headless Chromium. See README.md.
//
//   node scripts/e2e/run.mjs [spec files or name fragments…] [--grep re] [--keep] [--list]
//
// Environment: E2E_BIN (skip the build), E2E_BASE_PORT (default 47300),
// CHROMIUM (default /usr/bin/chromium), PLAYWRIGHT_CORE, E2E_TIMEOUT_SCALE.
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { loadChromium } from "../playwright.mjs";
import * as H from "./harness.mjs";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const ROOT = path.resolve(HERE, "../..");

// ---------------------------------------------------------------- args

const argv = process.argv.slice(2);
const opt = { grep: null, keep: false, list: false, files: [] };
for (let i = 0; i < argv.length; i++) {
  const a = argv[i];
  if (a === "--grep" || a === "-g") opt.grep = new RegExp(argv[++i], "i");
  else if (a === "--keep") opt.keep = true;
  else if (a === "--list") opt.list = true;
  else if (a === "--help" || a === "-h") {
    console.log(fs.readFileSync(fileURLToPath(import.meta.url), "utf8").split("\n").slice(1, 9).join("\n"));
    process.exit(0);
  } else opt.files.push(a);
}

// ---------------------------------------------------------------- specs

const specDir = path.join(HERE, "specs");
let files = fs
  .readdirSync(specDir)
  .filter((f) => f.endsWith(".spec.mjs"))
  .sort()
  .map((f) => path.join(specDir, f));
if (opt.files.length) {
  // An argument is a path to a spec, or a fragment of a spec's file name.
  files = opt.files.flatMap((a) => {
    if (fs.existsSync(a) && fs.statSync(a).isFile()) return [path.resolve(a)];
    const hit = files.filter((f) => path.basename(f).includes(a));
    if (!hit.length) {
      console.error(`no spec matches "${a}"`);
      process.exit(2);
    }
    return hit;
  });
}
let tests = [];
for (const f of files) tests.push(...(await H.loadSpec(f)));
const title = (t) => `${path.basename(t.file, ".spec.mjs")} › ${t.name}`;
if (opt.grep) tests = tests.filter((t) => opt.grep.test(title(t)));
if (opt.list) {
  for (const t of tests) console.log(title(t));
  process.exit(0);
}
if (!tests.length) {
  console.error("no tests selected");
  process.exit(2);
}

// ---------------------------------------------------------------- setup

const BASE = Number(process.env.E2E_BASE_PORT || 47300);
const runId = `${new Date().toISOString().replace(/[-:]/g, "").replace(/\..*/, "").replace("T", "-")}-${process.pid}`;
const runDir = path.join(ROOT, ".dev", "e2e", runId);
const artDir = path.join(runDir, "artifacts");
fs.mkdirSync(runDir, { recursive: true });
const log = (...a) => console.log(`[e2e ${new Date().toISOString().slice(11, 19)}]`, ...a);

let browser = null;
let rdv = null;
let interrupted = false;

async function teardown() {
  await browser?.close().catch(() => {});
  browser = null;
  if (rdv) await H.stopChild(rdv.child);
  rdv = null;
  H.killAllSync();
}
// Only the PIDs this run started are ever signalled. A second Ctrl-C while a
// graceful stop is in progress kills them outright.
for (const sig of ["SIGINT", "SIGTERM", "SIGHUP"]) {
  process.on(sig, () => {
    if (interrupted) {
      H.killAllSync();
      process.exit(130);
    }
    interrupted = true;
    console.error(`\n${sig}: stopping peers…`);
    teardown().finally(() => process.exit(130));
  });
}
process.on("exit", () => H.killAllSync());

function go(args, out) {
  log(`go build -o ${path.relative(ROOT, out)} ${args.join(" ")}`);
  execFileSync("go", ["build", "-o", out, ...args], { cwd: ROOT, stdio: "inherit" });
  return out;
}

let bin = process.env.E2E_BIN;
let rdvBin = process.env.E2E_RENDEZVOUS_BIN;
try {
  if (!bin) {
    // The binary embeds frontend/dist as it is on disk; `make e2e` rebuilds it
    // first. Building Go against a missing dist would fail the embed anyway, so
    // say what to do instead.
    if (!fs.existsSync(path.join(ROOT, "frontend/dist/index.html"))) {
      console.error("frontend/dist is missing: run `make e2e` or `npm --prefix frontend run build` first");
      process.exit(2);
    }
    bin = go(["."], path.join(runDir, "concord-web"));
  }
  if (!rdvBin) rdvBin = go(["./cmd/rendezvous"], path.join(runDir, "rendezvous"));
} catch {
  console.error("build failed");
  process.exit(2);
}
bin = path.resolve(bin);
rdvBin = path.resolve(rdvBin);

const token = `e2e${process.pid}`.padEnd(32, "0");

const chromium = await loadChromium();
const CHROMIUM = process.env.CHROMIUM || "/usr/bin/chromium";
async function getBrowser() {
  if (browser) return browser;
  const args = [
    "--use-fake-ui-for-media-stream",
    "--use-fake-device-for-media-stream",
    "--autoplay-policy=no-user-gesture-required",
    "--disable-dev-shm-usage",
  ];
  // The system Chromium when there is one; otherwise whatever browser the
  // borrowed playwright-core install has downloaded.
  const exe = fs.existsSync(CHROMIUM) ? CHROMIUM : undefined;
  browser = await chromium.launch({ headless: true, executablePath: exe, args: exe ? ["--no-sandbox", ...args] : args });
  return browser;
}

log(`run ${runId}  base port ${BASE}  ${tests.length} test(s)`);
log(`binary ${path.relative(ROOT, bin) || bin}`);
rdv = await H.startRendezvous({ bin: rdvBin, port: BASE, dir: runDir });
log(`rendezvous ${rdv.bootstrap}`);

// ---------------------------------------------------------------- run

function slug(s) {
  return s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 60);
}

async function saveArtifacts(world, name, err) {
  const dir = path.join(artDir, name);
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, "error.txt"), `${err?.stack || err}\n`);
  for (const p of world.all) {
    if (fs.existsSync(p.log)) fs.copyFileSync(p.log, path.join(dir, path.basename(p.log)));
    let k = 0;
    for (const app of p.apps) {
      k++;
      const base = path.join(dir, `${p.name.toLowerCase()}-page${k}`);
      await app.page?.screenshot({ path: `${base}.png` }).catch(() => {});
      fs.writeFileSync(`${base}.console.txt`, app.console.join("\n") + "\n");
    }
  }
  return dir;
}

const results = [];
for (const t of tests) {
  if (interrupted) break;
  const name = slug(title(t));
  const world = new H.World({
    dir: path.join(runDir, name),
    bin,
    bootstrap: rdv.bootstrap,
    token,
    // Peers take base+10…base+14; the rendezvous holds base and base+1.
    portBase: BASE + 10,
    getBrowser,
  });
  fs.mkdirSync(world.dir, { recursive: true });
  const ctx = {
    world,
    assert: H.assert,
    waitFor: H.waitFor,
    sleep: H.sleep,
    seedMessages: H.seedMessages,
    log: (...a) => log(`  ${t.name}:`, ...a),
    step: (s) => log(`  · ${s}`),
    artifacts: path.join(artDir, name),
    // A named screenshot under the test's artifacts directory. Kept on a green
    // run only with --keep or E2E_KEEP_RUN=1 (the run directory is removed).
    shot: (app, label) => app.screenshot(path.join(artDir, name, `${slug(label)}.png`)),
  };
  log(`▶ ${title(t)}`);
  const t0 = Date.now();
  let err = null;
  const limit = H.scaled(t.timeout || 300000);
  let timer;
  try {
    await Promise.race([
      t.fn(ctx),
      new Promise((_, rej) => (timer = setTimeout(() => rej(new Error(`test timed out after ${limit}ms`)), limit))),
    ]);
  } catch (e) {
    err = e;
  }
  clearTimeout(timer);
  const secs = ((Date.now() - t0) / 1000).toFixed(1);
  if (err && !interrupted) {
    const dir = await saveArtifacts(world, name, err);
    log(`✗ ${title(t)} (${secs}s)\n${String(err.stack || err).replace(/^/gm, "    ")}\n    artifacts: ${path.relative(ROOT, dir)}`);
  } else if (!err) log(`✓ ${title(t)} (${secs}s)`);
  results.push({ t, ok: !err, secs });
  if (opt.keep && (err || t === tests.at(-1))) {
    log("--keep: peers left running. Ctrl-C to stop them.");
    for (const p of world.all) log(`  ${p.name}  ${p.url}/?t=${token}  passphrase ${p.passphrase}  home ${path.relative(ROOT, p.home)}`);
    await new Promise(() => {});
  }
  await world.cleanup();
}

await teardown();

// ---------------------------------------------------------------- summary

console.log("\nSummary");
for (const r of results) console.log(`  ${r.ok ? "PASS" : "FAIL"}  ${title(r.t).padEnd(64)} ${r.secs}s`);
const failed = results.filter((r) => !r.ok).length;
const skipped = tests.length - results.length;
console.log(`\n${results.length - failed} passed, ${failed} failed${skipped ? `, ${skipped} not run` : ""}  (run dir ${path.relative(ROOT, runDir)})`);
if (!failed && !skipped && !opt.keep && !process.env.E2E_KEEP_RUN) {
  // A green run leaves nothing behind but the summary; a red one keeps the
  // whole directory (data dirs, logs, artifacts) for the post-mortem.
  fs.rmSync(runDir, { recursive: true, force: true });
}
process.exit(failed || skipped ? 1 : 0);
