// Package mediatok reads the attachment tokens out of a message body: the one
// parser the store's media index and the core's gallery pages share.
//
// The grammar is the one internal/app/attach.go writes and
// frontend/src/lib/attachments.js reads (ATTACH_RE and FILE_RE). The patterns
// below are those two, character for character, so a body the feed draws as a
// picture is a picture here and a body it leaves as text is not an item.
// testdata/tokens.json holds the cases both this package and the node test
// (frontend/src/lib/mediatok.test.mjs) run.
package mediatok

import (
	"encoding/base64"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Kind is what an item is in the gallery.
type Kind int

const (
	Photo Kind = 1 // an image token
	Video Kind = 2 // a file token with a video/* type
	File  Kind = 3 // any other file token, voice messages included
)

// Tab is the gallery tab an item is listed under.
type Tab int

const (
	TabVisual Tab = 1 // photos and videos
	TabFiles  Tab = 2
)

// TabOf is the tab a kind belongs to.
func TabOf(k Kind) Tab {
	if k == File {
		return TabFiles
	}
	return TabVisual
}

// Flag bits of a v2 image token.
const (
	FlagSpoiler = 1
	// FlagCard marks a link card's thumbnail. It is a picture of a link, not a
	// photo anyone sent, so it is never a gallery item.
	FlagCard = 2
)

// Item is one attachment of one message body, in body order.
type Item struct {
	Ord     int    // position among the body's tokens, from 0
	Kind    Kind   // Photo, Video or File
	BlobID  string // hex sha256 of the ciphertext
	Keys    string // base64url key||nonce, as the token carries it
	Subtype string // images: png | jpeg | gif | webp
	W, H    int    // images: the token's dimensions (0 = unknown)
	Flags   int    // images: the v2 flag bits
	Name    string // decoded; may be empty
	Desc    string // images: the v2 description
	Size    int64  // files: plaintext bytes
	Mime    string // files: the decoded type
}

// The same alternatives as ATTACH_RE (v1 | v2) and FILE_RE in
// lib/attachments.js, joined so the tokens come out in body order.
var tokenRe = regexp.MustCompile(
	`!\[image\]\(concord://attach/v1/([0-9a-f]{64})/([A-Za-z0-9_-]{75})/(png|jpeg|gif|webp)/(\d{1,5})x(\d{1,5})\)` +
		`|!\[image\]\(concord://attach/v2/([0-9a-f]{64})/([A-Za-z0-9_-]{75})/(png|jpeg|gif|webp)/(\d{1,5})x(\d{1,5})/(\d{1,3})/([A-Za-z0-9_-]*)/([A-Za-z0-9_-]*)\)` +
		`|\[file\]\(concord://file/v1/([0-9a-f]{64})/([A-Za-z0-9_-]{75})/(\d{1,9})/([A-Za-z0-9_-]+)/([A-Za-z0-9_-]*)\)`)

// Parse returns a body's attachment items in body order. Link-card
// thumbnails are skipped but still take an ordinal, so an item's Ord is the
// same whichever reader counts.
func Parse(body string) []Item {
	if !strings.Contains(body, "concord://") {
		return nil
	}
	var out []Item
	for ord, m := range tokenRe.FindAllStringSubmatch(body, -1) {
		switch {
		case m[1] != "":
			out = append(out, Item{Ord: ord, Kind: Photo, BlobID: m[1], Keys: m[2], Subtype: m[3], W: atoi(m[4]), H: atoi(m[5])})
		case m[6] != "":
			flags := atoi(m[11])
			if flags&FlagCard != 0 {
				continue
			}
			out = append(out, Item{Ord: ord, Kind: Photo, BlobID: m[6], Keys: m[7], Subtype: m[8], W: atoi(m[9]), H: atoi(m[10]),
				Flags: flags, Name: displayName(b64(m[12])), Desc: b64(m[13])})
		default:
			mime := b64(m[17])
			k := File
			if strings.HasPrefix(mime, "video/") {
				k = Video
			}
			size, _ := strconv.ParseInt(m[16], 10, 64)
			name := displayName(b64(m[18]))
			if name == "" {
				name = "file"
			}
			out = append(out, Item{Ord: ord, Kind: k, BlobID: m[14], Keys: m[15], Size: size, Mime: mime, Name: name})
		}
	}
	return out
}

// At returns the item a body holds at ord, if it is still that kind.
func At(body string, ord int, kind Kind) (Item, bool) {
	for _, it := range Parse(body) {
		if it.Ord == ord {
			return it, it.Kind == kind
		}
	}
	return Item{}, false
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// b64 decodes base64url without padding, as the frontend's b64urlDecode does:
// anything that does not decode, or is not UTF-8, is "".
func b64(s string) string {
	if s == "" {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	if !utf8.Valid(b) {
		return ""
	}
	return string(b)
}

// bidiControls are the characters displayFileName in lib/attachments.js
// strips from a name: a right-to-left override draws "photo‮gnp.exe" as
// a picture's name.
var bidiControls = regexp.MustCompile("[؜‎‏‪-‮⁦-⁩]")

func displayName(s string) string {
	return bidiControls.ReplaceAllString(s, "")
}
