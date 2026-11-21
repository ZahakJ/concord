package app

// Regressions for the 0.62 security review (F1-F6). Each was first a proof
// that the finding held, then turned round to what the fix guarantees.

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZahakJ/concord/internal/domain"
	"github.com/libp2p/go-libp2p/core/peer"
)

// F1. An answer with no payload proves nothing about the ratchet. The owner's
// governance is stale (it never saw the mod's grant), so the chain's first
// commit is refused and moves nothing. The chain answer must not lower the
// flag, and since the owner is still behind the epoch it names, it raises it,
// as EpochGap does, which is what sends the owner to the door.
func TestPayloadlessAnswerNeverLowersTheFlag(t *testing.T) {
	for _, flagged := range []bool{false, true} {
		t.Run(fmt.Sprintf("flaggedBefore=%v", flagged), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			holdSeatDefence(t)
			s := newSeatSetup(t, ctx)
			holdCatchUps(s.owner)
			var noDoor inviteAskFunc = func(context.Context, peer.AddrInfo, []byte) ([]byte, error) {
				return nil, fmt.Errorf("door held by the test")
			}
			s.owner.requestInvite.Store(&noDoor)
			_, _, epoch := offlineRemoval(t, ctx, s)

			s.owner.mu.Lock()
			st := s.owner.govState[s.g.ID]
			delete(st.MemberRoles, s.mod.Fingerprint())
			s.owner.govState[s.g.ID] = st
			s.owner.mu.Unlock()
			if s.owner.authorizedCommitter(s.g.ID, s.mod.myCredential) {
				t.Fatal("setup: the owner still authorizes the mod")
			}

			req, _ := json.Marshal(syncRequest{GuildID: s.g.ID, Epoch: epoch, OwnerChain: true})
			honest, err := s.mod.handleSyncRequest(ctx, s.owner.host.PeerID(), req)
			if err != nil || len(honest) == 0 {
				t.Fatalf("setup: the mod did not serve the chain (%v)", err)
			}
			var served atomic.Int32
			var ask syncAskFunc = func(context.Context, peer.ID, []byte) ([]byte, error) {
				if served.CompareAndSwap(0, 1) {
					return honest, nil
				}
				return []byte{}, nil
			}
			s.owner.requestSync.Store(&ask)

			s.owner.setOutOfSync(s.g.ID, flagged)
			_ = s.owner.syncGuildFromPeer(s.g.ID, s.mod.host.PeerID())
			if served.Load() != 1 {
				t.Fatal("the chain answer was never asked for")
			}
			if oe, _ := s.owner.mls.Epoch(ctx, s.gid); oe != epoch || s.owner.takenOutLatched(s.g.ID) {
				t.Fatalf("the owner moved (epoch %d, want %d) or latched on a refused chain", oe, epoch)
			}
			if !s.owner.OutOfSync(s.g.ID) {
				t.Fatal("a payload-less answer from a peer two epochs ahead left the guild unflagged")
			}
		})
	}
}

// F2. A device its account unlinked keeps a certificate that verifies and the
// key it names, so the door's binding check passes it. The account's own
// devices hold the revocation and must refuse it at the door, as they already
// did for the chain.
func TestTheDoorRefusesADeviceItsAccountUnlinked(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := newUnlinkWorld(t, ctx)
	g := w.guildOf(t, w.bob, w.phone, w.alice, w.carol) // Bob owns it
	gid := theGuild(t, w.bob).GroupID

	kp, err := w.phone.mls.KeyPackage(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cred := append([]byte(nil), w.phone.myCredential...)
	pid := w.phone.host.PeerID()
	before, _ := w.alice.mls.Epoch(ctx, gid)
	_ = w.phone.Close()

	if err := w.bob.UnlinkDevice(w.phoneKey); err != nil {
		t.Fatalf("UnlinkDevice: %v", err)
	}
	waitMemberCount(t, 60*time.Second, g.ID, 3, w.alice, w.carol, w.bob)
	if n := len(leavesWith(w.bob, gid, cred)); n != 0 {
		t.Fatalf("setup: the phone's leaf is still there (%d)", n)
	}

	if r := askChain(t, w.bob, pid, g.ID, before, true); !declined(r) {
		t.Fatalf("Bob's desktop served the unlinked phone the chain: %+v", r)
	}
	req, _ := json.Marshal(inviteRequest{GuildID: g.ID, KeyPackage: kp, Credential: cred, Heal: true})
	raw, err := w.bob.handleInviteRequest(ctx, pid, req)
	if err != nil {
		t.Fatal(err)
	}
	var resp inviteResponse
	_ = json.Unmarshal(raw, &resp)
	if len(resp.Welcome) > 0 || resp.Error == "" {
		t.Fatalf("Bob's desktop let the phone it unlinked back in (welcome %d bytes, error %q)", len(resp.Welcome), resp.Error)
	}
	time.Sleep(2 * time.Second)
	if n := len(leavesWith(w.bob, gid, cred)); n != 0 {
		t.Fatalf("the unlinked phone's certificate is seated again (%d leaves)", n)
	}
}

// F3. The decompressed cap bounds the JSON text; what the text parses into is
// bounded too. A million empty messages are 3 MiB of JSON (well under the
// 64 MiB floor) and about 3 KiB gzipped; before the entry budget they decoded
// into a 279 MiB slice, with 1.4 GiB allocated on the way.
func TestArchiveEntriesAreBoundedByTheText(t *testing.T) {
	const n = 1 << 20
	var js bytes.Buffer
	js.WriteString(`{"version":1,"messages":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			js.WriteByte(',')
		}
		js.WriteString("{}")
	}
	js.WriteString("]}")
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	_, _ = zw.Write(js.Bytes())
	_ = zw.Close()
	js.Reset()

	runtime.GC()
	var b, a runtime.MemStats
	runtime.ReadMemStats(&b)
	af, err := decodeArchive(gz.Bytes())
	runtime.ReadMemStats(&a)
	if !errors.Is(err, errArchiveOverCount) {
		t.Fatalf("%d gzip bytes holding a million messages: %d decoded, err %v; want errArchiveOverCount", gz.Len(), len(af.Messages), err)
	}
	// Reading 3 MiB of text and decoding up to the budget (49k messages,
	// 13 MiB) allocates tens of MiB; the unbounded decode allocated 1398 MiB.
	// TotalAlloc counts the whole process, so the bound leaves room for
	// other tests' services in a full -race run.
	if got := (a.TotalAlloc - b.TotalAlloc) >> 20; got > 384 {
		t.Fatalf("the refusal allocated %d MiB", got)
	}
}

// F3, the other side: the budgeted parse reads an honest archive exactly as
// json.Unmarshal does, nulls and unknown keys included.
func TestParseArchiveMatchesUnmarshal(t *testing.T) {
	af := archiveFile{Version: 1, Created: "2026-11-21T00:00:00Z", App: "0.62.0", Saved: []string{"m1"}, Blobs: map[string][]byte{"b1": {1, 2, 3}}}
	af.Guilds = []archiveGuild{{ID: "0123456789abcdef0123456789abcdef", Name: "g", Channels: []archiveChannel{{ID: "c1", Name: "general"}, {ID: "c2", Name: "voice", Type: "voice"}}}}
	for i := 0; i < 50; i++ {
		af.Messages = append(af.Messages, domain.Message{
			ID: fmt.Sprintf("%032x", i), ChannelID: "c1", Content: "hello", Sender: bytes.Repeat([]byte{byte(i)}, 32),
			Sent: time.Unix(1700000000+int64(i), 0).UTC(), Reactions: map[string][]string{"x": {"a"}},
		})
	}
	raw, _ := json.Marshal(af)
	extra := append(append([]byte(nil), bytes.TrimSuffix(raw, []byte("}"))...), []byte(`,"future":[{},{},{"a":[1,2]}],"saved":null}`)...)
	for name, text := range map[string][]byte{
		"as exported":            raw,
		"unknown key and a null": extra,
		"empty":                  []byte(`{}`),
		"nulls":                  []byte(`{"version":1,"guilds":null,"messages":null,"blobs":null}`),
		"empties":                []byte(`{"guilds":[],"messages":[],"saved":[],"blobs":{}}`),
		"repeated keys":          []byte(`{"saved":["a"],"saved":["b","c"],"blobs":{"x":"AQ=="},"blobs":{"y":"Ag=="},"messages":[{}],"messages":null}`),
	} {
		var want archiveFile
		if err := json.Unmarshal(text, &want); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := parseArchive(text)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: parseArchive differs from json.Unmarshal\n got %+v\nwant %+v", name, got, want)
		}
	}
	for _, bad := range []string{`[]`, `{"messages":{}}`, `{"version":1}x`, `{"guilds":[{}]`} {
		if _, err := parseArchive([]byte(bad)); err == nil {
			t.Errorf("parseArchive(%q) accepted", bad)
		}
	}
}

// F4. A committer of any group the responder is in seats a leaf whose
// certificate, signed by its own account, names the owner's key, on a leaf key
// the committer holds. The responder must not re-map the owner's peer, and
// keeps serving the owner the chain.
func TestRosterCertificateMustNameItsLeafKey(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	defer holdCatchUps(s.owner)()

	atk := startService(t, ctx)
	g2, err := atk.CreateGuild("attackers")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := atk.InviteCode(g2.ID)
	if _, err := s.mod.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMemberCount(t, 30*time.Second, g2.ID, 2, atk, s.mod)
	var gid2 []byte
	for _, g := range atk.Guilds() {
		if g.ID == g2.ID {
			gid2 = g.GroupID
		}
	}
	op := s.owner.host.PeerID()
	if got := s.mod.presence(op).Fingerprint; got != s.owner.Fingerprint() {
		t.Fatalf("setup: the mod resolves the owner's peer to %q", got)
	}

	cert := atk.id.IssueDeviceCertFor(s.owner.PublicKey(), "x", time.Now().Unix())
	addLeafWithCredential(t, atk, gid2, cert.Marshal(), freshKey(t))
	ae, _ := atk.mls.Epoch(ctx, gid2)
	waitUntil(t, 30*time.Second, func() bool {
		me, _ := s.mod.mls.Epoch(ctx, gid2)
		return me == ae
	}, "the mod never applied the attacker's add")
	s.mod.relearnDevices(gid2)
	if got := s.mod.presence(op).Fingerprint; got != s.owner.Fingerprint() {
		t.Fatalf("the mod now resolves the owner's peer to %q (attacker %q)", got, atk.Fingerprint())
	}

	_, _, epoch := offlineRemoval(t, ctx, s)
	if r := askChain(t, s.mod, op, s.g.ID, epoch, true); declined(r) {
		t.Fatal("the real owner's chain request was declined")
	}
}

// F5. The owner of another guild, with many device keys, fills the throttle
// table. This guild's owner is still served.
func TestThrottleKeepsRoomForAnotherGuildsOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	defer holdCatchUps(s.owner)()
	_, _, epoch := offlineRemoval(t, ctx, s)

	for i := 0; i < ownerChainTrack; i++ {
		if !s.mod.claimOwnerChain("another-guild", peer.ID(fmt.Sprintf("dev%d", i)), 0, 1) {
			t.Fatalf("filler %d refused", i)
		}
	}
	for i := 0; i < ownerChainTrack; i++ {
		s.mod.claimOwnerChain(fmt.Sprintf("guild%d", i), peer.ID(fmt.Sprintf("dev%d", i)), 0, 1)
	}
	if r := askChain(t, s.mod, s.owner.host.PeerID(), s.g.ID, epoch, true); declined(r) {
		t.Fatal("this guild's owner was declined behind a table full of other guilds' entries")
	}
}

// F6. A moderator seats a leaf carrying another moderator's account key under
// a key it holds, and removes the owner's device from that leaf. The notice
// must not name the bystander.
func TestOwnerRemovedNoticeDoesNotNameAFramedMember(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	holdSeatDefence(t)
	s := newSeatSetup(t, ctx)
	if err := s.owner.AssignRole(s.g.ID, s.member.Fingerprint(), s.roleID, true); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 20*time.Second, func() bool {
		return s.owner.authorizedCommitter(s.g.ID, s.member.myCredential)
	}, "grant")

	k := freshKey(t)
	e := addLeafWithCredential(t, s.mod, s.gid, s.member.myCredential, k)
	me, _ := s.mod.mls.Epoch(ctx, s.gid)
	waitUntil(t, 30*time.Second, func() bool {
		oe, _ := s.owner.mls.Epoch(ctx, s.gid)
		return oe == me
	}, "the owner never applied the add")

	ownerIdx, _ := s.owner.mls.OwnLeaf(ctx, s.gid)
	c2, err := e.RemoveLeaf(ctx, s.gid, ownerIdx, s.owner.ownAccountSigningKey())
	if err != nil {
		t.Fatalf("the phantom leaf could not mint the removal: %v", err)
	}
	if !s.owner.commitAuthorized(s.g.ID, s.gid, c2) {
		t.Fatal("setup: the owner does not authorize the phantom leaf's commit")
	}
	s.owner.screenCommit(s.g.ID, s.gid, c2)
	found := false
	for _, n := range s.owner.SeatNotices(s.g.ID) {
		if n.Kind != "owner_removed" {
			continue
		}
		found = true
		if n.Adder != "" {
			t.Fatalf("owner_removed names %q (bystander %q, mod %q); want no name", n.Adder, s.member.Fingerprint(), s.mod.Fingerprint())
		}
	}
	if !found {
		t.Fatal("no owner_removed notice")
	}
}
