// linkembed.js — the first sender-side link preview form, retired before any
// release.
//
// A development build of main appended a rider token after the text:
//   [lp](concord://lp/v1/<base64url(JSON array)>)
// A v0.60 reader printed it raw, several hundred characters of base64 under
// the message, so senders now write a link card instead, as markdown every
// client already reads (lib/linkcard.js, internal/app/linkcard.go). Messages
// those builds sent still exist, so the rider is stripped wherever a body is
// shown and nothing is drawn from it: a card is drawn only from the card form,
// whose every line prints the link's real domain.

export const LP_RE = /\[lp\]\(concord:\/\/lp\/v1\/([A-Za-z0-9_-]+)\)/;
const LP_RE_G = new RegExp(LP_RE.source, "g");

// stripLinkEmbed removes every rider token so the text renders clean.
export function stripLinkEmbed(content) {
  return content ? content.replace(LP_RE_G, "").trim() : content;
}
