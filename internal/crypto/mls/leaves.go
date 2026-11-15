package mls

import (
	"context"
	"errors"
	"fmt"

	upstream "github.com/thomas-vilte/mls-go"
	"github.com/thomas-vilte/mls-go/credentials"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
)

// Leaf is one occupied leaf of a group's current tree. Credential and Key are
// both read from the leaf node in the tree, so they always describe the same
// leaf. (Members reports the credential recorded when a leaf was added, which
// a later commit path can have changed; nothing that decides who a leaf
// belongs to should mix the two.)
type Leaf struct {
	Index      uint32
	Credential []byte
	Key        []byte // the leaf's signature public key
}

// ErrLeafChanged means the leaf at the index RemoveLeaf was given no longer
// holds the expected key: it was removed already, or the index was reused.
var ErrLeafChanged = upstream.ErrLeafChanged

// CommitLeaves is what a commit does to the tree's leaves, read from its
// public framing before it is applied.
type CommitLeaves struct {
	// Sender is the committing leaf's index (a member commit), or the index
	// the new leaf will take for an external commit (unknown: External is set
	// and Sender is meaningless).
	Sender   uint32
	External bool
	// Removed lists the leaf indexes the commit's Remove proposals blank.
	Removed []uint32
	// Added lists the leaves the commit's Add proposals seat. Index is not
	// known before the commit is applied and is left zero.
	Added []Leaf
	// Replaced lists leaf nodes the commit writes over an existing leaf: an
	// Update proposal, or the committer's own path leaf. Index is the leaf
	// they replace.
	Replaced []Leaf
	// ByReference is set when any proposal is carried by reference. Concord
	// never sends one, and it cannot be read without state we do not hold.
	ByReference bool
}

// leafEngine is the part of the engine that reads and changes single leaves.
// It is a separate interface so the app can reach it through a wrapped engine.
type leafEngine interface {
	Leaves(ctx context.Context, gid GroupID) ([]Leaf, error)
	LeafAt(ctx context.Context, gid GroupID, index uint32) (Leaf, bool, error)
	OwnLeaf(ctx context.Context, gid GroupID) (uint32, error)
	MemberLeaves(ctx context.Context, gid GroupID) (map[uint32][]byte, error)
	ReadCommitLeaves(ctx context.Context, gid GroupID, commit []byte) (*CommitLeaves, error)
	RemoveLeaf(ctx context.Context, gid GroupID, index uint32, expectedKey []byte) ([]byte, error)
}

var _ leafEngine = (*mlsEngine)(nil)

func toLeaf(l group.TreeLeaf) Leaf {
	return Leaf{Index: uint32(l.Index), Credential: l.Credential, Key: l.SignatureKey}
}

func (e *mlsEngine) Leaves(ctx context.Context, gid GroupID) ([]Leaf, error) {
	ls, err := e.c.TreeLeaves(ctx, gid)
	if err != nil {
		return nil, fmt.Errorf("mls: tree leaves: %w", err)
	}
	out := make([]Leaf, 0, len(ls))
	for _, l := range ls {
		out = append(out, toLeaf(l))
	}
	return out, nil
}

func (e *mlsEngine) LeafAt(ctx context.Context, gid GroupID, index uint32) (Leaf, bool, error) {
	l, ok, err := e.c.TreeLeafAt(ctx, gid, index)
	if err != nil {
		return Leaf{}, false, fmt.Errorf("mls: tree leaf: %w", err)
	}
	return toLeaf(l), ok, nil
}

// MemberLeaves maps each member's leaf index to the credential recorded for it
// when it was added (the identity Members and CommitSender report).
func (e *mlsEngine) MemberLeaves(ctx context.Context, gid GroupID) (map[uint32][]byte, error) {
	ms, err := e.c.ListMembers(ctx, gid)
	if err != nil {
		return nil, fmt.Errorf("mls: list members: %w", err)
	}
	out := make(map[uint32][]byte, len(ms))
	for _, m := range ms {
		out[m.LeafIndex] = append([]byte(nil), m.Identity...)
	}
	return out, nil
}

func (e *mlsEngine) OwnLeaf(ctx context.Context, gid GroupID) (uint32, error) {
	i, err := e.c.OwnLeafIndex(ctx, gid)
	if err != nil {
		return 0, fmt.Errorf("mls: own leaf: %w", err)
	}
	return i, nil
}

// RemoveLeaf commits the removal of the leaf at index, only if that leaf still
// holds expectedKey. It never removes this member's own leaf.
func (e *mlsEngine) RemoveLeaf(ctx context.Context, gid GroupID, index uint32, expectedKey []byte) ([]byte, error) {
	e.commitMu.Lock()
	defer e.commitMu.Unlock()
	e.discardOrphanProposals(ctx, gid)
	commit, err := e.c.RemoveLeaf(ctx, gid, index, expectedKey)
	if err != nil {
		if errors.Is(err, upstream.ErrLeafChanged) {
			return nil, ErrLeafChanged
		}
		return nil, fmt.Errorf("mls: remove leaf: %w", err)
	}
	return commit, nil
}

// ReadCommitLeaves parses a commit framed at the epoch we stand on. For a
// commit at any other epoch it returns (nil, nil): its leaf indexes belong to
// a tree we do not hold.
func (e *mlsEngine) ReadCommitLeaves(ctx context.Context, gid GroupID, commit []byte) (*CommitLeaves, error) {
	msg, err := framing.UnmarshalMLSMessage(commit)
	if err != nil {
		return nil, fmt.Errorf("mls: parse commit: %w", err)
	}
	pub, ok := msg.AsPublic()
	if !ok {
		return nil, fmt.Errorf("mls: commit is not a public message")
	}
	body, ok := pub.Content.Body.(framing.CommitBody)
	if !ok {
		return nil, fmt.Errorf("mls: message is not a commit")
	}
	epoch, err := e.c.Epoch(ctx, gid)
	if err != nil {
		return nil, fmt.Errorf("mls: epoch: %w", err)
	}
	if pub.Content.Epoch != epoch {
		return nil, nil
	}
	c, err := group.UnmarshalCommit(body.Data)
	if err != nil {
		return nil, fmt.Errorf("mls: parse commit body: %w", err)
	}
	out := &CommitLeaves{}
	switch pub.Content.Sender.Type {
	case framing.SenderTypeMember:
		out.Sender = pub.Content.Sender.LeafIndex
	case framing.SenderTypeNewMemberCommit:
		out.External = true
	default:
		return nil, fmt.Errorf("mls: commit sender type %v", pub.Content.Sender.Type)
	}
	for _, p := range c.Proposals {
		if p.Proposal == nil {
			out.ByReference = true
			continue
		}
		switch p.Proposal.Type {
		case group.ProposalTypeRemove:
			if p.Proposal.Remove != nil {
				out.Removed = append(out.Removed, uint32(p.Proposal.Remove.Removed))
			}
		case group.ProposalTypeAdd:
			if a := p.Proposal.Add; a != nil && a.KeyPackage != nil && a.KeyPackage.LeafNode != nil {
				ln := a.KeyPackage.LeafNode
				out.Added = append(out.Added, Leaf{Credential: basicIdentity(ln.Credential), Key: append([]byte(nil), ln.SignatureKeyBytes...)})
			}
		case group.ProposalTypeUpdate:
			if u := p.Proposal.Update; u != nil && u.LeafNode != nil {
				out.Replaced = append(out.Replaced, Leaf{Index: out.Sender, Credential: basicIdentity(u.LeafNode.Credential), Key: append([]byte(nil), u.LeafNode.SignatureKeyBytes...)})
			}
		}
	}
	if c.Path != nil && c.Path.LeafNode != nil {
		ln := c.Path.LeafNode
		l := Leaf{Index: out.Sender, Credential: basicIdentity(ln.Credential), Key: append([]byte(nil), ln.SigKeyBytes()...)}
		if out.External {
			out.Added = append(out.Added, l)
		} else {
			out.Replaced = append(out.Replaced, l)
		}
	}
	return out, nil
}

func basicIdentity(c *credentials.Credential) []byte {
	if c == nil {
		return nil
	}
	return append([]byte(nil), c.Identity...)
}
