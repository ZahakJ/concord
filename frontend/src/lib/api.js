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

// clientID names this page in the backend's background-pacing vote. One node
// can have several UIs attached — two browser tabs, a phone, the desktop
// window — and the core must only settle to its slow cadence when ALL of them
// are hidden, so each has to be distinguishable. The same id rides the /events
// query string, which is what gives the backend a lifetime to hang it on: the
// stream ending is how a CLOSED client is told apart from a merely hidden one.
// Per page load, not persisted — a reloaded tab is a new client, and the old
// one's stream has already ended.
export const clientID =
  typeof crypto !== "undefined" && crypto.randomUUID
    ? crypto.randomUUID()
    : `c${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`;

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

// ---- transport health ------------------------------------------------------
//
// A dead core is otherwise completely invisible. Every green dot, every peer
// count and every member row in the UI is drawn from the last state we were
// told about, so when the process behind the page dies the app carries on
// looking perfectly healthy — and the only thing that gives it away is a send
// failing with the browser's own "Failed to fetch".
//
// The HTTP transport is the only one that can know (a Wails binding call has no
// network under it), so it reports here and lib/state.svelte.js turns it into
// the reconnecting bar. Recovery needs no polling of its own: the EventSource
// redials by itself and its `open` says we're back.
let transportUp = true;
const healthWatchers = new Set();

export function onTransportHealth(cb) {
  healthWatchers.add(cb);
  cb(transportUp);
  return () => healthWatchers.delete(cb);
}

function reportTransport(up) {
  if (up === transportUp) return;
  transportUp = up;
  for (const cb of [...healthWatchers]) cb(up);
}

// The one error the UI should never print verbatim.
export function offlineError() {
  const err = new Error("Concord isn't responding — trying to reconnect");
  err.offline = true;
  return err;
}

async function call(name, ...args) {
  const b = wailsBindings();
  if (b && typeof b[name] === "function") {
    return b[name](...args);
  }
  // Browser transport.
  const headers = { "Content-Type": "application/json" };
  if (apiToken) headers["Authorization"] = `Bearer ${apiToken}`;
  let res;
  try {
    res = await fetch(`${apiBase}/rpc`, {
      method: "POST",
      headers,
      body: JSON.stringify({ method: name, args }),
    });
  } catch {
    // fetch only rejects when the request never reached anything — a refused
    // connection, a dropped socket, DNS. That is the core being gone, not this
    // particular call failing.
    reportTransport(false);
    throw offlineError();
  }
  reportTransport(true);
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
  reachability: () => call("ReachabilityStatus"),
  nudge: () => call("Nudge"),
  // Whether anyone is looking at THIS page; the backend votes across every
  // attached client. See lib/visibility.js and internal/bridge/visibility.go.
  setClientVisible: (visible) => call("SetClientVisible", clientID, visible),
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
  // Closing a post is moderation: every honest client refuses to send into it
  // AND drops anything that arrives for it, so a modified client can publish and
  // simply be ignored by everyone else.
  setPostLocked: (guildID, postID, locked) => call("SetPostLocked", guildID, postID, locked),
  // A forum's own artwork: a data URI, "preset:<id>", or "" to clear.
  setForumBanner: (guildID, forumID, banner) => call("SetForumBanner", guildID, forumID, banner),
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
  // Guild calendar events. Times are UTC Unix seconds (endUnix 0 = no stated
  // end); rsvp state is "going" | "maybe" | "no" | "" (clear your answer).
  // locationChannelId ties the event to one of the guild's OWN channels (Join
  // then navigates there instead of minting a meeting room; location stays as
  // the display label). "" keeps the location free text / external.
  // The ICS calls return RFC 5545 text to hand to the user's own calendar app
  // as a downloaded file — the format, never a vendor.
  createEvent: (guildID, title, details, startUnix, endUnix = 0, location = "", locationChannelId = "") =>
    call("CreateEvent", guildID, title, details, startUnix, endUnix, location, locationChannelId),
  updateEvent: (guildID, eventID, title, details, startUnix, endUnix = 0, location = "", locationChannelId = "") =>
    call("UpdateEvent", guildID, eventID, title, details, startUnix, endUnix, location, locationChannelId),
  deleteEvent: (guildID, eventID) => call("DeleteEvent", guildID, eventID),
  events: (guildID) => call("Events", guildID),
  rsvpEvent: (guildID, eventID, state) => call("RSVPEvent", guildID, eventID, state),
  eventICS: (guildID, eventID) => call("EventICS", guildID, eventID),
  eventsICS: (guildID) => call("EventsICS", guildID),
  // Guest access on an event: openEventGuests mints (or returns — the URL is
  // stable) a disposable meeting room and its browser-guest link, which lands
  // on the event record as guestUrl/guestHost. autoAdmit false = guests knock.
  // Revoke works only on the account that opened it (the room lives there).
  openEventGuests: (guildID, eventID, autoAdmit = false) =>
    call("OpenEventGuests", guildID, eventID, autoAdmit),
  revokeEventGuests: (guildID, eventID) => call("RevokeEventGuests", guildID, eventID),
  // One-tap Join for members: redeems the event's memberCode (a real invite
  // into the meeting room, no knock) or returns the room if already joined.
  joinEventRoom: (guildID, eventID) => call("JoinEventRoom", guildID, eventID),
  // Public booking page (Settings → Bookings). cfg is { enabled, blurb,
  // slotMinutes, horizonDays, windows: [{weekday, startMin, endMin}] } —
  // weekday 0 = Sunday, minutes counted from local midnight. The token/URL is
  // minted host-side; cancelBooking keys by the booking's calendar event id.
  bookingSettings: () => call("BookingSettings"),
  setBookingConfig: (cfg) => call("SetBookingConfig", cfg),
  cancelBooking: (eventID) => call("CancelBooking", eventID),
  setChannelMeta: (guildID, channelID, type, category, position, topic = "") =>
    call("SetChannelMeta", guildID, channelID, type, category, position, topic),
  renameChannel: (guildID, channelID, name) => call("RenameChannel", guildID, channelID, name),
  renameGuild: (guildID, name) => call("RenameGuild", guildID, name),
  leaveGuild: (guildID) => call("LeaveGuild", guildID),
  markRead: (channelID, atMs) => call("MarkRead", channelID, atMs),
  readState: () => call("ReadState"),
  appVersion: () => call("AppVersion"),
  // Saved messages (bookmarks) — device-local, never on any wire.
  bookmarkMessage: (id, channelId) => call("BookmarkMessage", id, channelId),
  unbookmarkMessage: (id) => call("UnbookmarkMessage", id),
  savedMessages: () => call("SavedMessages"),
  savedMessageIDs: () => call("SavedMessageIDs"),
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
  propsTally: (guildID) => call("PropsTally", guildID),
  networkStats: () => call("NetworkStats"),
  linkedDevices: () => call("LinkedDevices"),
  unlinkDevice: (deviceKey) => call("UnlinkDevice", deviceKey),
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
  // dir is the author's explicit base direction: "rtl", "ltr", or "" for the
  // per-line heuristic every message used before the composer could override
  // it. Bounded again on the Go side (domain.ValidDir), so this is a hint, not
  // a trust boundary.
  sendMessage: (channelID, content, replyTo = "", dir = "") =>
    call("SendMessage", channelID, content, replyTo, dir),
  sendCallNotice: (channelID, kind, content) =>
    call("SendCallNotice", channelID, kind, content),
  // Send-later queue, held by the Go service so it fires without this window.
  // fireAtUnix is unix SECONDS (the JS side otherwise speaks epoch ms).
  scheduleSend: (channelID, content, replyTo, fireAtUnix) =>
    call("ScheduleSend", channelID, content, replyTo, fireAtUnix),
  cancelScheduledSend: (id) => call("CancelScheduledSend", id),
  scheduledSends: () => call("ScheduledSends"),
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
  transferOwnership: (guildID, fingerprint) => call("TransferOwnership", guildID, fingerprint),
  setHeir: (guildID, fingerprint) => call("SetHeir", guildID, fingerprint),
  clearHeir: (guildID) => call("ClearHeir", guildID),
  claimOwnership: (guildID) => call("ClaimOwnership", guildID),
  banMember: (guildID, fingerprint) => call("BanMember", guildID, fingerprint),
  unbanMember: (guildID, fingerprint) => call("UnbanMember", guildID, fingerprint),
  muteMember: (guildID, fingerprint, minutes) => call("MuteMember", guildID, fingerprint, minutes),
  setSlowMode: (guildID, channelID, seconds) => call("SetSlowMode", guildID, channelID, seconds),
  // Retention: "" as channelID sets the guild-wide policy; 0 seconds = keep
  // everything. Enforced locally by each client (see ModalRetention).
  // Empty channelID exports the whole guild. Reads the store, so it is the
  // entire history rather than what the view has loaded.
  exportMarkdown: (guildID, channelID) => call("ExportMarkdown", guildID, channelID),
  exportArchive: (passphrase, withAttachments) => call("ExportArchive", passphrase, withAttachments),
  importArchive: (dataB64, passphrase) => call("ImportArchive", dataB64, passphrase),
  // A chronicle is a guild's bulk history archive: signed by the owner, carried
  // as a small index that every member holds, with the pages themselves fetched
  // from whoever has them only when somebody scrolls that far back.
  // chronicleMessages resolves to { messages, metered } — metered:true means the
  // page was not cached and the connection is billed by the byte, so nothing was
  // fetched; call again with allowMetered to override.
  attachChronicle: (guildID, manifestB64, chunksB64) => call("AttachChronicle", guildID, manifestB64, chunksB64),
  chronicleInfo: (guildID) => call("ChronicleInfo", guildID),
  chronicleMessages: (guildID, channelID, beforeNano, limit, allowMetered) =>
    call("ChronicleMessages", guildID, channelID, beforeNano, limit, allowMetered),
  setChroniclePinned: (guildID, pinned) => call("SetChroniclePinned", guildID, pinned),
  // Importing a chat export (a directory of per-channel JSON files) into a guild
  // as a chronicle. scanChatExport reads the directory ONCE and returns the size
  // report; estimateChatImport is pure arithmetic over that scan, so it is the
  // one to call on every slider drag. The policy object is
  // { fromNano, toNano, excludeChannels[], maxAttachmentBytes, includeImages,
  //   includeVideo, includeOther, includeReactions, includeEmoji, source,
  //   description } — an omitted field means false, so an empty policy imports
  // text only. importChatExport is owner-only, resolves to a JOB ID rather than
  // waiting (a real import runs for minutes), reports itself on the
  // "chronicle-import" event, and is read back with chronicleImportStatus("").
  // Nothing is ever fetched from the network: media the export did not bring
  // along is imported as a placeholder line naming the file and its size.
  scanChatExport: (dir) => call("ScanChatExport", dir),
  estimateChatImport: (dir, policy) => call("EstimateChatImport", dir, policy),
  importChatExport: (guildID, dir, policy) => call("ImportChatExport", guildID, dir, policy),
  chronicleImportStatus: (jobID) => call("ChronicleImportStatus", jobID),
  setRetention: (guildID, channelID, seconds) => call("SetRetention", guildID, channelID, seconds),
  guildRetention: (guildID) => call("GuildRetention", guildID),
  unmuteMember: (guildID, fingerprint) => call("UnmuteMember", guildID, fingerprint),
  bans: (guildID) => call("Bans", guildID),
  contacts: () => call("Contacts"),
  joinVoice: (channelID) => call("JoinVoice", channelID),
  leaveVoice: (channelID) => call("LeaveVoice", channelID),
  relaySignal: (toPeerID, data) => call("RelaySignal", toPeerID, data),
  callIceServers: () => call("CallIceServers"),
  revealDeleted: (channelID, messageID) => call("RevealDeleted", channelID, messageID),
  sendTyping: (channelID) => call("SendTyping", channelID),
  // birthday ("MM-DD" — never a year) rides at the END of the positional list.
  // Leave it undefined to keep the stored value: the bridge reads the short
  // arity as "don't touch", so callers patching other fields can't silently
  // wipe it (the games-wipe lesson). Pass "" to actually clear it.
  setProfile: (name, status, emoji, color, avatar, banner = "", presence = "", bio = "", color2 = "", frame = "", effect = "", style = "", birthday) =>
    birthday === undefined
      ? call("SetProfile", name, status, emoji, color, avatar, banner, presence, bio, color2, frame, effect, style)
      : call("SetProfile", name, status, emoji, color, avatar, banner, presence, bio, color2, frame, effect, style, birthday),
  // Moments: guild-scoped stories, text-on-a-banner scene (no video — see
  // internal/app/story.go). guildIds fans one signed record out to several
  // guilds; preset is EITHER a "preset:<id>" reference OR a whole raster image
  // data URI (png/jpeg/gif/webp, at most 250KB — story.go's scene gate); the
  // caption is at most 300 BYTES (the server counts bytes, not characters).
  // markStorySeen is strictly local — there are no view receipts, nothing
  // leaves this device. deleteStory retracts one of your OWN stories
  // everywhere it was posted.
  postStory: (guildIds, preset, caption) => call("PostStory", guildIds, preset, caption),
  guildStories: (guildID) => call("GuildStories", guildID),
  markStorySeen: (storyID) => call("MarkStorySeen", storyID),
  deleteStory: (storyID) => call("DeleteStory", storyID),
  verifyFingerprint: (fingerprint) => call("VerifyFingerprint", fingerprint),
  pinMessage: (channelID, messageID) => call("PinMessage", channelID, messageID),
  searchMessages: (query) => call("SearchMessages", query),
};

// Shared SSE connection for the browser transport. EventSource can't set
// headers, so the mobile token rides a query parameter instead.
let eventSource = null;
function sse() {
  if (!eventSource) {
    const qs = new URLSearchParams();
    if (apiToken) qs.set("token", apiToken);
    qs.set("client", clientID);
    eventSource = new EventSource(`${apiBase}/events?${qs}`);
    // An EventSource redials on its own after a dropped connection, and the
    // backend enters a reconnecting client into the visibility vote as
    // VISIBLE — the fail-safe direction, since being wrong there only costs a
    // node that stays eager slightly too long. Correct it straight away if
    // this page is in fact hidden; a visible page needs no call, because
    // visible is already what the backend assumed.
    eventSource.addEventListener("open", () => {
      reportTransport(true);
      if (typeof document !== "undefined" && document.hidden) {
        call("SetClientVisible", clientID, false).catch(() => {});
      }
    });
    // EventSource redials on its own schedule; `error` fires on every failed
    // attempt, so this is also the heartbeat that keeps the bar up while the
    // core is down, and `open` above is what takes it away again.
    eventSource.addEventListener("error", () => reportTransport(false));
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
