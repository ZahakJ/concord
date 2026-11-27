// contactdevices.js — the quiet line a contact's details show about their
// devices ("2 devices · Pixel added 26 Nov"). A device linked through their
// account is checked by the core against the account's signature and is never
// a notice in the chat (internal/app/devicewatch.go); this is where it shows.

// deviceLine: one short line, or "" when there is nothing worth a line.
// view is ContactDevices' answer { count, added: [{ name, issued, seen }] };
// now is for tests.
export function deviceLine(view, now = Date.now()) {
  const count = Number(view?.count) || 0;
  if (count <= 1 && !(view?.added || []).length) return "";
  const head = count === 1 ? "1 device" : `${count} devices`;
  const last = (view?.added || []).at(-1);
  if (!last) return head;
  const at = (Number(last.issued) || Number(last.seen) || 0) * 1000;
  const name = String(last.name || "").trim() || "A device";
  if (!at) return `${head} · ${name} added`;
  const d = new Date(at);
  const sameYear = d.getFullYear() === new Date(now).getFullYear();
  const day = d.toLocaleDateString(undefined, sameYear ? { day: "numeric", month: "short" } : { day: "numeric", month: "short", year: "numeric" });
  return `${head} · ${name} added ${day}`;
}
