package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/ZahakJ/concord/internal/domain"
)

// ownerAdminGuild has owner create a guild (create makes it), admin join it
// and receive a role with Manage members, and waits until admin can admit.
func ownerAdminGuild(t *testing.T, owner, admin *Service, create func() (domain.Guild, error)) domain.Guild {
	t.Helper()
	g, err := create()
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	code, err := owner.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := admin.JoinViaInvite(code); err != nil {
		t.Fatalf("admin join: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, admin)
	roleID, err := owner.UpsertRole(g.ID, "", "Admin", "#5865f2", PermManageMembers, 5)
	if err != nil {
		t.Fatalf("UpsertRole: %v", err)
	}
	if err := owner.AssignRole(g.ID, admin.id.Fingerprint(), roleID, true); err != nil {
		t.Fatalf("AssignRole: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool { return admin.CanManageMembers(g.ID) }, "the admin role never reached the admin")
	return g
}

// setStoredOwner replaces the owner in a device's copy of the guild record,
// leaving everything else as it was.
func setStoredOwner(s *Service, guildID string, key []byte) {
	s.mu.Lock()
	rec := s.guilds[guildID].Clone()
	rec.OwnerID = append([]byte(nil), key...)
	s.guilds[guildID] = &rec
	s.rebuildGovStateLocked(guildID)
	s.mu.Unlock()
}

// createLegacyGuild makes a guild the way builds before the founder scheme
// did: a random 16-byte group id.
func createLegacyGuild(s *Service, name string) (domain.Guild, error) {
	gid, err := s.mls.CreateGroup(s.ctx)
	if err != nil {
		return domain.Guild{}, err
	}
	g := domain.NewGuild(name, gid, s.PublicKey())
	if err := s.store.SaveGuild(g); err != nil {
		return domain.Guild{}, err
	}
	s.trackGuild(&g)
	return s.offerAfter(g, nil)
}

func storedGuild(s *Service, guildID string) domain.Guild {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.guilds[guildID].Clone()
}

// waitGovLog waits until s holds at least as many governance ops for the
// guild as from does.
func waitGovLog(t *testing.T, from, s *Service, guildID string) {
	t.Helper()
	from.mu.RLock()
	n := len(from.govOps[guildID])
	from.mu.RUnlock()
	waitUntil(t, 30*time.Second, func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		return len(s.govOps[guildID]) >= n
	}, "the governance log never arrived")
}

// FP-05 (a). In a guild made by this build, an admitting admin whose copy of
// the record names someone other than the founder does not get the newcomer:
// the join is refused and the newcomer stores nothing for the guild.
func TestOwnerANewGuildJoinerRefusesAnotherOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	admin := startService(t, ctx)
	newcomer := startService(t, ctx)
	g := ownerAdminGuild(t, owner, admin, func() (domain.Guild, error) { return owner.CreateGuild("g") })
	if !domain.IsFounderGroupID(g.GroupID) {
		t.Fatalf("a new guild has group id %x, not a founder-scheme id", g.GroupID)
	}

	setStoredOwner(admin, g.ID, admin.PublicKey())
	adminCode, err := admin.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("admin InviteCode: %v", err)
	}
	_, err = newcomer.JoinViaInvite(adminCode)
	if !errors.Is(err, errInviteMismatch) {
		t.Fatalf("join through a record naming another owner: err %v, want the mismatch refusal", err)
	}
	if holdsGuild(newcomer, g.ID) {
		t.Error("the refused newcomer holds the guild record")
	}
	if _, err := newcomer.mls.Epoch(ctx, g.GroupID); err == nil {
		t.Error("the refused newcomer holds MLS state for the guild")
	}
}

// FP-05 (b). KNOWN OPEN CASE, recorded as it stands: a guild made before the
// founder scheme (a 16-byte group id) still takes its owner from the
// admitting member's record. 0.61 keeps this behaviour for such guilds; the
// manual says so. If this test starts failing, the behaviour changed and the
// manual and release notes need to change with it.
func TestOwnerALegacyGuildJoinerTakesTheAdmittersOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	admin := startService(t, ctx)
	newcomer := startService(t, ctx)
	g := ownerAdminGuild(t, owner, admin, func() (domain.Guild, error) { return createLegacyGuild(owner, "legacy") })
	if len(g.GroupID) != domain.LegacyGroupIDLen {
		t.Fatalf("the legacy helper made a %d-byte group id", len(g.GroupID))
	}

	setStoredOwner(admin, g.ID, admin.PublicKey())
	adminCode, err := admin.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("admin InviteCode: %v", err)
	}
	if _, err := newcomer.JoinViaInvite(adminCode); err != nil {
		t.Fatalf("newcomer join via admin: %v", err)
	}
	waitMembers(t, 30*time.Second, 3, owner, admin, newcomer)
	if !newcomer.IsGuildOwner(g.ID, admin.id.Fingerprint()) {
		t.Errorf("legacy guild: the newcomer's owner is %q; the recorded behaviour is the admitter's record (%s)",
			newcomer.GuildOwnerFingerprint(g.ID), admin.id.Fingerprint())
	}
}

// FP-05 (c). An honest admin admits a newcomer to a new guild: the newcomer
// verifies the founder from the group and folds the founder's roles.
func TestOwnerAnHonestAdmissionVerifiesTheFounder(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	admin := startService(t, ctx)
	newcomer := startService(t, ctx)
	g := ownerAdminGuild(t, owner, admin, func() (domain.Guild, error) { return owner.CreateGuild("g") })

	adminCode, err := admin.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("admin InviteCode: %v", err)
	}
	if _, err := newcomer.JoinViaInvite(adminCode); err != nil {
		t.Fatalf("newcomer join via admin: %v", err)
	}
	waitMembers(t, 30*time.Second, 3, owner, admin, newcomer)
	waitGovLog(t, owner, newcomer, g.ID)
	if !domain.FounderMatches(storedGuild(newcomer, g.ID).GroupID, g.ID, owner.PublicKey()) {
		t.Error("the newcomer's record does not name the founder its group was made for")
	}
	if !newcomer.IsGuildOwner(g.ID, owner.id.Fingerprint()) {
		t.Errorf("the newcomer's owner is %q, want the founder", newcomer.GuildOwnerFingerprint(g.ID))
	}
	if !newcomer.memberHasPerm(g.ID, admin.id.Fingerprint(), PermManageMembers) {
		t.Error("the newcomer does not see the admin's role from the founder")
	}
}

// After the founder hands the guild to B, an honest admission by B still
// verifies the founder from the group and folds the transfer to B; a record
// that names B as founder is refused.
func TestOwnerATransferredGuildStillVerifiesTheFounder(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	founder := startService(t, ctx)
	b := startService(t, ctx)
	honest := startService(t, ctx)
	second := startService(t, ctx)
	g := ownerAdminGuild(t, founder, b, func() (domain.Guild, error) { return founder.CreateGuild("g") })
	if err := founder.TransferOwnership(g.ID, b.id.Fingerprint()); err != nil {
		t.Fatalf("TransferOwnership: %v", err)
	}
	waitUntil(t, 20*time.Second, func() bool { return b.IsGuildOwner(g.ID, b.id.Fingerprint()) }, "B never became owner")

	code, err := b.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("B InviteCode: %v", err)
	}
	if _, err := honest.JoinViaInvite(code); err != nil {
		t.Fatalf("honest join via B: %v", err)
	}
	waitGovLog(t, founder, honest, g.ID)
	waitUntil(t, 20*time.Second, func() bool { return honest.IsGuildOwner(g.ID, b.id.Fingerprint()) },
		"the newcomer did not fold the founder's transfer to B")

	setStoredOwner(b, g.ID, b.PublicKey())
	code, err = b.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("B InviteCode: %v", err)
	}
	if _, err := second.JoinViaInvite(code); !errors.Is(err, errInviteMismatch) {
		t.Fatalf("join through a record naming B as founder: err %v, want the mismatch refusal", err)
	}
}

// A member that pastes a code for a guild it holds keeps its own record when
// the answer re-seats it, even if the answer's record names another owner.
// This is the only protection a guild from before the founder scheme has, so
// it is checked on one.
func TestOwnerAProbeKeepsTheHeldRecord(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	admin := startService(t, ctx)
	member := startService(t, ctx)
	g := ownerAdminGuild(t, owner, admin, func() (domain.Guild, error) { return createLegacyGuild(owner, "legacy") })
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("member join: %v", err)
	}
	waitMembers(t, 30*time.Second, 3, owner, admin, member)
	held := storedGuild(member, g.ID)

	// The admin re-seats the member instead of answering "already a member",
	// and sends a record that names the admin.
	admin.host.HandleInvites(func(c context.Context, from peer.ID, raw []byte) ([]byte, error) {
		var req inviteRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		req.Probe = false
		plain, _ := json.Marshal(req)
		out, err := admin.handleInviteRequest(c, from, plain)
		if err != nil {
			return out, err
		}
		var resp inviteResponse
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, err
		}
		resp.Guild.OwnerID = admin.PublicKey()
		return json.Marshal(resp)
	})
	adminCode, err := admin.InviteCode(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := member.JoinViaInvite(adminCode); err != nil {
		t.Fatalf("probe answered with a re-seat: %v", err)
	}
	got := storedGuild(member, g.ID)
	if !bytes.Equal(got.OwnerID, held.OwnerID) || !bytes.Equal(got.GroupID, held.GroupID) {
		t.Fatalf("after the re-seat the member's record names owner %x group %x; it held %x %x",
			got.OwnerID, got.GroupID, held.OwnerID, held.GroupID)
	}
	if !member.IsGuildOwner(g.ID, owner.id.Fingerprint()) {
		t.Errorf("the member's owner is %q after the re-seat, want the founder", member.GuildOwnerFingerprint(g.ID))
	}
}

// A member that pastes a code for a guild it holds, answered with a welcome
// for another group under this guild's label, stores nothing for that group,
// keeps its own group and record as they were, and is told the invite does
// not match.
func TestOwnerAProbeRefusesAWelcomeForAnotherGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g, err := owner.CreateGuild("ours")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatalf("member join: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, owner, member)
	other, err := owner.CreateGuild("other")
	if err != nil {
		t.Fatal(err)
	}
	held := storedGuild(member, g.ID)
	before, err := member.mls.Epoch(ctx, g.GroupID)
	if err != nil {
		t.Fatal(err)
	}

	owner.host.HandleInvites(func(c context.Context, from peer.ID, raw []byte) ([]byte, error) {
		var req inviteRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		req.GuildID, req.Probe = other.ID, false
		swapped, _ := json.Marshal(req)
		out, err := owner.handleInviteRequest(c, from, swapped)
		if err != nil {
			return out, err
		}
		var resp inviteResponse
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, err
		}
		resp.Guild = storedGuild(owner, g.ID)
		return json.Marshal(resp)
	})
	if _, err := member.JoinViaInvite(code); !errors.Is(err, errInviteMismatch) {
		t.Fatalf("probe answered with another group's welcome: err %v, want the mismatch refusal", err)
	}
	if m, err := member.mls.Members(ctx, other.GroupID); err == nil && len(m) > 0 {
		t.Fatalf("the member now holds the MLS group of the other guild (%d members)", len(m))
	}
	if after, err := member.mls.Epoch(ctx, g.GroupID); err != nil || after != before {
		t.Fatalf("the held group changed: epoch %d -> %d, %v", before, after, err)
	}
	got := storedGuild(member, g.ID)
	if !bytes.Equal(got.OwnerID, held.OwnerID) || !bytes.Equal(got.GroupID, held.GroupID) {
		t.Fatal("the held record changed")
	}
}

// A device that holds a guild made by this build with the wrong owner in its
// record (as a record kept from an older build may) takes the founder
// back from its tree on the next audit, and the crown moves to the founder.
func TestOwnerAuditRecoversTheFounderFromTheTree(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 2, owner, member)

	setStoredOwner(member, g.ID, member.PublicKey())
	if !member.IsGuildOwner(g.ID, member.id.Fingerprint()) {
		t.Fatal("setup: the changed record did not take")
	}
	member.auditFounder(g.ID)
	if got := storedGuild(member, g.ID).OwnerID; !bytes.Equal(got, owner.PublicKey()) {
		t.Fatalf("after the audit the record names %x, want the founder", got)
	}
	if !member.IsGuildOwner(g.ID, owner.id.Fingerprint()) || member.IsGuildOwner(g.ID, member.id.Fingerprint()) {
		t.Fatalf("after the audit the owner is %q, want the founder", member.GuildOwnerFingerprint(g.ID))
	}
	if member.OwnerUnverified(g.ID) {
		t.Fatal("a recovered guild is marked unverified")
	}
}

// A founder on a linked device: the group id names the account key, and a
// member whose tree holds only the device's certificate leaf recovers the
// account key from the certificate.
func TestOwnerAuditRecoversALinkedDeviceFounder(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := testRendezvous(t, ctx)
	bob := startServiceOn(t, ctx, t.TempDir(), boot)
	tablet, _ := linkTablet(t, ctx, bob, boot)
	member := startServiceOn(t, ctx, t.TempDir(), boot)
	account := bob.PublicKey()
	// Bob's first device is closed, so only the tablet's leaf is seated.
	_ = bob.Close()

	g, err := tablet.CreateGuild("from the tablet")
	if err != nil {
		t.Fatal(err)
	}
	if !domain.FounderMatches(g.GroupID, g.ID, account) {
		t.Fatal("a guild made on a linked device does not name the account key")
	}
	code, _ := tablet.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 30*time.Second, func() bool {
		creds, err := member.mls.Members(ctx, g.GroupID)
		return err == nil && len(creds) == 2
	}, "the member never saw the tablet")
	creds, _ := member.mls.Members(ctx, g.GroupID)
	for _, c := range creds {
		if bytes.Equal(c, account) {
			t.Fatal("setup: the account key itself is a leaf, so the certificate path is not exercised")
		}
	}
	member.mu.Lock()
	member.govOps[g.ID] = nil // only the tree may name the founder here
	member.mu.Unlock()
	setStoredOwner(member, g.ID, member.PublicKey())
	member.auditFounder(g.ID)
	if got := storedGuild(member, g.ID).OwnerID; !bytes.Equal(got, account) {
		t.Fatalf("after the audit the record names %x, want the account key %x", got, account)
	}
}

// With the founder's key nowhere on the device, a guild made by this build
// whose record names someone else is owner-unverified: nobody is owner and no
// fingerprint stands in for one. A sync payload carrying a record that
// verifies ends it.
func TestOwnerUnverifiedUntilASyncCarriesTheFounder(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	founderLocalCandidatesOff.Store(true)
	defer founderLocalCandidatesOff.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := startService(t, ctx)
	member := startService(t, ctx)
	g, err := owner.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	code, _ := owner.InviteCode(g.ID)
	if _, err := member.JoinViaInvite(code); err != nil {
		t.Fatal(err)
	}
	waitMembers(t, 30*time.Second, 2, owner, member)

	setStoredOwner(member, g.ID, member.PublicKey())
	member.auditFounder(g.ID)
	if !member.OwnerUnverified(g.ID) {
		t.Fatal("a record naming a key the group was not made for is not unverified")
	}
	if fp := member.GuildOwnerFingerprint(g.ID); fp != "" {
		t.Fatalf("an unverified guild shows owner %q", fp)
	}
	for _, s := range []*Service{member, owner} {
		if member.IsGuildOwner(g.ID, s.id.Fingerprint()) {
			t.Fatalf("an unverified guild treats %s as owner", s.id.Fingerprint())
		}
	}
	if member.CanManageMembers(g.ID) {
		t.Fatal("an unverified guild lets the misnamed owner manage members")
	}

	if err := member.syncGuildFromPeer(g.ID, owner.host.PeerID()); err != nil {
		t.Logf("sync: %v", err)
	}
	waitUntil(t, 30*time.Second, func() bool { return !member.OwnerUnverified(g.ID) }, "a sync from the founder did not end the unverified state")
	if got := storedGuild(member, g.ID).OwnerID; !bytes.Equal(got, owner.PublicKey()) {
		t.Fatalf("after the sync the record names %x, want the founder", got)
	}
	if !member.IsGuildOwner(g.ID, owner.id.Fingerprint()) {
		t.Fatal("after the sync the founder is not owner")
	}
}

// Every kind of group this build creates carries a founder-scheme id for this
// account: guilds, meetings, Notes and DMs from a code. (Group DMs and the
// 1:1 opener need verified contacts and use the same helper.)
func TestEveryCreatorUsesTheFounderID(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := startService(t, ctx)
	check := func(what string, g domain.Guild) {
		t.Helper()
		if !domain.FounderMatches(g.GroupID, g.ID, s.PublicKey()) {
			t.Errorf("%s: group id %x does not name this account", what, g.GroupID)
		}
		if s.OwnerUnverified(g.ID) {
			t.Errorf("%s: marked unverified", what)
		}
	}
	g, err := s.CreateGuild("g")
	if err != nil {
		t.Fatal(err)
	}
	check("guild", g)
	m, _, err := s.StartMeeting()
	if err != nil {
		t.Fatal(err)
	}
	check("meeting", m)
	n, err := s.ensureNotesDM()
	if err != nil {
		t.Fatal(err)
	}
	check("notes", n)
	if _, err := s.NewDMInvite(); err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, x := range s.Guilds() {
		if x.Kind == "dm" && x.Name == "Direct message" {
			check("dm from a code", x)
			seen = true
		}
	}
	if !seen {
		t.Error("the DM from a code was not found")
	}
}

// joinWant names the group a join must land in, by the shape of the answer's
// group id, and refuses an answer it cannot place.
func TestJoinWant(t *testing.T) {
	founder := bytes.Repeat([]byte{0x11}, 32)
	other := bytes.Repeat([]byte{0x12}, 32)
	gid, err := domain.NewFounderGroupID("g", founder)
	if err != nil {
		t.Fatal(err)
	}
	legacy := bytes.Repeat([]byte{0x07}, domain.LegacyGroupIDLen)
	held := &domain.Guild{ID: "g", GroupID: legacy, OwnerID: other}

	cases := []struct {
		what   string
		answer domain.Guild
		held   *domain.Guild
		want   []byte
		ok     bool
	}{
		{"founder id naming its founder", domain.Guild{ID: "g", GroupID: gid, OwnerID: founder}, nil, gid, true},
		{"founder id naming another key", domain.Guild{ID: "g", GroupID: gid, OwnerID: other}, nil, nil, false},
		{"founder id for another guild id", domain.Guild{ID: "h", GroupID: gid, OwnerID: founder}, nil, gid, true}, // the code's id is what counts; see below
		{"legacy id", domain.Guild{ID: "g", GroupID: legacy, OwnerID: other}, nil, legacy, true},
		{"unknown length", domain.Guild{ID: "g", GroupID: make([]byte, 20), OwnerID: founder}, nil, nil, false},
		{"unknown version", domain.Guild{ID: "g", GroupID: append([]byte{0x02}, gid[1:]...), OwnerID: founder}, nil, nil, false},
		{"no group", domain.Guild{ID: "g", OwnerID: founder}, nil, nil, false},
		{"probe: the held group, whatever the answer", domain.Guild{ID: "g", GroupID: gid, OwnerID: other}, held, legacy, true},
		{"probe with no held group", domain.Guild{ID: "g", GroupID: gid, OwnerID: founder}, &domain.Guild{ID: "g"}, nil, false},
	}
	for _, c := range cases {
		got, err := joinWant("g", c.answer, c.held)
		if (err == nil) != c.ok || !bytes.Equal(got, c.want) {
			t.Errorf("%s: %x, %v", c.what, got, err)
		}
		if err != nil && !errors.Is(err, errInviteMismatch) {
			t.Errorf("%s: refusal %v is not the mismatch refusal", c.what, err)
		}
	}
	// The id the founder is checked against is the code's, not the answer's.
	if _, err := joinWant("h", domain.Guild{ID: "h", GroupID: gid, OwnerID: founder}, nil); err == nil {
		t.Error("a founder id made for guild g was accepted for guild h")
	}
}
