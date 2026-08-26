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
	"sync"

	upstream "github.com/thomas-vilte/mls-go"
	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/keypackages"
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

	// AddMembers admits several key packages in ONE epoch: a single commit for
	// every existing member to apply, and a single welcome addressed to all the
	// joiners at once (each finds its own secret in it). accepted holds the
	// indexes of the key packages that made it in, in the order given.
	//
	// A key package that cannot be admitted — malformed, expired, or belonging
	// to a leaf that is still in the tree — is DROPPED rather than allowed to
	// fail the batch, which is what makes a queue of joiners safe to commit
	// together. err is returned only when nothing could be admitted or the
	// commit itself failed; in both cases the group is left exactly as it was.
	AddMembers(ctx context.Context, gid GroupID, memberKeyPackages [][]byte) (commit, welcome []byte, accepted []int, err error)

	// Join enters a group from a welcome message and returns the group ID.
	Join(ctx context.Context, welcome []byte) (GroupID, error)

	// ApplyCommit advances an existing member to the epoch produced by a commit
	// from another member.
	ApplyCommit(ctx context.Context, gid GroupID, commit []byte) error

	// CommitSender returns the credential (Concord account public key) of the
	// member who authored a commit, read from the commit's public MLS framing
	// WITHOUT applying it. Callers gate membership changes on the committer's
	// authority before advancing the group — the commit is signed by this
	// member's leaf, so the identity is cryptographically bound. Errors if the
	// commit is not a member-authored public message or the sender leaf is not a
	// current member (e.g. a commit for an epoch we haven't reached).
	CommitSender(ctx context.Context, gid GroupID, commit []byte) ([]byte, error)

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

	// Epoch returns the group's current MLS epoch. Commits advance the epoch by
	// one and must be applied gaplessly, so comparing epochs tells two members
	// exactly which commits one of them is missing (the basis of history sync's
	// commit backfill).
	Epoch(ctx context.Context, gid GroupID) (uint64, error)

	// Close releases engine resources.
	Close() error
}

// mlsEngine is the mls-go-backed implementation of Engine.
type mlsEngine struct {
	c *upstream.Client
	// commitMu serializes every operation that MINTS a commit. The upstream
	// client already serializes each of its own calls per group, which is
	// enough while one call is one whole commit — but AddMembers is several
	// calls (stage each add, then commit them), and the upstream commit path
	// sweeps up EVERY stored proposal. A Remove landing between two of those
	// calls would carry the half-staged adds out on its own commit and return
	// no welcome for them: members added to the tree that nobody can reach,
	// forever. This mutex is what makes the staged window indivisible.
	commitMu sync.Mutex
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
	e.commitMu.Lock()
	defer e.commitMu.Unlock()
	e.discardOrphanProposals(ctx, gid)
	commit, welcome, err = e.c.InviteMember(ctx, gid, kp)
	if err != nil {
		return nil, nil, fmt.Errorf("mls: invite member: %w", err)
	}
	return commit, welcome, nil
}

// maxBatchAdds caps how many joiners one commit may carry. The ceiling is not
// MLS's — a commit may hold any number of Add proposals — it is the welcome's:
// one welcome carries the ratchet tree once plus a per-joiner encrypted secret,
// and every one of those joiners downloads the whole thing. Past a few dozen
// the batch stops paying for itself and starts making a single response large
// enough to matter on a phone. Callers hand over what they have; the surplus
// simply rides the next commit.
const maxBatchAdds = 32

func (e *mlsEngine) AddMembers(ctx context.Context, gid GroupID, kps [][]byte) (commit, welcome []byte, accepted []int, err error) {
	if len(kps) == 0 {
		return nil, nil, nil, fmt.Errorf("mls: add members: no key packages")
	}
	e.commitMu.Lock()
	defer e.commitMu.Unlock()
	e.discardOrphanProposals(ctx, gid)

	// Pre-flight, rather than staging everything and letting the commit fail.
	// The upstream proposal filter rejects an Add whose signature key already
	// sits in the tree (RFC 9420 ValSem101) — which is exactly what a joiner
	// retrying with a leaf we never cleaned up looks like — and it does so at
	// COMMIT time, where one bad joiner would take the whole queue down with
	// it. Checking here turns that into a dropped joiner, which the caller can
	// then repair on its own (evict the stale leaf, ask again) without the
	// others paying for it.
	inTree := make(map[string]bool)
	if members, mErr := e.c.ListMembers(ctx, gid); mErr == nil {
		for _, m := range members {
			if len(m.SigningKey) > 0 {
				inTree[string(m.SigningKey)] = true
			}
		}
	}
	staged := make([]int, 0, len(kps))
	for i, kp := range kps {
		if len(staged) >= maxBatchAdds {
			break
		}
		sig, sErr := signatureKeyOf(kp)
		if sErr != nil || inTree[string(sig)] {
			continue // unparseable, or a leaf (or an earlier entry in this very
			// batch) already holding that signature key
		}
		if _, pErr := e.c.ProposeAddMember(ctx, gid, kp); pErr != nil {
			continue
		}
		inTree[string(sig)] = true
		staged = append(staged, i)
	}
	if len(staged) == 0 {
		e.discardOrphanProposals(ctx, gid)
		return nil, nil, nil, fmt.Errorf("mls: add members: no admissible key packages")
	}

	commit, welcome, err = e.c.CommitPendingProposals(ctx, gid)
	if err != nil {
		// Nothing was merged (the upstream commit validates before it mutates),
		// so dropping the proposals returns the group to where it started and
		// leaves the caller free to fall back to one-at-a-time invites.
		e.discardOrphanProposals(ctx, gid)
		return nil, nil, nil, fmt.Errorf("mls: commit pending adds: %w", err)
	}
	if len(welcome) == 0 {
		// Adds without a welcome would seat members nobody can reach. The
		// upstream path cannot produce this for a commit that carried Adds; the
		// check is here because the failure would be silent and permanent.
		return nil, nil, nil, fmt.Errorf("mls: commit of %d adds produced no welcome", len(staged))
	}
	return commit, welcome, staged, nil
}

// signatureKeyOf reads the signature key out of a key package's leaf, the
// identity the RFC's uniqueness rule is enforced against.
func signatureKeyOf(kp []byte) ([]byte, error) {
	parsed, err := keypackages.UnmarshalKeyPackage(kp)
	if err != nil {
		return nil, err
	}
	if parsed.LeafNode == nil || len(parsed.LeafNode.SignatureKeyBytes) == 0 {
		return nil, fmt.Errorf("mls: key package has no leaf signature key")
	}
	return parsed.LeafNode.SignatureKeyBytes, nil
}

// discardOrphanProposals clears anything left staged on a group before we mint
// a commit. Concord never leaves a proposal staged across a call — AddMembers
// commits or discards under commitMu, and no other path stages one — so
// anything found here survived a crash mid-batch, persisted with the group
// state. It cannot be committed safely: an orphaned Add would ride out on the
// next commit (a kick, a self-update) and seat a member with no welcome behind
// it. Best-effort: a failure here just means the commit below fails too.
func (e *mlsEngine) discardOrphanProposals(ctx context.Context, gid GroupID) {
	_ = e.c.CancelPendingProposals(ctx, gid)
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

func (e *mlsEngine) CommitSender(ctx context.Context, gid GroupID, commit []byte) ([]byte, error) {
	msg, err := framing.UnmarshalMLSMessage(commit)
	if err != nil {
		return nil, fmt.Errorf("mls: parse commit: %w", err)
	}
	// Concord frames commits as PublicMessages (see commitCurrentState upstream),
	// so the sender leaf index is in the clear — no group secrets required.
	pub, ok := msg.AsPublic()
	if !ok {
		return nil, fmt.Errorf("mls: commit is not a public message")
	}
	if pub.Content.Sender.Type != framing.SenderTypeMember {
		return nil, fmt.Errorf("mls: commit sender is not a group member")
	}
	leaf := pub.Content.Sender.LeafIndex
	members, err := e.c.ListMembers(ctx, gid)
	if err != nil {
		return nil, fmt.Errorf("mls: list members: %w", err)
	}
	for _, m := range members {
		if m.LeafIndex == leaf {
			return append([]byte(nil), m.Identity...), nil
		}
	}
	return nil, fmt.Errorf("mls: commit sender leaf %d is not a current member", leaf)
}

func (e *mlsEngine) Remove(ctx context.Context, gid GroupID, memberCredential []byte) ([]byte, error) {
	e.commitMu.Lock()
	defer e.commitMu.Unlock()
	e.discardOrphanProposals(ctx, gid)
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

func (e *mlsEngine) Epoch(ctx context.Context, gid GroupID) (uint64, error) {
	epoch, err := e.c.Epoch(ctx, gid)
	if err != nil {
		return 0, fmt.Errorf("mls: epoch: %w", err)
	}
	return epoch, nil
}

func (e *mlsEngine) Close() error { return e.c.Close() }
