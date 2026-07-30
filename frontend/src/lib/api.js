// api.js is the frontend's single door to the Go backend. It works with three
// transports, chosen automatically:
//
//   - Wails desktop: calls the bindings Wails injects at window.go.main.App.*
//     and subscribes to events via window.runtime.EventsOn.
//   - Browser-served: POSTs to /rpc and streams events from /events (SSE).
//   - Mobile (Capacitor): same HTTP/SSE surface, but served by the in-process
//     Go core on a loopback port with a bearer token — the shell calls
//     configureTransport() with both before the app mounts.
//
// The rest of the UI is transport-agnostic — it only ever calls api.* and on().

// Mobile transport configuration. Empty base/token means the desktop behavior
// (relative URLs, no auth) — the webview origin IS the API origin there.
let apiBase = "";
let apiToken = "";

// configureTransport points the HTTP/SSE transport at the mobile core's
// loopback server. Must be called before the first api.* call.
export function configureTransport({ baseURL, authToken }) {
  apiBase = baseURL || "";
  apiToken = authToken || "";
}

function wailsBindings() {
  return typeof window !== "undefined" && window.go && window.go.main
    ? window.go.main.App
    : null;
}

const isWails = () => wailsBindings() !== null;

async function call(name, ...args) {
  const b = wailsBindings();
  if (b && typeof b[name] === "function") {
    return b[name](...args);
  }
  // Browser transport.
  const headers = { "Content-Type": "application/json" };
  if (apiToken) headers["Authorization"] = `Bearer ${apiToken}`;
  const res = await fetch(`${apiBase}/rpc`, {
    method: "POST",
    headers,
    body: JSON.stringify({ method: name, args }),
  });
  if (!res.ok) throw new Error(`rpc ${name}: HTTP ${res.status}`);
  const body = await res.json();
  if (body.error) throw new Error(body.error);
  return body.result;
}

// leaveVoiceOnUnload tells the backend we're gone while the page is being torn
// down. An ordinary fetch is cancelled mid-flight at that point; sendBeacon is
// the one request the browser promises to finish. Without it the Go node keeps
// announcing our presence every few seconds after the tab has closed, so
// everyone else holds a connection to a client that isn't there.
//
// Beacons can't carry headers, so this is skipped when a bearer token is in
// play (the mobile shell) — that shell doesn't reload pages anyway.
export function leaveVoiceOnUnload(channelID) {
  if (!channelID || apiToken || isWails() || typeof navigator === "undefined") return false;
  const body = new Blob([JSON.stringify({ method: "LeaveVoice", args: [channelID] })], {
    type: "application/json",
  });
  return navigator.sendBeacon?.(`${apiBase}/rpc`, body) ?? false;
}

export const api = {
  getBootstrap: () => call("GetBootstrap"),
  setBootstrap: (addrs) => call("SetBootstrap", addrs),
  setBootstrapLive: (addrs) => call("SetBootstrapLive", addrs),
  getPublicDht: () => call("GetPublicDHT"),
  setPublicDht: (on) => call("SetPublicDHT", on),
  getListenPort: () => call("GetListenPort"),
  setListenPort: (port) => call("SetListenPort", port),
  session: () => call("Session"),
  networkStatus: () => call("NetworkStatus"),
  nudge: () => call("Nudge"),
  registerPush: (platform, token) => call("RegisterPush", platform, token),
  linkOffer: () => call("LinkOffer"),
  cancelLinkOffer: () => call("CancelLinkOffer"),
  redeemLinkCode: (code, passphrase) => call("RedeemLinkCode", code, passphrase),
  logout: () => call("Logout"),
  hasIdentity: () => call("HasIdentity"),
  resetIdentity: () => call("ResetIdentity"),
  revealMnemonic: () => call("RevealMnemonic"),
  restoreFromMnemonic: (phrase, passphrase) => call("RestoreFromMnemonic", phrase, passphrase),
  restoreOverExisting: (phrase, passphrase) => call("RestoreOverExisting", phrase, passphrase),
  login: (passphrase) => call("Login", passphrase),
  identity: () => call("Identity"),
  guilds: () => call("Guilds"),
  createGuild: (name) => call("CreateGuild", name),
  notesDM: () => call("NotesDM"),
  startMeeting: () => call("StartMeeting"),
  // lifetimeHours picks how long the link (and the meeting room behind it)
  // lives, from the menu in internal/app/guild.go (meetingLifetimes — mirrored
  // by ModalMeeting's chips); 0 leaves it as it is. Calling this again for
  // the same meeting returns the SAME url with a new lifetime, so changing your
  // mind never kills a link you already sent.
  createGuestLink: (guildID, lifetimeHours = 0) => call("CreateGuestLink", guildID, lifetimeHours),
  meetingExpiry: (guildID) => call("MeetingExpiry", guildID),
  startDM: (fingerprint) => call("StartDM", fingerprint),
  createChannel: (guildID, name, type = "", category = "") =>
    call("CreateChannel", guildID, name, type, category),
  createCategory: (guildID, name) => call("CreateCategory", guildID, name),
  setChannelLinks: (guildID, channelID, links) => call("SetChannelLinks", guildID, channelID, links),
  // Forum posts. tags are ids from the forum's own palette (max 5); an id the
  // forum does not define is an error, not a silent drop.
  createThread: (guildID, forumID, title, firstMessage, tags = []) =>
    call("CreateThread", guildID, forumID, title, firstMessage, tags),
  // forumBoard is the ONE call a forum board needs: the tag palette plus every
  // post with the metadata a card shows. Author, reply count and excerpt are
  // DERIVED from each post's own messages rather than carried on the channel
  // record — so a post whose history hasn't synced yet comes back with
  // authorFingerprint "" and created 0. Render that as a pending card; it is not
  // a post by nobody. Posts arrive pinned-first, then newest activity first;
  // re-sorting and filtering are yours to do client-side over this list.
  // Times (created, lastActivity) are UnixNano, matching channel.lastActivity.
  forumBoard: (guildID, forumID) => call("ForumBoard", guildID, forumID),
  // setForumTags replaces the whole palette (Manage Channels). A tag is
  // {id, name, color, emoji}: omit id on a new tag and one is minted for you —
  // the returned palette carries the ids. Keep the id when editing a tag, or
  // every post carrying it comes untagged. Limits: 20 tags per forum, name 1–24
  // characters, emoji ≤8, color a strict "#rrggbb" (validated, because it lands
  // in a CSS context). Deleting a tag leaves posts carrying its id; resolve an
  // unknown id to nothing.
  setForumTags: (guildID, forumID, tags) => call("SetForumTags", guildID, forumID, tags),
  setPostTags: (guildID, postID, tags) => call("SetPostTags", guildID, postID, tags),
  // Pinning needs Manage Messages; tagging and answering also accept the post's
  // own author, so gate the buttons on both (see guild.myPerms).
  setPostPinned: (guildID, postID, pinned) => call("SetPostPinned", guildID, postID, pinned),
  setPostSolved: (guildID, postID, solved) => call("SetPostSolved", guildID, postID, solved),
  deleteChannel: (guildID, channelID) => call("DeleteChannel", guildID, channelID),
  deleteCategory: (guildID, categoryID) => call("DeleteCategory", guildID, categoryID),
  setGuildProfile: (guildID, name, icon, banner, description) =>
    call("SetGuildProfile", guildID, name, icon, banner, description),
  newDMInvite: () => call("NewDMInvite"),
  createGroupDM: (fingerprints) => call("CreateGroupDM", fingerprints),
  renameDM: (guildID, name) => call("RenameDM", guildID, name),
  setRichPresence: (enabled) => call("SetRichPresence", enabled),
  richPresenceEnabled: () => call("RichPresenceEnabled"),
  addCustomEmoji: (guildID, name, dataURI) => call("AddCustomEmoji", guildID, name, dataURI),
  removeCustomEmoji: (guildID, name) => call("RemoveCustomEmoji", guildID, name),
  // Guild GIF packs. The list is metadata only — each entry points at an
  // encrypted attachment blob, resolved through fetchAttachment like any image.
  // Searching it is a local filter in the picker: no query ever leaves the box.
  guildGifs: (guildID) => call("GuildGifs", guildID),
  addGuildGif: (guildID, name, tags, dataURL, w, h) =>
    call("AddGuildGif", guildID, name, tags, dataURL, w, h),
  removeGuildGif: (guildID, id) => call("RemoveGuildGif", guildID, id),
  sendGuildGif: (channelID, gifID, replyTo = "") => call("SendGuildGif", channelID, gifID, replyTo),
  // Tenor search, proxied through the user's own rendezvous. Note what is NOT
  // here: any way to get a URL. A result carries opaque handles, and both the
  // thumbnail and the full image come back from gifSearchMedia as inline data
  // URLs — so the browser never opens a connection to Google, which is the only
  // reason this feature is worth having. Do not "optimize" a handle into an
  // <img src>; that would silently undo the whole thing.
  gifSearchStatus: () => call("GifSearchStatus"),
  searchGifs: (query, pos = "") => call("SearchGifs", query, pos),
  gifSearchMedia: (ref, full = false) => call("GifSearchMedia", ref, full),
  sendSearchedGif: (channelID, ref, replyTo = "", w = 0, h = 0) =>
    call("SendSearchedGif", channelID, ref, replyTo, w, h),
  saveSearchedGif: (guildID, name, tags, ref, w = 0, h = 0) =>
    call("SaveSearchedGif", guildID, name, tags, ref, w, h),
  setChannelMeta: (guildID, channelID, type, category, position, topic = "") =>
    call("SetChannelMeta", guildID, channelID, type, category, position, topic),
  renameGuild: (guildID, name) => call("RenameGuild", guildID, name),
  leaveGuild: (guildID) => call("LeaveGuild", guildID),
  markRead: (channelID, atMs) => call("MarkRead", channelID, atMs),
  readState: () => call("ReadState"),
  appVersion: () => call("AppVersion"),
  canSelfUpdate: () => call("CanSelfUpdate"),
  applyUpdate: () => call("ApplyUpdate"),
  updateState: () => call("UpdateState"),
  restartApp: () => call("RestartApp"),
  setGames: (games) => call("SetGames", games),
  searchGames: (query) => call("SearchGames", query),
  deleteMessage: (channelID, messageID) => call("DeleteMessage", channelID, messageID),
  editMessage: (channelID, messageID, content) =>
    call("EditMessage", channelID, messageID, content),
  expireMessage: (channelID, messageID) => call("ExpireMessage", channelID, messageID),
  guildStats: (guildID) => call("GuildStats", guildID),
  networkStats: () => call("NetworkStats"),
  signalCall: (channelID, action, target = "", dest = "") =>
    call("SignalCall", channelID, action, target, dest),
  cancelPendingMember: (guildID, fingerprint) => call("CancelPendingMember", guildID, fingerprint),
  emptyTrash: (guildID = "") => call("EmptyTrash", guildID),
  blockUser: (fingerprint) => call("BlockUser", fingerprint),
  unblockUser: (fingerprint) => call("UnblockUser", fingerprint),
  blockedUsers: () => call("BlockedUsers"),
  messageRequests: () => call("MessageRequests"),
  acceptMessageRequest: (fingerprint) => call("AcceptMessageRequest", fingerprint),
  declineMessageRequest: (fingerprint, block = false) =>
    call("DeclineMessageRequest", fingerprint, block),
  typingEnabled: () => call("TypingEnabled"),
  setTypingEnabled: (on) => call("SetTypingEnabled", on),
  toggleReaction: (channelID, messageID, emoji) =>
    call("ToggleReaction", channelID, messageID, emoji),
  inviteCode: (guildID) => call("InviteCode", guildID),
  joinViaInvite: (code) => call("JoinViaInvite", code),
  messages: (channelID) => call("Messages", channelID),
  messagesBefore: (channelID, beforeISO, limit) => call("MessagesBefore", channelID, beforeISO, limit),
  unreadCounts: (sinceISO) => call("UnreadCounts", sinceISO),
  sendMessage: (channelID, content, replyTo = "") =>
    call("SendMessage", channelID, content, replyTo),
  sendCallNotice: (channelID, kind, content) =>
    call("SendCallNotice", channelID, kind, content),
  // Resolves to the new blob's id — the meme editor keys its local render
  // recipe by it. Everything else ignores the return.
  sendAttachment: (channelID, dataURL, w, h, replyTo = "", spoiler = false, name = "", desc = "") =>
    call("SendAttachment", channelID, dataURL, w, h, replyTo, spoiler, name, desc),
  // Re-point one of your own image messages at a freshly sealed picture, in
  // place. Also resolves to the new blob id. NOT sendAttachment + delete: that
  // leaves a tombstone where the original was.
  editAttachment: (channelID, messageID, dataURL, w, h) =>
    call("EditAttachment", channelID, messageID, dataURL, w, h),
  fetchAttachment: (channelID, blobID, keys, subtype) =>
    call("FetchAttachment", channelID, blobID, keys, subtype),
  sendFile: (channelID, dataURL, filename, replyTo = "") =>
    call("SendFile", channelID, dataURL, filename, replyTo),
  fetchFile: (channelID, blobID, keys, mime) =>
    call("FetchFile", channelID, blobID, keys, mime),
  linkPreview: (url) => call("LinkPreview", url),
  checkForUpdate: () => call("CheckForUpdate"),
  checkPeerUpdate: () => call("CheckPeerUpdate"),
  applyPeerUpdate: () => call("ApplyPeerUpdate"),
  members: (guildID) => call("Members", guildID),
  removeMember: (guildID, fingerprint) => call("RemoveMember", guildID, fingerprint),
  setNickname: (guildID, nick) => call("SetNickname", guildID, nick),
  setMemberNickname: (guildID, fpr, nick) => call("SetMemberNickname", guildID, fpr, nick),
  addMember: (guildID, fpr) => call("AddMember", guildID, fpr),
  purgeMessages: (channelID, n) => call("PurgeMessages", channelID, n),
  roles: (guildID) => call("Roles", guildID),
  upsertRole: (guildID, roleID, name, color, perms, position) =>
    call("UpsertRole", guildID, roleID, name, color, perms, position),
  deleteRole: (guildID, roleID) => call("DeleteRole", guildID, roleID),
  assignRole: (guildID, fingerprint, roleID, add) =>
    call("AssignRole", guildID, fingerprint, roleID, add),
  banMember: (guildID, fingerprint) => call("BanMember", guildID, fingerprint),
  unbanMember: (guildID, fingerprint) => call("UnbanMember", guildID, fingerprint),
  muteMember: (guildID, fingerprint, minutes) => call("MuteMember", guildID, fingerprint, minutes),
  unmuteMember: (guildID, fingerprint) => call("UnmuteMember", guildID, fingerprint),
  bans: (guildID) => call("Bans", guildID),
  contacts: () => call("Contacts"),
  joinVoice: (channelID) => call("JoinVoice", channelID),
  leaveVoice: (channelID) => call("LeaveVoice", channelID),
  relaySignal: (toPeerID, data) => call("RelaySignal", toPeerID, data),
  callIceServers: () => call("CallIceServers"),
  revealDeleted: (channelID, messageID) => call("RevealDeleted", channelID, messageID),
  sendTyping: (channelID) => call("SendTyping", channelID),
  setProfile: (name, status, emoji, color, avatar, banner = "", presence = "", bio = "", color2 = "", frame = "", effect = "", style = "") =>
    call("SetProfile", name, status, emoji, color, avatar, banner, presence, bio, color2, frame, effect, style),
  verifyFingerprint: (fingerprint) => call("VerifyFingerprint", fingerprint),
  pinMessage: (channelID, messageID) => call("PinMessage", channelID, messageID),
  searchMessages: (query) => call("SearchMessages", query),
};

// Shared SSE connection for the browser transport. EventSource can't set
// headers, so the mobile token rides a query parameter instead.
let eventSource = null;
function sse() {
  if (!eventSource) {
    const qs = apiToken ? `?token=${apiToken}` : "";
    eventSource = new EventSource(`${apiBase}/events${qs}`);
  }
  return eventSource;
}

// on subscribes to a backend event and returns an unsubscribe function.
// The desktop app streams events over the Wails runtime (window.runtime, which
// is injected even when the Go method bindings aren't); the browser build uses
// SSE. We probe window.runtime directly rather than via isWails() because the
// desktop build ships without the window.go.* bindings (RPC goes over HTTP).
export function on(event, handler) {
  const rt = typeof window !== "undefined" ? window.runtime : null;
  if (rt && typeof rt.EventsOn === "function") return rt.EventsOn(event, handler);
  const listener = (e) => {
    let data = null;
    try {
      data = e.data ? JSON.parse(e.data) : null;
    } catch {
      data = null;
    }
    handler(data);
  };
  sse().addEventListener(event, listener);
  return () => sse().removeEventListener(event, listener);
}
