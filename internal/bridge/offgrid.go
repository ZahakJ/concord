package bridge

import (
	"encoding/base64"
	"fmt"

	"github.com/ZahakJ/concord/internal/transport"
)

// OffGridView is Settings → Off-grid links: every non-internet link and the global
// switches.
type OffGridView struct {
	Links   []transport.LinkView `json:"links"`
	Offline bool                 `json:"offline"`
	Relay   bool                 `json:"relay"`
}

func (b *Bridge) offgrid() (*transport.Manager, error) {
	svc, err := b.service()
	if err != nil {
		return nil, err
	}
	return svc.OffGrid(), nil
}

// OffGrid lists the links with their status, capabilities and outboxes.
func (b *Bridge) OffGrid() (OffGridView, error) {
	m, err := b.offgrid()
	if err != nil {
		return OffGridView{}, err
	}
	s := m.Settings()
	return OffGridView{Links: m.Links(), Offline: s.Offline, Relay: s.Relay}, nil
}

// SetLinkEnabled is the per-link toggle. This, and nothing else, powers up a
// radio or opens a microphone.
func (b *Bridge) SetLinkEnabled(name string, on bool) error {
	m, err := b.offgrid()
	if err != nil {
		return err
	}
	return m.SetEnabled(name, on)
}

// SetLinksOffline switches offline mode: local links only.
func (b *Bridge) SetLinksOffline(on bool) error {
	m, err := b.offgrid()
	if err != nil {
		return err
	}
	return m.SetOffline(on)
}

// SetLinksRelay switches carrying other people's sealed messages.
func (b *Bridge) SetLinksRelay(on bool) error {
	m, err := b.offgrid()
	if err != nil {
		return err
	}
	return m.SetRelay(on)
}

// FlushResult is what a released outbox sent and cost.
type FlushResult struct {
	Frames int   `json:"frames"`
	Cost   int64 `json:"costMicroUSD"`
}

// FlushLink sends the first count frames of a manual or paid link's outbox.
// The UI calls it only after showing the user what those frames cost and
// getting a yes; expectCost is that figure. Exactly count frames go — anything
// queued while the dialog was open waits for the next approval — and a quote
// that no longer matches refuses rather than spending money the user did not
// see.
func (b *Bridge) FlushLink(name string, count int, expectCost int64) (FlushResult, error) {
	m, err := b.offgrid()
	if err != nil {
		return FlushResult{}, err
	}
	if n, cost := m.QueuedCost(name, count); n != count || cost != expectCost {
		return FlushResult{}, fmt.Errorf("the outbox changed: %d frames now cost %d µ$, not %d — review it again", n, cost, expectCost)
	}
	n, cost, err := m.Flush(name, count)
	return FlushResult{Frames: n, Cost: cost}, err
}

// ExportLink drains a manual link's outbox as a base64 pack for the UI to save
// or show as QR codes.
func (b *Bridge) ExportLink(name string) (string, error) {
	m, err := b.offgrid()
	if err != nil {
		return "", err
	}
	frames, err := m.Export(name)
	if err != nil || len(frames) == 0 {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(transport.EncodePack(frames)), nil
}

// PeekLink returns a manual link's outbox as a base64 pack without draining
// it; DiscardLinkQueued drains the first count once they were carried away.
// A QR sequence closed halfway loses nothing.
func (b *Bridge) PeekLink(name string) (string, error) {
	m, err := b.offgrid()
	if err != nil {
		return "", err
	}
	frames, err := m.Peek(name)
	if err != nil || len(frames) == 0 {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(transport.EncodePack(frames)), nil
}

// DiscardLinkQueued: see PeekLink.
func (b *Bridge) DiscardLinkQueued(name string, count int) error {
	m, err := b.offgrid()
	if err != nil {
		return err
	}
	m.Discard(name, count)
	return nil
}

// ImportLinkPack feeds a carried pack (base64) back in and reports how many
// frames it held. Frames for channels this account is not in are kept only
// long enough to relay, never shown.
func (b *Bridge) ImportLinkPack(packB64 string) (int, error) {
	m, err := b.offgrid()
	if err != nil {
		return 0, err
	}
	raw, err := base64.StdEncoding.DecodeString(packB64)
	if err != nil {
		return 0, err
	}
	frames, err := transport.DecodePack(raw)
	if err != nil {
		return 0, err
	}
	for _, f := range frames {
		m.Receive("file", f)
	}
	return len(frames), nil
}
