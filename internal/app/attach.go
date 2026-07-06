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

// maxAttachmentPlain caps the decoded image size.
const maxAttachmentPlain = 5 << 20 // 5 MiB

const attachKeysLen = 32 + 24 // secretbox key + nonce

var (
	blobIDRe    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	dataURLRe   = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp);base64,([A-Za-z0-9+/=]+)$`)
	b64url      = base64.RawURLEncoding
	errNotFound = fmt.Errorf("app: attachment not found on any reachable peer")
)

type attachRequest struct {
	BlobID string `json:"blobId"`
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

	var key [32]byte
	var nonce [24]byte
	if _, err := rand.Read(key[:]); err != nil {
		return domain.Message{}, err
	}
	if _, err := rand.Read(nonce[:]); err != nil {
		return domain.Message{}, err
	}
	ct := secretbox.Seal(nil, plain, &nonce, &key)
	sum := sha256.Sum256(ct)
	blobID := hex.EncodeToString(sum[:])

	if err := s.store.SaveAttachment(blobID, ct); err != nil {
		return domain.Message{}, err
	}

	keys := b64url.EncodeToString(append(key[:], nonce[:]...))
	if w < 0 || w > 99999 {
		w = 0
	}
	if h < 0 || h > 99999 {
		h = 0
	}
	token := fmt.Sprintf("![image](concord://attach/v1/%s/%s/%s/%dx%d)", blobID, keys, subtype, w, h)
	return s.send(channelID, token, "", replyTo)
}

// FetchAttachment resolves a token's blob to a plaintext image data URL:
// local store first, then connected members of the channel's guild. Fetched
// blobs are hash-verified and stored, so this node becomes a source too.
// Concurrent fetches of one blob collapse into a single network request.
func (s *Service) FetchAttachment(channelID, blobID, keys, subtype string) (string, error) {
	if !blobIDRe.MatchString(blobID) {
		return "", fmt.Errorf("app: bad attachment id")
	}
	keyBytes, err := b64url.DecodeString(keys)
	if err != nil || len(keyBytes) != attachKeysLen {
		return "", fmt.Errorf("app: bad attachment key")
	}
	switch subtype {
	case "png", "jpeg", "gif", "webp":
	default:
		return "", fmt.Errorf("app: bad attachment type")
	}

	v, err, _ := s.attachFlight.Do(blobID, func() (any, error) {
		return s.attachmentCiphertext(channelID, blobID)
	})
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("app: attachment key invalid")
	}
	return "data:image/" + subtype + ";base64," + base64.StdEncoding.EncodeToString(plain), nil
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
		if !s.guildHasMember(guildID, presenceFor(p).Fingerprint) {
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
