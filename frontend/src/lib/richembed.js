// richembed.js — a "rich embed" is an author-built card (accent colour, title,
// description, and up to a few name/value fields) carried inline in a message,
// the same way polls are: a single opaque token in the message content, so it
// needs zero backend changes and syncs like any other message. Rendering lives
// in EmbedView.svelte.

export const EMBED_RE = /\[embed\]\(concord:\/\/embed\/v1\/([A-Za-z0-9_-]+)\)/;

const MAX_TITLE = 200;
const MAX_DESC = 2000;
const MAX_FIELDS = 8;
const MAX_FIELD_NAME = 100;
const MAX_FIELD_VALUE = 400;

function b64urlEncode(str) {
  return btoa(unescape(encodeURIComponent(str)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
function b64urlDecode(s) {
  try {
    return decodeURIComponent(escape(atob(s.replace(/-/g, "+").replace(/_/g, "/"))));
  } catch {
    return "";
  }
}

// Only a #hex colour is ever kept — it renders into inline CSS, so anything
// else is dropped (same rule the profile/markdown colours follow).
const HEX_RE = /^#[0-9a-fA-F]{3,6}$/;
function cleanColor(c) {
  return typeof c === "string" && HEX_RE.test(c.trim()) ? c.trim() : "";
}

// encodeEmbed({ color, title, desc, fields }) -> the message content token.
export function encodeEmbed(embed) {
  const clean = {
    color: cleanColor(embed.color),
    title: String(embed.title || "").slice(0, MAX_TITLE),
    desc: String(embed.desc || "").slice(0, MAX_DESC),
    fields: (embed.fields || [])
      .map((f) => ({
        name: String(f.name || "").slice(0, MAX_FIELD_NAME),
        value: String(f.value || "").slice(0, MAX_FIELD_VALUE),
      }))
      .filter((f) => f.name || f.value)
      .slice(0, MAX_FIELDS),
  };
  return `[embed](concord://embed/v1/${b64urlEncode(JSON.stringify(clean))})`;
}

// parseEmbed(content) -> a validated embed object, or null if there's no embed.
export function parseEmbed(content) {
  if (!content) return null;
  const m = content.match(EMBED_RE);
  if (!m) return null;
  try {
    const e = JSON.parse(b64urlDecode(m[1]));
    if (!e || typeof e !== "object") return null;
    const embed = {
      color: cleanColor(e.color),
      title: typeof e.title === "string" ? e.title.slice(0, MAX_TITLE) : "",
      desc: typeof e.desc === "string" ? e.desc.slice(0, MAX_DESC) : "",
      fields: Array.isArray(e.fields)
        ? e.fields
            .map((f) => ({
              name: typeof f?.name === "string" ? f.name.slice(0, MAX_FIELD_NAME) : "",
              value: typeof f?.value === "string" ? f.value.slice(0, MAX_FIELD_VALUE) : "",
            }))
            .filter((f) => f.name || f.value)
            .slice(0, MAX_FIELDS)
        : [],
    };
    // An embed with nothing in it isn't worth rendering.
    if (!embed.title && !embed.desc && embed.fields.length === 0) return null;
    return embed;
  } catch {
    return null;
  }
}

// stripEmbedToken removes the token from a body so the surrounding text renders
// without the raw concord:// link showing.
export function stripEmbedToken(content) {
  return content ? content.replace(EMBED_RE, "").trim() : content;
}
