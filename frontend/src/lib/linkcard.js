// linkcard.js — link cards: a link's title, picture, site and length, built
// once by the SENDER's device and carried inside the message, so that nobody
// reading it contacts anybody until they tap.
//
// A card is the last one or two lines of a message body, written in markdown
// every Concord client already reads (internal/app/linkcard.go builds it):
//
//   [<title>](<url>) · <domain>
//   ![image](concord://attach/v2/<blob>/<keys>/jpeg/<w>x<h>/2/<name>/<desc>)
//
// The title is the masked link's text; the picture an ordinary encrypted
// attachment flagged ATTACH_CARD; its description carries "<site>" or
// "<site> · <duration>". All of it sits in Content, which is what the author's
// signature covers, and a v0.60 peer renders it as a titled link with a
// captioned picture under it.
//
// " · <domain>" is there for those v0.60 readers: they draw the title as a
// link and nothing else would tell them where it goes. This client never
// trusts it — the domain it prints is computed from the URL
// (lib/regdomain.js) — and drops the copy only when it says exactly that.
import { parseAttachTokens } from "./attachments.js";
import { stripLinkEmbed } from "./linkembed.js";
import { extractLinks } from "./embeds.js";
import { deceptiveText, sameAsURL, linkDomain, hostOf, stripForeignDomains } from "./regdomain.js";

// THE switch for link cards. false turns them off entirely: no message is
// drawn with a card (a card's lines then render as the masked link and
// captioned picture they are, which is what a v0.60 reader sees), no card is
// fetched or sent, the first-link question is never asked, and the Privacy
// row is hidden. It exists so a release can ship with cards off.
export const LINK_CARDS = true;

// The device-local pref that holds the choice: true, false, or absent (not
// chosen yet). Its name is new on purpose. Builds made before the question
// kept the choice under sendLinkPreviews, defaulted it to false and saved it
// with every other pref, so a false stored there is not an answer anybody
// gave; reading it would mean such a device is never asked. That key is left
// unread, as linkPreviews was before it.
export const CARD_PREF = "sendLinkCards";

// cardChoice(prefs): the choice as true, false, or null when not made yet.
export function cardChoice(prefs) {
  const v = prefs ? prefs[CARD_PREF] : undefined;
  return v === true || v === false ? v : null;
}

// Whether links you send get a card when you have not chosen yet
// (cardChoice is null). One of:
//   "off" — no card until you turn it on in Privacy;
//   "ask" — no card until the first time you send a link, then one question
//           (CARD_QUESTION), answered once; Privacy changes it later;
//   "on"  — cards from the start.
// The owner's decision is "ask": nothing is fetched for a link you send until
// you have answered the question once.
export const SEND_DEFAULT = "ask";

// The question says in plain words what Yes does. It is asked once; Settings ›
// Privacy holds the same choice (cardChoiceNote) and answers it there too.
export const CARD_QUESTION =
  "Show cards for links you send? Concord will look up the title and picture of each link you send, from your device, so others see a card. The site sees a visit from you, as if you had opened the link.";

// cardChoiceNote(pref): the line under the Settings switch, which says which
// of the three states you are in. Undecided shows as off (nothing is fetched)
// and says the question is still to come; choosing there is the answer.
export function cardChoiceNote(pref) {
  if (pref === true) return "On: Concord looks up a link's title and picture, from your device, when you send it";
  if (pref === false) return "Off: links you send go out as you typed them. You can turn this on here at any time.";
  return SEND_DEFAULT === "ask" ? "Not chosen yet: you will be asked at the first link you send" : "Off until you turn it on";
}

// sendingCards(pref, mode): does a link you send get a card?
export function sendingCards(pref, mode = SEND_DEFAULT) {
  if (!LINK_CARDS) return false;
  if (pref === true || pref === false) return pref;
  return mode === "on";
}

// askAboutCards(pref, mode): should sending a link raise the question?
export function askAboutCards(pref, mode = SEND_DEFAULT) {
  return LINK_CARDS && mode === "ask" && pref !== true && pref !== false;
}

// One masked link alone on a line, optionally followed by " · <domain>". The
// text may hold backslash escapes (the sender escapes markdown in titles),
// including an escaped "]".
const MASKED_LINE_RE = /^\[((?:\\.|[^\]\\\n])+)\]\((https?:\/\/[^\s)]+)\)(?: · (\S+))?$/;
const ATTACH_LINE_RE = /^!\[image\]\(concord:\/\/attach\/v2\/[^\s)]+\)$/;
// The stamps a composer puts IN FRONT of a body (send effect, disappearing
// timer, sealed timestamp). A card goes after the words, never between these.
const STAMPS_RE = /^(?:\[(?:fx|eph|ts)\]\(concord:\/\/(?:fx|eph|ts)\/v1\/[A-Za-z0-9_-]+\))*/;
const EPH_RE = /\[eph\]\(concord:\/\/eph\/v1\/\d+\)/;
const DURATION_RE = /^\d{1,2}(?::\d{2}){1,2}$/;

export const TITLE_MAX = 200;
export const SITE_MAX = 80;

// Characters that reorder or hide text. Stripped from anything a card prints:
// a right-to-left override in a title can make the words after it read
// backwards, a soft hyphen or word joiner hides inside a word, and the card's
// job is to say plainly where a link goes. That is every control and format
// character and every other default-ignorable code point, except the
// zero-width (non-)joiners (emoji sequences and Persian need them) and the
// variation selectors (an emoji's colour form). The address checks look
// through those too (regdomain.js foldForCheck).
const INVISIBLE_RE =
  /(?![\u200c\u200d\ufe00-\ufe0f\u{e0100}-\u{e01ef}])[\p{Cc}\p{Cf}\p{Default_Ignorable_Code_Point}]/gu;

export function cleanText(s, max) {
  const t = String(s || "")
    .replace(/\s+/g, " ")
    .replace(INVISIBLE_RE, "")
    .replace(/\s+/g, " ")
    .trim();
  const chars = [...t];
  return chars.length > max ? `${chars.slice(0, max).join("")}…` : t;
}

const unescapeMd = (s) => s.replace(/\\(.)/g, "$1");

// parseCard(content) -> null, or
//   { url, title, domain, site, duration, thumb, rest, linkOnly }
// `rest` is the body without the card's lines (what the feed renders as text),
// `thumb` the attachment token of the picture or null.
export function parseCard(content) {
  if (!content) return null;
  return parseCardBelow(content) || parseCardAbove(content);
}

// The usual place: the card's lines end the message, under the words.
function parseCardBelow(content) {
  const lines = String(content).replace(/\s+$/, "").split("\n");
  let thumb = null;
  let last = lines[lines.length - 1].trim();
  let blockLines = 1;
  if (ATTACH_LINE_RE.test(last)) {
    const toks = parseAttachTokens(last);
    if (toks.length !== 1 || !toks[0].card || toks[0].spoiler) return null;
    thumb = toks[0];
    lines.pop();
    if (!lines.length) return null;
    last = lines[lines.length - 1].trim();
    blockLines = 2;
  }
  const m = MASKED_LINE_RE.exec(last);
  if (!m) return null;
  const all = String(content).replace(/\s+$/, "").split("\n");
  const block = all.slice(all.length - blockLines).join("\n");
  lines.pop();
  const rest = lines.join("\n").replace(/\s+$/, "");
  return cardOf(m, thumb, rest, block, false);
}

// Above the words (the sender's choice, withCard's `above`): the card's lines
// open the message, after the stamps. Only a line in the exact form the core
// writes, its domain included, is read as a card there; a masked link the
// author typed at the top of a message stays the words it is.
function parseCardAbove(content) {
  const text = String(content).replace(/\s+$/, "");
  const prefix = (STAMPS_RE.exec(text) || [""])[0];
  let body = text.slice(prefix.length);
  if (prefix) {
    if (!body.startsWith("\n")) return null;
    body = body.slice(1);
  }
  const lines = body.split("\n");
  const m = MASKED_LINE_RE.exec(lines[0].trim());
  if (!m || m[3] === undefined) return null;
  let thumb = null;
  let n = 1;
  if (lines.length > 1 && ATTACH_LINE_RE.test(lines[1].trim())) {
    const toks = parseAttachTokens(lines[1].trim());
    if (toks.length !== 1 || !toks[0].card || toks[0].spoiler) return null;
    thumb = toks[0];
    n = 2;
  }
  const after = lines.slice(n).join("\n");
  if (!after.trim()) return null;
  return cardOf(m, thumb, prefix + after, lines.slice(0, n).join("\n"), true);
}

// cardOf: the card a masked line (MASKED_LINE_RE's match) and its picture
// make, or null when the line may not be a card's face.
function cardOf(m, thumb, rest, block, above) {
  const url = m[2];
  if (!hostOf(url)) return null;
  const domain = linkDomain(url);
  // The trailing domain is for older readers. One that is not this link's
  // own is not a card's: the line is left to render as the words it is.
  if (m[3] !== undefined && m[3].toLowerCase() !== domain) return null;
  const raw = unescapeMd(m[1]);
  const title = cleanText(raw, TITLE_MAX);
  if (!title) return null;
  // The same rule the markdown renderer applies to every masked link: words
  // that name another site are not allowed to be a link's face. Checked on
  // what will be printed and on what was sent, both folded (foldForCheck).
  if (deceptiveText(raw, url) || deceptiveText(title, url) || sameAsURL(title, url)) return null;
  const video = cardVideo(url);
  let site = "";
  let duration = "";
  if (thumb?.desc) {
    const d = thumb.desc;
    const at = d.lastIndexOf(" · ");
    if (at >= 0 && DURATION_RE.test(d.slice(at + 3))) {
      site = d.slice(0, at);
      duration = d.slice(at + 3);
    } else {
      site = d;
    }
  }
  // YouTube publishes no length to the sender (its oEmbed has none, and the
  // page is never loaded), so a YouTube card that shows one was written by
  // hand. A length nobody could have known is not printed.
  if (video.kind === "yt") duration = "";
  return {
    url,
    title,
    domain,
    // The page's own name for itself, minus any address that is not this
    // link's: the card prints the real one on its own line.
    site: cleanText(stripForeignDomains(cleanText(site, SITE_MAX * 2), domain), SITE_MAX),
    duration,
    thumb,
    video,
    rest,
    block,
    above,
    linkOnly: !rest.replace(STAMPS_RE, "").trim(),
  };
}

// cardFor(m): the card to draw under a message row, or null. Only for a row
// whose author's signature this device checked: an unsigned row reached us
// through another member, who could have written any of it, and a card is
// the most persuasive thing a row can show. Such a row renders its lines as
// the plain markdown they are, domain chip included.
export function cardFor(m) {
  if (!LINK_CARDS || !m || m.deleted || m.unverified || (m.kind || "") !== "") return null;
  return parseCard(m.content);
}

// restForDisplay(text, url): the words around a card as the feed shows them.
// The card already stands for its link, so a raw copy of that URL in the
// sentence is noise — on a phone a YouTube URL wraps over two lines. Where it
// ends the words it is dropped; elsewhere the markdown renderer prints it
// short, as its domain (renderMarkdown's cardUrl option). Copy and edit keep
// the words as they were sent.
export function restForDisplay(text, url) {
  const rest = String(text || "");
  const at = url ? rest.lastIndexOf(url) : -1;
  if (at < 0) return rest;
  const before = rest.slice(0, at);
  const after = rest.slice(at + url.length);
  // Only a bare link, at the very end (closing punctuation aside), with words
  // before it, and not inside a code span, a masked link or angle brackets.
  if (!/^[\s.,;:!?]*$/.test(after) || /[`(<]$/.test(before) || !before.trim()) return rest;
  return before.replace(/[\s:—–-]+$/, "") + after.trim().replace(/^[,;:]+/, "");
}

// ---- sending ----

// cardLink(body): the link a card should be built for, or "" for none. Only a
// plain link in the words qualifies: not one inside code, not one under a
// spoiler (a card would print what the author covered), not when the body
// already holds a masked link or a Concord token (polls, embeds, events…).
export function cardLink(body) {
  const words = String(body || "").replace(STAMPS_RE, "");
  if (!words.trim() || words.includes("concord://") || /\]\(https?:\/\//.test(words)) return "";
  const open = words.replace(/\|\|[\s\S]*?\|\|/g, " ");
  const [url] = extractLinks(open, 1);
  if (!url || !hostOf(url)) return "";
  return url;
}

// wantsThumb: a disappearing message gets a title-only card. Its picture would
// be a sealed blob that outlives the message it belonged to.
export function wantsThumb(body) {
  return !EPH_RE.test(String(body || ""));
}

// How long a send waits for its card before going out without one. Short on
// purpose, and never a queue: the message waits only while nothing else is
// sent into its channel. The next send in that channel, from anywhere in the
// app, sends this one at once without its card, and then itself (lib/cardgate.js).
// So nothing is ever reordered and nothing typed later waits behind a website.
// The core keeps fetching after this and caches the card, so sending the same
// link again shortly after gets it.
export const CARD_WAIT_MS = 2000;

// withCard(body, url, block, above): the body that goes out. A message that
// was only the link becomes only the card (the raw URL is what the card
// replaces); a link inside a sentence keeps the sentence and the card goes
// under it, or over it when the sender chose `above`.
//
// The placement is the order of the lines and nothing else: no new field (the
// signature covers Content, and a field older peers do not sign would break
// it), the same for every reader, and a reader that predates the choice draws
// its titled link and picture first and the words after, which is the order
// the sender chose.
export function withCard(body, url, block, above = false) {
  if (!block) return body;
  const prefix = (STAMPS_RE.exec(body) || [""])[0];
  const words = body.slice(prefix.length);
  // On a line of its own even after the stamps: the card is recognised by its
  // own lines alone, and "[eph](…)[title](url)" is not a card's line.
  if (words.trim() === url) return prefix ? `${prefix}\n${block}` : block;
  if (above) return `${prefix ? `${prefix}\n` : ""}${block}\n${words.replace(/^\s+|\s+$/g, "")}`;
  return `${body.replace(/\s+$/, "")}\n${block}`;
}

// placeLinkFirst(body, url): a video link with no card, placed above the
// words: the link alone on the first line (after the stamps), which the feed
// draws as the player (leadEmbed). A reader that predates the choice shows the
// link, then the words, then its player under them.
export function placeLinkFirst(body, url) {
  const prefix = (STAMPS_RE.exec(body) || [""])[0];
  const words = body.slice(prefix.length);
  const at = url ? words.indexOf(url) : -1;
  if (at < 0 || words.trim() === url) return body;
  const rest = (words.slice(0, at) + words.slice(at + url.length)).replace(/[ \t]{2,}/g, " ").replace(/^\s+|\s+$/g, "");
  if (!rest) return body;
  return `${prefix ? `${prefix}\n` : ""}${url}\n${rest}`;
}

// leadEmbed(content): { url, rest } when a message opens with a video link
// alone on its line and has words after it (placeLinkFirst's shape), else
// null. rest is the message without that line.
export function leadEmbed(content) {
  const text = String(content || "").replace(/\s+$/, "");
  const prefix = (STAMPS_RE.exec(text) || [""])[0];
  let body = text.slice(prefix.length);
  if (prefix) {
    if (!body.startsWith("\n")) return null;
    body = body.slice(1);
  }
  const nl = body.indexOf("\n");
  if (nl < 0) return null;
  const first = body.slice(0, nl).trim();
  const after = body.slice(nl + 1);
  if (!/^https?:\/\/\S+$/.test(first) || cardVideo(first).kind !== "yt" || !after.trim()) return null;
  return { url: first, rest: prefix + after };
}

// ---- editing ----

// cardEditSeed(content): what the edit box shows for a message, and the card
// to put back. The card's lines are the core's work, not the author's words:
// a masked line and an attachment token are noise in a phone's edit box. A
// message that was only the link shows the link. A rider from a development
// build (lib/linkembed.js) is dropped and never comes back.
export function cardEditSeed(content) {
  const c = stripLinkEmbed(String(content || ""));
  const card = parseCard(c);
  if (!card) return { text: c, url: "", block: "", above: false };
  const text = card.linkOnly ? card.rest + card.url : card.rest;
  return { text, url: card.url, block: card.block, above: card.above };
}

// cardEditSave(edited, seed): the content an edit stores. The card goes back
// on only while the words still hold the same link; change or remove the link
// and the card goes with it (a fresh card would need a fetch, and an edit
// fetches nothing).
export function cardEditSave(edited, seed) {
  if (!seed?.block || cardLink(edited) !== seed.url) return edited;
  return withCard(edited, seed.url, seed.block, seed.above);
}

// ---- reading ----

// cardVideo(url): what a card's picture does when tapped. "yt" plays in place
// (the id is checked, the host must really be YouTube's, and the player URL
// is rebuilt from the id — lib/embeds.js ytEmbed), "vimeo" is drawn as a video
// that opens on its site, "" is a page. Only these, decided by the URL's real
// host, get the big video layout, the play button and the length badge: a
// page that says it is a video (og:video:duration, a play button drawn into
// its picture) is still drawn as a page, with its real domain under it.
export function cardVideo(url) {
  let u;
  try {
    u = new URL(url);
  } catch {
    return { kind: "" };
  }
  const host = u.hostname.replace(/\.$/, "").toLowerCase();
  let id = "";
  if (host === "youtu.be") id = u.pathname.slice(1).split("/")[0];
  else if (host === "youtube.com" || host.endsWith(".youtube.com")) {
    if (u.pathname === "/watch") id = u.searchParams.get("v") || "";
    else {
      const m = /^\/(?:shorts|embed|live)\/([^/]+)/.exec(u.pathname);
      if (m) id = m[1];
    }
  }
  if (/^[A-Za-z0-9_-]{11}$/.test(id)) return { kind: "yt", id };
  if ((host === "vimeo.com" || host === "www.vimeo.com") && /^\/\d{1,12}\/?$/.test(u.pathname)) return { kind: "vimeo" };
  return { kind: "" };
}
