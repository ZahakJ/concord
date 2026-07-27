package app

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/zahak/concord/internal/compactcode"
)

// Compact wire format for guild invite codes ("Concord Invite v1"). The
// original codes were base64url(JSON) with string multiaddrs — several
// hundred characters for something people paste into chat. Here the guild ID
// travels as raw bytes (it's a hex string), the owner peer ID as its
// multihash, multiaddrs in binary, and relay circuit addresses are elided
// (re-derived from the carried bootstrap list on decode). Legacy JSON codes
// keep decoding; old clients can't read new codes, which is acceptable —
// invites are cheap to re-issue.
const invitePrefix = "CI1"

// Guild-ID entry flags: raw hex bytes when the ID is lowercase hex (always,
// today — MLS group IDs), UTF-8 fallback otherwise.
const (
	gidRaw = 0x00
	gidHex = 0x01
)

func encodeInviteCode(ic inviteCode) string {
	// Elide against the bootstrap list as encoded, not as given: an entry the
	// cap drops can't be an index the decoder resolves.
	boot := compactcode.DedupeCap(ic.Bootstrap, compactcode.MaxAddrs)
	kept, circuits := compactcode.ElideCircuits(ic.OwnerAddr, boot)
	addrs := compactcode.DedupeCap(compactcode.RankAddrs(kept), compactcode.MaxAddrs)
	b := make([]byte, 0, 256)
	if raw, err := hex.DecodeString(ic.GuildID); err == nil && ic.GuildID == hex.EncodeToString(raw) {
		b = append(b, gidHex)
		b = compactcode.AppendBytes(b, raw)
	} else {
		b = append(b, gidRaw)
		b = compactcode.AppendString(b, ic.GuildID)
	}
	b = compactcode.AppendString(b, ic.GuildName)
	b = compactcode.AppendPeerID(b, ic.OwnerID)
	b = compactcode.AppendAddrs(b, addrs)
	b = compactcode.AppendAddrs(b, boot)
	b = binary.AppendUvarint(b, circuits)
	return invitePrefix + base64.RawURLEncoding.EncodeToString(b)
}

func decodeInviteCode(code string) (inviteCode, error) {
	if rest, ok := strings.CutPrefix(code, invitePrefix); ok {
		return decodeInviteV1(rest)
	}
	// Legacy: base64url(JSON).
	raw, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return inviteCode{}, errors.New("app: bad invite code")
	}
	var ic inviteCode
	if err := json.Unmarshal(raw, &ic); err != nil {
		return inviteCode{}, errors.New("app: bad invite code")
	}
	return ic, nil
}

func decodeInviteV1(s string) (inviteCode, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return inviteCode{}, errors.New("app: bad invite code")
	}
	r := compactcode.NewReader(raw)
	var ic inviteCode
	gidFlag := r.Byte()
	gid := r.Bytes()
	if gidFlag == gidHex {
		ic.GuildID = hex.EncodeToString(gid)
	} else {
		ic.GuildID = string(gid)
	}
	ic.GuildName = r.String()
	ic.OwnerID = r.PeerID()
	ic.OwnerAddr = r.Addrs()
	ic.Bootstrap = r.Addrs()
	// The circuit mask was appended after the first CI1 codes shipped; those
	// end here and meant "a circuit for every rendezvous".
	circuits := compactcode.AllCircuits
	if r.More() {
		circuits = r.Uvarint()
	}
	if r.Err() != nil || ic.GuildID == "" || ic.OwnerID == "" {
		return inviteCode{}, errors.New("app: bad invite code")
	}
	ic.OwnerAddr = compactcode.RestoreCircuits(ic.OwnerAddr, ic.Bootstrap, circuits)
	return ic, nil
}
