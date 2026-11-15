package mls

// This file is a Concord-added patch (not part of upstream mls-go).

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/thomas-vilte/mls-go/group"
)

// ErrLeafChanged is returned by RemoveLeaf when the leaf at the given index no
// longer holds the expected signature key (it was removed, or the index now
// belongs to a different leaf).
var ErrLeafChanged = errors.New("mls: leaf at that index no longer holds that key")

// TreeLeaves lists the occupied leaves of the group's current tree, each with
// the credential and signature key its own leaf node carries.
func (c *Client) TreeLeaves(ctx context.Context, groupID []byte) ([]group.TreeLeaf, error) {
	g, unlock, err := c.lockedGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return g.TreeLeaves(), nil
}

// RemoveLeaf commits the removal of the leaf at index, provided that leaf
// still holds expectedKey. It returns the commit for the other members.
func (c *Client) RemoveLeaf(ctx context.Context, groupID []byte, index uint32, expectedKey []byte) ([]byte, error) {
	c.mu.Lock()
	if err := c.checkOpen(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	entry := c.getOrCreateEntryLocked(groupCacheKeyBytes(groupID))
	c.mu.Unlock()

	entry.mu.Lock()
	defer entry.mu.Unlock()

	g, err := c.loadGroupEntry(ctx, groupID, entry)
	if err != nil {
		return nil, err
	}
	key := g.MemberSigningKey(group.LeafNodeIndex(index))
	if len(key) == 0 || !bytes.Equal(key, expectedKey) {
		return nil, ErrLeafChanged
	}
	if group.LeafNodeIndex(index) == g.OwnLeafIndex() {
		return nil, fmt.Errorf("mls: refusing to remove this member's own leaf")
	}
	if _, err := g.RemoveMember(group.LeafNodeIndex(index)); err != nil {
		return nil, fmt.Errorf("creating remove proposal: %w", err)
	}
	return c.commitCurrentState(ctx, g, entry, "creating remove commit")
}

// lockedGroup loads a group under its entry lock; the caller must call unlock.
func (c *Client) lockedGroup(ctx context.Context, groupID []byte) (*group.Group, func(), error) {
	c.mu.Lock()
	if err := c.checkOpen(); err != nil {
		c.mu.Unlock()
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return nil, nil, err
	}
	entry := c.getOrCreateEntryLocked(groupCacheKeyBytes(groupID))
	c.mu.Unlock()
	entry.mu.Lock()
	g, err := c.loadGroupEntry(ctx, groupID, entry)
	if err != nil {
		entry.mu.Unlock()
		return nil, nil, err
	}
	return g, entry.mu.Unlock, nil
}

// TreeLeafAt returns the occupied leaf at index in the group's current tree.
func (c *Client) TreeLeafAt(ctx context.Context, groupID []byte, index uint32) (group.TreeLeaf, bool, error) {
	g, unlock, err := c.lockedGroup(ctx, groupID)
	if err != nil {
		return group.TreeLeaf{}, false, err
	}
	defer unlock()
	l, ok := g.TreeLeafAt(group.LeafNodeIndex(index))
	return l, ok, nil
}
