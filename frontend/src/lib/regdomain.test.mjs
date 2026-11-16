// Zero-dependency test for the registrable-domain helper (`npm test`). The
// domain printed beside a link is the one thing a lookalike site must not be
// able to choose, so the answers are pinned against Go's own
// publicsuffix.EffectiveTLDPlusOne (regdomain.vectors.json, written by
// scripts/gen-psl.go from the same list) and against the classic tricks.
import { readFileSync } from "node:fs";
import { etld1, linkDomain, registrableDomain, textDomain, deceptiveText, sameAsURL, foldForCheck, stripForeignDomains } from "./regdomain.js";
import { PSL_VERSION } from "./psl.js";

let failures = 0;
const eq = (got, want, msg) => {
  if (got !== want) {
    console.error(`FAIL: ${msg}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
};

const vec = JSON.parse(readFileSync(new URL("./regdomain.vectors.json", import.meta.url), "utf8"));
eq(vec.version, PSL_VERSION, "vectors and psl.js come from the same list");
for (const [host, want] of Object.entries(vec.etld1)) eq(etld1(host), want, `Go agreement for ${host}`);

// Lookalikes: the printed domain is the real one, however the host is dressed.
eq(linkDomain("https://youtube.com.evil.example/watch?v=dQw4w9WgXcQ"), "evil.example", "suffix-stuffed lookalike");
eq(linkDomain("https://www.youtube.com/watch?v=dQw4w9WgXcQ"), "youtube.com", "real youtube");
eq(linkDomain("https://m.youtube.com/watch?v=x"), "youtube.com", "mobile youtube");
// A Cyrillic "\u043e" in youtube: the WHATWG parser punycodes it, and the punycode is
// what is printed — it must not come out as "youtube.com".
const cyr = linkDomain("https://y\u043eutube.com/watch?v=x");
if (!cyr.startsWith("xn--") || cyr === "youtube.com") {
  console.error(`FAIL: homoglyph host printed as ${cyr}`);
  failures++;
}
eq(linkDomain("https://evil.github.io/x"), "evil.github.io", "private suffix keeps the user's label");
eq(linkDomain("https://news.bbc.co.uk/"), "bbc.co.uk", "multi-label public suffix");
eq(linkDomain("https://user:pw@paypal.com@evil.example/"), "evil.example", "userinfo trick");
eq(linkDomain("https://127.0.0.1/"), "127.0.0.1", "IP literal printed whole");
eq(linkDomain("javascript:alert(1)"), "", "not a web URL");
eq(registrableDomain("github.io"), "github.io", "a bare public suffix is printed whole");

// Does a link text name a site?
eq(textDomain("paypal.com"), "paypal.com", "bare domain");
eq(textDomain("https://www.paypal.com/signin"), "paypal.com", "full url");
eq(textDomain("www.paypal.com"), "paypal.com", "www form");
eq(textDomain("Node.js"), "", "a name with a dot is not an address");
eq(textDomain("Never Gonna Give You Up"), "", "a title is not an address");
eq(textDomain("example.co.uk/path?q=1"), "example.co.uk", "path after the host");

eq(deceptiveText("paypal.com", "https://evil.example/login"), true, "classic phish");
eq(deceptiveText("youtube.com", "https://www.youtube.com/watch?v=x"), false, "same site");
eq(deceptiveText("Docs", "https://evil.example"), false, "plain words are not deceptive");
eq(deceptiveText("youtube.com", "https://youtube.com.evil.example/"), true, "lookalike destination");
eq(sameAsURL("https://example.org/", "https://example.org"), true, "url as its own text");
eq(sameAsURL("example.org", "https://www.example.org/"), true, "scheme and www dropped");

// Read as the eye reads it (review LC-03): NFKC, browser full stops, and no
// default-ignorable characters.
eq(foldForCheck("ｐａｙｐａｌ．ｃｏｍ"), "paypal.com", "fullwidth folds");
eq(foldForCheck("pay\u00adpal\u2060\u200b\u200d\ufe0f\u3002com"), "paypal.com", "invisibles go, full stops are dots");
eq(foldForCheck("paypal\uff61com"), "paypal.com", "halfwidth ideographic full stop");
eq(foldForCheck("\u3164\u115f\u{e0041}x"), "x", "Hangul fillers and tags go");
for (const t of ["pay\u00adpal.com", "paypal\u2060.com", "paypal\u3002com", "ｐａｙｐａｌ．ｃｏｍ", "paypal.com\u200b"])
  eq(deceptiveText(t, "https://evil.example/login"), true, `disguised ${JSON.stringify(t)}`);
eq(sameAsURL("ｅｘａｍｐｌｅ．ｏｒｇ", "https://example.org"), true, "fullwidth address is the address");

// A site name loses every address that is not the link's own (review LC-01).
eq(stripForeignDomains("Rick Astley - Topic · youtube.com", "evil.example"), "Rick Astley - Topic", "trailing address");
eq(stripForeignDomains("Topic·youtube.com·x", "evil.example"), "Topic · x", "glued with middle dots");
eq(stripForeignDomains("Example · example.org", "example.org"), "Example · example.org", "own domain stays");
eq(stripForeignDomains("Node.js Foundation", "evil.example"), "Node.js Foundation", "a name with a dot stays");
eq(stripForeignDomains("www.paypal.com", "evil.example"), "", "nothing but an address");
// LC2-04: separators that only look like dots.
eq(stripForeignDomains("Rick Astley youtube\u00b7com", "evil.example"), "Rick Astley", "middle dot");
eq(stripForeignDomains("Rick Astley youtube\u2027com", "evil.example"), "Rick Astley", "hyphenation point");
eq(stripForeignDomains("Rick Astley youtube[.]com", "evil.example"), "Rick Astley", "[.]");
eq(stripForeignDomains("Rick Astley youtube(.)com", "evil.example"), "Rick Astley", "(.)");
eq(stripForeignDomains("Rick Astley youtube\u2219com", "evil.example"), "Rick Astley", "bullet operator");
eq(stripForeignDomains("Rick Astley youtube\u22c5com", "evil.example"), "Rick Astley", "dot operator");
eq(stripForeignDomains("Rick Astley youtube\u30fbcom", "evil.example"), "Rick Astley", "katakana middle dot");
eq(stripForeignDomains("Rick Astley youtube\uff65com", "evil.example"), "Rick Astley", "halfwidth katakana middle dot");
eq(stripForeignDomains("Topic\u00b7youtube\u00b7com", "evil.example"), "Topic", "glued, the words before stay");
eq(stripForeignDomains("Topic\u00b7youtube\u00b7com\u00b7x", "evil.example"), "Topic \u00b7 x", "glued on both sides");
eq(stripForeignDomains("News bbc[.]co[.]uk", "evil.example"), "News", "a two-label suffix goes whole");
eq(stripForeignDomains("Rick Astley \u00b7 3:33", "evil.example"), "Rick Astley \u00b7 3:33", "the spaced separator is not an address");
eq(stripForeignDomains("Col\u00b7lecci\u00f3 d'art", "evil.example"), "Col\u00b7lecci\u00f3 d'art", "a Catalan middle dot is a letter's");
eq(stripForeignDomains("Rick Astley youtube\u00b7com", "youtube.com"), "Rick Astley youtube\u00b7com", "its own domain stays");

if (failures) {
  console.error(`${failures} regdomain test(s) failed`);
  process.exit(1);
}
console.log("regdomain: all tests passed");
