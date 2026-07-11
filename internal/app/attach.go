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

	"github.com/zahak/concord/internal/domain"
	cnet "github.com/zahak/concord/internal/net"
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
//
// blobID = hex sha256(ciphertext); keys = base64url (no padding) of
// key(32) || nonce(24); subtype ∈ png|jpeg|gif|webp; w/h reserve layout space
// (0x0 = unknown). Every character survives the frontend's escape-first
// markdown pipeline untouched. The frontend mirrors this format in
// lib/attachments.js.

// maxAttachmentPlain caps a decoded inline image; maxFilePlain caps a generic
// file attachment (larger, since files aren't rendered inline).
const maxAttachmentPlain = 5 << 20  // 5 MiB
const maxFilePlain = 25 << 20       // 25 MiB
const maxFilenameLen = 200

const attachKeysLen = 32 + 24 // secretbox key + nonce

var (
	blobIDRe    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	dataURLRe   = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp);base64,([A-Za-z0-9+/=]+)$`)
	fileURLRe   = regexp.MustCompile(`^data:([a-zA-Z0-9!#$&^_.+-]+/[a-zA-Z0-9!#$&^_.+-]+);base64,([A-Za-z0-9+/=]+)$`)
	mimeRe      = regexp.MustCompile(`^[a-zA-Z0-9!#$&^_.+-]+/[a-zA-Z0-9!#$&^_.+-]+$`)
	b64url      = base64.RawURLEncoding
	errNotFound = fmt.Errorf("app: attachment not found on any reachable peer")
)

type attachRequest struct {
	BlobID string `json:"blobId"`
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

// SendAttachment seals an image (a data URL from the UI) into a local blob
// and sends the reference token as a normal chat message.
func (s *Service) SendAttachment(channelID, dataURL string, w, h int, replyTo string) (domain.Message, error) {
	m := dataURLRe.FindStringSubmatch(dataURL)
	if m == nil {
		return domain.Message{}, fmt.Errorf("app: attachment must be a png/jpeg/gif/webp image data URL")
	}
	subtype := m[1]
	plain, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil {
		return domain.Message{}, fmt.Errorf("app: bad attachment encoding: %w", err)
	}
	if len(plain) == 0 || len(plain) > maxAttachmentPlain {
		return domain.Message{}, fmt.Errorf("app: attachment must be 1 byte – %d MB", maxAttachmentPlain>>20)
	}

	blobID, keys, err := s.sealBlob(plain)
	if err != nil {
		return domain.Message{}, err
	}
	if w < 0 || w > 99999 {
		w = 0
	}
	if h < 0 || h > 99999 {
		h = 0
	}
	token := fmt.Sprintf("![image](concord://attach/v1/%s/%s/%s/%dx%d)", blobID, keys, subtype, w, h)
	return s.send(channelID, token, "", replyTo)
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
