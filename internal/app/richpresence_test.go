package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// track is a now-playing snapshot: one song, at one position, at one instant.
func track(title string, posMs int64, at time.Time) *Activity {
	return &Activity{
		Artist: "Kamkars", Title: title, DurationMs: 240_000,
		PositionMs: posMs, AtMs: at.UnixMilli(),
	}
}

// TestTrackChangeDoesNotReshipTheProfile is the point of the whole exercise.
//
// A profile announce carries the avatar and the profile banner — up to 320 KiB
// of base64 — MLS-encrypted and flooded to every guild's meta topic. Rich
// presence hung a change of song on that announce, so somebody listening to an
// album re-published their pictures to every guild they are in, once per track
// and again on every seek, to deliver a string. This asserts the announce keeps
// its mouth shut: the record of what we last said about our profile, and to
// whom, must be exactly where the track change found it.
func TestTrackChangeDoesNotReshipTheProfile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	if err := a.SetProfile(Profile{Name: "avicenna", Avatar: image(40 << 10), Banner: image(60 << 10)}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	g, err := a.CreateGuild("hamadan")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	if !a.publishProfile(g.ID, true) {
		t.Fatal("the baseline profile announce said nothing")
	}
	a.mu.RLock()
	before := a.announcedProfile[g.ID].stamp
	a.mu.RUnlock()

	base := time.Now()
	for i, title := range []string{"Ey Ranjbar", "Bahar Bahar", "Sarbaz"} {
		at := base.Add(time.Duration(i) * 3 * time.Minute)
		a.updateActivityAt("🎵 Kamkars — "+title, track(title, 0, at), at)
		a.mu.RLock()
		after := a.announcedProfile[g.ID].stamp
		a.mu.RUnlock()
		if after != before {
			t.Fatalf("track %d re-published the whole profile — avatar and banner included", i+1)
		}
	}

	// And the tracks did travel, on a frame with no room in it for a picture.
	frame := a.activityFrame()
	if frame.Type != "activity" {
		t.Fatalf("the activity frame has type %q", frame.Type)
	}
	if frame.Activity == nil || frame.Activity.Title != "Sarbaz" {
		t.Fatal("the activity frame did not carry the current track")
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal activity frame: %v", err)
	}
	if strings.Contains(string(raw), "data:image/") {
		t.Fatalf("the activity frame carries image bytes: %s", raw)
	}
	if len(raw) > 1024 {
		t.Fatalf("the activity frame is %d bytes; it should be a few hundred", len(raw))
	}
	// For scale: what the old path put on the wire to say the same thing.
	full, _ := json.Marshal(profileFrame(a.selfWireProfile(), a.id.Fingerprint()))
	if len(full) < 90<<10 {
		t.Fatalf("this test is not measuring what it claims: the full announce is only %d bytes", len(full))
	}
	t.Logf("one track change: %d bytes on the activity frame, %d on the profile announce it replaces", len(raw), len(full))
}

// TestTrackChangeDoesNotMoveTheSyncInventory is the other half of the same
// bill. Silencing the announce is worth nothing if anti-entropy picks the cost
// straight back up: the sync request carries a content hash per profile, and
// anything a song moves in that hash makes every beat find a difference and
// serve the whole profile to settle it — the same pictures, one lane over.
func TestTrackChangeDoesNotMoveTheSyncInventory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	if err := a.SetProfile(Profile{Name: "avicenna", Avatar: image(40 << 10)}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}

	// Ours, as the responder hashes it.
	silent := digestProfile(a.selfStoredProfile())
	// Theirs, as a peer hashes the copy our announce left them holding.
	heard := func() string {
		return digestProfile(profileFromFrame(profileFrame(a.selfWireProfile(), a.id.Fingerprint())))
	}
	silentHeard := heard()

	base := time.Now()
	a.updateActivityAt("🎵 Kamkars — Ey Ranjbar", track("Ey Ranjbar", 0, base), base)
	if got := digestProfile(a.selfStoredProfile()); got != silent {
		t.Fatal("starting playback moved our own profile hash: every peer is now served the whole profile every beat")
	}
	if got := heard(); got != silentHeard {
		t.Fatal("starting playback moved the hash of what peers cache about us")
	}
	next := base.Add(3 * time.Minute)
	a.updateActivityAt("🎵 Kamkars — Sarbaz", track("Sarbaz", 0, next), next)
	if got := heard(); got != silentHeard {
		t.Fatal("a change of track moved the hash of what peers cache about us")
	}
	// A status the user actually typed is a different matter and must move it.
	if err := a.SetProfile(Profile{Name: "avicenna", Status: "in the library", Avatar: image(40 << 10)}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	if got := digestProfile(a.selfStoredProfile()); got == silent {
		t.Fatal("a real status edit left the sync hash alone, so nobody will ever be told")
	}
}

// TestActivityAnnounceNeverBlanksACachedProfile is the trap this design was
// built around. The profile applier reads an absent field as a cleared one for
// everything except a name and a mailbox key, so a slim announce sent down the
// profile lane would have wiped the avatar, banner, bio and colours of every
// member whose music we could hear. The activity lane cannot reach those
// fields at all.
func TestActivityAnnounceNeverBlanksACachedProfile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	const fpr = "A FRIEND WHO LISTENS TO MUSIC"
	full := Profile{
		Name: "Rudaki", Status: "writing", Emoji: "🌙", Color: "#aabbcc", Color2: "#112233",
		Avatar: image(8 << 10), Banner: image(12 << 10), Bio: "poet", Presence: "idle",
		Frame: "gold", Effect: "sparkle", MailboxPub: []byte("a mailbox key"),
	}
	a.learnProfile(fpr, full)
	cached := a.ProfileOf(fpr)
	if cached.Avatar == "" || cached.Banner == "" {
		t.Fatalf("this test needs a profile with art on it, and got %+v", cached)
	}

	// A song starts, another follows, then the music stops.
	for _, act := range []*Activity{
		track("Ey Ranjbar", 0, time.Now()),
		track("Bahar Bahar", 0, time.Now()),
		nil,
	} {
		a.applyActivityMeta(fpr, guildMeta{Type: "activity", Fingerprint: fpr, Activity: act})
		got := a.ProfileOf(fpr)
		got.Activity = nil
		if !profilesEqual(got, cached) {
			t.Fatalf("an activity announce edited something other than the activity:\n got %+v\nwant %+v", got, cached)
		}
		switch live := a.ProfileOf(fpr).Activity; {
		case act == nil && live != nil:
			t.Fatal("the music stopped and the card still shows a track")
		case act != nil && (live == nil || live.Title != act.Title):
			t.Fatalf("the track never reached the cached profile: %+v", live)
		}
	}

	// An old-style FULL announce still works, activity and all — that lane is
	// untouched, and it is the one that introduces a member in the first place.
	a.applyProfileMeta("some-guild", fpr, profileFrame(Profile{
		Name: "Rudaki of Panjrud", Avatar: image(9 << 10), Banner: full.Banner, Bio: full.Bio,
		MailboxPub: full.MailboxPub, Activity: track("Rubaiyat", 0, time.Now()),
	}, fpr))
	got := a.ProfileOf(fpr)
	if got.Name != "Rudaki of Panjrud" || got.Avatar == cached.Avatar {
		t.Fatalf("a full profile announce no longer applies: %+v", got)
	}
	if got.Activity == nil || got.Activity.Title != "Rubaiyat" {
		t.Fatal("a full profile announce no longer carries the activity")
	}
}

// TestActivityFromAStrangerIsIgnored: an activity frame is an edit to a cached
// row, so it may only edit the row of the member MLS says wrote it.
func TestActivityFromAStrangerIsIgnored(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	const victim, forger = "THE VICTIM", "THE FORGER"
	a.learnProfile(victim, Profile{Name: "Rudaki"})
	a.learnProfile(forger, Profile{Name: "Somebody Else"})

	a.applyActivityMeta(forger, guildMeta{
		Type: "activity", Fingerprint: victim, Activity: track("Not My Song", 0, time.Now()),
	})
	if a.ProfileOf(victim).Activity != nil {
		t.Fatal("a member put a song on somebody else's card")
	}
	// A member we have never met gets no row conjured for them either.
	a.applyActivityMeta("A COMPLETE STRANGER", guildMeta{Type: "activity", Activity: track("Hello", 0, time.Now())})
	a.mu.RLock()
	_, known := a.profiles["A COMPLETE STRANGER"]
	a.mu.RUnlock()
	if known {
		t.Fatal("an activity frame invented a profile for someone we have never met")
	}
	// And art that is not fetchable art never reaches a client.
	a.applyActivityMeta(victim, guildMeta{Type: "activity", Fingerprint: victim, Activity: &Activity{
		Title: "A Song", ArtURL: "javascript:alert(1)",
	}})
	if got := a.ProfileOf(victim).Activity; got == nil || got.ArtURL != "" {
		t.Fatalf("an unfetchable art URL was cached: %+v", got)
	}
}

// TestActivityReachesAPeerWithoutTheProfile drives the whole lane between two
// real peers: A puts an album on, B must see each track appear on A's card and
// must still be holding A's avatar and banner at the end of it. This is the
// leg the unit tests cannot speak for — that the frame is published, decrypts,
// binds to its author and lands on the right row.
func TestActivityReachesAPeerWithoutTheProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("network integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	b := startService(t, ctx)
	if err := a.SetProfile(Profile{Name: "avicenna", Avatar: image(40 << 10), Banner: image(60 << 10), Status: "in the library"}); err != nil {
		t.Fatalf("SetProfile: %v", err)
	}
	g, err := a.CreateGuild("hamadan")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	code, err := a.InviteCode(g.ID)
	if err != nil {
		t.Fatalf("InviteCode: %v", err)
	}
	if _, err := b.JoinViaInvite(code); err != nil {
		t.Fatalf("JoinViaInvite: %v", err)
	}
	waitMembers(t, 30*time.Second, 2, a, b)
	waitUntil(t, 30*time.Second, func() bool {
		return b.ProfileOf(a.Fingerprint()).Avatar != ""
	}, "B never learned A's profile at all")
	before := b.ProfileOf(a.Fingerprint())

	base := time.Now()
	for i, title := range []string{"Ey Ranjbar", "Bahar Bahar", "Sarbaz"} {
		at := base.Add(time.Duration(i) * 3 * time.Minute)
		a.updateActivityAt("🎵 Kamkars — "+title, track(title, 0, at), at)
		waitUntil(t, 30*time.Second, func() bool {
			act := b.ProfileOf(a.Fingerprint()).Activity
			return act != nil && act.Title == title
		}, "a track never reached the other peer")

		got := b.ProfileOf(a.Fingerprint())
		got.Activity = nil
		if !profilesEqual(got, before) {
			t.Fatalf("track %q disturbed the cached profile:\n got %+v\nwant %+v", title, got, before)
		}
	}
	// The music stops and only the music stops.
	a.updateActivityAt("", nil, base.Add(10*time.Minute))
	waitUntil(t, 30*time.Second, func() bool {
		return b.ProfileOf(a.Fingerprint()).Activity == nil
	}, "the other peer still shows a track after playback stopped")
	if got := b.ProfileOf(a.Fingerprint()); !profilesEqual(got, before) {
		t.Fatalf("stopping playback disturbed the cached profile:\n got %+v\nwant %+v", got, before)
	}
}

// TestSeekAnnouncesAreCalmed: a track change is news and travels at once, but
// dragging a scrubber is the same track at a different place, and the poller
// notices every eight seconds. Without a floor, a minute of fiddling with a
// progress bar is a broadcast to every guild seven times over.
func TestSeekAnnouncesAreCalmed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := startService(t, ctx)
	base := time.Now()
	const line = "🎵 Kamkars — Ey Ranjbar"

	said := func() int64 {
		a.activityMu.Lock()
		defer a.activityMu.Unlock()
		return a.lastActivitySay.UnixNano()
	}
	a.updateActivityAt(line, track("Ey Ranjbar", 0, base), base)
	first := said()

	// A scrub on every poll for half a minute. Each jump is far enough that
	// clients' extrapolated progress is wrong, and none of it may go out.
	for i := 1; i <= 3; i++ {
		at := base.Add(time.Duration(i) * richPresenceInterval)
		a.updateActivityAt(line, track("Ey Ranjbar", int64(i)*90_000, at), at)
		if said() != first {
			t.Fatalf("seek %d was broadcast inside the %s floor", i, activitySeekCalm)
		}
	}
	// Past the floor the drift is still there to be measured — the calmed seeks
	// were not quietly adopted as though peers had heard them — so it is said.
	late := base.Add(activitySeekCalm + time.Second)
	a.updateActivityAt(line, track("Ey Ranjbar", 200_000, late), late)
	if said() == first {
		t.Fatalf("a seek was never announced at all, %s after the last word", activitySeekCalm)
	}
	if got := a.SelfProfile().Activity; got == nil || got.PositionMs != 200_000 {
		t.Fatalf("the recorded snapshot is not the one we last broadcast: %+v", got)
	}

	// A different track one second later ignores the floor entirely.
	next := late.Add(time.Second)
	a.updateActivityAt("🎵 Kamkars — Sarbaz", track("Sarbaz", 0, next), next)
	if got := a.SelfProfile().Activity; got == nil || got.Title != "Sarbaz" {
		t.Fatalf("a track change was held back by the seek floor: %+v", got)
	}
	// Stopping playback is not a seek either: the overlay drops at once.
	a.updateActivityAt("", nil, next.Add(time.Second))
	if got := a.SelfProfile(); got.Activity != nil {
		t.Fatal("the music stopped and our own card still shows a track")
	}
}
