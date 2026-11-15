package group

// This file is a Concord-added patch (not part of upstream mls-go).

import "github.com/thomas-vilte/mls-go/treesync"

// TreeLeaf is one occupied leaf of the ratchet tree as it stands now: the
// credential and signature key are both read from the tree's own leaf node,
// so they always describe the same leaf (the member list records the
// credential seen when the leaf was added, which a later path may have
// changed).
type TreeLeaf struct {
	Index        LeafNodeIndex
	Credential   []byte // basic-credential identity bytes
	SignatureKey []byte
}

// TreeLeaves lists every occupied leaf of the current tree in index order.
func (g *Group) TreeLeaves() []TreeLeaf {
	if g == nil || g.ratchetTree == nil {
		return nil
	}
	var out []TreeLeaf
	for i := treesync.LeafIndex(0); i < treesync.LeafIndex(g.ratchetTree.NumLeaves); i++ {
		leaf := g.ratchetTree.GetLeaf(i)
		if leaf == nil || leaf.State != treesync.NodeStatePresent || leaf.LeafData == nil {
			continue
		}
		var id []byte
		if c := leaf.LeafData.Credential; c != nil {
			id = append([]byte(nil), c.Identity...)
		}
		out = append(out, TreeLeaf{
			Index:        LeafNodeIndex(i),
			Credential:   id,
			SignatureKey: append([]byte(nil), leaf.LeafData.SigKeyBytes()...),
		})
	}
	return out
}

// TreeLeafAt returns the occupied leaf at index, if there is one.
func (g *Group) TreeLeafAt(index LeafNodeIndex) (TreeLeaf, bool) {
	for _, l := range g.TreeLeaves() {
		if l.Index == index {
			return l, true
		}
	}
	return TreeLeaf{}, false
}
