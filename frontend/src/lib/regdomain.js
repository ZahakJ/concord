// regdomain.js — the registrable domain (eTLD+1) of a link's destination: the
// part of a host name somebody actually had to register, and therefore the
// part a lookalike cannot borrow. "youtube.com.evil.example" is evil.example;
// "evil.github.io" is evil.github.io, not github.io, because github.io hands
// out names to anyone.
//
// It is what every link card and every masked link prints beside itself, so it
// is computed from the URL alone, never from anything a fetched page said
// about itself. The list is psl.js, generated from the copy embedded in the
// Go core's golang.org/x/net/publicsuffix (scripts/gen-psl.go), so the sender's
// core and every reader agree; regdomain.test.mjs holds them to that.
import { RULES, TLDS } from "./psl.js";

let exact = null; // Set of normal rules of two or more labels
let wild = null; // Set of parents P for which "*.P" is a rule
let except = null; // Set of exception rules, without the "!"
let tlds = null;

function load() {
  if (exact) return;
  exact = new Set();
  wild = new Set();
  except = new Set();
  for (const group of RULES.split(";")) {
    const at = group.indexOf(":");
    const parent = group.slice(0, at);
    for (const first of group.slice(at + 1).split(",")) {
      if (first === "*") wild.add(parent);
      else if (first.startsWith("!")) except.add(`${first.slice(1)}.${parent}`);
      else exact.add(`${first}.${parent}`);
    }
  }
  tlds = new Set(TLDS.split(" "));
}

const IPV4 = /^\d{1,3}(?:\.\d{1,3}){3}$/;

// publicSuffix: the public suffix of an already-lowercased ASCII host, by the
// standard algorithm — the longest matching rule wins, an exception beats the
// wildcard it carves out of, and an unlisted TLD falls to the default "*".
function publicSuffix(labels) {
  load();
  // Single-label TLD rules are implied by the default rule; wildcards on a
  // TLD itself ("*.ck") still have to be looked at, which the loop does.
  for (let i = 0; i < labels.length; i++) {
    const cand = labels.slice(i).join(".");
    if (except.has(cand)) return labels.slice(i + 1);
    if (exact.has(cand)) return labels.slice(i);
    if (i + 1 < labels.length && wild.has(labels.slice(i + 1).join("."))) return labels.slice(i);
  }
  return labels.slice(-1);
}

// hostOf: the destination host of a URL string, lowercased and in ASCII
// (punycode) form — the WHATWG parser already does both — or "" when the
// string is not an http(s) URL.
export function hostOf(url) {
  try {
    const u = new URL(url);
    if (u.protocol !== "http:" && u.protocol !== "https:") return "";
    return u.hostname.replace(/\.$/, "");
  } catch {
    return "";
  }
}

// registrableDomain: eTLD+1 of a host. An IP literal or a host that IS a
// public suffix has none, and is returned whole: it is still the honest thing
// to print, and printing nothing would hide where the link goes.
export function registrableDomain(host) {
  const h = String(host || "")
    .toLowerCase()
    .replace(/\.$/, "");
  return etld1(h) || h;
}

// etld1: the strict form, "" where there is no registrable domain — the same
// answers Go's publicsuffix.EffectiveTLDPlusOne gives (regdomain.test.mjs).
export function etld1(host) {
  const h = String(host || "").toLowerCase();
  if (!h || IPV4.test(h) || h.startsWith("[") || h.includes(":")) return "";
  const labels = h.split(".");
  if (labels.some((l) => !l)) return "";
  const suffix = publicSuffix(labels);
  if (suffix.length >= labels.length) return "";
  return labels.slice(labels.length - suffix.length - 1).join(".");
}

// linkDomain: what to print beside a link — the registrable domain of its URL.
export function linkDomain(url) {
  return registrableDomain(hostOf(url));
}

// isTLD: whether the list knows this label as a top-level domain. Used to tell
// a title that names a site ("paypal.com") from one that merely has a dot in
// it ("Node.js").
export function isTLD(label) {
  load();
  return tlds.has(String(label || "").toLowerCase());
}

// foldForCheck: text as an address check must read it, the way the eye does.
// NFKC turns fullwidth "ｐａｙｐａｌ．ｃｏｍ" into paypal.com; the full stops a
// browser's URL parser treats as dots (U+3002, and its halfwidth and small
// forms) become dots; and every default-ignorable code point goes — zero-width
// characters, the soft hyphen, word joiners, Hangul fillers, tag characters,
// joiners and variation selectors included — since "pay\u00adpal.com" shows
// as paypal.com. Control characters go too. The sender's core folds the same
// way (internal/app/linkcard.go foldForCheck).
const DOTS_RE = /[\u3002\uff0e\uff61\ufe52]/g;
const IGNORABLE_RE = /[\p{Default_Ignorable_Code_Point}\p{Cf}\p{Cc}]/gu;
export function foldForCheck(text) {
  return String(text || "")
    .normalize("NFKC")
    .replace(DOTS_RE, ".")
    .replace(/\s+/g, " ")
    .replace(IGNORABLE_RE, "");
}

// A link text that reads as an address: a scheme, a leading www., or a bare
// host (two or more labels) with an optional port/path/query after it.
const SCHEME_RE = /^[a-z][a-z0-9+.-]*:\/\//i;
const HOSTISH_RE = /^(?:www\.)?((?:[\p{L}\p{N}-]+\.)+([\p{L}\p{N}-]{2,}))(?::\d+)?(?:[/?#]\S*)?$/u;

// textDomain: when `text` reads as a web address, the registrable domain it
// names (or "?" when it plainly is one but cannot be parsed); "" when it does
// not read as an address at all.
export function textDomain(text) {
  // Wrapping punctuation is not part of what the words name: "*paypal.com*",
  // "(paypal.com)" and "paypal.com." all name paypal.com.
  const t = foldForCheck(text)
    .trim()
    .replace(/^[\s"'`*_~|([{<«“‘]+|[\s"'`*_~|)\]}>»”’.,:;!?]+$/gu, "");
  if (!t || /\s/.test(t)) return "";
  if (SCHEME_RE.test(t) || /^www\./i.test(t)) {
    const host = hostOf(SCHEME_RE.test(t) ? t : `https://${t}`);
    return host ? registrableDomain(host) : "?";
  }
  const m = HOSTISH_RE.exec(t);
  if (!m) return "";
  // "Node.js" and "ASP.NET Core" are names, not places — the last label has to
  // be a real TLD (in either script) before the text counts as an address.
  const host = hostOf(`https://${m[1]}`);
  if (!host) return "";
  const tld = host.split(".").pop();
  if (!isTLD(tld)) return "";
  return registrableDomain(host);
}

// A text that names a DIFFERENT site from the one the link goes to is not fit
// to be the link's face: "[paypal.com](https://evil.example)" is the textbook
// phish. Same site ("[youtube.com](https://www.youtube.com/…)") is fine.
export function deceptiveText(text, url) {
  const named = textDomain(text);
  if (!named) return false;
  return named !== linkDomain(url);
}

// sameAsURL: the text is just the URL (give or take the scheme, a www. and a
// trailing slash), so there is nothing to disclose beside it.
export function sameAsURL(text, url) {
  const norm = (s) =>
    foldForCheck(s)
      .trim()
      .replace(SCHEME_RE, "")
      .replace(/^www\./i, "")
      .replace(/\/$/, "")
      .toLowerCase();
  return norm(text) === norm(url);
}

// Stretches of text that read as a web address, inside other words too:
// "Topic·youtube.com" holds one.
const DOMAINISH_RE = /(?:[a-z][a-z0-9+.-]*:\/\/)?(?:[\p{L}\p{N}-]+\.)+[\p{L}\p{N}-]{2,}(?:[/?#]\S*)?/giu;
const SEP = "[\\s·•|:,;/\\-–—]";
const SEP_EDGES_RE = new RegExp(`^${SEP}+|${SEP}+$`, "gu");

// stripForeignDomains(text, domain): `text` without any address that names a
// site other than `domain`, the link's own registrable domain. A card's site
// or channel name is the page's claim about itself, printed right above the
// real domain, so "Rick Astley - Topic · youtube.com" from evil.example must
// lose the address. Text with nothing to remove comes back unchanged;
// otherwise its folded form, the addresses taken out, is what is left. The
// sender's core applies the same rule (internal/app/linkcard.go).
export function stripForeignDomains(text, domain) {
  const folded = foldForCheck(text);
  let hit = false;
  const out = folded.replace(DOMAINISH_RE, (tok) => {
    const named = textDomain(tok);
    if (!named || named === domain) return tok;
    hit = true;
    return "";
  });
  if (!hit) return String(text || "");
  return out
    .replace(/\s+/g, " ")
    .replace(/\s*[·•|]\s*(?:[·•|]\s*)+/g, " · ")
    .replace(SEP_EDGES_RE, "")
    .trim();
}
