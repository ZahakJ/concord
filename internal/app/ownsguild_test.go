package app

import (
	"context"
	"testing"

	"github.com/ZahakJ/concord/internal/crypto/mls"
	"github.com/ZahakJ/concord/internal/domain"
)

// senderEngine answers CommitSender with a fixed credential; every other
// Engine method is left nil and must not be reached.
type senderEngine struct {
	mls.Engine
	cred []byte
}

func (e senderEngine) CommitSender(context.Context, mls.GroupID, []byte) ([]byte, error) {
	return e.cred, nil
}

// A Service whose identity is not loaded owns no guild and sent no commit,
// even when the guild has an owner and the commit has a sender. Both checks
// fail closed instead of dereferencing the missing identity.
func TestOwnsGuildAndOwnCommitFailClosedWithoutIdentity(t *testing.T) {
	owner := mustID(t)
	s := &Service{
		ctx:      context.Background(),
		mls:      senderEngine{cred: owner.PublicKey()},
		guilds:   map[string]*domain.Guild{"g1": {ID: "g1", OwnerID: owner.PublicKey()}},
		govState: map[string]GuildState{},
	}
	if s.effectiveOwner("g1") == "" {
		t.Fatal("setup: guild g1 has no effective owner, so the test would not reach the identity check")
	}
	if s.ownsGuild("g1") {
		t.Error("ownsGuild = true with no identity loaded")
	}
	if s.commitFromOwnAccount([]byte("g1"), []byte("commit")) {
		t.Error("commitFromOwnAccount = true with no identity loaded")
	}
}
