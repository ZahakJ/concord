// Zero-dependency test for link cards (`npm test`): what a reader recognises
// as a card, what it refuses to, and what the sender's composer appends.
import {
  parseCard,
  cardLink,
  withCard,
  wantsThumb,
  cardVideo,
  cleanText,
  restForDisplay,
  cardFor,
  cardEditSeed,
  cardEditSave,
  sendingCards,
  askAboutCards,
  SEND_DEFAULT,
  CARD_QUESTION,
  cardChoiceNote,
  cardChoice,
  CARD_PREF,
  leadEmbed,
  placeLinkFirst,
} from "./linkcard.js";
import { plainSnippet } from "./snippet.js";

let failures = 0;
const eq = (got, want, msg) => {
  if (JSON.stringify(got) !== JSON.stringify(want)) {
    console.error(`FAIL: ${msg}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
};

const BLOB = "a".repeat(64);
const KEYS = "b".repeat(75);
const b64 = (s) => Buffer.from(s, "utf8").toString("base64url");
const thumb = (flags, desc) =>
  `![image](concord://attach/v2/${BLOB}/${KEYS}/jpeg/480x270/${flags}//${b64(desc)})`;
const YT = "https://www.youtube.com/watch?v=dQw4w9WgXcQ";

// The card the sender's core writes for a YouTube link, alone in a message.
let c = parseCard(`[Never Gonna Give You Up](${YT}) · youtube.com\n${thumb(2, "Rick Astley")}`);
eq(c && [c.title, c.site, c.duration, c.domain, c.linkOnly, c.rest], ["Never Gonna Give You Up", "Rick Astley", "", "youtube.com", true, ""], "youtube card");
// YouTube gives the sender no length (oEmbed has none), so a YouTube card that
// carries one was written by hand: it is not printed (review LC-11). Vimeo and
// pages that publish one keep it.
c = parseCard(`[Never Gonna Give You Up](${YT})\n${thumb(2, "Rick Astley · 3:33")}`);
eq(c && [c.site, c.duration], ["Rick Astley", ""], "a YouTube length nobody could know");
c = parseCard(`[A film](https://vimeo.com/76979871) · vimeo.com\n${thumb(2, "Someone · 1:02:05")}`);
eq(c && [c.site, c.duration, c.video.kind], ["Someone", "1:02:05", "vimeo"], "vimeo keeps its length");
eq(c?.thumb?.card, true, "thumb is flagged");

// A link in a sentence keeps the sentence.
c = parseCard(`look at this ${YT} lol\n[Never Gonna Give You Up](${YT})\n${thumb(2, "Rick Astley")}`);
eq(c && [c.rest, c.linkOnly, c.site, c.duration], [`look at this ${YT} lol`, false, "Rick Astley", ""], "card under a sentence");

// Title only.
c = parseCard("[A page title](https://example.org/a)");
eq(c && [c.title, c.domain, c.thumb], ["A page title", "example.org", null], "title-only card");

// Escapes are undone for display.
c = parseCard("[A \\*starred\\* \\[thing\\]](https://example.org/a)");
eq(c?.title, "A *starred* [thing]", "escaped title");

// Bidi controls never reach the screen.
c = parseCard("[Invoice \u202egnp.exe\u202c](https://example.org/a)");
eq(c?.title, "Invoice gnp.exe", "bidi stripped");
eq(cleanText("x".repeat(300), 200).length, 201, "title capped (plus ellipsis)");

// Refusals.
eq(parseCard("[paypal.com](https://evil.example/login)"), null, "deceptive text is not a card");
eq(parseCard("[https://example.org](https://example.org)"), null, "a URL as its own title is not a card");
eq(parseCard(`[Title](${YT})\n${thumb(0, "x")}`), null, "an ordinary picture after a masked line is not a card picture");
eq(parseCard(`${thumb(2, "x")}`), null, "a flagged picture alone is just a picture");
eq(parseCard(`[Title](${YT}) and more`), null, "masked link inside a sentence is not a card");
eq(parseCard(`[Title](javascript:alert(1))`), null, "non-web link");
eq(parseCard(`[Title](${YT})\n${thumb(3, "x")}`), null, "a spoilered picture is never a card picture");

// The domain after the title is for v0.60 readers (review LC-07). This client
// hides it only when it is exactly the domain it computes; any other is not a
// card's, and the line stays words.
c = parseCard("[A page title](https://www.example.org/a) · example.org");
eq(c && [c.title, c.domain, c.rest], ["A page title", "example.org", ""], "the old readers' domain is hidden");
eq(parseCard("[A page title](https://evil.example/a) · paypal.com"), null, "a domain that is not the link's is not a card's");
eq(parseCard("[A page title](https://evil.example/a) · EVIL.example")?.domain, "evil.example", "case does not matter");

// Invisible and look-alike characters do not get an address past the rule
// (review LC-03): checked as the eye reads it, after cleaning.
for (const t of ["paypal.com\u200b", "pay\u00adpal.com", "paypal\u2060.com", "paypal\u3002com", "paypal\uff61com", "ｐａｙｐａｌ．ｃｏｍ", "\u3164paypal.com", "paypal.com\u{e0041}"])
  eq(parseCard(`[${t}](https://evil.example/login)`), null, `disguised address ${JSON.stringify(t)} is not a card`);
eq(cleanText("pay\u00adpal \u2060x\u180e \u{e0041}y", 50), "paypal x y", "format characters are stripped from what a card prints");
eq(cleanText("👨\u200d👩\u200d👧 ❤\ufe0f", 50), "👨\u200d👩\u200d👧 ❤\ufe0f", "joiners and variation selectors stay");

// A page's site name cannot carry another site's address (review LC-01).
for (const [site, want] of [
  ["Rick Astley - Topic · youtube.com", "Rick Astley - Topic"],
  ["Rick Astley - Topic·youtube.com", "Rick Astley - Topic"],
  ["youtube.com | Rick Astley", "Rick Astley"],
  ["Rick Astley ｙｏｕｔｕｂｅ．ｃｏｍ", "Rick Astley"],
  ["Rick Astley you\u00adtube\u3002com", "Rick Astley"],
  ["Rick Astley https://www.youtube.com/@ra", "Rick Astley"],
  ["Node.js Foundation", "Node.js Foundation"],
]) {
  c = parseCard(`[Never Gonna Give You Up](https://evil.example/v) · evil.example\n${thumb(2, `${site} · 3:33`)}`);
  eq(c && [c.site, c.duration, c.video.kind, c.domain], [want, "3:33", "", "evil.example"], `site ${JSON.stringify(site)}`);
}
c = parseCard(`[Example](https://example.org/v)\n${thumb(2, "Example · example.org")}`);
eq(c?.site, "Example · example.org", "a site may name its own domain");

// The words around a card, as the feed shows them (review LC-13).
eq(restForDisplay(`check this out ${YT}`, YT), "check this out", "a link that ends the words is dropped");
eq(restForDisplay(`check this out: ${YT} !`, YT), "check this out!", "closing punctuation stays");
eq(restForDisplay(`this one ${YT} honestly`, YT), `this one ${YT} honestly`, "mid-sentence it stays (printed short)");
eq(restForDisplay(`see \`${YT}\``, YT), `see \`${YT}\``, "not inside code");
eq(restForDisplay(YT, YT), YT, "never empties the words");

// A duration only counts when it looks like one.
c = parseCard(`[T](${YT})\n${thumb(2, "Some · Channel")}`);
eq(c && [c.site, c.duration], ["Some · Channel", ""], "a middot in a channel name is not a duration");

// Sending.
eq(cardLink(`hey ${YT}`), YT, "first link");
eq(cardLink("`https://example.org` in code"), "", "not in code");
eq(cardLink("||https://example.org||"), "", "not under a spoiler");
eq(cardLink("[x](https://example.org)"), "", "not when there is a masked link already");
eq(cardLink("no links here"), "", "no link");
eq(withCard(YT, YT, "[T](u)"), "[T](u)", "link-only message becomes only the card");
eq(withCard(`hey ${YT}`, YT, "[T](u)"), `hey ${YT}\n[T](u)`, "sentence keeps its words");
eq(withCard(`[eph](concord://eph/v1/1900000000)${YT}`, YT, "[T](u)"), "[eph](concord://eph/v1/1900000000)\n[T](u)", "stamps stay in front");
{
  // A disappearing link-only message still reads as a card (the stamps are on
  // the line before it), and as a link-only one.
  const c2 = parseCard(withCard(`[eph](concord://eph/v1/1900000000)${YT}`, YT, `[T](${YT}) · youtube.com`));
  eq(c2 && [c2.title, c2.linkOnly], ["T", true], "a disappearing link-only card");
}
eq(wantsThumb(`[eph](concord://eph/v1/1900000000)${YT}`), false, "disappearing message: no picture");
eq(wantsThumb(YT), true, "ordinary message: picture");

// What plays in place, and what only looks like YouTube.
eq(cardVideo(YT), { kind: "yt", id: "dQw4w9WgXcQ" }, "watch url");
eq(cardVideo("https://youtu.be/dQw4w9WgXcQ?t=4"), { kind: "yt", id: "dQw4w9WgXcQ" }, "short url");
eq(cardVideo("https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ"), { kind: "" }, "lookalike host");
eq(cardVideo("https://evil.example/?u=youtu.be/dQw4w9WgXcQ"), { kind: "" }, "youtu.be in a query");
eq(cardVideo("https://vimeo.com/76979871"), { kind: "vimeo" }, "vimeo");

// Snippets (inbox, notifications, reply strip).
eq(
  plainSnippet(`[eph](concord://eph/v1/1900000000)\n[Never Gonna Give You Up](${YT}) · youtube.com`),
  "🔗 Never Gonna Give You Up · youtube.com",
  "a disappearing card's snippet is its title",
);
eq(plainSnippet(`[Never Gonna Give You Up](${YT}) · youtube.com\n${thumb(2, "Rick Astley")}`), "🔗 Never Gonna Give You Up · youtube.com", "card snippet");

// A card is drawn only on a row whose signature this device checked (ZL-08):
// an unsigned row came through another member, who could have written it.
{
  const content = `[Never Gonna Give You Up](${YT}) · youtube.com\n${thumb(2, "Rick Astley")}`;
  eq(!!cardFor({ content, kind: "" }), true, "a signed row draws its card");
  eq(cardFor({ content, kind: "", unverified: true }), null, "an unsigned row draws none");
  eq(cardFor({ content, kind: "", deleted: true }), null, "a deleted row draws none");
  eq(cardFor({ content, kind: "join" }), null, "a system row draws none");
}

// The edit box shows the words, not the card's lines (ZL-07), and the card goes
// back on while the link is unchanged.
{
  const block = `[Never Gonna Give You Up](${YT}) · youtube.com\n${thumb(2, "Rick Astley")}`;
  let seed = cardEditSeed(block);
  eq(seed.text, YT, "a link-only card edits as its link");
  eq(/concord:\/\/|\]\(/.test(seed.text), false, "no token or masked line in the edit box");
  eq(cardEditSave(seed.text, seed), block, "unchanged: the same content, so nothing is sent");
  eq(cardEditSave(`${YT} wow`, seed), `${YT} wow\n${block}`, "words added: the card stays");
  eq(cardEditSave("https://example.org/other", seed), "https://example.org/other", "another link: the card goes");
  seed = cardEditSeed(`look at this ${YT} lol\n${block}`);
  eq(seed.text, `look at this ${YT} lol`, "a sentence edits as the sentence");
  eq(cardEditSave("look at this lol", seed), "look at this lol", "link removed: the card goes");
  const eph = "[eph](concord://eph/v1/1900000000)";
  seed = cardEditSeed(`${eph}\n[T](${YT}) · youtube.com`);
  eq(seed.text, `${eph}${YT}`, "stamps stay in front of the link");
  eq(cardEditSave(seed.text, seed), `${eph}\n[T](${YT}) · youtube.com`, "and the card goes back after them");
  const lp = `[lp](concord://lp/v1/${b64(JSON.stringify([{ u: YT, t: "x" }]))})`;
  eq(cardEditSeed(`hi ${YT}\n${lp}`).text, `hi ${YT}`, "a development build's rider is not in the edit box");
  eq(cardEditSeed("plain words").text, "plain words", "no card, no change");
}

// The sending default (the owner's decision) and the first-link question.
eq(sendingCards(null, "off"), false, "off: undecided sends no card");
eq(sendingCards(null, "ask"), false, "ask: undecided sends no card until asked");
eq(sendingCards(null, "on"), true, "on: undecided sends cards");
eq(sendingCards(true, "off"), true, "a choice wins over the default");
eq(sendingCards(false, "on"), false, "a choice wins over the default (no)");
eq(askAboutCards(null, "ask"), true, "ask: the question comes while undecided");
eq(askAboutCards(false, "ask"), false, "asked once: No thanks is an answer");
eq(askAboutCards(true, "ask"), false, "asked once: Yes is an answer");
eq(askAboutCards(null, "off"), false, "off: never asked");
// The default this build ships with (owner's decision): asked, not on. A
// device that has not chosen sends its first link bare and gets the question.
eq(SEND_DEFAULT, "ask", "the shipped default is ask");
eq(sendingCards(null), false, "default: undecided sends no card");
eq(askAboutCards(null), true, "default: undecided is asked");
eq(sendingCards(true), true, "default: Yes sends cards");
eq(askAboutCards(true) || askAboutCards(false), false, "default: asked once only");
eq(
  CARD_QUESTION,
  "Show cards for links you send? Concord will look up the title and picture of each link you send, from your device, so others see a card. The site sees a visit from you, as if you had opened the link.",
  "the question, word for word",
);
// The question says what Yes does, in plain words.
for (const w of ["Concord will look up", "title and picture", "from your device", "so others see a card"])
  eq(CARD_QUESTION.includes(w), true, `the question says "${w}"`);

// The Settings switch: three states, each said under it.
eq(cardChoiceNote(null), "Not chosen yet: you will be asked at the first link you send", "Settings: undecided");
eq(cardChoiceNote(undefined), cardChoiceNote(null), "Settings: a missing pref is undecided");
eq(cardChoiceNote(true), "On: Concord looks up a link's title and picture, from your device, when you send it", "Settings: yes");
eq(
  cardChoiceNote(false),
  "Off: links you send go out as you typed them. You can turn this on here at any time.",
  "Settings: no, and it can be turned on there",
);

// Where the choice is kept. Builds before the question saved
// sendLinkPreviews: false with every pref; that is not an answer, so a device
// holding it is still undecided and still asked.
eq(CARD_PREF, "sendLinkCards", "the choice's key");
eq(cardChoice({ sendLinkPreviews: false }), null, "a stored sendLinkPreviews: false is not a choice");
eq(askAboutCards(cardChoice({ sendLinkPreviews: false, prefsV: 2 })), true, "a device holding the old false is asked");
eq(sendingCards(cardChoice({ sendLinkPreviews: true })), false, "nor is an old true: nothing is fetched before the question");
eq(cardChoice({ linkPreviews: true }), null, "the older linkPreviews key is not read");
eq(cardChoice({}), null, "nothing stored: undecided");
eq(cardChoice(null), null, "no prefs: undecided");
eq(cardChoice({ sendLinkCards: "yes" }), null, "a value that is not true or false: undecided");
eq(cardChoice({ sendLinkCards: true }), true, "Yes is read back");
eq(cardChoice({ sendLinkCards: false }), false, "No thanks is read back");
eq(askAboutCards(cardChoice({ sendLinkPreviews: false, sendLinkCards: false })), false, "No thanks under the new key is an answer");

// Placement: the sender chooses whether the card (or a bare video's player)
// sits above the words or below them. It is carried by the order of the
// lines themselves, so the signed content says it, every peer draws the same,
// and a reader that predates the choice still shows the card where it is.
{
  const block = `[Never Gonna Give You Up](${YT}) · youtube.com\n${thumb(2, "Rick Astley")}`;
  const eph = "[eph](concord://eph/v1/1900000000)";
  const up = withCard(`hey look ${YT}`, YT, block, true);
  eq(up, `${block}\nhey look ${YT}`, "above: the card goes first");
  const c = parseCard(up);
  eq(c && [c.title, c.above, c.rest, c.linkOnly], ["Never Gonna Give You Up", true, `hey look ${YT}`, false], "a card on top is read as one, above");
  eq(parseCard(withCard(`hey look ${YT}`, YT, block))?.above, false, "below is the default");
  eq(withCard(YT, YT, block, true), block, "a link-only message is only the card either way");
  eq(parseCard(block)?.above, false, "a card alone is not above anything");
  const stamped = withCard(`${eph}hey ${YT}`, YT, block, true);
  eq(stamped, `${eph}\n${block}\nhey ${YT}`, "above: after the stamps");
  const cs = parseCard(stamped);
  eq(cs && [cs.above, cs.rest], [true, `${eph}hey ${YT}`], "the stamps stay in the rest, in front");
  // A title-only card (a disappearing message) above.
  eq(parseCard(`[T](${YT}) · youtube.com\nwords`)?.above, true, "a title-only card on top");
  // What is not a card on top: a masked link the author typed among the words.
  eq(parseCard(`[my notes](https://example.org/n)\nand more`), null, "a typed masked link on the first line with no domain is not a card");
  eq(parseCard(`[T](${YT}) · evil.example\nwords`), null, "a top line whose domain is not the link's");
  // Old readers find the card only at the end: on top they draw a titled
  // link and a picture, then the words, which is the order the sender chose.
  // Editing keeps the placement.
  let seed = cardEditSeed(up);
  eq([seed.text, seed.above], [`hey look ${YT}`, true], "the edit box shows the words of an above card");
  eq(cardEditSave(seed.text, seed), up, "unchanged: the same content");
  eq(cardEditSave(`${seed.text}!`, seed), `${block}\nhey look ${YT}!`, "edited words keep the card on top");
  seed = cardEditSeed(stamped);
  eq(cardEditSave(seed.text, seed), stamped, "a stamped above card round-trips");
}

// A video link without a card: above is the link alone on the first line,
// which the feed replaces with the player.
{
  eq(placeLinkFirst(`hey look ${YT} cool`, YT), `${YT}\nhey look cool`, "the link moves to the first line");
  eq(placeLinkFirst(YT, YT), YT, "a link alone stays");
  const eph = "[eph](concord://eph/v1/1900000000)";
  eq(placeLinkFirst(`${eph}see ${YT}`, YT), `${eph}\n${YT}\nsee`, "after the stamps");
  eq(leadEmbed(`${YT}\nhey look cool`), { url: YT, rest: "hey look cool" }, "a lead link is found");
  eq(leadEmbed(`${eph}\n${YT}\nsee`), { url: YT, rest: `${eph}see` }, "after stamps");
  eq(leadEmbed(YT), null, "a link alone has nothing to sit above");
  eq(leadEmbed(`hey ${YT}`), null, "a link inside the words is not a lead");
  eq(leadEmbed(`https://example.org/x\nwords`), null, "only a video leads");
}

if (failures) {
  console.error(`${failures} linkcard test(s) failed`);
  process.exit(1);
}
console.log("linkcard: all tests passed");
