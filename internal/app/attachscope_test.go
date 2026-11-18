package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A blob is handed to a member of the guild whose message names it, and to
// nobody else: a peer in another guild of the same device, and a peer in no
// guild, get the answer for a blob that does not exist. A picture an import
// pinned goes to the members of the guild it was imported into, and a pin
// with no guild recorded goes to nobody.
func TestABlobIsHandedOnlyToMembersWhoseGuildKeepsIt(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	member := startService(t, ctx)
	g := guildWith(t, a, member)
	ch := g.Channels[0].ID

	otherGuildPeer := startService(t, ctx)
	g2, err := a.CreateGuild("another")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := a.InviteCode(g2.ID)
	if _, err := otherGuildPeer.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitFor(t, 30*time.Second, func() bool {
		return a.guildHasMember(g2.ID, otherGuildPeer.presence(otherGuildPeer.host.AddrInfo().ID).Fingerprint)
	}, "the second guild never seated its member")
	outsider := startService(t, ctx)
	for _, s := range []*Service{member, otherGuildPeer, outsider} {
		if err := s.host.Connect(ctx, a.host.AddrInfo()); err != nil {
			t.Fatalf("connect: %v", err)
		}
	}

	pic := strings.Repeat("7", 64)
	if err := a.store.SaveAttachment(pic, []byte("ciphertext of a guild picture")); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendMessage(ch, "a picture "+testBlobToken(pic), "", ""); err != nil {
		t.Fatal(err)
	}
	imported := strings.Repeat("8", 64)
	if err := a.store.SaveAttachment(imported, []byte("ciphertext of an imported picture")); err != nil {
		t.Fatal(err)
	}
	if err := a.store.PinImportedBlob(g.ID, imported); err != nil {
		t.Fatal(err)
	}
	older := strings.Repeat("6", 64)
	if err := a.store.SaveAttachment(older, []byte("ciphertext of an older imported picture")); err != nil {
		t.Fatal(err)
	}
	if err := a.store.PinAttachment(older, true); err != nil {
		t.Fatal(err)
	}

	aID := a.host.AddrInfo().ID
	ask := func(from *Service, id string) []byte {
		req, _ := json.Marshal(attachRequest{BlobID: id})
		resp, err := from.requestBlobFrom(aID, req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		return resp
	}
	nowhere := ask(outsider, strings.Repeat("9", 64)) // a blob nobody holds

	if got := ask(member, pic); !bytes.Equal(got, []byte("ciphertext of a guild picture")) {
		t.Fatalf("the guild's member did not get the picture: %q", got)
	}
	for name, s := range map[string]*Service{"a peer in another guild": otherGuildPeer, "a peer in no guild": outsider} {
		if got := ask(s, pic); len(got) != 0 || !bytes.Equal(got, nowhere) {
			t.Errorf("%s got %q for the guild's picture, where a missing blob answers %q", name, got, nowhere)
		}
	}
	if got := ask(member, imported); !bytes.Equal(got, []byte("ciphertext of an imported picture")) {
		t.Errorf("the guild's member did not get the guild's imported picture: %q", got)
	}
	for name, s := range map[string]*Service{"a peer in another guild": otherGuildPeer, "a peer in no guild": outsider} {
		if got := ask(s, imported); len(got) != 0 {
			t.Errorf("%s got the guild's imported picture: %q", name, got)
		}
	}
	for name, s := range map[string]*Service{"the guild's member": member, "a peer in another guild": otherGuildPeer} {
		if got := ask(s, older); len(got) != 0 {
			t.Errorf("%s got a picture pinned with no guild recorded: %q", name, got)
		}
	}
}
