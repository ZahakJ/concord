package framing

import (
	"errors"
	"testing"
	"time"

	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/secrettree"
)

// A member can encrypt sender data naming any generation: the sender data key
// is the epoch's, shared by every member. A frame naming 0xFFFFFFFF made the
// receiver run the ratchet four billion steps (about an hour) before the
// content AEAD could fail. It is now refused before any key is derived, and
// the receiver's tree still reads the sender's next real frame.
func TestForgedFarGenerationIsRefusedFast(t *testing.T) {
	cs := ciphersuite.MLS128DHKEMP256
	enc := ciphersuite.NewSecret(make([]byte, 32))
	sendTree, _ := secrettree.NewTree(enc, 2, cs)
	recvTree, _ := secrettree.NewTree(ciphersuite.NewSecret(make([]byte, 32)), 2, cs)
	sds := ciphersuite.NewSecret(make([]byte, 32))
	sig, _ := ciphersuite.GenerateSignaturePrivateKey()
	gc := make([]byte, 32)
	frame := func(text string) *PrivateMessage {
		pm, err := Encrypt(EncryptParams{
			Content: FramedContent{
				GroupID:           make([]byte, 16),
				Sender:            Sender{Type: SenderTypeMember, LeafIndex: 0},
				AuthenticatedData: []byte{},
				Body:              ApplicationData{Data: []byte(text)},
			},
			CipherSuite:      cs,
			SenderDataSecret: sds,
			SecretTree:       sendTree,
			SigKey:           sig,
			GroupContext:     gc,
		})
		if err != nil {
			t.Fatal(err)
		}
		return pm
	}
	open := func(pm *PrivateMessage) error {
		_, err := Decrypt(pm, DecryptParams{
			CipherSuite:      cs,
			SenderDataSecret: sds,
			SecretTree:       recvTree,
			SigPubKey:        ciphersuite.NewMLSSignaturePublicKey(sig.PublicKey().AsSlice(), ciphersuite.ECDSA_SECP256R1_SHA256),
			GroupContext:     gc,
		})
		return err
	}

	// The forgery: a real frame whose sender data is re-sealed to name
	// generation 0xFFFFFFFF, as any member holding the epoch can.
	forged := frame("hello")
	sample := forged.Ciphertext[:cs.HashLength()]
	key, _ := sds.KdfExpandLabel("key", sample, cs.AEADKeyLength())
	nonce, _ := sds.KdfExpandLabel("nonce", sample, cs.AEADNonceLength())
	sd := &MLSSenderData{LeafIndex: 0, Generation: 0xFFFFFFFF}
	ct, err := ciphersuite.EncryptWithCipherSuite(key.AsSlice(), nonce.AsSlice(), sd.Marshal(),
		buildSenderDataAAD(forged.GroupID, forged.Epoch, forged.ContentType), cs)
	if err != nil {
		t.Fatal(err)
	}
	forged.EncryptedSenderData = ct

	start := time.Now()
	err = open(forged)
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("refusing the forged frame took %v", d)
	}
	if !errors.Is(err, secrettree.ErrGenerationTooFar) {
		t.Fatalf("forged frame: %v, want ErrGenerationTooFar", err)
	}
	// The sender's next real frame still opens.
	if err := open(frame("still here")); err != nil {
		t.Fatalf("after the forgery the tree no longer reads the sender: %v", err)
	}
}
