package app

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ZahakJ/concord/internal/domain"
)

// Guild calendar events: shared guild state, handled like every other kind —
// applied locally, persisted, announced MLS-encrypted over the guild-meta
// lane, gated on the RECEIVE side exactly as on the local side, and converged
// to fresh joiners through the history-sync snapshot (sync.go). Nothing in
// this file talks to any third party: "calendar interop" is the ICS file
// format (ics.go), never a vendor.

const (
	maxEventTitleRunes    = 120
	maxEventDetailsRunes  = 4000
	maxEventLocationRunes = 200
	// maxGuildEvents bounds one guild's calendar. Creating events is open to
	// every member (unlike GIFs or emoji), so without a ceiling any single
	// member could grow every other member's database without limit.
	maxGuildEvents = 500
	// maxEventRSVPs bounds the RSVP map on records arriving from outside.
	// Live event_rsvp frames add one authenticated member at a time, but a
	// doctored history-sync snapshot could ship a map with a million entries.
	maxEventRSVPs = 1024
	// Guest-link fields (see eventguest.go). A minted link is base + peer id +
	// token (~150 chars today); the caps leave headroom without letting a
	// record ship a paragraph where a URL belongs.
	maxEventGuestURLRunes  = 300
	maxEventGuestHostRunes = 128
	// The members' invite code into the same room ("CI1" + base64, a few
	// hundred chars today; multiaddr-heavy hosts run longer). Generous headroom
	// without letting a record smuggle an essay through a code-shaped field.
	maxEventMemberCodeRunes = 2048
)

// validEventID bounds an event id to the same charset as other shared ids —
// locally minted ids are domain.NewID() (32 lowercase hex) and pass. The
// bound is not tidiness: the id is interpolated into an ICS "UID:" line,
// where a control character would corrupt the exported calendar file.
func validEventID(id string) bool { return validPresetID(id) }

// validEventText refuses control and invisible formatting codepoints, the
// same screen validGifText holds: Svelte escapes text so this is not an XSS
// gate, it stops a peer shipping a title that is a screenful of newlines or
// bidi overrides. Details may span lines; title and location may not.
func validEventText(s string, maxRunes int, multiline bool) bool {
	if utf8.RuneCountInString(s) > maxRunes {
		return false
	}
	for _, r := range s {
		if multiline && (r == '\n' || r == '\r') {
			continue
		}
		if r < 0x20 || r == 0x7f || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cs, r) {
			return false
		}
	}
	return true
}

// validEvent validates a record from ANY source — the local create/edit path
// and the receive path run the same function, so the two cannot drift.
func validEvent(ev domain.Event) error {
	if !validEventID(ev.ID) {
		return fmt.Errorf("app: bad event id")
	}
	if ev.Title == "" || ev.Title != strings.TrimSpace(ev.Title) ||
		!validEventText(ev.Title, maxEventTitleRunes, false) {
		return fmt.Errorf("app: an event title must be 1–%d characters of plain text", maxEventTitleRunes)
	}
	if !validEventText(ev.Details, maxEventDetailsRunes, true) {
		return fmt.Errorf("app: event details must be at most %d characters", maxEventDetailsRunes)
	}
	if !validEventText(ev.Location, maxEventLocationRunes, false) {
		return fmt.Errorf("app: an event location must be at most %d characters", maxEventLocationRunes)
	}
	// A channel location is an id-shaped reference, never free text. Shape only
	// here: whether the channel actually exists in THIS guild is re-checked at
	// every point of use (card render, Join, the start announcement), because a
	// receive-side record can legitimately arrive before its channel has synced.
	if ev.LocationChannelID != "" && !validEventID(ev.LocationChannelID) {
		return fmt.Errorf("app: bad event location channel")
	}
	if ev.StartUnix <= 0 {
		return fmt.Errorf("app: an event needs a start time")
	}
	if ev.EndUnix != 0 && ev.EndUnix < ev.StartUnix {
		return fmt.Errorf("app: an event cannot end before it starts")
	}
	if len(ev.RSVPs) > maxEventRSVPs {
		return fmt.Errorf("app: too many RSVPs")
	}
	// Guest fields come as a pair or not at all: a URL with no accountable
	// host (or a host claim with no link) is a record nobody could revoke.
	if (ev.GuestURL == "") != (ev.GuestHost == "") {
		return fmt.Errorf("app: a guest link and its host go together")
	}
	if ev.GuestURL != "" {
		// The URL renders on every member's card and in ICS export; bound it like
		// any other single-line text and insist it at least looks like a link.
		if !validEventText(ev.GuestURL, maxEventGuestURLRunes, false) ||
			(!strings.HasPrefix(ev.GuestURL, "https://") && !strings.HasPrefix(ev.GuestURL, "http://")) {
			return fmt.Errorf("app: bad guest link")
		}
		// GuestHost is an account fingerprint — base32 blocks separated by
		// spaces — so the single-line text screen (no control characters,
		// bounded runes) is the right shape check.
		if !validEventText(ev.GuestHost, maxEventGuestHostRunes, false) {
			return fmt.Errorf("app: bad guest host")
		}
	}
	// The member join code exists only while a recorded host answers for the
	// room — a code with nobody accountable is a door nobody could close.
	if ev.MemberCode != "" {
		if ev.GuestHost == "" {
			return fmt.Errorf("app: a member join code needs a recorded host")
		}
		if !validEventText(ev.MemberCode, maxEventMemberCodeRunes, false) {
			return fmt.Errorf("app: bad member join code")
		}
	}
	return nil
}

func validRSVPState(state string) bool {
	switch state {
	case "", "going", "maybe", "no":
		return true
	}
	return false
}

// mayManageEvent reports whether actor may edit or delete an event: its
// author may, and so may a moderator holding Manage Messages — the same two
// arms forum-post curation uses (mayCuratePost), because a guild event is
// member content, not guild structure.
func (s *Service) mayManageEvent(guildID, actor string, ev domain.Event) bool {
	if actor == "" {
		return false
	}
	if actor == ev.CreatedBy {
		return true
	}
	return s.memberHasPerm(guildID, actor, PermManageMessages)
}

// eventGroup resolves a guild's MLS group id, the handle publishMeta needs.
func (s *Service) eventGroup(guildID string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.guilds[guildID]
	if !ok {
		return nil, false
	}
	return g.GroupID, true
}

// publishEvent announces an event over the guild-meta lane. The record's
// GuildID claim is stripped first — the per-guild, MLS-encrypted topic is the
// only authority on which guild it belongs to (same as a GIF record). RSVPs
// are stripped too: each answer travels its own event_rsvp frame bound to its
// authenticated sender, so an author cannot ship an event pre-filled with
// other members' names.
func (s *Service) publishEvent(groupID []byte, ev domain.Event) {
	ev.GuildID = ""
	ev.RSVPs = nil
	s.publishMeta(groupID, guildMeta{Type: "event_upserted", Event: &ev})
}

// locationChannelInGuild is the LOCAL courtesy check behind a channel-located
// event: creating one pointing at a channel this guild doesn't own is a typo
// worth an error, not a record worth publishing. It is deliberately NOT the
// enforcement — every consumer re-resolves the id against the event's own
// guild on its own copy of the state (the receive side), so a doctored record
// naming a foreign channel is inert everywhere it could matter.
func (s *Service) locationChannelInGuild(guildID, channelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.channelToGuild[channelID] == guildID
}

// CreateEvent adds an event to a guild's calendar. Any member may create —
// scheduling is participation, not administration — which is also exactly the
// bar the receive gate holds (MLS decryption proves membership).
// locationChannelID ties the event to one of THIS guild's channels ("" = the
// free-text location stands alone).
func (s *Service) CreateEvent(guildID, title, details string, startUnix, endUnix int64, location, locationChannelID string) (domain.Event, error) {
	groupID, ok := s.eventGroup(guildID)
	if !ok {
		return domain.Event{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	if locationChannelID != "" && !s.locationChannelInGuild(guildID, locationChannelID) {
		return domain.Event{}, fmt.Errorf("app: that channel isn't in this guild")
	}
	now := time.Now().Unix()
	ev := domain.Event{
		ID:                domain.NewID(),
		GuildID:           guildID,
		Title:             strings.TrimSpace(title),
		Details:           strings.TrimSpace(details),
		StartUnix:         startUnix,
		EndUnix:           endUnix,
		Location:          strings.TrimSpace(location),
		LocationChannelID: locationChannelID,
		CreatedBy:         s.id.Fingerprint(),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := validEvent(ev); err != nil {
		return domain.Event{}, err
	}
	if have, err := s.store.Events(guildID); err == nil && len(have) >= maxGuildEvents {
		return domain.Event{}, fmt.Errorf("app: this guild already has %d events — remove one first", maxGuildEvents)
	}
	if err := s.store.SaveEvent(ev); err != nil {
		return domain.Event{}, err
	}
	s.emitGuildUpdate()
	s.publishEvent(groupID, ev)
	return ev, nil
}

// UpdateEvent edits an event's details (author or Manage Messages). Author,
// creation time and RSVPs are untouched: answers belong to the members who
// gave them, and an edited time deliberately keeps them — flipping everyone
// back to unanswered because the start moved an hour would throw away more
// signal than it protects.
func (s *Service) UpdateEvent(guildID, eventID, title, details string, startUnix, endUnix int64, location, locationChannelID string) (domain.Event, error) {
	groupID, ok := s.eventGroup(guildID)
	if !ok {
		return domain.Event{}, fmt.Errorf("app: unknown guild %s", guildID)
	}
	if locationChannelID != "" && !s.locationChannelInGuild(guildID, locationChannelID) {
		return domain.Event{}, fmt.Errorf("app: that channel isn't in this guild")
	}
	existing, found, err := s.store.EventByID(eventID)
	if err != nil {
		return domain.Event{}, err
	}
	if !found || existing.GuildID != guildID {
		return domain.Event{}, fmt.Errorf("app: unknown event %s", eventID)
	}
	if !s.mayManageEvent(guildID, s.id.Fingerprint(), existing) {
		return domain.Event{}, fmt.Errorf("app: only the event's author or a moderator can edit it")
	}
	ev := existing
	ev.Title = strings.TrimSpace(title)
	ev.Details = strings.TrimSpace(details)
	ev.StartUnix = startUnix
	ev.EndUnix = endUnix
	ev.Location = strings.TrimSpace(location)
	ev.LocationChannelID = locationChannelID
	ev.UpdatedAt = time.Now().Unix()
	if ev.UpdatedAt <= existing.UpdatedAt {
		// Two edits inside one second must still order: sync convergence is
		// newest-wins on UpdatedAt, and a tie would make the second edit lose.
		ev.UpdatedAt = existing.UpdatedAt + 1
	}
	if err := validEvent(ev); err != nil {
		return domain.Event{}, err
	}
	if err := s.store.SaveEvent(ev); err != nil {
		return domain.Event{}, err
	}
	// GuestURL/GuestHost rode through untouched (ev started as a copy of
	// existing): the link stays stable across edits. If this node hosts the
	// room, its lifetime follows the new times.
	s.syncEventGuestExpiry(ev)
	s.emitGuildUpdate()
	s.publishEvent(groupID, ev)
	return ev, nil
}

// DeleteEvent removes an event (author or Manage Messages) and announces the
// removal.
func (s *Service) DeleteEvent(guildID, eventID string) error {
	groupID, ok := s.eventGroup(guildID)
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	existing, found, err := s.store.EventByID(eventID)
	if err != nil {
		return err
	}
	if !found || existing.GuildID != guildID {
		return fmt.Errorf("app: unknown event %s", eventID)
	}
	if !s.mayManageEvent(guildID, s.id.Fingerprint(), existing) {
		return fmt.Errorf("app: only the event's author or a moderator can delete it")
	}
	if err := s.store.DeleteEvent(guildID, eventID); err != nil {
		return err
	}
	// A deleted event takes its guest room down with it (when this node hosts
	// one) — a link into a party that no longer exists is a trap.
	s.teardownEventGuestRoom(eventID)
	s.emitGuildUpdate()
	s.publishMeta(groupID, guildMeta{Type: "event_removed", EventID: eventID})
	return nil
}

// Events returns a guild's calendar, ordered by start time.
func (s *Service) Events(guildID string) ([]domain.Event, error) {
	return s.store.Events(guildID)
}

// RSVP records this account's answer to an event: going|maybe|no, or "" to
// clear it. Any member; one answer per account.
func (s *Service) RSVP(guildID, eventID, state string) error {
	if !validRSVPState(state) {
		return fmt.Errorf("app: an RSVP must be going, maybe, no, or empty to clear it")
	}
	groupID, ok := s.eventGroup(guildID)
	if !ok {
		return fmt.Errorf("app: unknown guild %s", guildID)
	}
	if err := s.applyEventRSVP(guildID, s.id.Fingerprint(), eventID, state); err != nil {
		return err
	}
	s.publishMeta(groupID, guildMeta{Type: "event_rsvp", EventID: eventID, RSVP: state})
	return nil
}

// applyEventRSVP is the shared half of RSVP: the local call runs it with our
// own fingerprint, receiveGuildMeta runs it with the MLS-authenticated
// sender. The answer binds to the ACTOR — no payload field names a target —
// so nobody can RSVP on someone else's behalf, on any peer.
func (s *Service) applyEventRSVP(guildID, actor, eventID, state string) error {
	if actor == "" || !validRSVPState(state) {
		return fmt.Errorf("app: bad rsvp")
	}
	ev, found, err := s.store.EventByID(eventID)
	if err != nil {
		return err
	}
	if !found || ev.GuildID != guildID {
		// An RSVP that outran its event (two gossip frames, no cross-frame
		// ordering) is dropped here; history sync carries the full RSVP map
		// with the event later, so nothing is permanently lost.
		return fmt.Errorf("app: unknown event %s", eventID)
	}
	if state == "" {
		if _, had := ev.RSVPs[actor]; !had {
			return nil
		}
		delete(ev.RSVPs, actor)
	} else {
		if ev.RSVPs == nil {
			ev.RSVPs = map[string]string{}
		}
		if ev.RSVPs[actor] == state {
			return nil
		}
		ev.RSVPs[actor] = state
	}
	// Bump UpdatedAt so the answer travels with the event through the
	// newest-wins history-sync path, not only through live gossip.
	if now := time.Now().Unix(); now > ev.UpdatedAt {
		ev.UpdatedAt = now
	} else {
		ev.UpdatedAt++
	}
	if err := s.store.SaveEvent(ev); err != nil {
		return err
	}
	s.emitGuildUpdate()
	return nil
}

// applyEventUpsert applies an event_upserted announcement from a peer. Any
// member may CREATE (MLS decryption already proves membership — the same bar
// the local path sets), but the recorded author is the authenticated sender,
// never the payload's claim. An UPDATE must come from the event's author or a
// moderator holding Manage Messages, re-checked HERE on the receive side —
// a modified client must be ignorable, or the permission is decorative.
func (s *Service) applyEventUpsert(guildID, actor string, ev domain.Event) {
	if actor == "" {
		return
	}
	// The record's own GuildID claim is discarded: the only authority on which
	// guild an event belongs to is the MLS-encrypted topic it arrived on.
	ev.GuildID = guildID
	if validEvent(ev) != nil {
		return
	}
	existing, found, err := s.store.EventByID(ev.ID)
	if err != nil {
		return
	}
	if found {
		// Refuse an id that already belongs to another guild — the events
		// table is keyed by id alone, so without this a member of one guild
		// could rewrite another guild's event through the shared primary key.
		if existing.GuildID != guildID {
			return
		}
		if !s.mayManageEvent(guildID, actor, existing) {
			return
		}
		// Author, creation time and RSVPs are immutable through this lane:
		// answers travel their own event_rsvp frames, so an edit must not
		// wipe (or invent) them.
		ev.CreatedBy, ev.CreatedAt, ev.RSVPs = existing.CreatedBy, existing.CreatedAt, existing.RSVPs
		// Guest access belongs to the account whose node hosts the room. Anyone
		// else's edit carries the fields through UNCHANGED — so a moderator
		// editing from a stale copy cannot kill a live link, and a forged frame
		// cannot re-point guests at an attacker's room or claim someone else
		// hosts one. GuestHost's own frames (their linked devices included) may
		// set, move or clear freely — their node is where the room lives. The
		// member join code is the same room through another door, so it lives
		// and dies by exactly the same rule.
		if actor != existing.GuestHost && existing.GuestHost != "" {
			ev.GuestURL, ev.GuestHost, ev.MemberCode = existing.GuestURL, existing.GuestHost, existing.MemberCode
		}
		if ev.GuestHost != "" && ev.GuestHost != actor && existing.GuestHost == "" {
			ev.GuestURL, ev.GuestHost, ev.MemberCode = "", "", "" // nobody opens a room in someone else's name
		}
	} else {
		if have, err := s.store.Events(guildID); err == nil && len(have) >= maxGuildEvents {
			return
		}
		ev.CreatedBy = actor // authorship is authenticated, not asserted
		ev.RSVPs = nil       // nobody arrives pre-RSVP'd on the author's say-so
		if ev.GuestHost != actor {
			ev.GuestURL, ev.GuestHost, ev.MemberCode = "", "", "" // same rule on a fresh record
		}
		if ev.CreatedAt <= 0 {
			ev.CreatedAt = time.Now().Unix()
		}
	}
	if s.store.SaveEvent(ev) != nil {
		return
	}
	// This node may host the event's guest room while another member (or our
	// own linked device) edits it: follow the applied times, and honour a
	// clear our other device published by tearing the room down here — the
	// only place it physically exists.
	if found && existing.GuestURL != "" && ev.GuestURL == "" {
		s.teardownEventGuestRoom(ev.ID)
	}
	s.syncEventGuestExpiry(ev)
	s.emitGuildUpdate()
}

// applyEventRemove is the receive half of DeleteEvent, mirroring its checks.
func (s *Service) applyEventRemove(guildID, actor, eventID string) {
	if eventID == "" || actor == "" {
		return
	}
	existing, found, err := s.store.EventByID(eventID)
	if err != nil || !found || existing.GuildID != guildID {
		return
	}
	if !s.mayManageEvent(guildID, actor, existing) {
		return
	}
	if s.store.DeleteEvent(guildID, eventID) != nil {
		return
	}
	// A moderator elsewhere deleted the event; if the guest room lives HERE,
	// this is the only node that can actually close the door — do it.
	s.teardownEventGuestRoom(eventID)
	s.emitGuildUpdate()
}

// applySyncedEvent folds one event served by a history-sync responder into
// local state. Same trust boundary as the GIF and emoji records beside it in
// applySyncPayload: catch-up is attested by whichever member answered, not by
// the event's author, so authorship cannot be re-verified here — the record
// rides the same responder trust messages do (see the note above the gif loop
// in applySyncPayload for what closing that fully would take). Adoption is
// newest-wins by UpdatedAt with RSVPs merged (theirs overlaid on ours), so
// answers given while the two sides were apart both survive; the cost is that
// a CLEARED answer can be resurrected by a peer that never saw the clear,
// which the next live event_rsvp frame corrects.
func (s *Service) applySyncedEvent(guildID string, ev domain.Event) {
	ev.GuildID = guildID
	for fpr, st := range ev.RSVPs {
		if fpr == "" || st == "" || !validRSVPState(st) {
			delete(ev.RSVPs, fpr)
		}
	}
	if validEvent(ev) != nil {
		return
	}
	existing, found, err := s.store.EventByID(ev.ID)
	if err != nil {
		return
	}
	if found {
		if existing.GuildID != guildID {
			return // same cross-guild-hijack refusal as applyEventUpsert
		}
		if ev.UpdatedAt <= existing.UpdatedAt {
			return
		}
		ev.CreatedBy, ev.CreatedAt = existing.CreatedBy, existing.CreatedAt
		// Same guest-field conservatism as the live lane, minus the actor (a
		// sync responder attests nothing): once a host is on record, a synced
		// copy can neither clear the link nor re-point it. A stale card is the
		// worst outcome — a revoked link's room is already gone, so the link
		// fails honestly — whereas adopting a doctored snapshot's URL would
		// aim guests at an attacker's room.
		if existing.GuestHost != "" {
			ev.GuestURL, ev.GuestHost, ev.MemberCode = existing.GuestURL, existing.GuestHost, existing.MemberCode
		}
		merged := existing.RSVPs
		if merged == nil {
			merged = map[string]string{}
		}
		for fpr, st := range ev.RSVPs {
			merged[fpr] = st
		}
		ev.RSVPs = merged
	} else if have, err := s.store.Events(guildID); err == nil && len(have) >= maxGuildEvents {
		return
	}
	if s.store.SaveEvent(ev) != nil {
		return
	}
	s.emitGuildUpdate()
}

// ---- in-channel start announcements ----
//
// A channel-located event gets ONE shared "it's happening" beat: a system
// message, spoken in the GUILD's name, posted into the event's own channel
// just before it starts. This is deliberately not the personal reminder
// (lib/scheduled.svelte.js — local notification, stays local): the personal
// one belongs to whoever asked for it, the in-channel one belongs to the
// event. Exactly one node posts it — the event's AUTHOR — because five
// members with reminders posting five "it's starting" lines is spam, and the
// author is the one deterministic, accountable identity every copy of the
// record already agrees on (the same single-writer philosophy as GuestHost
// owning the guest room). The cost is honest: an author whose devices are all
// offline at start time announces nothing, which beats electing a poster via
// a coordination protocol this feature does not deserve.

const (
	// eventAnnounceLead is the pre-roll: the announcement lands a few minutes
	// before start so "come to the lounge" arrives while it can still be acted
	// on, not after everyone is already late.
	eventAnnounceLead = 5 * 60 // seconds
	// eventAnnounceGrace keeps a briefly-offline author useful: booting within
	// this window past start still announces ("is starting" is still true-ish),
	// while booting the next day stays silent instead of necro-posting.
	eventAnnounceGrace = 10 * 60 // seconds
	// eventAnnounceTick paces the sweep. Coarse on purpose — the lead is
	// minutes, so a half-minute of jitter is invisible, and the differing tick
	// phase between an author's linked devices is what usually serializes them
	// ahead of the message-scan dedup below.
	eventAnnounceTick = 30 * time.Second
)

// runEventAnnounceLoop sweeps for channel-located events entering their start
// window. Started once at service start; lives until shutdown.
//
// Paced (bgPace) like every other periodic loop: this one was missed by the
// background-pacing sweep and kept waking a backgrounded phone every thirty
// seconds forever. The work is a store read, so the cost is not the query — it
// is the wake-up. A backgrounded announcement lands within one beat of its
// time, and the bgWake case fires the sweep the moment the app is back.
func (s *Service) runEventAnnounceLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.bgWakeCh():
			// Foregrounded: announce anything that came due during the beat.
		case <-time.After(s.bgPace(eventAnnounceTick)):
		}
		s.announceDueEvents(time.Now().Unix())
	}
}

// announceDueEvents posts the start announcement for every event this node is
// responsible for whose window [start-lead, start+grace] contains now.
// Split from the loop so tests can drive the time boundary directly.
func (s *Service) announceDueEvents(now int64) {
	s.mu.RLock()
	gids := make([]string, 0, len(s.guilds))
	for id := range s.guilds {
		gids = append(gids, id)
	}
	s.mu.RUnlock()
	me := s.id.Fingerprint()
	for _, gid := range gids {
		evs, err := s.store.Events(gid)
		if err != nil {
			continue
		}
		for _, ev := range evs {
			if ev.LocationChannelID == "" || ev.CreatedBy != me {
				continue // free-text/external event, or not ours to announce
			}
			if now < ev.StartUnix-eventAnnounceLead || now > ev.StartUnix+eventAnnounceGrace {
				continue
			}
			s.announceEventStart(ev)
		}
	}
}

// announceEventStart posts the single in-channel announcement for one event,
// deduplicated at three layers: a local "announced" marker (this node never
// posts twice, across restarts), a scan of the channel's recent messages for
// the identical announcement (an author's OTHER linked device — same
// fingerprint, so equally "the author" — may have posted first; its message
// syncs to us like any other), and only then the send. The content string is
// fully determined by the event record, which is what makes the scan a real
// equality check rather than a heuristic.
func (s *Service) announceEventStart(ev domain.Event) {
	if done, err := s.store.EventAnnounced(ev.ID); err != nil || done {
		return
	}
	// RECEIVE-side gate at the point of consequence: resolve the claimed
	// channel against OUR copy of the event's own guild. A record naming a
	// foreign guild's channel (doctored client) or a channel that no longer
	// exists posts nothing — and an honestly not-yet-synced channel simply
	// retries on the next tick while the window is open.
	s.mu.RLock()
	inGuild := s.channelToGuild[ev.LocationChannelID] == ev.GuildID
	var guildName, chName, chType string
	if g, ok := s.guilds[ev.GuildID]; ok {
		guildName = g.Name
		for _, c := range g.Channels {
			if c.ID == ev.LocationChannelID {
				chName, chType = c.Name, c.Type
			}
		}
	}
	s.mu.RUnlock()
	if !inGuild || chName == "" || guildName == "" {
		return
	}
	content := eventAnnounceText(ev.Title, chName, chType)
	// Cross-device dedup: if the identical announcement already sits in the
	// channel (posted by our other device, or by us before a lost marker),
	// adopt it instead of repeating it.
	if msgs, err := s.store.Messages(ev.LocationChannelID, 60); err == nil {
		for _, m := range msgs {
			if m.Kind == "system" && m.Content == content {
				_ = s.store.MarkEventAnnounced(ev.ID)
				return
			}
		}
	}
	// Spoken by the guild, not by a member: the user-facing rule is "the GUILD
	// reminds people". Name is the same self-asserted, decorative display field
	// every message carries — this changes who the line READS as, not any
	// authenticated fact (the signature is still this node's).
	if _, err := s.sendAs(ev.LocationChannelID, content, "system", "", guildName, ""); err != nil {
		return // transient send failure: leave unmarked so the next tick retries
	}
	_ = s.store.MarkEventAnnounced(ev.ID)
}

// eventAnnounceText is the one announcement string for an event — a pure
// function of the record so every device of the author computes the same
// bytes, which the dedup scan above depends on.
func eventAnnounceText(title, chName, chType string) string {
	if chType == "voice" {
		return fmt.Sprintf("⏰ %s is starting — join the call in 🔊 %s", title, chName)
	}
	return fmt.Sprintf("⏰ %s is starting here in #%s — come on in", title, chName)
}
