// openlink.js — which conversation a concord:// link asks to open, if any.
//
// Two shapes open a conversation, and nothing else:
//   concord://open?c=<id>       from another of the owner's apps (Jayb's board;
//                               jaybos CONTRACTS.md §5)
//   concord://channel?id=<id>   a tapped message notification
// The whole URL must be exactly that: one parameter, an id of 1 to 64
// characters of [A-Za-z0-9_-], no fragment, no second parameter, nothing
// encoded. Anything else is no conversation (""), and the caller does nothing.
// A link only opens: it never sends, joins, accepts or changes anything, and
// an id that names none of the owner's conversations opens nothing, quietly
// (jumpToChannel looks only among his own guilds).

const ID = "[A-Za-z0-9_-]{1,64}";
const OPEN = new RegExp(`^concord://open\\?c=(${ID})$`);
const CHANNEL = new RegExp(`^concord://channel\\?id=(${ID})$`);

export function conversationFromURL(url) {
  if (typeof url !== "string" || url.length > 200) return "";
  const m = OPEN.exec(url) || CHANNEL.exec(url);
  return m ? m[1] : "";
}

// isConversationLink: the URL is one of the two shapes above, valid or not, so
// the device-linking path never sees it.
export function isConversationLink(url) {
  return /^concord:\/\/(open|channel)(\?|$|\/)/i.test(url || "");
}
