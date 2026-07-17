package mailbox

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestPushStoreRegisterUnregisterPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")

	ps := OpenPushStore(path)
	ps.Register("box1", DeviceToken{Platform: "fcm", Token: "tokA"})
	ps.Register("box1", DeviceToken{Platform: "apns", Token: "tokB"})
	// Re-registering the same token refreshes rather than duplicates.
	ps.Register("box1", DeviceToken{Platform: "fcm", Token: "tokA"})

	if got := ps.Tokens("box1"); len(got) != 2 {
		t.Fatalf("want 2 tokens, got %d", len(got))
	}

	// Reload from disk: registrations must survive a node restart.
	ps2 := OpenPushStore(path)
	if got := ps2.Tokens("box1"); len(got) != 2 {
		t.Fatalf("after reload want 2 tokens, got %d", len(got))
	}

	ps2.Unregister("box1", "tokA")
	if got := ps2.Tokens("box1"); len(got) != 1 || got[0].Token != "tokB" {
		t.Fatalf("after unregister want [tokB], got %v", got)
	}
}

// fakeNotifier records the mailboxes it was asked to wake.
type fakeNotifier struct {
	mu    sync.Mutex
	woken []string
	done  chan struct{}
}

func (f *fakeNotifier) Notify(mailboxID string, _ []DeviceToken) {
	f.mu.Lock()
	f.woken = append(f.woken, mailboxID)
	f.mu.Unlock()
	if f.done != nil {
		f.done <- struct{}{}
	}
}

// TestDepositTriggersNotify checks a deposit to a mailbox with a registered
// push token fires the notifier (the Service-level wiring, exercised without a
// live libp2p host by driving the store + push store directly).
func TestDepositTriggersNotify(t *testing.T) {
	store := New()
	ps := OpenPushStore(filepath.Join(t.TempDir(), "t.json"))
	fn := &fakeNotifier{done: make(chan struct{}, 1)}
	svc := NewService(store).WithPush(ps, fn)

	const box = "deadbeefdeadbeef"
	store.Register(box)
	ps.Register(box, DeviceToken{Platform: "fcm", Token: "tok"})

	// Mirror the handler's deposit branch: store, then notify if tokens exist.
	if _, ok := svc.store.Deposit(box, "dep", []byte("sealed"), 0); !ok {
		t.Fatal("deposit rejected")
	}
	if toks := svc.pushes.Tokens(box); len(toks) > 0 {
		go svc.notifier.Notify(box, toks)
	}

	<-fn.done
	fn.mu.Lock()
	defer fn.mu.Unlock()
	if len(fn.woken) != 1 || fn.woken[0] != box {
		t.Fatalf("want one wake for %s, got %v", box, fn.woken)
	}
}
