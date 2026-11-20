package app

// The confirm step of device linking, end to end and at its edges: both
// screens show the same six digits, nothing moves without the owner's yes in
// time, closing the dialog says no, and devices on either side of 0.61 refuse
// each other with a reason instead of linking without the confirm step.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/link"
	cnet "github.com/ZahakJ/concord/internal/net"
)

// The new device and the issuer show the same digits for the same device, the
// prompt names the device, and the link completes on the owner's yes. The
// code is then spent: another device holding it hears that it was used.
func TestLinkConfirmShowsTheSameDigitsOnBothScreens(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startServiceInDir(t, ctx, t.TempDir())
	manualLinkConfirm(issuer)
	code, err := issuer.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}

	waiting := make(chan LinkWaiting, 1)
	done := make(chan error, 1)
	go func() {
		_, err := RedeemLinkWatch(ctx, t.TempDir(), code, "test-pass", "", func(w LinkWaiting) { waiting <- w })
		done <- err
	}()
	p := waitLinkPrompt(issuer, 60*time.Second)
	if p == nil {
		t.Fatal("the issuer never asked its owner")
	}
	var w LinkWaiting
	select {
	case w = <-waiting:
	case err := <-done:
		t.Fatalf("the link ended before the new device showed its digits: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("the new device never showed its digits")
	}
	if w.SAS != p.SAS || len(p.SAS) != 7 {
		t.Fatalf("new device shows %q, issuer shows %q; want the same six digits", w.SAS, p.SAS)
	}
	if p.DeviceName != defaultDeviceName() || w.DeviceName != p.DeviceName {
		t.Errorf("prompt names %q, new device says it is %q; want %q", p.DeviceName, w.DeviceName, defaultDeviceName())
	}
	if p.SecondsLeft <= 0 || p.SecondsLeft > int(linkConfirmWindow/time.Second) || w.Seconds <= 0 {
		t.Errorf("seconds left: issuer %d, new device %d", p.SecondsLeft, w.Seconds)
	}
	if err := issuer.ConfirmLink(p.ID, true); err != nil {
		t.Fatalf("ConfirmLink: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the confirmed link failed: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("the confirmed link never finished")
	}
	if st := issuer.LinkStatus(); !st.Used || st.Prompt != nil {
		t.Errorf("after the link the issuer reports %+v; want the code used and nothing waiting", st)
	}

	_, err = RedeemLink(ctx, t.TempDir(), code, "test-pass", "")
	if err == nil || !strings.Contains(err.Error(), linkErrUsed) {
		t.Fatalf("a second device with the spent code got %v; want %q", err, linkErrUsed)
	}
}

// An owner who does not answer has said no: the request is refused when the
// window closes, and a yes after that is refused too.
func TestLinkUnansweredIsRefused(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startLoopbackIssuer(t, ctx)
	manualLinkConfirm(issuer)
	issuer.linkMu.Lock()
	issuer.linkWindow = 300 * time.Millisecond
	issuer.linkMu.Unlock()
	secret := offerSecret(t, issuer)
	req, from := codeHolderRequest(t, secret, "slow")

	start := time.Now()
	out, err := issuerAnswer(ctx, issuer, from, req)
	if gotSeed(t, issuer, out, err) {
		t.Fatal("an unanswered request received the seed")
	}
	if err == nil || err.Error() != linkErrUnanswered {
		t.Fatalf("got %v; want %q", err, linkErrUnanswered)
	}
	if d := time.Since(start); d < 250*time.Millisecond {
		t.Errorf("refused after %v, before the window closed", d)
	}
	if st := issuer.LinkStatus(); st.Prompt != nil || !st.Used {
		t.Errorf("after the window the issuer reports %+v; want nothing waiting and the code used", st)
	}
}

// Closing the dialog, or showing a new code, while a device waits is a no.
func TestLinkCancelWhileWaitingSaysNo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startLoopbackIssuer(t, ctx)
	manualLinkConfirm(issuer)

	for _, end := range []struct {
		name string
		do   func()
	}{
		{"dialog closed", issuer.CancelLinkOffer},
		{"new code", func() { _, _ = issuer.LinkOffer() }},
	} {
		secret := offerSecret(t, issuer)
		req, from := codeHolderRequest(t, secret, "waiting")
		done := make(chan error, 1)
		var out []byte
		go func() {
			var err error
			out, err = issuerAnswer(ctx, issuer, from, req)
			done <- err
		}()
		p := waitLinkPrompt(issuer, 10*time.Second)
		if p == nil {
			t.Fatalf("%s: no prompt", end.name)
		}
		end.do()
		var err error
		select {
		case err = <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: the waiting request was not answered", end.name)
		}
		if gotSeed(t, issuer, out, err) || err == nil || err.Error() != linkErrDeclined {
			t.Fatalf("%s: got %v; want %q and no seed", end.name, err, linkErrDeclined)
		}
		if issuer.ConfirmLink(p.ID, true) == nil {
			t.Fatalf("%s: a yes for the device that was turned away was accepted", end.name)
		}
	}
}

// A request that does not prove the code leaves the code in place for the
// owner's device, and is not put to the owner.
func TestLinkBadProofDoesNotTakeTheCode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startLoopbackIssuer(t, ctx)
	secret := offerSecret(t, issuer)

	wrong := append([]byte(nil), secret...)
	wrong[0] ^= 1
	req, from := codeHolderRequest(t, wrong, "guess")
	if _, err := issuerAnswer(ctx, issuer, from, req); err == nil || err.Error() != linkErrProof {
		t.Fatalf("a wrong proof got %v; want %q", err, linkErrProof)
	}
	req, from = codeHolderRequest(t, secret, "owner")
	out, err := issuerAnswer(ctx, issuer, from, req)
	if !gotSeed(t, issuer, out, err) {
		t.Fatalf("the owner's device was refused after a wrong guess: %v", err)
	}
}

// A code past its two minutes is refused as expired, every time it is tried,
// and the dialog keeps saying so rather than showing it as live.
func TestLinkExpiredCodeStaysExpired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startLoopbackIssuer(t, ctx)
	secret := offerSecret(t, issuer)
	ageLinkOffer(issuer, link.OfferTTL+time.Second)
	for i := 0; i < 2; i++ {
		req, from := codeHolderRequest(t, secret, "late")
		out, err := issuerAnswer(ctx, issuer, from, req)
		if gotSeed(t, issuer, out, err) || err == nil || err.Error() != linkErrExpired {
			t.Fatalf("try %d: got %v; want %q and no seed", i+1, err, linkErrExpired)
		}
	}
	if st := issuer.LinkStatus(); !st.Expired || st.Prompt != nil {
		t.Fatalf("issuer reports %+v; want the code shown as expired", st)
	}
}

// A device from before 0.61 speaks /concord/link/1.0.0, which handed the seed
// to anyone with the code. A 0.61 issuer refuses it with a message to update,
// tells its own dialog, and leaves the code for an updated device.
func TestLinkOldJoinerIsToldToUpdate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startLoopbackIssuer(t, ctx)
	secret := offerSecret(t, issuer)

	old, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h, err := cnet.New(ctx, cnet.Config{
		Identity:    old,
		HostKey:     old.DeviceKey(),
		ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	dialCtx, dcancel := context.WithTimeout(ctx, 30*time.Second)
	defer dcancel()
	out, err := h.RequestLink(dialCtx, issuer.host.AddrInfo(), linkRequestFor(t, secret, "old phone", old.DevicePublicKey()))
	if err != nil {
		t.Fatalf("the old protocol got no answer: %v", err)
	}
	var resp linkResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccountSeed != nil || resp.Cert != nil || resp.Error != linkErrOldJoiner {
		t.Fatalf("old joiner got seed=%v cert=%v error=%q; want only the update message", resp.AccountSeed != nil, resp.Cert != nil, resp.Error)
	}
	// What that older device's own code makes of it: a refusal, nothing kept.
	if _, err := verifyLinkResponse(secret, old.DevicePublicKey(), &resp); err == nil {
		t.Fatal("the old joiner's check accepted the refusal")
	}
	if st := issuer.LinkStatus(); !st.OldDevice || st.Used {
		t.Fatalf("issuer reports %+v; want the old device noted and the code still unused", st)
	}

	req, from := codeHolderRequest(t, secret, "updated phone")
	out, err = issuerAnswer(ctx, issuer, from, req)
	if !gotSeed(t, issuer, out, err) {
		t.Fatalf("an updated device was refused the code the old one tried: %v", err)
	}
}

// A 0.61 device shown a code from a device before 0.61 refuses to link, says
// the other device must be updated, and writes nothing. It never sends its
// request over the old protocol.
func TestLinkNewJoinerRefusesOldIssuer(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	old, err := identity.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h, err := cnet.New(ctx, cnet.Config{Identity: old})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	asked := make(chan struct{}, 1)
	h.HandleLink(func(context.Context, peer.ID, []byte) ([]byte, error) {
		asked <- struct{}{}
		return nil, errors.New("old issuer")
	})
	var addrs []string
	for _, a := range h.AddrInfo().Addrs {
		addrs = append(addrs, a.String())
	}
	off, err := link.NewOffer(h.AddrInfo().ID.String(), addrs)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	start := time.Now()
	_, err = RedeemLink(ctx, dir, off.Encode(), "test-pass", "")
	if !errors.Is(err, cnet.ErrLinkPeerTooOld) {
		t.Fatalf("got %v; want ErrLinkPeerTooOld", err)
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("took %v to refuse an old issuer", d)
	}
	if _, err := os.Stat(keystorePathIn(dir)); !os.IsNotExist(err) {
		t.Fatalf("a keystore was written for a refused link: %v", err)
	}
	select {
	case <-asked:
		t.Fatal("the new device fell back to the pre-0.61 protocol")
	default:
	}
}
