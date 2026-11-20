package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/ZahakJ/concord/internal/bmffmeta"
	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/imgmeta"
	cnet "github.com/ZahakJ/concord/internal/net"
	"github.com/ZahakJ/concord/internal/store"
)

// Encrypted attachments: an image is sealed once with a random
// secretbox key, stored locally as an opaque, content-addressed blob, and the
// message carries only a ~190-char reference token — the key rides INSIDE the
// MLS-encrypted message content. Blobs travel over a dedicated stream protocol
// with its own large frame cap, so gossipsub and history sync never see image
// bytes. Any member holding a blob can serve it to the other members of the
// guild or DM it belongs to, which is what keeps images fetchable after the
// sender goes offline: every member who viewed the image becomes a source.
// A peer that shares no such guild or DM gets the answer for a blob nobody
// holds (handleAttachRequest).
//
// Token format, embedded in message content in place of inline base64:
//
//	![image](concord://attach/v1/<blobID>/<keys>/<subtype>/<w>x<h>)
//	![image](concord://attach/v2/<blobID>/<keys>/<subtype>/<w>x<h>/<flags>/<nameB64>/<descB64>)
//
// v2 carries what the composer's per-image controls set: a flag bitmask (1 =
// spoiler, so it arrives blurred) and an optional filename and description.
// v1 is still emitted whenever none of those are used, which is the ordinary
// case — a peer on an older build then keeps rendering every plain image
// exactly as before, and only sees a raw token for one that uses the new
// controls.
//
// blobID = hex sha256(ciphertext); keys = base64url (no padding) of
// key(32) || nonce(24); subtype ∈ png|jpeg|gif|webp; w/h reserve layout space
// (0x0 = unknown). Every character survives the frontend's escape-first
// markdown pipeline untouched. The frontend mirrors this format in
// lib/attachments.js.

// maxAttachmentPlain caps a decoded inline image; maxFilePlain caps a generic
// file attachment (larger, since files aren't rendered inline).
const maxAttachmentPlain = 5 << 20 // 5 MiB
const maxFilePlain = 25 << 20      // 25 MiB
const maxFilenameLen = 200

const attachKeysLen = 32 + 24 // secretbox key + nonce

var (
	blobIDRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// The blob id inside a token, for callers that hold a message body and want
	// to know which blob it points at. Both token versions put the id in the
	// same position, so one pattern covers v1 and v2.
	attachIDRe = regexp.MustCompile(`concord://attach/v[12]/([0-9a-f]{64})/`)

	dataURLRe   = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp);base64,([A-Za-z0-9+/=]+)$`)
	fileURLRe   = regexp.MustCompile(`^data:([a-zA-Z0-9!#$&^_.+-]+/[a-zA-Z0-9!#$&^_.+-]+);base64,([A-Za-z0-9+/=]+)$`)
	mimeRe      = regexp.MustCompile(`^[a-zA-Z0-9!#$&^_.+-]+/[a-zA-Z0-9!#$&^_.+-]+$`)
	b64url      = base64.RawURLEncoding
	errNotFound = fmt.Errorf("app: attachment not found on any reachable peer")
)

type attachRequest struct {
	BlobID string `json:"blobId"`
}

// AttachBlobID returns the blob id of the first image attachment token in a
// message body, or "" when there is none. The UI needs it to key per-image
// local state (the meme editor's render recipe) to the picture that went out,
// and the id is only ever minted here, inside sealBlob.
func AttachBlobID(content string) string {
	m := attachIDRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return m[1]
}

// sealBlob seals plaintext under a fresh random key, stores the content-
// addressed ciphertext locally, and returns the blob ID and the base64url
// key||nonce string that a token carries (the key that lets a member decrypt).
func (s *Service) sealBlob(plain []byte) (blobID, keys string, err error) {
	var key [32]byte
	var nonce [24]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", "", err
	}
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", "", err
	}
	ct := secretbox.Seal(nil, plain, &nonce, &key)
	sum := sha256.Sum256(ct)
	blobID = hex.EncodeToString(sum[:])
	if err := s.store.SaveAttachment(blobID, ct); err != nil {
		return "", "", err
	}
	return blobID, b64url.EncodeToString(append(key[:], nonce[:]...)), nil
}

// attachSpoiler is bit 0 of a v2 token's flag field.
const attachSpoiler = 1

// maxAttachDescLen caps an image description. It rides in the message body, so
// an unbounded one would be a cheap way to bloat every recipient's history.
const maxAttachDescLen = 500

// sealAttachment decodes an image data URL, seals it into a local blob, and
// returns the blob id and the reference token a message body carries.
//
// Split out of SendAttachment because posting is not the only thing that needs
// a token: EditAttachment re-points an EXISTING message at a freshly sealed
// blob, and doing that by sending and then deleting would leave a tombstone
// where the original picture used to be.
func (s *Service) sealAttachment(dataURL string, w, h int, spoiler bool, name, desc string) (blobID, token string, err error) {
	m := dataURLRe.FindStringSubmatch(dataURL)
	if m == nil {
		return "", "", fmt.Errorf("app: attachment must be a png/jpeg/gif/webp image data URL")
	}
	subtype := m[1]
	plain, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		return "", "", fmt.Errorf("app: bad attachment encoding: %w", err)
	}
	if len(plain) == 0 || len(plain) > maxAttachmentPlain {
		return "", "", fmt.Errorf("app: attachment must be 1 byte – %d MB", maxAttachmentPlain>>20)
	}
	if plain, err = cleanImage(plain); err != nil {
		return "", "", err
	}

	blobID, keys, err := s.sealBlob(plain)
	if err != nil {
		return "", "", err
	}
	if w < 0 || w > 99999 {
		w = 0
	}
	if h < 0 || h > 99999 {
		h = 0
	}
	name = clampBytes(name, maxFilenameLen)
	desc = clampBytes(desc, maxAttachDescLen)
	if !spoiler && name == "" && desc == "" {
		return blobID, fmt.Sprintf("![image](concord://attach/v1/%s/%s/%s/%dx%d)", blobID, keys, subtype, w, h), nil
	}
	flags := 0
	if spoiler {
		flags |= attachSpoiler
	}
	return blobID, fmt.Sprintf("![image](concord://attach/v2/%s/%s/%s/%dx%d/%d/%s/%s)",
		blobID, keys, subtype, w, h, flags,
		b64url.EncodeToString([]byte(name)), b64url.EncodeToString([]byte(desc))), nil
}

// SendAttachment seals an image (a data URL from the UI) into a local blob
// and sends the reference token as a normal chat message. spoiler/name/desc
// come from the per-image controls in the composer; when all three are unset
// the token emitted is the original v1 form.
//
// text is optional: a caption, and the stamps the composer puts on any message
// (the disappearing timer above all), sealed in the same message as the
// picture so the two travel, render and expire as one. Builds that predate it
// strip the token and show the rest, which is the caption.
func (s *Service) SendAttachment(channelID, dataURL string, w, h int, replyTo string, spoiler bool, name, desc, text, dir string) (domain.Message, error) {
	_, token, err := s.sealAttachment(dataURL, w, h, spoiler, name, desc)
	if err != nil {
		return domain.Message{}, err
	}
	return s.sendAs(channelID, withText(token, text), "", replyTo, "", dir)
}

// withText puts a message's text after its attachment token.
func withText(token, text string) string {
	if text == "" {
		return token
	}
	return token + "\n" + text
}

// imageTokenRe is a whole image token, as sealAttachment writes it.
var imageTokenRe = regexp.MustCompile(`!\[image\]\(concord://attach/v[12]/[^)\s]*\)`)

// EditAttachment replaces the picture in one of this peer's own image messages,
// in place. The new image becomes its own blob and the ORIGINAL message is
// edited to reference it, so re-rendering an image (the meme editor's "edit a
// sent meme") leaves exactly one message in the channel rather than a second
// one plus a "deleted" tombstone.
//
// The returned blob id is what the UI keys its local render recipe by, and it
// changes on every edit: the blob is content-addressed, so a re-render is by
// definition a different blob.
//
// applyEdit only accepts an edit signed by the message's own author, and it
// does not care that the new content is a token rather than prose — the body
// is opaque to it. The old blob is deliberately left in the local store: peers
// that never fetched the edit still reference it, and the store is swept by the
// same trash pass as everything else.
func (s *Service) EditAttachment(channelID, targetID, dataURL string, w, h int) (string, error) {
	if targetID == "" {
		return "", fmt.Errorf("app: which message?")
	}
	blobID, token, err := s.sealAttachment(dataURL, w, h, false, "", "")
	if err != nil {
		return "", err
	}
	// Only the picture changes: a caption and the disappearing timer stay.
	content := token
	if old, ok, _ := s.store.MessageByID(targetID); ok {
		if loc := imageTokenRe.FindStringIndex(old.Content); loc != nil {
			content = old.Content[:loc[0]] + token + old.Content[loc[1]:]
		}
	}
	if err := s.EditMessage(channelID, targetID, content, ""); err != nil {
		return "", err
	}
	return blobID, nil
}

// SendFile seals an arbitrary file (data URL of any mime) into a local blob and
// sends a file-reference token. Unlike images, files are not rendered inline —
// the UI shows a download card. Token:
//
//	[file](concord://file/v1/<blobID>/<keys>/<size>/<mimeB64url>/<nameB64url>)
//
// text is optional, as for SendAttachment.
func (s *Service) SendFile(channelID, dataURL, filename, replyTo, text, dir string) (domain.Message, error) {
	return s.sendFile(channelID, dataURL, filename, replyTo, text, dir, sendClean)
}

// SendFileAsIs is SendFile for a file whose details could not be removed (or
// a video with text tracks), once the person has been told it goes as it is,
// location and all, and has chosen to send it. Everything else is cleaned as
// in SendFile: "as is" only covers what the cleaner would not clean.
func (s *Service) SendFileAsIs(channelID, dataURL, filename, replyTo, text, dir string) (domain.Message, error) {
	return s.sendFile(channelID, dataURL, filename, replyTo, text, dir, sendAsIs)
}

// SendFileWithoutText is SendFile for a video with subtitle or text tracks,
// once the person has chosen to have them removed with the other details
// ("Remove and send").
func (s *Service) SendFileWithoutText(channelID, dataURL, filename, replyTo, text, dir string) (domain.Message, error) {
	return s.sendFile(channelID, dataURL, filename, replyTo, text, dir, sendDropText)
}

// SendVoiceClip sends a voice message the composer recorded. Ogg or WebM
// audio (what the recorder asks the renderer for) goes as it is, not held as
// a video container would be. Anything else, such as the MP4 a renderer
// records when it has neither, goes through the same cleaning and the same
// holds as SendFile.
func (s *Service) SendVoiceClip(channelID, dataURL, filename string) (domain.Message, error) {
	return s.sendFile(channelID, dataURL, filename, "", "", "", sendVoice)
}

// CheckFile answers as SendFile would for this file, without sending it:
// nil, or the same refusal (MediaAsIsMarker, MediaTextMarker). A dialog that
// sends files after creating something (a post) asks first, so nothing is
// created around a file that would be held.
func (s *Service) CheckFile(dataURL string) error {
	_, _, err := prepareFile(dataURL, sendClean)
	return err
}

// MediaAsIsMarker starts the error SendFile returns for a file it could not
// clean, followed by "video", "photo" or "file". The page matches it
// (lib/mediastrip.js) to offer sending the file as it is.
const MediaAsIsMarker = "details can't be removed from this "

// MediaTextMarker is in the error SendFile returns for a video it can clean
// only by removing its subtitle or text tracks. The page matches
// it to offer "Remove and send" (SendFileWithoutText) or "Send as it is".
const MediaTextMarker = "video has subtitles or other text tracks"

type sendMode int

const (
	sendClean    sendMode = iota
	sendAsIs              // the person chose to send what cannot be cleaned
	sendDropText          // the person chose to lose the text tracks
	sendVoice             // a voice clip the composer recorded
)

func (s *Service) sendFile(channelID, dataURL, filename, replyTo, text, dir string, mode sendMode) (domain.Message, error) {
	mime, plain, err := prepareFile(dataURL, mode)
	if err != nil {
		return domain.Message{}, err
	}

	blobID, keys, err := s.sealBlob(plain)
	if err != nil {
		return domain.Message{}, err
	}
	filename = clampBytes(filename, maxFilenameLen)
	token := fmt.Sprintf("[file](concord://file/v1/%s/%s/%d/%s/%s)",
		blobID, keys, len(plain), b64url.EncodeToString([]byte(mime)), b64url.EncodeToString([]byte(filename)))
	return s.sendAs(channelID, withText(token, text), "", replyTo, "", dir)
}

// prepareFile decodes a file and cleans it for sending, or refuses it.
func prepareFile(dataURL string, mode sendMode) (string, []byte, error) {
	m := fileURLRe.FindStringSubmatch(dataURL)
	if m == nil {
		return "", nil, fmt.Errorf("app: attachment must be a data URL")
	}
	mime := m[1]
	plain, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		return "", nil, fmt.Errorf("app: bad attachment encoding: %w", err)
	}
	if len(plain) == 0 || len(plain) > maxFilePlain {
		return "", nil, fmt.Errorf("app: file must be 1 byte – %d MB", maxFilePlain>>20)
	}
	if mode == sendVoice {
		if voiceClip(plain) {
			return mime, plain, nil
		}
		mode = sendClean
	}
	asIs := mode == sendAsIs
	// A picture that goes as a file is cleaned like any other: the composer
	// has no "send the original" option, and it routes pictures here on its
	// own (one the renderer cannot decode, or a file with no type). The format
	// is read from the bytes, so the mime the page guessed does not matter.
	// Videos (MP4, MOV) and HEIF pictures (HEIC, AVIF) lose the location and
	// device details in their index in package bmffmeta; one it cannot clean
	// is refused with MediaAsIsMarker unless the person chose to send it as
	// it is, and one with text tracks with MediaTextMarker unless the person
	// chose. Video containers nothing here reads (Matroska, WebM, AVI, MPEG
	// transport and program streams, ASF, FLV) are held the same way.
	// Anything else (documents, other audio) is sent as it is.
	switch routeOf(plain) {
	case routePicture:
		if plain, err = cleanImage(plain); err != nil {
			return "", nil, err
		}
	case routeHeldPicture:
		// A picture format that can carry a camera's location and that
		// nothing here cleans: held for the same choice as a video the
		// cleaner refuses.
		if !asIs {
			return "", nil, fmt.Errorf("app: the %sphoto, so it would be sent as it is", MediaAsIsMarker)
		}
	case routeHeldVideo:
		if !asIs {
			return "", nil, fmt.Errorf("app: the %svideo, so it would be sent as it is", MediaAsIsMarker)
		}
	case routeMedia:
		out, kind, err := bmffmeta.StripBytesWith(plain, bmffmeta.Options{DropText: mode == sendDropText})
		switch {
		case err == nil:
			plain = out
		case asIs:
		case errors.Is(err, bmffmeta.ErrTextTracks):
			return "", nil, fmt.Errorf("app: this %s, which can hold location", MediaTextMarker)
		default:
			return "", nil, fmt.Errorf("app: the %s%s, so it would be sent as it is", MediaAsIsMarker, mediaNoun(kind))
		}
	}
	return mime, plain, nil
}

// fileRoute is what SendFile does with a file, decided by its first bytes
// before any choice the person makes.
type fileRoute int

const (
	routeAsIs        fileRoute = iota // sent unchanged: documents, audio other than M4A, anything unknown
	routePicture                      // PNG, JPEG, GIF, WebP: cleaned by imgmeta
	routeHeldPicture                  // TIFF, camera raw, JPEG XL, JPEG 2000: held for the choice
	routeHeldVideo                    // Matroska/WebM, AVI, ASF, FLV, MPEG streams: held for the choice
	routeMedia                        // ISO base media (MP4, MOV, M4A, HEIC, AVIF): cleaned, or held
)

func routeOf(b []byte) fileRoute {
	switch f := imgmeta.Format(b); {
	case f == "jpeg" || f == "png" || f == "gif" || f == "webp":
		return routePicture
	case uncleanablePicture(b):
		return routeHeldPicture
	case uncleanableVideo(b):
		return routeHeldVideo
	case bmffmeta.Sniff(b):
		return routeMedia
	}
	return routeAsIs
}

// FileHeadBytes is how much of a file the page sends to FileGoesAsIs.
const FileHeadBytes = 64 << 10

// FileGoesAsIs reports whether SendFile sends a file that starts with head
// unchanged and without asking: a document, audio other than M4A, anything
// it does not recognise. The composer asks before it says so under the
// staged file. Every test routeOf makes reads the first few hundred bytes,
// except that an ISO file whose first box is longer than head and is not
// ftyp, moov, mdat or padding is not recognised from head alone; such a file
// is then called as-is when it is cleaned, which can only show the line
// where it was not needed.
func FileGoesAsIs(head []byte) bool {
	return routeOf(head) == routeAsIs
}

// uncleanableVideo reports video containers that can carry location (GPS in
// tags, in a subtitle track, in the stream) and that no cleaner here reads.
func uncleanableVideo(b []byte) bool {
	for _, sig := range []string{
		"\x1a\x45\xdf\xa3",                 // Matroska, WebM
		"\x30\x26\xb2\x75\x8e\x66\xcf\x11", // ASF, WMV
		"\x00\x00\x01\xba",                 // MPEG program stream
		"FLV\x01",
	} {
		if strings.HasPrefix(string(b), sig) {
			return true
		}
	}
	if len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "AVI " {
		return true
	}
	// MPEG transport stream: a sync byte every 188 bytes (every 192 with
	// the 4-byte timestamp of M2TS/AVCHD).
	for _, p := range [][2]int{{0, 188}, {4, 192}} {
		if len(b) > p[0]+2*p[1] && b[p[0]] == 0x47 && b[p[0]+p[1]] == 0x47 && b[p[0]+2*p[1]] == 0x47 {
			return true
		}
	}
	return false
}

// voiceClip: Ogg, or WebM (Matroska) audio as the composer records it.
func voiceClip(b []byte) bool {
	return strings.HasPrefix(string(b), "OggS") || strings.HasPrefix(string(b), "\x1a\x45\xdf\xa3")
}

// uncleanablePicture reports picture formats that can carry location (in
// TIFF tags, an Exif box, a GeoJP2 uuid) and that no cleaner here reads:
// TIFF and the camera raw formats built on it (DNG, CR2, NEF, ARW), JPEG XL
// and JPEG 2000.
func uncleanablePicture(b []byte) bool {
	for _, sig := range []string{
		"II*\x00", "MM\x00*", "II+\x00", "MM\x00+", // TIFF, BigTIFF
		"\xff\x0a",                       // JPEG XL codestream
		"\x00\x00\x00\x0cJXL \r\n\x87\n", // JPEG XL container
		"\x00\x00\x00\x0cjP  \r\n\x87\n", // JPEG 2000
		"\xff\x4f\xff\x51",               // JPEG 2000 codestream
	} {
		if strings.HasPrefix(string(b), sig) {
			return true
		}
	}
	return false
}

// mediaNoun names a file the way the person thinks of it.
func mediaNoun(k bmffmeta.Kind) string {
	switch k {
	case bmffmeta.KindVideo:
		return "video"
	case bmffmeta.KindPhoto:
		return "photo"
	}
	return "file"
}

// fetchBlobPlaintext resolves a token's blob to its plaintext: local store
// first, then connected members of the channel's guild. Fetched blobs are
// hash-verified and stored, so this node becomes a source too. Concurrent
// fetches of one blob collapse into a single network request.
func (s *Service) fetchBlobPlaintext(channelID, blobID, keys string) ([]byte, error) {
	if !blobIDRe.MatchString(blobID) {
		return nil, fmt.Errorf("app: bad attachment id")
	}
	keyBytes, err := b64url.DecodeString(keys)
	if err != nil || len(keyBytes) != attachKeysLen {
		return nil, fmt.Errorf("app: bad attachment key")
	}

	v, err, _ := s.attachFlight.Do(blobID, func() (any, error) {
		return s.attachmentCiphertext(channelID, blobID)
	})
	if err != nil {
		return nil, err
	}
	ct := v.([]byte)

	var key [32]byte
	var nonce [24]byte
	copy(key[:], keyBytes[:32])
	copy(nonce[:], keyBytes[32:])
	plain, ok := secretbox.Open(nil, ct, &nonce, &key)
	if !ok {
		// The blob is authentic (hash-verified) but this token's key doesn't
		// open it — a corrupt or malicious reference. Keep the blob cached;
		// only this message is broken.
		return nil, fmt.Errorf("app: attachment key invalid")
	}
	return plain, nil
}

// FetchAttachment resolves an image token to a plaintext image data URL.
func (s *Service) FetchAttachment(channelID, blobID, keys, subtype string) (string, error) {
	switch subtype {
	case "png", "jpeg", "gif", "webp":
	default:
		return "", fmt.Errorf("app: bad attachment type")
	}
	plain, err := s.fetchBlobPlaintext(channelID, blobID, keys)
	if err != nil {
		return "", err
	}
	// A viewed image is a decrypted image — queue its text for local search.
	return "data:image/" + subtype + ";base64," + base64.StdEncoding.EncodeToString(plain), nil
}

// FetchFile resolves a file token to a plaintext data URL of the given mime.
// The mime comes from the (attacker-controllable) token, so it is validated to
// a safe charset; the returned data URL is only ever used for a client-side
// download, never executed.
func (s *Service) FetchFile(channelID, blobID, keys, mime string) (string, error) {
	if !mimeRe.MatchString(mime) {
		return "", fmt.Errorf("app: bad file type")
	}
	plain, err := s.fetchBlobPlaintext(channelID, blobID, keys)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(plain), nil
}

// attachmentCiphertext returns a blob's ciphertext from the local store or by
// asking connected guild members, verifying and persisting whatever it fetches.
func (s *Service) attachmentCiphertext(channelID, blobID string) ([]byte, error) {
	if ct, ok, err := s.store.GetAttachment(blobID); err == nil && ok {
		return ct, nil
	}
	if s.store.BlobBanned(blobID) {
		// Removed with the message that held it (releaseBlobs): not fetched
		// again on this device.
		return nil, errNotFound
	}

	s.mu.RLock()
	guildID, tracked := s.channelToGuild[channelID]
	s.mu.RUnlock()
	if !tracked {
		return nil, fmt.Errorf("app: unknown channel")
	}

	req, _ := json.Marshal(attachRequest{BlobID: blobID})
	// Sequential, not fanned out: parallel multi-megabyte downloads of the
	// same blob waste bandwidth for no latency win at this scale.
	for _, p := range s.host.Peers() {
		if !s.guildHasMember(guildID, s.presence(p).Fingerprint) {
			continue
		}
		ct, err := s.requestBlobFrom(p, req)
		if err != nil || len(ct) == 0 {
			continue
		}
		sum := sha256.Sum256(ct)
		if hex.EncodeToString(sum[:]) != blobID {
			continue // wrong or corrupted bytes; try the next peer
		}
		_ = s.store.SaveAttachment(blobID, ct)
		// A picture of an archive page read here is pinned, so this device
		// can pass it on (chroniclepins.go).
		s.pinFetchedChroniclePicture(blobID)
		return ct, nil
	}
	return nil, errNotFound
}

func (s *Service) requestBlobFrom(p peer.ID, req []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	return s.host.RequestAttachment(ctx, p, req)
}

// handleAttachRequest serves a blob from the local store; an empty response
// means "don't have it". It is the same answer whether the blob is not held
// or the asking peer may not have it, so the answer tells a peer nothing
// about what this device holds.
func (s *Service) handleAttachRequest(ctx context.Context, p peer.ID, request []byte) ([]byte, error) {
	var req attachRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return []byte{}, nil
	}
	if !blobIDRe.MatchString(strings.TrimSpace(req.BlobID)) {
		return []byte{}, nil
	}
	return s.blobFor(s.presence(p).Fingerprint, req.BlobID), nil
}

// blobFor returns the blob for the account fingerprint, or an empty answer.
// Only a blob something on this device keeps (store.BlobServable), and only
// to a member of a guild or DM that keeps it here: a channel whose held
// message names it, a guild whose GIF pack holds it, whose archive pinned it,
// or into which an import here sealed it.
func (s *Service) blobFor(fingerprint, blobID string) []byte {
	if fingerprint == "" {
		return []byte{}
	}
	scope, ok := s.store.BlobServeScope(blobID)
	if !ok || !s.inBlobScope(fingerprint, scope) {
		return []byte{}
	}
	ct, ok, err := s.store.GetAttachment(blobID)
	if err != nil || !ok || len(ct) > cnet.MaxAttachResponse {
		return []byte{}
	}
	return ct
}

func (s *Service) inBlobScope(fingerprint string, scope store.BlobScope) bool {
	guilds := scope.Guilds
	s.mu.RLock()
	for _, ch := range scope.Channels {
		if g, ok := s.channelToGuild[ch]; ok {
			guilds = append(guilds, g)
		}
	}
	s.mu.RUnlock()
	for _, g := range guilds {
		if s.guildHasMember(g, fingerprint) {
			return true
		}
	}
	return false
}
