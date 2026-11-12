package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/ZahakJ/concord/internal/identity"
)

// Honest messages flagged as unproven. A message reaches a peer by two lanes,
// and before these rules which one won the race decided whether an honest
// message from a sender on a pre-signature build carried the "nobody proved who
// wrote this" mark forever.

// unsignedFrom is a message as a build that predates signatures writes it.
func unsignedFrom(sender []byte, channelID, body string) domain.Message {
	return domain.Message{
		ID:        domain.NewID(),
		ChannelID: channelID,
		Sender:    sender,
		Name:      "nizami",
		Content:   body,
		Sent:      time.Now().UTC(),
	}
}

// TestAuthorServedRowIsNotMarked: a catch-up payload MLS proves came from the
// author's own account is the author speaking, not a member relaying them. An
// unsigned row of theirs in it is as proven as the live frame would have been;
// an unsigned row of anybody else's in the same payload is still relayed.
func TestAuthorServedRowIsNotMarked(t *testing.T) {
	author := mustID(t)
	other := mustID(t)

	own := unsignedFrom(author.PublicKey(), "ch1", "my own words, served by me")
	if !messageAttestationFrom(&own, author.PublicKey()) || own.Unverified {
		t.Fatal("an unsigned row served by its author's own account must not be marked")
	}

	relayed := unsignedFrom(other.PublicKey(), "ch1", "somebody else's words, served by me")
	if !messageAttestationFrom(&relayed, author.PublicKey()) || !relayed.Unverified {
		t.Fatal("an unsigned row the server did not write must still be marked")
	}

	// The transport proves who served it; it does not excuse a signature that
	// fails — that row was altered, whoever holds it.
	bad := signedMessage(author, "ch1", "signed")
	bad.Content = "altered after signing"
	if messageAttestationFrom(&bad, author.PublicKey()) {
		t.Fatal("a signature that does not verify is refused even from its author's account")
	}

	// The author's device itself holds a row under the author's name that it
	// got relayed, and serves it with its mark: its own copy of a forgery is
	// not the author vouching for it.
	planted := unsignedFrom(author.PublicKey(), "ch1", "planted on the author's device by somebody else")
	planted.Unverified = true // as a current server sends a row it holds marked
	if !messageAttestationFrom(&planted, author.PublicKey()) || !planted.Unverified {
		t.Fatal("a row the author's own device holds marked must stay marked when it serves it")
	}

	// No server named (an archive, a test): the old rule.
	anon := unsignedFrom(author.PublicKey(), "ch1", "no server named")
	if !messageAttestationFrom(&anon, nil) || !anon.Unverified {
		t.Fatal("an unsigned row with no authenticated server must be marked")
	}
}

// TestLinkedDeviceSignsAsTheAccount pins the multi-device half of the question:
// a linked device holds the account seed alongside its own device key, and a
// message's signature is made with the ACCOUNT key — the key every receiver
// stores as the sender and checks against. Signing with the device key would
// fail verification and get the device's history refused on catch-up.
func TestLinkedDeviceSignsAsTheAccount(t *testing.T) {
	account := mustID(t)
	deviceSeed := make([]byte, identity.SeedSize)
	for i := range deviceSeed {
		deviceSeed[i] = byte(i + 1)
	}
	linked, err := identity.FromSeeds(account.Seed(), deviceSeed)
	if err != nil {
		t.Fatalf("FromSeeds: %v", err)
	}
	if string(linked.DevicePublicKey()) == string(account.PublicKey()) {
		t.Fatal("setup: a linked device must have its own device key")
	}
	svc := &Service{id: linked}
	m := unsignedFrom(svc.PublicKey(), "ch1", "from the phone")
	m.Sig = svc.signMessage(m)
	if string(m.Sender) != string(account.PublicKey()) {
		t.Fatal("a linked device must name the account as the sender")
	}
	if !verifyMessageSig(m) {
		t.Fatal("a linked device's message must verify against the account key")
	}
}

// TestLiveDeliveryProvesACatchUpMarkedRow drives the race over real peers. B
// holds A's message marked, because a catch-up from somebody else delivered an
// unsigned copy before the live frame; then A's frame arrives — exactly as a
// pre-signature build publishes it, no signature in it. MLS authenticated A as
// its sender, so the mark has to go. Before, SaveMessage's ON CONFLICT DO
// NOTHING dropped the frame as a duplicate and the mark stayed for good.
func TestLiveDeliveryProvesACatchUpMarkedRow(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	rb := &recorder{}
	b.OnMessage(rb.add)
	g, err := a.CreateGuild("sync-then-live")
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
	sendUntilReceived(t, a, channel, "mesh is warm", rb)

	a.mu.RLock()
	groupID := a.guilds[g.ID].GroupID
	a.mu.RUnlock()
	publishAsOldBuild := func(m domain.Message) {
		payload, _ := json.Marshal(m)
		ct, err := a.mls.Encrypt(a.ctx, groupID, payload)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if err := a.ps.Publish(a.ctx, domain.TopicID(groupID, channel), ct); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	// What a member's catch-up put on B's disk first: A's unsigned words, marked.
	holdMarked := func(m domain.Message) {
		relayed := m
		if !messageAttestation(&relayed) || !relayed.Unverified {
			t.Fatal("setup: an unsigned relayed row must be marked")
		}
		if _, err := b.store.UpsertSyncedMessage(relayed, b.id.Fingerprint(), false); err != nil {
			t.Fatalf("UpsertSyncedMessage: %v", err)
		}
	}

	same := unsignedFrom(a.PublicKey(), channel, "written on a build without signatures")
	holdMarked(same)
	// The relay's copy of a second message carries words A never wrote.
	bent := unsignedFrom(a.PublicKey(), channel, "what A actually wrote")
	bentCopy := bent
	bentCopy.Content = "what the relay says A wrote"
	holdMarked(bentCopy)

	settled := func(m domain.Message) bool {
		row, ok, err := b.store.MessageByID(m.ID)
		return err == nil && ok && !row.Unverified && row.Content == m.Content
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && !(settled(same) && settled(bent)) {
		publishAsOldBuild(same)
		publishAsOldBuild(bent)
		time.Sleep(time.Second)
	}
	if !settled(same) {
		row, _, _ := b.store.MessageByID(same.ID)
		t.Fatalf("the live, authenticated frame did not clear the mark: unverified=%v", row.Unverified)
	}
	if !settled(bent) {
		row, _, _ := b.store.MessageByID(bent.ID)
		t.Fatalf("the author's live words did not replace the relayed version: %q unverified=%v", row.Content, row.Unverified)
	}
	// The UI has to hear about it, or the mark stays on screen until a reload.
	waitFor(t, 5*time.Second, func() bool {
		rb.mu.Lock()
		defer rb.mu.Unlock()
		for _, m := range rb.msgs {
			if m.ID == same.ID && !m.Unverified {
				return true
			}
		}
		return false
	}, "the cleared row was never re-emitted to the UI")
}

// TestCatchUpFromTheAuthorIsNotMarked: B was away and catches up from A, and
// A's own history holds an unsigned message of A's — written on a build that
// predates signatures. The payload is MLS-authenticated as A's, so it is A
// vouching for A's words and B must not mark them. A row by somebody else in
// the same payload stays marked.
func TestCatchUpFromTheAuthorIsNotMarked(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	g, err := a.CreateGuild("author-served")
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

	mine := unsignedFrom(a.PublicKey(), channel, "A's pre-signature message")
	theirs := unsignedFrom(mustID(t).PublicKey(), channel, "someone else's, relayed by A")
	for _, m := range []domain.Message{mine, theirs} {
		if _, err := a.store.SaveMessage(m); err != nil {
			t.Fatalf("A SaveMessage: %v", err)
		}
	}
	// A row under A's own name that A's device itself got from a relay and
	// holds marked: A serving it is not A vouching for it.
	planted := unsignedFrom(a.PublicKey(), channel, "relayed to A under A's name")
	planted.Unverified = true
	if _, err := a.store.UpsertSyncedMessage(planted, a.id.Fingerprint(), false); err != nil {
		t.Fatalf("A UpsertSyncedMessage: %v", err)
	}
	mustSync(t, b, g.ID, a)
	row, ok, err := b.store.MessageByID(mine.ID)
	if err != nil || !ok {
		t.Fatalf("B never got A's row by catch-up: %v %v", ok, err)
	}
	if row.Unverified {
		t.Fatal("a row served by its author's own account was marked unverified")
	}
	other, ok, err := b.store.MessageByID(theirs.ID)
	if err != nil || !ok {
		t.Fatalf("B never got the relayed row: %v %v", ok, err)
	}
	if !other.Unverified {
		t.Fatal("a row the server did not write must stay marked")
	}
	pl, ok, err := b.store.MessageByID(planted.ID)
	if err != nil || !ok {
		t.Fatalf("B never got the planted row: %v %v", ok, err)
	}
	if !pl.Unverified {
		t.Fatal("a row A's device holds marked was laundered into a proven one by A serving it")
	}
}

// TestEverySendPathSigns walks the ways a member puts a row into a channel and
// checks each stored row carries a signature that verifies against the account.
// An unsigned path would be invisible on the live lane and flagged on every
// peer that later caught the row up from somebody else.
func TestEverySendPathSigns(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a real node")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := startService(t, ctx)
	g, err := a.CreateGuild("signed-paths")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	channel := g.Channels[0].ID

	var ids []string
	add := func(what string, m domain.Message, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		ids = append(ids, what+"\x00"+m.ID)
	}
	m, err := a.SendMessage(channel, "plain", "", "")
	add("SendMessage", m, err)
	r, err := a.SendMessage(channel, "a reply", m.ID, "rtl")
	add("reply", r, err)
	at, err := a.SendAttachment(channel, "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAABzenr0AAAAEUlEQVR4nGP8z8DAxMDAwAAABiIBUJMSSXAAAAAASUVORK5CYII=", 1, 1, "", false, "", "")
	add("SendAttachment", at, err)
	f, err := a.SendFile(channel, "data:text/plain;base64,aGVsbG8=", "hello.txt", "")
	add("SendFile", f, err)
	cn, err := a.SendCallNotice(channel, "call-missed", "missed")
	add("SendCallNotice", cn, err)
	gm, err := a.sendAs(channel, "a guest's line", "guest", "", "Visitor", "")
	add("guest relay", gm, err)
	sy, err := a.sendAs(channel, "the event is starting", "system", "", "The guild", "")
	add("system notice", sy, err)
	forum, err := a.CreateChannel(g.ID, "board", "forum", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	post, err := a.CreateThread(g.ID, forum.ID, "A post", "the opener", nil)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	if err := a.EditMessage(channel, m.ID, "plain, edited", ""); err != nil {
		t.Fatalf("EditMessage: %v", err)
	}
	if _, err := a.ScheduleSend(channel, "scheduled", "", time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatalf("ScheduleSend: %v", err)
	}
	a.fireDueScheduledSends(time.Now().Add(time.Minute).Unix())

	check := func(what string, row domain.Message) {
		t.Helper()
		if len(row.Sig) == 0 || !verifyMessageSig(row) {
			t.Errorf("%s: stored row is not signed by its author (sig %d bytes)", what, len(row.Sig))
		}
	}
	for _, e := range ids {
		var what, id string
		for i := range e {
			if e[i] == 0 {
				what, id = e[:i], e[i+1:]
				break
			}
		}
		row, ok, err := a.store.MessageByID(id)
		if err != nil || !ok {
			t.Fatalf("%s: row not stored: %v", what, err)
		}
		check(what, row)
	}
	found := map[string]bool{}
	for _, ch := range []string{channel, post.ID} {
		rows, err := a.store.Messages(ch, 0)
		if err != nil {
			t.Fatalf("Messages: %v", err)
		}
		for _, row := range rows {
			switch row.Content {
			case "the opener", "scheduled", "plain, edited":
				found[row.Content] = true
				check(row.Content, row)
			}
		}
	}
	for _, want := range []string{"the opener", "scheduled", "plain, edited"} {
		if !found[want] {
			t.Errorf("%q was never stored", want)
		}
	}
}
