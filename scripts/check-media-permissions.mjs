#!/usr/bin/env node
// Concord never asks for access to the photo library or to shared storage:
// photos come in through the system photo picker, the share sheet and the
// Files picker, each of which hands over only what the person chose. This
// check fails when an Android manifest asks for any of those permissions, so
// a dependency cannot add one quietly.
//
//   node scripts/check-media-permissions.mjs <AndroidManifest.xml>...
//   node scripts/check-media-permissions.mjs --merged <android project dir>
//
// --merged checks every merged manifest a Gradle build left under app/build,
// and fails when there is none (the check must not pass by finding nothing).
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const FORBIDDEN = [
  "android.permission.READ_MEDIA_IMAGES",
  "android.permission.READ_MEDIA_VIDEO",
  "android.permission.READ_MEDIA_VISUAL_USER_SELECTED",
  "android.permission.READ_MEDIA_AUDIO",
  "android.permission.READ_EXTERNAL_STORAGE",
  "android.permission.WRITE_EXTERNAL_STORAGE",
  "android.permission.MANAGE_EXTERNAL_STORAGE",
  "android.permission.ACCESS_MEDIA_LOCATION",
];

// forbiddenIn lists the forbidden permissions a manifest requests. A request
// is any uses-permission (or uses-permission-sdk-23) element naming one,
// unless it is a tools:node="remove" that takes one away.
export function forbiddenIn(xml) {
  const text = String(xml).replace(/<!--[\s\S]*?-->/g, "");
  const found = new Set();
  for (const m of text.matchAll(/<uses-permission(?:-sdk-23)?\b([^>]*)>/g)) {
    const attrs = m[1];
    const name = /android:name\s*=\s*"([^"]+)"/.exec(attrs)?.[1];
    if (!name || !FORBIDDEN.includes(name)) continue;
    if (/tools:node\s*=\s*"remove"/.test(attrs)) continue;
    found.add(name);
  }
  return [...found];
}

function mergedManifests(project) {
  const root = path.join(project, "app", "build", "intermediates");
  const out = [];
  const walk = (dir) => {
    let entries = [];
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const e of entries) {
      const p = path.join(dir, e.name);
      if (e.isDirectory()) walk(p);
      else if (e.name === "AndroidManifest.xml" && p.includes(`${path.sep}merged_manifest`)) out.push(p);
    }
  };
  walk(root);
  return out;
}

function main(argv) {
  let files = argv;
  if (argv[0] === "--merged") {
    files = mergedManifests(argv[1] || ".");
    if (!files.length) {
      console.error(`check-media-permissions: no merged manifest under ${argv[1]}/app/build; build first`);
      return 2;
    }
  }
  if (!files.length) {
    console.error("usage: check-media-permissions.mjs <AndroidManifest.xml>... | --merged <android dir>");
    return 2;
  }
  let bad = 0;
  for (const f of files) {
    const hits = forbiddenIn(fs.readFileSync(f, "utf8"));
    if (hits.length) {
      bad++;
      console.error(`${f}: asks for ${hits.join(", ")}. Concord uses the system photo picker and never asks for library or storage access.`);
    } else console.log(`${f}: no photo-library or storage permission`);
  }
  return bad ? 1 : 0;
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  process.exit(main(process.argv.slice(2)));
}
