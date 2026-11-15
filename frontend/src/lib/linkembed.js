// linkembed.js — link previews that the SENDER built.
//
// A message may carry, after its text, a rider token
//   [lp](concord://lp/v1/<base64url(JSON array)>)
// holding up to three previews (title, description, site name, the link, and a
// reference to an already-cleaned image blob). Receivers render only that. They
// never fetch the page or its image: the picture is an ordinary encrypted
// attachment blob, fetched from peers like any other.
//
// This mirrors internal/app/linkembed.go (ParseLinkEmbeds). The payload is
// hostile input from another member: sizes are capped, text is plain, URLs are
// http/https only, and a preview is shown only when its URL is exactly one of
// the links in the message text, so a card can never point where the words do
// not.
import { extractLinks } from "./embeds.js";

export const LP_RE = /\[lp\]\(concord:\/\/lp\/v1\/([A-Za-z0-9_-]+)\)/;
const LP_RE_G = new RegExp(LP_RE.source, "g");

export const MAX_EMBEDS = 3;
export const MAX_URL = 500;
export const MAX_TITLE = 200;
export const MAX_DESC = 400;
export const MAX_SITE = 100;
export const MAX_TOKEN = 8192;
export const MAX_IMAGE_PX = 400;

const BIDI_RE = /[‪-‮⁦-⁩‎‏﻿]/g;
// eslint-disable-next-line no-control-regex
const CTRL_RE = /[\u0000-\u0008\u000B-\u001F\u007F-\u009F]/g;

function plainText(s, max) {
  if (typeof s !== "string") return "";
  const t = s.replace(/[\n\r\t]/g, " ").replace(CTRL_RE, "").replace(BIDI_RE, "").replace(/\s+/g, " ").trim();
  return Array.from(t).slice(0, max).join("");
}

function httpURL(raw) {
  if (typeof raw !== "string" || !raw || raw.length > MAX_URL) return false;
  try {
    const u = new URL(raw);
    return (u.protocol === "http:" || u.protocol === "https:") && !!u.host && !u.username && !u.password;
  } catch {
    return false;
  }
}

function b64urlDecode(s) {
  try {
    const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
    return new TextDecoder().decode(Uint8Array.from(bin, (c) => c.charCodeAt(0)));
  } catch {
    return "";
  }
}

export function b64urlEncode(str) {
  const bytes = new TextEncoder().encode(str);
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// stripLinkEmbed removes every rider token so the text renders clean.
export function stripLinkEmbed(content) {
  return content ? content.replace(LP_RE_G, "").trim() : content;
}

// parseLinkEmbeds(content) -> validated [{url,title,desc,site,image}], where
// image is {blobId,keys,subtype,w,h} or null. Pure: no I/O.
export function parseLinkEmbeds(content) {
  const m = content && LP_RE.exec(content);
  if (!m || m[1].length > MAX_TOKEN) return [];
  let raw;
  try {
    raw = JSON.parse(b64urlDecode(m[1]));
  } catch {
    return [];
  }
  if (!Array.isArray(raw)) return [];
  const inText = new Set(extractLinks(stripLinkEmbed(content) || "", 50));
  const out = [];
  for (const e of raw) {
    if (out.length >= MAX_EMBEDS) break;
    if (!e || typeof e !== "object" || !httpURL(e.u) || !inText.has(e.u)) continue;
    const title = plainText(e.t, MAX_TITLE);
    const desc = plainText(e.d, MAX_DESC);
    if (!title && !desc) continue;
    let image = null;
    const i = e.i;
    if (
      i && typeof i === "object" &&
      typeof i.b === "string" && /^[0-9a-f]{64}$/.test(i.b) &&
      typeof i.k === "string" && /^[A-Za-z0-9_-]{75}$/.test(i.k) &&
      i.f === "jpeg" &&
      Number.isInteger(i.w) && i.w > 0 && i.w <= MAX_IMAGE_PX &&
      Number.isInteger(i.h) && i.h > 0 && i.h <= MAX_IMAGE_PX
    ) {
      image = { blobId: i.b, keys: i.k, subtype: "jpeg", w: i.w, h: i.h };
    }
    out.push({ url: e.u, title, desc, site: plainText(e.s, MAX_SITE), image });
  }
  return out;
}

// Which of the message's links the composer should prepare previews for. Must
// agree with the backend (linkURLs): the first few distinct links.
export function previewLinks(text) {
  return [...new Set(extractLinks(text || "", MAX_EMBEDS * 4))].slice(0, MAX_EMBEDS);
}

// --- composer side ----------------------------------------------------------
// createPreviewPrep: debounced "prepare while composing". `prepare(text)` is the
// backend call. The send path asks take(text) for whatever is READY for exactly
// the links currently in the text; it never waits, so a slow site delays
// nothing and a message sent early simply goes without a preview.
export function createPreviewPrep(prepare, { delay = 700, enabled = () => true } = {}) {
  let timer = null;
  let key = "";
  let ready = { key: "", token: "" };
  let gen = 0;
  return {
    update(text) {
      const links = previewLinks(text);
      const k = enabled() ? links.join("\n") : "";
      if (k === key) return;
      key = k;
      gen++;
      clearTimeout(timer);
      if (!k) return;
      const mine = gen;
      timer = setTimeout(async () => {
        try {
          const token = await prepare(links.join(" "));
          if (mine === gen && typeof token === "string") ready = { key: k, token };
        } catch {
          /* no preview: the message still sends */
        }
      }, delay);
    },
    // take returns the token for the text's current links, or "".
    take(text) {
      if (!enabled()) return "";
      const k = previewLinks(text).join("\n");
      return k && ready.key === k ? ready.token : "";
    },
    reset() {
      clearTimeout(timer);
      key = "";
      gen++;
      ready = { key: "", token: "" };
    },
  };
}

// withPreviews appends a ready token to a body. Strictly additive.
export function withPreviews(body, token) {
  return token ? `${body}\n${token}` : body;
}
