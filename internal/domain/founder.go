package domain

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
)

// A guild created by 0.61 or later has an MLS group id that names its founder:
//
//	GroupID = 0x01 || r || SHA-256("concord founder v1" || 0x00 || guildID || 0x00 || founder || r)[:16]
//
// where founder is the founder's account public key and r is 16 random bytes
// chosen at creation. A device recomputes the hash from the r inside the id,
// so the id alone, with the guild id, says which key founded the group. Guilds
// created earlier have 16-byte random ids and name nobody.
const (
	founderIDVersion = 0x01
	founderSaltLen   = 16
	founderHashLen   = 16
	// FounderGroupIDLen is the length of a founder-scheme group id.
	FounderGroupIDLen = 1 + founderSaltLen + founderHashLen
	// LegacyGroupIDLen is the length of a group id from before the scheme.
	LegacyGroupIDLen = 16
)

var founderTag = []byte("concord founder v1")

func founderHash(guildID string, founder, salt []byte) []byte {
	h := sha256.New()
	h.Write(founderTag)
	h.Write([]byte{0})
	h.Write([]byte(guildID))
	h.Write([]byte{0})
	h.Write(founder)
	h.Write(salt)
	return h.Sum(nil)[:founderHashLen]
}

// FounderGroupIDWithSalt builds a founder-scheme group id from a given salt.
// salt must be 16 bytes. NewFounderGroupID is what creation uses; this exists
// for it and for fixed test vectors.
func FounderGroupIDWithSalt(guildID string, founder, salt []byte) []byte {
	if len(salt) != founderSaltLen {
		return nil
	}
	out := make([]byte, 0, FounderGroupIDLen)
	out = append(out, founderIDVersion)
	out = append(out, salt...)
	return append(out, founderHash(guildID, founder, salt)...)
}

// NewFounderGroupID builds a founder-scheme group id with a fresh random salt.
func NewFounderGroupID(guildID string, founder []byte) ([]byte, error) {
	salt := make([]byte, founderSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	return FounderGroupIDWithSalt(guildID, founder, salt), nil
}

// IsFounderGroupID reports whether a group id has the founder scheme's shape.
// It says nothing about which founder; see FounderMatches.
func IsFounderGroupID(gid []byte) bool {
	return len(gid) == FounderGroupIDLen && gid[0] == founderIDVersion
}

// FounderMatches reports whether gid is a founder-scheme id for this guild id
// and founder key.
func FounderMatches(gid []byte, guildID string, founder []byte) bool {
	if !IsFounderGroupID(gid) || len(founder) == 0 {
		return false
	}
	salt := gid[1 : 1+founderSaltLen]
	return bytes.Equal(gid[1+founderSaltLen:], founderHash(guildID, founder, salt))
}

// NewGuildWithID is NewGuild with a guild id the caller chose (the founder
// scheme needs the guild id before the group exists).
func NewGuildWithID(id, name string, groupID, ownerKey []byte) Guild {
	g := NewGuild(name, groupID, ownerKey)
	g.ID = id
	for i := range g.Channels {
		g.Channels[i].GuildID = id
	}
	return g
}

// NewMeetingGuildWithID is NewMeetingGuild with a guild id the caller chose.
func NewMeetingGuildWithID(id, name string, groupID, ownerKey []byte) Guild {
	g := NewMeetingGuild(name, groupID, ownerKey)
	g.ID = id
	for i := range g.Channels {
		g.Channels[i].GuildID = id
	}
	return g
}
