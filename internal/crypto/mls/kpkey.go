package mls

import (
	"fmt"

	"github.com/thomas-vilte/mls-go/keypackages"
)

// VerifiedKeyPackageKey returns the signature key in a key package's leaf,
// after checking that the key package is signed by that key. A key package
// that passes was made by whoever holds the private half of the key, so a
// committer may treat a leaf already in its tree with the same key as that
// sender's own leaf.
//
// signatureKeyOf reads the key without this check, which is enough for
// refusing an add but not for removing anyone: anybody can copy a member's
// public key into a key package of their own.
func VerifiedKeyPackageKey(kp []byte) ([]byte, error) {
	parsed, err := keypackages.UnmarshalKeyPackage(kp)
	if err != nil {
		return nil, fmt.Errorf("mls: parse key package: %w", err)
	}
	if parsed.LeafNode == nil || len(parsed.LeafNode.SignatureKeyBytes) == 0 {
		return nil, fmt.Errorf("mls: key package has no leaf signature key")
	}
	if err := parsed.Verify(cipherSuite); err != nil {
		return nil, fmt.Errorf("mls: key package signature: %w", err)
	}
	return append([]byte(nil), parsed.LeafNode.SignatureKeyBytes...), nil
}
