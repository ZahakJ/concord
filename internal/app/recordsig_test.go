package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
)

// signedMessage builds a message signed by its author exactly the way sendAs
// does, so a divergence between the two shows up here rather than as "history
// stopped syncing" on somebody's laptop.
func signedMessage(id *identity.Identity, channelID, content string) domain.Message {
	m := domain.Message{
		ID:        domain.NewID(),
		ChannelID: channelID,
		Sender:    id.PublicKey(),
		Name:      "rudaki",
		Content:   content,
		Sent:      time.Now().UTC(),
	}
	m.Sig = id.Sign(messageSigningBytes(m))
	return m
}

func TestMessageSignVerifyRoundTrip(t *testing.T) {
	author := mustID(t)
	m := signedMessage(author, "ch1", "the wine is poured")

	if !verifyMessageSig(m) {
		t.Fatal("a message signed by its author must verify")
	}

	// Every field inside the projection is load-bearing. If any of these passes,
	// that field is forgeable by whoever relays the message.
	for name, mutate := range map[string]func(*domain.Message){
		"body":      func(x *domain.Message) { x.Content = "the wine is spilt" },
		"id":        func(x *domain.Message) { x.ID = domain.NewID() },
		"channel":   func(x *domain.Message) { x.ChannelID = "ch2" },
		"name":      func(x *domain.Message) { x.Name = "somebody else" },
		"kind":      func(x *domain.Message) { x.Kind = "system" },
		"replyTo":   func(x *domain.Message) { x.ReplyTo = domain.NewID() },
		"dir":       func(x *domain.Message) { x.Dir = "rtl" },
		"timestamp": func(x *domain.Message) { x.Sent = x.Sent.Add(time.Second) },
		"sender":    func(x *domain.Message) { x.Sender = mustID(t).PublicKey() },
	} {
		tampered := m
		mutate(&tampered)
		if verifyMessageSig(tampered) {
			t.Fatalf("tampering with the %s must break the signature", name)
		}
	}

	// A signature made by the wrong key over the right bytes is not a signature.
	wrong := m
	wrong.Sig = mustID(t).Sign(messageSigningBytes(m))
	if verifyMessageSig(wrong) {
		t.Fatal("a signature by a key other than Sender must not verify")
	}
}

// TestForgedAuthorMessageRejected is the §13 shape itself: a member serving
// history hands over a message with somebody else's name on it.
func TestForgedAuthorMessageRejected(t *testing.T) {
	alice := mustID(t)
	mallory := mustID(t)

	forged := signedMessage(mallory, "ch1", "I resign, effective immediately")
	// Mallory swaps in Alice's key as the sender and keeps her own signature —
	// the only two moves available to somebody who does not hold Alice's key.
	forged.Sender = alice.PublicKey()
	if verifyMessageSig(forged) {
		t.Fatal("a message claiming Alice's key must not verify under Mallory's signature")
	}
	// …and re-signing it does not help, because the signature is checked against
	// the key the message names.
	forged.Sig = mallory.Sign(messageSigningBytes(forged))
	if verifyMessageSig(forged) {
		t.Fatal("re-signing a forgery with the forger's key must still fail")
	}

	before := refusedMessages.count()
	if messageAttestation(&forged) {
		t.Fatal("a backfilled message whose signature does not verify must be refused")
	}
	refusedMessages.note(1, "test", "test")
	if refusedMessages.count() != before+1 {
		t.Fatal("a refusal must be counted, not swallowed")
	}
}

// TestMessageSignatureIsChannelBound covers replay across contexts: a message
// lifted out of one channel and served into another.
func TestMessageSignatureIsChannelBound(t *testing.T) {
	author := mustID(t)
	m := signedMessage(author, "ch-private", "the meeting is at three")

	replayed := m
	replayed.ChannelID = "ch-public"
	if verifyMessageSig(replayed) {
		t.Fatal("a message replayed into another channel must not verify")
	}
	if messageAttestation(&replayed) {
		t.Fatal("a cross-channel replay must be refused on ingest")
	}
}

// TestMessageAttestationGrandfathersUnsigned pins the compatibility decision:
// an unsigned backfilled row is KEPT and MARKED, never destroyed. Getting this
// wrong deletes the entire pre-signature history of every guild.
func TestMessageAttestationGrandfathersUnsigned(t *testing.T) {
	old := domain.Message{
		ID: domain.NewID(), ChannelID: "ch1", Sender: mustID(t).PublicKey(),
		Content: "written before signatures existed", Sent: time.Now().UTC(),
	}
	if !messageAttestation(&old) {
		t.Fatal("an unsigned backfilled message must be kept, not refused")
	}
	if !old.Unverified {
		t.Fatal("an unsigned backfilled message must be marked unverified")
	}

	// A signed one is kept and NOT marked — that is the whole difference the UI
	// draws.
	signed := signedMessage(mustID(t), "ch1", "written after")
	if !messageAttestation(&signed) || signed.Unverified {
		t.Fatal("a verified message must be kept and unmarked")
	}

	// A peer cannot smear a message by asserting the flag: it is recomputed on
	// every ingest, never adopted.
	liar := signedMessage(mustID(t), "ch1", "still fine")
	liar.Unverified = true
	if !messageAttestation(&liar) || liar.Unverified {
		t.Fatal("the unverified flag must be recomputed, not taken from the wire")
	}
}

// TestMessageAttestationClearsTombstones: a deleted row's body is gone, so its
// old signature covers nothing. Carrying it on would make every tombstone fail
// verification at the next peer, which is how deletes stop propagating.
func TestMessageAttestationClearsTombstones(t *testing.T) {
	m := signedMessage(mustID(t), "ch1", "regretted")
	m.Content = ""
	m.Deleted = true
	if !messageAttestation(&m) {
		t.Fatal("a tombstone must be accepted")
	}
	if len(m.Sig) != 0 {
		t.Fatal("a tombstone must not carry a signature over a body that is gone")
	}
	if m.Unverified {
		t.Fatal("a tombstone has no authorship claim to qualify")
	}
}

// TestEditReSignatureKeepsRowVerifiable covers the one mutation that changes
// what a signature covers. Without the author's re-signature an edited message
// would arrive at every catch-up peer carrying a signature over text that no
// longer exists — and be refused.
func TestEditReSignatureKeepsRowVerifiable(t *testing.T) {
	author := mustID(t)
	row := signedMessage(author, "ch1", "meet at eight")

	edited := row
	edited.Content = "meet at nine"
	edited.Edited = true
	if verifyMessageSig(edited) {
		t.Fatal("the original signature must not still cover the edited body")
	}

	// What the edit action carries, and what applyEdit checks.
	edited.Sig = author.Sign(messageRowSigningBytes(row, "meet at nine"))
	if !verifyMessageSig(edited) {
		t.Fatal("the author's re-signature must cover the edited row")
	}
	if !messageAttestation(&edited) || edited.Unverified {
		t.Fatal("an edited row carrying its re-signature must ingest clean")
	}

	// Somebody else's re-signature is worth nothing: the projection names the
	// original sender, so a stranger's key cannot produce bytes that verify.
	hijacked := row
	hijacked.Content = "meet at midnight"
	hijacked.Sig = mustID(t).Sign(messageRowSigningBytes(row, "meet at midnight"))
	if verifyMessageSig(hijacked) {
		t.Fatal("a third party must not be able to re-sign somebody's message")
	}
}

// TestApplyEditRefusesABogusReSignature: the edit still applies (it arrived
// authenticated on a lane that proved who sent it), but the row must not be left
// carrying a signature that does not check out. A stored signature is one that
// verified — every peer downstream depends on that, because a signature that
// fails there is a refusal, not a shrug.
func TestApplyEditRefusesABogusReSignature(t *testing.T) {
	author := mustID(t)
	svc := storyTestService(t)
	row := signedMessage(author, "ch1", "meet at eight")
	if _, err := svc.store.SaveMessage(row); err != nil {
		t.Fatalf("SaveMessage: %v", err)
	}

	// A re-signature by somebody else's key — what a tampering relay produces.
	bogus := mustID(t).Sign(messageRowSigningBytes(row, "meet at midnight"))
	svc.applyEdit(row.ID, "meet at midnight", author.PublicKey(), bogus)

	got, ok, err := svc.store.MessageByID(row.ID)
	if err != nil || !ok {
		t.Fatalf("MessageByID: %v %v", err, ok)
	}
	if got.Content != "meet at midnight" {
		t.Fatal("the edit itself must still apply")
	}
	if len(got.Sig) != 0 {
		t.Fatal("a re-signature that does not verify must not be stored")
	}

	// The author's own good re-signature is kept, and the row stays verifiable.
	good := author.Sign(messageRowSigningBytes(row, "meet at nine"))
	svc.applyEdit(row.ID, "meet at nine", author.PublicKey(), good)
	got, ok, err = svc.store.MessageByID(row.ID)
	if err != nil || !ok || len(got.Sig) == 0 || !verifyMessageSig(got) {
		t.Fatalf("the author's own re-signature must be kept and verify: %v %v %+v", err, ok, got)
	}
}

// TestPackSignatureBindsToItsGuild is the cross-context replay the pack rows
// need: an admin's record from one guild, served into another.
func TestPackSignatureBindsToItsGuild(t *testing.T) {
	admin := mustID(t)
	gif := GuildGif{
		ID:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Name: "shrug", Tags: []string{"reaction"}, Keys: "AAAA", Subtype: "gif",
		Width: 100, Height: 100, Author: admin.PublicKey(),
	}
	gif.Sig = admin.Sign(gifSigningBytes("guild-a", gif))

	if !verifyPackSig(gif.Author, gif.Sig, gifSigningBytes("guild-a", gif)) {
		t.Fatal("a record signed for this guild must verify in it")
	}
	if verifyPackSig(gif.Author, gif.Sig, gifSigningBytes("guild-b", gif)) {
		t.Fatal("a record signed for one guild must not verify in another")
	}
	// Every field a client renders or resolves is under the signature.
	for name, mutate := range map[string]func(*GuildGif){
		"name":    func(x *GuildGif) { x.Name = "not shrug" },
		"tags":    func(x *GuildGif) { x.Tags = []string{"nsfw"} },
		"keys":    func(x *GuildGif) { x.Keys = "BBBB" },
		"blob id": func(x *GuildGif) { x.ID = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" },
		"subtype": func(x *GuildGif) { x.Subtype = "png" },
	} {
		tampered := gif
		mutate(&tampered)
		if verifyPackSig(tampered.Author, tampered.Sig, gifSigningBytes("guild-a", tampered)) {
			t.Fatalf("tampering with the %s must break a pack signature", name)
		}
	}

	em := domain.CustomEmoji{
		Name: "blobcat", Image: "data:image/png;base64,AAAA", Author: admin.PublicKey(),
	}
	em.Sig = admin.Sign(emojiSigningBytes("guild-a", em))
	if !verifyPackSig(em.Author, em.Sig, emojiSigningBytes("guild-a", em)) {
		t.Fatal("an emoji signed for this guild must verify in it")
	}
	if verifyPackSig(em.Author, em.Sig, emojiSigningBytes("guild-b", em)) {
		t.Fatal("an emoji signed for one guild must not verify in another")
	}
	swapped := em
	swapped.Image = "data:image/png;base64,BBBB"
	if verifyPackSig(swapped.Author, swapped.Sig, emojiSigningBytes("guild-a", swapped)) {
		t.Fatal("replacing an emoji's image must break its signature")
	}
}

// TestRelayedMessageSurvivesTheStore is the property the whole feature rests on,
// tested without a network: a message written by A, round-tripped through B's
// database and out again the way history sync serves it, still proves A's
// authorship at a peer that has never met A and cannot ask anyone.
func TestRelayedMessageSurvivesTheStore(t *testing.T) {
	author := mustID(t)
	relay := storyTestService(t) // B: holds a copy, attests nothing
	receiver := storyTestService(t)

	original := signedMessage(author, "ch1", "a message with an author")
	if _, err := relay.store.SaveMessage(original); err != nil {
		t.Fatalf("relay SaveMessage: %v", err)
	}

	// Exactly what handleSyncRequest reads back to build a payload.
	served, err := relay.store.MessagesChangedSince("ch1", 0, 200)
	if err != nil || len(served) != 1 {
		t.Fatalf("MessagesChangedSince: %v (%d rows)", err, len(served))
	}
	m := served[0]
	if len(m.Sig) == 0 {
		t.Fatal("the relay must serve the author's signature onward, or the chain breaks here")
	}
	if !messageAttestation(&m) || m.Unverified {
		t.Fatal("a relayed signed message must ingest as verified")
	}
	if _, err := receiver.store.UpsertSyncedMessage(m, "self", false); err != nil {
		t.Fatalf("receiver UpsertSyncedMessage: %v", err)
	}

	// And it is still verifiable after landing in the receiver's own store.
	got, err := receiver.store.Messages("ch1", 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("receiver Messages: %v (%d rows)", err, len(got))
	}
	if got[0].Unverified {
		t.Fatal("a verified row must not be stored as unverified")
	}
	if !verifyMessageSig(got[0]) {
		t.Fatal("the signature must still verify after a full store round trip")
	}

	// The same journey with the signature stripped — what an older peer, or an
	// attacker downgrading, actually produces — lands as marked, not as A.
	stripped := served[0]
	stripped.ID = domain.NewID()
	stripped.Sig = nil
	if !messageAttestation(&stripped) || !stripped.Unverified {
		t.Fatal("an unsigned relayed message must be kept and marked")
	}
	if _, err := receiver.store.UpsertSyncedMessage(stripped, "self", false); err != nil {
		t.Fatalf("receiver UpsertSyncedMessage: %v", err)
	}
	rows, err := receiver.store.Messages("ch1", 0)
	if err != nil {
		t.Fatalf("receiver Messages: %v", err)
	}
	var marked int
	for _, r := range rows {
		if r.Unverified {
			marked++
		}
	}
	if marked != 1 {
		t.Fatalf("exactly the unsigned row must carry the mark, got %d of %d", marked, len(rows))
	}
}

// TestSignedMessageReachesAPeerAndSurvivesRelay drives the real stack: A writes,
// B receives it over gossip, and the row B would hand to a third peer still
// carries A's signature and verifies against A's key with A shut down.
func TestSignedMessageReachesAPeerAndSurvivesRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	rb := &recorder{}
	b.OnMessage(rb.add)

	g, err := a.CreateGuild("signed-history")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)
	sendUntilReceived(t, a, channel, "signed-on-the-wire", rb)

	aKey := a.PublicKey()
	// A leaves the network entirely. Everything after this is B's disk and a
	// stranger's arithmetic.
	if err := a.Close(); err != nil {
		t.Fatalf("A Close: %v", err)
	}

	served, err := b.store.MessagesChangedSince(channel, 0, 200)
	if err != nil {
		t.Fatalf("B MessagesChangedSince: %v", err)
	}
	var found bool
	for _, m := range served {
		if m.Content != "signed-on-the-wire" {
			continue
		}
		found = true
		if len(m.Sig) == 0 {
			t.Fatal("B must relay A's signature onward")
		}
		if !verifyMessageSig(m) {
			t.Fatal("A's signature must verify on the row B would serve")
		}
		if string(m.Sender) != string(aKey) {
			t.Fatal("the row must name A's account key as its sender")
		}
		// C's side of the transaction, with A offline and no roster consulted.
		c := storyTestService(t)
		if !messageAttestation(&m) || m.Unverified {
			t.Fatal("a third peer must accept A's message as verified")
		}
		if _, err := c.store.UpsertSyncedMessage(m, "self", false); err != nil {
			t.Fatalf("C UpsertSyncedMessage: %v", err)
		}
		got, err := c.store.Messages(channel, 0)
		if err != nil || len(got) != 1 || got[0].Unverified || !verifyMessageSig(got[0]) {
			t.Fatalf("C must hold A's message as verified: %v %+v", err, got)
		}
	}
	if !found {
		t.Fatal("B never stored A's message")
	}
}

// TestEditedMessageStaysVerifiableAtAPeer drives the re-signature through the
// real send/receive plumbing. It is the case that would have quietly stopped
// every edited message from ever reaching a peer that was offline for it.
func TestEditedMessageStaysVerifiableAtAPeer(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("edit-resign")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)
	sent, err := a.SendMessage(channel, "meet at eight", "", "")
	if err != nil {
		t.Fatalf("A SendMessage: %v", err)
	}
	// B must hold the original before the edit, or applyEdit has nothing to
	// re-sign against and the test proves nothing.
	waitFor(t, 20*time.Second, func() bool {
		row, ok, err := b.store.MessageByID(sent.ID)
		return err == nil && ok && row.Content == "meet at eight"
	}, "B never received the original message")

	if err := a.EditMessage(channel, sent.ID, "meet at nine"); err != nil {
		t.Fatalf("A EditMessage: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		row, ok, err := b.store.MessageByID(sent.ID)
		if err == nil && ok && row.Content == "meet at nine" {
			if len(row.Sig) == 0 {
				t.Fatal("B's copy of the edited row lost its signature")
			}
			if !verifyMessageSig(row) {
				t.Fatal("B's copy of the edited row must still verify against A's key")
			}
			// And A's own copy, which took the same path through applyEdit.
			mine, ok, err := a.store.MessageByID(sent.ID)
			if err != nil || !ok || !verifyMessageSig(mine) {
				t.Fatalf("A's own edited row must verify: %v %v", err, ok)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("B never applied the edit")
}

// TestLiveMessageWithBadSignatureIsDropped proves the live lane fails closed
// too. MLS authenticates the frame, so this cannot be a forgery by an outsider —
// it is a member whose own client produced a signature that does not match what
// it sent, which is either tampering or a bug, and neither belongs in the store
// where history sync would re-serve it as ours-attested.
func TestLiveMessageWithBadSignatureIsDropped(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)

	g, err := a.CreateGuild("live-gate")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)

	b.mu.RLock()
	groupID := b.guilds[g.ID].GroupID
	b.mu.RUnlock()

	deliver := func(m domain.Message) {
		t.Helper()
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		ct, err := a.mls.Encrypt(ctx, groupID, raw)
		if err != nil {
			t.Fatalf("A encrypt: %v", err)
		}
		if !b.deliverCiphertext(groupID, ct) {
			t.Fatal("B could not decrypt a frame A encrypted at the shared epoch")
		}
	}

	// The control: a properly signed message lands.
	good := signedMessage(a.id, channel, "properly signed")
	good.Sender = a.PublicKey()
	good.Sig = a.signMessage(good)
	deliver(good)

	// The same message with one character changed and the old signature kept.
	bad := good
	bad.ID = domain.NewID()
	bad.Content = "properly signed?"
	deliver(bad)

	msgs, err := b.Messages(channel, 0)
	if err != nil {
		t.Fatalf("B Messages: %v", err)
	}
	var sawGood, sawBad bool
	for _, m := range msgs {
		switch m.Content {
		case "properly signed":
			sawGood = true
		case "properly signed?":
			sawBad = true
		}
	}
	if !sawGood {
		t.Fatal("a correctly signed live message must be stored")
	}
	if sawBad {
		t.Fatal("a live message whose signature does not verify must be dropped")
	}
}

// TestLegacyPackRecordsAreAdoptedByAnAdmin: the sharp edge of failing closed on
// unsigned pack records is that a guild's existing emoji would quietly stop
// reaching new members. An admin re-signs their own copies at launch; a member
// without the permission leaves theirs alone, because they would be signing a
// claim they are not entitled to make.
func TestLegacyPackRecordsAreAdoptedByAnAdmin(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)

	g, err := a.CreateGuild("legacy-pack")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)

	// Exactly what an older build left on disk: a row with no author and no
	// signature, written straight past the add path.
	legacy := store.CustomEmojiRow{GuildID: g.ID, Name: "ancient", Image: "data:image/png;base64,AAAA"}
	if err := a.store.SaveCustomEmoji(legacy); err != nil {
		t.Fatalf("A SaveCustomEmoji: %v", err)
	}
	if err := b.store.SaveCustomEmoji(legacy); err != nil {
		t.Fatalf("B SaveCustomEmoji: %v", err)
	}

	a.adoptUnsignedPackRecords()
	b.adoptUnsignedPackRecords()

	mine, err := a.CustomEmoji(g.ID)
	if err != nil || len(mine) != 1 {
		t.Fatalf("A CustomEmoji: %v (%d)", err, len(mine))
	}
	if len(mine[0].Sig) == 0 {
		t.Fatal("the owner must adopt a legacy record so it can spread again")
	}
	if !b.authorizedPackRecord(g.ID, mine[0].Author, mine[0].Sig, emojiSigningBytes(g.ID, mine[0]), map[string]bool{}) {
		t.Fatal("the adopted record must pass the sync lane at another member")
	}

	theirs, err := b.CustomEmoji(g.ID)
	if err != nil || len(theirs) != 1 {
		t.Fatalf("B CustomEmoji: %v (%d)", err, len(theirs))
	}
	if len(theirs[0].Sig) != 0 {
		t.Fatal("a member without Manage Guild must not sign a pack record")
	}
}

// TestPackRecordAuthorityIsCheckedOnTheSyncLane is the second §13 row, driven
// against a real guild's governance state: the sync path now asks the same
// question of the same person that the gossip path always did.
func TestPackRecordAuthorityIsCheckedOnTheSyncLane(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx) // owner: holds Manage Guild
	b := startService(t, ctx) // ordinary member: does not

	g, err := a.CreateGuild("pack-authority")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("B JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)

	if err := a.AddCustomEmoji(g.ID, "blobcat", "data:image/png;base64,AAAA"); err != nil {
		t.Fatalf("AddCustomEmoji: %v", err)
	}
	// B learns it over gossip, complete with A's signature — that is what makes
	// B able to relay it to anyone later.
	deadline := time.Now().Add(20 * time.Second)
	var learned domain.CustomEmoji
	for time.Now().Before(deadline) {
		em, err := b.CustomEmoji(g.ID)
		if err == nil && len(em) == 1 {
			learned = em[0]
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if learned.Name != "blobcat" {
		t.Fatal("B never learned the emoji over gossip")
	}
	if len(learned.Sig) == 0 || len(learned.Author) == 0 {
		t.Fatal("B must keep the adding admin's signature, or it cannot relay the record")
	}

	perm := map[string]bool{}
	// The genuine record, judged by B against B's own governance state.
	if !b.authorizedPackRecord(g.ID, learned.Author, learned.Sig, emojiSigningBytes(g.ID, learned), perm) {
		t.Fatal("the owner's own signed record must be accepted through the sync lane")
	}
	// Fail closed on an absent signature: this is the injection the row named.
	unsigned := learned
	unsigned.Author, unsigned.Sig = nil, nil
	if b.authorizedPackRecord(g.ID, unsigned.Author, unsigned.Sig, emojiSigningBytes(g.ID, unsigned), map[string]bool{}) {
		t.Fatal("an unsigned pack record must be refused on the sync lane")
	}
	// A member without Manage Guild signing their own record is refused on
	// authority, not on arithmetic — the signature here is perfectly good.
	byB := domain.CustomEmoji{Name: "sneaky", Image: "data:image/png;base64,BBBB", Author: b.PublicKey()}
	byB.Sig = b.id.Sign(emojiSigningBytes(g.ID, byB))
	if !verifyPackSig(byB.Author, byB.Sig, emojiSigningBytes(g.ID, byB)) {
		t.Fatal("the test's own control record must be well signed")
	}
	if b.authorizedPackRecord(g.ID, byB.Author, byB.Sig, emojiSigningBytes(g.ID, byB), map[string]bool{}) {
		t.Fatal("a member without Manage Guild must not be able to inject a pack record")
	}
	// Swapping the image under the admin's signature — the "replace an existing
	// emoji's image by serving a doctored snapshot" case.
	doctored := learned
	doctored.Image = "data:image/png;base64,CCCC"
	if b.authorizedPackRecord(g.ID, doctored.Author, doctored.Sig, emojiSigningBytes(g.ID, doctored), map[string]bool{}) {
		t.Fatal("a doctored image under the admin's signature must be refused")
	}
}

// waitFor polls until cond holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal(msg)
}
