// pickmedia.js — the "+" sheet's "Photos and videos" tile on Android.
//
// The tile opens the system photo picker (ConcordCore.pickMedia, which uses
// androidx's PickMultipleVisualMedia): the system draws the library, the
// person picks up to ten, and only those reach the app. Concord never asks
// for photo-library or storage access; the merged manifest is checked for
// that in CI (scripts/check-media-permissions.mjs).
//
// What comes back has the shape of a share-sheet event: the native side
// copied each pick into the app's private cache (ShareIntake, same caps and
// skip reasons), so staging is the share path's, through the composer's own
// attachFile. A PNG, JPEG, GIF or WebP photo has its location and the rest
// of its metadata removed when it is sealed (cleanImage in internal/app), as
// for every other way a picture is attached. A video, and a HEIC or HEIF photo
// this renderer cannot read, go out as they are; the composer says so on the
// staged item (AS_IS_NOTE in lib/attachopts.js).

import { parseShare, fileSrc, skippedNote } from "./sharein.js";

// The most the picker lets a person select at once (ShareIntake.MAX_ITEMS).
export const PICK_MAX = 10;

// nativePicker is the plugin when this is the Android app and its build has
// pickMedia, else null (desktop, a browser, an older APK): the sheet then
// keeps its one "Photo or file" tile.
export function nativePicker(cap) {
  const core = cap?.Plugins?.ConcordCore;
  if (!core || typeof core.pickMedia !== "function") return null;
  const platform = typeof cap.getPlatform === "function" ? cap.getPlatform() : "";
  return platform === "android" ? core : null;
}

// pickAndStage runs one pick: opens the picker, stages every copy that came
// back through `attach(file)`, and has the copies deleted afterwards, staged
// or not. Resolves to { picked, staged, note } where note is the one line to
// show for anything that did not make it ("" when everything did, or the
// person backed out). `fetchImpl` is the page's fetch; tests pass their own.
export async function pickAndStage({ core, cap, attach, fetchImpl = fetch }) {
  const res = await core.pickMedia({ max: PICK_MAX });
  const share = parseShare(res);
  const paths = [...share.files.map((f) => f.path), ...share.skipped.filter((s) => s.path).map((s) => s.path)];
  const skipped = [...share.skipped];
  let staged = 0;
  try {
    for (const f of share.files) {
      try {
        const r = await fetchImpl(fileSrc(f.path, cap));
        if (!r.ok) throw new Error(`pick copy ${r.status}`);
        const blob = await r.blob();
        await attach(new File([blob], f.name, { type: f.type }));
        staged++;
      } catch {
        skipped.push({ name: f.name, reason: "unreadable" });
      }
    }
  } finally {
    if (paths.length) await core.releaseShared?.({ paths })?.catch?.(() => {});
  }
  return { picked: share.files.length, staged, note: skippedNote(skipped) };
}
