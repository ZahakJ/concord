package app

// Device linking hands the account seed to whoever the issuer answers. These
// tests hold the issuer to three promises (findings F-1 to F-3 of the Tamarin
// model in assure/concord/link, reproduced there in repro/):
//
//   F-1  seeing the code is not enough: the owner confirms the new device on
//        the issuer, and the request must come from the key it asks to have
//        certified;
//   F-2  a code is redeemed at most once, also under concurrent requests;
//   F-3  the issuer itself refuses a code older than its two minutes.
//
// Every Service lives in t.TempDir(), listens on loopback only, with mDNS off
// and no bootstrap peers.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"sync"
	"testing"
	"time"

	lcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/link"
)

func startLoopbackIssuer(t *testing.T, ctx context.Context) *Service {
	t.Helper()
	svc, err := Start(ctx, Config{
		DataDir:     t.TempDir(),
		Passphrase:  "test-pass",
		DisableMDNS: true,
		listenAddrs: []string{"/ip4/127.0.0.1/tcp/0", "/ip4/127.0.0.1/udp/0/quic-v1"},
	})
	if err != nil {
		t.Fatalf("Start issuer: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// keyPeer is the libp2p peer ID a host keyed by pub has.
func keyPeer(t *testing.T, pub ed25519.PublicKey) peer.ID {
	t.Helper()
	pk, err := lcrypto.UnmarshalEd25519PublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPublicKey(pk)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// codeHolderRequest builds what anyone who saw the code can send: a fresh
// nonce, the joiner proof, and a device key of its own. It returns the
// request and the peer ID of the host that sends it, which is keyed by that
// device key, as RedeemLink's host is.
func codeHolderRequest(t *testing.T, secret []byte, name string) ([]byte, peer.ID) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return linkRequestFor(t, secret, name, pub), keyPeer(t, pub)
}

func linkRequestFor(t *testing.T, secret []byte, name string, pub ed25519.PublicKey) []byte {
	t.Helper()
	nonce, err := link.Nonce()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(linkRequest{
		JoinerNonce: nonce,
		JoinerProof: link.Proof(secret, link.RoleJoiner, nonce),
		DevicePub:   pub,
		DeviceName:  name,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// gotSeed reports whether an issuer answer carries the issuer's account seed.
func gotSeed(t *testing.T, s *Service, out []byte, err error) bool {
	t.Helper()
	if err != nil {
		return false
	}
	var resp linkResponse
	if json.Unmarshal(out, &resp) != nil {
		t.Errorf("unparseable link response")
		return false
	}
	return bytes.Equal(resp.AccountSeed, s.id.Seed())
}

func offerSecret(t *testing.T, s *Service) []byte {
	t.Helper()
	code, err := s.LinkOffer()
	if err != nil {
		t.Fatal(err)
	}
	off, err := link.DecodeOffer(code)
	if err != nil {
		t.Fatalf("fresh offer should decode: %v", err)
	}
	return off.Secret
}

// F-1. Somebody photographs the QR code (or reads the clipboard) and dials
// the issuer before the owner's device does. The owner is shown a device it
// does not recognise and says no: the seed must not leave.
func TestLinkCodeHolderGetsNoSeedWithoutOwnerConfirm(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startLoopbackIssuer(t, ctx)
	manualLinkConfirm(issuer)
	secret := offerSecret(t, issuer)

	req, from := codeHolderRequest(t, secret, "thief")
	type answer struct {
		out []byte
		err error
	}
	done := make(chan answer, 1)
	go func() {
		out, err := issuerAnswer(ctx, issuer, from, req)
		done <- answer{out, err}
	}()

	name, sas, asked := declineLinkPrompt(issuer, 10*time.Second)
	var a answer
	select {
	case a = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the issuer never answered the request")
	}
	if gotSeed(t, issuer, a.out, a.err) {
		t.Fatalf("WEAKNESS: anyone holding the code received the account seed; the owner was never asked")
	}
	if !asked {
		t.Fatalf("the issuer never asked its owner about the requesting device")
	}
	if name != "thief" || len(sas) != 7 {
		t.Errorf("the prompt showed name %q and code %q; want the requester's name and a 6-digit code", name, sas)
	}
}

// F-1. The request names the device key to certify; it must be the key of the
// connection it arrived on, the one the joiner's host is keyed by. Otherwise
// the code holder could have a key of its choice certified from anywhere.
func TestLinkRequestMustComeFromTheKeyItCertifies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startLoopbackIssuer(t, ctx)
	secret := offerSecret(t, issuer)

	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, from := codeHolderRequest(t, secret, "x")
	out, herr := issuerAnswer(ctx, issuer, from, linkRequestFor(t, secret, "mismatch", other))
	if gotSeed(t, issuer, out, herr) {
		t.Fatalf("WEAKNESS: the issuer certified a device key other than the dialler's and handed over the seed")
	}
}

// F-2. Single use under concurrency: eight simultaneous valid requests for
// one code. Exactly one may succeed.
func TestLinkOfferRedeemedAtMostOnceUnderConcurrency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startLoopbackIssuer(t, ctx)

	const rounds, n = 20, 8
	dist := map[int]int{}
	for r := 0; r < rounds; r++ {
		secret := offerSecret(t, issuer)
		reqs := make([][]byte, n)
		froms := make([]peer.ID, n)
		for i := range reqs {
			reqs[i], froms[i] = codeHolderRequest(t, secret, "racer")
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		start := make(chan struct{})
		wins := 0
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(req []byte, from peer.ID) {
				defer wg.Done()
				<-start
				out, herr := issuerAnswer(ctx, issuer, from, req)
				if gotSeed(t, issuer, out, herr) {
					mu.Lock()
					wins++
					mu.Unlock()
				}
			}(reqs[i], froms[i])
		}
		close(start)
		wg.Wait()
		dist[wins]++
	}
	t.Logf("%d rounds x %d simultaneous valid requests; {seeds handed out in a round: rounds} = %v", rounds, n, dist)
	if dist[1] != rounds {
		t.Errorf("WEAKNESS: want exactly one seed per code in every round, got %v", dist)
	}

	// Sequential control: a second request after the first returned is refused.
	secret := offerSecret(t, issuer)
	req1, from1 := codeHolderRequest(t, secret, "first")
	out, err1 := issuerAnswer(ctx, issuer, from1, req1)
	if !gotSeed(t, issuer, out, err1) {
		t.Errorf("sequential control: the first request should succeed, err %v", err1)
	}
	req2, from2 := codeHolderRequest(t, secret, "second")
	out, err2 := issuerAnswer(ctx, issuer, from2, req2)
	if gotSeed(t, issuer, out, err2) || err2 == nil {
		t.Errorf("sequential control: the second request should be refused, got err %v", err2)
	}
}

// F-3. The issuer refuses a code past its two minutes by its own clock, not
// only the joiner's decoder: a code read off a screen and replayed later must
// not work.
func TestLinkIssuerEnforcesOfferExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	issuer := startLoopbackIssuer(t, ctx)

	secret := offerSecret(t, issuer)
	ageLinkOffer(issuer, 2*time.Minute+time.Second)
	req, from := codeHolderRequest(t, secret, "late")
	out, err := issuerAnswer(ctx, issuer, from, req)
	if gotSeed(t, issuer, out, err) {
		t.Fatalf("WEAKNESS: the issuer handed out the account seed for a code past its two minutes")
	}

	// Control: within the window the same request succeeds.
	secret = offerSecret(t, issuer)
	ageLinkOffer(issuer, 2*time.Minute-5*time.Second)
	req, from = codeHolderRequest(t, secret, "in time")
	out, err = issuerAnswer(ctx, issuer, from, req)
	if !gotSeed(t, issuer, out, err) {
		t.Fatalf("a code inside its window was refused: %v", err)
	}
}
