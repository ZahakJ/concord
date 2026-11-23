#!/usr/bin/env node
// Nothing of Concord's may leave the phone through Android backup or
// phone-to-phone transfer: a restored keystore or MLS ratchet state forks
// every group, and the recovery phrase is the only way to move an account.
// allowBackup="false" alone does not cover transfer on Android 12 and later,
// so this check fails unless the application element carries all three of
//   android:allowBackup="false"
//   android:fullBackupContent="false"
//   android:dataExtractionRules="@xml/<rules>"
// (and, in the source manifest, tools:targetApi="s")
// and the rules file excludes every domain from both <cloud-backup> and
// <device-transfer>, with no <include> anywhere.
//
//   node scripts/check-no-backup.mjs <AndroidManifest.xml>
//   node scripts/check-no-backup.mjs --merged <android project dir>
//
// The first form also reads the rules from the manifest's res/xml. --merged
// checks the attributes of every merged manifest under app/build, and fails
// when there is none.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const DOMAINS = [
  "root", "file", "database", "sharedpref", "external",
  "device_root", "device_file", "device_database", "device_sharedpref",
];

function applicationTag(xml) {
  const m = xml.replace(/<!--[\s\S]*?-->/g, "").match(/<application\b[^>]*>/);
  return m ? m[0] : "";
}

function attr(tag, name) {
  const m = tag.match(new RegExp(`\\bandroid:${name}\\s*=\\s*"([^"]*)"`));
  return m ? m[1] : null;
}

// manifestProblems lists what the manifest's application element lacks. The
// source manifest must also say tools:targetApi="s" (the build strips tools:
// attributes, so a merged manifest is not asked for it).
export function manifestProblems(xml, { source = false } = {}) {
  const app = applicationTag(xml);
  if (!app) return ["no <application> element"];
  const out = [];
  if (attr(app, "allowBackup") !== "false") out.push('android:allowBackup is not "false"');
  if (attr(app, "fullBackupContent") !== "false") out.push('android:fullBackupContent is not "false"');
  if (!/^@xml\/\w+$/.test(attr(app, "dataExtractionRules") || "")) out.push("android:dataExtractionRules does not name an @xml resource");
  if (source && !/\btools:targetApi\s*=\s*"s"/.test(app)) out.push('tools:targetApi is not "s"');
  return out;
}

export function rulesName(xml) {
  return (attr(applicationTag(xml), "dataExtractionRules") || "").replace(/^@xml\//, "");
}

// rulesProblems lists what a data-extraction-rules file lets out.
export function rulesProblems(xml) {
  const body = xml.replace(/<!--[\s\S]*?-->/g, "");
  const out = [];
  if (/<include\b/.test(body)) out.push("an <include> is present");
  for (const section of ["cloud-backup", "device-transfer"]) {
    const m = body.match(new RegExp(`<${section}\\b[^>]*>([\\s\\S]*?)</${section}>`));
    if (!m) { out.push(`no <${section}> section`); continue; }
    for (const d of DOMAINS) {
      const re = new RegExp(`<exclude\\b(?=[^>]*\\bdomain\\s*=\\s*"${d}")(?=[^>]*\\bpath\\s*=\\s*"\\.")[^>]*/>`);
      if (!re.test(m[1])) out.push(`<${section}> does not exclude all of domain "${d}"`);
    }
  }
  return out;
}

function mergedManifests(project) {
  const out = [];
  const walk = (dir) => {
    let entries;
    try { entries = fs.readdirSync(dir, { withFileTypes: true }); } catch { return; }
    for (const e of entries) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else if (e.name === "AndroidManifest.xml" && p.includes(`${path.sep}merged_manifest`)) out.push(p);
    }
  };
  walk(path.join(project, "app", "build"));
  return out;
}

function main(argv) {
  const problems = [];
  if (argv[0] === "--merged") {
    const files = mergedManifests(argv[1] || ".");
    if (files.length === 0) {
      console.error(`check-no-backup: no merged manifest under ${argv[1]}/app/build; build first`);
      return 1;
    }
    for (const f of files) for (const p of manifestProblems(fs.readFileSync(f, "utf8"))) problems.push(`${f}: ${p}`);
  } else if (argv.length === 1) {
    const f = argv[0];
    const xml = fs.readFileSync(f, "utf8");
    for (const p of manifestProblems(xml, { source: true })) problems.push(`${f}: ${p}`);
    const name = rulesName(xml);
    if (name) {
      const rf = path.join(path.dirname(f), "res", "xml", `${name}.xml`);
      if (!fs.existsSync(rf)) problems.push(`${rf}: missing`);
      else for (const p of rulesProblems(fs.readFileSync(rf, "utf8"))) problems.push(`${rf}: ${p}`);
    }
  } else {
    console.error("usage: check-no-backup.mjs <AndroidManifest.xml> | --merged <android dir>");
    return 2;
  }
  for (const p of problems) console.error(`check-no-backup: ${p}`);
  if (problems.length === 0) console.log("check-no-backup: ok");
  return problems.length ? 1 : 0;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  process.exit(main(process.argv.slice(2)));
}
