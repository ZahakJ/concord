// tokenbody.js — which message bodies the inline editor must refuse.
//
// A poll, a game, a doodle, a sound recipe and an announcement are not text
// with a decoration on it: the body IS an encoded payload, and the rendered
// card is the only honest view of it. Opening the plain editor on one showed
// two hundred characters of base64 in a box that did not even grow to fit them,
// with the card it replaced gone from the screen — and a single stray keystroke
// there corrupts a poll that three people have already voted in. There is no
// undo for that and no error either: the token simply stops parsing and the
// message becomes its own base64.
//
// So the editor refuses them and says what to do instead. The four dialogs that
// create these bodies all post them alone, so "delete and repost" is the whole
// of the recovery — it is not a workaround, it is what editing one of these
// would have to be anyway.
//
// MIXED BODIES ARE REFUSED TOO, and that is a decision rather than an
// oversight. Concord never produces one: the poll, game, doodle and soundboard
// dialogs each send their token as the entire message, and the composer sends a
// caption as a separate message rather than appending it. A body with a token
// AND prose in it therefore came from hand-typed markdown or from another
// client, and serving that case would mean a second editing model — a pinned
// uneditable chip beside an editable remainder, with its own rules about what
// happens when you delete across the boundary. That is a lot of contract for a
// case the app cannot create, and every version of it still ends with the
// payload one keystroke from being wrong.
//
// Attachments are DELIBERATELY not on this list. `concord://attach` and
// `concord://file` carry a blob reference rather than an encoded payload, they
// have no "repost it" story (the bytes are already sealed and shared), and the
// name/description they do carry has its own editor on the staged chip.

// The tokens that ARE the message, with what to say when one is edited.
const BODY_TOKENS = [
  { scheme: "poll", hint: "Polls can't be edited — delete and repost." },
  { scheme: "game", hint: "A game can't be edited — it plays in place." },
  { scheme: "doodle", hint: "A doodle can't be edited — delete and draw another." },
  { scheme: "sfx", hint: "A sound can't be edited — delete and post another." },
  { scheme: "announce", hint: "An announcement can't be edited — delete and repost." },
  // An event card reads the LIVE record, so editing the message would edit the
  // pointer rather than the event. Change the event in the calendar instead.
  { scheme: "event", hint: "Edit the event in the calendar — this card follows it." },
];

// The tokens that only MODIFY a message: an expiry, a sealed send time, an
// effect. They ride in front of ordinary text, so they are stripped before the
// question is asked — a disappearing message is still a message you can fix.
const MODIFIERS = /\[(?:eph|ts|fx)\]\(concord:\/\/(?:eph|ts|fx)\/v\d+\/[^)]*\)/g;

// The token shape, matched exactly as the render path matches it: a markdown
// link whose target is the scheme. That precision is the point — the renderers
// (parsePoll, gameAt, parseDoodle, parseSound, parseAnnounce) all key off this
// same bracketed form, so "would this body draw a card?" and "does this body
// refuse the editor?" are the same question, and a message that merely writes
// the word poll or quotes the scheme in prose keeps its editor.
const BODY_RE = /\[[^\]\n]*\]\(concord:\/\/(poll|game|doodle|sfx|announce)\/v\d+\/[^)\s]*\)/;

// bodyToken: the entry for the token this body carries, or null.
export function bodyToken(content) {
  const rest = String(content || "").replace(MODIFIERS, "").trim();
  if (!rest) return null;
  const m = rest.match(BODY_RE);
  return m ? BODY_TOKENS.find((t) => t.scheme === m[1]) || null : null;
}

// uneditableReason: the sentence to show instead of opening the editor, or "".
export function uneditableReason(content) {
  return bodyToken(content)?.hint || "";
}

// canEditBody: the predicate the menu item and the ArrowUp shortcut share.
export function canEditBody(content) {
  return !bodyToken(content);
}
