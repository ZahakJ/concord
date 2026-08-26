// Package domain is layer 3 of Concord: the pure logical model of guilds,
// channels, members and messages. It has no I/O and no dependency on the
// crypto, network, or storage layers — those layers persist and transport the
// types defined here. Keeping the model pure makes the rules (ID generation,
// topic derivation, message validation) trivially testable.
package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// A Guild is Concord's top-level space — what a centralized chat app would call
// a "server", except that there is no machine anywhere that holds it: a named
// collection of channels whose membership and message keys are governed by a
// single MLS group. GroupID is the MLS group identifier; every member of the guild is a
// member of that MLS group.
type Guild struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	GroupID  []byte    `json:"groupId"` // MLS group ID
	OwnerID  []byte    `json:"ownerId"` // owner's Ed25519 account public key
	Channels []Channel `json:"channels"`
	Created  time.Time `json:"created"`
	// Kind is "" for a normal guild or "dm" for a direct-message conversation
	// (rendered without server chrome). A self-DM ("Notes") is a dm with one
	// member — yourself.
	Kind string `json:"kind,omitempty"`
	// Personalization (all optional). Icon/Banner are small data-URI images
	// (an animated GIF banner just works — it's an <img>). Description is a short
	// blurb. Propagated over the guild-meta topic like the name.
	Icon        string `json:"icon,omitempty"`
	Banner      string `json:"banner,omitempty"`
	Description string `json:"description,omitempty"`
}

// A Channel is a named message stream within a guild. Each channel maps to one
// gossipsub topic (see TopicID). Type/Category/Position are advisory layout
// metadata (well-behaved-client trust, like pins), propagated over the guild-
// meta topic; they will fold into signed guild-state when roles land.
type Channel struct {
	ID       string `json:"id"`
	GuildID  string `json:"guildId"`
	Name     string `json:"name"`
	Type     string `json:"type,omitempty"`     // "" or "text" | "voice" | "announcement" | "forum" | "thread"
	Category string `json:"category,omitempty"` // category ID this channel sits under, or ""
	Position int    `json:"position,omitempty"` // sort order within its category
	Topic    string `json:"topic,omitempty"`    // channel topic/description (advisory)
	// Parent is the forum channel a thread (post) lives under, "" otherwise.
	// Threads are ordinary channels — same topics, encryption, unread logic —
	// that the UI nests under their forum instead of the sidebar.
	Parent string `json:"parent,omitempty"`
	// Links are the consumer channels an ANNOUNCEMENT channel publishes to
	// (channel IDs in the same guild). Advisory metadata, like Category.
	Links []string `json:"links,omitempty"`

	// --- Forum metadata. Every field below is optional and ignorable: a peer
	// that predates them still reads a forum as a forum and a post as an
	// ordinary thread, because none of them changes what a channel IS. They sit
	// at the same trust level as Category/Position — advisory layout a
	// well-behaved client honours — and only ever appear on forum or thread
	// channels, so no other channel pays a byte for them.

	// ForumTags is the tag palette a FORUM channel offers its posts (Bug, Idea,
	// Solved…). Set only on a channel of type "forum".
	ForumTags []ForumTag `json:"forumTags,omitempty"`
	// Tags are the ForumTag IDs a POST carries, referencing its forum's palette.
	// An ID with no matching palette entry renders as nothing — that is what
	// makes deleting a tag cheap (see SetForumTags: no post is rewritten).
	Tags []string `json:"tags,omitempty"`
	// Pinned floats a POST to the top of its forum board. Moderation, not
	// authorship: gated on Manage Messages, the same bit that pins a message.
	Pinned bool `json:"pinned,omitempty"`
	// Solved marks a POST answered. Distinct from a tag on purpose — the post's
	// own author may close their question without being handed a moderator
	// permission, and a board can only offer "show unanswered" if the signal
	// exists on every forum rather than only the ones that defined a tag for it.
	Solved bool `json:"solved,omitempty"`
	// Locked closes a POST to new messages. Advisory in the same sense as every
	// other field here, and worth being exact about what that buys with no
	// server: a locked post is enforced by every honest client — they refuse to
	// send into it and DROP anything that arrives for it — so a modified client
	// can still publish to the topic and simply be ignored by everyone else.
	// That is a real quorum, not a lock, and calling it a lock in the UI would
	// promise more than the network can.
	Locked bool `json:"locked,omitempty"`
	// Banner is a FORUM's own artwork: either a small data-URI image or
	// "preset:<id>" naming an entry in the client's banner library, exactly as a
	// guild banner does.
	//
	// Shared, unlike the board's layout and sort order, which stay in local
	// storage. That split is the point: how YOU like the posts arranged is your
	// business, but a banner is part of what the forum IS, so every member has
	// to see the same one or it is decoration rather than identity.
	Banner string `json:"banner,omitempty"`
}

// A ForumTag is one entry in a forum's tag palette: a short label with a colour
// the client renders a chip in. Colour is a strict "#rrggbb" — it is
// interpolated into a CSS context, so the format is validated rather than
// trusted, on the local path and on every path a peer's channel record arrives
// by (see app.sanitizeForumMeta).
type ForumTag struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`           // "#rrggbb", lowercase
	Emoji string `json:"emoji,omitempty"` // optional leading glyph for the chip
}

// ChannelType returns a channel's type, defaulting to "text".
func (c Channel) ChannelType() string {
	if c.Type == "" {
		return "text"
	}
	return c.Type
}

// A Category groups channels in the sidebar. Pure layout metadata.
type Category struct {
	ID       string `json:"id"`
	GuildID  string `json:"guildId"`
	Name     string `json:"name"`
	Position int    `json:"position"`
}

// A CustomEmoji is a guild-scoped emoji, referenced in text as :name: and
// stored as a small image data URI. Distributed to members like other guild
// metadata.
type CustomEmoji struct {
	GuildID string `json:"guildId,omitempty"`
	Name    string `json:"name"`
	Image   string `json:"image"` // data:image/...;base64,...
}

// A Message is a single chat message. Content is the human-readable body; it is
// what gets MLS-encrypted before transport and encrypted again at rest. Sender
// is the author's Ed25519 account public key, authenticated by MLS on receipt.
type Message struct {
	ID        string `json:"id"`
	ChannelID string `json:"channelId"`
	Sender    []byte `json:"sender"`
	Name      string `json:"name"`    // sender's self-asserted display name (decorative)
	Kind      string `json:"kind"`    // "" = normal chat, "system" notice, "delete" action
	ReplyTo   string `json:"replyTo"` // ID of the message this replies to / acts on
	Content   string `json:"content"`
	// Dir is the base direction the author laid this message out in: "rtl",
	// "ltr", or "" for the per-line heuristic every message used before this
	// field existed (and which is still the right answer almost always).
	//
	// It exists because that heuristic reads the FIRST strong character of a
	// line and there is no way to argue with it. Start a line in English and
	// its base direction is left-to-right for good, so an Arabic phrase added
	// afterwards lands to the right of the English and cannot be moved without
	// deleting back to the start and retyping it the other way round. Setting
	// the base direction explicitly is the only fix, and it has to travel with
	// the message: laid out one way as it was written and the other way as it
	// is read is not a message anyone wrote.
	//
	// Optional on the wire in both directions. A peer that predates the field
	// drops it on decode and renders by the heuristic, which is exactly what
	// it does today — so this degrades to the current behaviour rather than to
	// a broken one. Bounded to two values on receive (ValidDir): it reaches a
	// dir attribute in every client's DOM, and an unknown value there is a
	// string a stranger chose appearing in markup.
	Dir     string    `json:"dir,omitempty"`
	Deleted bool      `json:"deleted"`
	Expired bool      `json:"expired"` // erased by a disappearing-message timer (not a normal delete)
	Edited  bool      `json:"edited"`
	Pinned  bool      `json:"pinned"`
	Sent    time.Time `json:"sent"`

	// Updated is when the message's state (edit/delete/pin/reactions) last
	// changed, zero if never. Carried by history sync so receivers can prefer
	// the newer of two diverged states; not set on live messages.
	Updated time.Time `json:"updated"`

	// Reactions aggregates emoji -> fingerprints who reacted. Populated on load;
	// never sent over the wire (reactions travel as their own "reaction" action).
	Reactions map[string][]string `json:"reactions,omitempty"`
}

// A Contact is a peer this node has encountered, tracked for trust-on-first-use
// verification. Verified is set once a human has confirmed the Fingerprint
// out-of-band (a "safety number" check), which is what defeats a man-in-the-
// middle substituting their own key.
type Contact struct {
	PeerID      string    `json:"peerId"`
	Fingerprint string    `json:"fingerprint"`
	Verified    bool      `json:"verified"`
	FirstSeen   time.Time `json:"firstSeen"`
}

// An Event is one entry in a guild's shared calendar: a title, a time, a
// place. Shared state like a channel — created by a member, propagated
// MLS-encrypted over the guild-meta lane, gated on receive exactly as
// locally, and converged to fresh joiners through the history-sync snapshot.
// Recurrence is deliberately absent in v1: one event is one record, and the
// ICS export states that explicitly rather than leaving importers to guess
// whether a missing RRULE was intent or a bug.
type Event struct {
	ID      string `json:"id"`
	GuildID string `json:"guildId"`
	Title   string `json:"title"`
	Details string `json:"details,omitempty"`
	// StartUnix/EndUnix are UTC Unix seconds. EndUnix zero means "no stated
	// end": the ICS exporter then omits DTEND, which RFC 5545 defines as a
	// point-in-time event, not a day-long block.
	StartUnix int64 `json:"startUnix"`
	EndUnix   int64 `json:"endUnix,omitempty"`
	// Location is free text — a room, an address, or the name of a channel in
	// this guild. Never a vendor link; nothing here is fetched.
	Location string `json:"location,omitempty"`
	// LocationChannelID makes the location a REAL channel of the same guild:
	// Join then navigates there (and into its call when it is a voice channel)
	// instead of minting a disposable meeting guild, and the start announcement
	// lands in that channel's own chat. Kept alongside Location rather than
	// replacing it: Location doubles as the display label ("🔊 lounge") for ICS
	// export and for members whose copy of the channel list hasn't caught up.
	// It rides the same event_upserted lane and so inherits the exact same
	// receive-side ownership as every other event field (author/ManageMessages,
	// bound to the MLS-authenticated actor). Consumers must resolve it against
	// the guild the event belongs to — a record naming a foreign guild's
	// channel is ignored at the point of use, not trusted at face value.
	LocationChannelID string `json:"locationChannelId,omitempty"`
	// CreatedBy is the author's account fingerprint — the same identity string
	// every permission check runs on. On receive it is bound to the
	// MLS-authenticated sender, never adopted from the payload.
	CreatedBy string `json:"createdBy"`
	CreatedAt int64  `json:"createdAt"` // Unix seconds
	// UpdatedAt orders competing copies of the same event during history sync
	// (newest wins). Bumped on every edit and every RSVP change.
	UpdatedAt int64 `json:"updatedAt,omitempty"`
	// RSVPs maps a member fingerprint to going|maybe|no. One entry per
	// account, set only through its own event_rsvp lane so nobody can answer
	// on anyone else's behalf.
	RSVPs map[string]string `json:"rsvps,omitempty"`
	// GuestURL/GuestHost open the event to outsiders: a shareable browser-guest
	// link into a disposable meeting room (eventguest.go), and the account
	// fingerprint of whoever minted it — the node that room physically lives
	// on. They travel with the event so every member can copy the link, but on
	// receive only GuestHost's own frames may set, change or clear them: the
	// room, its tokens and its door policy exist solely on that host's node,
	// so nobody else can revoke a link there or point guests somewhere else.
	GuestURL  string `json:"guestUrl,omitempty"`
	GuestHost string `json:"guestHost,omitempty"`
	// MemberCode is the members' door into the same room: a guild invite code
	// for the meeting guild, minted by GuestHost's node alongside GuestURL.
	// Redeeming it makes the member a REAL member of the room — own identity,
	// full E2EE — with no knock: insiders walk straight in, and only outsiders
	// wait in the lobby. It travels only on this MLS-encrypted record, so
	// outsiders never see it, and it obeys the same receive-side rule as
	// GuestURL: only GuestHost's frames may touch it.
	MemberCode string `json:"memberCode,omitempty"`
}

// NewID returns a random 128-bit hex identifier for guilds, channels and
// messages. Random (not sequential) IDs avoid leaking counts or ordering.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// NewGuild constructs a guild owned by ownerKey with a default #general channel.
func NewGuild(name string, groupID, ownerKey []byte) Guild {
	id := NewID()
	return Guild{
		ID:      id,
		Name:    name,
		GroupID: groupID,
		OwnerID: ownerKey,
		Channels: []Channel{
			{ID: NewID(), GuildID: id, Name: "general"},
		},
		Created: time.Now().UTC(),
	}
}

// NewMeetingGuild constructs a disposable meeting room: Kind "meeting", with a
// single seed channel that is a real VOICE channel named "meeting". A meeting
// exists to be walked into as a call, so the room must READ as one the moment
// it appears — the speaker glyph, the sidebar voice roster and the reachable
// guest-eviction control all gate on Type == "voice", and the historically
// typeless seed channel rendered as a text channel that merely allowed a call.
// Voice channels carry their own chat, so seed system messages and meeting
// text keep working. Rooms minted before this helper stay typeless in old DBs
// and keep working through the Kind == "meeting" special-cases (voice-watch,
// the Call button, relay-forced calls); only NEW rooms are voice-first.
func NewMeetingGuild(name string, groupID, ownerKey []byte) Guild {
	g := NewGuild(name, groupID, ownerKey)
	g.Kind = "meeting"
	g.Channels[0].Name = "meeting"
	g.Channels[0].Type = "voice"
	return g
}

// NewMessage constructs a message with a fresh ID and the current timestamp.
func NewMessage(channelID string, sender []byte, content string) (Message, error) {
	if channelID == "" {
		return Message{}, errors.New("domain: message needs a channel")
	}
	if content == "" {
		return Message{}, errors.New("domain: message content is empty")
	}
	return Message{
		ID:        NewID(),
		ChannelID: channelID,
		Sender:    sender,
		Content:   content,
		Sent:      time.Now().UTC(),
	}, nil
}

// ValidDir bounds a message's base direction to the two values that mean
// anything, mapping everything else — including the empty string — to "".
//
// It fails CLOSED, and it has to: Dir arrives inside a decrypted message from
// another member and ends up as a `dir` attribute in the DOM of every client
// that renders the channel. "" is not a degraded result there, it is the
// behaviour every message had before the field existed, so a peer sending
// nonsense gets the per-line heuristic rather than an argument.
func ValidDir(d string) string {
	if d == "rtl" || d == "ltr" {
		return d
	}
	return ""
}

// topicPrefix namespaces all Concord gossipsub topics.
const topicPrefix = "concord/c/"

// TopicID returns the gossipsub topic name for a channel. It is a hash of the
// guild's group ID and the channel ID, so the topic string reveals neither the
// guild name nor the raw group ID to the pubsub network, yet every member
// derives the same value deterministically.
func TopicID(groupID []byte, channelID string) string {
	h := sha256.New()
	h.Write(groupID)
	h.Write([]byte{0}) // domain separator
	h.Write([]byte(channelID))
	return topicPrefix + hex.EncodeToString(h.Sum(nil)[:16])
}

// ControlTopicID returns the gossipsub topic carrying a guild's MLS group
// control messages (commits). One control topic per guild.
func ControlTopicID(groupID []byte) string {
	h := sha256.New()
	h.Write(groupID)
	h.Write([]byte("/control"))
	return topicPrefix + hex.EncodeToString(h.Sum(nil)[:16])
}

// GuildMetaTopicID returns the gossipsub topic carrying a guild's
// (MLS-encrypted) metadata updates, such as newly created channels. One per
// guild.
func GuildMetaTopicID(groupID []byte) string {
	h := sha256.New()
	h.Write(groupID)
	h.Write([]byte("/meta"))
	return topicPrefix + hex.EncodeToString(h.Sum(nil)[:16])
}

// TypingTopicID returns the gossipsub topic carrying ephemeral "is typing"
// signals for a channel. These are transient presence hints, not stored.
func TypingTopicID(groupID []byte, channelID string) string {
	h := sha256.New()
	h.Write(groupID)
	h.Write([]byte("/typing/"))
	h.Write([]byte(channelID))
	return topicPrefix + hex.EncodeToString(h.Sum(nil)[:16])
}

// VoiceTopicID returns the gossipsub topic on which peers announce joining and
// leaving a channel's voice room. Membership announcements let peers discover
// who to open a WebRTC media connection with.
func VoiceTopicID(groupID []byte, channelID string) string {
	h := sha256.New()
	h.Write(groupID)
	h.Write([]byte("/voice/"))
	h.Write([]byte(channelID))
	return topicPrefix + hex.EncodeToString(h.Sum(nil)[:16])
}

// Clone returns a deep copy of a channel: the slice fields get their own
// backing arrays rather than aliasing the original's.
func (c Channel) Clone() Channel {
	out := c
	out.Links = append([]string(nil), c.Links...)
	out.Tags = append([]string(nil), c.Tags...)
	out.ForumTags = append([]ForumTag(nil), c.ForumTags...)
	return out
}

// Clone returns a deep copy of a guild.
//
// A plain `*g` copies the struct, which copies the slice HEADERS and leaves
// every element shared with the original. Code that took a copy under a read
// lock and then released it was therefore still reading memory that a writer
// holding the lock could mutate: renaming a channel while a peer's history
// sync walked the same guild was a live data race, caught by the race
// detector once CI started running it. Copy guilds with this, not with `*g`.
func (g Guild) Clone() Guild {
	out := g
	out.GroupID = append([]byte(nil), g.GroupID...)
	out.OwnerID = append([]byte(nil), g.OwnerID...)
	if g.Channels != nil {
		out.Channels = make([]Channel, len(g.Channels))
		for i, c := range g.Channels {
			out.Channels[i] = c.Clone()
		}
	}
	return out
}
