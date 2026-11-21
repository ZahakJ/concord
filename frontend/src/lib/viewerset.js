// viewerset.js — which pictures the feed's viewer can step between.
//
// Every picture in the feed owns its own decrypted source. A message with
// several of them is one set: the viewer opened on one of them walks the
// others without closing, so each registers what the viewer needs under its
// frame element, and the opener looks its neighbours up by the frames the row
// holds. A WeakMap, so a frame that leaves the DOM takes its entry with it.
const entries = new WeakMap();

export function registerFrame(el, entry) {
  entries.set(el, entry);
  return () => {
    if (entries.get(el) === entry) entries.delete(el);
  };
}

export const frameEntry = (el) => (el ? entries.get(el) : undefined);

// whenLabel — "Today at 2:16 PM", "Yesterday at …", "12 Nov at …", with the
// year only when it is not this one. `clock` formats the time of day.
export function whenLabel(iso, clock, now = new Date()) {
  const d = new Date(iso);
  if (!iso || Number.isNaN(d.getTime())) return "";
  const t = clock(iso);
  const day = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const diff = Math.round((day(now) - day(d)) / 86400000);
  if (diff === 0) return `Today at ${t}`;
  if (diff === 1) return `Yesterday at ${t}`;
  const opts = { day: "numeric", month: "short" };
  if (d.getFullYear() !== now.getFullYear()) opts.year = "numeric";
  return `${d.toLocaleDateString(undefined, opts)} at ${t}`;
}
