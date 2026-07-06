// embeds.js — link detection for native embeds. YouTube is handled entirely
// client-side (URLs are REBUILT from the validated video ID, so the raw
// user-supplied URL never reaches an attribute); everything else goes through
// the backend's SSRF-guarded LinkPreview scrape.
import { api } from "./api.js";

const LINK_RE = /https?:\/\/[^\s<]+/g;

// extractLinks pulls up to `max` http(s) links from PLAIN message content,
// ignoring anything inside code fences or inline code.
export function extractLinks(content, max = 2) {
  const scrubbed = content
    .split("```")
    .filter((_, i) => i % 2 === 0)
    .join(" ")
    .replace(/`[^`]*`/g, " ");
  const out = [];
  for (const m of scrubbed.matchAll(LINK_RE)) {
    // Trim common trailing punctuation that rides along in prose.
    out.push(m[0].replace(/[).,;!?]+$/, ""));
    if (out.length >= max) break;
  }
  return out;
}

// youtubeID returns the validated 11-char video ID, or null.
export function youtubeID(url) {
  const m =
    /(?:youtube\.com\/(?:watch\?[^#\s]*v=|shorts\/|embed\/)|youtu\.be\/)([A-Za-z0-9_-]{11})(?![A-Za-z0-9_-])/.exec(
      url,
    );
  return m ? m[1] : null;
}

export const ytThumb = (id) => `https://i.ytimg.com/vi/${id}/hqdefault.jpg`;
export const ytEmbed = (id) => `https://www.youtube-nocookie.com/embed/${id}?autoplay=1`;

// Preview cache: url -> Promise<PreviewView>. Failures cache as null (the
// component renders nothing) so hostile/dead links don't refetch in a loop.
const CACHE_MAX = 200;
const cache = new Map();

export function loadPreview(url) {
  let p = cache.get(url);
  if (!p) {
    p = api.linkPreview(url).catch(() => null);
    cache.set(url, p);
    if (cache.size > CACHE_MAX) cache.delete(cache.keys().next().value);
  }
  return p;
}
