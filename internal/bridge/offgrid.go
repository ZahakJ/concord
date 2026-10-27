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

// FlushLink sends a manual or paid link's outbox. The UI calls it only after
// showing the user the queued cost and getting a yes; expectCost is that
// figure, and a mismatch (more queued since the dialog opened) refuses rather
// than spending money the user did not see.
func (b *Bridge) FlushLink(name string, expectCost int64) (FlushResult, error) {
	m, err := b.offgrid()
	if err != nil {
		return FlushResult{}, err
	}
	for _, v := range m.Links() {
		if v.Name == name && v.QueuedCost != expectCost {
			return FlushResult{}, fmt.Errorf("the outbox changed: it now costs %d µ$, not %d — review it again", v.QueuedCost, expectCost)
		}
	}
	n, cost, err := m.Flush(name)
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
