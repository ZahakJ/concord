// Package mls is Concord's group end-to-end encryption engine (part of layer 1).
//
// It wraps the vetted pure-Go MLS implementation (github.com/thomas-vilte/mls-go,
// RFC 9420) behind a small Engine interface. MLS gives a Concord "guild" a
// continuously-ratcheted shared group key with forward secrecy and
// post-compromise security: every message is encrypted under the current group
// epoch, and adding/removing members rekeys the group. Ciphertext is what we
// publish over gossipsub, so relays and non-members learn nothing.
//
// Keeping the engine behind an interface is deliberate (see plan risk #1): if
// the upstream library ever needs replacing, only this package changes.
//
// Cipher suite: CS1 (MLS_128_DHKEMX25519_AES128GCM_SHA256_Ed25519). We pick the
// Ed25519 suite so the MLS credential identity matches Concord's Ed25519
// account keys.
package mls

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"log/slog"

	upstream "github.com/thomas-vilte/mls-go"
	"github.com/thomas-vilte/mls-go/ciphersuite"
	filestore "github.com/thomas-vilte/mls-go/storage/file"
)

// cipherSuite is the fixed suite for all Concord groups.
const cipherSuite = ciphersuite.MLS128DHKEMX25519

// quietLogger silences the upstream library's INFO-level chatter; Concord owns
// its own logging and must never let per-message epoch/identity details leak to
// stdout.
var quietLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// GroupID identifies an MLS group (a Concord guild).
type GroupID []byte

func (g GroupID) String() string { return fmt.Sprintf("%x", []byte(g)) }

// Message is a decrypted application message and the authenticated identity of
// the member who sent it. SenderID is that member's credential — in Concord,
// their Ed25519 account public key.
type Message struct {
	Plaintext []byte
	SenderID  []byte
}

// Engine is Concord's group-encryption abstraction. The MLS implementation is
// the default; an alternative engine (e.g. a simpler sender-keys scheme) could
// satisfy the same interface without touching callers.
type Engine interface {
	// KeyPackage returns a fresh MLS KeyPackage for this member to publish so
	// others can add them to a group.
	KeyPackage(ctx context.Context) ([]byte, error)

	// CreateGroup starts a new group owned by this member and returns its ID.
	CreateGroup(ctx context.Context) (GroupID, error)

	// Invite adds the member identified by memberKeyPackage. It returns a
	// commit (which every *existing* member other than the inviter must apply
	// via ApplyCommit) and a welcome (delivered to the new member, who calls
	// Join). The inviter's own state is advanced in place.
	Invite(ctx context.Context, gid GroupID, memberKeyPackage []byte) (commit, welcome []byte, err error)

	// Join enters a group from a welcome message and returns the group ID.
	Join(ctx context.Context, welcome []byte) (GroupID, error)

	// ApplyCommit advances an existing member to the epoch produced by a commit
	// from another member.
	ApplyCommit(ctx context.Context, gid GroupID, commit []byte) error

	// Remove evicts the member with the given credential, returning a commit
	// that every remaining member must apply. After it takes effect the removed
	// member can no longer decrypt new messages (post-compromise security).
	Remove(ctx context.Context, gid GroupID, memberCredential []byte) (commit []byte, err error)

	// Encrypt seals plaintext under the current group key.
	Encrypt(ctx context.Context, gid GroupID, plaintext []byte) ([]byte, error)

	// Decrypt opens a group ciphertext, returning the plaintext and sender.
	Decrypt(ctx context.Context, gid GroupID, ciphertext []byte) (*Message, error)

	// Members lists the credential identities currently in the group.
	Members(ctx context.Context, gid GroupID) ([][]byte, error)

	// Close releases engine resources.
	Close() error
}

// mlsEngine is the mls-go-backed implementation of Engine.
type mlsEngine struct {
	c *upstream.Client
}

// New creates an in-memory MLS engine. credential is the member's identity (in
// Concord, its Ed25519 account public key); signingKey is the deterministic
// Ed25519 key used to sign MLS messages — supplying it (rather than letting the
// library generate a random one) is what lets a restarted member keep signing.
// Group state is lost on exit; use NewPersistent for durability.
func New(credential []byte, signingKey ed25519.PrivateKey) (Engine, error) {
	return newEngine(credential, signingKey, "")
}

// NewPersistent creates an MLS engine backed by on-disk storage under dir. With
// a deterministic signingKey, a restarted member recovers both its group state
// (from disk) and its signing key (re-supplied), so it can receive AND send.
func NewPersistent(credential []byte, signingKey ed25519.PrivateKey, dir string) (Engine, error) {
	return newEngine(credential, signingKey, dir)
}

func newEngine(credential []byte, signingKey ed25519.PrivateKey, dir string) (Engine, error) {
	if len(credential) == 0 {
		return nil, fmt.Errorf("mls: credential must not be empty")
	}
	if len(signingKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("mls: signingKey must be a %d-byte ed25519 key", ed25519.PrivateKeySize)
	}
	opts := []upstream.ClientOption{
		upstream.WithLogger(quietLogger),
		// Deterministic signing key: reproduced identically on every start, so
		// signing survives restarts and binds the MLS leaf to the account key.
		upstream.WithEd25519SignatureKey(credential, signingKey),
	}
	if dir != "" {
		fs, err := filestore.NewStore(dir)
		if err != nil {
			return nil, fmt.Errorf("mls: open storage %q: %w", dir, err)
		}
		opts = append(opts, upstream.WithStorage(fs, fs))
	}
	c, err := upstream.NewClient(credential, cipherSuite, opts...)
	if err != nil {
		return nil, fmt.Errorf("mls: new client: %w", err)
	}
	return &mlsEngine{c: c}, nil
}

func (e *mlsEngine) KeyPackage(ctx context.Context) ([]byte, error) {
	return e.c.FreshKeyPackageBytes(ctx)
}

func (e *mlsEngine) CreateGroup(ctx context.Context) (GroupID, error) {
	gid, err := e.c.CreateGroup(ctx)
	if err != nil {
		return nil, fmt.Errorf("mls: create group: %w", err)
	}
	return GroupID(gid), nil
}

func (e *mlsEngine) Invite(ctx context.Context, gid GroupID, kp []byte) (commit, welcome []byte, err error) {
	commit, welcome, err = e.c.InviteMember(ctx, gid, kp)
	if err != nil {
		return nil, nil, fmt.Errorf("mls: invite member: %w", err)
	}
	return commit, welcome, nil
}

func (e *mlsEngine) Join(ctx context.Context, welcome []byte) (GroupID, error) {
	gid, err := e.c.JoinGroup(ctx, welcome)
	if err != nil {
		return nil, fmt.Errorf("mls: join group: %w", err)
	}
	return GroupID(gid), nil
}

func (e *mlsEngine) ApplyCommit(ctx context.Context, gid GroupID, commit []byte) error {
	if err := e.c.ProcessCommit(ctx, gid, commit); err != nil {
		return fmt.Errorf("mls: process commit: %w", err)
	}
	return nil
}

func (e *mlsEngine) Remove(ctx context.Context, gid GroupID, memberCredential []byte) ([]byte, error) {
	commit, err := e.c.RemoveMember(ctx, gid, memberCredential)
	if err != nil {
		return nil, fmt.Errorf("mls: remove member: %w", err)
	}
	return commit, nil
}

func (e *mlsEngine) Encrypt(ctx context.Context, gid GroupID, plaintext []byte) ([]byte, error) {
	ct, err := e.c.SendMessage(ctx, gid, plaintext)
	if err != nil {
		return nil, fmt.Errorf("mls: encrypt: %w", err)
	}
	return ct, nil
}

func (e *mlsEngine) Decrypt(ctx context.Context, gid GroupID, ciphertext []byte) (*Message, error) {
	rm, err := e.c.ReceiveMessage(ctx, gid, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("mls: decrypt: %w", err)
	}
	return &Message{Plaintext: rm.Plaintext, SenderID: rm.SenderIdentity}, nil
}

func (e *mlsEngine) Members(ctx context.Context, gid GroupID) ([][]byte, error) {
	members, err := e.c.ListMembers(ctx, gid)
	if err != nil {
		return nil, fmt.Errorf("mls: list members: %w", err)
	}
	out := make([][]byte, 0, len(members))
	for _, m := range members {
		out = append(out, m.Identity)
	}
	return out, nil
}

func (e *mlsEngine) Close() error { return e.c.Close() }
