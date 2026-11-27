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

// NEW_DEVICE_MARK is the mark a verified contact carries, on the contact and
// its verified badge, while a device is on their account that was not when
// they were verified. Never a line in a chat; verifying again clears it.
export const NEW_DEVICE_MARK = "New device since you verified";

// strangerLine is the notice for a key in a 1:1 that their account did not
// sign (a "keychange" row, internal/app/devicewatch.go). It is always these
// words: the row's own content is never shown, so nothing a peer sent can
// speak in Concord's voice. who is the contact's name, as one clean line.
export function strangerLine(who) {
  const name =
    String(who ?? "")
      // eslint-disable-next-line no-control-regex
      .replace(/[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069\u061c\u2028\u2029\ufeff]/g, (c) => (c === "\n" || c === "\t" ? " " : ""))
      .replace(/\s+/g, " ")
      .trim() || "this contact";
  return `A key that isn't linked to ${name}'s account is in this conversation. Check with ${name} another way before you share anything private.`;
}
