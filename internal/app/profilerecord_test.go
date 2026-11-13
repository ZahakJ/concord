package app

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/nacl/secretbox"

	"github.com/ZahakJ/concord/internal/identity"
	"github.com/ZahakJ/concord/internal/store"
)

// recordService is a Service with a store and an identity and nothing else:
// enough for the record's seal, keep, serve and adopt halves, none of which
// touch the network.
func recordService(t *testing.T, id *identity.Identity) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "concord.db"), bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Service{store: st, id: id, profiles: map[string]Profile{}}
}

// storeProfile writes a profile as SetProfile would, without the announce.
func storeProfile(t *testing.T, s *Service, p Profile) {
	t.Helper()
	for k, v := range map[string]string{
		"display_name": p.Name, "status_text": p.Status, "avatar_emoji": p.Emoji,
		"accent_color": p.Color, "avatar_image": p.Avatar, "banner_image": p.Banner,
		"presence": p.Presence, "bio": p.Bio, "accent_color2": p.Color2,
		"avatar_frame": p.Frame, "card_effect": p.Effect, "card_style": encodeStyle(p.Style),
		"profile_updated_at": strconv.FormatInt(p.UpdatedAt, 10),
		gamesSettingKey:      gamesJSON(p.Games),
	} {
		if err := s.store.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
}

// announceOver is the wire: the account's announce frame, through JSON, as a
// member receives it.
func announceOver(t *testing.T, author *Service) guildMeta {
	t.Helper()
	meta := profileFrame(author.selfWireProfile(), author.id.Fingerprint())
	meta.ProfileSeal = author.profileSealFor(profileFromFrame(meta))
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	var got guildMeta
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func peerOf(t *testing.T, id *identity.Identity) peer.ID {
	t.Helper()
	p, ok := devicePeerID(id.PublicKey())
	if !ok {
		t.Fatal("peer id")
	}
	return p
}

func restoredFrom(t *testing.T, id *identity.Identity) *Service {
	t.Helper()
	again, err := identity.FromSeed(id.Seed())
	if err != nil {
		t.Fatal(err)
	}
	return recordService(t, again)
}

func recordProfile(at int64) Profile {
	p := sentinelProfile("back from the dead")
	p.Games = []Game{{Name: "Outer Wilds"}}
	p.UpdatedAt = at
	return p
}

// TestRestoredDeviceGetsItsProfileFromAMember is the whole path in one process:
// the account announces, a member keeps what the announce carried, the account
// is restored from its seed onto an empty device, asks the member, and comes
// back as itself — pictures, games and all.
func TestRestoredDeviceGetsItsProfileFromAMember(t *testing.T) {
	alice := recordService(t, mustID(t))
	want := recordProfile(1_700_000_000_000)
	storeProfile(t, alice, want)
	bob := recordService(t, mustID(t))

	m := announceOver(t, alice)
	bob.keepProfileRecord(m.Fingerprint, profileFromFrame(m), m.ProfileSeal)

	restored := restoredFrom(t, alice.id)
	if hint := bob.store.ProfileRecordAt(alice.id.Fingerprint()); hint != want.UpdatedAt {
		t.Fatalf("member's hint = %d, want %d", hint, want.UpdatedAt)
	}
	resp, err := bob.handleProfileRequest(context.Background(), peerOf(t, alice.id), []byte(`{"have":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if !restored.adoptProfileRecord(resp) {
		t.Fatal("the restored device refused its own profile")
	}
	got := restored.SelfProfile()
	if !profileMatches(got, want) || !hasGame(got.Games, "Outer Wilds") || got.UpdatedAt != want.UpdatedAt {
		t.Fatalf("restored profile = %+v, want %+v", got, want)
	}
}

// TestMemberServesOnlyTheAsker's own record: asking as anyone else gets nothing.
func TestMemberServesOnlyTheAskersOwnRecord(t *testing.T) {
	alice := recordService(t, mustID(t))
	storeProfile(t, alice, recordProfile(5))
	bob := recordService(t, mustID(t))
	m := announceOver(t, alice)
	bob.keepProfileRecord(m.Fingerprint, profileFromFrame(m), m.ProfileSeal)

	mallory := mustID(t)
	resp, err := bob.handleProfileRequest(context.Background(), peerOf(t, mallory), []byte(`{}`))
	if err != nil || string(resp) != "{}" {
		t.Fatalf("a stranger was served something: %q %v", resp, err)
	}
	// And the account itself, already as new as the record, is spared the bytes.
	resp, _ = bob.handleProfileRequest(context.Background(), peerOf(t, alice.id), []byte(`{"have":5}`))
	if string(resp) != "{}" {
		t.Fatalf("served a record no newer than the asker's: %d bytes", len(resp))
	}
}

// TestProfileRecordRefusals: each way a record can fail to be the account's
// own word is refused by the device it is served to.
func TestProfileRecordRefusals(t *testing.T) {
	alice := recordService(t, mustID(t))
	storeProfile(t, alice, recordProfile(10))
	good := profileRecord{Body: string(encodeProfileBody(alice.selfStoredProfile()))}
	good.Seal = sealProfileBody(alice.id, []byte(good.Body))
	mallory := mustID(t)

	tampered := good
	var b profileRecordBody
	_ = json.Unmarshal([]byte(good.Body), &b)
	b.Name = "Someone Else"
	nb, _ := json.Marshal(b)
	tampered.Body = string(nb)

	// Mallory's own, perfectly valid, record.
	mrec := profileRecord{Body: good.Body, Seal: sealProfileBody(mallory, []byte(good.Body))}

	// Alice's seal key, Mallory's signature: what a holder who somehow had the
	// seal key but not the account key could make.
	sig := mallory.Sign(profileSigned(alice.id.PublicKey(), []byte(good.Body)))
	var nonce [24]byte
	key := profileSealKey(alice.id)
	wrongSigner := profileRecord{Body: good.Body, Seal: secretboxSeal(nonce, sig, key)}

	for name, rec := range map[string]profileRecord{
		"unsigned":              {Body: good.Body},
		"body edited after":     tampered,
		"another account's":     mrec,
		"signed by another key": wrongSigner,
		"empty body":            {Seal: good.Seal},
	} {
		raw, _ := json.Marshal(rec)
		r := restoredFrom(t, alice.id)
		if r.adoptProfileRecord(raw) {
			t.Errorf("%s record was adopted", name)
		}
		if r.SelfProfile().Name == "Someone Else" || r.profileStamp() != 0 {
			t.Errorf("%s record changed the profile", name)
		}
	}
	raw, _ := json.Marshal(good)
	if !restoredFrom(t, alice.id).adoptProfileRecord(raw) {
		t.Fatal("the genuine record was refused (the refusals above prove nothing)")
	}
}

// TestProfileRecordNewestWins: a member keeps the newest record it was sent,
// and a device keeps the newest it can verify — so replaying an old record,
// even a genuine one, cannot roll either back.
func TestProfileRecordNewestWins(t *testing.T) {
	alice := recordService(t, mustID(t))
	bob := recordService(t, mustID(t))

	old := recordProfile(100)
	old.Name = "Old Name"
	storeProfile(t, alice, old)
	mOld := announceOver(t, alice)
	newer := recordProfile(200)
	storeProfile(t, alice, newer)
	mNew := announceOver(t, alice)

	bob.keepProfileRecord(alice.id.Fingerprint(), profileFromFrame(mNew), mNew.ProfileSeal)
	bob.keepProfileRecord(alice.id.Fingerprint(), profileFromFrame(mOld), mOld.ProfileSeal) // a replay
	if at := bob.store.ProfileRecordAt(alice.id.Fingerprint()); at != 200 {
		t.Fatalf("member holds stamp %d after a replay, want 200", at)
	}

	r := restoredFrom(t, alice.id)
	newRaw, _ := bob.handleProfileRequest(context.Background(), peerOf(t, alice.id), []byte(`{}`))
	oldRec := profileRecord{Body: string(encodeProfileBody(profileFromFrame(mOld))), Seal: mOld.ProfileSeal}
	oldRaw, _ := json.Marshal(oldRec)
	if !r.adoptProfileRecord(newRaw) {
		t.Fatal("newer record refused")
	}
	if r.adoptProfileRecord(oldRaw) {
		t.Fatal("an older genuine record rolled the profile back")
	}
	if got := r.SelfProfile().Name; got != newer.Name {
		t.Fatalf("name = %q, want %q", got, newer.Name)
	}
}

// TestOldAnnounceKeepsWorking: an announce from a build that predates the seal
// is learned exactly as before and leaves no record; a new announce decoded by
// the old field list is the same profile it always was.
func TestOldAnnounceKeepsWorking(t *testing.T) {
	alice := recordService(t, mustID(t))
	storeProfile(t, alice, recordProfile(42))
	bob := recordService(t, mustID(t))

	m := announceOver(t, alice)
	if len(m.ProfileSeal) == 0 {
		t.Fatal("a stamped profile was announced without a seal")
	}
	old := m
	old.ProfileSeal = nil
	bob.applyProfileMeta("g", alice.id.Fingerprint(), old)
	if !profileMatches(bob.ProfileOf(alice.id.Fingerprint()), recordProfile(42)) {
		t.Fatal("an unsealed (older build's) announce was not learned")
	}
	if bob.store.ProfileRecordAt(alice.id.Fingerprint()) != 0 {
		t.Fatal("a record was kept from an announce that carried no seal")
	}
	// A profile someone else announces "about" alice is bound away before
	// anything is kept.
	forged := m
	forged.Fingerprint = alice.id.Fingerprint()
	bob.applyProfileMeta("g", mustID(t).Fingerprint(), forged)
	if bob.store.ProfileRecordAt(alice.id.Fingerprint()) != 0 {
		t.Fatal("a record was kept from an announce by somebody else")
	}
	bob.applyProfileMeta("g", alice.id.Fingerprint(), m)
	if bob.store.ProfileRecordAt(alice.id.Fingerprint()) != 42 {
		t.Fatal("the account's own sealed announce left no record")
	}
}

// TestProvisionalNameNeverOutranksTheRealProfile is the restore-screen name:
// it names the device meanwhile and loses to the account's real profile.
func TestProvisionalNameNeverOutranksTheRealProfile(t *testing.T) {
	alice := recordService(t, mustID(t))
	storeProfile(t, alice, recordProfile(77))
	r := restoredFrom(t, alice.id)
	if err := r.SetProvisionalName("Typed On Restore"); err != nil {
		t.Fatal(err)
	}
	if r.profileStamp() != 0 {
		t.Fatal("a provisional name stamped the profile as an edit")
	}
	body := encodeProfileBody(alice.selfStoredProfile())
	raw, _ := json.Marshal(profileRecord{Body: string(body), Seal: sealProfileBody(alice.id, body)})
	if !r.adoptProfileRecord(raw) || r.SelfProfile().Name != recordProfile(77).Name {
		t.Fatalf("the real profile did not replace the provisional name: %q", r.SelfProfile().Name)
	}
	if err := r.SetProvisionalName("Again"); err == nil {
		t.Fatal("a provisional name was accepted over a real profile")
	}
}

func secretboxSeal(nonce [24]byte, msg []byte, key [32]byte) []byte {
	return secretbox.Seal(nonce[:], msg, &nonce, &key)
}

func gamesJSON(g []Game) string {
	if len(g) == 0 {
		return ""
	}
	b, _ := json.Marshal(g)
	return string(b)
}
