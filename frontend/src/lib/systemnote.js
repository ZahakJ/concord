// systemnote.js — who a "system" row speaks as, and in what words.
//
// A system row is a member's notice ("has joined the guild"), sent and signed
// under their key like any message. The name a row carries (senderName) is
// self-asserted, so it is never shown: a member could otherwise post as
// "Concord". Two shapes are allowed to stand without an author, the guest
// notices the meeting host's node writes (internal/app/guest.go,
// guestJoinNotice and guestLeftNotice), and they are drawn from our own
// text with the guest's name cleaned in. An event reminder (events.go,
// eventAnnounceText) speaks for the guild, under the guild's own name.

const GUEST_JOIN = /^👤 (.{1,64}) joined as a guest \(via your meeting link\)$/u;
const GUEST_LEFT = /^👤 (.{1,64}) \(guest\) left$/u;
const EVENT = /^⏰ .{1,200} is starting(?: here in #.{1,100} — come on in| — join the call in 🔊 .{1,100})$/u;

// Characters that hide, reorder or blank text: C0/C1 controls, soft hyphen,
// bidi marks and overrides, zero-width and invisible operators, Hangul fillers,
// the BOM. Written as code points so the source holds none of them.
const HIDDEN_RANGES = [
  [0x00, 0x1f], [0x7f, 0x9f], [0xad, 0xad], [0x61c, 0x61c], [0x115f, 0x1160],
  [0x180e, 0x180e], [0x200b, 0x200f], [0x2028, 0x202e], [0x2060, 0x2064],
  [0x2066, 0x206f], [0x3164, 0x3164], [0xfeff, 0xfeff], [0xe0000, 0xe007f],
];
const HIDDEN = new RegExp(
  "[" + HIDDEN_RANGES.map(([a, b]) => String.fromCodePoint(a) + "-" + String.fromCodePoint(b)).join("") + "]",
  "gu",
);

// oneLine is a name or label as one clean line: breaks and tabs become
// spaces, and nothing that hides, reorders or blanks text is kept. Matches
// cmd/concord/style.go hostile().
export function oneLine(s) {
  return String(s ?? "")
    .replace(/[\n\r\t]/g, " ")
    .replace(HIDDEN, "")
    .replace(/\s+/g, " ")
    .trim();
}

// systemNote returns { actor, text } for a system row m ({ content,
// senderName, sender }). actor "" means the line stands alone.
// ctx.nameOf(fingerprint) is the authenticated name lookup (nameFor);
// ctx.guildName is the guild the row is in.
export function systemNote(m, { nameOf, guildName = "" }) {
  const content = String(m?.content ?? "");
  let g = GUEST_JOIN.exec(content);
  if (g && oneLine(g[1])) return { actor: "", text: `👤 ${oneLine(g[1])} joined as a guest (via your meeting link)` };
  g = GUEST_LEFT.exec(content);
  if (g && oneLine(g[1])) return { actor: "", text: `👤 ${oneLine(g[1])} (guest) left` };
  if (EVENT.test(content) && guildName) return { actor: guildName, text: content };
  return { actor: nameOf(m?.sender || ""), text: content };
}
