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
}

// A Channel is a named message stream within a guild. Each channel maps to one
// gossipsub topic (see TopicID).
type Channel struct {
	ID      string `json:"id"`
	GuildID string `json:"guildId"`
	Name    string `json:"name"`
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
	Sent      time.Time `json:"sent"`
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
