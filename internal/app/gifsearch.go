package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/domain"
	cnet "github.com/ZahakJ/concord/internal/net"
)

// GIF search, client half.
//
// The guild pack (gifs.go) is still the private option: a collection the guild
// owns, searched with a substring match over records already on disk, nothing
// leaving the machine. This file adds the other one — searching a public GIF
// service — and routes every byte of it through the user's OWN rendezvous.
//
// WHICH service is the node's business, not the client's. The proxy speaks to
// whichever provider its operator configured (Giphy by default since Google
// decommissioned the public Tenor API on 30 June 2026), and names it in the
// reply's Source field. Nothing here may hardcode a vendor: the frontend prints
// Source verbatim, and a hardcoded "Tenor" in the UI is precisely how this
// feature spent a month telling users something false.
//
// What the proxy buys, precisely: the provider sees one IP (the rendezvous) and
// no search terms tied to a person. What it costs: the rendezvous operator sees
// the terms. That is a worse deal than the pack, and a better one than routing
// through a platform, where the intermediary is a company you did not choose.
// The UI is required to say all of this; see ModalGifs.svelte.
//
// The single property that makes this worth building is that the MEDIA BYTES
// come through the proxy too. If a result carried a provider URL and the UI
// rendered it, every member's browser would connect to the provider and the
// feature would be a privacy claim with nothing behind it. So a search result
// carries an opaque handle, never an address, and thumbnails and full GIFs are
// both fetched with an explicit "media" round trip. Nothing in the client can
// turn a handle into a URL, because nothing in the client ever sees one.

// Statuses the CLIENT decides, on top of the ones the node can report
// (cnet.GifStatus*). They exist because "there is no proxy" and "the proxy said
// no" need different words in the UI and a different fix by the user.
const (
	// GifSearchNoRendezvous: no rendezvous is configured at all, so there is
	// nothing to proxy through.
	GifSearchNoRendezvous = "no_rendezvous"
	// GifSearchUnreachable: a rendezvous is configured but we could not get an
	// answer out of it.
	GifSearchUnreachable = "unreachable"
	// GifSearchOff: the user has switched off searching a public GIF service
	// (Privacy & safety). Distinct from every other status here because nothing
	// is wrong and nobody else can fix it — the tab has to offer the switch
	// rather than explain an outage.
	GifSearchOff = "off"
)

// gifSearchTimeout bounds one proxy round trip from the client's side. It is
// shorter than the node's own upstream timeout budget plus slack, which is the
// point: the UI must reach a definite state rather than spin.
const gifSearchTimeout = 25 * time.Second

// maxGifSearchResults caps what we ask for in one page. The node clamps this
// too; asking for a sane number keeps a reply small enough that the thumbnails
// which follow are the expensive part, not the metadata.
const maxGifSearchResults = 24

// GifSearchResult is one page of results, or an explained absence of them.
// Status is always set; Results may be empty with Status == ok, which means the
// search genuinely matched nothing.
type GifSearchResult struct {
	Status  string        `json:"status"`
	Detail  string        `json:"detail,omitempty"`
	Source  string        `json:"source,omitempty"` // the provider the node used, e.g. "Giphy"
	Via     string        `json:"via,omitempty"`    // peer id of the rendezvous that served them
	Results []cnet.GifHit `json:"results"`
	Next    string        `json:"next,omitempty"`
}

var errNoRendezvous = errors.New("app: no rendezvous is configured")

// gifProxyRoundTrip is the one call that touches the network, indirected
// through a package var purely so tests can stand a fake proxy in its place —
// calling a real GIF API from a test is out of the question, and so is standing
// up a second libp2p host for every case. Production never reassigns it; a test
// that does must restore it with t.Cleanup.
var gifProxyRoundTrip = (*Service).askRendezvous

// gifProxyPeers returns the rendezvous nodes worth asking, connected ones
// first. A configured-but-not-currently-connected node is still tried: libp2p
// knows its address and will dial, and the alternative is telling the user
// "unreachable" without having tried.
func (s *Service) gifProxyPeers() []peer.ID {
	boot := s.bootstrapPeers()
	if len(boot) == 0 {
		return nil
	}
	live := map[peer.ID]bool{}
	for _, p := range s.host.Peers() {
		live[p] = true
	}
	var connected, rest []peer.ID
	for _, pi := range boot {
		if live[pi.ID] {
			connected = append(connected, pi.ID)
		} else {
			rest = append(rest, pi.ID)
		}
	}
	return append(connected, rest...)
}

// gifSearchOffResult is what both entry points return when the switch is off.
// Results is an empty slice rather than nil because the picker iterates it.
func gifSearchOffResult() GifSearchResult {
	return GifSearchResult{Status: GifSearchOff, Results: []cnet.GifHit{}}
}

// askRendezvous sends one request to the first rendezvous that answers.
// Returns the node's reply and which node gave it.
func (s *Service) askRendezvous(ctx context.Context, req cnet.GifRequest) (cnet.GifResponse, string, error) {
	peers := s.gifProxyPeers()
	if len(peers) == 0 {
		return cnet.GifResponse{}, "", errNoRendezvous
	}
	body, err := json.Marshal(req)
	if err != nil {
		return cnet.GifResponse{}, "", err
	}
	var lastErr error
	for _, p := range peers {
		rctx, cancel := context.WithTimeout(ctx, gifSearchTimeout)
		raw, err := s.host.RequestGifSearch(rctx, p, body)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		var resp cnet.GifResponse
		if err := json.Unmarshal(raw, &resp); err != nil {
			// A node that answers with nonsense is not a node that will answer
			// the next request either, but it IS reachable, so keep trying the
			// others rather than reporting it as an outage.
			lastErr = fmt.Errorf("app: rendezvous sent an unreadable GIF reply")
			continue
		}
		if resp.Status == "" {
			lastErr = fmt.Errorf("app: rendezvous sent a GIF reply with no status")
			continue
		}
		return resp, p.String(), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("app: no rendezvous answered")
	}
	return cnet.GifResponse{}, "", lastErr
}

// SearchGifs runs one search through the rendezvous. It never returns an error:
// every way this can go wrong is a state the picker has to describe in words,
// so they all come back as a Status.
func (s *Service) SearchGifs(ctx context.Context, query, pos string) GifSearchResult {
	if !s.GifSearchEnabled() {
		return gifSearchOffResult()
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return GifSearchResult{Status: cnet.GifStatusBadRequest, Detail: "type something to search for", Results: []cnet.GifHit{}}
	}
	resp, via, err := gifProxyRoundTrip(s, ctx, cnet.GifRequest{
		Op: "search", Query: query, Pos: pos, Limit: maxGifSearchResults,
	})
	if err != nil {
		return GifSearchResult{Status: gifErrStatus(err), Detail: gifErrDetail(err), Results: []cnet.GifHit{}}
	}
	out := GifSearchResult{
		Status: resp.Status, Detail: resp.Detail, Source: resp.Source,
		Via: via, Next: resp.Next, Results: resp.Results,
	}
	if out.Results == nil {
		out.Results = []cnet.GifHit{}
	}
	if out.Status == cnet.GifStatusOK && out.Source == "" {
		out.Source = "the GIF API"
	}
	return out
}

func gifErrStatus(err error) string {
	if errors.Is(err, errNoRendezvous) {
		return GifSearchNoRendezvous
	}
	return GifSearchUnreachable
}

func gifErrDetail(err error) string {
	if errors.Is(err, errNoRendezvous) {
		return "no rendezvous is configured, so there is nothing to search through"
	}
	return "your rendezvous did not answer"
}

// fetchGifMedia pulls one image through the proxy. full picks the full-size
// version over the thumbnail; both take this same path, which is the whole
// point of the feature.
func (s *Service) fetchGifMedia(ctx context.Context, ref string, full bool) (plain []byte, subtype string, err error) {
	// Every route to the proxy passes through here — thumbnails, sending, and
	// saving to the guild pack all resolve a handle. Gating the one funnel is
	// what makes "off means no request" true rather than approximately true: a
	// handle minted before the switch was flipped is still a live address on
	// somebody else's machine, and clicking a result left on screen must not
	// quietly redeem it.
	if !s.GifSearchEnabled() {
		return nil, "", fmt.Errorf("app: GIF search is switched off in Privacy & safety")
	}
	if strings.TrimSpace(ref) == "" {
		return nil, "", fmt.Errorf("app: no GIF selected")
	}
	resp, _, err := gifProxyRoundTrip(s, ctx, cnet.GifRequest{Op: "media", Ref: ref, Full: full})
	if err != nil {
		return nil, "", fmt.Errorf("%s", gifErrDetail(err))
	}
	if resp.Status != cnet.GifStatusOK {
		detail := resp.Detail
		if detail == "" {
			detail = resp.Status
		}
		return nil, "", fmt.Errorf("app: %s", detail)
	}
	switch resp.Subtype {
	case "png", "jpeg", "gif", "webp":
	default:
		// The node is not trusted to be honest about what it sent; the subtype
		// ends up in an attachment token that every other member will render.
		return nil, "", fmt.Errorf("app: rendezvous returned an unsupported image type")
	}
	// The node caps this too, but its cap is ITS policy. A hostile or modified
	// rendezvous could send a full frame, and a blob over maxGifPlain could not
	// be posted anyway — better to refuse here than to seal something unsendable.
	if len(resp.Media) == 0 || len(resp.Media) > maxGifPlain {
		return nil, "", fmt.Errorf("app: that image is %d bytes — the limit is %d MB", len(resp.Media), maxGifPlain>>20)
	}
	return resp.Media, resp.Subtype, nil
}

// GifSearchMedia returns one search result's image as a data URL, fetched
// through the rendezvous. The UI renders this string directly, so the browser
// makes no request of its own — if it did, Google would see every member's IP
// and this whole file would be pointless.
func (s *Service) GifSearchMedia(ctx context.Context, ref string, full bool) (string, error) {
	plain, subtype, err := s.fetchGifMedia(ctx, ref, full)
	if err != nil {
		return "", err
	}
	return gifDataURLOf(subtype, plain), nil
}

func gifDataURLOf(subtype string, plain []byte) string {
	return "data:image/" + subtype + ";base64," + base64.StdEncoding.EncodeToString(plain)
}

// SendSearchedGif posts a searched GIF into a channel. It seals the bytes into
// an ordinary encrypted attachment and emits the same v1 token the guild pack
// does, so recipients need no new code and no idea where it came from — and, in
// particular, do NOT fetch it from the provider themselves.
func (s *Service) SendSearchedGif(ctx context.Context, channelID, ref, replyTo string, w, h int) (domain.Message, error) {
	s.mu.RLock()
	_, tracked := s.channelToGuild[channelID]
	s.mu.RUnlock()
	if !tracked {
		return domain.Message{}, fmt.Errorf("app: unknown channel")
	}
	plain, subtype, err := s.fetchGifMedia(ctx, ref, true)
	if err != nil {
		return domain.Message{}, err
	}
	// Dimensions come from the search result, i.e. from the provider via the node, so
	// they are a layout hint from an untrusted source. Out-of-range values are
	// dropped to "unknown" rather than rejected: a wrong hint should not stop a
	// GIF being sent, but it must not be able to blow out every member's layout.
	if w < 0 || w > 99999 || h < 0 || h > 99999 {
		w, h = 0, 0
	}
	blobID, keys, err := s.sealBlob(plain)
	if err != nil {
		return domain.Message{}, err
	}
	token := fmt.Sprintf("![image](concord://attach/v1/%s/%s/%s/%dx%d)", blobID, keys, subtype, w, h)
	return s.send(channelID, token, "", replyTo)
}

// SaveSearchedGif adds a searched GIF to the guild's own pack, so a good result
// stops needing the proxy at all: from then on it is a guild-owned blob served
// by members. Deliberately routed through AddGuildGif rather than a private
// copy of it, so the permission check, the pack cap, the validation and the
// announcement to other members are the same ones.
func (s *Service) SaveSearchedGif(ctx context.Context, guildID, name string, tags []string, ref string, w, h int) (GuildGif, error) {
	if !s.hasPerm(guildID, PermManageGuild) {
		return GuildGif{}, fmt.Errorf("app: you don't have permission to manage this guild")
	}
	plain, subtype, err := s.fetchGifMedia(ctx, ref, true)
	if err != nil {
		return GuildGif{}, err
	}
	if w < 0 || w > 99999 || h < 0 || h > 99999 {
		w, h = 0, 0
	}
	return s.AddGuildGif(guildID, name, tags, gifDataURLOf(subtype, plain), w, h)
}

// GifSearchAvailable reports whether a GIF search could work at all, without
// running one. The picker asks this when its Search tab is opened so it can
// explain an unusable tab BEFORE the user types, rather than after.
func (s *Service) GifSearchAvailable(ctx context.Context) GifSearchResult {
	// Asked before anything is typed, so the switch is checked here too — the
	// probe is itself a round trip to the rendezvous, and an install that has
	// opted out should not be announcing that it opened the picker.
	if !s.GifSearchEnabled() {
		return gifSearchOffResult()
	}
	resp, via, err := gifProxyRoundTrip(s, ctx, cnet.GifRequest{Op: "status"})
	if err != nil {
		return GifSearchResult{Status: gifErrStatus(err), Detail: gifErrDetail(err), Results: []cnet.GifHit{}}
	}
	out := GifSearchResult{Status: resp.Status, Detail: resp.Detail, Via: via, Source: resp.Source, Results: []cnet.GifHit{}}
	if out.Status == cnet.GifStatusOK && out.Source == "" {
		out.Source = "the GIF API"
	}
	return out
}
