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

export const api = {
  getBootstrap: () => call("GetBootstrap"),
  setBootstrap: (addrs) => call("SetBootstrap", addrs),
  setBootstrapLive: (addrs) => call("SetBootstrapLive", addrs),
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
  createGuestLink: (guildID) => call("CreateGuestLink", guildID),
  startDM: (fingerprint) => call("StartDM", fingerprint),
  createChannel: (guildID, name, type = "", category = "") =>
    call("CreateChannel", guildID, name, type, category),
  createCategory: (guildID, name) => call("CreateCategory", guildID, name),
  setChannelLinks: (guildID, channelID, links) => call("SetChannelLinks", guildID, channelID, links),
  createThread: (guildID, forumID, title, firstMessage) =>
    call("CreateThread", guildID, forumID, title, firstMessage),
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
  toggleReaction: (channelID, messageID, emoji) =>
    call("ToggleReaction", channelID, messageID, emoji),
  inviteCode: (guildID) => call("InviteCode", guildID),
  joinViaInvite: (code) => call("JoinViaInvite", code),
  messages: (channelID) => call("Messages", channelID),
  sendMessage: (channelID, content, replyTo = "") =>
    call("SendMessage", channelID, content, replyTo),
  sendCallNotice: (channelID, kind, content) =>
    call("SendCallNotice", channelID, kind, content),
  sendAttachment: (channelID, dataURL, w, h, replyTo = "") =>
    call("SendAttachment", channelID, dataURL, w, h, replyTo),
  fetchAttachment: (channelID, blobID, keys, subtype) =>
    call("FetchAttachment", channelID, blobID, keys, subtype),
  sendFile: (channelID, dataURL, filename, replyTo = "") =>
    call("SendFile", channelID, dataURL, filename, replyTo),
  fetchFile: (channelID, blobID, keys, mime) =>
    call("FetchFile", channelID, blobID, keys, mime),
  linkPreview: (url) => call("LinkPreview", url),
  checkForUpdate: () => call("CheckForUpdate"),
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
