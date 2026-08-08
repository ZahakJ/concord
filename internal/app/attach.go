package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/ZahakJ/concord/internal/domain"
	cnet "github.com/ZahakJ/concord/internal/net"
)

// Encrypted attachments (Signal-style): an image is sealed once with a random
// secretbox key, stored locally as an opaque, content-addressed blob, and the
// message carries only a ~190-char reference token — the key rides INSIDE the
// MLS-encrypted message content. Blobs travel over a dedicated stream protocol
// with its own large frame cap, so gossipsub and history sync never see image
// bytes. Any peer holding a blob can serve it (the ciphertext is useless
// without the key, and the blobID is an unguessable 256-bit capability), which
// is what keeps images fetchable after the sender goes offline: every member
// who viewed the image becomes a source.
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
	if len(name) > maxFilenameLen {
		name = name[:maxFilenameLen]
	}
	if len(desc) > maxAttachDescLen {
		desc = desc[:maxAttachDescLen]
	}
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
func (s *Service) SendAttachment(channelID, dataURL string, w, h int, replyTo string, spoiler bool, name, desc string) (domain.Message, error) {
	_, token, err := s.sealAttachment(dataURL, w, h, spoiler, name, desc)
	if err != nil {
		return domain.Message{}, err
	}
	return s.send(channelID, token, "", replyTo)
}

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
	if err := s.EditMessage(channelID, targetID, token); err != nil {
		return "", err
	}
	return blobID, nil
}

// SendFile seals an arbitrary file (data URL of any mime) into a local blob and
// sends a file-reference token. Unlike images, files are not rendered inline —
// the UI shows a download card. Token:
//
//	[file](concord://file/v1/<blobID>/<keys>/<size>/<mimeB64url>/<nameB64url>)
func (s *Service) SendFile(channelID, dataURL, filename, replyTo string) (domain.Message, error) {
	m := fileURLRe.FindStringSubmatch(dataURL)
	if m == nil {
		return domain.Message{}, fmt.Errorf("app: attachment must be a data URL")
	}
	mime := m[1]
	plain, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		return domain.Message{}, fmt.Errorf("app: bad attachment encoding: %w", err)
	}
	if len(plain) == 0 || len(plain) > maxFilePlain {
		return domain.Message{}, fmt.Errorf("app: file must be 1 byte – %d MB", maxFilePlain>>20)
	}

	blobID, keys, err := s.sealBlob(plain)
	if err != nil {
		return domain.Message{}, err
	}
	if len(filename) > maxFilenameLen {
		filename = filename[:maxFilenameLen]
	}
	token := fmt.Sprintf("[file](concord://file/v1/%s/%s/%d/%s/%s)",
		blobID, keys, len(plain), b64url.EncodeToString([]byte(mime)), b64url.EncodeToString([]byte(filename)))
	return s.send(channelID, token, "", replyTo)
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
// means "don't have it". Serving is deliberately ungated — see the package
// comment above.
func (s *Service) handleAttachRequest(ctx context.Context, _ peer.ID, request []byte) ([]byte, error) {
	var req attachRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return []byte{}, nil
	}
	if !blobIDRe.MatchString(strings.TrimSpace(req.BlobID)) {
		return []byte{}, nil
	}
	ct, ok, err := s.store.GetAttachment(req.BlobID)
	if err != nil || !ok || len(ct) > cnet.MaxAttachResponse {
		return []byte{}, nil
	}
	return ct, nil
}
