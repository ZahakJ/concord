package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	cnet "github.com/ZahakJ/concord/internal/net"
)

// Peer-to-peer software distribution, transport half.
//
// A node that holds a release binary can hand it to a peer still running an
// older one, so an update survives GitHub being unreachable, rate-limited or
// blocked. This file is deliberately dumb: it moves bytes and knows nothing
// about whether they are trustworthy. Every trust decision — signature over
// the manifest, downgrade refusal, platform match — lives in internal/bridge,
// next to the embedded release key, and is identical to the GitHub path.
//
// What we serve is our OWN executable. There is no second copy on disk: the
// binary we are running (or the one we just installed and haven't restarted
// into) IS the asset, and release.json records which signed asset it is.

// ReleaseManifest describes the release binary this node can serve. It is
// written by the updater once it has verified a download, so its mere presence
// means "these bytes were covered by a signature we checked".
type ReleaseManifest struct {
	Version string `json:"version"`
	Asset   string `json:"asset"` // release asset filename, e.g. concord-linux-amd64-v0.9.0
	Size    int64  `json:"size"`  // size of the executable when it was verified
	Sums    []byte `json:"sums"`  // the release's SHA256SUMS, verbatim
	Sig     []byte `json:"sig"`   // detached ed25519 signature over Sums
}

func releaseManifestPath(dataDir string) string {
	return filepath.Join(dataDir, "release.json")
}

// LoadReleaseManifest reads the seedable-release record; a missing or corrupt
// file just means this node has nothing to offer.
func LoadReleaseManifest(dataDir string) ReleaseManifest {
	var m ReleaseManifest
	b, err := os.ReadFile(releaseManifestPath(dataDir))
	if err != nil {
		return ReleaseManifest{}
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return ReleaseManifest{}
	}
	return m
}

// SaveReleaseManifest records what this node may serve to peers.
func SaveReleaseManifest(dataDir string, m ReleaseManifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(releaseManifestPath(dataDir), b, 0o600)
}

// Wire shapes. Ops are separate round trips so a client can decide from the
// cheap ones (offer, manifest) whether the expensive one (chunk) is worth it.
type releaseRequest struct {
	Op     string `json:"op"`               // offer | manifest | chunk
	Offset int64  `json:"offset,omitempty"` // chunk: byte offset into the asset
	Length int    `json:"length,omitempty"` // chunk: bytes wanted
}

// ReleaseOffer is what a peer claims to hold. Every field is untrusted input:
// the caller re-derives the truth from the signed manifest before installing.
type ReleaseOffer struct {
	PeerID  string `json:"peerId"`
	Version string `json:"version"`
	Asset   string `json:"asset"`
	Size    int64  `json:"size"`
}

// ReleaseSignedSums carries the release's checksum manifest and its detached
// signature, exactly as they were published.
type ReleaseSignedSums struct {
	Sums []byte `json:"sums"`
	Sig  []byte `json:"sig"`
}

// releasePeerTimeout bounds one metadata round trip. Chunks get their own,
// longer budget (see PeerReleaseChunk).
const releasePeerTimeout = 8 * time.Second

// servableRelease returns the manifest and executable path this node can serve,
// or ok=false when it has nothing verified to offer.
//
// The size check is the guard against seeding the wrong bytes: if the binary on
// disk is no longer the one the manifest was written for (someone swapped it by
// hand, or an update is half-applied) we stay silent rather than waste a peer's
// download on something that will fail its checksum.
func servableRelease(dataDir string) (ReleaseManifest, string, bool) {
	m := LoadReleaseManifest(dataDir)
	if m.Version == "" || m.Asset == "" || m.Size <= 0 || len(m.Sums) == 0 {
		return ReleaseManifest{}, "", false
	}
	exe, err := os.Executable()
	if err != nil {
		return ReleaseManifest{}, "", false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	fi, err := os.Stat(exe)
	if err != nil || fi.Size() != m.Size {
		return ReleaseManifest{}, "", false
	}
	return m, exe, true
}

// handleReleaseRequest serves this node's release to peers we share a guild
// with. An empty response means "nothing to offer" — the caller moves on to the
// next peer, or to GitHub.
//
// The bytes are public, so the gate is not about them: it is about the version
// string. An ungated responder tells anyone who connects — a DHT routing peer,
// any IPFS node once the public-DHT opt-in is on — exactly which release, OS
// and architecture this install is, which is a ready-made way to find every
// node still on a version with a known hole. The gate covers all three ops, not
// just "offer": the signed SHA256SUMS names the release just as plainly.
func (s *Service) handleReleaseRequest(_ context.Context, from peer.ID, request []byte) ([]byte, error) {
	if !s.sharesGuild(s.presence(from).Fingerprint) {
		return []byte{}, nil
	}
	return s.serveRelease(request), nil
}

// serveRelease answers one release request from a peer that has already passed
// the membership gate.
func (s *Service) serveRelease(request []byte) []byte {
	var req releaseRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return []byte{}
	}
	m, exe, ok := servableRelease(s.dataDir)
	if !ok {
		return []byte{}
	}
	switch req.Op {
	case "offer":
		b, _ := json.Marshal(ReleaseOffer{Version: m.Version, Asset: m.Asset, Size: m.Size})
		return b
	case "manifest":
		b, _ := json.Marshal(ReleaseSignedSums{Sums: m.Sums, Sig: m.Sig})
		return b
	case "chunk":
		if req.Offset < 0 || req.Offset >= m.Size || req.Length <= 0 {
			return []byte{}
		}
		n := req.Length
		if n > cnet.ReleaseChunkSize {
			n = cnet.ReleaseChunkSize
		}
		if int64(n) > m.Size-req.Offset {
			n = int(m.Size - req.Offset)
		}
		f, err := os.Open(exe)
		if err != nil {
			return []byte{}
		}
		defer f.Close()
		buf := make([]byte, n)
		if _, err := f.ReadAt(buf, req.Offset); err != nil {
			return []byte{}
		}
		return buf
	}
	return []byte{}
}

// PeerReleaseOffers asks every connected peer what release it holds. Fanned
// out, unlike attachment fetches: the replies are a few hundred bytes each, so
// there is no bandwidth reason to pay one timeout at a time.
func (s *Service) PeerReleaseOffers(ctx context.Context) []ReleaseOffer {
	req, _ := json.Marshal(releaseRequest{Op: "offer"})
	peers := s.host.Peers()

	var mu sync.Mutex
	var out []ReleaseOffer
	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Add(1)
		go func(p peer.ID) {
			defer wg.Done()
			rctx, cancel := context.WithTimeout(ctx, releasePeerTimeout)
			defer cancel()
			resp, err := s.host.RequestRelease(rctx, p, req)
			if err != nil || len(resp) == 0 {
				return
			}
			var o ReleaseOffer
			if err := json.Unmarshal(resp, &o); err != nil || o.Version == "" {
				return
			}
			o.PeerID = p.String()
			mu.Lock()
			out = append(out, o)
			mu.Unlock()
		}(p)
	}
	wg.Wait()
	// Stable order by peer, not by version: comparing releases is the caller's
	// job (it owns the semver rules and the downgrade refusal), so this layer
	// only guarantees the list doesn't reshuffle between retries.
	sort.Slice(out, func(i, j int) bool { return out[i].PeerID < out[j].PeerID })
	return out
}

// PeerReleaseManifest fetches a peer's SHA256SUMS and its detached signature.
func (s *Service) PeerReleaseManifest(ctx context.Context, peerID string) (ReleaseSignedSums, error) {
	req, _ := json.Marshal(releaseRequest{Op: "manifest"})
	rctx, cancel := context.WithTimeout(ctx, releasePeerTimeout)
	defer cancel()
	resp, err := s.requestReleaseFrom(rctx, peerID, req)
	if err != nil {
		return ReleaseSignedSums{}, err
	}
	var out ReleaseSignedSums
	if err := json.Unmarshal(resp, &out); err != nil {
		return ReleaseSignedSums{}, fmt.Errorf("app: peer sent an unreadable release manifest")
	}
	return out, nil
}

// PeerReleaseChunk fetches the next slice of the peer's binary at offset,
// where remaining is how many bytes are still outstanding. How big a slice
// that turns into is this layer's business, so the caller never has to know
// the wire chunk size; it just advances by len(chunk) until it is done.
func (s *Service) PeerReleaseChunk(ctx context.Context, peerID string, offset, remaining int64) ([]byte, error) {
	want := min(remaining, int64(cnet.ReleaseChunkSize))
	if want <= 0 {
		return nil, fmt.Errorf("app: nothing left to fetch")
	}
	req, _ := json.Marshal(releaseRequest{Op: "chunk", Offset: offset, Length: int(want)})
	rctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	b, err := s.requestReleaseFrom(rctx, peerID, req)
	if err != nil {
		return nil, err
	}
	// Exactly what was asked for, or nothing. An honest seeder always sends the
	// full slice — handleReleaseRequest clamps the length to the chunk size and
	// to the bytes remaining, and this request already asked for no more than
	// either — so a short answer is a peer answering a question we didn't ask.
	// The strictness is the point: at one byte per reply a peer could otherwise
	// keep a transfer "in progress" for a hundred million round trips without
	// ever erroring. If chunking ever grows a legitimate short reply, that is a
	// new releaseProtocol version, not a relaxation here.
	if int64(len(b)) != want {
		return nil, fmt.Errorf("app: peer answered a %d-byte chunk request with %d bytes", want, len(b))
	}
	return b, nil
}

func (s *Service) requestReleaseFrom(ctx context.Context, peerID string, req []byte) ([]byte, error) {
	pid, err := peer.Decode(peerID)
	if err != nil {
		return nil, fmt.Errorf("app: bad peer id")
	}
	return s.host.RequestRelease(ctx, pid, req)
}
