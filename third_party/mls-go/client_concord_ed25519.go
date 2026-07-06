package mls

// This file is a Concord-added patch (not part of upstream mls-go v1.5.0).
//
// Upstream NewClient generates a random signature key per client and never
// reloads it from storage, so after a process restart a member signs with a
// different key than the one bound to its leaf, and peers reject its messages
// (send-after-restart is broken). This option lets the caller supply a
// deterministic Ed25519 signing key — mirroring the existing X.509 key-supply
// seam (WithX509Credential) — so the same key is used on every start and
// signing survives restarts. Intended to be contributed upstream.

import (
	"crypto/ed25519"

	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/credentials"
)

// WithEd25519SignatureKey configures a basic-credential client to sign with the
// supplied Ed25519 private key rather than a freshly generated one. identity is
// the basic-credential identity and must match NewClient's identity argument.
// The cipher suite must be Ed25519-based (CS1/CS3).
func WithEd25519SignatureKey(identity []byte, priv ed25519.PrivateKey) ClientOption {
	return func(cfg *clientConfig) {
		if cfg == nil || len(priv) != ed25519.PrivateKeySize {
			return
		}
		pub, ok := priv.Public().(ed25519.PublicKey)
		if !ok {
			return
		}
		cfg.ed25519Cred = &credentials.CredentialWithKey{
			Credential:        credentials.NewBasicCredential(identity),
			Ed25519PrivateKey: append(ed25519.PrivateKey(nil), priv...),
			SignatureKeyBytes: append([]byte(nil), pub...),
		}
		cfg.sigKey = ciphersuite.NewEd25519SignaturePrivateKey(priv)
	}
}
