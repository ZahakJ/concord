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
	"strings"
	"time"
)

// A Guild is Concord's equivalent of a Discord "server": a named collection of
// channels whose membership and message keys are governed by a single MLS
// group. GroupID is the MLS group identifier; every member of the guild is a
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
	ID        string    `json:"id"`
	ChannelID string    `json:"channelId"`
	Sender    []byte    `json:"sender"`
	Name      string    `json:"name"`    // sender's self-asserted display name (decorative)
	Kind      string    `json:"kind"`    // "" = normal chat, "system" notice, "delete" action
	ReplyTo   string    `json:"replyTo"` // ID of the message this replies to / acts on
	Content   string    `json:"content"`
	Deleted   bool      `json:"deleted"`
	Expired   bool      `json:"expired"` // erased by a disappearing-message timer (not a normal delete)
	Edited    bool      `json:"edited"`
	Pinned    bool      `json:"pinned"`
	Sent      time.Time `json:"sent"`

	// Updated is when the message's state (edit/delete/pin/reactions) last
	// changed, zero if never. Carried by history sync so receivers can prefer
	// the newer of two diverged states; not set on live messages.
	Updated time.Time `json:"updated"`

	// Reactions aggregates emoji -> fingerprints who reacted. Populated on load;
	// never sent over the wire (reactions travel as their own "reaction" action).
	Reactions map[string][]string `json:"reactions,omitempty"`

	// OCRMatch marks a search hit that matched (only) through text extracted
	// locally from an image attachment — "matched text in image". Transient:
	// set by search, never persisted, never sent over the wire.
	OCRMatch bool `json:"ocrMatch,omitempty"`
}

// Message kinds. The empty string is a normal human chat message; every other
// kind is something a client renders differently or not at all.
const (
	// KindChat is an ordinary message typed by a person. Deliberately "" so
	// that an absent field on the wire means chat and nothing existing changes
	// meaning.
	KindChat = ""
	// KindApp is machine-to-machine payload traffic: one local app (sentinel,
	// trove, …) talking to another over Concord's E2EE transport.
	//
	// This is the DATA PLANE, and it is separate from the chat plane on
	// purpose. App payloads ride the same encrypted transport, are stored the
	// same way, and are readable by the same members — but they are not
	// conversation, so no client renders them in a channel, they never mark a
	// channel unread, and they never ping anybody. They surface only in a
	// dedicated apps/integrations view.
	KindApp = "app"
)

// AppBusPrefix marks the legacy, pre-KindApp form of an app payload: an
// ordinary chat message whose first line is "APPBUS:<app>:<schema-version>",
// JSON body after.
//
// We still honor it, and must keep honoring it. App-bus producers live in
// other repos on other release cadences (and on other machines), so requiring
// a lockstep upgrade to KindApp would mean either breaking them or leaving
// their traffic in the human channel until every one of them shipped. Treating
// the prefix as app-kind means older producers keep working untouched, and it
// retroactively hides payloads already sitting in people's channels.
const AppBusPrefix = "APPBUS:"

// IsApp reports whether this message is app-plane traffic rather than
// conversation — by its Kind, or by the legacy content prefix.
//
// Every renderer, counter, notifier and search path should ask this rather
// than comparing Kind directly, so the legacy form is handled in exactly one
// place.
func (m Message) IsApp() bool {
	return m.Kind == KindApp || strings.HasPrefix(m.Content, AppBusPrefix)
}

// EffectiveKind is Kind with legacy app-bus payloads reported as KindApp, so
// wire output and client filtering agree on what a message actually is.
func (m Message) EffectiveKind() string {
	if m.IsApp() {
		return KindApp
	}
	return m.Kind
}

// AppBusApp returns the producing app name from an "APPBUS:<app>:<ver>"
// payload, or "" when the message doesn't carry one.
func (m Message) AppBusApp() string {
	if !strings.HasPrefix(m.Content, AppBusPrefix) {
		return ""
	}
	rest := m.Content[len(AppBusPrefix):]
	if i := strings.IndexAny(rest, ":\n"); i >= 0 {
		return rest[:i]
	}
	return rest
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
