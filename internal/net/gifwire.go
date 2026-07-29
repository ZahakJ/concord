package net

// Wire shapes for the GIF-search protocol. They live here, in the package both
// halves already import, so the rendezvous proxy (cmd/rendezvous) and the
// client (internal/app) cannot drift apart with nothing to catch it.

// GifSearchStatus values. Every one of these is something the UI has to be able
// to say out loud, which is why "no key configured" is a status and not an
// error: an operator who never set one has not broken anything, and the tab has
// to explain that rather than show a red failure.
const (
	// GifStatusOK: results (or media) follow.
	GifStatusOK = "ok"
	// GifStatusUnavailable: this node has no GIF API key, so it cannot search.
	GifStatusUnavailable = "unavailable"
	// GifStatusRateLimited: this peer is asking too fast.
	GifStatusRateLimited = "rate_limited"
	// GifStatusExpired: the media handle was minted by a previous run of the
	// node (the signing secret is per-process), so it can no longer be checked.
	GifStatusExpired = "expired"
	// GifStatusUpstream: the GIF API or CDN failed, timed out, or answered with
	// something we refuse to relay.
	GifStatusUpstream = "upstream"
	// GifStatusBadRequest: malformed or out-of-range request.
	GifStatusBadRequest = "bad_request"
)

// GifRequest is client → rendezvous.
type GifRequest struct {
	Op    string `json:"op"`              // "search" | "media"
	Query string `json:"q,omitempty"`     // search: the terms the user typed
	Pos   string `json:"pos,omitempty"`   // search: pagination cursor from a previous reply
	Limit int    `json:"limit,omitempty"` // search: results wanted (clamped by the node)
	Ref   string `json:"ref,omitempty"`   // media: a handle from a search reply
	// Full selects the full-size image rather than the thumbnail for a media
	// fetch. Both go through this same proxy — see the file comment in
	// gifsearch.go for why that is not optional.
	Full bool `json:"full,omitempty"`
}

// GifHit is one search result. It carries no URL: the client has nothing to
// fetch by itself, and could not, because Preview/Full are opaque handles that
// only this node can turn back into an address.
type GifHit struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Preview string `json:"preview"` // handle for the small thumbnail
	Full    string `json:"full"`    // handle for the full-size image
	Width   int    `json:"w,omitempty"`
	Height  int    `json:"h,omitempty"`
}

// GifResponse is rendezvous → client.
type GifResponse struct {
	Status string `json:"status"`
	// Detail is a short human-readable reason, shown verbatim by the UI. It
	// describes the NODE's situation, never the upstream body — a proxy that
	// echoes Google's error text back is a proxy that can be used to probe.
	Detail  string   `json:"detail,omitempty"`
	Results []GifHit `json:"results,omitempty"`
	Next    string   `json:"next,omitempty"` // cursor for the next page, "" when exhausted
	// Source names who actually produced the results, so the UI can say so
	// instead of the frontend hardcoding a guess.
	Source string `json:"source,omitempty"`

	// media reply
	Media   []byte `json:"media,omitempty"`
	Subtype string `json:"subtype,omitempty"` // gif | webp | png | jpeg
}
